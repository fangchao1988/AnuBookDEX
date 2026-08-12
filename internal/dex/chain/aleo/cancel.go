package aleo

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
)

// HandleOrderCancel POST /order/cancel 链上撤单：
// operator 用 Order record 密文执行 cancel_order transition（返还锁定资产）。
// 请求：{order_id} —— 订单必须处于 waiting/partial（未完全成交）。
func HandleOrderCancel(pool *OrderPool) http.HandlerFunc {
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

		// 校验订单状态（只能撤未完全成交的订单）
		pool.mu.Lock()
		rec, ok := pool.records[req.OrderId]
		pool.mu.Unlock()
		if !ok {
			http.Error(w, "order not found", http.StatusNotFound)
			return
		}
		if rec.Status != OrderStatusWaiting && rec.Status != OrderStatusPartial {
			http.Error(w, "order not cancellable (status: "+rec.Status+")", http.StatusBadRequest)
			return
		}

		// 取 Order record 密文
		ct, ok := pool.Ciphertext(req.OrderId)
		if !ok {
			http.Error(w, "order ciphertext not found", http.StatusNotFound)
			return
		}

		// operator 私钥（config -> ALEO_PRIVATE_KEY）
		priv := config.GetString("chain.aleo.private-key", "")
		if priv == "" {
			priv = os.Getenv("ALEO_PRIVATE_KEY")
		}
		if priv == "" {
			http.Error(w, "ALEO_PRIVATE_KEY 未配置（operator 撤单需签名）", http.StatusInternalServerError)
			return
		}

		// leo execute cancel_order（--no-local 用链上版本，显式私钥绕过 .env bug）
		args := []string{
			"execute", "cancel_order", ct,
			"--broadcast",
			"--endpoint", config.GetString("chain.aleo.rpc-endpoint", "https://api.explorer.provable.com/v1"),
			"--network", config.GetString("chain.aleo.network", "testnet"),
			"--yes",
			"--no-local",
			"--private-key", priv,
		}
		cmd := exec.Command("leo", args...)
		cmd.Dir = config.GetString("chain.aleo.program-dir", "./contracts/leo/")
		out, err := cmd.CombinedOutput()
		if err != nil {
			common.Error("aleo cancel_order failed", "order", req.OrderId, "err", err, "out", truncateBytes(out, 300))
			http.Error(w, "链上撤单失败: "+truncateBytes(out, 300), http.StatusBadGateway)
			return
		}

		// 构造 Cancel 订单进撮合队列：matcher 的 matchCancel 会从订单簿移除该订单
		// 并触发深度刷新（深度广播从订单簿生成，撤单必须同步移除，否则已撤订单仍显示在订单簿）
		cancelOrder := &match.Order{
			OrderId: req.OrderId,
			Symbol:  rec.Symbol,
			Type:    match.Cancel,
			State:   match.Submitted,
			CreateAt: common.TimestampNowMs(),
		}
		if err := pool.Submit(&PooledOrder{Order: cancelOrder, Ciphertext: ct}); err != nil {
			common.Error("aleo cancel_order: submit to matcher failed", "order", req.OrderId, "err", err)
			http.Error(w, "撤单入队失败: "+err.Error(), http.StatusInternalServerError)
			return
		}

		common.Info("aleo cancel_order ok", "order", req.OrderId)
		w.Write([]byte("ok"))
	}
}
