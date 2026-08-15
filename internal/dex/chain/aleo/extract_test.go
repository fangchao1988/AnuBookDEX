package aleo

import (
	"os"
	"strings"
	"testing"

	"github.com/AnuBookDEX/engine/internal/core/match"
)

// TestParseOrderPlaintext 用实测 place_order_buy 解密明文验证字段解析。
func TestParseOrderPlaintext(t *testing.T) {
	plain := `{
  owner: aleo16dwwdmqqwft7hugdelwx6d8g39xlqwsath8u3kxrhztw4re4xv9qhe7sal.private,
  order_id: 2u128.private,
  side: 0u8.private,
  price: 15784u64.private,
  amount: 63355300u64.private,
  deadline: 18789244u32.private,
  _nonce: 7756023592035278112952389447799547955293145112024686832632395352723860494682group.public,
  _version: 1u8.public
}`
	o, err := parseOrderPlaintext(plain)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if o.OrderId != 2 || o.BuyOrSell != match.Buy {
		t.Errorf("order id/side = %d/%v, want 2/buy", o.OrderId, o.BuyOrSell)
	}
	if o.Price.String() != "15784" || o.UnfilledAmount.String() != "63355300" {
		t.Errorf("price/amount = %s/%s, want 15784/63355300", o.Price, o.UnfilledAmount)
	}
	if o.Deadline != 18789244 {
		t.Errorf("deadline = %d, want 18789244", o.Deadline)
	}
}

// TestExtractAndDecryptOrderBuy 真实 testnet 买单交易（place_order_buy）端到端验证：
// 链上提取 Order CT + inner transfer_private_with_creds 的托管 Token/Credentials，
// operator view key 解密。需要 ALEO_VIEW_KEY + 网络 + leo CLI，否则跳过。
func TestExtractAndDecryptOrderBuy(t *testing.T) {
	if os.Getenv("ALEO_VIEW_KEY") == "" {
		t.Skip("ALEO_VIEW_KEY not set")
	}
	rpc := NewRESTClient("https://api.explorer.provable.com/v1")
	payload, err := ExtractAndDecryptOrder(rpc,
		"at157y2d4ac8gtepgv30e24w2xg0j6j5d7gmflfms56y4t82dg6vs9sj9uugy",
		"anubook_dex_p4.aleo")
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	o := payload.Order
	if o.OrderId != 2 || o.BuyOrSell != match.Buy {
		t.Errorf("order id/side = %d/%v, want 2/buy", o.OrderId, o.BuyOrSell)
	}
	if o.Price.String() != "15784" || o.UnfilledAmount.String() != "63355300" {
		t.Errorf("price/amount = %s/%s, want 15784/63355300", o.Price, o.UnfilledAmount)
	}
	if !strings.HasPrefix(payload.OrderCT, "record1") {
		t.Errorf("OrderCT is not record ciphertext: %.20s", payload.OrderCT)
	}
	// 托管资产：USDCX Token 1000000（1 USDCX，锁定 amount*price/1e6）
	if !strings.Contains(payload.OpFund, "amount: 1000000u128") {
		t.Errorf("OpFund mismatch: %q", payload.OpFund)
	}
	if !strings.Contains(payload.Creds, "freeze_list_root") {
		t.Errorf("Creds mismatch: %q", payload.Creds)
	}
}
