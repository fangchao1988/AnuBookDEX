package validate

import (
	"testing"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/shopspring/decimal"
)

func TestValidateOrderbook(t *testing.T) {
	// Test that ResultEqual correctly identifies identical match results
	price := decimal.NewFromFloat(0.33)
	unfilledAmount := decimal.NewFromFloat(0.66)
	filledAmount := decimal.NewFromFloat(0.99)

	orderResult := &match.OrderResult{
		OrderId:        111,
		Role:           "maker",
		Price:          &price,
		UnfilledAmount: &unfilledAmount,
		FilledAmount:   &filledAmount,
		State:          "filled",
	}

	result1 := &match.MatchResult{
		Id:           1,
		Symbol:       "ethbtc",
		Ts:           1235566,
		PublishTs:    23323,
		OrderTypeStr: "sell-limit",
		Items:        []*match.OrderResult{orderResult},
	}

	// Same data should be equal
	orderResult2 := &match.OrderResult{
		OrderId:        111,
		Role:           "maker",
		Price:          &price,
		UnfilledAmount: &unfilledAmount,
		FilledAmount:   &filledAmount,
		State:          "filled",
	}

	result2 := &match.MatchResult{
		Id:           1,
		Symbol:       "ethbtc",
		Ts:           1235566,
		PublishTs:    23323,
		OrderTypeStr: "sell-limit",
		Items:        []*match.OrderResult{orderResult2},
	}

	bytes1, _ := json.Marshal(result1)
	bytes2, _ := json.Marshal(result2)

	equal, err := match.ResultEqual(string(bytes1), string(bytes2))
	if err != nil {
		t.Error(err)
	}
	if !equal {
		t.Error("expected equal results")
	}

	// Different symbol should NOT be equal
	result3 := &match.MatchResult{
		Id:           1,
		Symbol:       "btcusdt",
		Ts:           1235566,
		PublishTs:    23323,
		OrderTypeStr: "sell-limit",
		Items:        []*match.OrderResult{orderResult},
	}
	bytes3, _ := json.Marshal(result3)
	equal, _ = match.ResultEqual(string(bytes1), string(bytes3))
	if equal {
		t.Error("expected non-equal results for different symbols")
	}
}

func TestValidateOrderbookDecimal(t *testing.T) {
	// Test decimal precision in match result comparison
	price, err := decimal.NewFromString("23.233333333333333333333999999999")
	if err != nil {
		t.Fatal(err)
	}

	result1 := &match.MatchResult{
		Id:           1,
		Symbol:       "tbcusdt",
		Ts:           12234,
		PublishTs:    22222,
		OrderTypeStr: "filled",
		Items: []*match.OrderResult{{
			OrderId: 10000101,
			Price:   &price,
			Role:    "taker",
		}},
	}

	price2, err := decimal.NewFromString("23.233333333333333333333999999999")
	if err != nil {
		t.Fatal(err)
	}
	result2 := &match.MatchResult{
		Id:           1,
		Symbol:       "tbcusdt",
		Ts:           12234,
		PublishTs:    22222,
		OrderTypeStr: "filled",
		Items: []*match.OrderResult{{
			OrderId: 10000101,
			Price:   &price2,
			Role:    "taker",
		}},
	}

	s1, _ := json.Marshal(result1)
	s2, _ := json.Marshal(result2)

	equal, err := match.ResultEqual(string(s1), string(s2))
	if err != nil {
		t.Error(err)
	}
	if !equal {
		t.Error("expected equal results for identical decimal values")
	}
}
