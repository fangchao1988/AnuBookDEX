package anubis

// ecies_vector_test.go: 导出前端 ECIES 的测试向量。
// 用法（供 web 侧测试对齐，非 Go 断言）：
//   go test -run TestEmitEciesVector -v ./internal/dex/chain/anubis/
// 固定 engine pubkey（可预知 ViewTag）+ 明文，输出前端应产出的密文，
// 便于用 node/webcrypto 复现同一流程做交叉验证。

import (
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/AnuBookDEX/engine/internal/dex/privacy"
)

func TestEmitEciesVector(t *testing.T) {
	enginePriv, err := parseP256PrivateKey("0x" + strings.Repeat("22", 32))
	if err != nil {
		t.Fatal(err)
	}
	enginePub := elliptic.MarshalCompressed(enginePriv.PublicKey.Curve, enginePriv.PublicKey.X, enginePriv.PublicKey.Y)
	tag := sha256.Sum256(enginePub)

	nullifier := sha256.Sum256([]byte("order-42"))
	plain := []byte(`{"order_id":42,"buy_or_sell":0,"type":1,"price":"100","amount":"2","circuit_rate":"0","create_at":1790671730268,"stp":0,"deadline":2000000000}`)

	eo, err := privacy.Encrypt(plain, &enginePriv.PublicKey, nullifier[:], 2000000000)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println("=== ECIES TEST VECTOR ===")
	fmt.Println("engine_privkey   : 0x" + strings.Repeat("22", 32))
	fmt.Println("engine_pubkey    : 0x" + hex.EncodeToString(enginePub))
	fmt.Println("engine_view_tag  : 0x" + hex.EncodeToString(tag[:4]))
	fmt.Println("nullifier        : 0x" + hex.EncodeToString(nullifier[:]))
	fmt.Println("ephemeral_pk     : 0x" + hex.EncodeToString(eo.EphemeralPK))
	fmt.Println("iv               : 0x" + hex.EncodeToString(eo.IV))
	fmt.Println("encrypted_data   : 0x" + hex.EncodeToString(eo.EncryptedData))
	fmt.Println("note_commitment  : 0x" + hex.EncodeToString(eo.NoteCommitment))
	fmt.Println("view_tag         : 0x" + hex.EncodeToString(eo.ViewTag))
	fmt.Println("=== END ===")

	// 断言：ViewTag 必须等于由公钥派生的值（前端不需解密即可预知）
	if !eo.MatchViewTag(tag[:4]) {
		t.Fatal("view tag must equal sha256(engine_pubkey)[:4]")
	}
}
