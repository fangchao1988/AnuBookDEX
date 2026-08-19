package aleo

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/spf13/viper"
)

// TestManualSettleVP 诊断/补救隐私买单 1787019311912 × 公开卖 1787019175250 的结算。
//
// 背景：引擎 executeTransition 的 "already exists" 幂等判定把被拒/内存池交易误判为已上链，
// 链上 public_orders[1787019175250].status 实测仍为 0（结算 final 未生效）。但 settle_vp 广播
// 又被节点拒绝："input ID ... already exists in the ledger"——说明结算用到的 record 已处于
// 已花费记账状态。本测试绕过节流逻辑，直接 exec leo 看节点完整响应，分辨是哪个 record 被消费。
//
// 模式（SETTLE_MODE 环境变量）：
//   - settle（默认）：重新广播 settle_vp（若 record 未消费则真正完成结算）
//   - cancel：广播 cancel_buy_private（消费 Order+op_usdcx）退回资产——若成功说明 Order 未消耗、
//     结算确未完成；若被拒 input already exists 则确认 record 已被 spend
func TestManualSettleVP(t *testing.T) {
	viper.SetConfigFile("/Users/fangchao/GolandProjects/AnuBookDEX/conf/config.yaml")
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	common.ZapInit() // 测试环境默认无 zap logger，Warn 会 nil 指针 panic
	rpc := envOr("SETTLE_ENDPOINT", viper.GetString("chain.aleo.rpc-endpoint"))
	// 提取端点是 explorer（transaction 正常）；广播端点可分离到备选 provable.com（statePaths 快）
	extractRPC := envOr("SETTLE_EXTRACT_ENDPOINT", rpc)
	programID := viper.GetString("chain.aleo.program-id")
	priv := viper.GetString("chain.aleo.private-key")
	network := viper.GetString("chain.aleo.network")
	workingDir := "/Users/fangchao/GolandProjects/AnuBookDEX/contracts/leo" // 测试 cwd 是包目录，相对路径会解析失败
	credStatePath := "/Users/fangchao/GolandProjects/AnuBookDEX/conf/.operator_credentials.state"
	creds := viper.GetString("chain.aleo.operator-credentials") // config 固定值（可能已被结算消费）
	if b, err := os.ReadFile(credStatePath); err == nil && len(b) > 0 {
		creds = strings.TrimSpace(string(b)) // 优先用轮换后的当前有效凭证
		t.Logf("using rotated operator credentials (state file)")
	}

	var (
		privacyBuyTx = envOr("SETTLE_TX", "") // 隐私买单链上下单交易（完整 txid，用户从钱包提供）
		sellOrderID  = envOr("SETTLE_SELL_ORDER", "1787019175250")
		seller       = envOr("SETTLE_SELLER", "aleo1cwzattgxzw7kxklq0rp0dxntn27yf9qftjk2jrk5qv73r4lkngxqvhsw9v")
		priceU       = envOr("SETTLE_PRICE", "16200")
		amountU      = envOr("SETTLE_AMOUNT", "100000")
		mode         = envOr("SETTLE_MODE", "settle")
	)
	if privacyBuyTx == "" {
		t.Skip("SETTLE_TX 未设置：需从用户钱包获取隐私买单的完整链上交易 id")
	}

	payload, err := ExtractAndDecryptOrder(NewRESTClient(extractRPC), privacyBuyTx, programID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("extracted: order_id=%d side=%d price=%s amount=%s",
		payload.Order.OrderId, payload.Order.BuyOrSell,
		payload.Order.Price.String(), payload.Order.UnfilledAmount.String())

	os.WriteFile("/tmp/manual_order_ct.txt", []byte(payload.OrderCT), 0o644)
	// op_fund 覆盖：诊断/补救时托管 Token 可能已被 join 合并成新的 record（record 是 UTXO，
	// 合并后原 record 已 spent，settle 必须用合并后的 Token）——SETTLE_OP_FUND_FILE 指定明文
	opFund := payload.OpFund
	if f, err := os.ReadFile(envOr("SETTLE_OP_FUND_FILE", "")); err == nil && len(f) > 0 {
		opFund = strings.TrimSpace(string(f))
		t.Logf("using op_fund override from %s (amount 需 >= quote_out)", envOr("SETTLE_OP_FUND_FILE", ""))
	}
	os.WriteFile("/tmp/manual_op_fund.txt", []byte(opFund), 0o644)
	os.WriteFile("/tmp/manual_creds.txt", []byte(creds), 0o644)

	var args []string
	switch mode {
	case "settle":
		args = append([]string{"execute", "settle_vp",
			payload.OrderCT, opFund, creds,
			sellOrderID + "u128", seller, priceU + "u64", amountU + "u64"}, broadcastFlags(rpc, network, priv)...)
	case "cancel":
		// cancel_buy_private(Order, op_usdcx, op_creds) 只消费记录并退回，不依赖撮合
		args = append([]string{"execute", "cancel_buy_private",
			payload.OrderCT, opFund, creds}, broadcastFlags(rpc, network, priv)...)
	default:
		t.Fatalf("unknown SETTLE_MODE=%s (settle|cancel)", mode)
	}

	cmd := exec.Command("leo", args...)
	cmd.Dir = workingDir
	cmd.Env = append(os.Environ(), "RUST_LOG=debug")
	var stdout, stderrBuf bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderrBuf
	err = cmd.Run()
	outStr := strings.TrimSpace(stdout.String())
	errStr := strings.TrimSpace(stderrBuf.String())
	t.Logf("MODE=%s leo exit err=%v", mode, err)
	t.Logf("leo stdout:\n%s", outStr)
	t.Logf("leo stderr:\n%s", errStr)
	if err != nil {
		t.Fatalf("leo execute %s exited non-zero: %v", mode, err)
	}
}

func broadcastFlags(rpc, network, priv string) []string {
	return []string{
		"--broadcast",
		"--endpoint", rpc,
		"--network", network,
		"--yes",
		"--no-local",
		"--private-key", priv,
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}