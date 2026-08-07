package runner

import (
	"fmt"

	"github.com/AnuBookDEX/engine/internal/core/l2quote"
	"github.com/AnuBookDEX/engine/internal/core/market"
	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/dex/chain"
	"github.com/AnuBookDEX/engine/internal/dex/rocksdb"
	"github.com/AnuBookDEX/engine/internal/dex/ws"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
)

// StartEngine 启动 DEX 引擎：按交易对初始化行情(l2quote)/快照/订单订阅/撮合循环，
// 并把撮合结果批量提交到链上结算。
//
// 链后端由 src（订单源）与 sink（结算目标）注入，撮合核心链无关--
// cmd/engine/{anubis,aleo}/main.go 各自注入对应链后端。
//
// batchSize 控制撮合结果累积到多少条后触发一次 sink.SubmitBatch，
// 由各链入口按 chain.<chain>.settlement-batch-size 传入。
func StartEngine(src chain.OrderSource, sink chain.SettlementSink, snap *rocksdb.SnapshotStore, hub *ws.Hub, batchSize int) {
	match.Init()

	// 行情线程（WS 模式）
	for id, symbol := range config.GetStringSlice("symbols", []string{}) {
		common.Trace("initializing symbol[", id, "]:", symbol)
		market.MarketThreadInitWS(symbol, hub)
	}

	// 每个交易对独立 goroutine 撮合
	for _, symbol := range config.GetStringSlice("symbols", []string{}) {
		mrChan := make(chan []byte, 5000)

		l2 := l2quote.NewL2quote(symbol, nil, mrChan,
			config.GetString("l2quote.snapshot.path", "./sp/"),
			"",
			config.GetInt64("l2quote.mq-send-interval-ms", 500),
			config.GetInt64("l2quote.kline-forward-limit", 1440),
			config.GetInt("batch_result", 90),
			config.GetInt64("l2quote.snapshot.n-history", 10),
			config.GetInt("l2quote.make-new-kline-at-sec", 2),
		)
		l2.Init()
		// DEX 模式：l2quote 直连 WS Hub（K线/成交/Ticker），替代 RabbitMQ
		l2.SetRawPublisher(hub.BroadcastRaw)
		go l2.Run()
		common.Info(fmt.Sprintf("init %s l2quote complete", symbol))

		book, err := snap.LoadLatest(symbol)
		if err != nil {
			common.Fatal("load snapshot failed:", err, "symbol:", symbol)
		}
		if book == nil {
			book = match.InitOrderBook(0, symbol)
		}

		orderCh, err := src.Subscribe(symbol)
		if err != nil {
			common.Fatal("subscribe chain events failed:", err, "symbol:", symbol)
		}

		match.OrderBookMap[symbol] = book

		// per-symbol 待结算累积
		var pendingSettlements []*match.MatchResult

		go StartMatcher(book, orderCh,
			func(cloneBook *match.OrderBook) {
				if err := snap.Save(book.Symbol, cloneBook); err != nil {
					common.Error("snapshot save error:", err)
				}
			},
			func() {
				mrChan <- []byte("snapshot!")
			},
			func(mrJSON []byte, mr *match.MatchResult) {
				mrChan <- mrJSON
				pendingSettlements = append(pendingSettlements, mr)
				if len(pendingSettlements) >= batchSize {
					sink.SubmitBatch(book.Symbol, pendingSettlements)
					pendingSettlements = nil
				}
			},
			func() {
				if len(pendingSettlements) > 0 {
					sink.SubmitBatch(book.Symbol, pendingSettlements)
					pendingSettlements = nil
				}
			},
		)
	}
}
