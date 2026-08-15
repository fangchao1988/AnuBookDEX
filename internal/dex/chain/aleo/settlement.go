package aleo

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"github.com/shopspring/decimal"
)

// Settlement Aleo 结算（Phase 2b record 模型）：对每个 maker 成交 shell out
// `leo execute settle <maker_ct> <taker_ct> <price>u64 <amount>u64 --broadcast ...`。
//
// 订单输入为链上 Order record 的 ciphertext——leo CLI 用 operator 的 view key 自动解密并 spend，
// Go 侧不需要实现 record 解密。maker=买单、taker=卖单（settle 约定）。
type Settlement struct {
	rpcEndpoint string
	programID   string
	privateKey  string
	network     string // leo execute --network（testnet/mainnet）
	workingDir  string // leo 项目目录（含 program.json + src/main.leo）
	batchSize   int
	pool        *OrderPool // 订单池：按 OrderId 查 record ciphertext
	seq         uint64
	stopCh      chan struct{} // 重结算循环停止信号
}

// NewSettlement 创建 Aleo 结算适配器。
func NewSettlement(rpcEndpoint, programID, privateKey string, pool *OrderPool) *Settlement {
	return &Settlement{
		rpcEndpoint: rpcEndpoint,
		programID:   programID,
		privateKey:  privateKey,
		network:     config.GetString("chain.aleo.network", "testnet"),
		workingDir:  config.GetString("chain.aleo.program-dir", "./contracts/leo/"),
		batchSize:   config.GetInt("chain.aleo.settlement-batch-size", 100),
		pool:        pool,
		stopCh:      make(chan struct{}),
	}
}

// SubmitBatch 对每个撮合结果中每个 maker 成交执行一次 settle transition。
func (s *Settlement) SubmitBatch(symbol string, mrs []*match.MatchResult) (string, error) {
	common.Info("aleo settlement: SubmitBatch enter", "symbol", symbol, "mrs", len(mrs),
		"enabled", config.GetBool("chain.aleo.settlement-enabled", true))
	// P3：撮合回执更新订单状态（委托列表数据源）
	s.pool.RecordMatch(mrs)

	// 本地联调开关：chain.aleo.settlement-enabled=false 时跳过链上 settle
	// （占位 ciphertext 无法解密，leo execute 必然失败；生产必须开启）
	if !config.GetBool("chain.aleo.settlement-enabled", true) {
		return "", nil
	}

	batchID := atomic.AddUint64(&s.seq, 1)
	settled := 0
	for _, mr := range mrs {
		takerID := mr.OrderId
		for _, item := range mr.Items {
			if item.Role != "maker" || item.OrderId == 0 {
				continue
			}
			// 取买卖双方订单（含 Order record + operator 托管资产 + 合规凭证）
			makerPO, ok := s.pool.GetOrder(item.OrderId)
			if !ok {
				common.Error("aleo settlement: maker order not found", "maker", item.OrderId)
				continue
			}
			takerPO, ok := s.pool.GetOrder(takerID)
			if !ok {
				common.Error("aleo settlement: taker order not found", "taker", takerID)
				continue
			}
			// settle 约定 maker=买、taker=卖；撮合结果中买卖方向以订单为准
			buyPO, sellPO := makerPO, takerPO
			if makerPO.Order.BuyOrSell == match.Sell {
				buyPO, sellPO = takerPO, makerPO
			}
			price := uint64(0)
			if item.Price != nil {
				price = uint64(item.Price.IntPart())
			} else {
				price = uint64(mr.Price.IntPart())
			}
			amount := uint64(0)
			if item.FilledAmount != nil {
				amount = uint64(item.FilledAmount.IntPart())
			}
			if err := s.executeSettle(buyPO.Ciphertext, sellPO.Ciphertext, sellPO.OpFund, buyPO.OpFund, buyPO.Creds, price, amount); err != nil {
				common.Error("aleo settlement: settle failed",
					"maker", item.OrderId, "taker", takerID, "err", err)
				// 结算状态回执（前端展示）
				s.pool.UpdateTradeSettleStatus(item.OrderId, SettleFailed)
				continue
			}
			// 结算成功回执（前端展示）
			s.pool.UpdateTradeSettleStatus(item.OrderId, SettleSettled)
			settled++
		}
	}
	txID := fmt.Sprintf("aleo_batch_%d_%s_%dsettle", batchID, symbol, settled)
	common.Info("aleo settlement: batch done",
		"symbol", symbol, "mrs", len(mrs), "settles", settled, "tx", txID)
	return txID, nil
}

// executeSettle 调用 leo execute settle <maker_order_ct> <taker_order_ct> <op_aloe> <op_usdcx> <op_creds> <price>u64 <amount>u64 --broadcast
// 注意：私钥必须显式传参（--private-key）——leo 读 .env 私钥会触发 InvalidCharacter bug；
// 且 .env 的 PRIVATE_KEY 引号问题会导致解析失败，显式传参已实测可用
//
// 内置 3 次重试：leo 构建证明前需从 explorer API 拉 statePaths，该端点偶发网络
// 失败（"Failed to fetch from .../statePaths"），此时交易尚未广播、record 未消费，
// 重试安全。间隔 2s/4s。
func (s *Settlement) executeSettle(makerOrderCT, takerOrderCT, opAloe, opUsdcx, creds string, price, amount uint64) error {
	// 私钥兜底：config chain.aleo.private-key -> ALEO_PRIVATE_KEY（viper BindEnv 对嵌套 key 不可靠）
	priv := s.privateKey
	if priv == "" {
		priv = os.Getenv("ALEO_PRIVATE_KEY")
	}
	args := []string{
		"execute", "settle",
		makerOrderCT,
		takerOrderCT,
		opAloe,
		opUsdcx,
		creds,
		strconv.FormatUint(price, 10) + "u64",
		strconv.FormatUint(amount, 10) + "u64",
		"--broadcast",
		"--endpoint", s.rpcEndpoint,
		"--network", s.network,
		"--yes",
		// 用链上版本程序构建证明（本地合约与链上字节码不同会导致广播被节点拒绝）
		"--no-local",
	}
	if priv != "" {
		args = append(args, "--private-key", priv)
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		cmd := exec.Command("leo", args...)
		cmd.Dir = s.workingDir
		out, err := cmd.CombinedOutput()
		if err == nil {
			common.Debug("aleo settlement: leo execute settle ok",
				"price", price, "amount", amount,
				"out", strings.TrimSpace(string(out)))
			return nil
		}
		outStr := strings.TrimSpace(string(out))
		// 幂等成功：广播已上链但 leo CLI 因状态查询失败返回非零码，重试时节点报
		// "input ID ... already exists in the ledger"——说明该笔结算已成功，视为 settled
		if strings.Contains(outStr, "already exists") {
			common.Info("aleo settlement: settle already on-chain (idempotent)",
				"price", price, "amount", amount)
			return nil
		}
		lastErr = fmt.Errorf("leo execute settle: %w; out: %s", err, outStr)
		if attempt < 2 {
			common.Warn("aleo settlement: leo execute failed, will retry",
				"attempt", attempt+1, "price", price, "amount", amount)
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
		}
	}
	return lastErr
}

// StartRetryLoop 启动后台重结算循环：定期扫描结算失败的成交重新执行 settle。
// 失败的结算交易未广播、record 未消费，重试安全；成功后前端结算状态回执为 settled。
func (s *Settlement) StartRetryLoop() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.retryFailedSettlements()
			}
		}
	}()
}

// retryFailedSettlements 扫描失败成交并重试链上结算（买卖侧输入与 SubmitBatch 同规则）。
func (s *Settlement) retryFailedSettlements() {
	for _, t := range s.pool.ListFailedTrades() {
		makerPO, ok1 := s.pool.GetOrder(t.OrderId)
		takerPO, ok2 := s.pool.GetOrder(t.TakerOrderId)
		if !ok1 || !ok2 {
			common.Warn("aleo settlement: retry skip, order missing", "maker", t.OrderId, "taker", t.TakerOrderId)
			continue
		}
		buyPO, sellPO := makerPO, takerPO
		if makerPO.Order.BuyOrSell == match.Sell {
			buyPO, sellPO = takerPO, makerPO
		}
		price, err := decimal.NewFromString(t.Price)
		if err != nil || price.Sign() <= 0 {
			common.Error("aleo settlement: retry skip, bad price", "trade", t.OrderId, "price", t.Price)
			continue
		}
		amount, err := decimal.NewFromString(t.Amount)
		if err != nil || amount.Sign() <= 0 {
			common.Error("aleo settlement: retry skip, bad amount", "trade", t.OrderId, "amount", t.Amount)
			continue
		}
		if err := s.executeSettle(buyPO.Ciphertext, sellPO.Ciphertext, sellPO.OpFund, buyPO.OpFund, buyPO.Creds,
			uint64(price.IntPart()), uint64(amount.IntPart())); err != nil {
			common.Error("aleo settlement: retry settle failed", "maker", t.OrderId, "taker", t.TakerOrderId, "err", err)
			continue
		}
		s.pool.UpdateTradeSettleStatus(t.OrderId, SettleSettled)
		common.Info("aleo settlement: retry settled ok", "maker", t.OrderId, "taker", t.TakerOrderId)
	}
}

// Shutdown 关闭结算适配器（停止重结算循环）。
func (s *Settlement) Shutdown() {
	close(s.stopCh)
	common.Info("aleo settlement: shutdown")
}
