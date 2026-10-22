package anubis

// ecies_interop_test.go: 反向互操作 —— 前端（WebCrypto/TS）密文能否被 Go 解密。
// 测试向量由 node 复刻 web/src/lib/crypto/ecies.ts 的 encryptOrder 流程生成，
// 若前端算法与 Go privacy.Decrypt 不同步，本测试会失败。
//
// 重新生成向量：
//   cd web && node <生成脚本> > /tmp/ts_vector.json

import (
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/AnuBookDEX/engine/internal/dex/privacy"
)

// TestTSVectorDecryptsWithGo 若 /tmp/ts_vector.json 存在则验证 TS->Go 互操作。
// 用 t.Skip 而非失败，避免 CI/开发机缺少该文件时阻塞（向量可用 npm script 重新生成）。
func TestTSVectorDecryptsWithGo(t *testing.T) {
	raw, err := os.ReadFile("/tmp/ts_vector.json")
	if err != nil {
		t.Skip("no /tmp/ts_vector.json; generate with the node script to run interop check")
	}
	var v struct {
		NoteCommitment string `json:"note_commitment"`
		EncryptedData  string `json:"encrypted_data"`
		EphemeralPK    string `json:"ephemeral_pk"`
		IV             string `json:"iv"`
		ViewTag        string `json:"view_tag"`
		Nullifier      string `json:"nullifier"`
		Deadline       int64  `json:"deadline"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("parse vector: %v", err)
	}
	dec := func(s string) []byte {
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			t.Fatalf("b64 %q: %v", s, err)
		}
		return b
	}
	eo := &privacy.EncryptedOrder{
		NoteCommitment: dec(v.NoteCommitment),
		EncryptedData:  dec(v.EncryptedData),
		EphemeralPK:    dec(v.EphemeralPK),
		IV:             dec(v.IV),
		ViewTag:        dec(v.ViewTag),
		Nullifier:      dec(v.Nullifier),
		Deadline:       v.Deadline,
	}

	enginePriv, err := parseP256PrivateKey("0x" + strings.Repeat("22", 32))
	if err != nil {
		t.Fatal(err)
	}
	pub := elliptic.MarshalCompressed(enginePriv.PublicKey.Curve, enginePriv.PublicKey.X, enginePriv.PublicKey.Y)
	tag := sha256.Sum256(pub)
	vk := &privacy.ViewKey{PrivateKey: enginePriv, ViewTag: tag[:4]}

	// ViewTag 契约（由公钥派生，发送方可预知）
	if !eo.MatchViewTag(vk.ViewTag) {
		t.Fatalf("view tag mismatch: got %x want %x", eo.ViewTag, vk.ViewTag)
	}
	// note commitment 契约
	h := sha256.New()
	h.Write(eo.Nullifier)
	h.Write(eo.EncryptedData)
	h.Write(eo.EphemeralPK)
	if hex.EncodeToString(eo.NoteCommitment) != hex.EncodeToString(h.Sum(nil)) {
		t.Error("note commitment mismatch (TS sha256(nullifier||ct||ephPk) expected)")
	}

	order, err := privacy.DecryptOrderFromNote(eo, vk)
	if err != nil {
		t.Fatalf("Go cannot decrypt TS ciphertext: %v", err)
	}
	if order.OrderId != 77 {
		t.Errorf("order_id = %d, want 77", order.OrderId)
	}
	if order.Price.String() != "2500" || order.UnfilledAmount.String() != "3" {
		t.Errorf("price/amount = %s/%s, want 2500/3", order.Price, order.UnfilledAmount)
	}
	if order.BuyOrSell != 1 {
		t.Errorf("buy_or_sell = %v, want Sell", order.BuyOrSell)
	}
	t.Logf("TS->Go interop OK: order %d %d @ %s", order.OrderId, order.BuyOrSell, order.Price)
}

