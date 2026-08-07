package aleo

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
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
	}
}

// SubmitBatch 对每个撮合结果中每个 maker 成交执行一次 settle transition。
func (s *Settlement) SubmitBatch(symbol string, mrs []*match.MatchResult) (string, error) {
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
			// 按 OrderId 取链上 Order record ciphertext（leo 用 operator view key 解密）
			makerCT, ok := s.pool.Ciphertext(item.OrderId)
			if !ok {
				common.Error("aleo settlement: maker ciphertext not found", "maker", item.OrderId)
				continue
			}
			takerCT, ok := s.pool.Ciphertext(takerID)
			if !ok {
				common.Error("aleo settlement: taker ciphertext not found", "taker", takerID)
				continue
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
			if err := s.executeSettle(makerCT, takerCT, price, amount); err != nil {
				common.Error("aleo settlement: settle failed",
					"maker", item.OrderId, "taker", takerID, "err", err)
				continue
			}
			settled++
		}
	}
	txID := fmt.Sprintf("aleo_batch_%d_%s_%dsettle", batchID, symbol, settled)
	common.Info("aleo settlement: batch done",
		"symbol", symbol, "mrs", len(mrs), "settles", settled, "tx", txID)
	return txID, nil
}

// executeSettle 调用 leo execute settle <maker_ct> <taker_ct> <price>u64 <amount>u64 --broadcast
func (s *Settlement) executeSettle(makerCT, takerCT string, price, amount uint64) error {
	args := []string{
		"execute", "settle",
		makerCT,
		takerCT,
		strconv.FormatUint(price, 10) + "u64",
		strconv.FormatUint(amount, 10) + "u64",
		"--broadcast",
		"--endpoint", s.rpcEndpoint,
		"--network", s.network,
		"--yes",
	}
	if s.privateKey != "" {
		args = append(args, "--private-key", s.privateKey)
	}
	cmd := exec.Command("leo", args...)
	cmd.Dir = s.workingDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("leo execute settle: %w; out: %s", err, strings.TrimSpace(string(out)))
	}
	common.Debug("aleo settlement: leo execute settle ok",
		"price", price, "amount", amount,
		"out", strings.TrimSpace(string(out)))
	return nil
}

// Shutdown 关闭结算适配器。
func (s *Settlement) Shutdown() {
	common.Info("aleo settlement: shutdown")
}
