package anubis

import (
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/AnuBookDEX/engine/internal/core/match"
)

// makePlainOrderLog 构造一条 OrderSubmitted 事件日志（按真实 ABI）
// topics[1]=noteCommitment(bytes32), topics[2]=viewTag(bytes4 右对齐), topics[3]=submitter(address 右对齐)
// data=nullifier(bytes32) + deadline(uint64 右对齐)
func makePlainOrderLog() rpcLog {
	nc := make([]byte, 32)
	for i := range nc {
		nc[i] = 0xaa
	}
	viewTag := make([]byte, 32) // viewTag 在 topic[28:32]
	viewTag[28], viewTag[29], viewTag[30], viewTag[31] = 0x01, 0x02, 0x03, 0x04
	submitter := make([]byte, 32) // address 右对齐 topic[12:32]
	for i := 12; i < 32; i++ {
		submitter[i] = 0xcc
	}
	data := make([]byte, 64)
	for i := 0; i < 32; i++ {
		data[i] = 0xbb // nullifier
	}
	binary.BigEndian.PutUint64(data[56:64], 8500000) // deadline

	return rpcLog{
		Topics: []string{
			"0x942d46dddbdc36d1ed575e5093656f2952053568a7867ea0aaf449ace306f03c",
			"0x" + hex.EncodeToString(nc),
			"0x" + hex.EncodeToString(viewTag),
			"0x" + hex.EncodeToString(submitter),
		},
		Data: "0x" + hex.EncodeToString(data),
	}
}

// TestDecodePlainOrderABI 验证按真实 ABI 从日志解析 noteCommitment/nullifier/deadline
func TestDecodePlainOrderABI(t *testing.T) {
	lg := makePlainOrderLog()
	s := &Subscriber{}
	order := s.decodePlainOrder(lg)
	if order == nil {
		t.Fatal("expected non-nil order")
	}
	if order.Deadline != 8500000 {
		t.Errorf("Deadline = %d, want 8500000", order.Deadline)
	}
	if len(order.NoteCommitment) != 32 || order.NoteCommitment[0] != 0xaa {
		t.Errorf("NoteCommitment mismatch: %x", order.NoteCommitment)
	}
	if len(order.Nullifier) != 32 || order.Nullifier[0] != 0xbb {
		t.Errorf("Nullifier mismatch: %x", order.Nullifier)
	}
	if order.State != match.Submitted {
		t.Errorf("State = %v, want Submitted", order.State)
	}
}

// TestDecodePlainOrderShortData 验证 data 不足时不崩溃，仍返回含 noteCommitment 的 order
func TestDecodePlainOrderShortData(t *testing.T) {
	lg := rpcLog{
		Topics: []string{
			"0x" + strings.Repeat("c", 64),
			"0x" + strings.Repeat("aa", 32),
			"0x" + strings.Repeat("00", 32),
			"0x" + strings.Repeat("cc", 32),
		},
		Data: "0x" + hex.EncodeToString([]byte("short")),
	}
	s := &Subscriber{}
	order := s.decodePlainOrder(lg)
	if order == nil {
		t.Fatal("expected non-nil order for short data")
	}
	if len(order.NoteCommitment) != 32 {
		t.Errorf("NoteCommitment should still be parsed from topics")
	}
}

// TestDecodeEncryptedOrderABI 验证隐私模式按真实 ABI 解码 EncryptedOrder
func TestDecodeEncryptedOrderABI(t *testing.T) {
	lg := makePlainOrderLog()
	s := &Subscriber{}
	eo := s.decodeEncryptedOrder(lg)
	if eo == nil {
		t.Fatal("expected non-nil EncryptedOrder")
	}
	if len(eo.NoteCommitment) != 32 || eo.NoteCommitment[0] != 0xaa {
		t.Errorf("NoteCommitment mismatch: %x", eo.NoteCommitment)
	}
	if len(eo.ViewTag) != 4 || eo.ViewTag[0] != 0x01 || eo.ViewTag[3] != 0x04 {
		t.Errorf("ViewTag mismatch: %x (want 01020304)", eo.ViewTag)
	}
	if len(eo.Nullifier) != 32 || eo.Nullifier[0] != 0xbb {
		t.Errorf("Nullifier mismatch: %x", eo.Nullifier)
	}
	if eo.Deadline != 8500000 {
		t.Errorf("Deadline = %d, want 8500000", eo.Deadline)
	}
}
