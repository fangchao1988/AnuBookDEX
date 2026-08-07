package ai

import (
	"encoding/json"
	"net/http"

	"github.com/shopspring/decimal"
)

// HandleAI 返回 AI 相关 HTTP API 的 handler（挂到引擎 9000 端口）：
//
//	GET  /ai/signal?symbol=ETH_USDT      查询研判信号（HOLD/BUY/SELL/STRONG_BUY/STRONG_SELL）
//	GET  /ai/indicators?symbol=ETH_USDT  查询市场指标（价差/失衡/深度偏向/压力）
//	POST /ai/sentiment {symbol,score}    设置舆情 [-1,1]
//	POST /ai/iceberg {symbol,order_id,total_amount,limit_price,side,strategy}  提交冰山订单（大单拆分）
func HandleAI(hub *AIHub) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ai/signal", hub.handleSignal)
	mux.HandleFunc("/ai/indicators", hub.handleIndicators)
	mux.HandleFunc("/ai/sentiment", hub.handleSentiment)
	mux.HandleFunc("/ai/iceberg", hub.handleIceberg)
	return mux
}

func (h *AIHub) handleSignal(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	sig := h.engine.GetSignal(symbol)
	writeJSON(w, map[string]interface{}{
		"symbol": symbol, "signal": sig.String(), "code": int(sig),
	})
}

func (h *AIHub) handleIndicators(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	ind := h.engine.GetIndicators(symbol)
	if ind == nil {
		writeJSON(w, map[string]interface{}{"symbol": symbol, "indicators": nil})
		return
	}
	writeJSON(w, map[string]interface{}{"symbol": symbol, "indicators": ind})
}

func (h *AIHub) handleSentiment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Symbol string  `json:"symbol"`
		Score  float64 `json:"score"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.engine.SetSentiment(req.Symbol, req.Score)
	writeJSON(w, map[string]interface{}{"symbol": req.Symbol, "score": req.Score})
}

func (h *AIHub) handleIceberg(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Symbol      string `json:"symbol"`
		OrderID     string `json:"order_id"`
		TotalAmount string `json:"total_amount"`
		LimitPrice  string `json:"limit_price"`
		Side        int    `json:"side"` // 0=buy, 1=sell
		Strategy    int    `json:"strategy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	total, err := decimal.NewFromString(req.TotalAmount)
	if err != nil || total.Sign() <= 0 {
		http.Error(w, "invalid total_amount", http.StatusBadRequest)
		return
	}
	price, err := decimal.NewFromString(req.LimitPrice)
	if err != nil {
		http.Error(w, "invalid limit_price", http.StatusBadRequest)
		return
	}
	ice, err := h.iceberg.SubmitIceberg(req.OrderID, req.Symbol, total, price, req.Side, SplitStrategy(req.Strategy))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]interface{}{
		"order_id": req.OrderID, "total": total.String(),
		"slice_size": ice.SliceSize.String(), "interval_sec": int(ice.SliceInterval.Seconds()),
	})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
