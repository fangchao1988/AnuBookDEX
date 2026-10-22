package anubis

// settlement_encode_test.go: encodeMatchResults 的回归测试。
//
// 曾出现的缺陷：撮合器对「无对手方」的挂单也会产出一条 item
// （state=submitted，AccFilledAmount=该单总量而非成交量）。
// 旧实现用 `FilledAmount 否则 AccFilledAmount` 取值，未过滤 state，
// 导致未成交单被当成成交下发 → 同一地址被重复扣款 → 合约
// revert "insufficient quote balance"，整批结算回滚。

import (
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/shopspring/decimal"
)

func dec(s string) *decimal.Decimal {
	d, _ := decimal.NewFromString(s)
	return &d
}

// TestEncodeSkipsUnfilledItems 未成交（state=submitted/canceled）条目必须被跳过。
func TestEncodeSkipsUnfilledItems(t *testing.T) {
	withPairTokens(t, "0xaaaa111111111111111111111111111111111111", "0xbbbb111111111111111111111111111111111111")
	mrs := []*match.MatchResult{{
		Id: 1, Symbol: "ETH_USDT", Price: *dec("0"), OrderId: 100,
		Items: []*match.OrderResult{{
			OrderId: 100, Role: "taker", State: "submitted",
			FilledAmount: nil, AccFilledAmount: dec("100"), // 注意：这是单量，不是成交量
		}},
	}}
	out, err := encodeMatchResults(mrs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("未成交条目应被跳过，却编码出 %d 条（会造成重复扣款）", len(out))
	}
}

// TestEncodeOnlyFilledItems 混合场景：仅 filled/partial-filled 被编码。
func TestEncodeOnlyFilledItems(t *testing.T) {
	withPairTokens(t, "0xaaaa111111111111111111111111111111111111", "0xbbbb111111111111111111111111111111111111")
	// 镜像真实结构：先是一条无对手方的挂单结果，随后是真实成交
	mrs := []*match.MatchResult{
		{ // 挂单到达，未成交
			Id: 1, Symbol: "ETH_USDT", Price: *dec("0"), OrderId: 100,
			Items: []*match.OrderResult{{
				OrderId: 100, Role: "taker", State: "submitted", AccFilledAmount: dec("100"),
			}},
		},
		{ // 真实成交：maker(100) + taker(101)
			Id: 2, Symbol: "ETH_USDT", Price: *dec("200"), OrderId: 101,
			Items: []*match.OrderResult{
				{OrderId: 100, Role: "maker", State: "filled", FilledAmount: dec("100"), AccFilledAmount: dec("100")},
				{OrderId: 101, Role: "taker", State: "filled", FilledAmount: nil, AccFilledAmount: dec("100")},
			},
		},
	}
	// 提供地址与方向解析
	resolver := func(orderID int64) (orderInfo, bool) {
		switch orderID {
		case 100:
			return orderInfo{Trader: "0x1111111111111111111111111111111111111111", Side: 0}, true // 买
		case 101:
			return orderInfo{Trader: "0x2222222222222222222222222222222222222222", Side: 1}, true // 卖
		}
		return orderInfo{}, false
	}

	out, err := encodeMatchResults(mrs, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("应只编码 2 条真实成交，得到 %d 条", len(out))
	}
	// 买方向、卖方金额与手续费基数正确
	if out[0].Side != 0 || out[0].Amount.String() != "100" || out[0].Cost.String() != "20000" {
		t.Errorf("买方条目错误: side=%d amount=%s cost=%s", out[0].Side, out[0].Amount, out[0].Cost)
	}
	if out[1].Side != 1 || out[1].Amount.String() != "100" || out[1].Cost.String() != "20000" {
		t.Errorf("卖方条目错误: side=%d amount=%s cost=%s", out[1].Side, out[1].Amount, out[1].Cost)
	}
}

// withPairTokens 在测试期替换币对代币解析（测试环境无 config.yaml）。
func withPairTokens(t *testing.T, base, quote string) {
	t.Helper()
	orig := pairTokensFor
	pairTokensFor = func(symbol string) (ethcommon.Address, ethcommon.Address, bool) {
		if symbol != "ETH_USDT" {
			return ethcommon.Address{}, ethcommon.Address{}, false
		}
		return ethcommon.HexToAddress(base), ethcommon.HexToAddress(quote), true
	}
	t.Cleanup(func() { pairTokensFor = orig })
}

// TestEncodeSkipsPairWithoutTokens 未配置代币地址的币对跳过（避免下发零地址被合约拒绝）。
func TestEncodeSkipsPairWithoutTokens(t *testing.T) {
	mrs := []*match.MatchResult{{
		Id: 1, Symbol: "UNCONFIGURED_PAIR", Price: *dec("10"), OrderId: 1,
		Items: []*match.OrderResult{{OrderId: 1, Role: "maker", State: "filled", FilledAmount: dec("1")}},
	}}
	out, err := encodeMatchResults(mrs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("未配置代币的币对应跳过，得到 %d 条", len(out))
	}
}
