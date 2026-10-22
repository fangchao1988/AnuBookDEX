# AnuBookDEX × Aleo：隐私 DEX 改造方案（双链同步开发）

> 版本：v0.6　|　日期：2026-09-01　|　状态：**Aleo Phase 1-3 完成**（p7 公开/隐私混合 4+4+4 + AI 策略引擎，testnet 联调通过），**Anubis Phase 1-2 未完成**（订阅解密/STUB 结算）。下一步：Anubis Phase 1-2 真实链路接通 + Aleo Phase 4（leverage.aleo）
> 关系：与面向 Anubis Chain 的 [DEX改造方案.md](DEX改造方案.md) 并行。两链**同步开发、全部 Phase 实现**（不受黑客松时间限制），共享撮合核心，链后端各自独立。
> 范围：本文聚焦 **Aleo 链** 侧改造；双链共享架构见 §2。Anubis 侧实现依据见 [DEX改造方案.md](DEX改造方案.md)。

---

## 一、背景与定位

### 1.1 为什么单独写 Aleo 方案

[DEX改造方案.md](DEX改造方案.md) 是为 **Anubis Chain（EVM 兼容）** 写的：Solidity 合约、go-ethereum、PLONK 预编译（`0x0100`/`0x0103`）、Type 100-103 隐私交易。**Aleo 与 Anubis 技术栈几乎不兼容**（见 §3），需独立的 Aleo 侧方案。

但两链的**链下撮合核心完全相同**，且 AnuBookDEX 的设计哲学（链下撮合 + 链上验证）恰好踩在 Aleo 的原生架构上。

### 1.2 核心论点

Aleo 官方架构：**offchain execution, onchain verification, records as private state**。这与 AnuBookDEX「链下 Go 撮合 + 链上结算验证」**同构**。Aleo 侧不是"硬凑"，而是把撮合核心接到 Aleo 原生隐私栈上。

> 来源：[QuickNode Aleo REST API](https://www.quicknode.com/docs/aleo)、[QuickNode: Build & Deploy Leo](https://www.quicknode.com/guides/aleo/build-and-deploy-leo-program)

### 1.3 现状基线（代码审查 + 联网验证）

| 层 | 现状 | Aleo 处置 |
|----|------|-----------|
| 撮合核心 [match/](../internal/core/match/)、[runner.go](../internal/dex/runner/runner.go)、l2quote、ws | ✅ 生产级、链无关 | 直接复用 |
| [chain/adapter.go](../internal/dex/chain/adapter.go)（OrderSource/SettlementSink/ChainAdapter 接口）| ✅ 已落地 | 双链共享契约 |
| [chain/anubis/](../internal/dex/chain/anubis/)（迁自旧 chain 包）| ✅ 已迁移、config 命名空间化 | Anubis 后端 |
| [chain/aleo/](../internal/dex/chain/aleo/)（subscriber/settlement/orderpool/extract/cancel）| ✅ 已填实 | **p7 公开/隐私混合**（`anubook_dex_p7.aleo`，4+4+4 transition）+ operator 凭证 UTXO 轮换 + 公开 mapping status 核验 + `POST /order` 链下订单通道 + `leo execute settle` 结算 |
| [encryption.go](../internal/dex/privacy/encryption.go) / [decrypt.go](../internal/dex/privacy/decrypt.go)（ECIES/AES）| ✅ 真实，但是"加密"非 ZK | Aleo 侧**丢弃**，改原生 record 隐私 |
| [zk_prover.go](../internal/dex/privacy/zk_prover.go)（gnark PLONK）| ⚠️ STUB（SHA256 哈希占位，未接入 0x0100） | Aleo 侧**丢弃**，ZK 由 snarkVM 自动生成 |
| [nullifier.go](../internal/dex/privacy/nullifier.go) | ⚠️ 仅内存 | Aleo 侧**丢弃**，record 消费即 spent |
| Leo 程序 `anubook_dex_p7.aleo` | ✅ 已落地 | `contracts/leo/src/main.leo` + `build/anubook_dex_p7/`，testnet 部署 + 联调通过 |
| Anubis `leverage.go` | ⚠️ 骨架（`LeverageAdapter` 完成本地风控，SC TODO）| Phase 4 参考对象，SC 未接通 |
| Anubis `subscriber.go` | ⚠️ `DecryptOrder` not implemented | Phase 1 真实链路待接通 |
| Anubis `settlement.go` | ⚠️ `submitToChain` STUB | Phase 1 真实链路待接通 |

---

## 二、双链架构总览（已落地）

```
                       共享撮合核心（链无关）
 ┌────────────────────────────────────────────────────────────────┐
 │  internal/core/match/  · l2quote/  · market/  · internal/dex/ws │
 │  internal/dex/runner/runner.go (StartMatcher) + engine.go (StartEngine) │
 └───────────────┬───────────────────────────────────┬───────────┘
                 │ chain.OrderSource / SettlementSink  │ (接口注入)
   ┌─────────────┴─────────────┐        ┌──────────────┴──────────────┐
   │  chain/anubis/ (EVM)       │        │  chain/aleo/ (Leo/snarkVM)  │
   │   Subscriber: eth_getLogs  │        │   OrderPool + Subscriber   │
   │   (DecryptOrder STUB)      │        │   (链下订单池驱动)           │
   │   Settlement: STUB         │        │   SettlementSink: settle_*  │
   │   LeverageAdapter: skeleton│        │   4 组合 via leo execute   │
   │   (SC TODO)                │        │   retry loop + creds rotate│
   └─────────────┬─────────────┘        └──────────────┬──────────────┘
                 ▼                                     ▼
   cmd/engine/anubis/main.go              cmd/engine/aleo/main.go
   → engine-anubis.bin                     → engine-aleo.bin
   (simulate-orders / AI not enabled)      (+ AI hub + /order APIs)
```

**关键文件**：
- 接口：[internal/dex/chain/adapter.go](../internal/dex/chain/adapter.go)（`OrderSource`/`SettlementSink`/`ChainAdapter`）
- 共享启动：[internal/dex/runner/engine.go](../internal/dex/runner/engine.go)（`StartEngine(src, sink, snap, hub, batchSize)`）
- 入口：[cmd/engine/anubis/main.go](../cmd/engine/anubis/main.go)、[cmd/engine/aleo/main.go](../cmd/engine/aleo/main.go)
- 构建：`make anubis` / `make aleo`（`make dex` 为 anubis 别名）
- 配置：`chain.anubis.*` / `chain.aleo.*` 分段

**设计原则**：
1. 撮合核心零改动；`go build ./...` 与撮合测试在双链下均通过。
2. 隐私委托给 Aleo 原生 record（不自造 ECIES/gnark/nullifier）。
3. 链下撮合 + 链上 Leo 结算；`finalize` 写公开承诺供审计/行情。
4. 结算层强制不变量：价格不越限、token 守恒、record 正确消费。

### 信任模型（Aleo MVP：operator-custody）

Aleo 约束：消费 record 须 `self.signer == record.owner`。链下引擎无法直接花"用户拥有的"订单 record。MVP 采用**操作员托管**：
- 用户 `place_order` 把资金锁进 operator 拥有的 Order record。
- operator（引擎）持 record 明文，构造 `settle` transition 消费之。
- `settle` 强制资金按有效价格流向正确对手方 → **operator 无法盗取，只能排序/审查**，由声誉/bonding 兜底（后续阶段）。
- 用户可 `cancel_order` 取回。

> 去信任升级路径见 §9（订单归用户 + 双方签名授权，借 v4.3.0 原生 Keccak+ECDSA 验签）。

---

## 三、Aleo vs Anubis：关键差异

| 维度 | Anubis（[DEX改造方案.md](DEX改造方案.md)）| Aleo（本方案）|
|------|----------------|---------------|
| 虚拟机 | EVM（Modified Geth）| snarkVM（Varuna zkSNARK） |
| 合约语言 | Solidity 0.8.x | **Leo 4.x** |
| 隐私模型 | 双状态 + Type 100-103 + 预编译 | **record 默认私有** + finalize 公开状态 |
| ZK | gnark 手写 PLONK + `0x0100` 预编译（⚠️ 当前为 SHA256 哈希占位，STUB） | **snarkVM 自动生成** |
| Nullifier | `0x0103` 预编译 / 自造 SHA256 | record 消费即 spent（协议原生） |
| 公开状态 | MPT / storage | `mapping`（finalize `get`/`contains`） |
| 链交互 | go-ethereum / EVM JSON-RPC | **snarkOS REST**（无 Go SDK，手写客户端） |
| 账户/密钥 | ECDSA P256 / EIP-5564 | Aleo account（`aleo1...`）/ record view_key |
| 共识 | PoS + IBFT 2.0 | AleoBFT（DAG-BFT PoS） |
| Gas | gasDAI | credits（microcredits） |

---

## 四、目标架构（Aleo 侧，当前 p7）

```
                       Aleo Layer 1（snarkOS / AleoBFT）
 ┌────────────────────────────────────────────────────────────────┐
 │  program anubook_dex_p7.aleo                                      │
 │   ├─ record Order { owner, trader, order_id, side, price, ... }  私有订单(隐私下单) │
 │   ├─ struct PublicOrder { user, operator, side, price, amount, status } 公开订单 │
 │   ├─ mapping public_orders: field => PublicOrder  公开订单簿         │
 │   ├─ fn place_order_buy_public/sell_public -> Final  标准下单（mapping） │
 │   ├─ fn place_order_buy_private/sell_private -> (Order, ...)  隐私下单（record） │
 │   ├─ fn settle_pp/vp/pv/vv -> (...)   4 组合撮合结算                │
 │   ├─ fn cancel_buy_public/sell_public/buy_private/sell_private  4 变体撤单 │
 │   └─ finalize settle_* { assert + set public_orders.status=1 }  链上可审计、隐私订单链上不可见 │
 │   snarkVM 对每个 transition 自动生成 ZK 证明，网络验证               │
 └───────────────┬────────────────────────────────────┬───────────┘
                 │ REST（snarkOS /v2 或 QuickNode）   │
                 ▼                                    ▼
 ┌────────────────────────────────────────────────────────────────┐
 │  Off-Chain Go Engine（AnuBookDEX，复用）                        │
 │  chain/aleo/：RESTClient + OrderPool + Subscriber + SettlementSink │
 │   -> match/（复用）-> l2quote/ws（复用） + ai/（复用）         │
 │  POST /order  /order/privacy  /order/cancel  /api/v1/{orders,trades,operator,balance} │
 └────────────────────────────────────────────────────────────────┘
```

---

## 五、Leo 程序设计（`anubook_dex_p7.aleo`）

> ⚠️ **本节为当前已落地的 p7 架构**（公开/隐私混合模式）。`contracts/leo/src/main.leo` 是 p7 程序源码；编译产物在 `contracts/leo/build/anubook_dex_p7/`。Phase 2 的 record 私有模型在 p7 中通过 `place_order_buy_private/sell_private` 实现；公开下单通过 `public_orders` mapping。设计文档见 [Aleo公开隐私混合下单方案.md](Aleo公开隐私混合下单方案.md)（初稿命名 p6，实施时因占名落为 p7）。后续 Phase 4 杠杆交易将新增 `leverage.aleo`。

### 5.1 record 与 mapping（p7 公开/隐私混合）

当前生产程序 `anubook_dex_p7.aleo` 实现标准下单（公开余额 `public_orders mapping`）与隐私下单（`record Order`）的混合模式：

```leo
program anubook_dex_p7.aleo {

    // 隐私订单 record（owner=operator，trader=下单用户）
    record Order {
        owner: address,        // operator（record 归属，operator 签名 spend 做 settle/cancel）
        trader: address,       // 下单用户（收益/退回接收人）
        order_id: u128,
        side: u8,              // 0=buy, 1=sell
        price: u64,
        amount: u64,
        deadline: u32,
    }

    // 公开订单（存 mapping，不铸 record）
    struct PublicOrder {
        user: address,
        operator: address,
        side: u8,
        price: u64,
        amount: u64,
        deadline: u32,
        status: u8,           // 0=active, 1=settled, 2=canceled
    }

    mapping public_orders: field => PublicOrder;
```

### 5.2 下单（p7 公开/隐私混合）

**标准下单（公开余额）**：`place_order_buy_public` / `place_order_sell_public` — 两阶段：用户先直调 `transfer_public` 把资金托管给 operator，`place_order_*_public` 只记 mapping（await 外部 transfer_public 在 testnet 不可行——finalize 输入错位，实测 rejected）；`needed = price*amount/1e6` 由调用者保证与转账一致。settle 时 operator 前置转账垫付（见 [settlement.go](../internal/dex/chain/aleo/settlement.go) 的 settle_pp 前置 credits/USDCX 转账）。

**隐私下单（record 锁仓）**：`place_order_buy_private` / `place_order_sell_private` — 用户 USDCX/credits Token + Credentials → operator 托管（`transfer_private_with_creds`），铸 `Order` record。

```leo
    fn place_order_buy_private(
        fund: test_usdcx_stablecoin.aleo::Token,
        creds: test_usdcx_stablecoin.aleo::Credentials,
        order_id: u128, price: u64, amount: u64, deadline: u32, operator: address,
    ) -> (Order, ComplianceRecord, Token, Token, Credentials, Final)
```

### 5.3 撮合结算 `settle`（4 组合，核心不变量）

`settle_pp`（双公开） / `settle_vp`（买隐私×卖公开） / `settle_pv`（买公开×卖隐私） / `settle_vv`（双隐私）：

```leo
    fn settle_vp(
        maker: Order, op_usdcx: Token, op_creds: Credentials,
        taker_order_id: u128, taker_owner: address,
        match_price: u64, match_amount: u64,
    ) -> (...) {
        assert_eq(maker.side, 0u8);                    // 不变量 1：maker=buy, taker=sell
        assert_eq(match_price <= maker.price, true);   // 不变量 2：价在双方限价内
        assert_eq(match_amount <= maker.amount, true);  // 不变量 3：量不超剩余
        let f = transfer_private_with_creds(taker_owner,
            ((match_price * match_amount) / 1000000u64) as u128, op_usdcx, op_creds);
        return (..., final {
            let taker: PublicOrder = public_orders.get(taker_order_id as field);
            assert_eq(taker.user, taker_owner);       // 防 operator 篡改收款人
            assert_eq(taker.status, 0u8);             // 防 double-spend
            assert_eq(match_price >= taker.price, true);
            assert_eq(match_amount <= taker.amount, true);
            f.4.run();                                // 链上 transfer_private_with_creds
            let t: PublicOrder = taker;
            t.status = 1u8;                           // 标记已结算
            public_orders.set(taker_order_id as field, t);
        });
    }
```

### 5.4 撤单（4 变体）

`cancel_buy_public` / `cancel_sell_public`（公开 mapping status=2） / `cancel_buy_private` / `cancel_sell_private`（record 退回）。

```leo
    fn cancel_buy_private(order: Order, op_usdcx: Token, op_creds: Credentials) -> (...) {
        assert_eq(order.side, 0u8);
        let needed: u128 = ((order.price * order.amount) / 1000000u64) as u128;
        let f = transfer_private_with_creds(order.trader, needed, op_usdcx, op_creds);
        return (..., final { f.4.run(); });
    }
}
```

### 5.5 链上可见性（隐私来源）

**隐私订单（record 模型）**：链上仅见 commitment + ciphertext，订单价格/数量/双方地址均不可见（仅 operator 用 view key 解密）。record 消费即 spent（协议原生防双花）。

| 链上可见 | 链上不可见（record 私有）|
|---------|------------------------|
| `public_orders[order_id].status`（公开订单状态）| 隐私订单 record 明文（价格/数量/side）|
| `public_orders[order_id].user/operator`（公开订单收款人）| Order record ciphertext + owner/trader 字段 |
| snarkVM proof（自动验证 transition 合法性）| 订单间的匹配关系 |

---

## 六、Go 侧 Aleo 适配器（Phase 1-3 完成 → 当前处于 p7 公开/隐私混合模式）

接口与共享启动见 §2。Aleo 后端 [internal/dex/chain/aleo/](../internal/dex/chain/aleo/) 已填实，当前运行在 `anubook_dex_p7.aleo`（公开/隐私混合 4+4+4 transition），testnet 联调通过：

```go
// internal/dex/chain/aleo/orderpool.go：链下订单池 + POST /order API（明文 + tx_id 两种模式）
//   PooledOrder 含 Order 明文 + Order record ciphertext + operator 托管资产 + 凭证
//   POST /order：tx_id 模式（链上提取 + view key 解密）| 明文字段模式（p2 铸币兼容）
//   subscriber.go：从订单池消费 PooledOrder，按 Symbol 路由到撮合通道
//
// internal/dex/chain/aleo/settlement.go：结算路由 p7 公开/隐私混合 4 组合
//   settle_pp（双公开）/ settle_vp（买隐私×卖公开）/ settle_pv（买公开×卖隐私）/ settle_vv（双隐私）
//   每笔成交 shell out `leo execute settle_* <maker_ct> <taker_ct> <price>u64 <amount>u64 --broadcast`
//   （leo CLI 负责 snarkVM 证明+广播；operator view key 自动解密 record）
//   operator 凭证 UTXO 轮换（settle 消费旧凭证，leo 输出捕获新凭证 → .operator_credentials.state）
//   公开 mapping status 核验（unquoteJSON + parseRecordPlaintext，防 already-exists 误判）
//
// internal/dex/chain/aleo/extract.go：ExtractAndDecryptOrder 从链上交易提取订单载荷
//   定位 dex transition → 提取 Order record ciphertext → view key 解密
//   跨程序 inner transition 提取托管资产（transfer_private_with_creds 输出）
//
// internal/dex/chain/aleo/cancel.go：POST /order/cancel，4 撤单变体路由
//   cancel_buy_public/sell_public/cancel_buy_private/cancel_sell_private
```

> 实测要点：REST 路径为 `/v1/testnet/...`（非 testnet3）；u128 key 需带类型后缀（`1u128`）；p7 settle/settle_pp 等 final 块的 status 字段通过 `GET /v1/testnet/program/anubook_dex_p7.aleo/mapping/public_orders/{id}field` 验证；`leo execute` 单次耗时数十秒，10s 重结算循环兜底；`--no-local` 保证链上版本程序生成证明。ETH_USDT 经 `chain.aleo.program-id-by-symbol` 路由到 p2 铸币模式（[extract.go](../internal/dex/chain/aleo/extract.go) 按交易对选 program）。

---

## 七、Phase 1-8 完整路线（双链同步）

> 不受时间限制，分批落地。每 Phase 双链各交付；Anubis 侧依据 [DEX改造方案.md](DEX改造方案.md)，Aleo 侧依据本文。

| Phase | 主题 | Anubis 侧 | Aleo 侧 |
|------|------|-----------|---------|
| **1** | 基础迁移 | ⚠️ 部分完成：`eth_getLogs` 轮询订阅实现，但 `DecryptOrder` 仍未对接 Anubis View Key SDK（not implemented）；`Settlement.submitToChain` 为 STUB（ZK proof 生成 OK，链上提交未接通） | ✅ 完成：`anubook_dex_p7.aleo`（公开/隐私混合 4+4+4 transition）部署 testnet；snarkOS REST 订阅/广播 + `POST /order` 链下订单通道 + `leo execute settle` 结算全联调通过 |
| **2** | 隐私层 | ⚠️ 部分完成：ECIES/AES/gnark 代码存在但 gnark ZK prover 仅为 SHA256 哈希占位（MVP），生产替换为 0x0100 预编译 TODO 待实现；`DecryptOrder` 未联调 | ✅ 完成：原生 record 隐私 + `finalize` 公开承诺（snarkVM 自动生成 proof）；隐私铁证（链上全为 commitment+ciphertext）；operator view key 自动解密 |
| **3** | AI 策略引擎 | ✅ 完成（引擎实现，入口未启用）：`ai/hub.go`（Engine 行情研判 + Iceberg 冰山拆分 + 深度订阅 + Tick 定时器）+ `ai/apis.go`（`/ai/signal` `/ai/indicators` `/ai/sentiment` `/ai/iceberg`）+ `ai/risk.go`（风控引擎）；`market.go` 新增 `GetDepthChannel` 供 AI 订阅盘口；联调通过：深度+舆情→信号 BUY；50 ETH 大单经 VWAP 拆分 → 子单按 30s 间隔进订单池 → 撮合（当前为规则引擎 MVP，ML 插拔点预留，见 `ai/todo.md`）| ✅ 完成：同 Anubis（链无关，双链共享）；Aleo 入口 (`cmd/engine/aleo/main.go`) 已挂 `/ai/*` API |
| **4** | 杠杆交易 | ⚠️ 骨架：`LeverageAdapter` 接入 `ai.RiskEngine`（仓位/强平/保证金/资金费率本地逻辑完成），但所有链上 SC 调用为 TODO（LeverageManager SC 未接通）；ABI 已就绪 (`contracts/abi/LeverageManager.json`) | ❌ 未开始：`leverage.aleo` 不存在，无保证金 record + 强平 transition |
| **5** | 暗池 MPC | ❌ 未开始：`DarkPoolRouter.json` ABI 就绪，但无 SPDZ/Shamir 实现、无 Go 集成 | ❌ 未开始：无 Leo 暗池 program |
| **6** | ZK-KYC | ❌ 未开始：`ZKKYC.json` ABI 就绪，无 Go 集成 | ❌ 未开始：无 Leo zk-identity program |
| **7** | 跨协议互通 | ❌ 未开始：`LiquidityRouter.json`/`LPMiningRewards.json` ABI 就绪，无 AMM 集成 | ❌ 未开始：无 Aleo 生态 AMM 路由 |
| **8** | NFT 权益 + 生产加固 | ⚠️ 部分：NFT discount ABI 就绪，Prometheus 未接入 | ⚠️ 部分：生产部署已落地（yilecai.top：CI GHCR 镜像 + 宝塔反代 + `git tag` 发布流程 + Shield 注入，见 [运维文档.md](运维文档.md)）；Aleo 验证节点未部署 |

### 状态说明

**Anubis 侧（EVM/Solidity）**：
- Phase 1-2 仅达到脚手架 + 本地逻辑，真实链路未接通（订阅解密、结算提交均为 TODO/STUB）。
- Phase 3 AI 引擎完整实现，但 Anubis 引擎入口 (`cmd/engine/anubis/main.go`) 未启用 AI hub（仅 Aleo 入口启用）。
- Phase 4 `LeverageAdapter` 完成本地风控逻辑（`ai/risk.go`）但链上交互为 TODO。
- Phase 5-8 ABI 已就绪在 `contracts/abi/`，Go 集成未开始。

**Aleo 侧（Leo/snarkVM）**：
- Phase 1-3 完全完成并 testnet 联调通过，当前运行在 `anubook_dex_p7.aleo`（公开/隐私混合模式，4 下单 + 4 settle + 4 撤单）。
- 程序迭代史：p2（record 私有模型，08-07）→ p3/p4（过渡版）→ p5（凭证托管 USDCX，08-15）→ p7（公开/隐私混合 4+4+4，08-19）；`contracts/leo/build/` 保留 p2~p7 各版本编译产物。Phase 4-8 未正式开始：`leverage.aleo`、暗池、zk-identity、AMM 路由等均未实现。
- 生产特性已落地：operator 凭证 UTXO 轮换（`credentialsStatePath`），幻影凭证防护，公开 mapping JSON wrapper 解析修复，订单池 + 委托列表 + 成交记录 API；生产部署（yilecai.top：CI GHCR + 宝塔反代 + tag 发布 + Shield 注入，见 [运维文档.md](运维文档.md)）。
- AI 引擎为规则引擎 MVP（`ai/todo.md`：sentiment 内存 map、冰山 Adaptive 固定 5%、风控固定阈值，均预留 ML 插拔点）。

**下一步**：Anubis 侧 Phase 1-2 真实链路接通（`DecryptOrder` + `submitToChain`）+ Aleo Phase 4（Leo `leverage.aleo`）。

---

## 八、开发环境与关键命令

```bash
# Rust + Leo（Leo 需 Rust 1.94.1+）
cargo install aleo-executor
leo --version                 # 期望 4.x

# 本地快速迭代：leo devnode（绕过共识与证明生成，秒级反馈，默认 http://localhost:3030）
leo devnode start --private-key <PRIVATE_KEY>
leo build && leo test && leo execute settle_* <inputs...> --broadcast --no-local

# testnet 部署（经 QuickNode 托管端点；水龙头领测试积分）
leo deploy --broadcast --endpoint https://api.quicknode.com/aleo/<KEY>/v1
curl https://api.quicknode.com/aleo/<KEY>/v1/testnet/transaction/<at...txid>   # 实测路径为 /testnet/（非 testnet3）

# Go 双链构建
make anubis    # -> engine-anubis.bin
make aleo      # -> engine-aleo.bin
make test      # 含 ./internal/dex/chain/{anubis,aleo}/
```

> 来源：[leo devnode 文档](https://docs.leo-lang.org/cli/cli_devnode)、[QuickNode Build & Deploy](https://www.quicknode.com/guides/aleo/build-and-deploy-leo-program)（2026-05 更新）

---

## 九、验证与升级路径

### 9.1 验证
- `go build ./...` 全通过；`make anubis`/`make aleo` 产出二进制且 `/health` 200。
- 撮合核心 `go test ./internal/core/match/` 不受影响；`chain/anubis`、`chain/aleo` 测试通过。
- **Aleo testnet 实测（2026-08-16 p4/p5 单隐私 E2E → 08-19+ p7 公开/隐私混合四组合）**：
  - E2E 独立用户钱包买卖全通（凭证 owner 坑已解：settle 用 operator 自有凭证）；领凭证通用常量证明；leo 4.4 `--yes` 坑；uid 精确定位 record。
  - p7 公开/隐私混合下单：`place_order_buy_public/sell_public`（mapping 记账）+ `place_order_buy_private/sell_private`（record 铸造）；真实 testnet 隐私买单交易用例见 [extract_test.go](../internal/dex/chain/aleo/extract_test.go)（`at1tlpvpu...`）。
  - 4 种结算路径均链上确认：`settle_pp`/`settle_vp`/`settle_pv`/`settle_vv`；each via `leo execute` → snarkVM 证明 → testnet 确认。
  - 4 种撤单变体均链上确认：`cancel_buy_public/sell_public/cancel_buy_private/cancel_sell_private`。
  - **隐私铁证**：`leo query transaction` 确认隐私订单下 inputs/outputs 全为 commitment+ciphertext，成交价/数量也是密文。
  - **防双花**：operator 凭证 UTXO 消费后自动轮换（`.operator_credentials.state`），幻影凭证陷阱已规避；failed broadcast 的输出不可用。
  - 公开 mapping status 核验（unquoteJSON + parseRecordPlaintext）。
  - ⚠️ 已知边界：final 块 status 更新可能滞后——[manual_settle_test.go](../internal/dex/chain/aleo/manual_settle_test.go) 记录过 settle_vp 广播成功但 `public_orders[..].status` 仍为 0 的案例（重结算循环兜底重查）。
- `leo test` 仅能校验编译与签名（on-chain `final` 逻辑无法在 proof context 执行，需 testnet 实测）。
- **Anubis 侧**：subscriber `DecryptOrder` 为 not-implemented，settlement `submitToChain` 为 STUB — 未联调真实链。

### 9.2 升级路径
| 方向 | 现状 | 升级 | 价值 |
|------|------|------|------|
| 信任模型（Aleo） | operator 托管 | 订单归用户 + 双方签名授权（v4.3.0 Keccak+ECDSA）| 去 operator 盗用/抢跑风险 |
| 信任模型（Anubis） | DecryptOrder 未联调 | 接入 Anubis View Key SDK 解密 Note | 订阅真实链上订单 |
| 撮合正确性证明 | 仅结算层守恒 | Leo 电路证明单笔撮合满足价格-时间优先 | 把"operator 可排序"再退一步 |
| Anubis ZK 证明 | SHA256 哈希占位（MVP） | gnark PLONK → 0x0100 预编译验证 | 真正的撮合正确性证明 |
| 公开行情 | `public_orders` mapping | 解密行情聚合（脱敏 OHLCV）| 兼顾隐私与可观测 |
| 多链 | Anubis + Aleo | `ChainAdapter` 已可插拔，后续可扩展第三链 | 真正多链 |
| Phase 4 杠杆 | Anubis LeverageAdapter 骨架 / Aleo 未开始 | Leo `leverage.aleo` + Anubis LeverageManager SC 接通 | 杠杆交易 |
| Phase 5-8 | 全部未开始 | 暗池 MPC / ZK-KYC / 跨协议 AMM / NFT 权益 | 功能扩展 |

> v4.3.0（2025-10）原生 Keccak+ECDSA 验签、max array size 提升；v4.2.0 基础费降 90% + 优先费市场。来源：[Provable v4.3.0](https://provable.com/blog/announcing-aleo-stack-v4-3-0)、[v4.2.0](https://provable.com/blog/announcing-aleo-stack-v4.2.0)

---

## 十、风险与待办

| 风险 | 影响 | 缓解 |
|------|------|------|
| **无官方 Go SDK** | 链交互靠手写 REST | 已验证可行；复用 `rpcCall` 风格自写薄客户端 |
| **record 消费需 owner 签名** | 引擎无法直接花用户订单 | MVP 用 operator-custody（§2）；升级走签名授权 |
| **单 transition record 数上限** | 批量结算受限 | v4.3.0 已提升 array 上限；MVP 每批 1-2 笔撮合可演示 |
| **Leo 语法迭代** | 4.x 仍在演进 | 已实测应对：devnode 秒级迭代 + testnet 实测 final 逻辑（`leo test` 无法执行 on-chain final）；`--no-local` 强制链上程序证明 |
| **撮合正确性无法全链上证明** | operator 可排序 | 叙事讲清"结算可审计"，加分项补小电路 |

---

## 十一、参考链接

| 资源 | 地址 |
|------|------|
| QuickNode Aleo REST API | https://www.quicknode.com/docs/aleo |
| QuickNode: Build & Deploy Leo | https://www.quicknode.com/guides/aleo/build-and-deploy-leo-program |
| Leo CLI 文档 | https://docs.leo-lang.org/cli/cli_overview |
| leo devnode | https://docs.leo-lang.org/cli/cli_devnode |
| Aleo RPC API（JSON-RPC 2.0）| https://docs.leo.app/aleo-rpc-api |
| Aleo Standard/Finalize Operations | https://docs.aleo.org/build/aleo-instructions/reference/standard-operations/index.html |
| Provable: Aleo Stack v4.3.0 / v4.2.0 | https://provable.com/blog/announcing-aleo-stack-v4-3-0 |
| Aleo 开发者门户 | https://aleo.org/developers/ |
| snarkOS（Rust 节点）| https://docs.rs/crate/snarkos/4.7.3 |
