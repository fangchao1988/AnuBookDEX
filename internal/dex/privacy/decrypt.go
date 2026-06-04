package privacy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/core/match"

	jsoniter "github.com/json-iterator/go"
	"github.com/shopspring/decimal"
)

// ViewKey Anubis 视图密钥，用于解密属于该用户的 Note
type ViewKey struct {
	PrivateKey *ecdsa.PrivateKey `json:"private_key"`
	ViewTag    []byte            `json:"view_tag"` // 4-byte tag for fast Note scanning
}

// GenerateViewKey 生成新的 View Key 对
func GenerateViewKey() (*ViewKey, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate view key: %w", err)
	}
	pubKeyBytes := elliptic.MarshalCompressed(priv.PublicKey.Curve, priv.PublicKey.X, priv.PublicKey.Y)
	viewTagHash := sha256.Sum256(pubKeyBytes)

	return &ViewKey{
		PrivateKey: priv,
		ViewTag:    viewTagHash[:4],
	}, nil
}

// DecryptOrderFromNote 使用 View Key 从链上 EncryptedOrder 解密出 match.Order
//
// 工作流程（对应 Anubis 三层扫描优化）：
//   1. ViewTag 快速过滤 — 剔除 >99% 无关 Note
//   2. ECDH 密钥交换恢复共享密钥
//   3. AES-GCM 解密 → 订单明文 JSON
//   4. JSON 反序列化 → match.Order
//   5. Nullifier 本地预检（最终由链上 0x0103 预编译裁决）
func DecryptOrderFromNote(encryptedOrder *EncryptedOrder, viewKey *ViewKey) (*match.Order, error) {
	if !encryptedOrder.MatchViewTag(viewKey.ViewTag) {
		return nil, fmt.Errorf("view tag mismatch")
	}

	plaintext, err := encryptedOrder.Decrypt(viewKey.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt order: %w", err)
	}

	order := &match.Order{
		State:          match.Submitted,
		NoteCommitment: encryptedOrder.NoteCommitment,
		Nullifier:      encryptedOrder.Nullifier,
		Deadline:       encryptedOrder.Deadline,
	}
	if err := unmarshalOrderFields(plaintext, order); err != nil {
		return nil, fmt.Errorf("unmarshal order: %w", err)
	}

	if IsNullifierSpent(encryptedOrder.Nullifier) {
		return nil, fmt.Errorf("duplicate nullifier for order %d", order.OrderId)
	}

	common.Debug("privacy: decrypted order", order.OrderId,
		"nullifier", fmt.Sprintf("%x", encryptedOrder.Nullifier[:8]))
	return order, nil
}

// BatchDecryptOrders 批量解密链上订单，返回成功解密的订单列表和被跳过的数量
func BatchDecryptOrders(encryptedOrders []*EncryptedOrder, viewKey *ViewKey) ([]*match.Order, int) {
	orders := make([]*match.Order, 0, len(encryptedOrders))
	skipped := 0
	for _, eo := range encryptedOrders {
		order, err := DecryptOrderFromNote(eo, viewKey)
		if err != nil {
			skipped++
			continue
		}
		orders = append(orders, order)
	}
	return orders, skipped
}

// unmarshalOrderFields 从加密载荷 JSON 还原订单字段
func unmarshalOrderFields(data []byte, order *match.Order) error {
	type encryptedFields struct {
		OrderId   int64  `json:"order_id"`
		BuyOrSell int    `json:"buy_or_sell"`
		Type      int    `json:"type"`
		Price     string `json:"price"`
		Amount    string `json:"amount"`
		CircuitRt string `json:"circuit_rate"`
		CreateAt  int64  `json:"create_at"`
		Stp       int8   `json:"stp"`
		Deadline  int64  `json:"deadline"`
	}

	var ef encryptedFields
	if err := jsoniter.Unmarshal(data, &ef); err != nil {
		return err
	}

	order.OrderId = ef.OrderId
	order.BuyOrSell = match.OrderBuyOrSell(ef.BuyOrSell)
	order.Type = match.OrderType(ef.Type)
	order.CreateAt = ef.CreateAt
	order.Stp = match.SelfTradeWMType(ef.Stp)
	order.Deadline = ef.Deadline

	order.Price, _ = decimal.NewFromString(ef.Price)
	order.UnfilledAmount, _ = decimal.NewFromString(ef.Amount)
	order.CircuitRate, _ = decimal.NewFromString(ef.CircuitRt)

	return nil
}
