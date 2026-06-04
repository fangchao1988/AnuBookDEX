package privacy

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/AnuBookDEX/engine/internal/infra/common"
)

// EncryptedOrder 加密后的订单载荷
// 链上仅存储密文 + 承诺，链下引擎解密后匹配
type EncryptedOrder struct {
	NoteCommitment []byte `json:"note_commitment"` // Pedersen 承诺
	EncryptedData  []byte `json:"encrypted_data"`  // AES-GCM 密文
	EphemeralPK    []byte `json:"ephemeral_pk"`    // ECDH 临时公钥 (33 bytes compressed)
	IV             []byte `json:"iv"`              // AES-GCM IV (12 bytes)
	ViewTag        []byte `json:"view_tag"`        // 视图标签 (前4字节的密钥哈希)
	Nullifier      []byte `json:"nullifier"`       // 唯一性标识
	Deadline       int64  `json:"deadline"`         // 过期区块号
	Signature      []byte `json:"signature"`        // ECDSA 签名
}

// Encrypt 使用接收方公钥 + ECIES 加密订单数据
// 返回 EncryptedOrder 用于提交到链上
func Encrypt(plaintext []byte, recipientPubKey *ecdsa.PublicKey, nullifier []byte, deadline int64) (*EncryptedOrder, error) {
	// 1. 生成临时 ECDH 密钥对
	ephemeralPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral key: %w", err)
	}

	// 2. ECDH 共享密钥
	sharedX, _ := recipientPubKey.Curve.ScalarMult(recipientPubKey.X, recipientPubKey.Y, ephemeralPriv.D.Bytes())
	sharedSecret := sha256.Sum256(sharedX.Bytes())

	// 3. AES-256-GCM 加密
	block, err := aes.NewCipher(sharedSecret[:])
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	iv := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, fmt.Errorf("generate IV: %w", err)
	}

	ciphertext := aesGCM.Seal(nil, iv, plaintext, nil)

	// 4. 计算 ViewTag（共享密钥前 4 字节，用于客户端快速筛选 Note）
	viewTag := sharedSecret[:4]

	// 5. 生成 Note 承诺：SHA256(nullifier || ciphertext || ephemeralPK)
	h := sha256.New()
	h.Write(nullifier)
	h.Write(ciphertext)
	h.Write(elliptic.MarshalCompressed(ephemeralPriv.PublicKey.Curve, ephemeralPriv.PublicKey.X, ephemeralPriv.PublicKey.Y))
	noteCommitment := h.Sum(nil)

	return &EncryptedOrder{
		NoteCommitment: noteCommitment,
		EncryptedData:  ciphertext,
		EphemeralPK:    elliptic.MarshalCompressed(ephemeralPriv.PublicKey.Curve, ephemeralPriv.PublicKey.X, ephemeralPriv.PublicKey.Y),
		IV:             iv,
		ViewTag:        viewTag,
		Nullifier:      nullifier,
		Deadline:       deadline,
	}, nil
}

// Decrypt 使用接收方私钥解密 EncryptedOrder
func (eo *EncryptedOrder) Decrypt(recipientPrivKey *ecdsa.PrivateKey) ([]byte, error) {
	// 1. 解析临时公钥
	x, y := elliptic.UnmarshalCompressed(elliptic.P256(), eo.EphemeralPK)
	if x == nil {
		return nil, fmt.Errorf("invalid ephemeral public key")
	}

	// 2. ECDH 共享密钥
	sharedX, _ := recipientPrivKey.Curve.ScalarMult(x, y, recipientPrivKey.D.Bytes())
	sharedSecret := sha256.Sum256(sharedX.Bytes())

	// 3. AES-256-GCM 解密
	block, err := aes.NewCipher(sharedSecret[:])
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	plaintext, err := aesGCM.Open(nil, eo.IV, eo.EncryptedData, nil)
	if err != nil {
		common.Warn("privacy: decrypt failed — wrong view key or corrupted data")
		return nil, fmt.Errorf("AES-GCM open: %w", err)
	}

	return plaintext, nil
}

// MatchViewTag 检查 ViewTag 是否匹配（快速过滤非目标 Note）
func (eo *EncryptedOrder) MatchViewTag(viewTag []byte) bool {
	if len(viewTag) != 4 || len(eo.ViewTag) != 4 {
		return false
	}
	for i := 0; i < 4; i++ {
		if viewTag[i] != eo.ViewTag[i] {
			return false
		}
	}
	return true
}

// Sign 使用发送方私钥对加密订单签名
func (eo *EncryptedOrder) Sign(signerPrivKey *ecdsa.PrivateKey) error {
	h := sha256.New()
	h.Write(eo.NoteCommitment)
	h.Write(eo.EncryptedData)
	h.Write(eo.EphemeralPK)
	h.Write(eo.Nullifier)
	digest := h.Sum(nil)

	sig, err := ecdsa.SignASN1(rand.Reader, signerPrivKey, digest)
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}
	eo.Signature = sig
	return nil
}

// Verify 验证加密订单签名
func (eo *EncryptedOrder) Verify(signerPubKey *ecdsa.PublicKey) bool {
	if len(eo.Signature) == 0 {
		return false
	}
	h := sha256.New()
	h.Write(eo.NoteCommitment)
	h.Write(eo.EncryptedData)
	h.Write(eo.EphemeralPK)
	h.Write(eo.Nullifier)
	digest := h.Sum(nil)
	return ecdsa.VerifyASN1(signerPubKey, digest, eo.Signature)
}
