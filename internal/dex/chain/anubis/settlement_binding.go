package anubis

// settlement_binding.go: Anubis Settlement v3 的轻量 Go 绑定（嵌入 ABI，无 abigen 依赖）。
// 来源：contracts/anubis/contracts/Settlement.sol（v3，金库 + 真实 ERC-20 划转）。
// 后续如引入 abigen 可废弃此文件，交由 abigen 重新生成。

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// settlementV3ABIJSON 是 Settlement v3 的关键方法 + 事件（从编译 artifacts 抽取）。
//   submitBatch(MatchResult[] results, bytes zkProof, bytes32 newRoot)
//   vaultBalance(address,address) / deposit / withdraw / batchSeq / currentStateRoot
//   events: BatchSettled / TradeSettled / Unshielded
var settlementV3ABIJSON = `[{"anonymous":false,"inputs":[{"indexed":true,"internalType":"uint64","name":"batchId","type":"uint64"},{"indexed":false,"internalType":"bytes32","name":"stateRoot","type":"bytes32"},{"indexed":false,"internalType":"uint256","name":"totalVolume","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"feeCollected","type":"uint256"}],"name":"BatchSettled","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"user","type":"address"},{"indexed":true,"internalType":"address","name":"token","type":"address"},{"indexed":false,"internalType":"uint256","name":"amount","type":"uint256"}],"name":"Deposited","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"bytes32","name":"orderId","type":"bytes32"},{"indexed":true,"internalType":"address","name":"user","type":"address"},{"indexed":false,"internalType":"address","name":"payToken","type":"address"},{"indexed":false,"internalType":"uint256","name":"payAmount","type":"uint256"},{"indexed":false,"internalType":"address","name":"receiveToken","type":"address"},{"indexed":false,"internalType":"uint256","name":"receiveAmount","type":"uint256"}],"name":"TradeSettled","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"user","type":"address"},{"indexed":true,"internalType":"address","name":"token","type":"address"},{"indexed":false,"internalType":"uint256","name":"amount","type":"uint256"},{"indexed":false,"internalType":"bytes32","name":"settlementId","type":"bytes32"}],"name":"Unshielded","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"user","type":"address"},{"indexed":true,"internalType":"address","name":"token","type":"address"},{"indexed":false,"internalType":"uint256","name":"amount","type":"uint256"}],"name":"Withdrawn","type":"event"},{"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"authorizedEngines","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"batchSeq","outputs":[{"internalType":"uint64","name":"","type":"uint64"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"currentStateRoot","outputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"token","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"deposit","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"requireProof","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[{"components":[{"internalType":"uint64","name":"id","type":"uint64"},{"internalType":"bytes32","name":"orderId","type":"bytes32"},{"internalType":"address","name":"user","type":"address"},{"internalType":"address","name":"baseToken","type":"address"},{"internalType":"address","name":"quoteToken","type":"address"},{"internalType":"uint256","name":"price","type":"uint256"},{"internalType":"uint256","name":"amount","type":"uint256"},{"internalType":"uint256","name":"cost","type":"uint256"},{"internalType":"uint8","name":"side","type":"uint8"},{"internalType":"bytes32","name":"nullifier","type":"bytes32"},{"internalType":"string","name":"role","type":"string"}],"internalType":"struct Settlement.MatchResult[]","name":"results","type":"tuple[]"},{"internalType":"bytes","name":"zkProof","type":"bytes"},{"internalType":"bytes32","name":"newRoot","type":"bytes32"}],"name":"submitBatch","outputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"","type":"address"},{"internalType":"address","name":"","type":"address"}],"name":"vaultBalance","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"token","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"withdraw","outputs":[],"stateMutability":"nonpayable","type":"function"}]`

var settleContractAB abi.ABI

func init() {
	parsed, err := abi.JSON(strings.NewReader(settlementV3ABIJSON))
	if err != nil {
		panic(fmt.Sprintf("parse settlement v3 ABI: %v", err))
	}
	settleContractAB = parsed
}

// SettlementMatchV3 mirrors contracts/anubis/contracts/Settlement.sol MatchResult（v3）。
// abi tag 必需：go-ethereum 以 ToCamelCase(abiName) 匹配字段名，
// "id" → "Id" 与 Go 惯例的 "ID" 不符，故显式声明。
type SettlementMatchV3 struct {
	ID         uint64         `abi:"id"`
	OrderId    [32]byte       `abi:"orderId"`
	User       common.Address `abi:"user"`
	BaseToken  common.Address `abi:"baseToken"`
	QuoteToken common.Address `abi:"quoteToken"`
	Price      *big.Int       `abi:"price"`
	Amount     *big.Int       `abi:"amount"`
	Cost       *big.Int       `abi:"cost"`
	Side       uint8          `abi:"side"` // 0=buy（收 base 付 quote），1=sell
	Nullifier  [32]byte       `abi:"nullifier"`
	Role       string         `abi:"role"`
}

// SettlementBatchSettled 解析 BatchSettled 事件
type SettlementBatchSettled struct {
	BatchID      uint64
	StateRoot    [32]byte
	TotalVolume  *big.Int
	FeeCollected *big.Int
}

// encodeSubmitBatch ABI-pack submitBatch(results, zkProof, newRoot)。
// calldata = selector(4B) + abi.encode(results, zkProof, newRoot)
func encodeSubmitBatch(results []SettlementMatchV3, zkProof []byte, newRoot [32]byte) ([]byte, error) {
	method, ok := settleContractAB.Methods["submitBatch"]
	if !ok {
		return nil, fmt.Errorf("submitBatch not in settlement v3 ABI")
	}
	packed, err := method.Inputs.Pack(results, zkProof, newRoot)
	if err != nil {
		return nil, fmt.Errorf("abi pack submitBatch: %w", err)
	}
	return append(method.ID, packed...), nil
}

// encodeVaultBalance 生成 vaultBalance(token, user) 的 eth_call data（引擎预检余额）。
func encodeVaultBalance(token, user common.Address) ([]byte, error) {
	method, ok := settleContractAB.Methods["vaultBalance"]
	if !ok {
		return nil, fmt.Errorf("vaultBalance not in settlement v3 ABI")
	}
	packed, err := method.Inputs.Pack(token, user)
	if err != nil {
		return nil, fmt.Errorf("abi pack vaultBalance: %w", err)
	}
	return append(method.ID, packed...), nil
}

// parseBatchSettled 从 receipt 的 BatchSettled 日志解码。
func parseBatchSettled(receipt *types.Receipt) *SettlementBatchSettled {
	ev, ok := settleContractAB.Events["BatchSettled"]
	if !ok {
		return nil
	}
	for _, lg := range receipt.Logs {
		if len(lg.Topics) < 2 || lg.Topics[0] != ev.ID {
			continue
		}
		r := SettlementBatchSettled{
			TotalVolume:  new(big.Int),
			FeeCollected: new(big.Int),
		}
		r.BatchID = new(big.Int).SetBytes(lg.Topics[1].Bytes()).Uint64()
		if len(lg.Data) >= 96 {
			r.StateRoot = [32]byte(lg.Data[0:32])
			r.TotalVolume = new(big.Int).SetBytes(lg.Data[32:64])
			r.FeeCollected = new(big.Int).SetBytes(lg.Data[64:96])
		}
		return &r
	}
	return nil
}
