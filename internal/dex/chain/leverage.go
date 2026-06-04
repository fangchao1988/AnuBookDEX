package chain

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/AnuBookDEX/engine/internal/dex/ai"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"github.com/AnuBookDEX/engine/internal/dex/privacy"

	"github.com/shopspring/decimal"
)

// LeverageAdapter 杠杆交易链上适配器
// 连接 ai.RiskEngine 与 Anubis LeverageManager SC
type LeverageAdapter struct {
	contractAddr string
	rpcEndpoint  string
	privateKey   string
	chainID      *big.Int

	riskEngine *ai.RiskEngine

	mu               sync.RWMutex
	fundingRates     map[string]decimal.Decimal // symbol → funding rate
	lastFundingTime  map[string]time.Time
	marginCalls      map[string][]*MarginCall
	liquidationQueue []*LiquidationOrder
	liquidateCh      chan *LiquidationOrder

	// 加密保证金
	encryptor *privacy.EncryptedOrder // 复用 Phase 2 加密基础设施
}

// MarginCall 追加保证金通知
type MarginCall struct {
	Account       string          `json:"account"`
	Symbol        string          `json:"symbol"`
	RequiredAmt   decimal.Decimal `json:"required_amount"`
	CurrentMargin decimal.Decimal `json:"current_margin"`
	MinMargin     decimal.Decimal `json:"min_margin"`
	Deadline      int64           `json:"deadline"`       // 追加保证金截止区块
	IssuedAt      time.Time       `json:"issued_at"`
}

// LiquidationOrder 强平订单
type LiquidationOrder struct {
	Account       string          `json:"account"`
	Symbol        string          `json:"symbol"`
	Size          decimal.Decimal `json:"size"`
	Side          int             `json:"side"`
	Price         decimal.Decimal `json:"price"`
	LiquidationPrc decimal.Decimal `json:"liquidation_price"`
	Loss          decimal.Decimal `json:"loss"`
	Timestamp     time.Time       `json:"timestamp"`
	TxHash        string          `json:"tx_hash"`
}

// NewLeverageAdapter 创建杠杆适配器
func NewLeverageAdapter(rpcEndpoint, contractAddr, privateKey string, chainID *big.Int) *LeverageAdapter {
	riskCfg := &ai.RiskConfig{
		MaxLeverage:        10,
		MaintenanceMargin:  decimal.NewFromFloat(0.005),
		LiquidationPenalty: decimal.NewFromFloat(0.025),
		MaxPositionSize:    decimal.NewFromFloat(100000),
		AutoReducePct:     decimal.NewFromFloat(0.5),
		RiskCheckInterval: time.Second * 5,
	}
	engine := ai.NewRiskEngine(riskCfg)

	la := &LeverageAdapter{
		contractAddr:     contractAddr,
		rpcEndpoint:      rpcEndpoint,
		privateKey:       privateKey,
		chainID:          chainID,
		riskEngine:       engine,
		fundingRates:     make(map[string]decimal.Decimal),
		lastFundingTime:  make(map[string]time.Time),
		marginCalls:      make(map[string][]*MarginCall),
		liquidationQueue: make([]*LiquidationOrder, 0),
		liquidateCh:      make(chan *LiquidationOrder, 100),
	}

	// 注册风控回调
	engine.SetCallbacks(
		la.onMarginCall,
		la.onLiquidation,
		la.onAutoReduce,
	)

	// 启动强平 worker
	for i := 0; i < config.GetInt("chain.leverage-workers", 2); i++ {
		go la.liquidationWorker()
	}
	// 启动资金费率更新
	go la.fundingRateUpdater()

	common.Info("chain leverage: adapter initialized, max_leverage=10x")
	return la
}

// ─── 仓位管理 ──────────────────────────────────────────

// OpenPosition 开立杠杆仓位
// 1. 加密保证金（隐私层）
// 2. 提交到 LeverageManager SC
// 3. 在本地 RiskEngine 中登记
func (la *LeverageAdapter) OpenPosition(account, symbol string, side int, size, entryPrice decimal.Decimal, leverage int) (*ai.Position, error) {
	if leverage < 1 || leverage > 10 {
		return nil, fmt.Errorf("leverage %dx out of range [1, 10]", leverage)
	}

	// 加密保证金（Phase 2 隐私层）
	margin := entryPrice.Mul(size).Div(decimal.NewFromInt(int64(leverage)))
	_ = margin // 链上交互阶段使用

	// TODO: 提交到 LeverageManager SC
	// auth, _ := bind.NewKeyedTransactorWithChainID(la.privateKey, la.chainID)
	// sc, _ := NewLeverageManager(common.HexToAddress(la.contractAddr), la.client)
	// tx, _ := sc.OpenPosition(auth, symbol, side, size.BigInt(), entryPrice.BigInt(), uint8(leverage), encryptedMargin)
	// receipt, _ := bind.WaitMined(ctx, la.client, tx)

	// 本地登记
	pos, err := la.riskEngine.OpenPosition(account, symbol, side, size, entryPrice, leverage)
	if err != nil {
		return nil, err
	}

	common.Info("chain leverage: position opened", account, symbol,
		"side:", side, "size:", size, "leverage:", fmt.Sprintf("%dx", leverage))
	return pos, nil
}

// ClosePosition 平仓
func (la *LeverageAdapter) ClosePosition(account, symbol string) (*ai.Position, error) {
	pos := la.riskEngine.ClosePosition(account, symbol)
	if pos == nil {
		return nil, fmt.Errorf("no position found for %s/%s", account, symbol)
	}

	// TODO: 提交平仓交易到 LeverageManager SC
	// sc.ClosePosition(auth, symbol)
	common.Info("chain leverage: position closed", account, symbol, "pnl:", pos.UnrealizedPnL)
	return pos, nil
}

// AddMargin 追加保证金
func (la *LeverageAdapter) AddMargin(account, symbol string, amount decimal.Decimal) error {
	pos := la.riskEngine.GetPosition(account, symbol)
	if pos == nil {
		return fmt.Errorf("no position found for %s/%s", account, symbol)
	}

	// 更新保证金
	pos.Margin = pos.Margin.Add(amount)
	// 重新计算强平价格（保证金增加 → 强平价格更远）
	pos.LiquidationPrc = la.riskEngine.CalcLiquidationPrice(pos.EntryPrice, pos.Leverage, pos.Side)

	// TODO: 提交到 LeverageManager SC
	// sc.AddMargin(auth, symbol, amount.BigInt())
	common.Info("chain leverage: margin added", account, symbol, "amount:", amount)
	return nil
}

// GetPosition 查询仓位
func (la *LeverageAdapter) GetPosition(account, symbol string) *ai.Position {
	return la.riskEngine.GetPosition(account, symbol)
}

// GetRiskScore 查询风险评分
func (la *LeverageAdapter) GetRiskScore(account string) float64 {
	return la.riskEngine.GetRiskScore(account)
}

// ActivePositions 活跃仓位总数
func (la *LeverageAdapter) ActivePositions() int {
	return la.riskEngine.ActivePositions()
}

// ─── 标记价格更新（由 l2quote 驱动） ─────────────────

// UpdateMarkPrice 更新标记价格（从 l2quote ticker 推送）
func (la *LeverageAdapter) UpdateMarkPrice(symbol string, markPrice decimal.Decimal) {
	la.riskEngine.UpdateMarkPrice(symbol, markPrice)
}

// ─── 风控回调（由 RiskEngine 触发） ──────────────────

func (la *LeverageAdapter) onMarginCall(pos *ai.Position, level ai.RiskLevel) {
	call := &MarginCall{
		Account:       pos.Account,
		Symbol:        pos.Symbol,
		RequiredAmt:   pos.Margin.Mul(decimal.NewFromFloat(0.5)), // 需要追加 50% 保证金
		CurrentMargin: pos.Margin,
		MinMargin:     pos.EntryPrice.Mul(pos.Size).Div(decimal.NewFromInt(100)), // 1% 最小保证金
		Deadline:      time.Now().Unix() + 3600, // 1 小时内
		IssuedAt:      time.Now(),
	}

	la.mu.Lock()
	la.marginCalls[pos.Account] = append(la.marginCalls[pos.Account], call)
	la.mu.Unlock()

	common.Warn("chain leverage: MARGIN CALL", pos.Account, pos.Symbol,
		"level:", level, "required:", call.RequiredAmt)
}

func (la *LeverageAdapter) onAutoReduce(pos *ai.Position, reduceAmt decimal.Decimal) {
	common.Warn("chain leverage: AUTO REDUCE", pos.Account, pos.Symbol,
		"reduce:", reduceAmt, "size:", pos.Size)

	// TODO: 提交减仓交易到 LeverageManager SC
	// 减仓 = 平掉部分仓位
	pos.Size = pos.Size.Sub(reduceAmt)
	pos.Margin = pos.Margin.Mul(pos.Size).Div(pos.Size.Add(reduceAmt))
	pos.LiquidationPrc = la.riskEngine.CalcLiquidationPrice(pos.EntryPrice, pos.Leverage, pos.Side)
}

func (la *LeverageAdapter) onLiquidation(pos *ai.Position) {
	order := &LiquidationOrder{
		Account:        pos.Account,
		Symbol:         pos.Symbol,
		Size:           pos.Size,
		Side:           pos.Side,
		Price:          pos.MarkPrice,
		LiquidationPrc: pos.LiquidationPrc,
		Loss:           pos.UnrealizedPnL.Abs(),
		Timestamp:      time.Now(),
	}

	la.mu.Lock()
	la.liquidationQueue = append(la.liquidationQueue, order)
	la.mu.Unlock()

	la.liquidateCh <- order
}

// ─── 强平执行 ────────────────────────────────────────

func (la *LeverageAdapter) liquidationWorker() {
	for order := range la.liquidateCh {
		la.executeLiquidation(order)
	}
}

func (la *LeverageAdapter) executeLiquidation(order *LiquidationOrder) {
	common.Warn("chain leverage: EXECUTING LIQUIDATION", order.Account, order.Symbol,
		"size:", order.Size, "price:", order.Price, "loss:", order.Loss)

	// TODO: 提交强平交易到 LeverageManager SC
	// sc.Liquidate(auth, order.Account, order.Symbol)
	// 强平罚金 = position.size * liquidationPenalty
	penalty := order.Size.Mul(decimal.NewFromFloat(0.025))

	// 模拟交易哈希
	order.TxHash = fmt.Sprintf("liq_%s_%s_%d", order.Account[:8], order.Symbol, order.Timestamp.Unix())

	common.Info("chain leverage: liquidation executed, tx:", order.TxHash,
		"penalty:", penalty)
}

// GetLiquidationHistory 查询强平历史
func (la *LeverageAdapter) GetLiquidationHistory(account string) []*LiquidationOrder {
	la.mu.RLock()
	defer la.mu.RUnlock()

	var result []*LiquidationOrder
	for _, order := range la.liquidationQueue {
		if order.Account == account {
			result = append(result, order)
		}
	}
	return result
}

// GetMarginCalls 查询保证金追缴通知
func (la *LeverageAdapter) GetMarginCalls(account string) []*MarginCall {
	la.mu.RLock()
	defer la.mu.RUnlock()
	return la.marginCalls[account]
}

// ─── 资金费率 ────────────────────────────────────────

// fundingRateUpdater 定期更新资金费率（从链上 FundingRate SC 或外部数据源）
func (la *LeverageAdapter) fundingRateUpdater() {
	ticker := time.NewTicker(time.Hour * 8) // 默认 8 小时
	defer ticker.Stop()

	for range ticker.C {
		la.updateFundingRates()
	}
}

func (la *LeverageAdapter) updateFundingRates() {
	// TODO: 从 LeverageManager SC 或预言机读取资金费率
	// 默认 0.01%（每 8 小时）
	for _, symbol := range config.GetStringSlice("symbols", []string{}) {
		la.mu.Lock()
		la.fundingRates[symbol] = decimal.NewFromFloat(0.0001)
		la.lastFundingTime[symbol] = time.Now()
		la.mu.Unlock()
	}
}

// GetFundingRate 查询当前资金费率
func (la *LeverageAdapter) GetFundingRate(symbol string) decimal.Decimal {
	la.mu.RLock()
	defer la.mu.RUnlock()
	if rate, ok := la.fundingRates[symbol]; ok {
		return rate
	}
	return decimal.Zero
}

// CalculateFundingPayment 计算资金费用
// fundingPayment = positionSize * markPrice * fundingRate
// long pays short if rate > 0, short pays long if rate < 0
func (la *LeverageAdapter) CalculateFundingPayment(account, symbol string) decimal.Decimal {
	pos := la.riskEngine.GetPosition(account, symbol)
	if pos == nil {
		return decimal.Zero
	}

	rate := la.GetFundingRate(symbol)
	if rate.IsZero() {
		return decimal.Zero
	}

	payment := pos.Size.Mul(pos.MarkPrice).Mul(rate)
	// long: rate > 0 → pay, rate < 0 → receive
	if pos.Side == 1 { // short: reverse
		payment = payment.Neg()
	}
	return payment
}

// ─── 序列化（链上 ABI 编码的辅助函数） ────────────────

// encodeLeverageParams 将仓位参数编码为链上 ABI 格式
func encodeLeverageParams(account, symbol string, side int, size, price decimal.Decimal, leverage int, encryptedMargin []byte) ([]byte, error) {
	params := map[string]interface{}{
		"account":         account,
		"symbol":          symbol,
		"side":            side,
		"size":            size.String(),
		"price":           price.String(),
		"leverage":        leverage,
		"encrypted_margin": fmt.Sprintf("%x", encryptedMargin),
	}
	return json.Marshal(params)
}

// Shutdown 关闭杠杆适配器
func (la *LeverageAdapter) Shutdown() {
	close(la.liquidateCh)
	la.riskEngine.Shutdown()
	common.Info("chain leverage: shutdown, active positions:", la.riskEngine.ActivePositions())
}
