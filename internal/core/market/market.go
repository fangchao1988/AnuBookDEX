package market

import (
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"

	"github.com/shopspring/decimal"
)

// DepthBroadcaster 深度行情广播接口（避免 import cycle）
type DepthBroadcaster interface {
	BroadcastDepth(symbol string, depth *QuoteDepths)
}

var (
	registration map[string]*MarketThread    //不同的交易对一个Thread, Thread是刚开始的设计，目前看后面可以精简掉
	depthChan    map[string]chan *QuoteDepths //不同的交易所与不同的交易对各有一个chan
	wsBroadcaster DepthBroadcaster           // WebSocket 广播器（可选）
)

func init() {
	decimal.MarshalJSONWithoutQuotes = true
	registration = make(map[string]*MarketThread)
	depthChan = make(map[string]chan *QuoteDepths)
}

func MarketThreadInit(exchange, symbol string) {

	//后边加上配置可控buffer大小
	ch := make(chan *QuoteDepths, config.GetInt64("exchange.depth.size", 1000))
	m := &MarketThread{
		symbol:    symbol,
		exchange:  exchange,
		depthChan: ch,
	}

	registration[symbol] = m

	common.Trace("MarketThreadInit symbol", symbol)
	depthChan[symbol] = ch

	go m.thread()
}

// MarketThreadInitWS 初始化行情线程（WebSocket 模式，替代 RabbitMQ）
func MarketThreadInitWS(symbol string, broadcaster DepthBroadcaster) {
	wsBroadcaster = broadcaster
	ch := make(chan *QuoteDepths, config.GetInt64("exchange.depth.size", 1000))
	m := &MarketThread{
		symbol:    symbol,
		exchange:  "dex",
		depthChan: ch,
	}

	registration[symbol] = m
	depthChan[symbol] = ch

	common.Trace("MarketThreadInitWS symbol", symbol)

	go m.threadWS(broadcaster)
}
