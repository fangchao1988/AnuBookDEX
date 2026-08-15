package aleo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"github.com/shopspring/decimal"
)

// 订单状态（P3 委托列表真实数据）
const (
	OrderStatusWaiting  = "waiting"  // 等待中（已提交 / 挂单）
	OrderStatusPartial  = "partial"  // 部分成交
	OrderStatusFilled   = "filled"   // 已完成
	OrderStatusCanceled = "canceled" // 已撤销（链上 cancel_order）
)

// 成交结算状态（链上 settle）
const (
	SettlePending = "pending" // 待链上结算
	SettleSettled = "settled" // 已链上结算
	SettleFailed  = "failed"  // 链上结算失败
)

// TradeRecord 成交明细（P3 成交记录真实数据）：撮合回执时按 maker 成交逐笔记录。
// 内部存储以撮合视角（Trader=maker，Side=maker 方向）；ListTrades 输出时按查询者视角翻转。
type TradeRecord struct {
	OrderId      int64  `json:"order_id"` // maker 订单
	TakerOrderId int64  `json:"taker_order_id,omitempty"` // taker 订单（重结算用）
	Symbol       string `json:"symbol"`
	Side         string `json:"side"`   // maker 方向（内部）/ 查询者方向（API 输出）
	Price        string `json:"price"`
	Amount       string `json:"amount"`
	Trader       string `json:"trader"` // maker 交易者（内部）/ 查询者（API 输出）
	Taker        string `json:"taker"`  // taker 交易者（内部）/ 对手方（API 输出）
	Ts           int64  `json:"ts"`
	SettleStatus string `json:"settle_status"` // 链上结算状态（pending/settled/failed）
}

// OrderRecord 内存订单状态记录：提交时落库（等待中），撮合回执时更新成交量与状态。
// 数据源：POST /order（RecordSubmit）+ Settlement.SubmitBatch（RecordMatch）。
type OrderRecord struct {
	OrderId  int64  `json:"order_id"`
	Symbol   string `json:"symbol"`
	Trader   string `json:"trader"`
	Side     string `json:"side"`  // buy / sell
	Type     string `json:"type"`  // limit
	Price    string `json:"price"`
	Amount   string `json:"amount"`
	Filled   string `json:"filled"`
	Status   string `json:"status"`
	CreateAt int64  `json:"create_at"`
}

// RecordSubmit 记录新提交的订单（状态：等待中）。
func (p *OrderPool) RecordSubmit(o *match.Order, symbol string) {
	side := "buy"
	if o.BuyOrSell == match.Sell {
		side = "sell"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records[o.OrderId] = &OrderRecord{
		OrderId:  o.OrderId,
		Symbol:   symbol,
		Trader:   o.UserAddress,
		Side:     side,
		Type:     "limit",
		Price:    o.Price.String(),
		Amount:   o.UnfilledAmount.String(),
		Filled:   decimal.Zero.String(),
		Status:   OrderStatusWaiting,
		CreateAt: o.CreateAt,
	}
}

// RecordMatch 撮合回执：按 mr.OrderId 更新成交量与状态（由 Settlement.SubmitBatch 调用），
// 并按 maker 成交逐笔记录成交明细（/api/v1/trades 数据源）。
func (p *OrderPool) RecordMatch(mrs []*match.MatchResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, mr := range mrs {
		rec, ok := p.records[mr.OrderId]
		if !ok {
			continue
		}
		total := decimal.Zero
		state := ""
		for _, item := range mr.Items {
			if item.FilledAmount != nil {
				total = total.Add(*item.FilledAmount)
			}
			if item.Role == "taker" {
				state = item.State
			}
			// 成交明细：maker 的成交条目（filled/partial-filled）
			if item.Role != "maker" || item.OrderId == 0 || item.FilledAmount == nil ||
				item.FilledAmount.IsZero() || (item.State != "filled" && item.State != "partial-filled") {
				continue
			}
			makerRec := p.records[item.OrderId]
			makerTrader := ""
			makerSide := "buy"
			if makerRec != nil {
				makerTrader = makerRec.Trader
				makerSide = makerRec.Side
			}
			price := mr.Price.String()
			if item.Price != nil {
				price = item.Price.String()
			}
			p.trades = append(p.trades, &TradeRecord{
				OrderId:      item.OrderId,
				TakerOrderId: mr.OrderId,
				Symbol:       mr.Symbol,
				Side:         makerSide,
				Price:        price,
				Amount:       item.FilledAmount.String(),
				Trader:       makerTrader,
				Taker:        rec.Trader,
				Ts:           mr.Ts,
				SettleStatus: SettlePending,
			})
		}
		rec.Filled = total.String()
		switch state {
		case "filled":
			rec.Status = OrderStatusFilled
		case "partial-filled":
			rec.Status = OrderStatusPartial
		case "canceled", "partial-canceled":
			rec.Status = OrderStatusCanceled
		default:
			rec.Status = OrderStatusWaiting
		}

		// maker 订单状态同步更新（撮合中 maker 被成交，前端委托列表需反映）
		for _, item := range mr.Items {
			if item.Role != "maker" || item.OrderId == 0 {
				continue
			}
			makerRec, ok := p.records[item.OrderId]
			if !ok {
				continue
			}
			if item.FilledAmount != nil && !item.FilledAmount.IsZero() {
				// 累计成交（按撮合回执全量覆盖：mr 为当前撮合结果）
				makerRec.Filled = item.FilledAmount.String()
			}
			switch item.State {
			case "filled":
				makerRec.Status = OrderStatusFilled
			case "partial-filled":
				makerRec.Status = OrderStatusPartial
			}
		}
	}
}

// UpdateTradeSettleStatus 更新成交的链上结算状态（settlement 回执时调用）
func (p *OrderPool) UpdateTradeSettleStatus(orderId int64, status string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range p.trades {
		if t.OrderId == orderId && t.SettleStatus != status {
			t.SettleStatus = status
		}
	}
}

// ListTrades 查询成交记录（trader 匹配 maker 或 taker，symbol 过滤，倒序）。
// 内部存储以撮合视角（Trader=maker，Side=maker 方向）；带 trader 查询时按查询者
// 视角翻转：Side 恒为查询者自己的方向，Taker 恒为对手方（前端直接渲染）。
func (p *OrderPool) ListTrades(trader, symbol string, limit int) []*TradeRecord {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*TradeRecord, 0, len(p.trades))
	for i := len(p.trades) - 1; i >= 0; i-- {
		t := p.trades[i]
		if trader != "" && t.Trader != trader && t.Taker != trader {
			continue
		}
		if symbol != "" && t.Symbol != symbol {
			continue
		}
		cp := *t
		// 查询者是 taker：Trader/Taker 对调，Side 取反（查询者自己的方向）
		if trader != "" && cp.Taker == trader {
			cp.Trader, cp.Taker = cp.Taker, cp.Trader
			if cp.Side == "buy" {
				cp.Side = "sell"
			} else {
				cp.Side = "buy"
			}
		}
		out = append(out, &cp)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// ListFailedTrades 返回链上结算失败的成交（供 settlement 后台重结算扫描）。
func (p *OrderPool) ListFailedTrades() []*TradeRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*TradeRecord, 0, len(p.trades))
	for _, t := range p.trades {
		if t.SettleStatus == SettleFailed {
			cp := *t
			out = append(out, &cp)
		}
	}
	return out
}

// ListOrders 查询订单记录（trader/symbol 过滤，按 order_id 倒序，limit 默认 50 上限 200）。
func (p *OrderPool) ListOrders(trader, symbol string, limit int) []*OrderRecord {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*OrderRecord, 0, len(p.records))
	for _, rec := range p.records {
		if trader != "" && rec.Trader != trader {
			continue
		}
		if symbol != "" && rec.Symbol != symbol {
			continue
		}
		cp := *rec
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OrderId > out[j].OrderId })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// handleOrders GET /api/v1/orders 委托列表查询：
// ?trader=&symbol=&limit=（trader 必填语义由调用方决定，空串查全部）
func HandleOrders(pool *OrderPool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}
		trader := strings.TrimSpace(r.URL.Query().Get("trader"))
		symbol := strings.TrimSpace(r.URL.Query().Get("symbol"))
		limit := 0
		fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
		records := pool.ListOrders(trader, symbol, limit)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(records); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// HandleTrades GET /api/v1/trades 成交记录查询：
// ?trader=&symbol=&limit=（trader 匹配 maker 或 taker）
func HandleTrades(pool *OrderPool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}
		trader := strings.TrimSpace(r.URL.Query().Get("trader"))
		symbol := strings.TrimSpace(r.URL.Query().Get("symbol"))
		limit := 0
		fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
		trades := pool.ListTrades(trader, symbol, limit)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(trades); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// HandleOperator GET /api/v1/operator 引擎 operator 地址（Order record owner）：
// 前端 place_order 需以 operator 为接收方，地址来自 chain.aleo.address 配置
func HandleOperator() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"address": config.GetString("chain.aleo.address", ""),
		})
	}
}
