package anubis

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/dex/privacy"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	jsoniter "github.com/json-iterator/go"
)

// Subscriber 链上事件订阅器
// 订阅 Anubis Chain OrderBookRegistry SC 的 OrderSubmitted 事件并解密订单
type Subscriber struct {
	symbol       string                       // 当前订阅的交易对，如 "BTC/USDT"
	rpcEndpoint  string                       // Anubis Chain HTTP RPC 节点地址（用于 eth_getLogs 轮询）
	contractAddr string                       // OrderBookRegistry 智能合约地址
	pollInterval time.Duration                // 轮询间隔（MVP 阶段），后续替换为 WebSocket 推送
	httpClient   *http.Client                 // JSON-RPC HTTP 客户端
	orderTopic   string                       // OrderSubmitted 事件签名 topic（从合约 bytecode 提取，待与 ABI 核对）
	ctx          context.Context              // 控制 subscriber 生命周期的 context
	cancel       context.CancelFunc           // 取消 context，触发所有 goroutine 退出
	mu           sync.Mutex                   // 保护 subs map 的并发安全
	subs         map[string]chan *match.Order // 交易对 -> 订单 channel 的映射，每个交易对独立推送
	viewKey      *privacy.ViewKey             // 用于解密链上加密的订单 Note
}

// NewSubscriber 创建链上事件订阅器
func NewSubscriber(rpcEndpoint string) *Subscriber {
	ctx, cancel := context.WithCancel(context.Background())
	// OrderSubmitted 事件签名 topic：从 OrderBookRegistry 合约 bytecode 提取（PUSH32），
	// 待与合约 ABI 核对；可用 chain.order-submitted-topic 配置覆盖
	orderTopic := config.GetString("chain.anubis.order-submitted-topic",
		"0x942d46dddbdc36d1ed575e5093656f2952053568a7867ea0aaf449ace306f03c")
	pollInterval := config.GetDuration("chain.anubis.poll-interval-ms", 200) * time.Millisecond
	common.Info("chain subscriber: initialized http=%s poll-interval=%s topic=%s",
		rpcEndpoint, pollInterval, orderTopic)
	return &Subscriber{
		rpcEndpoint:  rpcEndpoint,
		pollInterval: pollInterval,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		orderTopic:   orderTopic,
		ctx:          ctx,
		cancel:       cancel,
		subs:         make(map[string]chan *match.Order),
	}
}

// SetViewKey 设置用于解密链上订单的 View Key
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

// fetchOrders 从链上拉取 OrderSubmitted 事件并解密为 match.Order
// 用 HTTP json-rpc eth_getLogs 轮询（MVP），生产阶段可替换为 WebSocket 订阅
func (s *Subscriber) fetchOrders(symbol string, fromBlock uint64) ([]*match.Order, uint64, error) {
	if s.contractAddr == "" {
		return nil, fromBlock, fmt.Errorf("registry contract address not configured")
	}

	// 当前最新区块作为 toBlock
	latest, err := s.getBlockNumber()
	if err != nil {
		return nil, fromBlock, fmt.Errorf("get block number: %w", err)
	}
	if latest <= fromBlock {
		return nil, fromBlock, nil
	}

	// eth_getLogs: (fromBlock+1, latest], address=registry, topics=[orderSubmittedTopic]
	logs, err := s.getLogs(fromBlock+1, latest)
	if err != nil {
		return nil, fromBlock, fmt.Errorf("get logs: %w", err)
	}
	if len(logs) == 0 {
		return nil, latest, nil
	}

	orders := make([]*match.Order, 0, len(logs))
	processed, skipped := 0, 0
	mode := "plain"
	for _, lg := range logs {
		var order *match.Order
		if s.viewKey == nil {
			// 明文模式：未配置 ViewKey，链上为明文订单，直接从日志解析（无需解密）
			order = s.decodePlainOrder(lg)
		} else {
			// 隐私模式：解密 EncryptedOrder
			mode = "encrypted"
			eo := s.decodeEncryptedOrder(lg)
			if eo == nil {
				skipped++
				continue
			}
			if privacy.IsNullifierSpent(eo.Nullifier) {
				skipped++
				continue
			}
			var err error
			order, err = privacy.DecryptOrderFromNote(eo, s.viewKey)
			if err != nil {
				skipped++
				continue
			}
		}
		if order == nil {
			skipped++
			continue
		}
		// nullifier 去重（明文/加密通用）
		if len(order.Nullifier) > 0 && privacy.IsNullifierSpent(order.Nullifier) {
			skipped++
			continue
		}
		orders = append(orders, order)
		processed++
	}

	if processed > 0 || skipped > 0 {
		common.Info(fmt.Sprintf("chain subscriber: fetched %d logs, processed %d, skipped %d, mode=%s, symbol=%s block=[%d,%d]",
			len(logs), processed, skipped, mode, symbol, fromBlock+1, latest))
	}
	return orders, latest, nil
}

// decodePlainOrder 明文模式下从链上日志解析订单（未配置 ViewKey，订单未加密）
// OrderSubmitted 事件 ABI（已核对 contracts/abi/OrderBookRegistry.json）：
//
//	topics[1]=noteCommitment(bytes32 indexed)
//	topics[2]=viewTag(bytes4 indexed)
//	topics[3]=submitter(address indexed)
//	data=nullifier(bytes32, [0:32]) + deadline(uint64, 右对齐 [56:64])
//
// ⚠️ 事件不含订单明文字段（price/amount/orderId/buyOrSell/type/circuitRate/createAt/stp），
//
//	这些字段未上链。明文模式仅恢复 noteCommitment/nullifier/deadline，price 等留空。
func (s *Subscriber) decodePlainOrder(lg rpcLog) *match.Order {
	if len(lg.Topics) < 4 {
		return nil
	}
	noteCommitment, _ := hex.DecodeString(strings.TrimPrefix(lg.Topics[1], "0x"))
	order := &match.Order{State: match.Submitted}
	if len(noteCommitment) == 32 {
		order.NoteCommitment = noteCommitment
	}
	data, err := hex.DecodeString(strings.TrimPrefix(lg.Data, "0x"))
	if err != nil {
		return order
	}
	if len(data) >= 32 {
		order.Nullifier = data[0:32]
	}
	if len(data) >= 64 {
		order.Deadline = int64(parseHexUint64Bytes(data[56:64]))
	}
	return order
}

// SetContractAddr 设置合约地址
func (s *Subscriber) SetContractAddr(addr string) {
	s.contractAddr = addr
	common.Info("chain subscriber: registry contract =", addr)
}

// ─── HTTP JSON-RPC 辅助 ────────────────────────────────

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcLog struct {
	BlockNumber string   `json:"blockNumber"`
	LogIndex    string   `json:"logIndex"`
	Topics      []string `json:"topics"`
	Data        string   `json:"data"`
}

// rpcCall 通用 JSON-RPC POST 调用，结果反序列化到 result
func (s *Subscriber) rpcCall(method string, params interface{}, result interface{}) error {
	reqBody := struct {
		Jsonrpc string      `json:"jsonrpc"`
		Method  string      `json:"method"`
		Params  interface{} `json:"params"`
		Id      int         `json:"id"`
	}{Jsonrpc: "2.0", Method: method, Params: params, Id: 1}
	body, err := jsoniter.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(s.ctx, "POST", s.rpcEndpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	var envelope struct {
		Result jsoniter.RawMessage `json:"result"`
		Error  *rpcError           `json:"error"`
	}
	if err := jsoniter.Unmarshal(raw, &envelope); err != nil {
		tail := string(raw)
		if len(tail) > 300 {
			tail = tail[:300] + "..."
		}
		return fmt.Errorf("parse response: %w (body: %s)", err, tail)
	}
	if envelope.Error != nil {
		return fmt.Errorf("rpc error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if err := jsoniter.Unmarshal(envelope.Result, result); err != nil {
		return fmt.Errorf("parse result: %w", err)
	}
	return nil
}

// getBlockNumber 查询当前最新区块号
func (s *Subscriber) getBlockNumber() (uint64, error) {
	var hexBlock string
	if err := s.rpcCall("eth_blockNumber", []interface{}{}, &hexBlock); err != nil {
		return 0, err
	}
	return parseHexUint64(hexBlock)
}

// getLogs 查询 OrderSubmitted 事件日志
func (s *Subscriber) getLogs(fromBlock, toBlock uint64) ([]rpcLog, error) {
	filter := map[string]interface{}{
		"fromBlock": toHexUint64(fromBlock),
		"toBlock":   toHexUint64(toBlock),
		"address":   s.contractAddr,
	}
	if s.orderTopic != "" {
		filter["topics"] = []string{s.orderTopic}
	}
	var logs []rpcLog
	if err := s.rpcCall("eth_getLogs", []interface{}{filter}, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

// decodeEncryptedOrder 从链上日志解码为 EncryptedOrder
// OrderSubmitted 事件 ABI（已核对 contracts/abi/OrderBookRegistry.json）：
//
//	topics[1]=noteCommitment(bytes32 indexed)
//	topics[2]=viewTag(bytes4 indexed, 右对齐 topic[28:32])
//	topics[3]=submitter(address indexed, 右对齐 topic[12:32])
//	data=nullifier(bytes32, [0:32]) + deadline(uint64, 右对齐 [56:64])
//
// 注意：EncryptedData/EphemeralPK/IV 不在事件中，需从其他渠道获取才能解密
func (s *Subscriber) decodeEncryptedOrder(lg rpcLog) *privacy.EncryptedOrder {
	if len(lg.Topics) < 4 {
		return nil
	}
	noteCommitment, err := hex.DecodeString(strings.TrimPrefix(lg.Topics[1], "0x"))
	if err != nil || len(noteCommitment) != 32 {
		return nil
	}
	viewTagTopic, err := hex.DecodeString(strings.TrimPrefix(lg.Topics[2], "0x"))
	if err != nil || len(viewTagTopic) != 32 {
		return nil
	}
	data, err := hex.DecodeString(strings.TrimPrefix(lg.Data, "0x"))
	if err != nil || len(data) < 64 {
		return nil
	}
	return &privacy.EncryptedOrder{
		NoteCommitment: noteCommitment,
		ViewTag:        viewTagTopic[28:32],
		Nullifier:      data[0:32],
		Deadline:       int64(parseHexUint64Bytes(data[56:64])),
	}
}

// ─── hex 工具 ──────────────────────────────────────────

func toHexUint64(v uint64) string {
	return "0x" + strconv.FormatUint(v, 16)
}

func parseHexUint64(s string) (uint64, error) {
	return strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
}

func parseHexUint64Bytes(b []byte) uint64 {
	var v uint64
	for _, byt := range b {
		v = v<<8 | uint64(byt)
	}
	return v
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
