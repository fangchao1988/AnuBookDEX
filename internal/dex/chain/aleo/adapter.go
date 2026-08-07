package aleo

import (
	"github.com/AnuBookDEX/engine/internal/dex/chain"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
)

// adapter 聚合 Aleo 链后端，实现 chain.ChainAdapter。
type adapter struct {
	sub   *Subscriber
	settl *Settlement
	pool  *OrderPool
}

// NewAdapter 从 chain.aleo.* 配置构造 Aleo 链后端（Phase 2b：链下订单池 + 密文结算）。
func NewAdapter() chain.ChainAdapter {
	rpc := config.GetString("chain.aleo.rpc-endpoint", "https://api.explorer.provable.com/v1")
	programID := config.GetString("chain.aleo.program-id", "anubook_dex_p2.aleo")
	priv := config.GetString("chain.aleo.private-key", "")
	if priv == "" {
		common.Warn("chain.aleo.private-key is empty - export ALEO_PRIVATE_KEY in your shell")
	}
	pool := NewOrderPool()
	return &adapter{
		sub:   NewSubscriber(pool),
		settl: NewSettlement(rpc, programID, priv, pool),
		pool:  pool,
	}
}

func (a *adapter) Name() string                     { return "aleo" }
func (a *adapter) Orders() chain.OrderSource        { return a.sub }
func (a *adapter) Settlement() chain.SettlementSink { return a.settl }

// Pool 返回链下订单池（供入口注册 POST /order API）。
func (a *adapter) Pool() *OrderPool { return a.pool }

// 编译期接口断言：确保 Aleo 后端满足链无关接口契约。
var (
	_ chain.ChainAdapter   = (*adapter)(nil)
	_ chain.OrderSource    = (*Subscriber)(nil)
	_ chain.SettlementSink = (*Settlement)(nil)
)
