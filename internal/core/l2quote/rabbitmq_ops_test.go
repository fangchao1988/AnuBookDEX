package l2quote

import (
	"testing"
	"time"
)

func TestWsChannelFromRoutingKey(t *testing.T) {
	cases := map[string]string{
		"market.ETH_USDT.kline.1min":  "kline.ETH_USDT.1min",
		"market.ETH_USDT.kline.4hour": "kline.ETH_USDT.4hour",
		"market.BTC_USDT.trade.detail": "trade.BTC_USDT",
		"market.BTC_USDT.ticker":       "ticker.BTC_USDT",
		"bogus.key":                    "",
		"market.ETH_USDT.detail":       "",
	}
	for key, want := range cases {
		if got := wsChannelFromRoutingKey(key); got != want {
			t.Errorf("wsChannelFromRoutingKey(%q) = %q, want %q", key, got, want)
		}
	}
}

// TestSendToMQRawPublisher 验证 DEX 模式下 sendToMQ 通过 RawPublisher
// 把 K线/成交/Ticker 转发到正确的 WS 频道
func TestSendToMQRawPublisher(t *testing.T) {
	L := &L2quote{
		symbol:         "ETH_USDT",
		mqSendChan:     make(chan *MqMessage, 16),
		mqBatchSize:    10,
		mqSendIntervalMS: 100,
	}
	received := make(chan struct {
		channel string
		body    []byte
	}, 16)
	L.SetRawPublisher(func(channel string, data []byte) {
		received <- struct {
			channel string
			body    []byte
		}{channel, data}
	})
	go L.sendToMQ(L.mqSendChan)

	// 推送 K线 + 成交两条消息
	L.mqSendChan <- &MqMessage{
		RoutingKey: "market.ETH_USDT.kline.1min",
		Body:       []byte(`{"type":"market.candles"}`),
	}
	L.mqSendChan <- &MqMessage{
		RoutingKey: "market.ETH_USDT.trade.detail",
		Body:       []byte(`{"type":"market.fills"}`),
	}

	want := map[string]string{
		"kline.ETH_USDT.1min": `{"type":"market.candles"}`,
		"trade.ETH_USDT":      `{"type":"market.fills"}`,
	}
	got := make(map[string]string)
	timeout := time.After(3 * time.Second)
	for len(got) < 2 {
		select {
		case m := <-received:
			got[m.channel] = string(m.body)
		case <-timeout:
			t.Fatalf("timeout waiting for broadcasts, got %v", got)
		}
	}
	for ch, body := range want {
		if got[ch] != body {
			t.Errorf("channel %q body = %q, want %q", ch, got[ch], body)
		}
	}
}
