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

// unquoteJSON 剥离 GET /mapping 返回的 JSON string wrapper（外层引号 + \\n 字面转义，
// 例如 `"{\n  status: 1u8\n}"`），还原为真实换行的 Leo plaintext。
// 失败时原样返回（decrypt 输出本就是真实换行，无 wrapper）。
func unquoteJSON(s string) string {
	var out string
	if err := json.Unmarshal([]byte(s), &out); err == nil {
		return out
	}
	return s
}

// parseRecordPlaintext 解析 leo decrypt 输出（{ field: valueu64.private, ... }）与
// node mapping 明文（{ field: valueu64, ... }，无 .private 后缀）两种格式。
// 调用方须先用 unquoteJSON 还原 JSON wrapper（否则 `status: 1u8\n}` 的 `\` 终止不了字段）。
func parseRecordPlaintext(plain string) map[string]int64 {
	re := regexp.MustCompile(`(\w+):\s*([0-9]+)u(?:64|128|32|8)[.,}\s]`)
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
