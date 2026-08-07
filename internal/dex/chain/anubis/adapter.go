package anubis

import (
	"math/big"

	"github.com/AnuBookDEX/engine/internal/dex/chain"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
)

// adapter 聚合 Anubis 链后端，实现 chain.ChainAdapter。
type adapter struct {
	sub   *Subscriber
	settl *Settlement
}

// NewAdapter 从 chain.anubis.* 配置构造 Anubis 链后端。
func NewAdapter() chain.ChainAdapter {
	rpc := config.GetString("chain.anubis.rpc-endpoint", "http://localhost:8545")

	sub := NewSubscriber(rpc)
	sub.SetContractAddr(config.GetString("chain.anubis.registry-contract", ""))

	priv := config.GetString("chain.anubis.private-key", "")
	if priv == "" {
		common.Warn("chain.anubis.private-key is empty - export ANUBIS_PRIVATE_KEY in your shell")
	}
	chainID := big.NewInt(int64(config.GetInt("chain.anubis.chain-id", 0)))
	settl := NewSettlement(
		rpc,
		config.GetString("chain.anubis.settlement-contract", ""),
		priv,
		chainID,
	)

	return &adapter{sub: sub, settl: settl}
}

func (a *adapter) Name() string                    { return "anubis" }
func (a *adapter) Orders() chain.OrderSource       { return a.sub }
func (a *adapter) Settlement() chain.SettlementSink { return a.settl }

// 编译期接口断言：确保 Anubis 后端满足链无关接口契约。
var (
	_ chain.ChainAdapter   = (*adapter)(nil)
	_ chain.OrderSource    = (*Subscriber)(nil)
	_ chain.SettlementSink = (*Settlement)(nil)
)
