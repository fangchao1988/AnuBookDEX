package privacy

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
)

// Nullifier 防双花标识生成与校验
// 每个订单绑定唯一 Nullifier，引擎维护已消费 Nullifier 集合

var (
	spentNullifiers   = make(map[string]bool)
	nullifierMu       sync.RWMutex
)

// GenerateNullifier 生成订单唯一 Nullifier
// 输入：用户地址 + 随机盐 + 订单数据哈希
// Nullifier = SHA256(userAddress || salt || orderDataHash)
// 该 Nullifier 在 Settlement SC 的 0x0103 预编译中校验唯一性
func GenerateNullifier(userAddress string, salt []byte, orderDataHash []byte) ([]byte, error) {
	if len(salt) == 0 {
		salt = make([]byte, 32)
		if _, err := rand.Read(salt); err != nil {
			return nil, fmt.Errorf("generate salt: %w", err)
		}
	}

	h := sha256.New()
	h.Write([]byte(userAddress))
	h.Write(salt)
	h.Write(orderDataHash)
	return h.Sum(nil), nil
}

// CheckAndMarkNullifier 检查 Nullifier 是否已消费，未消费则标记
// 本地检查（链上通过 0x0103 预编译做最终裁决）
func CheckAndMarkNullifier(nullifier []byte) (bool, string) {
	key := hex.EncodeToString(nullifier)

	nullifierMu.Lock()
	defer nullifierMu.Unlock()

	if spentNullifiers[key] {
		return false, ""
	}
	spentNullifiers[key] = true
	return true, key
}

// IsNullifierSpent 检查 Nullifier 是否已消费（只读）
func IsNullifierSpent(nullifier []byte) bool {
	key := hex.EncodeToString(nullifier)

	nullifierMu.RLock()
	defer nullifierMu.RUnlock()

	return spentNullifiers[key]
}

// NullifierSetSize 返回已消费 Nullifier 数量（用于监控）
func NullifierSetSize() int {
	nullifierMu.RLock()
	defer nullifierMu.RUnlock()
	return len(spentNullifiers)
}
