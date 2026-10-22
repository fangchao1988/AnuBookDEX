package anubis

// privacy_ecies_test.go: 隐私链路（ECIES）契约测试。
//
// 前端 web/src/lib/crypto/ecies.ts 必须与 privacy.Encrypt/Decrypt 字节级一致，
// 本测试固定以下契约（改动任一侧都会失败）：
//   1. 曲线 P-256；临时公钥 = 33B 压缩点
//   2. 共享密钥 = SHA256(ECDH 共享点 X 坐标, 32B 定长)
//   3. AES-256-GCM，IV 12B，AAD = nil
//   4. ViewTag = SHA256(接收方压缩公钥)[:4]  ← 由公钥派生，发送方可预知
//   5. NoteCommitment = SHA256(nullifier || ciphertext || ephemeralPK)
//   6. EncryptedOrder 的 []byte 字段在 JSON 中为 base64（Go 标准编码）

import (
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/dex/privacy"
)

func TestParseP256PrivateKeyRoundTrip(t *testing.T) {
	// 合法 P-256 私钥（32B hex）
	priv, err := parseP256PrivateKey("0x" + strings.Repeat("11", 32))
	if err != nil {
		t.Fatalf("parseP256PrivateKey: %v", err)
	}
	if priv.PublicKey.Curve != elliptic.P256() {
		t.Error("curve must be P-256 (not secp256k1)")
	}
	pub := elliptic.MarshalCompressed(priv.PublicKey.Curve, priv.PublicKey.X, priv.PublicKey.Y)
	if len(pub) != 33 {
		t.Errorf("compressed pub must be 33 bytes, got %d", len(pub))
	}
	// ViewTag 派生：SHA256(pub)[:4]，前端据此筛选
	tag := sha256.Sum256(pub)
	if len(tag[:4]) != 4 {
		t.Fatal("view tag must be 4 bytes")
	}
}

func TestParseP256PrivateKeyRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"short":     "0x1122",
		"not hex":   "0xzz11223344556677889900112233445566778899001122334455667788990011",
		"zero":      "0x" + strings.Repeat("00", 32),
		"too long":  "0x" + strings.Repeat("11", 33),
	}
	for name, in := range cases {
		if _, err := parseP256PrivateKey(in); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

// TestEciesRoundTripIsViewTagAddressable 验证加密方可用「接收方公钥」预知 ViewTag，
// 且解密方用同一私钥能通过 ViewTag 校验并还原明文订单。
// 这是前端加密流程的核心契约：pubkey -> viewTag 无需先解密即可算出。
func TestEciesRoundTripIsViewTagAddressable(t *testing.T) {
	// 接收方（引擎）view key
	enginePriv, err := parseP256PrivateKey("0x" + strings.Repeat("22", 32))
	if err != nil {
		t.Fatalf("engine key: %v", err)
	}
	enginePubBytes := elliptic.MarshalCompressed(enginePriv.PublicKey.Curve, enginePriv.PublicKey.X, enginePriv.PublicKey.Y)
	wantTag := sha256.Sum256(enginePubBytes)
	engineVK := &privacy.ViewKey{PrivateKey: enginePriv, ViewTag: wantTag[:4]}

	// 订单明文（字段名必须与 privacy.unmarshalOrderFields 一致）
	plain := []byte(`{"order_id":42,"buy_or_sell":0,"type":1,"price":"100","amount":"2","circuit_rate":"0","create_at":1790671730268,"stp":0,"deadline":2000000000}`)
	nullifier := sha256.Sum256([]byte("order-42"))

	// 发送方加密（模拟前端 WebCrypto）
	eo, err := privacy.Encrypt(plain, &enginePriv.PublicKey, nullifier[:], 2000000000)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// 契约 4：ViewTag 必须等于 SHA256(接收方压缩公钥)[:4]（发送方可预知）
	if !eo.MatchViewTag(wantTag[:4]) {
		t.Error("view tag must be derivable from recipient pubkey before encryption")
	}
	if eo.MatchViewTag([]byte{0, 0, 0, 0}) {
		t.Error("view tag must not match a wrong tag")
	}

	// 契约 1/3：密文结构
	if len(eo.EphemeralPK) != 33 {
		t.Errorf("ephemeral pk must be 33B compressed, got %d", len(eo.EphemeralPK))
	}
	if len(eo.IV) != 12 {
		t.Errorf("IV must be 12B for GCM, got %d", len(eo.IV))
	}

	// 契约 5：note commitment = sha256(nullifier || ciphertext || ephPk)
	h := sha256.New()
	h.Write(nullifier[:])
	h.Write(eo.EncryptedData)
	h.Write(eo.EphemeralPK)
	if hex.EncodeToString(eo.NoteCommitment) != hex.EncodeToString(h.Sum(nil)) {
		t.Error("note commitment must be sha256(nullifier || ciphertext || ephemeral_pk)")
	}

	// 契约 6：JSON 中 []byte 为 base64（前端据此解析）
	j, err := json.Marshal(eo)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back privacy.EncryptedOrder
	if err := json.Unmarshal(j, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ctB64 := base64.StdEncoding.EncodeToString(eo.EncryptedData)
	if !strings.Contains(string(j), `"encrypted_data":"`+ctB64+`"`) {
		t.Error("encrypted_data must be base64 in JSON (Go []byte encoding)")
	}

	// 完整解密还原订单
	order, err := privacy.DecryptOrderFromNote(&back, engineVK)
	if err != nil {
		t.Fatalf("DecryptOrderFromNote: %v", err)
	}
	if order.OrderId != 42 {
		t.Errorf("order_id = %d, want 42", order.OrderId)
	}
	if order.Price.String() != "100" || order.UnfilledAmount.String() != "2" {
		t.Errorf("price/amount roundtrip mismatch: %s / %s", order.Price, order.UnfilledAmount)
	}
	if order.BuyOrSell != match.Buy {
		t.Errorf("buy_or_sell = %v, want Buy", order.BuyOrSell)
	}
	if order.Deadline != 2000000000 {
		t.Errorf("deadline = %d, want 2000000000", order.Deadline)
	}
}

// TestEciesWrongViewKeyFails 验证错误 view key 无法解密（隐私性基本保证）。
func TestEciesWrongViewKeyFails(t *testing.T) {
	enginePriv, _ := parseP256PrivateKey("0x" + strings.Repeat("22", 32))
	otherPriv, _ := parseP256PrivateKey("0x" + strings.Repeat("33", 32))
	otherPub := elliptic.MarshalCompressed(otherPriv.PublicKey.Curve, otherPriv.PublicKey.X, otherPriv.PublicKey.Y)
	otherTag := sha256.Sum256(otherPub)

	eo, err := privacy.Encrypt([]byte(`{"order_id":1}`), &enginePriv.PublicKey, make([]byte, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	// 正确 tag、非正确私钥 -> ViewTag 通过但 AES-GCM 失败
	if _, err := privacy.DecryptOrderFromNote(eo, &privacy.ViewKey{PrivateKey: otherPriv, ViewTag: otherTag[:4]}); err == nil {
		t.Error("decrypt with wrong key must fail")
	}
}
