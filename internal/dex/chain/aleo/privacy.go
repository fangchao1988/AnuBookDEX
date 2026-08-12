package aleo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"github.com/shopspring/decimal"
)

// HandlePrivacyOrder POST /order/privacy 隐私下单（Aleo 隐私特性）：
// 前端不发送明文订单，仅提交链上交易 ID；引擎用 operator view key 解密
// Order record（leo account decrypt）后送入撮合。
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

		// 1) 查链上交易，提取 Order record ciphertext
		tx, err := rpc.GetTransaction(req.TxID)
		if err != nil {
			http.Error(w, "query tx failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		ct, err := extractRecordCiphertext(tx, config.GetString("chain.aleo.program-id", "anubook_dex_p2.aleo"))
		if err != nil {
			http.Error(w, "提取 Order record 失败: "+err.Error(), http.StatusNotFound)
			return
		}

		// 2) operator view key 解密（leo CLI；本机 leo 可用，仅 account decrypt 不涉及 deploy bug）。
		// 读取顺序：配置文件 chain.aleo.view-key-private -> 环境变量 ALEO_VIEW_KEY（viper BindEnv
		// 对嵌套 key + 配置文件空值的组合不可靠，直接读 env 兜底）
		viewKey := config.GetString("chain.aleo.view-key-private", "")
		if viewKey == "" {
			viewKey = os.Getenv("ALEO_VIEW_KEY")
		}
		if viewKey == "" {
			http.Error(w, "view key 未配置（chain.aleo.view-key-private 或 ALEO_VIEW_KEY）", http.StatusInternalServerError)
			return
		}
		plain, err := decryptRecord(ct, viewKey)
		if err != nil {
			http.Error(w, "解密订单失败: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// 3) 解析订单明文字段
		fields := parseRecordPlaintext(plain)
		orderId := fields["order_id"]
		side := fields["side"]
		price := fields["price"]
		amount := fields["amount"]
		deadline := fields["deadline"]
		if orderId == 0 || price == 0 || amount == 0 {
			http.Error(w, "订单字段不完整", http.StatusBadRequest)
			return
		}

		bs := match.Buy
		if side == 1 {
			bs = match.Sell
		}
		o := &match.Order{
			SeqId:          orderId,
			OrderId:        orderId,
			Symbol:         strings.TrimSpace(req.Symbol),
			UserAddress:    strings.TrimSpace(req.Trader),
			BuyOrSell:      bs,
			Type:           match.Limit,
			State:          match.Submitted,
			Price:          decimal.NewFromInt(price),
			UnfilledAmount: decimal.NewFromInt(amount),
			CreateAt:       common.TimestampNowMs(),
			Deadline:       deadline,
		}
		if err := pool.Submit(&PooledOrder{Order: o, Ciphertext: ct}); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		pool.RecordSubmit(o, strings.TrimSpace(req.Symbol))
		common.Info("aleo privacy order accepted", "order_id", orderId, "side", bs, "price", price, "amount", amount, "tx", req.TxID[:16])
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
