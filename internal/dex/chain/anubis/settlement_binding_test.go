package anubis

// settlement_binding_test.go: Settlement v2 ABI 打包/解析单测。
// 回归重点：tuple 字段名与 go-ethereum 的 ToCamelCase 匹配（曾因 ID/Id 不匹配导致 abi pack 失败，
// 结算静默停在 queued 而不上链）。

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// TestKeccakABIEncodeMatchesSolidsity 验证 keccakABIEncode 能打包 results（此前因字段名不匹配 panic/error）。
func TestKeccakABIEncodeMatchesSolidsity(t *testing.T) {
	results := []SettlementMatchV3{{
		ID:         1,
		OrderId:    crypto.Keccak256Hash([]byte("order-1")),
		User:       common.HexToAddress("0x1111111111111111111111111111111111111111"),
		BaseToken:  common.HexToAddress("0xaaaa111111111111111111111111111111111111"),
		QuoteToken: common.HexToAddress("0xbbbb111111111111111111111111111111111111"),
		Price:      big.NewInt(100),
		Amount:     big.NewInt(2),
		Cost:       big.NewInt(200),
		Side:       0,
		Nullifier:  crypto.Keccak256Hash([]byte("null-1")),
		Role:       "maker",
	}}
	h := keccakABIEncode(results)
	if h == (common.Hash{}) {
		t.Fatal("keccakABIEncode returned zero hash — pack failed")
	}
}

// TestCommitmentRootChainsPrevRoot 验证 newRoot 随 prevRoot 变化（v2 hash 模式链式状态根）。
func TestCommitmentRootChainsPrevRoot(t *testing.T) {
	results := []SettlementMatchV3{{
		ID: 1, OrderId: crypto.Keccak256Hash([]byte("o")), User: common.HexToAddress("0x1"),
		BaseToken: common.HexToAddress("0xaaaa111111111111111111111111111111111111"),
		QuoteToken: common.HexToAddress("0xbbbb111111111111111111111111111111111111"),
		Price: big.NewInt(100), Amount: big.NewInt(1), Cost: big.NewInt(100),
		Nullifier: crypto.Keccak256Hash([]byte("n")), Role: "taker",
	}}
	zero := commitmentRoot(results, common.Hash{})
	other := commitmentRoot(results, common.HexToHash("0xdeadbeef"))
	if zero == other {
		t.Error("commitmentRoot must differ when prevRoot differs")
	}
}

// TestEncodeSubmitBatchSelectorAndLength 验证 calldata 前 4 字节为 submitBatch selector。
func TestEncodeSubmitBatchSelectorAndLength(t *testing.T) {
	results := []SettlementMatchV3{{
		ID: 7, OrderId: crypto.Keccak256Hash([]byte("o7")), User: common.HexToAddress("0xabc"),
		BaseToken: common.HexToAddress("0xaaaa111111111111111111111111111111111111"),
		QuoteToken: common.HexToAddress("0xbbbb111111111111111111111111111111111111"),
		Price: big.NewInt(68000), Amount: big.NewInt(1500000), Cost: big.NewInt(102000000000),
		Nullifier: crypto.Keccak256Hash([]byte("n7")), Role: "maker",
	}}
	root := crypto.Keccak256Hash([]byte("root"))
	data, err := encodeSubmitBatch(results, []byte("anubis-hash-mvp"), root)
	if err != nil {
		t.Fatalf("encodeSubmitBatch: %v", err)
	}
	want := settleContractAB.Methods["submitBatch"].ID
	if len(data) < 4 || data[0] != want[0] || data[1] != want[1] || data[2] != want[2] || data[3] != want[3] {
		t.Errorf("selector mismatch: got %x want %x", data[:4], want)
	}
	// tuple[] 动态编码：单元素 results 的 calldata 应显著长于头部
	if len(data) <= 4+32*3 {
		t.Errorf("calldata too short (%d bytes) — tuple[] likely not encoded", len(data))
	}
}
