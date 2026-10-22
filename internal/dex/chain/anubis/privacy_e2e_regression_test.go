package anubis

// privacy_e2e_regression_test.go: 隐私（ECIES）下单路径的回归测试。
//
// 曾经的缺陷：buildOrder 的私有分支未设置 order.SeqId，而撮合器
// （matcher.go Matchable）用 `order.SeqId <= oppoOrder.SeqId` 做价格-时间优先判定，
// 导致隐私订单 SeqId 恒为 0、永远无法成交 —— 表现为「订单 matched 但链上
// BatchSettled.totalVolume=0」。此测试固定该契约。

import (
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/dex/privacy"
)

func TestPrivateOrderSetsSeqIdForMatching(t *testing.T) {
	vk := testViewKey(t)
	orderID := int64(1790676249462)
	deadline := time.Now().Unix() + 3600

	// 构造一个真实的 ECIES 订单（复用测试 view key）
	nullifier := make([]byte, 32)
	copy(nullifier, []byte("nullifier-1"))
	plain := []byte(`{"order_id":1790676249462,"buy_or_sell":0,"type":1,"price":"100","amount":"1","circuit_rate":"0","create_at":1790676249462,"stp":0,"deadline":` +
		itoa(deadline) + `}`)
	eo, err := privacy.Encrypt(plain, &vk.PrivateKey.PublicKey, nullifier, deadline)
	if err != nil {
		t.Fatal(err)
	}
	ct := "0x" + hex.EncodeToString(mustJSON(t, eo))

	order, _, err := buildOrder(orderSubmitReq{
		OrderId:    orderID,
		Symbol:     "ETH_USDT",
		Side:       0,
		Trader:     "0xabc",
		Mode:       "private",
		Ciphertext: ct,
		Deadline:   deadline,
	}, "private", vk)
	if err != nil {
		t.Fatalf("buildOrder: %v", err)
	}

	// 核心回归断言：SeqId 必须非零且等于 order_id，
	// 否则撮合器会因 SeqId 相等而拒绝成交（totalVolume 永远为 0）
	if order.SeqId == 0 {
		t.Fatal("SeqId must be set for private orders (matcher rejects SeqId<=oppoOrder.SeqId)")
	}
	if order.SeqId != orderID {
		t.Errorf("SeqId = %d, want %d", order.SeqId, orderID)
	}
	// 其余字段也需完整（价格/数量来自密文明文）
	if order.Price.String() != "100" || order.UnfilledAmount.String() != "1" {
		t.Errorf("decrypted fields wrong: price=%s amount=%s", order.Price, order.UnfilledAmount)
	}
	if order.Symbol != "ETH_USDT" || order.UserAddress != "0xabc" {
		t.Errorf("symbol/trader not applied: %s / %s", order.Symbol, order.UserAddress)
	}
	if order.State != match.Submitted {
		t.Errorf("state = %v, want Submitted", order.State)
	}
}

// TestPrivateOrdersCanMatch 两笔隐私订单（SeqId 递增）应能撮合成功。
func TestPrivateOrdersCanMatch(t *testing.T) {
	vk := testViewKey(t)
	deadline := time.Now().Unix() + 3600

	mk := func(orderID int64, side int, price, amount string) *match.Order {
		nullifier := make([]byte, 32)
		copy(nullifier, []byte{byte(orderID)})
		plain := []byte(`{"order_id":` + itoa(orderID) + `,"buy_or_sell":` + itoa(int64(side)) +
			`,"type":1,"price":"` + price + `","amount":"` + amount +
			`","circuit_rate":"0","create_at":` + itoa(orderID) + `,"stp":0,"deadline":` + itoa(deadline) + `}`)
		eo, err := privacy.Encrypt(plain, &vk.PrivateKey.PublicKey, nullifier, deadline)
		if err != nil {
			t.Fatal(err)
		}
		o, _, err := buildOrder(orderSubmitReq{
			OrderId: orderID, Symbol: "ETH_USDT", Side: side, Trader: "0xt",
			Mode: "private", Ciphertext: "0x" + hex.EncodeToString(mustJSON(t, eo)), Deadline: deadline,
		}, "private", vk)
		if err != nil {
			t.Fatal(err)
		}
		return o
	}

	book := match.InitOrderBook(0, "ETH_USDT")
	buy := mk(1001, 0, "100", "1")
	sell := mk(1002, 1, "100", "1")

	book.Enqueue(buy)
	mr1 := book.GenMatchResult(sell)
	if mr1 == nil {
		t.Fatal("GenMatchResult returned nil")
	}
	if mr1.Mr.Price.IsZero() {
		t.Errorf("match price is zero — private orders did not match (SeqId regression?): %+v", mr1.Mr)
	}
	if len(mr1.Mr.Items) == 0 {
		t.Error("expected match items")
	}
}

func testViewKey(t *testing.T) *privacy.ViewKey {
	t.Helper()
	priv, err := parseP256PrivateKey("0x2222222222222222222222222222222222222222222222222222222222222222")
	if err != nil {
		t.Fatal(err)
	}
	pub := elliptic.MarshalCompressed(priv.PublicKey.Curve, priv.PublicKey.X, priv.PublicKey.Y)
	tag := sha256.Sum256(pub)
	return &privacy.ViewKey{PrivateKey: priv, ViewTag: tag[:4]}
}

func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
