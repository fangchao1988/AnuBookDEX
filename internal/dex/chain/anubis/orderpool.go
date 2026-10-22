package anubis

// orderpool.go: Anubis 链下订单通道（镜像 aleo 的 OrderPool 模式）。
// 前端 EVM 钱包经 POST /order 提交订单：
//   - mode=public ：明文字段直传（标准下单，MVP 主路径）
//   - mode=private：ECIES 加密订单（privacy.EncryptedOrder，引擎用 view-key 解密）+ 可选签名
// 引擎入池后由 Subscriber 按 symbol 路由到撮合；结算走 Settlement v2（链上 submitBatch）。
// 与 Aleo 不同：无 record/凭证托管（Anubis 侧结算由合约事件记账），故 PooledOrder 更轻。

import (
	"context"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/dex/privacy"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"github.com/shopspring/decimal"
)

// PooledOrder 链下订单（Anubis 版）。
// Order 明文用于撮合；Mode 标识下单路径；Ciphertext 为前端 ECIES 密文（mode=private 时）；
// Signature 为前端 personal_sign（可选校验）；Nullifier 防重放。
type PooledOrder struct {
	Order      *match.Order `json:"order"`
	Mode       string       `json:"mode"` // "public" | "private"
	Ciphertext string       `json:"ciphertext"`
	Signature  string       `json:"signature"`
	Nullifier  []byte       `json:"nullifier"`
}

// OrderRecord 委托记录（/api/v1/orders 数据源）。
type OrderRecord struct {
	OrderId   int64  `json:"order_id"`
	Symbol    string `json:"symbol"`
	Side      int    `json:"side"` // 0=buy, 1=sell
	Price     string `json:"price"`
	Amount    string `json:"amount"`
	Trader    string `json:"trader"`
	Mode      string `json:"mode"`
	State     string `json:"state"` // submitted | matched | canceled
	TxHash    string `json:"tx_hash,omitempty"`
	CreateAt  int64  `json:"create_at"`
	Deadline  int64  `json:"deadline"`
	Nullifier string `json:"nullifier,omitempty"`
}

// TradeRecord 成交记录（/api/v1/trades 数据源，由 Settlement.SubmitBatch 追加）。
type TradeRecord struct {
	OrderId int64  `json:"order_id"` // taker 订单号（前端展示用）
	Symbol  string `json:"symbol"`
	Side    int    `json:"side"` // taker 方向：0=buy, 1=sell
	Price   string `json:"price"`
	Amount  string `json:"amount"`
	// 买卖双方地址：一笔成交有两位参与者，前端据此判断「我的方向」与对手方。
	// 缺少这两个字段时前端无法区分同一条成交的双方（曾导致同一条显示两遍）。
	Buyer  string `json:"buyer"`
	Seller string `json:"seller"`
	Ts     int64  `json:"ts"`
	Settle string `json:"settle_status"` // queued | sent | confirmed
	TxHash string `json:"tx_hash,omitempty"`
}

// OrderPool 链下订单池：HandleOrder 写入，Subscriber 消费，Settlement 追加成交。
type OrderPool struct {
	mu      sync.Mutex
	pending map[uint64]*PooledOrder // order_id -> 待撮合
	orders  map[int64]*OrderRecord  // order_id -> 委托记录
	trades  []*TradeRecord          // 成交明细
	ch      chan *PooledOrder       // 待撮合队列
	nullifs map[string]bool         // nullifier 去重
}

// NewOrderPool 创建订单池。
func NewOrderPool() *OrderPool {
	return &OrderPool{
		pending: make(map[uint64]*PooledOrder),
		orders:  make(map[int64]*OrderRecord),
		trades:  make([]*TradeRecord, 0, 1024),
		ch:      make(chan *PooledOrder, 5000),
		nullifs: make(map[string]bool),
	}
}

// Submit 提交订单进待撮合队列（nullifier 去重）。
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
	if len(o.Nullifier) > 0 {
		key := string(o.Nullifier)
		if p.nullifs[key] {
			return fmt.Errorf("duplicate nullifier")
		}
		p.nullifs[key] = true
	}
	p.pending[id] = o
	p.orders[o.Order.OrderId] = &OrderRecord{
		OrderId:   o.Order.OrderId,
		Symbol:    o.Order.Symbol,
		Side:      int(o.Order.BuyOrSell),
		Price:     o.Order.Price.String(),
		Amount:    o.Order.UnfilledAmount.String(),
		Trader:    o.Order.UserAddress,
		Mode:      o.Mode,
		State:     "submitted",
		CreateAt:  o.Order.CreateAt,
		Deadline:  o.Order.Deadline,
		Nullifier: hex.EncodeToString(o.Nullifier),
	}
	select {
	case p.ch <- o:
	default:
		delete(p.pending, id)
		return fmt.Errorf("order pool full")
	}
	return nil
}

// GetOrder 按 OrderId 查订单。
func (p *OrderPool) GetOrder(orderID int64) (*PooledOrder, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	o, ok := p.pending[uint64(orderID)]
	return o, ok
}

// Orders 返回待撮合队列（Subscriber 消费）。
func (p *OrderPool) Orders() <-chan *PooledOrder { return p.ch }

// Record 按 order id 查委托记录（settlement 解析收款地址用；Complete 后仍可查）。
func (p *OrderPool) Record(orderID int64) *OrderRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.orders[orderID]
}

// Complete 撮合完成：待撮合队列移除，委托记录置已撮合。
func (p *OrderPool) Complete(orderID uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.pending[orderID]; ok {
		if rec, ok2 := p.orders[int64(orderID)]; ok2 {
			rec.State = "matched"
		}
		delete(p.pending, orderID)
	}
}

// Cancel 本地撤单：待撮合队列移除，委托记录置 canceled（MVP 链下撤单）。
func (p *OrderPool) Cancel(orderID int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if rec, ok := p.orders[orderID]; ok {
		if rec.State == "matched" {
			return fmt.Errorf("order %d already matched", orderID)
		}
		rec.State = "canceled"
		delete(p.pending, uint64(orderID))
		return nil
	}
	return fmt.Errorf("order %d not found", orderID)
}

// AppendTrades 由 Settlement.SubmitBatch 写入成交明细（settle queued）。
// 一笔成交只记一条（含买卖双方地址）——
// 旧实现遍历 mr.Items 逐个记录，maker/taker 各一条，导致同一笔成交在成交流水中出现两次。
func (p *OrderPool) AppendTrades(mrs []*match.MatchResult) {
	p.mu.Lock()
	defer p.mu.Unlock()

	addrOf := func(id int64) string {
		if rec, ok := p.orders[id]; ok {
			return rec.Trader
		}
		return ""
	}

	for _, mr := range mrs {
		var makerID, takerID int64
		var filled *decimal.Decimal
		for _, item := range mr.Items {
			// 未成交条目（挂单到达但无对手方）不计入成交
			if item.State != "filled" && item.State != "partial-filled" {
				continue
			}
			switch item.Role {
			case "maker":
				makerID = item.OrderId
			case "taker":
				takerID = item.OrderId
			}
			// maker 记 FilledAmount；taker 记 AccFilledAmount（其 FilledAmount 为空）
			if filled == nil {
				if item.FilledAmount != nil {
					filled = item.FilledAmount
				} else if item.AccFilledAmount != nil {
					filled = item.AccFilledAmount
				}
			}
		}
		if filled == nil || filled.Sign() <= 0 {
			continue
		}

		makerAddr, takerAddr := addrOf(makerID), addrOf(takerID)
		// mr.OrderTypeStr 是 **taker** 的方向：
		//   taker 买入 → 买方=taker，卖方=maker
		//   taker 卖出 → 买方=maker，卖方=taker
		// 注意：OrderTypeStr 形如 "sell-limit" / "buy-market"（含订单类型后缀），
		// 不能用等值比较，须按前缀判断方向
		side := 0
		buyer, seller := takerAddr, makerAddr
		if strings.HasPrefix(mr.OrderTypeStr, "sell") {
			side = 1
			buyer, seller = makerAddr, takerAddr
		}

		p.trades = append(p.trades, &TradeRecord{
			OrderId: takerID,
			Symbol:  mr.Symbol,
			Side:    side,
			Price:   mr.Price.String(),
			Amount:  filled.String(),
			Buyer:   buyer,
			Seller:  seller,
			Ts:      mr.Ts,
			Settle:  "queued",
		})
	}
}

// MarkTradesSettled 结算成功后更新最近成交流水（by batch symbol/txhash）。
func (p *OrderPool) MarkTradesSettled(symbol, txHash string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.trades) - 1; i >= 0 && i > len(p.trades)-64; i-- {
		// 仅在回执 status==1（已上链确认）时调用，故置为 confirmed 而非 sent
		if p.trades[i].Symbol == symbol && p.trades[i].Settle == "queued" {
			p.trades[i].Settle = "confirmed"
			p.trades[i].TxHash = txHash
		}
	}
}

// ListOrders 委托列表副本（trader/symbol 可选过滤）。
func (p *OrderPool) ListOrders(trader, symbol string, limit int) []*OrderRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*OrderRecord, 0, len(p.orders))
	for _, rec := range p.orders {
		if trader != "" && !strings.EqualFold(rec.Trader, trader) {
			continue
		}
		if symbol != "" && !strings.EqualFold(rec.Symbol, symbol) {
			continue
		}
		out = append(out, rec)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// ListTrades 成交明细副本（trader/symbol/limit 过滤由调用方做，这里按 symbol）。
func (p *OrderPool) ListTrades(symbol string, limit int) []*TradeRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	if limit <= 0 || limit > len(p.trades) {
		limit = len(p.trades)
	}
	out := make([]*TradeRecord, 0, limit)
	for i := len(p.trades) - 1; i >= 0 && len(out) < limit; i-- {
		if symbol != "" && !strings.EqualFold(p.trades[i].Symbol, symbol) {
			continue
		}
		out = append(out, p.trades[i])
	}
	return out
}

// ---- HTTP 处理器 -------------------------------------------------

// orderSubmitReq POST /order 请求体。
type orderSubmitReq struct {
	OrderId    int64  `json:"order_id"`
	Symbol     string `json:"symbol"`
	Side       int    `json:"side"`
	Price      string `json:"price"`
	Amount     string `json:"amount"`
	Deadline   int64  `json:"deadline"`
	Trader     string `json:"trader"`
	Mode       string `json:"mode"`
	Ciphertext string `json:"ciphertext"`
	Signature  string `json:"signature"`
	Nullifier  string `json:"nullifier"`
}

// HandleOrder POST /order 链下订单接收。
// JSON：{order_id, symbol, side(0/1), price, amount, deadline, trader, mode, ciphertext, signature, nullifier}
//   - mode=private：引擎可用 view-key 解密 EncryptedOrder（若无 view-key 则拒绝并提示）
//   - mode=public ：明文直传
func HandleOrder(pool *OrderPool, viewKey *privacy.ViewKey, settl *Settlement) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req orderSubmitReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		mode := strings.TrimSpace(req.Mode)
		if mode == "" {
			mode = "public"
		}

		o, mode, err := buildOrder(req, mode, viewKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// 金库余额预检：下单即校验，避免撮合后结算才失败
		// （合约结算要求付款方金库余额充足，否则整批 revert；
		//   此前无资金挂单会一直卡住结算，且错误信息要到链上才可见）
		if err := checkVaultFunds(settl, o); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		nul, _ := hex.DecodeString(strings.TrimPrefix(req.Nullifier, "0x"))
		if err := pool.Submit(&PooledOrder{Order: o, Mode: mode, Ciphertext: req.Ciphertext,
			Signature: req.Signature, Nullifier: nul}); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		common.Info("anubis order accepted", "order_id", req.OrderId, "symbol", o.Symbol,
			"side", o.BuyOrSell, "mode", mode, "price", o.Price.String(), "amount", o.UnfilledAmount.String())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "order_id": req.OrderId})
	}
}

// checkVaultFunds 校验下单方在金库中是否有足够资金完成该单。
//   买单：需 quote 代币 ≥ price × amount
//   卖单：需 base  代币 ≥ amount
// 返回 nil 表示通过；未配置代币/结算合约时跳过（无法链上结算，交由结算层告警）。
func checkVaultFunds(settl *Settlement, o *match.Order) error {
	if settl == nil {
		return nil
	}
	base, quote, ok := pairTokensFor(o.Symbol)
	if !ok {
		return nil // 该币对未配置代币，不做金库结算
	}
	token, need, label := base, o.UnfilledAmount, "base"
	if o.BuyOrSell == match.Buy {
		token, label = quote, "quote"
		need = o.Price.Mul(o.UnfilledAmount)
	}
	bal, ok := settl.VaultBalance(token, o.UserAddress)
	if !ok {
		return nil // 查询失败不阻断（结算层仍会拦截）
	}
	if bal.Cmp(need.BigInt()) < 0 {
		return fmt.Errorf("金库余额不足：%s 需 %s，当前 %s（请先在持仓面板入金）",
			label, need.String(), bal.String())
	}
	return nil
}

// buildOrder 从请求构造 match.Order。
// mode=private 且带 ciphertext 时用 view-key 解密订单字段（EncryptedOrder → 明文 JSON）。
func buildOrder(req orderSubmitReq, mode string, viewKey *privacy.ViewKey) (*match.Order, string, error) {
	// private 模式：优先解密 ciphertext（ECIES EncryptedOrder JSON → 订单字段）
	if mode == "private" && strings.TrimSpace(req.Ciphertext) != "" {
		if viewKey == nil {
			return nil, mode, fmt.Errorf("mode=private requires engine view key (chain.anubis.view-key-private)")
		}
		ctHex := strings.TrimPrefix(strings.TrimSpace(req.Ciphertext), "0x")
		raw, err := hex.DecodeString(ctHex)
		if err != nil {
			return nil, mode, fmt.Errorf("ciphertext not hex: %w", err)
		}
		var eo privacy.EncryptedOrder
		if err := json.Unmarshal(raw, &eo); err != nil {
			return nil, mode, fmt.Errorf("ciphertext not EncryptedOrder JSON: %w", err)
		}
		order, err := privacy.DecryptOrderFromNote(&eo, viewKey)
		if err != nil {
			return nil, mode, fmt.Errorf("decrypt order: %w", err)
		}
		order.OrderId = req.OrderId
		// SeqId 必须设置：撮合器用它做价格-时间优先的先后判定
		//（matcher.go Matchable: order.SeqId <= oppoOrder.SeqId 直接拒绝撮合）。
		// 隐私分支若不设则恒为 0，订单永远无法成交（表现为结算 totalVolume=0）。
		order.SeqId = req.OrderId
		order.Symbol = strings.TrimSpace(req.Symbol)
		order.UserAddress = strings.TrimSpace(req.Trader)
		order.State = match.Submitted
		order.CreateAt = time.Now().UnixMilli()
		order.Deadline = eo.Deadline
		return order, mode, nil
	}

	// public / 明文路径：直接校验字段
	price, err := decimal.NewFromString(req.Price)
	if err != nil || price.Sign() <= 0 {
		return nil, mode, fmt.Errorf("invalid price")
	}
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || amount.Sign() <= 0 {
		return nil, mode, fmt.Errorf("invalid amount")
	}
	bs := match.Buy
	if req.Side == 1 {
		bs = match.Sell
	}
	o := &match.Order{
		SeqId:          req.OrderId,
		OrderId:        req.OrderId,
		Symbol:         strings.TrimSpace(req.Symbol),
		UserAddress:    strings.TrimSpace(req.Trader),
		BuyOrSell:      bs,
		Type:           match.Limit,
		State:          match.Submitted,
		Price:          price,
		UnfilledAmount: amount,
		CreateAt:       time.Now().UnixMilli(),
		Deadline:       req.Deadline,
	}
	return o, mode, nil
}

// HandleCancel POST /order/cancel 本地撤单（MVP 链下）。
func HandleCancel(pool *OrderPool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			OrderId int64 `json:"order_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if err := pool.Cancel(req.OrderId); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Write([]byte("ok"))
	}
}

// HandleListOrders GET /api/v1/orders?trader=&symbol=&limit=
func HandleListOrders(pool *OrderPool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit := 100
		if v := q.Get("limit"); v != "" {
			fmt.Sscanf(v, "%d", &limit)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pool.ListOrders(q.Get("trader"), q.Get("symbol"), limit))
	}
}

// HandleListTrades GET /api/v1/trades?symbol=&limit=
func HandleListTrades(pool *OrderPool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit := 100
		if v := q.Get("limit"); v != "" {
			fmt.Sscanf(v, "%d", &limit)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pool.ListTrades(q.Get("symbol"), limit))
	}
}

// TokenInfo 链上代币元信息。
type TokenInfo struct {
	Address  string `json:"address"`
	Symbol   string `json:"symbol"`
	Decimals uint8  `json:"decimals"`
}

// PairTokens 币对与其 base/quote 代币。
type PairTokens struct {
	Symbol string     `json:"symbol"`
	Base   *TokenInfo `json:"base"`
	Quote  *TokenInfo `json:"quote"`
}

// ChainTokens GET /api/v1/tokens 的响应体。
type ChainTokens struct {
	// Settlement 结算合约地址（前端 approve/deposit 的目标）
	Settlement string       `json:"settlement"`
	Pairs      []*PairTokens `json:"pairs"`
}

// HandlePairTokens GET /api/v1/tokens
// 返回结算合约地址 + 各币对的 base/quote 代币（前端入金/余额展示用）。
// 数据源：chain.anubis.settlement-contract / pair-tokens.<SYMBOL>.{base,quote}；
// 未配置代币的币对不返回（该币对无法链上结算）。
func HandlePairTokens(evm *EVMClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		symbols := config.GetStringSlice("symbols", nil)
		pairs := make([]*PairTokens, 0, len(symbols))
		for _, sym := range symbols {
			b := config.GetString("chain.anubis.pair-tokens."+sym+".base", "")
			q := config.GetString("chain.anubis.pair-tokens."+sym+".quote", "")
			if b == "" || q == "" {
				continue
			}
			pairs = append(pairs, &PairTokens{
				Symbol: sym,
				Base:   describeToken(evm, b),
				Quote:  describeToken(evm, q),
			})
		}
		json.NewEncoder(w).Encode(ChainTokens{
			Settlement: config.GetString("chain.anubis.settlement-contract", ""),
			Pairs:      pairs,
		})
	}
}

// describeToken 读链上 symbol()/decimals()；读不到时回落地址前缀与 18 位小数。
func describeToken(evm *EVMClient, addr string) *TokenInfo {
	info := &TokenInfo{Address: addr, Symbol: shortAddr(addr), Decimals: 18}
	if evm == nil {
		return info
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	to := ethcommon.HexToAddress(addr)
	// symbol() = 0x95d89b41 / decimals() = 0x313ce567（标准 ERC-20）
	if res, err := evm.Client.CallContract(ctx, ethereum.CallMsg{To: &to, Data: mustHex("95d89b41")}, nil); err == nil {
		if sym := decodeAbiString(res); sym != "" {
			info.Symbol = sym
		}
	}
	if res, err := evm.Client.CallContract(ctx, ethereum.CallMsg{To: &to, Data: mustHex("313ce567")}, nil); err == nil && len(res) >= 32 {
		info.Decimals = res[31]
	}
	return info
}

func shortAddr(a string) string {
	if len(a) > 10 {
		return a[:10] + "…"
	}
	return a
}

func mustHex(h string) []byte {
	b, _ := hex.DecodeString(h)
	return b
}

// decodeAbiString 解 ABI 编码的 string（offset + length + data）。
func decodeAbiString(b []byte) string {
	if len(b) < 64 {
		return ""
	}
	off := int(new(big.Int).SetBytes(b[:32]).Int64())
	if off < 0 || off+32 > len(b) {
		return ""
	}
	l := int(new(big.Int).SetBytes(b[off : off+32]).Int64())
	if l <= 0 || off+32+l > len(b) {
		return ""
	}
	return string(b[off+32 : off+32+l])
}

// HandleEciesPubkey GET /api/v1/privacy/pubkey
// 前端隐私下单需要引擎的 ECIES 公钥（P-256 压缩点）与 ViewTag：
// 公钥用于 ECDH 派生共享密钥，ViewTag 用于密文快速筛选。
// 未配置 view key 时返回空（前端降级为明文下单）。
func HandleEciesPubkey(vk *privacy.ViewKey) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if vk == nil || vk.PrivateKey == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"enabled": false})
			return
		}
		pub := vk.PrivateKey.PublicKey
		compressed := elliptic.MarshalCompressed(pub.Curve, pub.X, pub.Y)
		tag := vk.ViewTag
		if len(tag) != 4 {
			tagHash := sha256.Sum256(compressed)
			tag = tagHash[:4]
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enabled":    true,
			"pubkey":     "0x" + hex.EncodeToString(compressed),
			"curve":      "P-256",
			"view_tag":   "0x" + hex.EncodeToString(tag),
			"kdf":        "sha256(ECDH x-coordinate), AES-256-GCM",
			"commitment": "sha256(nullifier || ciphertext || ephemeral_pk)",
		})
	}
}