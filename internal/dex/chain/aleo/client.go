package aleo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/shopspring/decimal"
)

// RESTClient snarkOS REST 客户端：读 mapping / 查交易 / 广播。
//
// 目标可为本地 leo devnode（http://localhost:3030）或 testnet（QuickNode /v2 端点）。
// 端点路径以 /testnet/ 为基准（devnode 与 testnet3 同构）；testnet 需在 base 末尾带 /v2。
//
// 注：响应格式为 Phase 1 假设，需用 leo devnode 实测后微调。
type RESTClient struct {
	base       string
	httpClient *http.Client
}

// NewRESTClient 创建 snarkOS REST 客户端。
func NewRESTClient(base string) *RESTClient {
	return &RESTClient{
		base:       strings.TrimRight(base, "/"),
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// GetLatestHeight 返回最新区块高度。snarkOS: GET /testnet/latest/height -> "12345"
func (c *RESTClient) GetLatestHeight() (uint64, error) {
	body, err := c.get("/testnet/latest/height")
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.Trim(strings.TrimSpace(body), `"`), 10, 64)
}

// GetProgramMapping 读取 program 的 mapping[key]，返回原始 plaintext（Leo 字面量）。
// snarkOS: GET /testnet/program/{program}/mapping/{mapping}/{key}
func (c *RESTClient) GetProgramMapping(program, mappingName, key string) (string, error) {
	path := fmt.Sprintf("/testnet/program/%s/mapping/%s/%s", program, mappingName, key)
	return c.get(path)
}

// GetTransaction 查询交易回执。snarkOS: GET /testnet/transaction/{id}
func (c *RESTClient) GetTransaction(txID string) (map[string]interface{}, error) {
	path := fmt.Sprintf("/testnet/transaction/%s", txID)
	body, err := c.get(path)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return nil, fmt.Errorf("parse transaction json: %w", err)
	}
	return m, nil
}

// BroadcastTransaction 广播交易，返回 txID。
// snarkOS: POST /testnet/transaction/broadcast（body = 交易字符串）
func (c *RESTClient) BroadcastTransaction(tx string) (string, error) {
	resp, err := c.httpClient.Post(c.base+"/testnet/transaction/broadcast", "application/json", strings.NewReader(tx))
	if err != nil {
		return "", fmt.Errorf("broadcast: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("broadcast http %d: %s", resp.StatusCode, string(body))
	}
	return strings.Trim(strings.TrimSpace(string(body)), `"`), nil
}

func (c *RESTClient) get(path string) (string, error) {
	resp, err := c.httpClient.Get(c.base + path)
	if err != nil {
		return "", fmt.Errorf("get %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d on %s: %s", resp.StatusCode, path, string(body))
	}
	return string(body), nil
}

// ── Order plaintext 解析 ──────────────────────────────────

// leoFieldRe 匹配 Leo struct 字面量中的 `field: value`。
// 注意排除反斜杠：snarkOS REST 返回的是 JSON 字符串，字段间换行为字面 `\n`（反斜杠+n 两个字符），
// 若不排除反斜杠，value 会被吞成带 `\n` 尾巴（如 active: false 变成 "false\n"），导致解析错误。
var leoFieldRe = regexp.MustCompile(`(\w+)\s*:\s*([^,}\s\\]+)`)

// leoUintRe 从 "100u64" / "0u8" 等中提取前导数字。
var leoUintRe = regexp.MustCompile(`^\d+`)

// ParseOrder 将 snarkOS mapping 返回的 Order plaintext 解析为 match.Order。
//
// 示例 plaintext（dex.aleo struct Order）：
//
//	"Order { owner: aleo1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq3ljyzc, side: 0u8, price: 100u64, amount: 5u64, base_token: 1u32, quote_token: 2u32, deadline: 1000u32, active: true }"
//
// orderID 为 mapping 的 key（链下引擎分配），用于 OrderId/SeqId（价格-时间优先序）。
func ParseOrder(orderID uint64, plaintext string) (*match.Order, error) {
	fields := parseLeoStruct(plaintext)
	if fields == nil {
		return nil, fmt.Errorf("parse order plaintext: %q", plaintext)
	}
	owner := fields["trader"] // Leo 字段名 trader（owner 是保留字）
	side, _ := parseLeoUint(fields["side"]) // 0=buy, 1=sell
	price, _ := parseLeoUint(fields["price"])
	amount, _ := parseLeoUint(fields["amount"])
	deadline, _ := parseLeoUint(fields["deadline"])

	bs := match.Buy
	if side == 1 {
		bs = match.Sell
	}

	// active=false 表示已结算/已撤单（链上金额已扣减），不入撮合
	state := match.Submitted
	if a, ok := fields["active"]; ok && a == "false" {
		state = match.Canceled
	}

	return &match.Order{
		SeqId:          int64(orderID),
		OrderId:        int64(orderID),
		UserAddress:    owner,
		BuyOrSell:      bs,
		Type:           match.Limit,
		State:          state,
		Price:          decimal.NewFromInt(int64(price)),
		UnfilledAmount: decimal.NewFromInt(int64(amount)),
		CreateAt:       time.Now().UnixMilli(),
		Deadline:       int64(deadline),
	}, nil
}

// parseLeoStruct 把 "Order { a: 1u8, b: 2u64 }" 解析为 map[string]string。
func parseLeoStruct(s string) map[string]string {
	m := make(map[string]string)
	for _, mt := range leoFieldRe.FindAllStringSubmatch(s, -1) {
		m[mt[1]] = mt[2]
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// parseLeoUint 从 "100u64" / "0u8" 等提取 uint64。
func parseLeoUint(v string) (uint64, error) {
	digits := leoUintRe.FindString(v)
	if digits == "" {
		return 0, fmt.Errorf("no uint in %q", v)
	}
	return strconv.ParseUint(digits, 10, 64)
}
