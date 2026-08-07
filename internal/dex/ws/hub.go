package ws

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/AnuBookDEX/engine/internal/dex/auth"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/core/l2quote"
	"github.com/AnuBookDEX/engine/internal/core/market"
)

// Hub WebSocket 连接中心，负责行情数据广播
type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client             // clientID -> Client
	subs    map[string]map[string]struct{} // channel -> set of clientIDs

	// 消息广播通道
	broadcast chan *message

	// 注册/注销
	register   chan *Client
	unregister chan *Client
}

type message struct {
	channel string
	data    []byte
}

// NewHub 创建 WebSocket Hub
func NewHub() *Hub {
	h := &Hub{
		clients:    make(map[string]*Client),
		subs:       make(map[string]map[string]struct{}),
		broadcast:  make(chan *message, 10000),
		register:   make(chan *Client, 100),
		unregister: make(chan *Client, 100),
	}
	go h.run()
	common.Info("websocket hub: started")
	return h
}

// run Hub 主循环
func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.id] = client
			h.mu.Unlock()
			common.Info(fmt.Sprintf("websocket: client %s connected (total: %d)", client.id, len(h.clients)))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.id]; ok {
				delete(h.clients, client.id)
				close(client.send)
			}
			for channel, clientIDs := range h.subs {
				delete(clientIDs, client.id)
				if len(clientIDs) == 0 {
					delete(h.subs, channel)
				}
			}
			h.mu.Unlock()
			common.Info(fmt.Sprintf("websocket: client %s disconnected (total: %d)", client.id, len(h.clients)))

		case msg := <-h.broadcast:
			h.mu.RLock()
			clientIDs, ok := h.subs[msg.channel]
			if !ok {
				h.mu.RUnlock()
				continue
			}
			for clientID := range clientIDs {
				if client, exists := h.clients[clientID]; exists {
					select {
					case client.send <- msg.data:
					default:
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

// BroadcastDepth 广播深度行情
func (h *Hub) BroadcastDepth(symbol string, depth *market.QuoteDepths) {
	data, err := json.Marshal(depth)
	if err != nil {
		common.Error("ws: marshal depth error:", err)
		return
	}
	h.broadcast <- &message{
		channel: fmt.Sprintf("depth.%s", symbol),
		data:    data,
	}
}

// BroadcastKline 广播 K线
func (h *Hub) BroadcastKline(symbol string, interval string, kline *l2quote.QuoteKline) {
	data, err := json.Marshal(kline)
	if err != nil {
		common.Error("ws: marshal kline error:", err)
		return
	}
	h.broadcast <- &message{
		channel: fmt.Sprintf("kline.%s.%s", symbol, interval),
		data:    data,
	}
}

// BroadcastTrade 广播成交明细
func (h *Hub) BroadcastTrade(symbol string, trade *l2quote.TradeDetail) {
	data, err := json.Marshal(trade)
	if err != nil {
		common.Error("ws: marshal trade error:", err)
		return
	}
	h.broadcast <- &message{
		channel: fmt.Sprintf("trade.%s", symbol),
		data:    data,
	}
}

// BroadcastRaw 广播已序列化消息到指定频道（DEX 模式：l2quote 直连 Hub，
// 消息体由 l2quote 生成，避免二次序列化）
func (h *Hub) BroadcastRaw(channel string, data []byte) {
	h.broadcast <- &message{channel: channel, data: data}
}

// Subscribe 客户端订阅频道
func (h *Hub) Subscribe(clientID string, channels []string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, channel := range channels {
		if h.subs[channel] == nil {
			h.subs[channel] = make(map[string]struct{})
		}
		h.subs[channel][clientID] = struct{}{}
	}
	return nil
}

// Unsubscribe 客户端退订频道
func (h *Hub) Unsubscribe(clientID string, channels []string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, channel := range channels {
		if clientIDs, ok := h.subs[channel]; ok {
			delete(clientIDs, clientID)
			if len(clientIDs) == 0 {
				delete(h.subs, channel)
			}
		}
	}
	return nil
}

// HandleWebSocket 处理 WebSocket 升级请求
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Authenticate before upgrade
	token := r.URL.Query().Get("token")
	if token == "" {
		token = r.Header.Get("Authorization")
	}
	if !auth.ValidateToken(token) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	conn, err := upgradeHTTP(w, r)
	if err != nil {
		common.Error("ws: upgrade error:", err)
		return
	}

	clientID := r.URL.Query().Get("client_id")
	if clientID == "" {
		clientID = fmt.Sprintf("client_%d", time.Now().UnixNano())
	}

	client := &Client{
		id:   clientID,
		hub:  h,
		conn: conn,
		send: make(chan []byte, 256),
	}

	h.register <- client

	go client.writePump()
	go client.readPump()
}
