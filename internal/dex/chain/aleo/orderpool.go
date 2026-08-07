package aleo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"github.com/shopspring/decimal"
)

// PooledOrder 链下提交的订单（Phase 2b 链下订单通道）：
// Order 明文用于撮合；Ciphertext 是链上 Order record 密文，供结算时构造 settle。
type PooledOrder struct {
	Order      *match.Order `json:"order"`
	Ciphertext string       `json:"ciphertext"` // Order record ciphertext（链上，settle 用）
}

// OrderPool 链下订单池：
// 用户经引擎 API 提交（明文 + Order ciphertext）；subscriber 消费明文送撮合；
// settlement 按 OrderId 查 ciphertext 构造 settle（leo execute 用 operator view key 自动解密）。
type OrderPool struct {
	mu          sync.Mutex
	pending     map[uint64]*PooledOrder // order_id -> 待撮合订单
	ciphertexts map[int64]string        // order_id -> record ciphertext（结算用，撮合后保留）
	ch          chan *PooledOrder       // 待撮合队列（FIFO）
	records     map[int64]*OrderRecord  // order_id -> 订单状态记录（P3 委托列表）
	trades      []*TradeRecord          // 成交明细（P3 成交记录，撮合回执时追加）
}

// NewOrderPool 创建订单池。
func NewOrderPool() *OrderPool {
	return &OrderPool{
		pending:     make(map[uint64]*PooledOrder),
		ciphertexts: make(map[int64]string),
		ch:          make(chan *PooledOrder, 5000),
		records:     make(map[int64]*OrderRecord),
		trades:      make([]*TradeRecord, 0, 1024),
	}
}

// Submit 提交链下订单（明文 + ciphertext），进入待撮合队列。
func (p *OrderPool) Submit(o *PooledOrder) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if o.Order == nil || o.Order.OrderId <= 0 {
		return fmt.Errorf("invalid order: missing order_id")
	}
	id := uint64(o.Order.OrderId)
	if _, ok := p.pending[id]; ok {
		return fmt.Errorf("order %d already submitted", o.Order.OrderId)
	}
	// 生产模式要求 Order record ciphertext（结算用）；本地联调可关闭
	// （chain.aleo.require-ciphertext=false，前端先接订单通道、钱包后续接）
	if o.Ciphertext == "" && config.GetBool("chain.aleo.require-ciphertext", true) {
		return fmt.Errorf("order %d missing ciphertext", o.Order.OrderId)
	}
	p.pending[id] = o
	p.ciphertexts[o.Order.OrderId] = o.Ciphertext
	select {
	case p.ch <- o:
	default:
		delete(p.pending, id)
		return fmt.Errorf("order pool full")
	}
	return nil
}

// Orders 返回待撮合订单 channel（subscriber 消费）。
func (p *OrderPool) Orders() <-chan *PooledOrder { return p.ch }

// Ciphertext 按 OrderId 查 record ciphertext（settlement 结算用）。
func (p *OrderPool) Ciphertext(orderID int64) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ct, ok := p.ciphertexts[orderID]
	return ct, ok
}

// Complete 撮合完成，从待撮合队列移除（ciphertext 保留给结算）。
func (p *OrderPool) Complete(orderID uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.pending, orderID)
}

// HandleOrder 订单接收 API（Phase 2b 链下订单通道）：
// POST /order，JSON 含订单明文字段 + Order record ciphertext（用户执行 place_order 后提交）。
func HandleOrder(pool *OrderPool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			OrderId    int64  `json:"order_id"`
			Symbol     string `json:"symbol"` // 交易对（ETH_USDT），可选；缺省时记录为空
			Side       int    `json:"side"`  // 0=buy, 1=sell
			Price      string `json:"price"`
			Amount     string `json:"amount"`
			BaseToken  uint32 `json:"base_token"`
			QuoteToken uint32 `json:"quote_token"`
			Deadline   int64  `json:"deadline"`
			Trader     string `json:"trader"`
			Ciphertext string `json:"ciphertext"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		price, err := decimal.NewFromString(req.Price)
		if err != nil || price.Sign() <= 0 {
			http.Error(w, "invalid price", http.StatusBadRequest)
			return
		}
		amount, err := decimal.NewFromString(req.Amount)
		if err != nil || amount.Sign() <= 0 {
			http.Error(w, "invalid amount", http.StatusBadRequest)
			return
		}
		bs := match.Buy
		if req.Side == 1 {
			bs = match.Sell
		}
		o := &match.Order{
			SeqId:          req.OrderId,
			OrderId:        req.OrderId,
			Symbol:         strings.TrimSpace(req.Symbol), // 交易对路由（多交易对必需）
			UserAddress:    req.Trader,
			BuyOrSell:      bs,
			Type:           match.Limit,
			State:          match.Submitted,
			Price:          price,
			UnfilledAmount: amount,
			CreateAt:       time.Now().UnixMilli(),
			Deadline:       req.Deadline,
		}
		if err := pool.Submit(&PooledOrder{Order: o, Ciphertext: req.Ciphertext}); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// P3：订单状态记录（委托列表数据源）
		pool.RecordSubmit(o, strings.TrimSpace(req.Symbol))
		common.Info("aleo order accepted", "order_id", req.OrderId, "side", bs, "price", price.String(), "amount", amount.String())
		w.Write([]byte("ok"))
	}
}
