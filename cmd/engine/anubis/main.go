package main

import (
	"net/http"
	"time"

	"github.com/AnuBookDEX/engine/internal/dex/chain"
	"github.com/AnuBookDEX/engine/internal/dex/chain/anubis"
	"github.com/AnuBookDEX/engine/internal/dex/rocksdb"
	"github.com/AnuBookDEX/engine/internal/dex/runner"
	"github.com/AnuBookDEX/engine/internal/dex/ws"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"github.com/AnuBookDEX/engine/internal/infra/statistics"

	"github.com/shopspring/decimal"

	_ "time/tzdata" // embed timezone database for FROM scratch Docker images
)

var (
	GitTag    = "2026.7.16-anubis"
	BuildTime = "2026-07-16T00:00:00+0800"
)

func init() {
	decimal.DivisionPrecision = 37
}

func main() {
	if err := common.LoadConfigViper(); err != nil {
		panic("config load error: " + err.Error())
	}

	runner.StartHTTPServer(config.GetInt("http.port", 9000))
	common.ZapInit()
	common.Info("=============== AnuBookDEX engine [anubis] starting ===============")
	defer runner.Recover()

	statistics.Init()

	// 本地存储（替代 redis + snapshotter）
	snapshotStore, err := rocksdb.NewSnapshotStore(config.GetString("rocksdb.data-dir", "./data/snapshot"))
	if err != nil {
		common.Fatal("init snapshot store error:", err)
	}
	defer snapshotStore.Close()

	// WebSocket Hub（替代 rabbitmq）
	wsHub := ws.NewHub()

	// 本地联调模式：chain.anubis.simulate-orders=true 时用模拟订单源替代链后端，
	// 驱动 撮合 -> l2quote -> WS 全链路，供前端开发联调（无需真实链）
	var src chain.OrderSource
	var sink chain.SettlementSink
	if config.GetBool("chain.anubis.simulate-orders", false) {
		common.Info("=============== SIMULATE MODE: simulated order source ===============")
		sim := runner.NewSimOrderSource(time.Duration(config.GetInt("chain.anubis.simulate-interval-ms", 300)) * time.Millisecond)
		defer sim.Shutdown()
		src = sim
		sink = &runner.NoopSettlementSink{}
	} else {
		// Anubis 链后端：订单订阅 + 链上结算（chain.anubis.* 配置）
		adapt := anubis.NewAdapter()
		defer adapt.Settlement().Shutdown()
		defer adapt.Orders().Shutdown()
		src = adapt.Orders()
		sink = adapt.Settlement()
	}

	runner.StartEngine(
		src,
		sink,
		snapshotStore,
		wsHub,
	)

	http.HandleFunc("/ws", wsHub.HandleWebSocket)

	common.WriteStartOk()
	common.Info("AnuBookDEX engine [anubis] started")

	for {
		time.Sleep(100 * time.Second)
	}
}
