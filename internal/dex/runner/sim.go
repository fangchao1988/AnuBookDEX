package runner

import (
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/infra/config"

	"github.com/shopspring/decimal"
)

var errSimSymbolNotFound = errors.New("sim: symbol not found")

// SimOrderSource 本地联调用模拟订单源（无链环境下驱动 撮合 -> l2quote -> WS 全链路）。
// 注入方式：chain.<chain>.simulate-orders=true 时替代真实链后端。
type SimOrderSource struct {
	mu       sync.Mutex
	chans    map[string]chan *match.Order // symbol -> 订单通道
	seqId    int64
	orderId  int64
	stop     chan struct{}
	interval time.Duration
}

// NewSimOrderSource 创建模拟订单源。interval 为每笔订单的生成间隔。
func NewSimOrderSource(interval time.Duration) *SimOrderSource {
	s := &SimOrderSource{
		chans:    make(map[string]chan *match.Order),
		stop:     make(chan struct{}),
		interval: interval,
	}
	// 每个交易对独立 goroutine 持续产生随机限价单（买卖交替，价格围绕基准游走）
	for _, symbol := range config.GetStringSlice("symbols", []string{}) {
		ch := make(chan *match.Order, 1024)
		s.chans[symbol] = ch
		go s.genOrders(symbol, ch)
	}
	return s
}

func (s *SimOrderSource) genOrders(symbol string, ch chan *match.Order) {
	base := 68000.0
	if symbol == "ETH_USDT" {
		base = 3500.0
	}
	buy := true
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
		}
		// 随机游走 + 买卖价差，形成可撮合的双向订单流
		base += (rand.Float64() - 0.5) * 40
		price := base + (rand.Float64()-0.5)*10
		if buy {
			price -= 2 + rand.Float64()*8 // 买价略低，留出撮合空间
		} else {
			price += 2 + rand.Float64()*8
		}
		amt := decimal.NewFromFloat(0.05 + rand.Float64()*0.5)

		s.mu.Lock()
		s.seqId++
		s.orderId++
		seq, oid := s.seqId, s.orderId
		s.mu.Unlock()

		order := &match.Order{
			SeqId:          seq,
			OrderId:        oid,
			Symbol:         symbol,
			UserId:         int64(rand.Intn(1000) + 1),
			UserAddress:    "0xsim",
			BuyOrSell:      buyOrSell(buy),
			Type:           match.Limit,
			State:          match.Submitted,
			Price:          decimal.NewFromFloat(price),
			UnfilledAmount: amt,
			CreateAt:       time.Now().UnixMilli(),
		}
		select {
		case ch <- order:
		case <-s.stop:
			return
		}
		buy = !buy
	}
}

// buyOrSell 将 bool 转为买卖方向
func buyOrSell(isBuy bool) match.OrderBuyOrSell {
	if isBuy {
		return match.Buy
	}
	return match.Sell
}

func (s *SimOrderSource) Subscribe(symbol string) (<-chan *match.Order, error) {
	ch, ok := s.chans[symbol]
	if !ok {
		return nil, errSimSymbolNotFound
	}
	return ch, nil
}

func (s *SimOrderSource) Unsubscribe(symbol string) error { return nil }

func (s *SimOrderSource) Shutdown() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
}

// NoopSettlementSink 联调模式下的空结算目标（模拟模式下无需上链）
type NoopSettlementSink struct{}

func (NoopSettlementSink) SubmitBatch(symbol string, mrs []*match.MatchResult) (string, error) {
	return "", nil
}

func (NoopSettlementSink) Shutdown() {}
