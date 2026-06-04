package ai

import (
	"sync"
	"time"

	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/core/market"

	"github.com/shopspring/decimal"
)

// Signal AI 研判信号
type Signal int

const (
	SignalHold Signal = iota
	SignalBuy
	SignalSell
	SignalStrongBuy
	SignalStrongSell
)

func (s Signal) String() string {
	switch s {
	case SignalHold:
		return "HOLD"
	case SignalBuy:
		return "BUY"
	case SignalSell:
		return "SELL"
	case SignalStrongBuy:
		return "STRONG_BUY"
	case SignalStrongSell:
		return "STRONG_SELL"
	default:
		return "UNKNOWN"
	}
}

// Engine AI 行情研判引擎
// 链下独立进程，分析盘口结构、资金流向，输出交易信号
type Engine struct {
	mu sync.RWMutex

	// 市场数据缓存（按 symbol）
	depths     map[string]*market.QuoteDepths // 最新深度
	orderFlow  map[string]*OrderFlowTracker   // 资金流向追踪
	signals    map[string]Signal              // 最新信号
	indicators map[string]*MarketIndicators   // 技术指标

	// 配置
	imbalanceThreshold decimal.Decimal // 盘口失衡阈值
	spreadThreshold    decimal.Decimal // 价差异常阈值
	flowLookback       int             // 资金流向回溯窗口（秒）
	signalCooldown     int             // 信号冷却时间（秒）
	lastSignalTime     map[string]time.Time

	// 舆情（外部数据源接入）
	sentiment map[string]float64 // symbol -> sentiment score [-1, 1]
}

// OrderFlowTracker 资金流向追踪器
type OrderFlowTracker struct {
	BuyVolume    decimal.Decimal // 主动买量
	SellVolume   decimal.Decimal // 主动卖量
	BuyCount     int64
	SellCount    int64
	LargeBuyAmt  decimal.Decimal // 大单买量 (> 阈值)
	LargeSellAmt decimal.Decimal // 大单卖量
	LastUpdate   time.Time
}

// MarketIndicators 市场指标
type MarketIndicators struct {
	BidAskSpread    decimal.Decimal // 买卖价差
	ImbalanceRatio  decimal.Decimal // 盘口失衡度 [-1, 1]
	DepthBias       decimal.Decimal // 深度偏向 [-1, 1]（正 = 买盘厚）
	Volatility      decimal.Decimal // 波动率
	PressureIndex   decimal.Decimal // 买卖压力指数 [-1, 1]
}

// NewEngine 创建 AI 引擎
func NewEngine() *Engine {
	return &Engine{
		depths:             make(map[string]*market.QuoteDepths),
		orderFlow:          make(map[string]*OrderFlowTracker),
		signals:            make(map[string]Signal),
		indicators:         make(map[string]*MarketIndicators),
		imbalanceThreshold: decimal.NewFromFloat(0.3),
		spreadThreshold:    decimal.NewFromFloat(0.02),
		flowLookback:       300, // 5 分钟
		signalCooldown:     10,  // 10 秒
		lastSignalTime:     make(map[string]time.Time),
		sentiment:          make(map[string]float64),
	}
}

// UpdateDepth 更新最新深度数据并计算指标
func (e *Engine) UpdateDepth(symbol string, depth *market.QuoteDepths) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.depths[symbol] = depth
	e.computeIndicators(symbol, depth)
	e.evaluateSignal(symbol)
}

// RecordTrade 记录成交（用于资金流向分析）
func (e *Engine) RecordTrade(symbol string, isBuyer bool, amount, price decimal.Decimal) {
	e.mu.Lock()
	defer e.mu.Unlock()

	flow, ok := e.orderFlow[symbol]
	if !ok {
		flow = &OrderFlowTracker{}
		e.orderFlow[symbol] = flow
	}

	flow.LastUpdate = time.Now()
	if isBuyer {
		flow.BuyVolume = flow.BuyVolume.Add(amount)
		flow.BuyCount++
		if amount.GreaterThan(decimal.NewFromFloat(10)) { // 大单阈值
			flow.LargeBuyAmt = flow.LargeBuyAmt.Add(amount)
		}
	} else {
		flow.SellVolume = flow.SellVolume.Add(amount)
		flow.SellCount++
		if amount.GreaterThan(decimal.NewFromFloat(10)) {
			flow.LargeSellAmt = flow.LargeSellAmt.Add(amount)
		}
	}
}

// SetSentiment 设置外部舆情得分（由外部数据源提供）
func (e *Engine) SetSentiment(symbol string, score float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	// 截断到 [-1, 1]
	if score > 1.0 {
		score = 1.0
	} else if score < -1.0 {
		score = -1.0
	}
	e.sentiment[symbol] = score
}

// GetSignal 获取当前交易信号
func (e *Engine) GetSignal(symbol string) Signal {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if sig, ok := e.signals[symbol]; ok {
		return sig
	}
	return SignalHold
}

// GetIndicators 获取市场指标
func (e *Engine) GetIndicators(symbol string) *MarketIndicators {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.indicators[symbol]
}

// computeIndicators 计算市场指标
func (e *Engine) computeIndicators(symbol string, depth *market.QuoteDepths) {
	ind := &MarketIndicators{}
	e.indicators[symbol] = ind

	if len(depth.Bids) == 0 || len(depth.Asks) == 0 {
		return
	}

	bestBid := depth.Bids[0][0]
	bestAsk := depth.Asks[0][0]

	// 1. 买卖价差率
	if !bestAsk.IsZero() {
		ind.BidAskSpread = bestAsk.Sub(bestBid).Div(bestAsk)
	}

	// 2. 盘口失衡度 = (买量 - 卖量) / (买量 + 卖量)
	totalBidVol := decimal.Zero
	totalAskVol := decimal.Zero
	bidDepthWeight := decimal.Zero // 加权买盘深度
	askDepthWeight := decimal.Zero // 加权卖盘深度

	for i, bid := range depth.Bids {
		totalBidVol = totalBidVol.Add(bid[1])
		// 越靠近最优价的挂单权重越高
		weight := decimal.NewFromInt(int64(len(depth.Bids) - i))
		bidDepthWeight = bidDepthWeight.Add(bid[1].Mul(weight))
	}
	for i, ask := range depth.Asks {
		totalAskVol = totalAskVol.Add(ask[1])
		weight := decimal.NewFromInt(int64(len(depth.Asks) - i))
		askDepthWeight = askDepthWeight.Add(ask[1].Mul(weight))
	}

	totalVol := totalBidVol.Add(totalAskVol)
	if !totalVol.IsZero() {
		ind.ImbalanceRatio = totalBidVol.Sub(totalAskVol).Div(totalVol)
	}

	// 3. 深度偏向
	totalWeight := bidDepthWeight.Add(askDepthWeight)
	if !totalWeight.IsZero() {
		ind.DepthBias = bidDepthWeight.Sub(askDepthWeight).Div(totalWeight)
	}

	// 4. 买卖压力指数（综合盘口和资金流向）
	flow, ok := e.orderFlow[symbol]
	if ok && !flow.BuyVolume.Add(flow.SellVolume).IsZero() {
		flowRatio := flow.BuyVolume.Sub(flow.SellVolume).Div(flow.BuyVolume.Add(flow.SellVolume))
		ind.PressureIndex = ind.ImbalanceRatio.Add(flowRatio).Div(decimal.NewFromInt(2))
	} else {
		ind.PressureIndex = ind.ImbalanceRatio
	}
}

// evaluateSignal 根据指标生成交易信号
func (e *Engine) evaluateSignal(symbol string) {
	ind := e.indicators[symbol]
	if ind == nil {
		return
	}

	// 冷却检查
	if last, ok := e.lastSignalTime[symbol]; ok {
		if time.Since(last).Seconds() < float64(e.signalCooldown) {
			return
		}
	}

	var signal Signal
	score := decimal.Zero

	// 盘口失衡度加权 (+40%)
	if ind.ImbalanceRatio.Abs().GreaterThan(e.imbalanceThreshold) {
		score = score.Add(ind.ImbalanceRatio.Mul(decimal.NewFromFloat(0.4)))
	}

	// 深度偏向加权 (+30%)
	score = score.Add(ind.DepthBias.Mul(decimal.NewFromFloat(0.3)))

	// 舆情加权 (+30%)
	if sent, ok := e.sentiment[symbol]; ok {
		score = score.Add(decimal.NewFromFloat(sent * 0.3))
	}

	// 判定信号
	switch {
	case score.GreaterThan(decimal.NewFromFloat(0.5)):
		signal = SignalStrongBuy
	case score.GreaterThan(decimal.NewFromFloat(0.15)):
		signal = SignalBuy
	case score.LessThan(decimal.NewFromFloat(-0.5)):
		signal = SignalStrongSell
	case score.LessThan(decimal.NewFromFloat(-0.15)):
		signal = SignalSell
	default:
		signal = SignalHold
	}

	prev := e.signals[symbol]
	if prev != signal {
		e.signals[symbol] = signal
		e.lastSignalTime[symbol] = time.Now()
		common.Info("AI: signal change", symbol, ":", prev, "→", signal,
			"score:", score.StringFixed(4),
			"imbalance:", ind.ImbalanceRatio.StringFixed(4),
			"pressure:", ind.PressureIndex.StringFixed(4))
	}
}

// DetectSpoofing 检测幌骗（频繁挂撤单）
// 返回疑似幌骗的订单 ID 列表
func (e *Engine) DetectSpoofing(symbol string, recentCancels []int64, recentOrders []int64) []int64 {
	if len(recentOrders) == 0 {
		return nil
	}
	// 幌骗特征：取消量 / 下单量 > 80%
	cancelRatio := float64(len(recentCancels)) / float64(len(recentOrders))
	if cancelRatio > 0.8 {
		common.Warn("AI: potential spoofing detected for", symbol,
			"cancel_ratio:", cancelRatio)
		return recentCancels
	}
	return nil
}

// Shutdown 关闭 AI 引擎
func (e *Engine) Shutdown() {
	common.Info("AI engine: shutdown")
}
