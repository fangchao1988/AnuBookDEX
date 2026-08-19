package aleo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"github.com/shopspring/decimal"
)

// Settlement Aleo 结算（Phase 2b record 模型）：对每个 maker 成交 shell out
// `leo execute settle <maker_ct> <taker_ct> <price>u64 <amount>u64 --broadcast ...`。
//
// 订单输入为链上 Order record 的 ciphertext——leo CLI 用 operator 的 view key 自动解密并 spend，
// Go 侧不需要实现 record 解密。maker=买单、taker=卖单（settle 约定）。
type Settlement struct {
	rpcEndpoint string
	rpc         *RESTClient // 链上查询（verifyPublicOrderSettled 校验 mapping status）
	programID   string
	privateKey  string
	network     string // leo execute --network（testnet/mainnet）
	workingDir  string // leo 项目目录（含 program.json + src/main.leo）
	batchSize   int
	pool        *OrderPool // 订单池：按 OrderId 查 record ciphertext
	seq         uint64
	stopCh      chan struct{} // 重结算循环停止信号
	retrying    atomic.Bool   // 重结算循环防重叠（leo execute 单次耗时数十秒，周期仅 10s）

	// credentialsMu 保护当前有效 operator 凭证。credentials 是 UTXO record：每次
	// transfer_private_with_creds 消费旧凭证并输出新凭证（新 nonce）。结算成功后须
	// 从 leo 输出捕获新凭证替换，否则复用已 spent 的旧凭证会报 "input already exists"。
	credentialsMu sync.Mutex
	credentials   string // 当前有效 operator Credentials record 明文
}

// NewSettlement 创建 Aleo 结算适配器。
func NewSettlement(rpcEndpoint, programID, privateKey string, pool *OrderPool) *Settlement {
	s := &Settlement{
		rpcEndpoint: rpcEndpoint,
		rpc:         NewRESTClient(rpcEndpoint),
		programID:   programID,
		privateKey:  privateKey,
		network:     config.GetString("chain.aleo.network", "testnet"),
		workingDir:  config.GetString("chain.aleo.program-dir", "./contracts/leo/"),
		batchSize:   config.GetInt("chain.aleo.settlement-batch-size", 100),
		pool:        pool,
		stopCh:      make(chan struct{}),
		credentials: config.GetString("chain.aleo.operator-credentials", ""),
	}
	// 优先加载上次结算后轮换的凭证（UTXO 已消费、config 固定值过期）；无状态文件时回退 config。
	if rotated, err := os.ReadFile(s.credentialsStatePath()); err == nil && len(rotated) > 0 {
		s.credentials = strings.TrimSpace(string(rotated))
		common.Info("aleo settlement: loaded rotated operator credentials",
			"nonce", credsNonce(s.credentials), "file", s.credentialsStatePath())
	}
	return s
}

// credentialsStatePath 轮换凭证持久化路径（与 conf/config.yaml 同目录，避免破坏 YAML 注释）。
func (s *Settlement) credentialsStatePath() string {
	return filepath.Join(filepath.Dir(common.ConfFile), ".operator_credentials.state")
}

// currentCredentials 返回当前有效 operator 凭证明文（结算输入）。
func (s *Settlement) currentCredentials() string {
	s.credentialsMu.Lock()
	defer s.credentialsMu.Unlock()
	return s.credentials
}

// rotateCredentials 结算成功后更新 operator 凭证：从 leo 输出捕获新 Credentials record
// （owner operator + freeze_list_root + 新 nonce）并替换，供下一笔结算使用。凭证是 UTXO，
// 不在输出中找到新凭证说明已被消费但无法续上——告警提示人工处理。
func (s *Settlement) rotateCredentials(out string) {
	creds, ok := extractCredentialsRecord(out, s.privateKeyOwner())
	if !ok {
		common.Error("aleo settlement: credentials consumed but new record not found in output (rotation skipped)")
		return
	}
	s.credentialsMu.Lock()
	s.credentials = creds
	s.credentialsMu.Unlock()
	if err := os.WriteFile(s.credentialsStatePath(), []byte(creds), 0644); err != nil {
		common.Error("aleo settlement: persist rotated credentials failed", "err", err, "file", s.credentialsStatePath())
	}
	common.Info("aleo settlement: operator credentials rotated", "nonce", credsNonce(creds))
}

// privateKeyOwner 解析 operator 地址（config chain.aleo.address）。
func (s *Settlement) privateKeyOwner() string {
	return config.GetString("chain.aleo.address", "")
}

// credsRecordRe 匹配 leo execute 输出里一条 Credentials record 明文块。leo 在
// "➡️  Outputs" 下逐条打印执行输出的 record 明文（{ owner: aleo1..., freeze_list_root:
// ....private, _nonce: ....group.public, _version: 1u8.public }）。只有 Credentials 同时
// 含 freeze_list_root + _nonce + _version 三个字段，用该特征识别新凭证。
var credsRecordRe = regexp.MustCompile(`(?s)\{\s*owner:\s*([^,\n]+)\.private,\s*freeze_list_root:\s*([0-9]+)field\.private,\s*_nonce:\s*([0-9]+)group\.public,\s*_version:\s*([0-9]+)u8\.public\s*\}`)

// nonceRe 提取凭证 _nonce（日志用）。
var nonceRe = regexp.MustCompile(`_nonce:\s*([0-9]+)`)

// extractCredentialsRecord 从 leo execute 输出提取 operator 的新 Credentials 明文记录
// （settle 消费旧凭证，转移输出新凭证，owner 保持 operator）。返回格式与 config
// chain.aleo.operator-credentials 一致（单行 + .private 后缀），可直接作为 settle 输入。
// ownerAddr 非空时校验 owner 必须为 operator（避免误捕获用户的凭证）。
func extractCredentialsRecord(out, ownerAddr string) (string, bool) {
	m := credsRecordRe.FindStringSubmatch(out)
	if m == nil {
		return "", false
	}
	owner := strings.TrimSpace(m[1])
	if ownerAddr != "" && owner != ownerAddr {
		return "", false
	}
	return fmt.Sprintf(
		"{ owner: %s.private, freeze_list_root: %sfield.private, _nonce: %sgroup.public, _version: %su8.public }",
		owner, m[2], m[3], m[4]), true
}

// credsNonce 提取凭证 _nonce 值（日志展示）。
func credsNonce(creds string) string {
	if m := nonceRe.FindStringSubmatch(creds); m != nil {
		return m[1]
	}
	return ""
}

// SubmitBatch 对每个撮合结果中每个 maker 成交执行一次 settle transition。
func (s *Settlement) SubmitBatch(symbol string, mrs []*match.MatchResult) (string, error) {
	common.Info("aleo settlement: SubmitBatch enter", "symbol", symbol, "mrs", len(mrs),
		"enabled", config.GetBool("chain.aleo.settlement-enabled", true))
	// P3：撮合回执更新订单状态（委托列表数据源）
	s.pool.RecordMatch(mrs)

	// 本地联调开关：chain.aleo.settlement-enabled=false 时跳过链上 settle
	// （占位 ciphertext 无法解密，leo execute 必然失败；生产必须开启）
	if !config.GetBool("chain.aleo.settlement-enabled", true) {
		return "", nil
	}

	batchID := atomic.AddUint64(&s.seq, 1)
	settled := 0
	for _, mr := range mrs {
		takerID := mr.OrderId
		for _, item := range mr.Items {
			if item.Role != "maker" || item.OrderId == 0 {
				continue
			}
			// 取买卖双方订单（含 Order record + operator 托管资产 + 合规凭证）
			makerPO, ok := s.pool.GetOrder(item.OrderId)
			if !ok {
				common.Error("aleo settlement: maker order not found", "maker", item.OrderId)
				continue
			}
			takerPO, ok := s.pool.GetOrder(takerID)
			if !ok {
				common.Error("aleo settlement: taker order not found", "taker", takerID)
				continue
			}
			// settle 约定 maker=买、taker=卖；撮合结果中买卖方向以订单为准
			buyPO, sellPO := makerPO, takerPO
			if makerPO.Order.BuyOrSell == match.Sell {
				buyPO, sellPO = takerPO, makerPO
			}
			price := uint64(0)
			if item.Price != nil {
				price = uint64(item.Price.IntPart())
			} else {
				price = uint64(mr.Price.IntPart())
			}
			amount := uint64(0)
			if item.FilledAmount != nil {
				amount = uint64(item.FilledAmount.IntPart())
			}
			// 结算开始即回执 settling（前端显示"结算中"，不再一直 pending；
			// leo execute 单次耗时数十秒，状态可见性对用户体感重要）
			s.pool.UpdateTradeSettleStatus(item.OrderId, SettleSettling)
			if err := s.settlePair(buyPO, sellPO, price, amount); err != nil {
				common.Error("aleo settlement: settle failed",
					"maker", item.OrderId, "taker", takerID, "err", err)
				// 结算状态回执（前端展示）
				s.pool.UpdateTradeSettleStatus(item.OrderId, SettleFailed)
				continue
			}
			// 结算成功回执（前端展示）
			s.pool.UpdateTradeSettleStatus(item.OrderId, SettleSettled)
			settled++
		}
	}
	txID := fmt.Sprintf("aleo_batch_%d_%s_%dsettle", batchID, symbol, settled)
	common.Info("aleo settlement: batch done",
		"symbol", symbol, "mrs", len(mrs), "settles", settled, "tx", txID)
	return txID, nil
}

// settlePair 按买卖双方的托管形态（Mode）路由 4 种 settle 组合（SubmitBatch 与
// 重结算循环共用）。核心规则：收益形态 = 对手方托管形态（买方收卖方托管形态的
// ALEO，卖方收买方托管形态的 USDCX）。
//   - public × public → settle_pp（双公开，纯 final 块，无 record 输入）
//   - private × public → settle_vp（消费 maker Order + op_usdcx + 凭证；taker 公开校验）
//   - public × private → settle_pv（消费 taker Order + op_aloe；maker 公开校验）
//   - private × private → settle_vv（p5 路径 + trader）
// settle 的合规凭证统一用 operator 自有凭证（chain.aleo.operator-credentials）：
// transfer_private_with_creds 的 Credentials 输出保持原 owner（用户），operator 解不了
// 也 spend 不了用户凭证；合约只校验 freeze_list_root 不校验 owner，operator 凭证可复用。
func (s *Settlement) settlePair(buyPO, sellPO *PooledOrder, price, amount uint64) error {
	creds := s.currentCredentials()
	if creds == "" {
		return fmt.Errorf("operator-credentials 未配置（需 operator 先领凭证）")
	}
	// 合约语义：maker=买方订单、taker=卖方订单（settle 断言 maker.side==0 / taker.side==1）。
	// 此前误传 mr.OrderId（新订单可能是买或卖），导致 settle_pp/vp 的 taker_order_id 指向
	// 买方、finalize 校验 taker.user 失败（exit 248 rejected）——统一用买卖双方订单 ID
	makerID := buyPO.Order.OrderId  // 买方订单 ID
	takerID := sellPO.Order.OrderId // 卖方订单 ID
	switch {
	case buyPO.Mode == "public" && sellPO.Mode == "public":
		// 两阶段：await 外部 transfer_public 在 testnet 不可行（finalize 输入错位，实测
		// rejected）——先由 operator 直调转账（credits/USDCX transfer_public），
		// 再调 settle_pp 校验并标记 status。转账失败即返回（重试循环兜底，
		// 已成功的转账幂等判定 "already exists" 视为成功）。
		quoteOut := price * amount / 1000000 // USDCX 微单位（u128）
		if err := s.executeTransfer("credits.aleo::transfer_public", []string{
			buyPO.Order.UserAddress, // 买方收公开 ALEO（from=operator 托管余额）
			strconv.FormatUint(amount, 10) + "u64",
		}, price, amount); err != nil {
			return fmt.Errorf("settle_pp 前置 credits 转账: %w", err)
		}
		if err := s.executeTransfer("test_usdcx_stablecoin.aleo::transfer_public", []string{
			sellPO.Order.UserAddress, // 卖方收公开 USDCX（from=operator 托管余额）
			strconv.FormatUint(quoteOut, 10) + "u128",
		}, price, amount); err != nil {
			return fmt.Errorf("settle_pp 前置 USDCX 转账: %w", err)
		}
		return s.settleTx("settle_pp", []string{
			strconv.FormatInt(makerID, 10) + "u128",
			strconv.FormatInt(takerID, 10) + "u128",
			buyPO.Order.UserAddress,  // maker_owner（买方）
			sellPO.Order.UserAddress, // taker_owner（卖方）
			strconv.FormatUint(price, 10) + "u64",
			strconv.FormatUint(amount, 10) + "u64",
		}, price, amount, takerID)
	case buyPO.Mode == "private" && sellPO.Mode == "public":
		// 两阶段：先直调 credits transfer_public 给买方（隐私买收公开 ALEO）
		if err := s.executeTransfer("credits.aleo::transfer_public", []string{
			buyPO.Order.UserAddress,
			strconv.FormatUint(amount, 10) + "u64",
		}, price, amount); err != nil {
			return fmt.Errorf("settle_vp 前置 credits 转账: %w", err)
		}
		return s.settleTx("settle_vp", []string{
			buyPO.Ciphertext,
			buyPO.OpFund,              // op_usdcx（USDCX Token 托管）
			creds,                     // operator 凭证
			strconv.FormatInt(takerID, 10) + "u128",
			sellPO.Order.UserAddress, // taker_owner（卖方）
			strconv.FormatUint(price, 10) + "u64",
			strconv.FormatUint(amount, 10) + "u64",
		}, price, amount, takerID)
	case buyPO.Mode == "public" && sellPO.Mode == "private":
		// 两阶段：先直调 USDCX transfer_public 给卖方（隐私卖收公开 USDCX）
		quoteOut := price * amount / 1000000
		if err := s.executeTransfer("test_usdcx_stablecoin.aleo::transfer_public", []string{
			sellPO.Order.UserAddress,
			strconv.FormatUint(quoteOut, 10) + "u128",
		}, price, amount); err != nil {
			return fmt.Errorf("settle_pv 前置 USDCX 转账: %w", err)
		}
		return s.settleTx("settle_pv", []string{
			sellPO.Ciphertext,
			sellPO.OpFund,             // op_aloe（ALEO credits 托管）
			strconv.FormatInt(makerID, 10) + "u128",
			buyPO.Order.UserAddress,  // maker_owner（买方）
			strconv.FormatUint(price, 10) + "u64",
			strconv.FormatUint(amount, 10) + "u64",
		}, price, amount, makerID)
	default:
		// 双隐私（settle_vv）：无公开 mapping 可验证，只能以广播结果为准
		return s.settleTx("settle_vv", []string{
			buyPO.Ciphertext,
			sellPO.Ciphertext,
			sellPO.OpFund,
			buyPO.OpFund,
			creds,
			strconv.FormatUint(price, 10) + "u64",
			strconv.FormatUint(amount, 10) + "u64",
		}, price, amount, 0)
	}
}

// executeTransfer 前置公开转账：转账无公开 mapping 可验证，幂等判定"已存在"视为成功
// （已成功广播的转账重放时节点报 already-exists 即已入账；内存池 pending 的情况由后续
// settle 的 mapping 校验兜底）。executeSettleVV 已不复用 executeTransition 的幂等判定。
func (s *Settlement) executeTransfer(fn string, args []string, price, amount uint64) error {
	_, err := s.executeTransition(fn, args, price, amount)
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "already exists") {
		common.Info("aleo settlement: "+fn+" transfer already on-chain (idempotent)",
			"price", price, "amount", amount)
		return nil
	}
	return err
}

// executeTransition 泛化 leo execute 广播：settle_pp（双公开）/ settle_vp（买隐私×卖公开）/
// settle_pv（买公开×卖隐私）/ settle_vv（双隐私）与前置公开转账共用一个执行器。
// 成功时返回 leo 输出（含执行过渡输出的 record 明文，供凭证轮换解析）。
func (s *Settlement) executeTransition(fn string, args []string, price, amount uint64) (string, error) {
	// 私钥兜底：config chain.aleo.private-key -> ALEO_PRIVATE_KEY（viper BindEnv 对嵌套 key 不可靠）
	priv := s.privateKey
	if priv == "" {
		priv = os.Getenv("ALEO_PRIVATE_KEY")
	}
	full := append([]string{"execute", fn}, args...)
	full = append(full,
		"--broadcast",
		"--endpoint", s.rpcEndpoint,
		"--network", s.network,
		"--yes",
		// 用链上版本程序构建证明（本地合约与链上字节码不同会导致广播被节点拒绝）
		"--no-local",
	)
	if priv != "" {
		full = append(full, "--private-key", priv)
	}
	cmd := exec.Command("leo", full...)
	cmd.Dir = s.workingDir
	out, err := cmd.CombinedOutput()
	if err == nil {
		common.Debug("aleo settlement: leo execute "+fn+" ok",
			"price", price, "amount", amount,
			"out", strings.TrimSpace(string(out)))
		return string(out), nil
	}
	outStr := strings.TrimSpace(string(out))
	// 返回原始输出：leo 在执行完成（含打印 "➡️  Outputs" 的 record 明文）后才尝试广播，
	// 广播层 HTTP 失败（522）时交易仍可能被节点确认并消费凭证——错误分支也带回输出，
	// 供 settleTx 在链上已确认时轮换凭证。
	// 不再把 "already exists" 直接视为幂等成功：该文本既可能在交易已确认时出现，
	// 也可能在交易仍停留内存池/被节点拒时出现（实测 settle_vp 被判幂等成功但链上
	// mapping status 仍为 0）。广播层失败统一返回 error，由 settleTx 查链上最终状态
	// （public_orders.status==1）决定是否幂等成功——明确已确认才放行。
	return string(out), fmt.Errorf("leo execute %s: %w; out: %s", fn, err, outStr)
}

// settleTx 执行 settle 并校验链上最终状态：广播失败（含 already-exists）不直接定论，
// 对公开侧订单查 public_orders mapping 的 status 字段——已置 1 才是真正结算完成。
// verifyOrderID<=0（如 settle_vv 双隐私无公开 mapping 可查）时只能以广播结果为准。
// 结算成功且该函数消费 operator 凭证时轮换凭证（UTXO 已消耗，必须捕获新凭证续用）。
func (s *Settlement) settleTx(fn string, args []string, price, amount uint64, verifyOrderID int64) error {
	out, txErr := s.executeTransition(fn, args, price, amount)
	if verifyOrderID <= 0 {
		// 双隐私等无公开 mapping 可验证：以广播结果为准
		if txErr == nil && consumesCredentials(fn) {
			s.rotateCredentials(out)
		}
		return txErr
	}
	verr := s.verifyPublicOrderSettled(verifyOrderID)
	switch {
	case verr == nil:
		// 链上 mapping 已置 1：广播层可能报错（leo 状态查询失败/已存在），但真结算完成
		if consumesCredentials(fn) {
			s.rotateCredentials(out)
		}
		common.Info("aleo settlement: "+fn+" verified on-chain",
			"order_id", verifyOrderID, "price", price, "amount", amount)
		return nil
	case txErr == nil:
		// 广播成功但 mapping 未置位：finalize 可能被拒（记录最终状态不匹配时）。
		// 返回错误，重试循环继续（record 未消费、重试安全）。
		return fmt.Errorf("%s broadcast ok but on-chain verify failed: %w", fn, verr)
	default:
		// 广播失败 + 链上未置位：返回广播错误，重试循环继续
		common.Warn("aleo settlement: "+fn+" broadcast fail + verify fail",
			"order_id", verifyOrderID, "err", txErr, "verify", verr, "out", truncateBytes([]byte(txErr.Error()), 200))
		return txErr
	}
}

// consumesCredentials 该 transition 是否消费 operator 凭证（transfer_private_with_creds）——
// 消费后旧凭证不可复用，成功后须从输出捕获新凭证轮换。settle_pp 双公开、settle_pv 买公开
// ×卖隐私均不消费凭证，无需轮换。
func consumesCredentials(fn string) bool {
	return strings.HasPrefix(fn, "settle_vp") || strings.HasPrefix(fn, "settle_vv") ||
		strings.HasPrefix(fn, "cancel_buy_private")
}

// verifyPublicOrderSettled 查链上 public_orders[orderID].status==1（settle 的 final 已生效）。
func (s *Settlement) verifyPublicOrderSettled(orderID int64) error {
	key := fmt.Sprintf("%dfield", orderID)
	raw, err := s.rpc.GetProgramMapping(s.programID, "public_orders", key)
	if err != nil {
		return fmt.Errorf("query public_orders[%d]: %w", orderID, err)
	}
	// GET /mapping 返回 JSON string wrapper（`\n` 是字面反斜杠+n），剥离后才能解析字段；
	// 此前未剥离导致 `status: 1u8\n}` 的 `` 终止不了字段 → status 缺失 → 误判 status=0。
	fields := parseRecordPlaintext(unquoteJSON(raw))
	if fields["status"] != 1 {
		return fmt.Errorf("public_orders[%d].status=%d，want 1（结算 final 未生效）；mapping=%s",
			orderID, fields["status"], truncateBytes([]byte(raw), 400))
	}
	return nil
}

// StartRetryLoop 启动后台重结算循环：定期扫描结算失败的成交重新执行 settle。
// 失败的结算交易未广播、record 未消费，重试安全；成功后前端结算状态回执为 settled。
// 10s 周期：executeSettle 已不内置重试，失败单由本循环快速兜底，前端能尽快看到 settled。
func (s *Settlement) StartRetryLoop() {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.retryFailedSettlements()
			}
		}
	}()
}

// retryFailedSettlements 扫描失败成交并重试链上结算（买卖侧输入与 SubmitBatch 同规则）。
func (s *Settlement) retryFailedSettlements() {
	// 防重叠：上一轮还没跑完（leo execute 慢，10s 周期会追上）时跳过本轮
	if !s.retrying.CompareAndSwap(false, true) {
		return
	}
	defer s.retrying.Store(false)
	for _, t := range s.pool.ListFailedTrades() {
		makerPO, ok1 := s.pool.GetOrder(t.OrderId)
		takerPO, ok2 := s.pool.GetOrder(t.TakerOrderId)
		if !ok1 || !ok2 {
			common.Warn("aleo settlement: retry skip, order missing", "maker", t.OrderId, "taker", t.TakerOrderId)
			continue
		}
		buyPO, sellPO := makerPO, takerPO
		if makerPO.Order.BuyOrSell == match.Sell {
			buyPO, sellPO = takerPO, makerPO
		}
		price, err := decimal.NewFromString(t.Price)
		if err != nil || price.Sign() <= 0 {
			common.Error("aleo settlement: retry skip, bad price", "trade", t.OrderId, "price", t.Price)
			continue
		}
		amount, err := decimal.NewFromString(t.Amount)
		if err != nil || amount.Sign() <= 0 {
			common.Error("aleo settlement: retry skip, bad amount", "trade", t.OrderId, "amount", t.Amount)
			continue
		}
		s.pool.UpdateTradeSettleStatus(t.OrderId, SettleSettling)
		if err := s.settlePair(buyPO, sellPO,
			uint64(price.IntPart()), uint64(amount.IntPart())); err != nil {
			common.Error("aleo settlement: retry settle failed", "maker", t.OrderId, "taker", t.TakerOrderId, "err", err)
			continue
		}
		s.pool.UpdateTradeSettleStatus(t.OrderId, SettleSettled)
		common.Info("aleo settlement: retry settled ok", "maker", t.OrderId, "taker", t.TakerOrderId)
	}
}

// Shutdown 关闭结算适配器（停止重结算循环）。
func (s *Settlement) Shutdown() {
	close(s.stopCh)
	common.Info("aleo settlement: shutdown")
}
