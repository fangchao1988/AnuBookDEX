package rocksdb

import (
	"bytes"
	"encoding/gob"
	"testing"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/shopspring/decimal"
)

// 回归测试：gob 快照只编码导出字段（cache 未导出），解码后必须 RebuildCache，
// 否则盘口订单 Find/Dequeue 会 cache miss 触发 Fatal（"cached key is not in cache"）。
// 场景：重启引擎加载快照 → 撮合吃掉盘口订单 → Dequeue 崩溃。
func TestSnapshotRebuildCache(t *testing.T) {
	book := match.InitOrderBook(0, "ALEO_USDCX")
	buy := &match.Order{OrderId: 101, BuyOrSell: match.Buy, State: match.Submitted,
		Price: decimal.NewFromInt(15784), UnfilledAmount: decimal.NewFromInt(80000000)}
	sell := &match.Order{OrderId: 102, BuyOrSell: match.Sell, State: match.Submitted,
		Price: decimal.NewFromInt(16000), UnfilledAmount: decimal.NewFromInt(50000000)}
	book.Enqueue(buy)
	book.Enqueue(sell)

	// gob 编码（等价于 Save 的编码路径：导出字段 Symbol/FromId/BuySet/SellSet）
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(book); err != nil {
		t.Fatalf("encode: %v", err)
	}

	// 解码（等价于 LoadLatest）+ RebuildCache
	decoded := match.NewOrderBook()
	if err := gob.NewDecoder(bytes.NewBuffer(buf.Bytes())).Decode(decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Find(101) != nil || decoded.Find(102) != nil {
		t.Fatalf("cache should be empty before rebuild (gob skips unexported fields)")
	}
	decoded.RebuildCache()

	// 重建后 Find/Dequeue 必须与盘口一致
	for _, id := range []int64{101, 102} {
		if decoded.Find(id) == nil {
			t.Fatalf("order %d not in cache after rebuild", id)
		}
	}
	if decoded.Find(101).Price.Cmp(buy.Price) != 0 || decoded.Find(102).Price.Cmp(sell.Price) != 0 {
		t.Fatalf("price mismatch after roundtrip")
	}
	decoded.Dequeue(101)
	decoded.Dequeue(102)
	if decoded.BuySet.Size() != 0 || decoded.SellSet.Size() != 0 || len(decoded.Cache()) != 0 {
		t.Fatalf("book not empty after dequeue")
	}
}
