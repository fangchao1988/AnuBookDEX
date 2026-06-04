package ai

import (
	"math/rand"
	"sync"
	"time"

	"github.com/AnuBookDEX/engine/internal/infra/common"

	"github.com/shopspring/decimal"
)

// SplitStrategy 拆分策略
type SplitStrategy int

const (
	StrategyTWAP SplitStrategy = iota // 时间加权均价
	StrategyVWAP                      // 成交量加权均价
	StrategyAdaptive                  // 自适应（根据盘口动态调整）
)

func (s SplitStrategy) String() string {
	switch s {
	case StrategyTWAP:
		return "TWAP"
	case StrategyVWAP:
		return "VWAP"
	case StrategyAdaptive:
		return "ADAPTIVE"
	default:
		return "UNKNOWN"
	}
}

// IcebergOrder 冰山订单
type IcebergOrder struct {
	Symbol        string          `json:"symbol"`
	TotalAmount   decimal.Decimal `json:"total_amount"`   // 总数量
	ExecutedAmt   decimal.Decimal `json:"executed_amt"`   // 已执行数量
	RemainingAmt  decimal.Decimal `json:"remaining_amt"`  // 剩余数量
	SliceSize     decimal.Decimal `json:"slice_size"`     // 每片大小
	SliceInterval time.Duration   `json:"slice_interval"`  // 片间隔
	Strategy      SplitStrategy   `json:"strategy"`
	BuyOrSell     int             `json:"buy_or_sell"`    // 0=buy, 1=sell
	LimitPrice    decimal.Decimal `json:"limit_price"`    // 限价（可为 0 表示市价）
	CreatedAt     time.Time       `json:"created_at"`
	LastSliceAt   time.Time       `json:"last_slice_at"`
	SlicesSent    int             `json:"slices_sent"`     // 已发送片数
	Active        bool            `json:"active"`
	JitterPct     float64         `json:"jitter_pct"`      // 随机抖动比例 [0, 1]
}

// IcebergEngine 冰山订单拆分引擎
type IcebergEngine struct {
	mu sync.RWMutex

	orders map[string]*IcebergOrder // orderID → iceberg order

	// 执行回调：每片就绪时调用
	onSlice func(slice *IcebergSlice)

	// 默认参数
	defaultSliceSize     decimal.Decimal
	defaultSliceInterval time.Duration
	defaultJitterPct     float64
	maxSlicesPerMinute   int
}

// IcebergSlice 冰山订单的一片
type IcebergSlice struct {
	ParentID  string          `json:"parent_id"`
	Symbol    string          `json:"symbol"`
	Amount    decimal.Decimal `json:"amount"`
	Price     decimal.Decimal `json:"price"`      // 限价，0=市价
	BuyOrSell int             `json:"buy_or_sell"`
	SliceNum  int             `json:"slice_num"`  // 第几片
	TotalAmt  decimal.Decimal `json:"total_amt"`  // 总订单量
	Progress  float64         `json:"progress"`    // 进度 [0, 1]
}

// NewIcebergEngine 创建冰山拆分引擎
func NewIcebergEngine(onSlice func(slice *IcebergSlice)) *IcebergEngine {
	return &IcebergEngine{
		orders:               make(map[string]*IcebergOrder),
		onSlice:              onSlice,
		defaultSliceSize:     decimal.NewFromFloat(1.0),     // 默认每片 1 单位
		defaultSliceInterval: time.Second * 30,               // 默认 30 秒间隔
		defaultJitterPct:     0.2,                            // 默认 ±20% 随机抖动
		maxSlicesPerMinute:   10,                             // 每分钟最多 10 片
	}
}

// SubmitIceberg 提交冰山订单
func (e *IcebergEngine) SubmitIceberg(orderID, symbol string, totalAmount, limitPrice decimal.Decimal,
	buyOrSell int, strategy SplitStrategy) (*IcebergOrder, error) {

	e.mu.Lock()
	defer e.mu.Unlock()

	ice := &IcebergOrder{
		Symbol:        symbol,
		TotalAmount:   totalAmount,
		RemainingAmt:  totalAmount,
		SliceSize:     e.defaultSliceSize,
		SliceInterval: e.defaultSliceInterval,
		Strategy:      strategy,
		BuyOrSell:     buyOrSell,
		LimitPrice:    limitPrice,
		CreatedAt:     time.Now(),
		Active:        true,
		JitterPct:     e.defaultJitterPct,
	}

	// 自适应策略：根据总金额动态调整片大小
	if strategy == StrategyAdaptive {
		ice.SliceSize = e.computeAdaptiveSliceSize(totalAmount, limitPrice)
	}

	e.orders[orderID] = ice

	direction := "BUY"
	if buyOrSell == 1 {
		direction = "SELL"
	}
	common.Info("AI iceberg: submitted", orderID, direction, symbol,
		"total:", totalAmount, "slice:", ice.SliceSize, "strategy:", strategy)

	return ice, nil
}

// CancelIceberg 取消冰山订单
func (e *IcebergEngine) CancelIceberg(orderID string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if ice, ok := e.orders[orderID]; ok {
		ice.Active = false
		common.Info("AI iceberg: cancelled", orderID,
			"executed:", ice.ExecutedAmt, "of", ice.TotalAmount,
			"slices:", ice.SlicesSent)
	}
}

// Tick 定时触发（由外部定时器调用），检查所有活跃冰山订单是否需要发送下一片
func (e *IcebergEngine) Tick() {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()

	for id, ice := range e.orders {
		if !ice.Active || ice.RemainingAmt.LessThanOrEqual(decimal.Zero) {
			continue
		}

		// 检查间隔
		if now.Sub(ice.LastSliceAt) < ice.SliceInterval {
			continue
		}

		// 限速检查
		if ice.SlicesSent > 0 {
			elapsedMin := now.Sub(ice.CreatedAt).Minutes()
			if elapsedMin > 0 && float64(ice.SlicesSent)/elapsedMin > float64(e.maxSlicesPerMinute) {
				continue
			}
		}

		// 计算本片大小
		sliceAmt := decimal.Min(ice.SliceSize, ice.RemainingAmt)

		// 随机抖动（±jitterPct 范围内）
		if ice.JitterPct > 0 && ice.RemainingAmt.GreaterThan(sliceAmt) {
			jitter := decimal.NewFromFloat(1.0 + (rand.Float64()*2-1.0)*ice.JitterPct)
			sliceAmt = sliceAmt.Mul(jitter)
			if sliceAmt.GreaterThan(ice.RemainingAmt) {
				sliceAmt = ice.RemainingAmt
			}
		}

		// 最后一片 = 全部剩余
		if ice.RemainingAmt.Sub(sliceAmt).LessThan(ice.SliceSize.Mul(decimal.NewFromFloat(0.5))) {
			sliceAmt = ice.RemainingAmt
		}

		ice.ExecutedAmt = ice.ExecutedAmt.Add(sliceAmt)
		ice.RemainingAmt = ice.RemainingAmt.Sub(sliceAmt)
		ice.SlicesSent++
		ice.LastSliceAt = now

		progress, _ := ice.ExecutedAmt.Div(ice.TotalAmount).Float64()

		slice := &IcebergSlice{
			ParentID:  id,
			Symbol:    ice.Symbol,
			Amount:    sliceAmt,
			Price:     ice.LimitPrice,
			BuyOrSell: ice.BuyOrSell,
			SliceNum:  ice.SlicesSent,
			TotalAmt:  ice.TotalAmount,
			Progress:  progress,
		}

		// 回调通知外部
		if e.onSlice != nil {
			e.onSlice(slice)
		}

		// 完成
		if ice.RemainingAmt.LessThanOrEqual(decimal.Zero) {
			ice.Active = false
			common.Info("AI iceberg: completed", id, "slices:", ice.SlicesSent,
				"total:", ice.TotalAmount)
		}
	}
}

// GetStatus 查询冰山订单状态
func (e *IcebergEngine) GetStatus(orderID string) *IcebergOrder {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.orders[orderID]
}

// ActiveOrders 返回活跃的冰山订单数量
func (e *IcebergEngine) ActiveOrders() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	count := 0
	for _, ice := range e.orders {
		if ice.Active {
			count++
		}
	}
	return count
}

// computeAdaptiveSliceSize 根据市场深度计算自适应片大小
func (e *IcebergEngine) computeAdaptiveSliceSize(totalAmount, limitPrice decimal.Decimal) decimal.Decimal {
	// 自适应策略：
	//   - 大盘口 → 大片（减少执行时间）
	//   - 小盘口 → 小片（降低市场影响）
	//   - 默认 = 总量的 5%，最少 0.01
	pct := decimal.NewFromFloat(0.05)
	slice := totalAmount.Mul(pct)

	minSlice := decimal.NewFromFloat(0.01)
	if slice.LessThan(minSlice) {
		slice = minSlice
	}

	return slice.Truncate(2) // 截断到 2 位小数
}

// Shutdown 关闭冰山引擎
func (e *IcebergEngine) Shutdown() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, ice := range e.orders {
		ice.Active = false
		common.Info("AI iceberg: force-cancelled on shutdown:", id)
	}
}
