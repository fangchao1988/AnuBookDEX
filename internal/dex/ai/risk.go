package ai

import (
	"sync"
	"time"

	"github.com/AnuBookDEX/engine/internal/infra/common"

	"github.com/shopspring/decimal"
)

// RiskLevel 风险等级
type RiskLevel int

const (
	RiskLow    RiskLevel = iota // 低风险
	RiskMedium                   // 中风险
	RiskHigh                     // 高风险
	RiskCritical                 // 危险（触发强平）
)

func (r RiskLevel) String() string {
	switch r {
	case RiskLow:
		return "LOW"
	case RiskMedium:
		return "MEDIUM"
	case RiskHigh:
		return "HIGH"
	case RiskCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

// Position 用户持仓
type Position struct {
	Account        string          `json:"account"`         // 账户地址
	Symbol         string          `json:"symbol"`          // 交易对
	Side           int             `json:"side"`            // 0=long, 1=short
	Size           decimal.Decimal `json:"size"`            // 持仓数量
	EntryPrice     decimal.Decimal `json:"entry_price"`     // 开仓均价
	MarkPrice      decimal.Decimal `json:"mark_price"`      // 标记价格
	Leverage       int             `json:"leverage"`        // 杠杆倍数 (1-10x)
	Margin         decimal.Decimal `json:"margin"`          // 保证金
	UnrealizedPnL  decimal.Decimal `json:"unrealized_pnl"`  // 未实现盈亏
	LiquidationPrc decimal.Decimal `json:"liquidation_prc"` // 强平价格
	LastUpdate     time.Time       `json:"last_update"`
}

// RiskConfig 风控配置
type RiskConfig struct {
	MaxLeverage        int             // 最大杠杆倍数
	MaintenanceMargin  decimal.Decimal // 维持保证金率 (e.g. 0.005 = 0.5%)
	LiquidationPenalty decimal.Decimal // 强平罚金率
	MaxPositionSize    decimal.Decimal // 单一持仓上限
	AutoReducePct     decimal.Decimal // 自动减仓比例
	RiskCheckInterval time.Duration   // 风控检查间隔
}

// DefaultRiskConfig 默认风控配置
func DefaultRiskConfig() *RiskConfig {
	return &RiskConfig{
		MaxLeverage:        10,
		MaintenanceMargin:  decimal.NewFromFloat(0.005),  // 0.5%
		LiquidationPenalty: decimal.NewFromFloat(0.025),  // 2.5%
		MaxPositionSize:    decimal.NewFromFloat(100000), // 10 万单位
		AutoReducePct:     decimal.NewFromFloat(0.5),    // 减仓 50%
		RiskCheckInterval: time.Second * 5,
	}
}

// RiskEngine 风控引擎
// 7×24 监控杠杆仓位，自动触发减仓/强平
type RiskEngine struct {
	mu     sync.RWMutex
	config *RiskConfig

	positions   map[string]*Position // account+symbol → position
	riskScore   map[string]float64   // account → risk score [0, 1]
	liquidations map[string][]*LiquidationEvent // account → history

	// 回调：触发风控动作时通知外部
	onMarginCall     func(pos *Position, level RiskLevel)
	onLiquidation    func(pos *Position)
	onAutoReduce     func(pos *Position, reduceAmt decimal.Decimal)
}

// LiquidationEvent 强平事件
type LiquidationEvent struct {
	Account       string          `json:"account"`
	Symbol        string          `json:"symbol"`
	Size          decimal.Decimal `json:"size"`
	Price         decimal.Decimal `json:"price"`          // 强平价格
	PnL           decimal.Decimal `json:"pnl"`            // 盈亏
	Timestamp     time.Time       `json:"timestamp"`
}

// NewRiskEngine 创建风控引擎
func NewRiskEngine(config *RiskConfig) *RiskEngine {
	if config == nil {
		config = DefaultRiskConfig()
	}
	return &RiskEngine{
		config:       config,
		positions:    make(map[string]*Position),
		riskScore:    make(map[string]float64),
		liquidations: make(map[string][]*LiquidationEvent),
	}
}

// SetCallbacks 设置风控回调
func (e *RiskEngine) SetCallbacks(onMarginCall func(*Position, RiskLevel),
	onLiquidation func(*Position), onAutoReduce func(*Position, decimal.Decimal)) {
	e.onMarginCall = onMarginCall
	e.onLiquidation = onLiquidation
	e.onAutoReduce = onAutoReduce
}

// OpenPosition 开仓
func (e *RiskEngine) OpenPosition(account, symbol string, side int, size, entryPrice decimal.Decimal, leverage int) (*Position, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// 杠杆校验
	if leverage > e.config.MaxLeverage {
		leverage = e.config.MaxLeverage
		common.Warn("AI risk: leverage capped to", leverage, "for", account)
	}

	// 仓位大小校验
	if size.GreaterThan(e.config.MaxPositionSize) {
		common.Warn("AI risk: position size", size, "exceeds max", e.config.MaxPositionSize, "for", account)
	}

	margin := entryPrice.Mul(size).Div(decimal.NewFromInt(int64(leverage)))
	liqPrice := e.CalcLiquidationPrice(entryPrice, leverage, side)

	pos := &Position{
		Account:        account,
		Symbol:         symbol,
		Side:           side,
		Size:           size,
		EntryPrice:     entryPrice,
		MarkPrice:      entryPrice,
		Leverage:       leverage,
		Margin:         margin,
		LiquidationPrc: liqPrice,
		LastUpdate:     time.Now(),
	}

	key := e.posKey(account, symbol)
	e.positions[key] = pos

	direction := "LONG"
	if side == 1 {
		direction = "SHORT"
	}
	common.Info("AI risk: position opened", account, direction, symbol,
		"size:", size, "leverage:", leverage, "x", "liq_price:", liqPrice)

	return pos, nil
}

// ClosePosition 平仓
func (e *RiskEngine) ClosePosition(account, symbol string) *Position {
	e.mu.Lock()
	defer e.mu.Unlock()

	key := e.posKey(account, symbol)
	pos, ok := e.positions[key]
	if !ok {
		return nil
	}

	delete(e.positions, key)
	delete(e.riskScore, account)

	common.Info("AI risk: position closed", account, symbol)
	return pos
}

// UpdateMarkPrice 更新标记价格并检查风险
func (e *RiskEngine) UpdateMarkPrice(symbol string, markPrice decimal.Decimal) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for key, pos := range e.positions {
		if pos.Symbol != symbol {
			continue
		}
		pos.MarkPrice = markPrice
		pos.LastUpdate = time.Now()

		// 计算未实现盈亏
		if pos.Side == 0 { // long
			pos.UnrealizedPnL = markPrice.Sub(pos.EntryPrice).Mul(pos.Size)
		} else { // short
			pos.UnrealizedPnL = pos.EntryPrice.Sub(markPrice).Mul(pos.Size)
		}

		// 风险检查
		level := e.assessRisk(pos)
		e.riskScore[pos.Account], _ = e.riskScoreFloat(pos)

		switch level {
		case RiskCritical:
			if e.onLiquidation != nil {
				e.onLiquidation(pos)
			}
			e.recordLiquidation(key, pos, markPrice)

		case RiskHigh:
			if e.onAutoReduce != nil {
				reduceAmt := pos.Size.Mul(e.config.AutoReducePct)
				e.onAutoReduce(pos, reduceAmt)
			}

		case RiskMedium:
			if e.onMarginCall != nil {
				e.onMarginCall(pos, level)
			}
		}
	}
}

// assessRisk 评估仓位风险等级
func (e *RiskEngine) assessRisk(pos *Position) RiskLevel {
	if pos.Size.IsZero() {
		return RiskLow
	}

	// 计算距离强平价格的距离
	var distancePct decimal.Decimal
	if pos.MarkPrice.IsZero() {
		return RiskLow
	}

	if pos.Side == 0 { // long
		// 强平价格 < 标记价格时安全
		if pos.LiquidationPrc.GreaterThanOrEqual(pos.MarkPrice) {
			return RiskCritical
		}
		distancePct = pos.MarkPrice.Sub(pos.LiquidationPrc).Div(pos.MarkPrice)
	} else { // short
		if pos.LiquidationPrc.LessThanOrEqual(pos.MarkPrice) {
			return RiskCritical
		}
		distancePct = pos.LiquidationPrc.Sub(pos.MarkPrice).Div(pos.MarkPrice)
	}

	distance, _ := distancePct.Float64()

	switch {
	case distance < 0.01:
		return RiskCritical
	case distance < 0.05:
		return RiskHigh
	case distance < 0.10:
		return RiskMedium
	default:
		return RiskLow
	}
}

// calcLiquidationPrice 计算强平价格
// long:  entryPrice * (1 - 1/leverage + maintenanceMargin)
// short: entryPrice * (1 + 1/leverage - maintenanceMargin)
// CalcLiquidationPrice 计算强平价格（导出供 chain 层使用）
func (e *RiskEngine) CalcLiquidationPrice(entryPrice decimal.Decimal, leverage int, side int) decimal.Decimal {
	lev := decimal.NewFromInt(int64(leverage))
	buffer := decimal.NewFromInt(1).Div(lev).Add(e.config.MaintenanceMargin)

	if side == 0 { // long
		return entryPrice.Mul(decimal.NewFromInt(1).Sub(buffer))
	}
	// short
	return entryPrice.Mul(decimal.NewFromInt(1).Add(buffer))
}

// recordLiquidation 记录强平事件
func (e *RiskEngine) recordLiquidation(key string, pos *Position, price decimal.Decimal) {
	event := &LiquidationEvent{
		Account:   pos.Account,
		Symbol:    pos.Symbol,
		Size:      pos.Size,
		Price:     price,
		PnL:       pos.UnrealizedPnL,
		Timestamp: time.Now(),
	}
	e.liquidations[pos.Account] = append(e.liquidations[pos.Account], event)

	common.Warn("AI risk: LIQUIDATION", pos.Account, pos.Symbol,
		"size:", pos.Size, "price:", price, "pnl:", pos.UnrealizedPnL)

	// 清除仓位
	delete(e.positions, key)
}

// posKey 生成仓位键
func (e *RiskEngine) posKey(account, symbol string) string {
	return account + ":" + symbol
}

// riskScoreFloat 计算风险评分
func (e *RiskEngine) riskScoreFloat(pos *Position) (float64, error) {
	if pos.Margin.IsZero() {
		return 1.0, nil
	}
	// 风险评分 = |unrealizedPnL| / margin → [0, 1]
	ratio := pos.UnrealizedPnL.Abs().Div(pos.Margin)
	f, _ := ratio.Float64()
	if f > 1.0 {
		f = 1.0
	}
	return f, nil
}

// GetPosition 查询持仓
func (e *RiskEngine) GetPosition(account, symbol string) *Position {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.positions[e.posKey(account, symbol)]
}

// GetRiskScore 获取账户风险评分
func (e *RiskEngine) GetRiskScore(account string) float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.riskScore[account]
}

// GetLiquidationHistory 获取强平历史
func (e *RiskEngine) GetLiquidationHistory(account string) []*LiquidationEvent {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.liquidations[account]
}

// ActivePositions 返回活跃仓位数量
func (e *RiskEngine) ActivePositions() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.positions)
}

// Shutdown 关闭风控引擎
func (e *RiskEngine) Shutdown() {
	common.Info("AI risk: shutdown, active positions:", len(e.positions))
}
