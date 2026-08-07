package l2quote

import (
	"fmt"
	"strings"

	jsoniter "github.com/json-iterator/go"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"github.com/AnuBookDEX/engine/internal/centralized/rabbitmq"
	"time"
)

type MsgBundle struct {
	CMD  string              `json:"cmd"`
	Data jsoniter.RawMessage `json:"data"`
}

type MqMessage struct {
	Ts         time.Time
	Type       string
	Interval   string
	PairCode   string
	RoutingKey string
	Body       []byte
}

// RawPublisher 可选的 WS 发布器：DEX 模式下由引擎注入 WS Hub
// （核心包不依赖 dex 包，通过函数注入避免反向依赖）
type RawPublisher func(channel string, data []byte)

// wsChannelFromRoutingKey 将 MQ routing key 映射为 WS 频道名：
//
//	market.{symbol}.kline.{interval} -> kline.{symbol}.{interval}
//	market.{symbol}.trade.detail     -> trade.{symbol}
//	market.{symbol}.ticker           -> ticker.{symbol}
func wsChannelFromRoutingKey(key string) string {
	parts := strings.SplitN(key, ".", 4)
	if len(parts) < 3 {
		return ""
	}
	switch parts[2] {
	case "kline":
		if len(parts) < 4 {
			return ""
		}
		return "kline." + parts[1] + "." + parts[3]
	case "trade":
		return "trade." + parts[1]
	case "ticker":
		return "ticker." + parts[1]
	}
	return ""
}

/*
伪MQ批量发送协程
逻辑：尽可能实时，尽可能打包
*/
func (L *L2quote) sendToMQ(mqCh chan *MqMessage) {
	var count int64
	var publishedPeriod int
	reportTicker := time.NewTicker(time.Second * 10)
	for {
		select {
		case <-reportTicker.C:
			common.Info(fmt.Sprintf("%s l2quote MQ status --- count[%d] published[%d/10sec]", L.symbol, count, publishedPeriod))
			publishedPeriod = 0
		case msg := <-mqCh:
			//case <-mqCh:
			// 批量发送的逻辑
			size := len(mqCh)
			if size > L.mqBatchSize {
				size = L.mqBatchSize
			}
			beans := BatchMqPub(msg, mqCh, size)
			count = count + int64(size) + 1
			publishedPeriod = publishedPeriod + size + 1

			// DEX 模式：注入 WS 发布器时直连 Hub 广播（替代 RabbitMQ）
			if L.rawPublisher != nil {
				for _, bean := range beans {
					ch := wsChannelFromRoutingKey(bean.CMD)
					if ch != "" {
						L.rawPublisher(ch, bean.Data)
					}
				}
				continue
			}

			data, err := json.Marshal(beans)
			if err != nil {
				common.Fatal(L.symbol, "quotation msg beans jsonlize error :", err)
			}
			rabbitmqCh := rabbitmq.GetMatchResultRabbitMq(L.symbol)
			// DEX 模式下 RabbitMQ 未初始化，跳过发布
			if rabbitmqCh == nil {
				continue
			}
			// 打包后，发送到mq的routing key已经没有实际效果，写死一个
			routeKey := fmt.Sprintf("market.%s.kline.1min", L.symbol)
			exchangeName := config.GetString("app.profile", "market") + "." + config.GetString("rabbitmq.exchange.quotation", "l2quote")

			rabbitmq.PublishWithChan(rabbitmqCh, exchangeName, routeKey, data, msg.Ts.UnixNano()/int64(time.Millisecond))
		}
	}
}

func BatchMqPub(msg *MqMessage, ch chan *MqMessage, size int) (beans []*MsgBundle) {
	bean := &MsgBundle{}
	bean.CMD = msg.RoutingKey
	bean.Data = msg.Body
	beans = append(beans, bean)
	count := size
	for count > 0 {
		m := <-ch
		bean = &MsgBundle{}
		bean.CMD = m.RoutingKey
		bean.Data = m.Body
		beans = append(beans, bean)
		count--
	}
	return beans
}
