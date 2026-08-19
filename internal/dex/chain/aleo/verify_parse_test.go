package aleo

import "testing"

// TestParseRecordPlaintextJSONWrapper 回归：GET /mapping 返回 JSON string wrapper
// （最外层 " + 字面 \n），必须先 unquoteJSON 还原真实换行，parseRecordPlaintext
// 才能解析出 status 字段。此前未剥离导致 status 缺失 → 误判结算未生效。
func TestParseRecordPlaintextJSONWrapper(t *testing.T) {
	raw := "\"{\\n  user: aleo1cwzattgxzw7kxklq0rp0dxntn27yf9qftjk2jrk5qv73r4lkngxqvhsw9v,\\n  operator: aleo16dwwdmqqwft7hugdelwx6d8g39xlqwsath8u3kxrhztw4re4xv9qhe7sal,\\n  side: 1u8,\\n  price: 16000u64,\\n  amount: 1000000u64,\\n  deadline: 1787037223u32,\\n  status: 1u8\\n}\""
	got := parseRecordPlaintext(unquoteJSON(raw))
	if got["status"] != 1 {
		t.Fatalf("status=%d want 1（unquoteJSON 后应解析出 status）", got["status"])
	}
	if got["price"] != 16000 {
		t.Fatalf("price=%d want 16000", got["price"])
	}
}