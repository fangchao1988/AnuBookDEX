package anubis

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/dex/privacy"
)

// Settlement 链上结算适配器
// 批量提交撮合结果到 Anubis Settlement SC，包含 ZK 证明（0x0100 预编译验证）
type Settlement struct {
	rpcEndpoint    string        // Anubis Chain RPC 节点地址
	contractAddr   string        // Settlement 智能合约地址
	privateKey     string        // 提交交易用的私钥（链上 Gas 支付）
	chainID        *big.Int      // Anubis Chain 链 ID，用于 EIP-155 签名
	batchSize      int           // 批量提交的撮合结果数量阈值
	batchInterval  time.Duration // 定时刷新待处理批次的时间间隔
	ctx            context.Context // 控制 settlement 生命周期的 context
	cancel         context.CancelFunc // 取消 context，触发所有 worker 退出

	mu         sync.Mutex                          // 保护 pending map 的并发安全
	pending    map[string][]*match.MatchResult     // 交易对 → 待提交撮合结果，累积到 batchSize 后触发提交
	submitCh   chan *settlementBatch               // 批次提交 channel，worker 从该 channel 消费
	zkProver   *privacy.ZKProver                   // Phase 2: ZK 证明生成器，为每批撮合结果生成匹配证明
	batchSeq   uint64                              // 批次序号（原子递增），用于唯一标识每批结算
}

type settlementBatch struct {
	symbol   string
	mrs      []*match.MatchResult
	batchID  uint64
}

// NewSettlement 创建链上结算适配器
func NewSettlement(rpcEndpoint, contractAddr, privateKey string, chainID *big.Int) *Settlement {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Settlement{
		rpcEndpoint:   rpcEndpoint,
		contractAddr:  contractAddr,
		privateKey:    privateKey,
		chainID:       chainID,
		batchSize:     config.GetInt("chain.anubis.settlement-batch-size", 100),
		batchInterval: config.GetDuration("chain.anubis.settlement-interval-ms", 500) * time.Millisecond,
		ctx:           ctx,
		cancel:        cancel,
		pending:       make(map[string][]*match.MatchResult),
		submitCh:      make(chan *settlementBatch, 100),
		zkProver:      privacy.NewZKProver(),
	}

	for i := 0; i < config.GetInt("chain.anubis.settlement-workers", 3); i++ {
		go s.submitWorker()
	}
	go s.batchTimer()

	chainIDStr := "unset"
	if s.chainID != nil {
		chainIDStr = s.chainID.String()
	}
	common.Info("chain settlement: initialized rpc=%s chain-id=%s contract=%s batch-size=%d workers=%d zk=%s",
		s.rpcEndpoint, chainIDStr, s.contractAddr, s.batchSize, config.GetInt("chain.anubis.settlement-workers", 3), s.zkProver.CircuitID)
	return s
}

// SubmitBatch 提交单笔撮合结果
// 内部累积到 batch 后批量提交
func (s *Settlement) SubmitBatch(symbol string, mrs []*match.MatchResult) (string, error) {
	s.mu.Lock()
	s.pending[symbol] = append(s.pending[symbol], mrs...)
	size := len(s.pending[symbol])
	s.mu.Unlock()

	// 达到批量大小时立即触发提交
	if size >= s.batchSize {
		s.flushSymbol(symbol)
	}

	return "", nil // async submit, tx hash returned via event
}

// flushSymbol 提交指定交易对的待处理结果
func (s *Settlement) flushSymbol(symbol string) {
	s.mu.Lock()
	mrs := s.pending[symbol]
	if len(mrs) == 0 {
		s.mu.Unlock()
		return
	}
	s.pending[symbol] = nil
	s.mu.Unlock()

	batchID := atomic.AddUint64(&s.batchSeq, 1)
	select {
	case s.submitCh <- &settlementBatch{symbol: symbol, mrs: mrs, batchID: batchID}:
	case <-s.ctx.Done():
	}
}

// batchTimer 定时刷新所有待处理批次
func (s *Settlement) batchTimer() {
	ticker := time.NewTicker(s.batchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			symbols := make([]string, 0, len(s.pending))
			for symbol, mrs := range s.pending {
				if len(mrs) > 0 {
					symbols = append(symbols, symbol)
				}
			}
			s.mu.Unlock()

			for _, symbol := range symbols {
				s.flushSymbol(symbol)
			}
		}
	}
}

// submitWorker 提交工作协程
func (s *Settlement) submitWorker() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case batch := <-s.submitCh:
			txHash, err := s.submitToChain(batch)
			if err != nil {
				common.Error("chain settlement: submit failed", "symbol", batch.symbol,
					"count", len(batch.mrs), "err", err)
				// TODO: 重试逻辑
				continue
			}
			common.Info(fmt.Sprintf("chain settlement: batch submitted symbol=%s count=%d tx=%s",
				batch.symbol, len(batch.mrs), txHash))
		}
	}
}

// submitToChain 向 Settlement SC 提交撮合结果 + ZK 证明
func (s *Settlement) submitToChain(batch *settlementBatch) (string, error) {
	// Phase 2: 生成 ZK 匹配证明
	zkProof, err := s.zkProver.GenerateMatchProof(batch.batchID, batch.mrs)
	if err != nil {
		return "", fmt.Errorf("generate ZK proof: %w", err)
	}

	// 本地预检 ZK 证明
	if ok, _ := s.zkProver.VerifyMatchProof(zkProof); !ok {
		return "", fmt.Errorf("ZK proof self-verification failed for batch %d", batch.batchID)
	}

	// TODO: 接入 Anubis Chain 后实现
	// auth, err := bind.NewKeyedTransactorWithChainID(s.privateKey, s.chainID)
	// settlement, err := NewSettlementContract(common.HexToAddress(s.contractAddr), s.client)
	// tx, err := settlement.SubmitBatch(auth, encodeMatchResults(batch.mrs), zkProof.Proof, zkProof.Inputs)
	// receipt, err := bind.WaitMined(s.ctx, s.client, tx)
	// return tx.Hash().Hex(), nil

	common.Warn("chain settlement: submitToChain is STUB — ZK proof generated but not submitted. symbol:", batch.symbol, "batch:", batch.batchID, "count:", len(batch.mrs))
	return fmt.Sprintf("stub_%d_%s", batch.batchID, batch.symbol), nil
}

// Shutdown 关闭结算适配器
func (s *Settlement) Shutdown() {
	s.cancel()
	// 刷新所有待处理数据
	s.mu.Lock()
	symbols := make([]string, 0, len(s.pending))
	for symbol := range s.pending {
		symbols = append(symbols, symbol)
	}
	s.mu.Unlock()
	for _, symbol := range symbols {
		s.flushSymbol(symbol)
	}
}

