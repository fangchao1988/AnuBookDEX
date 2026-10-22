package anubis

import (
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/AnuBookDEX/engine/internal/dex/privacy"
)

func TestDebugPrivateOrderPrice(t *testing.T) {
	raw, err := os.ReadFile("/tmp/ct_debug.json")
	if err != nil {
		t.Skip("no /tmp/ct_debug.json")
	}
	var eo privacy.EncryptedOrder
	if err := json.Unmarshal(raw, &eo); err != nil {
		t.Fatal(err)
	}
	priv, _ := parseP256PrivateKey("0x" + repeat("22", 32))
	pub := elliptic.MarshalCompressed(priv.PublicKey.Curve, priv.PublicKey.X, priv.PublicKey.Y)
	tag := sha256.Sum256(pub)
	o, err := privacy.DecryptOrderFromNote(&eo, &privacy.ViewKey{PrivateKey: priv, ViewTag: tag[:4]})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("order=%d side=%v price=%s amount=%s", o.OrderId, o.BuyOrSell, o.Price, o.UnfilledAmount)
}

func repeat(s string, n int) string {
	out := make([]byte, 0, n*len(s))
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

var _ = hex.EncodeToString
