# AnuBookDEX × Aleo：隐私 DEX 改造方案（双链同步开发）

> 版本：v0.5　|　日期：2026-08-04　|　状态：**Phase 1-3 已完成**（管道 + record 隐私 + AI 策略引擎，testnet 全链验证通过），进入 Phase 4
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
| [chain/aleo/](../internal/dex/chain/aleo/)（subscriber/settlement 骨架）| ✅ 骨架可编译可启动 | **Phase 1 填实**：snarkOS REST |
| [encryption.go](../internal/dex/privacy/encryption.go) / [decrypt.go](../internal/dex/privacy/decrypt.go)（ECIES/AES）| ✅ 真实，但是"加密"非 ZK | Aleo 侧**丢弃**，改原生 record 隐私 |
| [zk_prover.go](../internal/dex/privacy/zk_prover.go)（gnark PLONK）| ❌ STUB | Aleo 侧**丢弃**，ZK 由 snarkVM 自动生成 |
| [nullifier.go](../internal/dex/privacy/nullifier.go) | ⚠️ 仅内存 | Aleo 侧**丢弃**，record 消费即 spent |
| Leo 程序 `dex.aleo` | 📄 设计骨架（§5）| **Phase 1 实现 + `leo build` 验证** |

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
   │   Subscriber: eth_getLogs  │        │   Subscriber: snarkOS REST  │
   │   Settlement: Settlement SC│        │   Settlement: settle transition│
   │   LeverageAdapter          │        │   (骨架，Phase 1 填实)       │
   └─────────────┬─────────────┘        └──────────────┬──────────────┘
                 ▼                                     ▼
   cmd/engine/anubis/main.go              cmd/engine/aleo/main.go
   → engine-anubis.bin                     → engine-aleo.bin
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
| ZK | gnark 手写 PLONK + `0x0100` 预编译 | **snarkVM 自动生成** |
| Nullifier | `0x0103` 预编译 / 自造 SHA256 | record 消费即 spent（协议原生） |
| 公开状态 | MPT / storage | `mapping`（finalize `get`/`contains`） |
| 链交互 | go-ethereum / EVM JSON-RPC | **snarkOS REST**（无 Go SDK，手写客户端） |
| 账户/密钥 | ECDSA P256 / EIP-5564 | Aleo account（`aleo1...`）/ record view_key |
| 共识 | PoS + IBFT 2.0 | AleoBFT（DAG-BFT PoS） |
| Gas | gasDAI | credits（microcredits） |

---

## 四、目标架构（Aleo 侧）

```
                       Aleo Layer 1（snarkOS / AleoBFT）
 ┌────────────────────────────────────────────────────────────────┐
 │  program anubook_dex.aleo                                       │
 │   ├─ record Token   { owner, amount, token_id }  私有资产        │
 │   ├─ record Order   { owner, side, price, amount, ... } 私有订单 │
 │   ├─ mapping settled_commit: u64 => field   公开审计承诺          │
 │   ├─ fn place_order(...)  -> Order       锁仓铸单                │
 │   ├─ fn settle(maker, taker, price, amt) -> (Token,...)  撮合结算│
 │   ├─ fn cancel_order(order) -> Token     撤单返还                │
 │   └─ finalize settle { 写公开承诺 }      链上可审计、不泄露细节  │
 │   snarkVM 对每个 transition 自动生成 ZK 证明，网络验证          │
 └───────────────┬────────────────────────────────────┬───────────┘
                 │ REST（snarkOS /v2 或 QuickNode）   │
                 ▼                                    ▼
 ┌────────────────────────────────────────────────────────────────┐
 │  Off-Chain Go Engine（AnuBookDEX，复用）                        │
 │  chain/aleo/：RESTClient + OrderSource + SettlementSink          │
 │  -> match/（复用）-> l2quote/ws（复用）                          │
 └────────────────────────────────────────────────────────────────┘
```

---

## 五、Leo 程序设计（`dex.aleo`）

> ⚠️ **本节为 Phase 2 目标架构（record 私有模型）**。Phase 1 实际实现的 [contracts/leo/src/main.leo](../contracts/leo/src/main.leo) 是**公开 mapping 管道模型**：`struct Order`（字段 `trader`，因 `owner` 是保留字）+ `mapping orders: u128 => Order` + `return final { ... }` 内联（Leo 4.3/4.4 语法，`Final` 返回类型 + `get_or_use`/`set` 方法调用）。Phase 2 迁到下方 record 私有模型。

### 5.1 record 与 mapping

```leo
program anubook_dex.aleo {

    record Token {
        owner: address,
        amount: u64,
        token_id: u32,   // 1=ETH, 2=USDT ...
    }

    record Order {
        owner: address,         // MVP：operator 地址（托管）
        order_id: u128,
        side: u8,               // 0=buy, 1=sell
        price: u64,
        amount: u64,
        base_token: u32,
        quote_token: u32,
        deadline: u32,
    }

    mapping settled_commit: u64 => field;
```

### 5.2 下单 `place_order`

```leo
    fn place_order(
        fund: Token, side: u8, price: u64, amount: u64,
        base_token: u32, quote_token: u32, deadline: u32, operator: address,
    ) -> Order {
        assert_eq(fund.token_id == if side == 0u8 { quote_token } else { base_token }, true);
        return Order { owner: operator, order_id: 0u128, side, price, amount, base_token, quote_token, deadline };
    }
```

### 5.3 撮合结算 `settle`（核心不变量）

```leo
    fn settle(maker: Order, taker: Order, match_price: u64, match_amount: u64)
        -> (Token, Token, Token, Token) {
        // 不变量 1：交易对一致、方向相反
        assert_eq(maker.base_token == taker.base_token, true);
        assert_eq(maker.quote_token == taker.quote_token, true);
        assert_eq(maker.side != taker.side, true);
        // 不变量 2：成交价在双方限价内
        let buyer_price = if maker.side == 0u8 { maker.price } else { taker.price };
        let seller_price = if maker.side == 1u8 { maker.price } else { taker.price };
        assert_eq(match_price <= buyer_price, true);
        assert_eq(match_price >= seller_price, true);
        // 不变量 3：成交量不超双方剩余量
        assert_eq(match_amount <= maker.amount, true);
        assert_eq(match_amount <= taker.amount, true);
        // 不变量 4：token 守恒
        let quote_out = match_price * match_amount;
        let buyer = if maker.side == 0u8 { maker.owner } else { taker.owner };
        let seller = if maker.side == 1u8 { maker.owner } else { taker.owner };
        return (
            Token { owner: buyer,  amount: match_amount, token_id: maker.base_token },
            Token { owner: buyer,  amount: 0u64,          token_id: maker.quote_token },
            Token { owner: seller, amount: quote_out,    token_id: maker.quote_token },
            Token { owner: seller, amount: 0u64,          token_id: maker.base_token },
        );
    }

    finalize settle(maker: Order, taker: Order, match_price: u64, match_amount: u64) {
        let h = hash_to_field([maker.order_id, taker.order_id, match_price, match_amount]);
        set settled_commit[block.height] into h;
    }
```

### 5.4 撤单 `cancel_order`

```leo
    fn cancel_order(order: Order) -> Token {
        assert_eq(block.height <= order.deadline, true);
        return Token {
            owner: order.owner, amount: order.amount,
            token_id: if order.side == 0u8 { order.quote_token } else { order.base_token },
        };
    }
}
```

### 5.5 链上可见性（隐私来源）

| 链上可见 | 链上不可见（record 私有）|
|---------|------------------------|
| `settled_commit` 哈希承诺 | 订单价格、数量、双方地址 |
| record nullifier（spent 标记）| record 明文（仅 owner 用 view_key 可解） |

---

## 六、Go 侧 Aleo 适配器（Phase 1 已填实，testnet 联调通过）

接口与共享启动见 §2。Aleo 后端 [internal/dex/chain/aleo/](../internal/dex/chain/aleo/) Phase 1 已填实：

```go
// internal/dex/chain/aleo/client.go：snarkOS REST 客户端 + Order plaintext 解析
//   GetProgramMapping / BroadcastTransaction / GetLatestHeight / GetTransaction
//   ParseOrder：解析 Leo struct 字面量 -> match.Order（含 active -> State 映射）
//   注意：snarkOS 返回 JSON 字符串，字段间为字面 \n，正则须排除反斜杠

// internal/dex/chain/aleo/subscriber.go：轮询 orders mapping（key 带 u128 后缀），
//   跳过已结算/撤单订单（active=false -> Canceled），送入撮合
//   实测：key 不存在返回 "null"（200，带引号），视为未下单不推进

// internal/dex/chain/aleo/settlement.go：对每个 maker 成交 shell out
//   leo execute settle <maker>u128 <taker>u128 <price>u64 <amount>u64
//   --broadcast --network testnet --yes   （leo CLI 负责 snarkVM 证明+广播）
```

> 实测要点：REST 路径为 `/v1/testnet/...`（非 testnet3）；u128 key 需带类型后缀（`1u128`）；settle 链上不变量经 `GET /v1/testnet/program/anubook_dex.aleo/mapping/orders/{id}u128` 验证。

---

## 七、Phase 1-8 完整路线（双链同步）

> 不受时间限制，分批落地。每 Phase 双链各交付；Anubis 侧依据 [DEX改造方案.md](DEX改造方案.md)，Aleo 侧依据本文。

| Phase | 主题 | Anubis 侧 | Aleo 侧 |
|------|------|-----------|---------|
| **1** | 基础迁移 | eth_getLogs 订阅 + Settlement SC 接通（去 stub）| `dex.aleo` 编译 + snarkOS REST 订阅/广播接通（去 stub）|
| **2** | 隐私层 | ECIES 解密 + gnark PLONK 证明 + `0x0100`/`0x0103` 预编译 | 原生 record 隐私 + `finalize` 公开承诺（无需自造 ZK/nullifier）|
| **3** | AI 策略引擎 | [ai/](../internal/dex/ai/) 行情研判 + 冰山拆分 + 风控 | 同（链无关，双链共享）|
| **4** | 杠杆交易 | [LeverageAdapter](../internal/dex/chain/anubis/leverage.go) -> LeverageManager SC | Leo `leverage.aleo`（保证金 record + 强平 transition）|
| **5** | 暗池 MPC | SPDZ/Shamir 秘密分享 + DarkPoolRouter SC | Leo 暗池 transition（MPC 在链下，结算上链）|
| **6** | ZK-KYC | ZKKYC SC（ICAO/MiCA/FATF 分级）| Leo zk-identity（v4.3.0 Keccak+ECDSA 验签复用以太坊身份）|
| **7** | 跨协议互通 | LiquidityRouter -> RocketSwap AMM | Aleo 生态 AMM 路由 + LP 挖矿 |
| **8** | NFT 权益 + 生产加固 | NFT 手续费折扣 + Prometheus + RPC 重连 | 同（链无关运维）+ Aleo 验证节点 |

**已落地**：
- 双链脚手架：接口、包结构、两入口、`StartEngine`、makefile、config 分段。
- **Aleo Phase 1 完成**：`anubook_dex.aleo`（公开 mapping 管道）部署 testnet + Go 引擎联调（轮询 → 撮合 → `leo execute settle` → 链上扣减确认）。
- **Aleo Phase 2 完成**：
  - 链上：`anubook_dex_p2.aleo`（record 私有模型）部署 testnet（`at1suz8...`）；mint/place_order/settle/cancel_order 全链确认；链上只见 commitment+ciphertext（隐私铁证）；settled_commit 公开承诺、deadline 防过期撤单、record 防双花均链上强制。
  - Go：**链下订单通道 + 密文结算**（[orderpool.go](../internal/dex/chain/aleo/orderpool.go) 订单池 + `POST /order` API；subscriber 订单池驱动；settlement 用 Order record 调 `leo execute settle`，operator view key 自动解密，Go 零 record 解密；config `chain.aleo.program-id`→p2）。
  - E2E：POST /order(明文+ciphertext) → 撮合 → settle → settled_commit 链上确认（`18455790u64`）。
- **Aleo Phase 3 完成**（AI 策略引擎，链无关双链共享）：[ai/hub.go](../internal/dex/ai/hub.go)（Engine 行情研判 + Iceberg 冰山拆分 + 深度订阅 + Tick 定时器）+ [ai/apis.go](../internal/dex/ai/apis.go)（`/ai/signal` `/ai/indicators` `/ai/sentiment` `/ai/iceberg`）；[market.go](../internal/core/market/market.go) 新增 `GetDepthChannel` 供 AI 订阅盘口。联调通过：深度+舆情→信号 BUY；大单 50 ETH 经 VWAP 拆分 → 子单按 30s 间隔进订单池 → 撮合。（AI 冰山子单无链上 record，ciphertext 为占位，结算需 `require-ciphertext=false` 链下撮合或走 place_order——MVP 边界）
**下一步**：Aleo Phase 4（杠杆交易 Leo `leverage.aleo`）+ Anubis 侧 Phase 1 真实链路接通。

---

## 八、开发环境与关键命令

```bash
# Rust + Leo（Leo 需 Rust 1.94.1+）
cargo install aleo-executor
leo --version                 # 期望 4.x

# 本地快速迭代：leo devnode（绕过共识与证明生成，秒级反馈，默认 http://localhost:3030）
leo devnode start --private-key <PRIVATE_KEY>
leo build && leo test && leo execute settle <inputs...> --broadcast

# testnet 部署（经 QuickNode 托管端点；水龙头领测试积分）
leo deploy --broadcast --endpoint https://api.quicknode.com/aleo/<KEY>/v1
curl https://api.quicknode.com/aleo/<KEY>/v1/testnet3/transaction/<at...txid>

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
- 撮合核心 `go test ./internal/core/match/` 不受影响；`chain/anubis` ABI 解码测试通过。
- **Aleo testnet 实测（2026-08-03/04）**：
  - Phase 1（公开 mapping）：`anubook_dex.aleo` 部署（`at18cn9993...`）+ 手动/引擎 E2E 链上扣减确认。
  - Phase 2（record 隐私）：`anubook_dex_p2.aleo` 部署（`at1suz8...`）；mint→place_order×2→settle→cancel_order 全链确认；**隐私铁证**（`leo query transaction`）：链上 inputs/outputs 全为 commitment+ciphertext，成交价/数量也是密文；settled_commit=BHP256 hash（`18452056u64`/`18455790u64`）；deadline 过期撤单被 rejected；record 防双花。
  - Phase 2b（Go 引擎）：POST /order(明文+ciphertext) → 订单池 → 撮合 → `leo execute settle` → settled_commit 链上确认（`18455790u64`，hash 与手动一致）。
  - `leo test` 仅能校验编译与签名（on-chain `final` 逻辑无法在 proof context 执行，需 testnet 实测）。
- 隐私性（Phase 2）：区块浏览器只见加密 record + `settled_commit` 承诺，不见价格/量/双方。

### 9.2 升级路径
| 方向 | 现状（MVP）| 升级 | 价值 |
|------|------------|------|------|
| 信任模型 | operator 托管 | 订单归用户 + 双方签名授权（v4.3.0 Keccak+ECDSA）| 去 operator 盗用/抢跑风险 |
| 撮合正确性证明 | 仅结算层守恒 | Leo 电路证明单笔撮合满足价格-时间优先 | 把"operator 可排序"再退一步 |
| 公开行情 | `settled_commit` 哈希 | 解密行情聚合（脱敏 OHLCV）| 兼顾隐私与可观测 |
| 多链 | Anubis + Aleo | `ChainAdapter` 已可插拔，后续可扩展第三链 | 真正多链 |

> v4.3.0（2025-10）原生 Keccak+ECDSA 验签、max array size 提升；v4.2.0 基础费降 90% + 优先费市场。来源：[Provable v4.3.0](https://provable.com/blog/announcing-aleo-stack-v4-3-0)、[v4.2.0](https://provable.com/blog/announcing-aleo-stack-v4.2.0)

---

## 十、风险与待办

| 风险 | 影响 | 缓解 |
|------|------|------|
| **无官方 Go SDK** | 链交互靠手写 REST | 已验证可行；复用 `rpcCall` 风格自写薄客户端 |
| **record 消费需 owner 签名** | 引擎无法直接花用户订单 | MVP 用 operator-custody（§2）；升级走签名授权 |
| **单 transition record 数上限** | 批量结算受限 | v4.3.0 已提升 array 上限；MVP 每批 1-2 笔撮合可演示 |
| **Leo 语法迭代** | 4.x 仍在演进 | Phase 1 先用 `leo build`/`leo test` 验证骨架，不臆测 |
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
