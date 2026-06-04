package privacy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"sync"

	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/core/match"
)

// PrivacyTier 隐私等级
type PrivacyTier int

const (
	// TierAnonymous 完全匿名：小额交易，无需身份验证，使用 Type 101 (Transfer)
	TierAnonymous PrivacyTier = iota
	// TierPseudonymous 假名：中等交易，链上可见地址但价格/数量加密
	TierPseudonymous
	// TierZKVerified ZK-KYC 验证：大额交易，使用 Type 103 + ZK 身份证明
	TierZKVerified
	// TierCompliant 合规披露：机构级交易，满足 FATF/MiCA/DAC8 报告要求
	TierCompliant
)

func (t PrivacyTier) String() string {
	switch t {
	case TierAnonymous:
		return "anonymous"
	case TierPseudonymous:
		return "pseudonymous"
	case TierZKVerified:
		return "zk-verified"
	case TierCompliant:
		return "compliant"
	default:
		return "unknown"
	}
}

// PrivacyLevel 分级隐私配置
type PrivacyLevel struct {
	// 阈值（以 ETH 计）
	AnonymousThreshold    *big.Float // 低于此值完全匿名
	PseudonymousThreshold *big.Float // 低于此值假名
	ZKVerifiedThreshold    *big.Float // 低于此值 ZK-KYC，高于此值合规披露
}

// DefaultPrivacyLevel 默认隐私分级
// 待 Anubis ZK-KYC 框架正式发布后从链上 Registry SC 拉取
func DefaultPrivacyLevel() *PrivacyLevel {
	return &PrivacyLevel{
		AnonymousThreshold:    big.NewFloat(0.1),  // < 0.1 ETH: 完全匿名
		PseudonymousThreshold: big.NewFloat(1.0),  // < 1 ETH: 假名
		ZKVerifiedThreshold:   big.NewFloat(100.0), // < 100 ETH: ZK-KYC
		// >= 100 ETH: 合规披露
	}
}

// ZKKYCVerifier ZK-KYC 验证器
// 分段隐私：小额隐私自由，大额自动触发 ZK-KYC
type ZKKYCVerifier struct {
	mu            sync.RWMutex
	levels        *PrivacyLevel
	verifiedUsers map[string]*KYCProof // userAddress -> KYC proof
}

// KYCProof ZK-KYC 证明
type KYCProof struct {
	UserAddress   string `json:"user_address"`
	CountryCode   string `json:"country_code"`   // ISO 3166-1 alpha-2
	IsVerified    bool   `json:"is_verified"`
	VerificationTS int64 `json:"verification_ts"`
	ProofHash     []byte `json:"proof_hash"`     // ZK 证明哈希
	ExpiryBlock   int64  `json:"expiry_block"`   // 证明过期区块
}

// NewZKKYCVerifier 创建 ZK-KYC 验证器
func NewZKKYCVerifier(levels *PrivacyLevel) *ZKKYCVerifier {
	if levels == nil {
		levels = DefaultPrivacyLevel()
	}
	return &ZKKYCVerifier{
		levels:        levels,
		verifiedUsers: make(map[string]*KYCProof),
	}
}

// ClassifyOrder 根据订单金额判定隐私等级
func (v *ZKKYCVerifier) ClassifyOrder(order *match.Order) PrivacyTier {
	valueETH := orderPriceInETH(order)

	switch {
	case valueETH.Cmp(v.levels.AnonymousThreshold) < 0:
		return TierAnonymous
	case valueETH.Cmp(v.levels.PseudonymousThreshold) < 0:
		return TierPseudonymous
	case valueETH.Cmp(v.levels.ZKVerifiedThreshold) < 0:
		return TierZKVerified
	default:
		return TierCompliant
	}
}

// RequireKYC 判断是否需要 KYC 验证
func (v *ZKKYCVerifier) RequireKYC(order *match.Order) bool {
	tier := v.ClassifyOrder(order)
	return tier == TierZKVerified || tier == TierCompliant
}

// RegisterKYCProof 注册用户的 KYC 证明
func (v *ZKKYCVerifier) RegisterKYCProof(userAddress string, proof *KYCProof) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.verifiedUsers[userAddress] = proof
	common.Info("privacy: KYC proof registered for", userAddress,
		"country:", proof.CountryCode, "expiry:", proof.ExpiryBlock)
}

// IsKYCVerified 检查用户是否已通过 KYC
func (v *ZKKYCVerifier) IsKYCVerified(userAddress string, currentBlock int64) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()

	proof, ok := v.verifiedUsers[userAddress]
	if !ok {
		return false
	}
	if !proof.IsVerified {
		return false
	}
	if proof.ExpiryBlock > 0 && currentBlock > proof.ExpiryBlock {
		return false
	}
	return true
}

// SanctionCheck 合规制裁检查（AML/FATF）
// 返回 true 表示该地址未被制裁
func (v *ZKKYCVerifier) SanctionCheck(userAddress string) bool {
	// MVP: 不进行实际制裁检查
	// 生产阶段接入链上制裁名单 SC 或外部 AML 服务
	return true
}

// AuditLogEntry 合规审计条目（不泄露交易细节）
type AuditLogEntry struct {
	Timestamp   int64       `json:"timestamp"`
	BlockNumber int64       `json:"block_number"`
	PrivacyTier PrivacyTier `json:"privacy_tier"`
	TxnHash     string      `json:"txn_hash"`
	// 不包含：交易价格、数量、交易对
}

// GenerateAuditLog 生成合规审计条目
func (v *ZKKYCVerifier) GenerateAuditLog(order *match.Order, tier PrivacyTier) *AuditLogEntry {
	entry := &AuditLogEntry{
		Timestamp:   order.CreateAt,
		BlockNumber: order.BlockNumber,
		PrivacyTier: tier,
	}
	if len(order.TxHash) > 0 {
		entry.TxnHash = order.TxHash
	} else {
		// 生成审计用交易哈希
		data, _ := json.Marshal(order)
		h := sha256.Sum256(data)
		entry.TxnHash = fmt.Sprintf("%x", h[:16])
	}
	return entry
}

// orderPriceInETH 估算订单的 ETH 价值
func orderPriceInETH(order *match.Order) *big.Float {
	value := new(big.Float).SetInt(order.Price.BigInt())
	amount := new(big.Float).SetInt(order.UnfilledAmount.BigInt())
	ethValue := new(big.Float).Mul(value, amount)
	// 假设 18 位精度，除以 10^18
	divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	return new(big.Float).Quo(ethValue, divisor)
}
