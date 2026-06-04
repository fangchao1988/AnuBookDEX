// Package bindings 包含合约事件结构体与 ABI 元数据
// 与 Anubis Chain 上 Solidity 合约一一对应
//
// 当 go-ethereum 可用时，运行 abigen 重新生成此文件即可替换
package bindings

import "math/big"

// ─── Anubis 地址与哈希原始类型 (go-ethereum-free) ───
// 生产阶段引入 go-ethereum 后，替换为 common.Address / common.Hash

// Address 20 字节 EVM 地址
type Address [20]byte

// Hash 32 字节 Keccak256 哈希
type Hash [32]byte

// ─── OrderBookRegistry ─────────────────────────────────────

// RegistryOrderSubmitted 订单提交事件（链下引擎监听）
type RegistryOrderSubmitted struct {
	NoteCommitment Hash
	ViewTag        [4]byte
	Nullifier      Hash
	Deadline       uint64
	Submitter      Address
}

// RegistryPairConfig 交易对参数
type RegistryPairConfig struct {
	BaseAsset    string
	QuoteAsset   string
	PriceScale   uint8
	AmountScale  uint8
	MinOrderSize *big.Int
	MaxOrderSize *big.Int
	Active       bool
}
