package bindings

import "math/big"

// ─── Settlement ────────────────────────────────────────────

// SettlementMatchResult 撮合结果（链上 ABI 格式）
type SettlementMatchResult struct {
	ID        uint64
	OrderId   Hash
	User      Address
	Price     *big.Int
	Amount    *big.Int
	Nullifier Hash
	Role      string
}

// SettlementBatchSettled 批量结算事件
type SettlementBatchSettled struct {
	BatchID      uint64
	StateRoot    Hash
	TotalVolume  *big.Int
	FeeCollected *big.Int
}

// ─── 预编译合约地址 ───────────────────────────────────────

// PrecompileAddrs Anubis Chain 预编译合约（固定地址）
var PrecompileAddrs = struct {
	VerifyProof    Address
	NullifierCheck Address
}{
	VerifyProof:    Address{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00},
	NullifierCheck: Address{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x03},
}

// HexToAddress 辅助：将 hex string 转为 Address
func HexToAddress(hex string) Address {
	var addr Address
	b, _ := hexDecode(hex)
	copy(addr[20-len(b):], b)
	return addr
}

func hexDecode(s string) ([]byte, bool) {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
	}
	if len(s)%2 != 0 {
		return nil, false
	}
	b := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		b[i/2] = hexChar(s[i])<<4 | hexChar(s[i+1])
	}
	return b, true
}

func hexChar(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10
	default:
		return 0
	}
}
