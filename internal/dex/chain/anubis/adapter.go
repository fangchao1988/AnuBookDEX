package anubis

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	ethcommon "github.com/ethereum/go-ethereum/common"

	"github.com/AnuBookDEX/engine/internal/dex/privacy"
	"github.com/AnuBookDEX/engine/internal/dex/chain"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
)

// adapter 聚合 Anubis 链后端，实现 chain.ChainAdapter。
// 订单源 = 链下订单池（Subscriber 持有 OrderPool）；结算 = Settlement → Settlement v2 SC。
type adapter struct {
	sub   *Subscriber
	pool  *OrderPool
	settl *Settlement
	evm   *EVMClient
}

// NewAdapter 从 chain.anubis.* 配置构造 Anubis 链后端。
func NewAdapter() chain.ChainAdapter {
	rpc := config.GetString("chain.anubis.rpc-endpoint", "http://localhost:8545")

	pool := NewOrderPool()
	sub := NewSubscriber(rpc, pool)
	sub.SetContractAddr(config.GetString("chain.anubis.registry-contract", ""))

	// 配置地址校验（EIP-55）：钱包会拒绝错误校验和，提前暴露
	checkChecksummed("chain.anubis.settlement-contract", "Settlement")
	checkChecksummed("chain.anubis.registry-contract", "Registry")

	priv := operatorPrivateKey()
	if priv == "" {
		common.Warn("anubis operator private key empty - export ANUBIS_PRIVATE_KEY_METAMASK (or ANUBIS_PRIVATE_KEY)")
	}
	chainID := big.NewInt(int64(config.GetInt("chain.anubis.chain-id", 0)))

	var evm *EVMClient
	if priv != "" {
		var err error
		evm, err = NewEVMClient(rpc)
		if err != nil {
			common.Warn("anubis evm client init failed:", err)
		}
	}
	settl := NewSettlement(
		rpc,
		config.GetString("chain.anubis.settlement-contract", ""),
		priv,
		chainID,
		evm,
	)
	settl.SetPool(pool) // 成交明细写入订单池（/api/v1/trades）
	if evm != nil {
		settl.SetClient(evm)
		// view-key 解密（Phase 2 密文订单）
		if vk := buildViewKey(); vk != nil {
			sub.SetViewKey(vk)
		}
	}

	return &adapter{sub: sub, pool: pool, settl: settl, evm: evm}
}

// checkChecksummed 校验配置里的 EVM 地址是否符合 EIP-55 校验和。
// go-ethereum 的 HexToAddress 不校验大小写，地址写错也能正常调用链上——
// 但 MetaMask 等钱包会拒绝错误校验和的地址，导致前端入金/签名静默失败。
// 此处只告警（引擎可继续工作），并给出正确写法。
func checkChecksummed(key, label string) {
	raw := config.GetString(key, "")
	if raw == "" {
		return
	}
	if !ethcommon.IsHexAddress(raw) {
		common.Warn("anubis config: invalid address", "key", key, "label", label, "value", raw)
		return
	}
	// 全小写/全大写视为未做校验和（EIP-55 允许），仅混合大小写才比对
	body := strings.TrimPrefix(raw, "0x")
	if body == strings.ToLower(body) || body == strings.ToUpper(body) {
		return
	}
	want := ethcommon.HexToAddress(raw).Hex()
	if raw != want {
		common.Warn("anubis config: EIP-55 checksum mismatch — 钱包可能拒绝该地址",
			"key", key, "label", label, "got", raw, "want", want)
	}
}

// operatorPrivateKey 读取 operator 结算私钥：
// 优先 chain.anubis.metamask-private-key（ANUBIS_PRIVATE_KEY_METAMASK，测试网充值账户），
// 回落 chain.anubis.private-key（ANUBIS_PRIVATE_KEY）。
func operatorPrivateKey() string {
	if k := config.GetString("chain.anubis.metamask-private-key", ""); k != "" {
		return k
	}
	return config.GetString("chain.anubis.private-key", "")
}

// buildViewKey 从 chain.anubis.view-key-private 构造 privacy.ViewKey。
// 密钥曲线必须与 privacy.Encrypt/Decrypt 的 P-256 ECIES 一致（不可用 secp256k1
// 的 crypto.HexToECDSA —— 曲线不匹配会导致 ECDH 得到无效点）。
// ViewTag 由公钥派生（SHA256(compressedPub)[:4]，稳定可预知），前端据此筛选密文。
func buildViewKey() *privacy.ViewKey {
	vkHex := config.GetString("chain.anubis.view-key-private", "")
	if vkHex == "" {
		return nil
	}
	priv, err := parseP256PrivateKey(vkHex)
	if err != nil {
		common.Warn("build view key failed:", err)
		return nil
	}
	pubBytes := elliptic.MarshalCompressed(priv.PublicKey.Curve, priv.PublicKey.X, priv.PublicKey.Y)
	tag := sha256.Sum256(pubBytes)
	return &privacy.ViewKey{PrivateKey: priv, ViewTag: tag[:4]}
}

// parseP256PrivateKey 解析 hex 私钥（D 坐标，32B）为 P-256 ecdsa 私钥。
// 与 secp256k1 的 crypto.HexToECDSA 不同：这里显式指定 P-256 曲线。
func parseP256PrivateKey(hexKey string) (*ecdsa.PrivateKey, error) {
	s := strings.TrimPrefix(strings.TrimSpace(hexKey), "0x")
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode hex: %w", err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("expect 32-byte private key, got %d", len(b))
	}
	d := new(big.Int).SetBytes(b)
	curve := elliptic.P256()
	if d.Sign() == 0 || d.Cmp(curve.Params().N) >= 0 {
		return nil, fmt.Errorf("private key out of P-256 range")
	}
	priv := &ecdsa.PrivateKey{D: d}
	priv.PublicKey.Curve = curve
	priv.PublicKey.X, priv.PublicKey.Y = curve.ScalarBaseMult(b)
	return priv, nil
}

func (a *adapter) Name() string                    { return "anubis" }
func (a *adapter) Orders() chain.OrderSource       { return a.sub }
func (a *adapter) Settlement() chain.SettlementSink { return a.settl }
func (a *adapter) Pool() *OrderPool                { return a.pool }
func (a *adapter) ViewKey() *privacy.ViewKey       { return a.sub.viewKey }
func (a *adapter) EVM() *EVMClient                 { return a.evm }

// 编译期接口断言：确保 Anubis 后端满足链无关接口契�约。
var (
	_ chain.ChainAdapter   = (*adapter)(nil)
	_ chain.OrderSource    = (*Subscriber)(nil)
	_ chain.SettlementSink = (*Settlement)(nil)
)
