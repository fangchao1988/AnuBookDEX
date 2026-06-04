package chain

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/dex/privacy"
)

// Subscriber 链上事件订阅器
// 订阅 Anubis Chain OrderBookRegistry SC 的 OrderSubmitted 事件并解密订单
type Subscriber struct {
	symbol       string
	rpcEndpoint  string
	contractAddr string
	pollInterval time.Duration
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	subs         map[string]chan *match.Order
	viewKey      *privacy.ViewKey // Phase 2: 用于解密链上 Note
}

// NewSubscriber 创建链上事件订阅器
func NewSubscriber(rpcEndpoint string) *Subscriber {
	ctx, cancel := context.WithCancel(context.Background())
	return &Subscriber{
		rpcEndpoint:  rpcEndpoint,
		pollInterval: config.GetDuration("chain.poll-interval-ms", 200) * time.Millisecond,
		ctx:          ctx,
		cancel:       cancel,
		subs:         make(map[string]chan *match.Order),
	}
}

// SetViewKey 设置用于解密链上订单的 View Key（Phase 2）
func (s *Subscriber) SetViewKey(vk *privacy.ViewKey) {
	s.viewKey = vk
}

// Subscribe 订阅指定交易对的链上订单事件
func (s *Subscriber) Subscribe(symbol string) (<-chan *match.Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.subs[symbol]; exists {
		return nil, fmt.Errorf("symbol %s already subscribed", symbol)
	}

	ch := make(chan *match.Order, 5000)
	s.subs[symbol] = ch

	go s.eventLoop(symbol, ch)
	common.Info(fmt.Sprintf("chain subscriber: subscribed to %s events", symbol))
	return ch, nil
}

// Unsubscribe 取消订阅
func (s *Subscriber) Unsubscribe(symbol string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch, exists := s.subs[symbol]
	if !exists {
		return fmt.Errorf("symbol %s not subscribed", symbol)
	}
	close(ch)
	delete(s.subs, symbol)

	common.Info(fmt.Sprintf("chain subscriber: unsubscribed from %s events", symbol))
	return nil
}

// Shutdown 关闭订阅器
func (s *Subscriber) Shutdown() {
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	for symbol, ch := range s.subs {
		close(ch)
		delete(s.subs, symbol)
	}
}

// eventLoop 事件轮询循环
// 当前实现为定时轮询（MVP 阶段），后续替换为 WebSocket 实时订阅
func (s *Subscriber) eventLoop(symbol string, ch chan *match.Order) {
	defer func() {
		if e := recover(); e != nil {
			common.Error("chain subscriber event loop panic:", e, "symbol:", symbol)
		}
	}()

	// fromId 从快照恢复后的序列号开始
	// MVP 阶段使用轮询方式，后续替换为 Anubis WebSocket 事件订阅
	var lastBlock uint64

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			orders, latestBlock, err := s.fetchOrders(symbol, lastBlock)
			if err != nil {
				common.Warn("chain subscriber: fetch orders error:", err, "symbol:", symbol)
				continue
			}
			for _, order := range orders {
				select {
				case ch <- order:
				case <-s.ctx.Done():
					return
				}
			}
			if latestBlock > lastBlock {
				lastBlock = latestBlock
			}
		}
	}
}

// fetchOrders 从链上拉取订单事件
// MVP 阶段使用 RPC 查询日志，生产阶段替换为 WebSocket 订阅 + Note 解密
func (s *Subscriber) fetchOrders(symbol string, fromBlock uint64) ([]*match.Order, uint64, error) {
	// TODO: 接入 Anubis Chain RPC 后实现
	// 1. 调用 eth_getLogs 查询 OrderBookRegistry.OrderSubmitted 事件
	// 2. 对每个事件，使用 View Key 解密 Note 提取订单数据
	// 3. 将链上订单映射为 match.Order 结构体
	//
	// 示例伪代码：
	// query := ethereum.FilterQuery{
	//     FromBlock: new(big.Int).SetUint64(fromBlock + 1),
	//     Addresses: []common.Address{common.HexToAddress(s.contractAddr)},
	//     Topics:    [][]common.Hash{{orderSubmittedTopic}},
	// }
	// logs, err := s.client.FilterLogs(s.ctx, query)
	// ...

	// STUB: 链上事件订阅尚未接入 Anubis Chain RPC，当前为 stub 实现
	// 引擎不会收到任何订单，启动时会输出此警告
	common.Warn("chain subscriber: fetchOrders is STUB — no orders will be received. symbol:", symbol, "fromBlock:", fromBlock)
	return nil, fromBlock, nil
}

// SetContractAddr 设置合约地址
func (s *Subscriber) SetContractAddr(addr string) {
	s.contractAddr = addr
}

// 以下为 Anubis 集成预留的类型和工具函数

// DecryptOrder 使用 View Key 解密 Anubis Note 中的订单数据
// 生产阶段实现
func DecryptOrder(noteCommitment []byte, viewKey []byte) (*match.Order, error) {
	// TODO: 使用 Anubis View Key SDK 解密 Note
	// 1. 根据 ViewTag 快速过滤
	// 2. 使用 View Key 尝试解密 Note 承诺
	// 3. 提取订单字段 (price, amount, orderType, etc.)
	// 4. 验证 Nullifier 唯一性
	return nil, fmt.Errorf("not implemented: Anubis View Key SDK not yet integrated")
}

// encodeOrderToABI 将 match.Order 编码为 ABI 格式提交到链上
func encodeOrderToABI(order *match.Order) ([]byte, error) {
	// TODO: 使用 abi.Arguments.Pack() 编码
	return nil, fmt.Errorf("not implemented")
}

// bigIntToDecimal 将链上 big.Int (以最小单位表示) 转换为 decimal
func bigIntToDecimal(v *big.Int, decimals int32) string {
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	intPart := new(big.Int).Div(v, divisor)
	fracPart := new(big.Int).Mod(v, divisor)
	return fmt.Sprintf("%s.%0*s", intPart.String(), decimals, fracPart.String())
}
