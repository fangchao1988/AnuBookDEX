package aleo

import (
	"context"
	"fmt"
	"sync"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/infra/common"
)

// Subscriber Aleo 链下订单订阅器（Phase 2b）：
// 从 OrderPool 消费链下提交的订单（明文撮合）。record 模型下订单在私有 record 中
// （链上仅密文），无法像 Phase 1 那样轮询公开 mapping，故完全由链下通道驱动。
type Subscriber struct {
	pool *OrderPool

	mu       sync.Mutex
	subs     map[string]chan *match.Order
	firstSym string

	ctx      context.Context
	cancel   context.CancelFunc
	loopOnce sync.Once
}

// NewSubscriber 创建订单订阅器（依赖链下订单池）。
func NewSubscriber(pool *OrderPool) *Subscriber {
	ctx, cancel := context.WithCancel(context.Background())
	return &Subscriber{
		pool:   pool,
		subs:   make(map[string]chan *match.Order),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Subscribe 订阅指定交易对。首个 symbol 触发订单消费循环。
func (s *Subscriber) Subscribe(symbol string) (<-chan *match.Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.subs[symbol]; exists {
		return nil, fmt.Errorf("symbol %s already subscribed", symbol)
	}
	ch := make(chan *match.Order, 5000)
	s.subs[symbol] = ch
	if s.firstSym == "" {
		s.firstSym = symbol
	}
	s.loopOnce.Do(func() { go s.eventLoop() })
	return ch, nil
}

// Unsubscribe 取消订阅。
func (s *Subscriber) Unsubscribe(symbol string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.subs[symbol]; ok {
		close(ch)
		delete(s.subs, symbol)
	}
	return nil
}

// Shutdown 关闭订阅器。
func (s *Subscriber) Shutdown() {
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	for sym, ch := range s.subs {
		close(ch)
		delete(s.subs, sym)
	}
}

// eventLoop 从订单池消费链下订单，按订单 Symbol 路由到对应交易对的撮合通道。
func (s *Subscriber) eventLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case po := <-s.pool.Orders():
			sym := po.Order.Symbol
			if sym == "" {
				// 兼容旧客户端（未带 symbol）：回退首个订阅的交易对
				sym = s.firstSym
			}
			s.mu.Lock()
			ch, ok := s.subs[sym]
			s.mu.Unlock()
			if !ok {
				common.Warn("aleo subscriber: no channel for symbol", sym, "order", po.Order.OrderId)
				s.pool.Complete(uint64(po.Order.OrderId))
				continue
			}
			select {
			case ch <- po.Order:
			case <-s.ctx.Done():
				return
			}
			s.pool.Complete(uint64(po.Order.OrderId))
			common.Debug("aleo subscriber: pooled order -> matcher",
				po.Order.OrderId, "|symbol", sym, "side", po.Order.BuyOrSell, "price", po.Order.Price.String())
		}
	}
}
