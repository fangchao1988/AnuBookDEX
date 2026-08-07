// Package chain 定义链后端的链无关抽象接口。
//
// 具体链后端实现位于子包 anubis/ 与 aleo/，各自实现 OrderSource / SettlementSink，
// 由 cmd/engine/{anubis,aleo}/main.go 注入到 runner.StartEngine，使撮合核心可复用。
package chain

import "github.com/AnuBookDEX/engine/internal/core/match"

// OrderSource 订单输入源：订阅链上订单事件并产出 match.Order（替代集中式 puller）。
type OrderSource interface {
	Subscribe(symbol string) (<-chan *match.Order, error)
	Unsubscribe(symbol string) error
	Shutdown()
}

// SettlementSink 链上结算目标：批量提交撮合结果（替代集中式 persistence）。
type SettlementSink interface {
	SubmitBatch(symbol string, mrs []*match.MatchResult) (string, error)
	Shutdown()
}

// ChainAdapter 聚合一个链后端，供引擎入口注入。
type ChainAdapter interface {
	Name() string
	Orders() OrderSource
	Settlement() SettlementSink
}
