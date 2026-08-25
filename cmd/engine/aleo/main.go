package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/dex/ai"
	"github.com/AnuBookDEX/engine/internal/dex/chain/aleo"
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
	GitTag    = "2026.7.16-aleo"
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
	common.Info("=============== AnuBookDEX engine [aleo] starting ===============")
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

	// Aleo 链后端：snarkOS REST 订阅 + settle transition 结算（chain.aleo.* 配置）
	// 骨架阶段：Subscribe/SubmitBatch 为 TODO 占位，引擎可启动并服务 /health。
	adapt := aleo.NewAdapter()
	defer adapt.Settlement().Shutdown()
	defer adapt.Orders().Shutdown()
	// 后台重结算：explorer statePaths 网络抖动导致的 settle 失败每 30s 自动重试
	if st, ok := adapt.Settlement().(interface{ StartRetryLoop() }); ok {
		st.StartRetryLoop()
	}

	runner.StartEngine(
		adapt.Orders(),
		adapt.Settlement(),
		snapshotStore,
		wsHub,
	)

	http.HandleFunc("/ws", wsHub.HandleWebSocket)
	http.HandleFunc("/api/v1/market/trades", wsHub.HandleMarketTrades) // 最近成交历史（刷新回放）

	// Phase 2b 链下订单通道：POST /order（tx_id 提取/明文两种模式，标准/隐私下单共用）
	var pool *aleo.OrderPool
	if pp, ok := adapt.(interface{ Pool() *aleo.OrderPool }); ok {
		pool = pp.Pool()
		aleoRPC := aleo.NewRESTClient(config.GetString("chain.aleo.rpc-endpoint", "https://api.explorer.provable.com/v1"))
		http.HandleFunc("/order", aleo.HandleOrder(pool, aleoRPC))
		http.HandleFunc("/order/privacy", aleo.HandlePrivacyOrder(pool, aleoRPC)) // 隐私下单（链上解密）
		http.HandleFunc("/order/cancel", aleo.HandleOrderCancel(pool, adapt.Settlement().(*aleo.Settlement))) // 链上撤单（p6 四变体路由）
		http.HandleFunc("/api/v1/orders", aleo.HandleOrders(pool))                // P3 委托列表
		http.HandleFunc("/api/v1/trades", aleo.HandleTrades(pool))                // P3 成交记录
		http.HandleFunc("/api/v1/operator", aleo.HandleOperator()) // P3 operator 地址（place_order 接收方）
		// P3 交易查询代理：钱包广播 place_order 后用 tx_id 换 Order record ciphertext
		http.HandleFunc("/order/tx/", aleo.HandleOrderTx(aleoRPC, config.GetString("chain.aleo.program-id", "anubook_dex_p2.aleo")))
		http.HandleFunc("/api/v1/balance/", aleo.HandleBalance(aleoRPC)) // P3 链上 ALEO 公开余额
		common.Info("aleo engine: /order + /order/privacy + /api/v1/{orders,trades,operator,balance} + /order/tx/ enabled")
	}

	// Phase 3: AI 策略引擎（链无关，双链共享）——行情研判 + 冰山拆分
	aiHub := ai.NewAIHub(func(symbol string, side int, price, amount decimal.Decimal, parentID string) error {
		if pool == nil {
			return fmt.Errorf("order pool not available")
		}
		bs := match.Buy
		if side == 1 {
			bs = match.Sell
		}
		oid := time.Now().UnixNano() / 1e6 // 子单 order id
		o := &match.Order{
			SeqId:          oid,
			OrderId:        oid,
			BuyOrSell:      bs,
			Type:           match.Limit,
			State:          match.Submitted,
			Price:          price,
			UnfilledAmount: amount,
			CreateAt:       time.Now().UnixMilli(),
		}
		// AI 冰山子单无链上 Order record（策略单）：require-ciphertext=false 时走链下撮合
		return pool.Submit(&aleo.PooledOrder{Order: o, Ciphertext: "ai-iceberg:" + parentID})
	})
	aiHub.Start(config.GetStringSlice("symbols", []string{}), 30*time.Second)
	defer aiHub.Shutdown()
	http.Handle("/ai/", ai.HandleAI(aiHub))
	common.Info("aleo engine: AI hub enabled (/ai/signal /ai/indicators /ai/sentiment /ai/iceberg)")

	common.WriteStartOk()
	common.Info("AnuBookDEX engine [aleo] started (Phase 3: order pool + record settlement + AI)")

	for {
		time.Sleep(100 * time.Second)
	}
}
