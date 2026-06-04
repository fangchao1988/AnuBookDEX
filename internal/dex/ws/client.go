package ws

import (
	"encoding/json"
	"io"
	"time"

	"github.com/AnuBookDEX/engine/internal/infra/common"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096
)

// Client WebSocket 客户端
type Client struct {
	id   string
	hub  *Hub
	conn *WSConn
	send chan []byte
}

// wsRequest 客户端请求消息格式
type wsRequest struct {
	Cmd      string   `json:"cmd"`
	Channels []string `json:"channels"`
}

// readPump 从 WebSocket 连接读取消息
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	for {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))

		message, err := c.conn.ReadMessage()
		if err != nil {
			if err != io.EOF {
				common.Warn("ws: read error:", err, "client:", c.id)
			}
			break
		}

		if len(message) > maxMessageSize {
			continue
		}

		var req wsRequest
		if err := json.Unmarshal(message, &req); err != nil {
			common.Warn("ws: invalid message from", c.id, ":", string(message))
			continue
		}

		switch req.Cmd {
		case "subscribe":
			c.hub.Subscribe(c.id, req.Channels)
		case "unsubscribe":
			c.hub.Unsubscribe(c.id, req.Channels)
		default:
			common.Warn("ws: unknown cmd from", c.id, ":", req.Cmd)
		}
	}
}

// writePump 向 WebSocket 连接写入消息
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.Close()
				return
			}
			if err := c.conn.WriteMessage(message); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WritePing(); err != nil {
				return
			}
		}
	}
}
