package market

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"github.com/AnuBookDEX/engine/internal/centralized/rabbitmq"

	jsoniter "github.com/json-iterator/go"
)

var json = jsoniter.ConfigCompatibleWithStandardLibrary

// 深度超时日志限频：稳态下深度构建持续超时（积压场景）时，
// 每条都打 Info 会洪泛日志（每小时数百 MB），10 秒最多 1 条
var lastDepthTimeoutLogMs int64

type MarketThread struct {
	// 交易对标识
	symbol string
	// 交易所标识
	exchange string
	// 深度消息队列
	depthChan chan *QuoteDepths
}

type MsgBundle struct {
	CMD  string `json:"cmd"`
	Data *QuoteDepths
}

//目前Thread只保留了一个向mq发送消息的作用，鉴于扩展考虑，先保留这个结构
func (m *MarketThread) thread() {
	for {
		ts00 := time.Now().UnixNano()
		depth := <-m.depthChan
		batchSize := config.GetInt64("batch_result", 90) - 1
		chanLen := len(m.depthChan)
		sizeToBatch := int(math.Min(float64(batchSize), float64(chanLen)))
		sizeToBatch = int(math.Max(float64(sizeToBatch), 1))
		depthArr := make([]*QuoteDepths, sizeToBatch)
		depthArr[0] = depth
		for i := 1; i < sizeToBatch; i++ {
			_depth := <-m.depthChan
			depthArr[i] = _depth
		}
		bundleArr := make([]MsgBundle, sizeToBatch)
		for i, dpt := range depthArr {
			bundle := MsgBundle{}
			bundle.CMD = dpt.ch
			bundle.Data = dpt
			bundleArr[i] = bundle
		}
		content, err := json.Marshal(bundleArr)
		ts01 := time.Now().UnixNano()
		// bundle := MsgBundle{}
		// bundle.CMD = depth.Ch
		// bundle.Data = depth
		// content, err := json.Marshal([]MsgBundle{bundle})
		if err != nil {
			common.Fatal("content encode json error:", err)
		}
		rabbitmqCh := rabbitmq.GetMatchResultRabbitMq(m.symbol)
		//exchangeName := config.GetString("app.profile", "market") + "." + config.GetString("rabbitmq.exchange.quotation", "l2quote") + "." + m.symbol
		exchangeName := config.GetString("app.profile", "market") + "." + config.GetString("rabbitmq.exchange.quotation", "l2quote")
		ts02 := time.Now().UnixNano() / int64(time.Millisecond)
		rabbitmq.PublishWithChan(rabbitmqCh, exchangeName, depth.ch, content, depth.Ts)
		ts03 := time.Now().UnixNano() / int64(time.Millisecond)

		tsDepthChan := (ts01 - ts00) / int64(time.Millisecond)
		tsGetMq := (ts02 - ts01) / int64(time.Millisecond)
		tsPub := (ts03 - ts02) / int64(time.Millisecond)
		tsAll := (ts03 - ts00) / int64(time.Millisecond)
		tsFromBuild := (ts03 - depth.Ts) / int64(time.Millisecond)

		if tsAll > 20 || (tsFromBuild > 50 && tsFromBuild < 10000) {
			common.Info("DEPTH|MarketThread.thread|timeout|tsAll:", tsAll,
				", tsDepthChan:", tsDepthChan,
				", tsGetMq:", tsGetMq,
				", tsPub:", tsPub,
				", tsFromBuild:", tsFromBuild,
				", key:", depth.ch,
				"depthChan_len:", len(m.depthChan),
				"rabbitmqCh_len:", len(rabbitmqCh),
			)
		}
	}
}

// threadWS WebSocket 模式的深度广播线程（替代 RabbitMQ 发布）
func (m *MarketThread) threadWS(broadcaster DepthBroadcaster) {
	for {
		ts00 := time.Now().UnixNano()
		depth := <-m.depthChan
		batchSize := config.GetInt64("batch_result", 90) - 1
		chanLen := len(m.depthChan)
		sizeToBatch := int(math.Min(float64(batchSize), float64(chanLen)))
		sizeToBatch = int(math.Max(float64(sizeToBatch), 1))
		depthArr := make([]*QuoteDepths, sizeToBatch)
		depthArr[0] = depth
		for i := 1; i < sizeToBatch; i++ {
			_depth := <-m.depthChan
			depthArr[i] = _depth
		}

		ts01 := time.Now().UnixNano()

		for _, dpt := range depthArr {
			broadcaster.BroadcastDepth(m.symbol, dpt)
		}

		ts02 := time.Now().UnixNano()

		tsBatch := (ts01 - ts00) / int64(time.Millisecond)
		tsPub := (ts02 - ts01) / int64(time.Millisecond)
		tsAll := (ts02 - ts00) / int64(time.Millisecond)
		tsFromBuild := (ts02 - depthArr[0].Ts*int64(time.Millisecond))

		if tsAll > 20 || (tsFromBuild > 50 && tsFromBuild < 10000) {
			nowMs := time.Now().UnixMilli()
			last := atomic.LoadInt64(&lastDepthTimeoutLogMs)
			if nowMs-last >= 10000 && atomic.CompareAndSwapInt64(&lastDepthTimeoutLogMs, last, nowMs) {
				common.Info("DEPTH|MarketThread.threadWS|timeout|tsAll:", tsAll,
					", tsBatch:", tsBatch,
					", tsPub:", tsPub,
					", tsFromBuild:", tsFromBuild,
					", key:", depthArr[0].ch,
					"depthChan_len:", len(m.depthChan),
				)
			}
		}
	}
}
