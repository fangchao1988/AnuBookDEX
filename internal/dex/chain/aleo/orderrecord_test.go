package aleo

import (
	"testing"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/shopspring/decimal"
)

func TestOrderRecordLifecycle(t *testing.T) {
	pool := NewOrderPool()

	// 提交两笔订单（等待中）
	o1 := &match.Order{OrderId: 1, UserAddress: "aleo1a", BuyOrSell: match.Buy, Type: match.Limit,
		Price: decimal.NewFromInt(3500), UnfilledAmount: decimal.NewFromInt(1)}
	o2 := &match.Order{OrderId: 2, UserAddress: "aleo1b", BuyOrSell: match.Sell, Type: match.Limit,
		Price: decimal.NewFromInt(3505), UnfilledAmount: decimal.NewFromInt(2)}
	pool.RecordSubmit(o1, "ETH_USDT")
	pool.RecordSubmit(o2, "ETH_USDT")

	if got := pool.ListOrders("", "", 10); len(got) != 2 {
		t.Fatalf("expect 2 orders, got %d", len(got))
	}
	if got := pool.ListOrders("aleo1a", "", 10); len(got) != 1 || got[0].Side != "buy" {
		t.Fatalf("trader filter failed: %+v", got)
	}

	// 撮合回执：订单1 部分成交 -> partial；订单2 完全成交 -> filled
	mr1 := &match.MatchResult{
		OrderId: 1,
		Items: []*match.OrderResult{
			{Role: "maker", State: "filled", FilledAmount: ptr(decimal.NewFromFloat(0.5))},
			{Role: "taker", State: "partial-filled", FilledAmount: ptr(decimal.NewFromFloat(0.5))},
		},
	}
	mr2 := &match.MatchResult{
		OrderId: 2,
		Items: []*match.OrderResult{
			{Role: "maker", State: "filled", FilledAmount: ptr(decimal.NewFromInt(2))},
			{Role: "taker", State: "filled", FilledAmount: ptr(decimal.NewFromInt(2))},
		},
	}
	pool.RecordMatch([]*match.MatchResult{mr1, mr2})

	recs := pool.ListOrders("", "", 10)
	byID := map[int64]*OrderRecord{}
	for _, r := range recs {
		byID[r.OrderId] = r
	}
	if byID[1].Status != OrderStatusPartial || byID[1].Filled != "1" {
		t.Errorf("order1: expect partial/1, got %s/%s", byID[1].Status, byID[1].Filled)
	}
	if byID[2].Status != OrderStatusFilled || byID[2].Filled != "4" {
		t.Errorf("order2: expect filled/4, got %s/%s", byID[2].Status, byID[2].Filled)
	}
	// 排序：order_id 倒序
	if recs[0].OrderId != 2 {
		t.Errorf("expect newest first, got %d", recs[0].OrderId)
	}
}

func TestExtractRecordCiphertext(t *testing.T) {
	tx := map[string]interface{}{
		"execution": map[string]interface{}{
			"transitions": []interface{}{
				map[string]interface{}{
					"program_id": "anubook_dex_p2.aleo",
					"outputs": []interface{}{
						map[string]interface{}{"type": "future"},
						map[string]interface{}{"type": "record", "value": "ciphertext1qxy..."},
					},
				},
			},
		},
	}
	ct, err := extractRecordCiphertext(tx, "anubook_dex_p2.aleo")
	if err != nil || ct != "ciphertext1qxy..." {
		t.Fatalf("extract failed: ct=%q err=%v", ct, err)
	}
	// program 不匹配 -> 找不到
	if _, err := extractRecordCiphertext(tx, "other.aleo"); err == nil {
		t.Fatal("expect error for mismatched program")
	}
}

func ptr(d decimal.Decimal) *decimal.Decimal { return &d }
