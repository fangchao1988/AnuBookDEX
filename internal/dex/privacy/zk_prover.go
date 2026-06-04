package privacy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/core/match"
)

// CircuitInput ZK 证明电路的公开输入
// 对应 Anubis Settlement SC 中 0x0100 预编译的输入格式
type CircuitInput struct {
	BatchID        uint64   `json:"batch_id"`
	StateRootBefore []byte  `json:"state_root_before"` // 撮合前状态根
	StateRootAfter  []byte  `json:"state_root_after"`  // 撮合后状态根
	MatchResultHash []byte  `json:"match_result_hash"` // 撮合结果哈希
	Nullifiers      [][]byte `json:"nullifiers"`        // 本批次所有 Nullifier
	PublicInputs    [][]byte `json:"public_inputs"`     // 额外公开输入
}

// ZKProof PLONK 零知识证明
type ZKProof struct {
	Proof     []byte `json:"proof"`     // PLONK 证明数据
	Inputs    []byte `json:"inputs"`    // 公开输入（ABI 编码）
	CircuitID string `json:"circuit_id"` // 电路标识
}

// ZKProver ZK 证明生成器
// MVP 阶段使用哈希承诺代替完整 ZK 证明，生产阶段替换为 gnark
type ZKProver struct {
	CircuitID string // 电路标识（公开）
}

// NewZKProver 创建 ZK 证明生成器
func NewZKProver() *ZKProver {
	return &ZKProver{
		CircuitID: "match_correctness_v1",
	}
}

// GenerateMatchProof 为撮合结果生成 PLONK 证明
// 证明内容：给定撮合前状态和撮合后状态，匹配算法按照价格-时间优先规则正确执行
//
// MVP 实现：使用 SHA256 承诺代替 ZK 证明
// 生产实现：使用 gnark 生成 PLONK 证明 → 提交到 0x0100 预编译验证
func (p *ZKProver) GenerateMatchProof(batchID uint64, mrs []*match.MatchResult) (*ZKProof, error) {
	// 1. 计算撮合结果哈希
	mrData, err := json.Marshal(mrs)
	if err != nil {
		return nil, fmt.Errorf("marshal match results: %w", err)
	}
	mrHash := sha256.Sum256(mrData)

	// 2. 收集所有 Nullifier
	nullifiers := make([][]byte, 0, len(mrs))
	for _, mr := range mrs {
		for _, item := range mr.Items {
			nullifiers = append(nullifiers, []byte(item.Taker))
		}
	}

	// 3. 构造电路输入
	input := CircuitInput{
		BatchID:        batchID,
		MatchResultHash: mrHash[:],
		Nullifiers:      nullifiers,
	}

	inputData, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal circuit input: %w", err)
	}

	// MVP: 使用哈希作为"证明"占位
	// 生产阶段替换为：
	//   circuit := frontend.NewCS(...)
	//   witness := &MatchCircuit{...}
	//   proof, err := groth16.Prove(circuit, pk, witness)
	proofData := sha256.Sum256(inputData)

	common.Debug("privacy: generated ZK proof for batch", batchID,
		"with", len(mrs), "match results (MVP hash mode)")

	return &ZKProof{
		Proof:     proofData[:],
		Inputs:    inputData,
		CircuitID: p.CircuitID,
	}, nil
}

// VerifyMatchProof 验证撮合证明（本地预检，最终由链上 0x0100 验证）
func (p *ZKProver) VerifyMatchProof(proof *ZKProof) (bool, error) {
	// MVP: 仅验证结构完整性
	if len(proof.Proof) == 0 {
		return false, fmt.Errorf("empty proof")
	}
	if len(proof.Inputs) == 0 {
		return false, fmt.Errorf("empty inputs")
	}

	// 生产阶段替换为：
	//   err := groth16.Verify(proof.Proof, vk, publicWitness)
	//   return err == nil, err
	return true, nil
}

// MatchCircuit gnark 电路结构（生产阶段实现）
// 该电路证明以下约束：
//   1. 对于每个撮合对 (taker, maker)，maker 的订单在 taker 之前存在于订单簿
//   2. 撮合价格 = maker 的挂单价格
//   3. 撮合数量 = min(taker.unfilled, maker.unfilled)
//   4. 撮合后双方状态正确更新（成交/部分成交/撤单）
//   5. 未成交的 taker 订单（如果是限价单）正确挂入订单簿
//
// type MatchCircuit struct {
//     BatchID       frontend.Variable
//     TakerOrder    OrderCircuit  // taker 订单（私有输入）
//     MakerOrders   []OrderCircuit // maker 订单列表（私有输入）
//     MatchResults  []MatchResultCircuit // 撮合结果（公开输入）
//     StateRootPre  frontend.Variable `gnark:",public"`
//     StateRootPost frontend.Variable `gnark:",public"`
// }
//
// func (c *MatchCircuit) Define(api frontend.API) error {
//     // 1. 验证 taker 订单签名
//     // 2. 对于每个撮合对，验证：
//     //    a. maker price <= taker price (buy) 或 maker price >= taker price (sell)
//     //    b. match amount = min(taker.amount_left, maker.amount_left)
//     //    c. 撮合后状态正确
//     // 3. 验证状态根迁移正确
//     // 4. 验证 Nullifier 未被消费
//     return nil
// }
