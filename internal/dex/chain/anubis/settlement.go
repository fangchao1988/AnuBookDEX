package anubis

// settlement.go: Anubis Chain 上链结算适配器（SettlementSink 接口）。
// 批量提交撮合结果到 Settlement v2 SC；Phase 2 ZK proof 先用哈希承诺占位（requireProof=false），
// 待 Anubis 官方公开 PLONK 电路参数后再上 0x0100 验证。

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/dex/privacy"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"github.com/shopspring/decimal"
)

// Settlement 链上结算适配器：批量提交撮合结果到 Anubis Settlement v2 SC。
type Settlement struct {
	rpcEndpoint    string        // Anubis Chain RPC（HTTP）
	contractAddr   string        // Settlement v2 合约地址
	chainID        *big.Int      // Anubis Chain 链 ID
	batchSize      int           // 批量提交阈值
	batchInterval  time.Duration // 定时刷新间隔
	requireProof   bool          // 是否要求 0x0100 PLONK 验证（v2 默认 false）
	ctx            context.Context
	cancel         context.CancelFunc

	mu       sync.Mutex
	pending  map[string][]*match.MatchResult // symbol → 待提交
	submitCh chan *settlementBatch
	batchSeq uint64 // 原子递增批次号

	evm      *EVMClient         // 交易签发/广播/回执（go-ethereum）
	pool     *OrderPool         // 订单池（结算回执触发 MarkTradesSettled）
	zkProver *privacy.ZKProver  // Phase 2 proof 生成器（当前哈希占位）
}

type settlementBatch struct {
	symbol  string
	mrs     []*match.MatchResult
	batchID uint64
}

// NewSettlement 创建链上结算适配器。
// evm client 由 NewAdapter 按配置构造并 SetClient 注入；evm=nil 时仅本地 dry-run。
func NewSettlement(rpcEndpoint, contractAddr, privateKey string, chainID *big.Int, evm *EVMClient) *Settlement {
	return newSettlement(rpcEndpoint, contractAddr, privateKey, chainID, evm, true)
}

// NewSettlementNoKey：无 operator 私钥模式（dry-run/测试），evm=nil。
func NewSettlementNoKey(rpcEndpoint, contractAddr string, chainID *big.Int) *Settlement {
	return newSettlement(rpcEndpoint, contractAddr, "", chainID, nil, false)
}

func newSettlement(rpcEndpoint, contractAddr, privateKey string, chainID *big.Int, evm *EVMClient, withKey bool) *Settlement {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Settlement{
		rpcEndpoint:   rpcEndpoint,
		contractAddr:  contractAddr,
		chainID:       chainID,
		batchSize:     config.GetInt("chain.anubis.settlement-batch-size", 100),
		batchInterval: config.GetDuration("chain.anubis.settlement-interval-ms", 500) * time.Millisecond,
		requireProof:  config.GetBool("chain.anubis.settlement-gate", false),
		evm:           evm,
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
	common.Info("chain settlement: initialized rpc=%s chain-id=%s contract=%s batch-size=%d workers=%d requireProof=%v zk=%s evm=%v",
		s.rpcEndpoint, chainIDStr, s.contractAddr, s.batchSize,
		config.GetInt("chain.anubis.settlement-workers", 3), s.requireProof, s.zkProver.CircuitID, s.evm != nil)
	return s
}

// SetClient 注入 evm client（由 NewAdapter 在配置就绪后调用）。
func (s *Settlement) SetClient(c *EVMClient) { s.evm = c }

// SubmitBatch 提交单笔撮合结果，内部累积到 batch 后批量提交。
func (s *Settlement) SubmitBatch(symbol string, mrs []*match.MatchResult) (string, error) {
	s.mu.Lock()
	s.pending[symbol] = append(s.pending[symbol], mrs...)
	size := len(s.pending[symbol])
	s.mu.Unlock()
	if size >= s.batchSize {
		s.flushSymbol(symbol)
	}
	return "", nil // async；tx hash 通过事件/日志返回
}

// flushSymbol 提交指定交易对的待处理结果。
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
	// 记录该批次的成交到订单池（settle 状态 = queued）
	if s.pool != nil && len(mrs) > 0 {
		s.pool.AppendTrades(mrs)
	}
	select {
	case s.submitCh <- &settlementBatch{symbol: symbol, mrs: mrs, batchID: batchID}:
	case <-s.ctx.Done():
	}
}

// batchTimer 定时刷新所有待处理批次。
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

// submitWorker 提交工作协程，消费 submitCh。
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
				// TODO: 重入队重试（当前 worker loop 直接丢弃；后续加退避重排）
				continue
			}
			common.Info("chain settlement: batch submitted", "symbol", batch.symbol,
				"count", len(batch.mrs), "tx", txHash)
		}
	}
}

// submitToChain 将撮合结果打包为 Settlement v2 submitBatch tx 并上链。
func (s *Settlement) submitToChain(batch *settlementBatch) (string, error) {
	if s.evm == nil {
		// 无私钥模式：仅本地生成哈希承诺 + 日志（dry-run/模拟）
		h := sha256.Sum256([]byte(fmt.Sprintf("dryrun:%d:%s", batch.batchID, batch.symbol)))
		common.Warn("chain settlement: evm client nil — dry run only. symbol:", batch.symbol,
			"batch:", batch.batchID, "commit:", commonHex(h[:]))
		return fmt.Sprintf("dryrun_%d_%s", batch.batchID, batch.symbol), nil
	}

	// 1. 构造 MatchResult[] → SettlementMatchV3[]
	results, err := encodeMatchResults(batch.mrs, s.infoFor)
	if err != nil {
		return "", fmt.Errorf("encode match results: %w", err)
	}
	// 编解码可能过滤掉全部条目（如「挂单到达但未成交」的批次）——
	// 此时不可下发空 batch：合约会 revert "Empty batch"，白耗 gas 且污染重试循环。
	if len(results) == 0 {
		common.Debug("chain settlement: batch has no settled fills, skip tx",
			"symbol", batch.symbol, "batch", batch.batchID, "mrs", len(batch.mrs))
		return "", nil
	}

	// 2. ZK proof：MVP 哈希承诺；requireProof=true 时走 0x0100，待参数公开
	var zkProof []byte
	if s.requireProof {
		proof, err := s.zkProver.GenerateMatchProof(batch.batchID, batch.mrs)
		if err != nil {
			return "", fmt.Errorf("zk proof generation: %w", err)
		}
		ok, _ := s.zkProver.VerifyMatchProof(proof)
		if !ok {
			return "", fmt.Errorf("zk proof self-verification failed")
		}
		zkProof = proof.Proof
	} else {
		// v2 hash 模式：zkProof 可为空；合约仅校验 newRoot 一致性
		zkProof = []byte("anubis-hash-mvp")
	}

	// 3. newRoot = keccak256(abi.encode(results) || keccak256(..., prevRoot)) —— 镜像 v2 solidity
	prevRoot, err := s.stateRootFetch(s.ctx)
	if err != nil {
		common.Warn("chain settlement: stateRoot fetch failed, using ZeroHash", "err", err)
	}
	newRoot := commitmentRoot(results, prevRoot)

	// 4. ABI-pack + 发送
	data, err := encodeSubmitBatch(results, zkProof, newRoot)
	if err != nil {
		return "", err
	}
	to := ethcommon.HexToAddress(s.contractAddr)
	receipt, tx, err := s.evm.SendTransaction(s.ctx, to, data, nil)
	if err != nil {
		return "", fmt.Errorf("submitBatch tx: %w", err)
	}
	if receipt == nil {
		return tx.Hash().Hex(), nil
	}

	// 5. 解析 BatchSettled 事件
	bse := parseBatchSettled(receipt)
	if bse == nil {
		common.Warn("chain settlement: no BatchSettled log in receipt", "tx", tx.Hash().Hex(),
			"status", receipt.Status, "logs", len(receipt.Logs))
	} else {
		common.Info("chain settlement: BatchSettled", "batchId", bse.BatchID,
			"stateRoot", commonHex(bse.StateRoot[:]), "vol", bse.TotalVolume, "fee", bse.FeeCollected)
		// 结算已确认：更新成交流水状态（/api/v1/trades 的 settle_status）
		if receipt.Status == 1 && s.pool != nil {
			s.pool.MarkTradesSettled(batch.symbol, tx.Hash().Hex())
		}
	}
	return tx.Hash().Hex(), nil
}

// SetPool 绑定订单池（结算回执触发 MarkTradesSettled）。
func (s *Settlement) SetPool(pool *OrderPool) { s.pool = pool }

// VaultBalance 查用户在某代币上的结算金库余额。
// 返回 (balance, ok)；未配置结算合约时 ok=false（跳过校验）。
func (s *Settlement) VaultBalance(token ethcommon.Address, user string) (*big.Int, bool) {
	if s.evm == nil || s.contractAddr == "" {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := encodeVaultBalance(token, ethcommon.HexToAddress(user))
	if err != nil {
		return nil, false
	}
	res, err := s.evm.CallContract(ctx, ethcommon.HexToAddress(s.contractAddr), data)
	if err != nil || len(res) < 32 {
		return nil, false
	}
	return new(big.Int).SetBytes(res[:32]), true
}

// infoFor 按 order id 查订单池中的收款地址与买卖方向（撮合核心不填这两项）。
func (s *Settlement) infoFor(orderID int64) (orderInfo, bool) {
	if s.pool == nil {
		return orderInfo{}, false
	}
	rec := s.pool.Record(orderID)
	if rec == nil {
		return orderInfo{}, false
	}
	return orderInfo{Trader: rec.Trader, Side: rec.Side}, true
}

// orderInfo 结算所需的订单侧信息（收款地址 + 买卖方向）。
type orderInfo struct {
	Trader string
	Side   int // 0=buy, 1=sell
}

// pairTokensFor 从配置读取币对的 base/quote 代币地址：
// chain.anubis.pair-tokens.<SYMBOL>.base / .quote
// 未配置时返回 ok=false（该币对跳过结算，仅记账不上链）。
// 声明为变量以便测试注入（测试环境不加载 config.yaml）。
var pairTokensFor = func(symbol string) (ethcommon.Address, ethcommon.Address, bool) {
	b := config.GetString("chain.anubis.pair-tokens."+symbol+".base", "")
	q := config.GetString("chain.anubis.pair-tokens."+symbol+".quote", "")
	if b == "" || q == "" {
		return ethcommon.Address{}, ethcommon.Address{}, false
	}
	return ethcommon.HexToAddress(b), ethcommon.HexToAddress(q), true
}

// encodeMatchResults 把撮合结果映射为 Settlement v3 的 MatchResult[]。
// 每笔成交产生两条结果（买卖双方各一条），合约据此在金库内划转：
//   买方(side=0)：付 quote(cost)，收 base(amount)
//   卖方(side=1)：付 base(amount)，收 quote(cost - fee)
//
// 精度约定：当前测试代币为 0 位小数，price/amount 即代币最小单位，
// cost = price * amount 无需换算。接入真实精度代币时在此处按 decimals 缩放。
func encodeMatchResults(mrs []*match.MatchResult, resolver func(orderID int64) (orderInfo, bool)) ([]SettlementMatchV3, error) {
	out := make([]SettlementMatchV3, 0, totalItems(mrs))
	for _, mr := range mrs {
		baseTok, quoteTok, ok := pairTokensFor(mr.Symbol)
		if !ok {
			common.Warn("chain settlement: pair tokens not configured — 该币对跳过结算（不会上链）",
				"symbol", mr.Symbol)
			continue
		}
		for _, item := range mr.Items {
			// 只编码「真实成交」的条目：挂单到达但无对手方时，撮合器也会产出一条
			// state=submitted 的 item，且其 AccFilledAmount 是该单总量（非成交量）。
			// 若不过滤，未成交单会被当成成交下发，导致同一地址被重复扣款而结算回滚。
			if item.State != "filled" && item.State != "partial-filled" {
				continue
			}
			role := item.Role
			if role != "maker" && role != "taker" {
				role = "taker"
			}

			// 收款地址 + 买卖方向：优订单池（链下订单），回落连接器级 UserAddress
			var info orderInfo
			if resolver != nil {
				if got, ok := resolver(item.OrderId); ok {
					info = got
				}
			}
			addr := info.Trader
			if addr == "" {
				addr = mr.UserAddress
			}
			if addr == "" {
				common.Warn("chain settlement: no user address for order", "order", item.OrderId, "mr", mr.OrderId)
				continue // 无地址无法结算，跳过该条（否则会转给零地址）
			}
			user := ethcommon.HexToAddress(addr)

			// 价格/数量：item 级指针可能为空（maker 无 price、taker 无 filled-amount），回落结果级
			price := mr.Price
			if item.Price != nil {
				price = *item.Price
			}
			filled := decimal.Zero
			if item.FilledAmount != nil {
				filled = *item.FilledAmount
			} else if item.AccFilledAmount != nil {
				filled = *item.AccFilledAmount
			}
			if filled.Sign() <= 0 {
				continue // 无成交量的条目不下发（零额会被合约拒绝）
			}

			priceBig := price.BigInt()
			amountBig := filled.BigInt()
			costBig := new(big.Int).Mul(priceBig, amountBig)

			out = append(out, SettlementMatchV3{
				ID:         uint64(mr.Id),
				OrderId:    commitmentHashFromID(mr.OrderId, item.OrderId),
				User:       user,
				BaseToken:  baseTok,
				QuoteToken: quoteTok,
				Price:      priceBig,
				Amount:     amountBig,
				Cost:       costBig,
				Side:       uint8(info.Side),
				Nullifier:  commitmentHashFromID(mr.OrderId, item.OrderId),
				Role:       role,
			})
		}
	}
	return out, nil
}

func totalItems(mrs []*match.MatchResult) int {
	n := 0
	for _, mr := range mrs {
		n += len(mr.Items)
	}
	return n
}

// --- 哈希承诺 / 状态根（MVP；v2 requireProof=false 校验 newRoot 一致性） ---
// Settlement v2 solidity hash 模式校验：
//   resultsHash = keccak256(abi.encode(results))
//   newRoot     = keccak256(abi.encodePacked(resultsHash, currentStateRoot))
// Go 端必须用相同的 abi.encode 打包 + crypto.Keccak256 才与链上一致。

// commitmentRoot 计算 v2 提交的 newRoot。
func commitmentRoot(results []SettlementMatchV3, prevRoot ethcommon.Hash) ethcommon.Hash {
	resultsHash := keccakABIEncode(results)
	return crypto.Keccak256Hash(resultsHash.Bytes(), prevRoot.Bytes())
}

// keccakABIEncode = keccak256(abi.encode(results)) — 使用 go-ethereum abi pack。
// abi.encode(results) 对 tuple[] 做 head/tail 打包；go-ethereum 用
// abi.Arguments（仅 results，无 selector）的 Pack 获得完整 encoding。
// commitmentHashFromID 用 order-id 对生成 32B 锚定哈希（MVP nullifier/orderId 的退化）。
func commitmentHashFromID(orderID, itemOrderID int64) ethcommon.Hash {
	return crypto.Keccak256Hash([]byte(fmt.Sprintf("anubis-order:%d:%d", orderID, itemOrderID)))
}

func keccakABIEncode(results []SettlementMatchV3) ethcommon.Hash {
	method := settleContractAB.Methods["submitBatch"]
	if len(method.Inputs) == 0 {
		common.Warn("keccakABIEncode: submitBatch inputs not found")
		return ethcommon.Hash{}
	}
	// abi.encode(results) := 仅 results 参数（无 selector）的完整打包
	packed, err := abi.Arguments{method.Inputs[0]}.Pack(results)
	if err != nil {
		common.Warn("keccakABIEncode pack failed", "err", err)
		return ethcommon.Hash{}
	}
	return crypto.Keccak256Hash(packed)
}

// stateRootFetch 从链上查询当前 stateRoot（currentStateRoot() view）。
func (s *Settlement) stateRootFetch(ctx context.Context) (ethcommon.Hash, error) {
	var root ethcommon.Hash
	if s.evm == nil {
		return root, nil
	}
	to := ethcommon.HexToAddress(s.contractAddr)
	data := settleContractAB.Methods["currentStateRoot"].ID
	res, err := s.evm.Client.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, nil)
	if err != nil {
		return root, fmt.Errorf("currentStateRoot call: %w", err)
	}
	if len(res) != 32 {
		return root, fmt.Errorf("currentStateRoot ret len=%d", len(res))
	}
	copy(root[:], res)
	return root, nil
}

func commonHex(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return fmt.Sprintf("0x%x", b)
}

// Shutdown 刷新待处理并退出（保持原签名）。
func (s *Settlement) Shutdown() {
	s.cancel()
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
