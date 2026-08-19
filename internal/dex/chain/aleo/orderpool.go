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

// PooledOrder 链下提交的订单（Phase 2b 链下订单通道 + Phase 4 真实币对 + Phase 6 公开/隐私混合）：
// Order 明文用于撮合；Ciphertext 是链上 Order record（settle 消费）；
// OpFund 是下单产出、operator 托管的资产 record 明文（买单=USDCX Token，卖单=ALEO credits）；
// Creds 是 operator 的 USDCX 合规凭证（transfer_private_with_creds 用）；
// Mode 是下单路径：public（标准下单=公开余额托管，public_orders mapping，结算走 settle_pp/vp/pv）
// 或 private（隐私下单=record 托管，Order record 加密，结算走 settle_vv）。
type PooledOrder struct {
	Order      *match.Order `json:"order"`
	Ciphertext string       `json:"ciphertext"`  // Order record（链上，settle 输入）
	OpFund     string       `json:"op_fund"`     // operator 托管资产 record 明文
	Creds      string       `json:"credentials"` // operator USDCX 合规凭证 明文
	Mode       string       `json:"mode"`        // "public" | "private"（结算路由 + 撤单路由）
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
	orders      map[int64]*PooledOrder  // order_id -> 全量订单（含托管资产/凭证，settle 用）
}

// NewOrderPool 创建订单池。
func NewOrderPool() *OrderPool {
	return &OrderPool{
		pending:     make(map[uint64]*PooledOrder),
		ciphertexts: make(map[int64]string),
		ch:          make(chan *PooledOrder, 5000),
		records:     make(map[int64]*OrderRecord),
		trades:      make([]*TradeRecord, 0, 1024),
		orders:      make(map[int64]*PooledOrder),
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
	// （chain.aleo.require-ciphertext=false，前端先接订单通道、钱包后续接）。
	// 公开下单（mode=public）无 Order record（public_orders mapping 记账），跳过校验
	if o.Ciphertext == "" && o.Mode != "public" && config.GetBool("chain.aleo.require-ciphertext", true) {
		return fmt.Errorf("order %d missing ciphertext", o.Order.OrderId)
	}
	p.pending[id] = o
	p.ciphertexts[o.Order.OrderId] = o.Ciphertext
	p.orders[o.Order.OrderId] = o
	select {
	case p.ch <- o:
	default:
		delete(p.pending, id)
		return fmt.Errorf("order pool full")
	}
	return nil
}

// GetOrder 按 OrderId 查订单（含托管资产/凭证，settlement 结算用）。
func (p *OrderPool) GetOrder(orderID int64) (*PooledOrder, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	o, ok := p.orders[orderID]
	return o, ok
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

// HandleOrder 订单接收 API（Phase 2b 链下订单通道 + p4 真实币对）：
// POST /order，JSON 两种模式：
//   - tx_id 模式（ALEO/USDCX p4，标准/隐私下单共用）：只传 {tx_id, symbol, trader}，
//     引擎从链上交易提取 + view key 解密（ExtractAndDecryptOrder）
//   - 明文字段模式（ETH/USDT p2 铸币兼容）：订单明文字段 + Order record ciphertext
func HandleOrder(pool *OrderPool, rpc *RESTClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			TxID       string `json:"tx_id"` // 链上下单交易 id（p4/p6 隐私）；空则走明文字段模式
			OrderId    int64  `json:"order_id"`
			Symbol     string `json:"symbol"` // 交易对（ALEO_USDCX），可选；缺省时记录为空
			Side       int    `json:"side"`   // 0=buy, 1=sell
			Price      string `json:"price"`
			Amount     string `json:"amount"`
			BaseToken  uint32 `json:"base_token"`
			QuoteToken uint32 `json:"quote_token"`
			Deadline   int64  `json:"deadline"`
			Trader     string `json:"trader"`
			Mode       string `json:"mode"` // "public"（标准=公开余额托管）| "private"（隐私=record 托管，默认）
			Ciphertext string `json:"ciphertext"`  // Order record（settle 输入）
			OpFund     string `json:"op_fund"`     // operator 托管资产 record 明文（买单=USDCX Token，卖单=ALEO credits）
			Creds      string `json:"credentials"` // operator USDCX 合规凭证 明文
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}

		// tx_id 模式：链上提取 + 解密（隐私下单：前端只提交 tx_id，订单参数全部来自链上加密 record）
		if req.TxID != "" {
			payload, err := ExtractAndDecryptOrder(rpc, req.TxID, programIDFor(req.Symbol))
			if err != nil {
				http.Error(w, "提取订单失败: "+err.Error(), http.StatusBadGateway)
				return
			}
			o := payload.Order
			o.Symbol = strings.TrimSpace(req.Symbol)
			o.UserAddress = strings.TrimSpace(req.Trader)
			if err := pool.Submit(&PooledOrder{Order: o, Ciphertext: payload.OrderCT, OpFund: payload.OpFund, Creds: payload.Creds, Mode: "private"}); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			pool.RecordSubmit(o, strings.TrimSpace(req.Symbol))
			common.Info("aleo order accepted (tx_id)", "order_id", o.OrderId, "side", o.BuyOrSell,
				"price", o.Price.String(), "amount", o.UnfilledAmount.String(), "tx", req.TxID[:16])
			w.Write([]byte("ok"))
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
		mode := strings.TrimSpace(req.Mode)
		if mode == "" {
			mode = "private" // 默认隐私（p2/p5 兼容路径）
		}
		if err := pool.Submit(&PooledOrder{Order: o, Ciphertext: req.Ciphertext, OpFund: req.OpFund, Creds: req.Creds, Mode: mode}); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// P3：订单状态记录（委托列表数据源）
		pool.RecordSubmit(o, strings.TrimSpace(req.Symbol))
		common.Info("aleo order accepted", "order_id", req.OrderId, "side", bs, "price", price.String(), "amount", amount.String(), "mode", mode)
		w.Write([]byte("ok"))
	}
}
