package aleo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/AnuBookDEX/engine/internal/infra/common"
)

// HandleOrderTx GET /order/tx/{tx_id} 交易查询代理：
// 用户钱包广播 place_order transition 后，前端用 tx_id 换取 Order record ciphertext
// （snarkOS: GET /testnet/transaction/{id}，遍历 execution.transitions[].outputs 找 record）。
func HandleOrderTx(rpc *RESTClient, programID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		txID := strings.TrimPrefix(r.URL.Path, "/order/tx/")
		txID = strings.TrimSpace(txID)
		if txID == "" || strings.Contains(txID, "/") {
			http.Error(w, "bad tx id", http.StatusBadRequest)
			return
		}
		if rpc == nil {
			http.Error(w, "rpc not configured", http.StatusServiceUnavailable)
			return
		}
		tx, err := rpc.GetTransaction(txID)
		if err != nil {
			http.Error(w, "query tx failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		ct, err := extractRecordCiphertext(tx, programID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"ciphertext": ct})
	}
}

// HandleBalance GET /api/v1/balance/{address} 链上 ALEO 公开余额查询：
// snarkOS credits.aleo account mapping（public balance），返回 ALEO 数量（1 ALEO = 1e6 microcredits）
func HandleBalance(rpc *RESTClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		addr := strings.TrimPrefix(r.URL.Path, "/api/v1/balance/")
		addr = strings.TrimSpace(addr)
		if addr == "" || strings.Contains(addr, "/") {
			http.Error(w, "bad address", http.StatusBadRequest)
			return
		}
		if rpc == nil {
			http.Error(w, "rpc not configured", http.StatusServiceUnavailable)
			return
		}
		plain, err := rpc.GetProgramMapping("credits.aleo", "account", addr)
		if err != nil {
			// 地址无记录（未激活账户）视为 0，不报错
			common.Debug("aleo balance: mapping query failed (may be zero balance)", addr, err)
			writeBalance(w, 0)
			return
		}
		// mapping 值格式：{ microcredits: 123456u64 }
		micro := parseMicrocredits(plain)
		writeBalance(w, micro)
	}
}

func writeBalance(w http.ResponseWriter, microcredits uint64) {
	w.Header().Set("Content-Type", "application/json")
	// 1 ALEO = 1,000,000 microcredits
	aleo := float64(microcredits) / 1e6
	json.NewEncoder(w).Encode(map[string]interface{}{
		"aleo":         aleo,
		"microcredits": microcredits,
	})
}

// parseMicrocredits 从 mapping plaintext 提取 microcredits。
// snarkOS 实际响应格式为裸值："45685966u64"（带引号 + u64 类型后缀）；
// 兼容较旧的 { microcredits: 123u64 } 包装格式。
func parseMicrocredits(plain string) uint64 {
	// 裸值格式："45685966u64"
	re := regexp.MustCompile(`"([0-9]+)u64"`)
	if m := re.FindStringSubmatch(plain); len(m) >= 2 {
		v, _ := strconv.ParseUint(m[1], 10, 64)
		return v
	}
	// 兼容 { microcredits: 123u64 } 格式
	re2 := regexp.MustCompile(`microcredits:\s*([0-9]+)u64`)
	if m := re2.FindStringSubmatch(plain); len(m) >= 2 {
		v, _ := strconv.ParseUint(m[1], 10, 64)
		return v
	}
	return 0
}

// extractRecordCiphertext 从交易回执中提取指定 program 的 record ciphertext。
// snarkOS 响应结构：{ execution: { transitions: [ { program_id, outputs: [ { type: "record", value: "ciphertext1..." } ] } ] } }
func extractRecordCiphertext(tx map[string]interface{}, programID string) (string, error) {
	execution, ok := tx["execution"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("tx has no execution")
	}
	transitions, ok := execution["transitions"].([]interface{})
	if !ok {
		return "", fmt.Errorf("tx has no transitions")
	}
	for _, t := range transitions {
		tr, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		// 新版 snarkOS transition 字段为 program，旧版为 program_id（兼容两者）
		pid, _ := tr["program"].(string)
		if pid == "" {
			pid, _ = tr["program_id"].(string)
		}
		if programID != "" && pid != programID {
			continue
		}
		outputs, _ := tr["outputs"].([]interface{})
		for _, o := range outputs {
			om, ok := o.(map[string]interface{})
			if !ok {
				continue
			}
			if om["type"] == "record" {
				if v, ok := om["value"].(string); ok &&
					(strings.HasPrefix(v, "record1") || strings.HasPrefix(v, "ciphertext1")) {
					// record1 = 新格式 record 密文；ciphertext1 = 旧格式（兼容）
					return v, nil
				}
			}
		}
	}
	return "", fmt.Errorf("record ciphertext not found in tx")
}
