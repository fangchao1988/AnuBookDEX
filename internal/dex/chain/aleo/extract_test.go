package aleo

import (
	"os"
	"strings"
	"testing"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/infra/common"
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

// TestExtractAndDecryptOrderBuy 真实 testnet 隐私买单交易（p7 place_order_buy_private）
// 端到端验证：链上提取 Order CT + inner transfer_private_with_creds 的托管 Token，
// operator view key 解密。需要 ALEO_VIEW_KEY + 网络 + leo CLI，否则跳过。
func TestExtractAndDecryptOrderBuy(t *testing.T) {
	if os.Getenv("ALEO_VIEW_KEY") == "" {
		t.Skip("ALEO_VIEW_KEY not set")
	}
	common.ZapInit() // ExtractAndDecryptOrder 内部 common.Debug 需要 zap logger
	rpc := NewRESTClient("https://api.explorer.provable.com/v1")
	payload, err := ExtractAndDecryptOrder(rpc,
		"at1tlpvpus23kjzppzned3aujymzt09ca73nefsz2whhaglfzglxy8qk6wdeg",
		"anubook_dex_p7.aleo")
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	o := payload.Order
	if o.OrderId != 1787036458182 || o.BuyOrSell != match.Buy {
		t.Errorf("order id/side = %d/%v, want 1787036458182/buy", o.OrderId, o.BuyOrSell)
	}
	if o.Price.String() != "16000" || o.UnfilledAmount.String() != "1000000" {
		t.Errorf("price/amount = %s/%s, want 16000/1000000", o.Price, o.UnfilledAmount)
	}
	if !strings.HasPrefix(payload.OrderCT, "record1") {
		t.Errorf("OrderCT is not record ciphertext: %.20s", payload.OrderCT)
	}
	// 托管资产：USDCX Token（operator 托管 16000，price*amount/1e6）
	if !strings.Contains(payload.OpFund, "amount: 16000u128") {
		t.Errorf("OpFund mismatch: %q", payload.OpFund)
	}
	// p7 设计：结算统一用 operator 自有凭证（chain.aleo.operator-credentials），
	// 用户 Credentials 因 owner!=operator 解密失败降级为空，不阻塞下单
	if payload.Creds != "" {
		t.Errorf("Creds should be empty (user-owned, decrypt skipped), got %q", payload.Creds)
	}
}
