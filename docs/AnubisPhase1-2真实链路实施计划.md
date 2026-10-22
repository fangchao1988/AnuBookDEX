# Anubis Phase 1-2 真实链路实施计划（含前后端）

> 版本：v1.2　|　日期：2026-09-05　|　状态：**Phase 1 后端完成并通过测试网全链验证**　|　关联：[Aleo改造方案.md](Aleo改造方案.md)（双链同步）、[DEX改造方案.md](DEX改造方案.md)

## 进度（2026-09-05）

**已完成 —— Anubis 测试网（Cheras, chainId 202601）全链验证通过**：

| 环节 | 结果 |
|------|------|
| 测试网连通 | `eth_chainId=0x31769(202601)`、`net_version=202601`、区块 1400 万+、gasPrice 1 gwei |
| 合约部署 | Settlement v2 `0x2Eca1b3DA6A43166660c2192bEb5E234E79d28e6`；OrderBookRegistry `0x12cF0C1A8855e56CFBFc78b3845925b5c0d26514`；owner/operator = `0xfB2Efb…b412`（2.0 DAI 测试余额） |
| 引擎授权 | `authorizeEngine` 成功（链上 `authorizedEngines=true`） |
| 订单通道 | `POST /order` → OrderPool → Subscriber 路由 → 撮合（`state=matched`） |
| **链上结算** | **真实 EIP-1559 交易广播/上链/回执 `status=1`**，gasUsed 171k；`BatchSettled(batchId=2, vol=22500, fee=25)`；`Unshielded(user=0x1111…, amount=7497)` 收款方正确；`batchSeq`/`stateRoot` 链上与引擎解析一致 |

**实施中修复的 4 个真实缺陷**（均有回归测试或链上复验）：
1. **go-ethereum tuple 字段名不匹配** — `abi:"id"` → `ToCamelCase`= `Id`，结构体字段为 `ID`，导致 `abi pack` 静默失败、结算永远停在 queued。加 `abi` tag（[settlement_binding.go](internal/dex/chain/anubis/settlement_binding.go)）+ 3 个单测。
2. **合约 `/1e18` 精度错误** — 结构体注释声明 price/amount 为最小单位，引擎 decimal 亦为最小单位，合约再除 1e18 致 `totalVolume=0`。修为直接相乘（[Settlement.sol:191](contracts/anubis/contracts/Settlement.sol#L191)），重新部署。
3. **收款地址为���地址** — 撮合核心 `UserAddress` 字段从未填充（Phase 2 预留），改为结算时从订单池按 order id 解析 trader（`encodeMatchResults` + `OrderPool.Record`）。
4. **私钥 key 名不匹配** — `config.go` 绑定 `metamask-private-key`，代码仍读 `private-key` → 静默拿不到 key。新增 `operatorPrivateKey()`（metamask 优先、回落通用）。

### 追加：真实代币结算（Settlement v3，2026-10-03 完成）

| 环节 | 结果 |
|------|------|
| 测试币部署 | TETH `0x0Ac44Ef4…3Be99` / TUSDT `0x7233112f…2aF0` / TSOL `0x7405ac42…DfA3F`（0 位小数，公开 mint） |
| Settlement v3 | `0xB11c339001b8db3827aD3047BA8Beb07Ca60e9Af` —— 金库模式：`deposit`/`withdraw` + `submitBatch` 内真实 ERC-20 划转 |
| 双向 E2E | 买 100 TETH@200 → 再卖 50 TETH@200；终态 TETH `50/50`、TUSDT `9985/9970`、协议费 45，**守恒精确** |
| 前端 | 金库面板（`AnubisVaultPanel`）：ERC-20 余额/授权/入金/提现；引擎 `/api/v1/tokens` 下发合约地址与币对代币 |

**本阶段修复的真实缺陷**：
1. **未成交单被当成交下发** —— 撮合器对无对手方的挂单也产出 `state=submitted` 的 item，且 `AccFilledAmount` 是该单总量。旧编码未过滤 state → 同一地址被重复扣款 → 合约 `insufficient quote balance` 整批回滚。已改为只编码 `filled/partial-filled`（+ 回归测试）。
2. **手写函数选择器错误** —— 前端把 `vaultBalance` 写成 `0x8b8e6b8a`（实际 `0x152bceca`），无编译报错、仅链上静默失败。已加 `SelectorSync.test.ts` 与 ABI 对账。
3. **Anubis gas 代币 DAI 是 ERC-20** —— 原生币不可转账（节点拒绝 `value>0`，错误信息伪装成 `insufficient funds`）；给测试账户发 gas 必须走 `DAI.transfer`。

**未完成 / 遗留**：
- ⏳ 前端 Part 3（EVM 钱包适配器 + ECIES + 链切换下单表单）未开始。
- ⏳ 隐私模式（`mode=private`）端到端未验证：需前端 ECIES 加密 + `ANUBIS_VIEW_KEY` 配置 + 合约侧 Registry 审计锚定。
- ⚠️ `internal/core/{match,market,l2quote}` 既有断言测试失败为 **pre-existing**（MarshalJSON 字段漂移，本任务未触碰撮合核心），留待专项。
- ⚠️ 主网（chainId 6714）仍指向 v1 合约 `0xe1E7...965`（`requireProof` 硬校验 0x0100，电路参数未公开前不可用），需另行部署 v2。

---

## Context

Aleo 侧 Phase 1-3 已完成（p7 公开/隐私混合，testnet 联调通过）；**Anubis 侧全部是脚手架/STUB**：`subscriber.go` 的 `DecryptOrder` not implemented、`settlement.go` 的 `submitToChain` 只打日志不提交、`internal/dex/privacy/` 是无人引用的占位、前端是 Aleo-only。本计划把 Anubis 接到**真实 Anubis 主网**（chainId 6714，`rpc.anubispace.org` HTTP JSON-RPC 已实测可用；WS 不可用）。

> 合约工程已从独立兄弟仓库 `AnuBookDEX-contracts` 移入 **`contracts/anubis/`**（硬帽子工程，与 `contracts/leo/` 分区），后续在此开发。

**已实测核实的硬约束（2026-09-01 主网探测）**：
- 已部署 Settlement `0xe1E7...965` 的 `submitBatch` **硬性调用 `0x0100` PLONK 验证**（`_verifyZKProof` → VERIFY_PROOF.staticcall）；实测 `0x0100/0x0103/0x0104` 对任意输入返回**空 returndata**（`staticcall` success=true 但返回 <32B）→ 无真证明的调用必然 revert（"ZK proof failed"），**无法直接复用 → 需部署 v2**。
- 已部署 OrderBookRegistry `0x5037...9d8` 的 `submitOrder` 实测可行（`(bool ok,) = staticcall(...)` 的 `ok`=调用 success，0x0103 是空响应的静默 no-op）；但只存 `nullifier+deadline`，无订单字段/载荷 → 无法链上订阅拿可成交订单。
- 仓库已 vendor 化，无 go-ethereum/gnark；`privacy/` 的 ECIES(AES-GCM)/decrypt 代码真实但未被引用。
- 前端 `web/` 无线程 EVM 钱包接入（Aleo-only）。

**已确认的决策**：
1. **订单通道** = 链下订单池（镜像 Aleo p7：前端 POST /order → 引擎池 → 撮合 → 链上结算）。
2. **结算合约** = 部署 Settlement **v2**（`requireProof` 可关，承诺哈希模式），主网部署 + `authorizeEngine`。
3. **EVM 依赖** = 引入 go-ethereum（ethclient + ABI + 签名）。
4. **ZK 证明** = 保持哈希承诺 MVP，`0x0100` 真 PLONK 明确延后（等 Anubis 电路参数公开）。

目标架构（与 Aleo p7 对称，共享撮合核心零改动）：

```
前端 EVM 钱包(MetaMask) ──构造订单(隐私可 ECIES 加密到引擎 view-key)+personal_sign──┐
                                                                                  ▼
                                       POST /order → chain/anubis/orderpool → Subscriber 消费
                                          └ (可选) Registry.submitOrder(nullifier) 审计锚定
engine ─ match（复用）─ SettlementSink.submitBatch
                                          └ 真实 EVM tx → Settlement v2.submitBatch(commitHash)
                                             → eth_sendRawTransaction → eth_getTransactionReceipt → BatchSettled 事件
WS 行情广播(复用 ws/hub) + /api/v1/{orders,trades,operator,balance}
```

---

## Part 1 — 合约（`contracts/anubis/`，已移入本仓库）

1. **`contracts/anubis/contracts/Settlement.sol` v2 化**（最小改）：
   - 新增 `bool public requireProof`（默认 `false`=承诺哈希模式）+ `setRequireProof`（onlyOwner）+ `ProofGateUpdated` 事件。
   - `_verifyZKProof`：`requireProof=false` 时跳过 0x0100，仅本地校验 `newRoot == keccak(abi.encodePacked(resultsHash, currentStateRoot))`（撮合正确性由 operator 信誉背书）；`=true` 时走原 0x0100 路径。
   - Nullifier 预编译调用门控在 `requireProof=true` 时才执行，且常量改 `0x0104`（对齐 Anubis 指南；0x0103=STEALTH）。启动注释注明主网空 returndata 实测。
   - 保留 `onlyEngine` / nullifier mapping 去重 / maker-taker 费用 / `BatchSettled` / `stateRoot` / `batchSeq` / `Unshielded`（事件记账 MVP）。
2. **`scripts/deploy-settlement.ts`**：打印 requireProof 状态；可选 `REQUIRE_PROOF=true` 环境变量。
3. **导出 ABI** → `contracts/abi/Settlement.json`（对齐 `contracts/bindings/settlement.go` 的 `SettlementMatchResult{ID, OrderId, User, Price, Amount, Nullifier, Role}` 形状；新增 `requireProof()` view）。
4. **`npx hardhat test`** 覆盖 v2 两档（requireProof=false 接受哈希、=true 拒绝假证明）。
5. **部署**（需误费 ANUB 的部署 EOA）：`npx hardhat run scripts/deploy-settlement.ts --network anubisMainnet` → `scripts/authorize-engine.ts` 授权 operator EOA → 地址回填 Go config。

## Part 2 — 后端 Go（`internal/dex/chain/anubis/` + 入口）

1. **依赖**：`go.mod` 加 `github.com/ethereum/go-ethereum`（`ethclient`、`accounts/abi/bind`、`crypto`、`types`）+ `go mod tidy && go mod vendor`。
2. **`evm.go`（新建）** — EVM 客户端薄封装：`ethclient.Dial(rpc-endpoint)`（HTTP）；operator 私钥 `chain.anubis.private-key`/`ANUBIS_PRIVATE_KEY` → `crypto.HexToECDSA` → from 地址；pending nonce + gas 估算 + EIP-1559 签名 + `eth_sendRawTransaction` + receipt 轮询（超时/重试）+ `BatchSettled` 日志解析。
3. **`settlement.go` 去 STUB**：
   - `submitToChain(batch)`：ABI-pack `results[]`（`SettlementMatchResult`）+ `zkProof`=SHA256 承诺 + `newRoot`=keccak(results) → `bind.TransactOpts` → `submitBatch` → 等回执 → 解析 `BatchSettled(batchId, stateRoot, totalVolume, feeCollected)` → 返回真实 txhash。重试沿用现有 worker 循环。
   - 新增 config：`chain.anubis.settlement-gate`（bool，默认 false）、`settlement-contract`→v2 地址。
4. **`orderpool.go`（新建，镜像 [aleo/orderpool.go](internal/dex/chain/aleo/orderpool.go)）**：`PooledOrder{OrderId, Symbol, Side, Price, Amount, Deadline, Trader, Mode(public/private), Ciphertext, Signature, NoteCommitment, Nullifier}`；nullifier 去重 + 查单；`POST /order` 校验入池 → 按 Symbol 投递撮合通道。
5. **`subscriber.go` 改造**：主订单源切 orderpool；保留 `eth_getLogs` 作审计监听（Registry OrderSubmitted 的 nullifier/commitment 防重放）；`SetViewKey` 读 `chain.anubis.view-key-private`（新增 `ANUBIS_VIEW_KEY` env bind）→ `privacy.ViewKey`；替换 `DecryptOrder` STUB 为 `privacy.DecryptOrderFromNote`；修 `contracts/bindings/settlement.go` 的 `NullifierCheck` 地址 `0x0103`→`0x0104`。
6. **`adapter.go`**：`NewAdapter` 组装 evm client → settlement；view-key 配置则 `sub.SetViewKey`。
7. **`cmd/engine/anubis/main.go` 注册 HTTP**（镜像 [aleo/main.go](cmd/engine/aleo/main.go) 71-88）：`/order`、`/order/cancel`、`/api/v1/{orders,trades,operator,balance}`、`/order/tx/{txId}`。
8. **隐私接线（Phase 2）**：`decrypt.go` 既有 `DecryptOrderFromNote/BatchDecryptOrders` 接入 intake；`nullifier.go` 在下单/入池铸造（SHA256）+ 内存防重 + 可选 Registry 审计；`zk_prover.go` 注释明确「0x0100 延后至官方参数公开」并修正 `[]byte(item.Taker)` 占位收集逻辑。
9. **测试**：单元（ABI pack 往返、orderpool 去重、ECIES 加解密往返、viewkey 过滤、settle dry-run `eth_estimateGas`）；`//go:build integration` 集成（有 gas 则真实 settle）。

## Part 3 — 前端（`web/`）

1. **钱包抽象泛化**（[web/src/lib/wallet/types.ts](web/src/lib/wallet/types.ts)）：`AleoWallet` → `ChainWallet = AleoWallet | EVMWallet`（`connect/placeOrder/getBalances/sign` 同构）。
2. **`web/src/lib/wallet/evm.ts`（新建）** — MetaMask 适配器：`window.ethereum` 惰性检测（复用 [aleo.ts](web/src/lib/wallet/aleo.ts) `resolveWallet` 竞态思路）；connect → 账号 + chainId 6714（非则弹添加网络）；placeOrder 构造订单载荷。
3. **`web/src/lib/crypto/ecies.ts`（新建）** — WebCrypto(ECDH P-256 + AES-GCM) 与 Go [EncryptedOrder](internal/dex/privacy/encryption.go) **字节一致**：临时密钥、共享密钥=SHA256(ECDH)、IV 12B、viewTag=shared[:4]、commitment=SHA256(nullifier‖ct‖ephPk)、压缩 P-256 点。
4. **`web/src/features/trading/OrderForm.tsx` 加 Anubis 分支**：链切换（Aleo/Anubis）→ standard=明文 `{mode:'public'}`；private=ECIES 加密 `{mode:'private', ciphertext, signature}` → `POST /order`。复用 `toUnits`/`pairTokens`。
5. **`web/src/app/layout/TopBar.tsx`**：链切换器 + MetaMask 连接按钮；显示 Anubis 账号。
6. **余额**：EVM `getBalances`（ANUB 原生经引擎 `/api/v1/balance/{addr}`）；结算币后续。
7. **展示**：`/api/v1/orders`、`/api/v1/trades` 由引擎新路由服务，现有组件（[OrdersPanel](web/src/features/trading/OrdersPanel.tsx)/[AssetsPage](web/src/pages/AssetsPage.tsx)）复用；行情 WS 共享。

## 配置新增（[conf/config.yaml](conf/config.yaml)）

- `chain.anubis.settlement-contract` → v2 地址；`chain.anubis.settlement-gate: false`
- `chain.anubis.view-key-private` + [common/config.go](internal/infra/common/config.go) 加 `ANUBIS_VIEW_KEY` BindEnv
- `ANUBIS_PRIVATE_KEY` 绑定已有 → operator 结算 EOA 复用

## 实施顺序

1. 合约 v2 + `hardhat test`（已移入 `contracts/anubis/`）。
2. 部署 v2 + authorizeEngine（需误费 ANUB；可与后端并行）。
3. 后端结算去 STUB（最大块：evm.go + settlement.go）。
4. 后端 /order 订单池 + 入口 HTTP。
5. 前端 EVM 适配器 + 下单表单 + 链切换。
6. 主网 E2E 验证 + 更新本方案与 [Aleo改造方案.md](Aleo改造方案.md) §7。

## 验证

- `go build ./...` + `go test ./internal/dex/chain/...`（含新单测）+ `make anubis` 产出且 `/health` 200。
- `npx hardhat test`（v2 两档）+ 主网 `eth_getCode` 校验。
- 主网 E2E（`//go:build integration` 或手动脚本）：authorize-engine → POST /order（明文+密文各一）→ 撮合 → **真实 EVM 结算** → `eth_getTransactionReceipt` 见 `BatchSettled` → `/api/v1/trades` 显示 `settle_status` → WS trade。
- 隐私链路：前端 ECIES 加密 → 引擎 viewkey 解密（ViewTag 命中）→ nullifier 去重（重复 POST 被拒）。

## 前提 / 待确认

- **需 ANUB 主网 gas 的部署者 EOA + operator（结算签名）EOA**（无公开测试网）。
- MVP 结算 `_unshield` 先事件记账，不接 ERC-20 真转账（后续视 settle 币种配置）。
- 是否用现有 Registry `0x5037...` 做审计锚定（可省一次部署）——建议用，`submitOrder` 主网实测可行。