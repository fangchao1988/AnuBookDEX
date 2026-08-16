package aleo

import (
	"fmt"
	"os"
	"strings"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"github.com/shopspring/decimal"
)

// OrderPayload 从链上下单交易提取并解密的订单载荷（标准/隐私下单共用）：
// Order 解密后的订单明文（撮合用）；OrderCT 链上 Order record ciphertext（settle 输入，
// leo execute 用 operator 私钥自动解密并 spend）；OpFund/Creds 是 operator 托管资产与
// 合规凭证的 record 明文（settle 输入，view key 解密）。
type OrderPayload struct {
	Order   *match.Order
	OrderCT string
	OpFund  string
	Creds   string
}

const (
	usdcxProgramID   = "test_usdcx_stablecoin.aleo"
	creditsProgramID = "credits.aleo"
)

// programIDFor 按交易对解析链上程序 id：
// chain.aleo.program-id-by-symbol.<SYMBOL> 优先，回退 chain.aleo.program-id
// （ALEO/USDCX 用 p4；ETH/USDT 铸币模式用 p2，见配置）。
func programIDFor(symbol string) string {
	sym := strings.TrimSpace(symbol)
	if sym != "" {
		if pid := config.GetString("chain.aleo.program-id-by-symbol."+sym, ""); pid != "" {
			return pid
		}
	}
	return config.GetString("chain.aleo.program-id", "anubook_dex_p5.aleo")
}

// operatorViewKey 读取 operator view key：chain.aleo.view-key-private -> ALEO_VIEW_KEY
// （viper BindEnv 对嵌套 key 不可靠，env 兜底）。
func operatorViewKey() string {
	if vk := config.GetString("chain.aleo.view-key-private", ""); vk != "" {
		return vk
	}
	return os.Getenv("ALEO_VIEW_KEY")
}

// ExtractAndDecryptOrder 从链上交易提取并解密下单产出：
//  1. GetTransaction(txID)
//  2. 定位 dex transition（program=programID，function=place_order_buy/sell）
//  3. Order CT = dex transition 首个 record output（Order 是唯一本体 record，其余为 external_record）
//  4. 托管资产/凭证在跨程序 inner transition 的 record outputs（record 本体在 inner）：
//     buy:  transfer_private_with_creds outputs[2]=Token托管、[3]=Credentials
//     sell: credits.aleo::transfer_private outputs[1]=credits托管
//  5. operator view key 解密 Order -> 明文字段（order_id/side/price/amount/deadline）-> match.Order
//     解密 OpFund/Creds -> record 明文（leo execute settle 输入，格式与 --json-output 一致）
func ExtractAndDecryptOrder(rpc *RESTClient, txID, programID string) (*OrderPayload, error) {
	tx, err := rpc.GetTransaction(txID)
	if err != nil {
		return nil, fmt.Errorf("query tx: %w", err)
	}
	execution, ok := tx["execution"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("tx has no execution")
	}
	transitions, _ := execution["transitions"].([]interface{})
	if len(transitions) == 0 {
		return nil, fmt.Errorf("tx has no transitions")
	}

	// 定位 dex transition 与函数名
	dexTr, function, err := findTransition(transitions, programID)
	if err != nil {
		return nil, err
	}
	orderCT, err := firstRecordOutput(dexTr)
	if err != nil {
		return nil, fmt.Errorf("order record: %w", err)
	}

	// 托管资产/合规凭证：跨程序调用以 inner transition 形式出现在 execution.transitions
	payload := &OrderPayload{OrderCT: orderCT}
	switch function {
	case "place_order_buy":
		recs, err := recordOutputsOf(transitions, usdcxProgramID, "transfer_private_with_creds")
		if err != nil {
			return nil, err
		}
		if len(recs) < 4 {
			return nil, fmt.Errorf("transfer_private_with_creds records=%d, want 4", len(recs))
		}
		payload.OpFund, payload.Creds = recs[2], recs[3] // Token托管 + Credentials
	case "place_order_sell":
		recs, err := recordOutputsOf(transitions, creditsProgramID, "transfer_private")
		if err != nil {
			return nil, err
		}
		if len(recs) < 2 {
			return nil, fmt.Errorf("credits transfer_private records=%d, want 2", len(recs))
		}
		// credits.aleo::transfer_private 输出 (credits, credits)：[0]=转给 operator 的托管、
		// [1]=找零给 sender（可能为 0u64，settle 用它做 op_aloe 会下溢）
		payload.OpFund = recs[0] // credits 托管
	default:
		// p2 铸币兼容（place_order）：仅提取 Order record，无托管资产/凭证
		common.Debug("aleo extract: function not p4 buy/sell (p2 place_order?)", "function", function)
	}

	// 解密（operator view key；Order/托管资产/凭证的 owner 均为 operator）
	vk := operatorViewKey()
	if vk == "" {
		return nil, fmt.Errorf("operator view key 未配置（chain.aleo.view-key-private 或 ALEO_VIEW_KEY）")
	}
	orderPlain, err := decryptRecord(orderCT, vk)
	if err != nil {
		return nil, fmt.Errorf("decrypt order: %w", err)
	}
	order, err := parseOrderPlaintext(orderPlain)
	if err != nil {
		return nil, fmt.Errorf("parse order: %w", err)
	}
	payload.Order = order
	if payload.OpFund != "" {
		if payload.OpFund, err = decryptRecord(payload.OpFund, vk); err != nil {
			return nil, fmt.Errorf("decrypt op_fund: %w", err)
		}
	}
	// 凭证（Credentials）归下单用户而非 operator（transfer_private_with_creds 输出保持
	// 原 owner），operator view key 解密必失败——降级不阻塞：settle 统一使用 operator
	// 自有凭证（chain.aleo.operator-credentials），不依赖本字段。
	if payload.Creds != "" {
		if plain, derr := decryptRecord(payload.Creds, vk); derr != nil {
			common.Warn("aleo extract: credentials belongs to user (owner != operator), skip decrypt", "err", truncateBytes([]byte(derr.Error()), 120))
			payload.Creds = ""
		} else {
			payload.Creds = plain
		}
	}
	return payload, nil
}

// parseOrderPlaintext 解密后的 Order record 明文 -> match.Order 字段。
// 明文格式：{ owner: aleo1..., order_id: 2u128.private, side: 0u8.private,
// price: 15784u64.private, amount: 63355300u64.private, deadline: 18789244u32.private, ... }
func parseOrderPlaintext(plain string) (*match.Order, error) {
	fields := parseRecordPlaintext(plain)
	orderId := fields["order_id"]
	side := fields["side"]
	price := fields["price"]
	amount := fields["amount"]
	deadline := fields["deadline"]
	if orderId == 0 || price == 0 || amount == 0 {
		return nil, fmt.Errorf("order fields incomplete: %q", truncateBytes([]byte(plain), 200))
	}
	bs := match.Buy
	if side == 1 {
		bs = match.Sell
	}
	return &match.Order{
		SeqId:          orderId,
		OrderId:        orderId,
		BuyOrSell:      bs,
		Type:           match.Limit,
		State:          match.Submitted,
		Price:          decimal.NewFromInt(price),
		UnfilledAmount: decimal.NewFromInt(amount),
		CreateAt:       common.TimestampNowMs(),
		Deadline:       deadline,
	}, nil
}

// findTransition 定位 program 匹配的 transition，返回 transition map + function 名。
func findTransition(transitions []interface{}, programID string) (map[string]interface{}, string, error) {
	for _, t := range transitions {
		tr, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		if transitionProgram(tr) != programID {
			continue
		}
		fn, _ := tr["function"].(string)
		return tr, fn, nil
	}
	return nil, "", fmt.Errorf("transition of %s not found in tx", programID)
}

// transitionProgram 新版 snarkOS transition 字段为 program，旧版为 program_id（兼容两者）。
func transitionProgram(tr map[string]interface{}) string {
	if pid, _ := tr["program"].(string); pid != "" {
		return pid
	}
	pid, _ := tr["program_id"].(string)
	return pid
}

// firstRecordOutput 返回 transition 第一个 record output 的 ciphertext。
func firstRecordOutput(tr map[string]interface{}) (string, error) {
	outputs, _ := tr["outputs"].([]interface{})
	for _, o := range outputs {
		om, ok := o.(map[string]interface{})
		if !ok {
			continue
		}
		if om["type"] != "record" {
			continue
		}
		v, _ := om["value"].(string)
		if isRecordCiphertext(v) {
			return v, nil
		}
	}
	return "", fmt.Errorf("record ciphertext not found in transition outputs")
}

// recordOutputsOf 收集指定 program+function 的 transition 全部 record ciphertexts（按输出顺序）。
func recordOutputsOf(transitions []interface{}, programID, function string) ([]string, error) {
	for _, t := range transitions {
		tr, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		if transitionProgram(tr) != programID {
			continue
		}
		fn, _ := tr["function"].(string)
		if fn != function {
			continue
		}
		outputs, _ := tr["outputs"].([]interface{})
		recs := make([]string, 0, len(outputs))
		for _, o := range outputs {
			om, ok := o.(map[string]interface{})
			if !ok {
				continue
			}
			if om["type"] != "record" {
				continue
			}
			v, _ := om["value"].(string)
			if isRecordCiphertext(v) {
				recs = append(recs, v)
			}
		}
		return recs, nil
	}
	return nil, fmt.Errorf("transition %s::%s not found in tx", programID, function)
}

func isRecordCiphertext(v string) bool {
	return strings.HasPrefix(v, "record1") || strings.HasPrefix(v, "ciphertext1")
}
