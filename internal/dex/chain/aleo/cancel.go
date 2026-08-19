package aleo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"github.com/shopspring/decimal"
)

// HandleOrderCancel POST /order/cancel 链上撤单（p6 四变体路由）：
//   - 公开订单（Mode=public）：cancel_buy_public / cancel_sell_public
//     （operator 或下单用户签名，公开余额全额退回用户）
//   - 隐私订单（Mode=private）：cancel_buy_private / cancel_sell_private
//     （operator 用 Order record + 托管 record 全额退回用户）
//
// 只允许未成交（waiting）订单撤销：公开/隐私撤单都退回全额托管（price*amount 或 amount），
// partial 订单已成交部分经 settle 转出、剩余托管 < 全额，撤销会超退——部分成交的剩余
// 处理不在当前范围（设计文档 §6）。
// 请求：{order_id}。
func HandleOrderCancel(pool *OrderPool, s *Settlement) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			OrderId int64 `json:"order_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderId <= 0 {
			http.Error(w, "invalid order_id", http.StatusBadRequest)
			return
		}

		// 校验订单状态（只能撤未成交的订单；partial 撤销会超退托管，见函数注释）
		pool.mu.Lock()
		rec, ok := pool.records[req.OrderId]
		pool.mu.Unlock()
		if !ok {
			http.Error(w, "order not found", http.StatusNotFound)
			return
		}
		if rec.Status != OrderStatusWaiting {
			http.Error(w, "order not cancellable (status: "+rec.Status+")", http.StatusBadRequest)
			return
		}

		// 按 Mode 路由撤单 transition（pool 里保留下单时的托管资产/凭证）
		po, ok := pool.GetOrder(req.OrderId)
		if !ok {
			http.Error(w, "order pool record not found", http.StatusNotFound)
			return
		}
		if err := executeCancel(s, po, rec); err != nil {
			common.Error("aleo cancel failed", "order", req.OrderId, "err", err)
			http.Error(w, "链上撤单失败: "+err.Error(), http.StatusBadGateway)
			return
		}

		// 构造 Cancel 订单进撮合队列：matcher 的 matchCancel 会从订单簿移除该订单
		// 并触发深度刷新（深度广播从订单簿生成，撤单必须同步移除，否则已撤订单仍显示在订单簿）
		cancelOrder := &match.Order{
			OrderId:  req.OrderId,
			Symbol:   rec.Symbol,
			Type:     match.Cancel,
			State:    match.Submitted,
			CreateAt: common.TimestampNowMs(),
		}
		if err := pool.Submit(&PooledOrder{Order: cancelOrder, Ciphertext: po.Ciphertext, Mode: po.Mode}); err != nil {
			common.Error("aleo cancel_order: submit to matcher failed", "order", req.OrderId, "err", err)
			http.Error(w, "撤单入队失败: "+err.Error(), http.StatusInternalServerError)
			return
		}

		common.Info("aleo cancel ok", "order", req.OrderId, "mode", po.Mode)
		w.Write([]byte("ok"))
	}
}

// executeCancel 按订单 Mode 路由链上撤单 transition（operator 私钥签名执行）。
func executeCancel(s *Settlement, po *PooledOrder, rec *OrderRecord) error {
	operator := config.GetString("chain.aleo.address", "")
	switch {
	case po.Mode == "public" && po.Order.BuyOrSell == match.Buy:
		// cancel_buy_public(order_id, user, operator, needed)：USDCX 公开退回。
		// 两阶段：await 外部 transfer_public 在 testnet 不可行——先由 operator 直调
		// transfer_public(user, needed) 退回，再调合约标记 status=2
		price, _ := decimal.NewFromString(rec.Price)
		amount, _ := decimal.NewFromString(rec.Amount)
		needed := price.Mul(amount).Div(decimal.New(1000000, 0)).IntPart()
		if err := s.executeTransfer("test_usdcx_stablecoin.aleo::transfer_public", []string{
			rec.Trader,
			strconv.FormatInt(needed, 10) + "u128",
		}, 0, 0); err != nil {
			return fmt.Errorf("cancel_buy_public 前置 USDCX 退回: %w", err)
		}
		_, err := s.executeTransition("cancel_buy_public", []string{
			strconv.FormatInt(rec.OrderId, 10) + "u128",
			rec.Trader,
			operator,
			strconv.FormatInt(needed, 10) + "u128",
		}, 0, 0)
		return err
	case po.Mode == "public" && po.Order.BuyOrSell == match.Sell:
		// cancel_sell_public(order_id, user, operator, amount)：ALEO 公开退回（两阶段）
		amount, _ := decimal.NewFromString(rec.Amount)
		if _, err := s.executeTransition("credits.aleo::transfer_public", []string{
			rec.Trader,
			strconv.FormatInt(amount.IntPart(), 10) + "u64",
		}, 0, 0); err != nil {
			return fmt.Errorf("cancel_sell_public 前置 credits 退回: %w", err)
		}
		_, err := s.executeTransition("cancel_sell_public", []string{
			strconv.FormatInt(rec.OrderId, 10) + "u128",
			rec.Trader,
			operator,
			strconv.FormatInt(amount.IntPart(), 10) + "u64",
		}, 0, 0)
		return err
	case po.Mode == "private" && po.Order.BuyOrSell == match.Buy:
		// cancel_buy_private(OrderCT, op_usdcx, creds)：USDCX record 退回用户
		if po.Ciphertext == "" || po.OpFund == "" {
			return fmt.Errorf("隐私买单缺少结算材料（Order CT/托管 Token）")
		}
		creds := s.currentCredentials()
		if creds == "" {
			return fmt.Errorf("operator-credentials 未配置（需 operator 先领凭证）")
		}
		out, err := s.executeTransition("cancel_buy_private", []string{
			po.Ciphertext,
			po.OpFund,
			creds,
		}, 0, 0)
		if err != nil {
			return err
		}
		// cancel_buy_private 消费 operator 凭证（transfer_private_with_creds），成功后轮换
		s.rotateCredentials(out)
		return nil
	case po.Mode == "private" && po.Order.BuyOrSell == match.Sell:
		// cancel_sell_private(OrderCT, op_aloe)：ALEO record 退回用户
		if po.Ciphertext == "" || po.OpFund == "" {
			return fmt.Errorf("隐私卖单缺少结算材料（Order CT/托管 credits）")
		}
		_, err := s.executeTransition("cancel_sell_private", []string{
			po.Ciphertext,
			po.OpFund,
		}, 0, 0)
		return err
	default:
		return fmt.Errorf("unknown order mode: %q", po.Mode)
	}
}
