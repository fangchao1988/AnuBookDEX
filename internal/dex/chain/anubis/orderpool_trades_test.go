package anubis

// orderpool_trades_test.go: 成交流水（TradeRecord）回归测试。
//
// 曾出现的缺陷：AppendTrades 遍历 mr.Items，把 maker 与 taker **各记一条**，
// 且记录里没有地址字段 —— 前端拿到同一条成交的两条记录、无法区分方向，
// 表现为「同一笔成交在成交流水中重复显示两遍」。

import (
	"testing"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/shopspring/decimal"
)

// seedOrder 在订单池登记一笔订单（提供地址解析所需的 trader 记录）
func seedOrder(p *OrderPool, orderID int64, trader string, side int, price, amount string) {
	p.orders[orderID] = &OrderRecord{
		OrderId: orderID,
		Symbol:  "ETH_USDT",
		Side:    side,
		Price:   price,
		Amount:  amount,
		Trader:  trader,
		Mode:    "public",
		State:   "matched",
	}
}

// TestAppendTradesOneRecordPerFill 一笔成交只能产生一条流水，且买卖双方地址正确。
func TestAppendTradesOneRecordPerFill(t *testing.T) {
	pool := NewOrderPool()
	const maker, taker = "0x1111111111111111111111111111111111111111", "0x2222222222222222222222222222222222222222"
	seedOrder(pool, 100, maker, 0, "200", "5") // maker 挂买
	seedOrder(pool, 101, taker, 1, "200", "5") // taker 卖出，吃掉买单

	// 撮合器对一笔成交产出两个 item（maker + taker）
	mrs := []*match.MatchResult{{
		Id: 1, Symbol: "ETH_USDT", Price: *dec("200"), Ts: 123, OrderTypeStr: "sell-limit", OrderId: 101,
		Items: []*match.OrderResult{
			{OrderId: 100, Role: "maker", State: "filled", FilledAmount: dec("5"), AccFilledAmount: dec("5")},
			{OrderId: 101, Role: "taker", State: "filled", FilledAmount: nil, AccFilledAmount: dec("5")},
		},
	}}

	pool.AppendTrades(mrs)

	if len(pool.trades) != 1 {
		t.Fatalf("一笔成交应只记 1 条流水，得到 %d 条（重复展示的根因）", len(pool.trades))
	}
	tr := pool.trades[0]
	if tr.Amount != "5" || tr.Price != "200" {
		t.Errorf("价格/数量错误: price=%s amount=%s", tr.Price, tr.Amount)
	}
	// taker 是卖方 → 买方为 maker
	if tr.Side != 1 {
		t.Errorf("side = %d, want 1 (taker 卖出)", tr.Side)
	}
	if tr.Buyer != maker || tr.Seller != taker {
		t.Errorf("买卖双方错误: buyer=%s seller=%s", tr.Buyer, tr.Seller)
	}
}

// TestAppendTradesTakerBuy 反方向：taker 买入时买方应为 taker。
func TestAppendTradesTakerBuy(t *testing.T) {
	pool := NewOrderPool()
	const maker, taker = "0x1111111111111111111111111111111111111111", "0x2222222222222222222222222222222222222222"
	seedOrder(pool, 200, maker, 1, "200", "3") // maker 挂卖
	seedOrder(pool, 201, taker, 0, "200", "3") // taker 买入

	mrs := []*match.MatchResult{{
		Id: 2, Symbol: "ETH_USDT", Price: *dec("200"), Ts: 456, OrderTypeStr: "buy-limit", OrderId: 201,
		Items: []*match.OrderResult{
			{OrderId: 200, Role: "maker", State: "filled", FilledAmount: dec("3"), AccFilledAmount: dec("3")},
			{OrderId: 201, Role: "taker", State: "filled", AccFilledAmount: dec("3")},
		},
	}}
	pool.AppendTrades(mrs)

	if len(pool.trades) != 1 {
		t.Fatalf("应只记 1 条，得到 %d 条", len(pool.trades))
	}
	tr := pool.trades[0]
	if tr.Side != 0 {
		t.Errorf("side = %d, want 0 (taker 买入)", tr.Side)
	}
	if tr.Buyer != taker || tr.Seller != maker {
		t.Errorf("买卖双方错误: buyer=%s seller=%s", tr.Buyer, tr.Seller)
	}
}

// TestAppendTradesSkipsUnfilled 未成交条目（挂单到达无对手方）不得计入成交流水。
func TestAppendTradesSkipsUnfilled(t *testing.T) {
	pool := NewOrderPool()
	seedOrder(pool, 300, "0x3333333333333333333333333333333333333333", 0, "0", "7")

	mrs := []*match.MatchResult{{
		Id: 3, Symbol: "ETH_USDT", Price: *dec("0"), OrderId: 300,
		Items: []*match.OrderResult{
			// 未成交：state=submitted，AccFilledAmount 是该单总量而非成交量
			{OrderId: 300, Role: "taker", State: "submitted", AccFilledAmount: dec("7")},
		},
	}}
	pool.AppendTrades(mrs)

	if len(pool.trades) != 0 {
		t.Fatalf("未成交不应产生流水，得到 %d 条", len(pool.trades))
	}
}

var _ = decimal.Zero
