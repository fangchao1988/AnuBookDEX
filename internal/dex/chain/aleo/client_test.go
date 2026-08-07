package aleo

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AnuBookDEX/engine/internal/core/match"
)

// TestGetLatestHeight 校验区块高度解析（snarkOS 返回纯数字串）。
func TestGetLatestHeight(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/testnet/latest/height" {
			t.Errorf("path = %s, want /testnet/latest/height", r.URL.Path)
		}
		w.Write([]byte("12345"))
	}))
	defer srv.Close()

	c := NewRESTClient(srv.URL)
	h, err := c.GetLatestHeight()
	if err != nil {
		t.Fatal(err)
	}
	if h != 12345 {
		t.Errorf("height = %d, want 12345", h)
	}
}

// TestGetProgramMapping 校验读取 mapping 返回原始 plaintext。
func TestGetProgramMapping(t *testing.T) {
	want := "Order { trader:aleo1abc, side: 0u8, price: 100u64 }"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/testnet/program/anubook_dex.aleo/mapping/orders/1"
		if r.URL.Path != wantPath {
			t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
		}
		w.Write([]byte(want))
	}))
	defer srv.Close()

	c := NewRESTClient(srv.URL)
	got, err := c.GetProgramMapping("anubook_dex.aleo", "orders", "1")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("mapping = %q, want %q", got, want)
	}
}

// TestParseOrder 校验从 Leo struct plaintext 解析为 match.Order。
func TestParseOrder(t *testing.T) {
	plaintext := "Order { trader:aleo1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq3ljyzc, side: 1u8, price: 250u64, amount: 7u64, base_token: 1u32, quote_token: 2u32, deadline: 9000u32, active: true }"
	o, err := ParseOrder(42, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if o.OrderId != 42 || o.SeqId != 42 {
		t.Errorf("OrderId/SeqId = %d/%d, want 42/42", o.OrderId, o.SeqId)
	}
	if o.BuyOrSell != match.Sell {
		t.Errorf("BuyOrSell = %v, want Sell (side=1)", o.BuyOrSell)
	}
	if o.Price.String() != "250" {
		t.Errorf("Price = %s, want 250", o.Price.String())
	}
	if o.UnfilledAmount.String() != "7" {
		t.Errorf("UnfilledAmount = %s, want 7", o.UnfilledAmount.String())
	}
	if o.Deadline != 9000 {
		t.Errorf("Deadline = %d, want 9000", o.Deadline)
	}
	if o.UserAddress != "aleo1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq3ljyzc" {
		t.Errorf("UserAddress = %s", o.UserAddress)
	}
	if o.Type != match.Limit || o.State != match.Submitted {
		t.Errorf("Type/State = %v/%v, want Limit/Submitted", o.Type, o.State)
	}
}

// TestParseOrderBuy 校验 side=0 映射为 Buy。
func TestParseOrderBuy(t *testing.T) {
	o, err := ParseOrder(1, "Order { trader:aleo1x, side: 0u8, price: 100u64, amount: 1u64, deadline: 0u32 }")
	if err != nil {
		t.Fatal(err)
	}
	if o.BuyOrSell != match.Buy {
		t.Errorf("BuyOrSell = %v, want Buy", o.BuyOrSell)
	}
}

// TestParseOrderRealFormat 用真实 testnet 返回的 mapping 格式（多行 plaintext）验证解析。
func TestParseOrderRealFormat(t *testing.T) {
	raw := "{\n  trader: aleo16dwwdmqqwft7hugdelwx6d8g39xlqwsath8u3kxrhztw4re4xv9qhe7sal,\n  side: 0u8,\n  price: 100u64,\n  amount: 5u64,\n  base_token: 1u32,\n  quote_token: 2u32,\n  deadline: 10000u32,\n  active: true\n}"
	o, err := ParseOrder(1, raw)
	if err != nil {
		t.Fatal(err)
	}
	if o.UserAddress != "aleo16dwwdmqqwft7hugdelwx6d8g39xlqwsath8u3kxrhztw4re4xv9qhe7sal" {
		t.Errorf("UserAddress = %s", o.UserAddress)
	}
	if o.BuyOrSell != match.Buy {
		t.Errorf("BuyOrSell = %v, want Buy", o.BuyOrSell)
	}
	if o.Price.String() != "100" || o.UnfilledAmount.String() != "5" {
		t.Errorf("Price/Amount = %s/%s, want 100/5", o.Price.String(), o.UnfilledAmount.String())
	}
	if o.Deadline != 10000 {
		t.Errorf("Deadline = %d, want 10000", o.Deadline)
	}
}

// TestParseOrderLiteralNewline 用真实 snarkOS REST 响应格式（JSON 字符串，字段间为字面 `\n`
// 反斜杠+n 两个字符）验证解析。曾因正则不排除反斜杠导致 active 被吞成 "false\n"。
func TestParseOrderLiteralNewline(t *testing.T) {
	raw := `"{\n  trader: aleo16dwwdmqqwft7hugdelwx6d8g39xlqwsath8u3kxrhztw4re4xv9qhe7sal,\n  side: 0u8,\n  price: 100u64,\n  amount: 5u64,\n  base_token: 1u32,\n  quote_token: 2u32,\n  deadline: 10000u32,\n  active: false\n}"`
	o, err := ParseOrder(1, raw)
	if err != nil {
		t.Fatal(err)
	}
	if o.State != match.Canceled {
		t.Errorf("State = %v, want Canceled (active=false with literal \\n)", o.State)
	}
	if o.UnfilledAmount.String() != "5" {
		t.Errorf("amount = %s, want 5", o.UnfilledAmount.String())
	}
}

// TestParseOrderLiteralNewlineActive 字面 \n 版 active=true 应保持 Submitted。
func TestParseOrderLiteralNewlineActive(t *testing.T) {
	raw := `"{\n  trader: aleo16dwwdmqqwft7hugdelwx6d8g39xlqwsath8u3kxrhztw4re4xv9qhe7sal,\n  side: 0u8,\n  price: 100u64,\n  amount: 5u64,\n  base_token: 1u32,\n  quote_token: 2u32,\n  deadline: 10000u32,\n  active: true\n}"`
	o, err := ParseOrder(1, raw)
	if err != nil {
		t.Fatal(err)
	}
	if o.State != match.Submitted {
		t.Errorf("State = %v, want Submitted (active=true)", o.State)
	}
}

// TestParseOrderBad 校验空/坏 plaintext 返回 error。
func TestParseOrderBad(t *testing.T) {
	if _, err := ParseOrder(1, ""); err == nil {
		t.Error("expected error for empty plaintext")
	}
	if _, err := ParseOrder(1, "not a struct"); err == nil {
		t.Error("expected error for non-struct plaintext")
	}
}

// TestBroadcastTransaction 校验广播返回 txID。
func TestBroadcastTransaction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/testnet/transaction/broadcast" {
			t.Errorf("path = %s, want /testnet/transaction/broadcast", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "txdata") {
			t.Errorf("body = %q, want to contain txdata", string(body))
		}
		w.WriteHeader(200)
		w.Write([]byte("at1stubtxid"))
	}))
	defer srv.Close()

	c := NewRESTClient(srv.URL)
	txID, err := c.BroadcastTransaction("txdata123")
	if err != nil {
		t.Fatal(err)
	}
	if txID != "at1stubtxid" {
		t.Errorf("txID = %q, want at1stubtxid", txID)
	}
}

// TestBroadcastHTTPError 校验非 2xx 返回 error。
func TestBroadcastHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte("bad tx"))
	}))
	defer srv.Close()

	c := NewRESTClient(srv.URL)
	if _, err := c.BroadcastTransaction("bad"); err == nil {
		t.Error("expected error on 400")
	}
}

// TestGetTransaction 校验交易回执 JSON 解析。
func TestGetTransaction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"at1abc","status":"accepted"}`))
	}))
	defer srv.Close()

	c := NewRESTClient(srv.URL)
	m, err := c.GetTransaction("at1abc")
	if err != nil {
		t.Fatal(err)
	}
	if m["status"] != "accepted" {
		t.Errorf("status = %v, want accepted", m["status"])
	}
}
