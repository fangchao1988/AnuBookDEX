package ai

import (
	"time"

	"github.com/AnuBookDEX/engine/internal/core/market"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/shopspring/decimal"
)

// OrderSubmitter 冰山子单提交回调（引擎入口注入：提交到链下订单池等执行通道）。
type OrderSubmitter func(symbol string, side int, price, amount decimal.Decimal, parentID string) error

// AIHub 聚合 AI 组件，由引擎入口创建并接入数据流（双链共享，链无关）。
// 行情研判（Engine）订阅盘口深度；冰山拆分（IcebergEngine）定时产出子单并经回调提交。
type AIHub struct {
	engine  *Engine
	iceberg *IcebergEngine
	submit  OrderSubmitter
	done    chan struct{}
}

// NewAIHub 创建 AIHub；submit 为冰山子单提交回调（可 nil）。
func NewAIHub(submit OrderSubmitter) *AIHub {
	h := &AIHub{
		engine: NewEngine(),
		submit: submit,
		done:   make(chan struct{}),
	}
	h.iceberg = NewIcebergEngine(func(slice *IcebergSlice) {
		if h.submit != nil {
			if err := h.submit(slice.Symbol, slice.BuyOrSell, slice.Price, slice.Amount, slice.ParentID); err != nil {
				common.Error("AI iceberg: slice submit failed", "parent", slice.ParentID, "err", err)
			}
		}
	})
	return h
}

// Engine 返回行情研判引擎。
func (h *AIHub) Engine() *Engine { return h.engine }

// Iceberg 返回冰山拆分引擎。
func (h *AIHub) Iceberg() *IcebergEngine { return h.iceberg }

// Start 启动：订阅各交易对深度喂入研判引擎 + 冰山拆分定时器。
func (h *AIHub) Start(symbols []string, icebergInterval time.Duration) {
	for _, symbol := range symbols {
		ch := market.GetDepthChannel(symbol)
		if ch == nil {
			common.Warn("AI hub: no depth channel for", symbol)
			continue
		}
		go func(sym string, ch <-chan *market.QuoteDepths) {
			for {
				select {
				case <-h.done:
					return
				case d := <-ch:
					h.engine.UpdateDepth(sym, d)
				}
			}
		}(symbol, ch)
	}
	go func() {
		t := time.NewTicker(icebergInterval)
		defer t.Stop()
		for {
			select {
			case <-h.done:
				return
			case <-t.C:
				h.iceberg.Tick()
			}
		}
	}()
	common.Info("AI hub: started", "symbols", len(symbols), "iceberg_interval", icebergInterval.String())
}

// Shutdown 停止 AIHub。
func (h *AIHub) Shutdown() {
	close(h.done)
	h.engine.Shutdown()
}
