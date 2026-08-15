package aleo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/AnuBookDEX/engine/internal/infra/common"
)

// HandlePrivacyOrder POST /order/privacy 隐私下单（Aleo 隐私特性）：
// 前端不发送明文订单，仅提交链上交易 ID；引擎从链上交易提取 + operator view key
// 解密 Order/托管资产/凭证 record（leo account decrypt）后送入撮合。
//
// 请求：{tx_id, symbol, trader} —— 订单明文字段（价格/数量等）不经过 HTTP，
// 仅存在于链上加密 record 与 operator 本地解密结果中。
func HandlePrivacyOrder(pool *OrderPool, rpc *RESTClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			TxID   string `json:"tx_id"`
			Symbol string `json:"symbol"`
			Trader string `json:"trader"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.TxID == "" {
			http.Error(w, "missing tx_id", http.StatusBadRequest)
			return
		}

		// 链上提取 + view key 解密（Order 明文撮合；OrderCT/OpFund/Creds 供结算）
		payload, err := ExtractAndDecryptOrder(rpc, req.TxID, programIDFor(req.Symbol))
		if err != nil {
			http.Error(w, "提取订单失败: "+err.Error(), http.StatusBadGateway)
			return
		}
		o := payload.Order
		o.Symbol = strings.TrimSpace(req.Symbol)
		o.UserAddress = strings.TrimSpace(req.Trader)
		if err := pool.Submit(&PooledOrder{Order: o, Ciphertext: payload.OrderCT, OpFund: payload.OpFund, Creds: payload.Creds}); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		pool.RecordSubmit(o, strings.TrimSpace(req.Symbol))
		common.Info("aleo privacy order accepted", "order_id", o.OrderId, "side", o.BuyOrSell,
			"price", o.Price.String(), "amount", o.UnfilledAmount.String(), "tx", req.TxID[:16])
		w.Write([]byte("ok"))
	}
}

// decryptRecord 用 view key 解密 record ciphertext（leo account decrypt）
func decryptRecord(ciphertext, viewKey string) (string, error) {
	cmd := exec.Command("leo", "account", "decrypt", "-c", ciphertext, "-k", viewKey, "-q", "--disable-update-check")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("leo decrypt: %w: %s", err, truncateBytes(out, 200))
	}
	return string(out), nil
}

// parseRecordPlaintext 解析 leo decrypt 输出（{ field: valueu64.private, ... }）
func parseRecordPlaintext(plain string) map[string]int64 {
	re := regexp.MustCompile(`(\w+):\s*([0-9]+)u(?:64|128|32|8)\.`)
	fields := make(map[string]int64)
	for _, m := range re.FindAllStringSubmatch(plain, -1) {
		if v, err := strconv.ParseInt(m[2], 10, 64); err == nil {
			fields[m[1]] = v
		}
	}
	return fields
}

func truncateBytes(b []byte, n int) string {
	s := string(b)
	if len(s) > n {
		return s[:n]
	}
	return s
}
