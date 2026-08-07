package aleo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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
		pid, _ := tr["program_id"].(string)
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
				if v, ok := om["value"].(string); ok && strings.HasPrefix(v, "ciphertext1") {
					return v, nil
				}
			}
		}
	}
	return "", fmt.Errorf("record ciphertext not found in tx")
}
