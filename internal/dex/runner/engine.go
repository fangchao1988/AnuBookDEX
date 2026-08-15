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
// 并把撮合结果提交到链上结算。
//
// 链后端由 src（订单源）与 sink（结算目标）注入，撮合核心链无关--
// cmd/engine/{anubis,aleo}/main.go 各自注入对应链后端。
//
// 撮合结果每笔立即提交 sink.SubmitBatch（不攒批）：链上 settle 单次耗时
// 数十秒，早开始早回执，前端结算状态尽快可见。
func StartEngine(src chain.OrderSource, sink chain.SettlementSink, snap *rocksdb.SnapshotStore, hub *ws.Hub) {
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

		// DEX 模式默认不恢复订单簿快照：快照中的挂单与链上状态脱节（链上 Order
		// record 被 settle 一次性消费后，快照挂单已无法结算，且 Order CT 不在
		// 快照里），恢复只会产生幻影订单。需要恢复的场景可配置
		// chain.restore-snapshot: true 开启。
		var book *match.OrderBook
		if config.GetBool("chain.restore-snapshot", false) {
			var err error
			book, err = snap.LoadLatest(symbol)
			if err != nil {
				common.Fatal("load snapshot failed:", err, "symbol:", symbol)
			}
		}
		if book == nil {
			book = match.InitOrderBook(0, symbol)
		}

		orderCh, err := src.Subscribe(symbol)
		if err != nil {
			common.Fatal("subscribe chain events failed:", err, "symbol:", symbol)
		}

		match.OrderBookMap[symbol] = book

		go StartMatcher(book, orderCh,
			func(cloneBook *match.OrderBook) {
				// 与 LoadLatest 对称：chain.restore-snapshot=false（默认）时不落快照
				if !config.GetBool("chain.restore-snapshot", false) {
					return
				}
				if err := snap.Save(book.Symbol, cloneBook); err != nil {
					common.Error("snapshot save error:", err)
				}
			},
			func() {
				mrChan <- []byte("snapshot!")
			},
			func(mrJSON []byte, mr *match.MatchResult) {
				mrChan <- mrJSON
				// 撮合结果立即进入结算：不攒批（settlement-batch-size 需 100 条才触发）
				// 也不等 10s 上报周期 flush——链上 settle 单次 leo execute 耗时数十秒，
				// 早开始早回执，前端结算状态尽快可见。
				sink.SubmitBatch(book.Symbol, []*match.MatchResult{mr})
			},
			func() {
				// 上报周期回调：结算已即时触发，此处无需 flush
			},
		)
	}
}
