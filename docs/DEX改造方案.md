# AnuBookDEX：集中式撮合引擎 → 隐私 DEX 改造方案

## 背景

现有 Go 代码是一个生产级加密货币现货交易撮合引擎（`market-match`），包含订单簿、撮合、行情生成、持久化等完整功能。PRD 要求基于 ANUBIS Chain 构建融合 ZK-SNARK + MPC + AI 的隐私订单簿 DEX。本方案评估复用性并给出改造路径。

## 暗池项目参考：https://renegade.fi/

## Anubis Chain 技术分析

### 基本信息

| 属性 | 值 |
|------|-----|
| 类型 | Layer 1 EVM 兼容公链 |
| 主网上线 | 2026 年 4 月 8 日 |
| EVM 兼容 | 100% 兼容，Solidity 合约可一键迁移 |
| 出块时间 | ~2 秒 |
| 吞吐量 | > 1000 TPS |
| ZK 协议 | PLONK / Turbo PLONK |
| ZK 验证速度 | < 10ms |
| 生态 DEX | RocketSwap |
| 生态 Launchpad | Capybara |
| 孵化器 | Anubis Labs |

### 混合状态模型（Hybrid State Model）

Anubis 的核心创新是**双状态分离 + 协同同步**架构：

```
┌──────────────────────────────────────────────────┐
│              Anubis Chain 区块头                    │
│  ┌─────────────────┐  ┌─────────────────┐         │
│  │  私有状态根 (SMT) │  │ 公开状态根 (MPT) │         │
│  └────────┬────────┘  └────────┬────────┘         │
│           │                    │                   │
│  ┌────────▼────────┐  ┌───────▼─────────┐         │
│  │ 私有状态层 (UTXO) │  │ 公开状态层 (EVM) │         │
│  │                  │  │                  │         │
│  │ · Note 票据系统   │  │ · 标准 MPT 存储   │         │
│  │ · Pedersen 承诺  │  │ · Solidity 合约   │         │
│  │ · Nullifier 集   │  │ · MetaMask 兼容   │         │
│  │ · View Key 权限  │  │ · 标准账户结构     │         │
│  └────────┬────────┘  └───────┬─────────┘         │
│           │                    │                   │
│           └───────┬────────────┘                   │
│                   │                                │
│          ┌────────▼────────┐                       │
│          │  预编译合约 (协同) │                       │
│          │  0x0100 VERIFY   │                       │
│          │  0x0103 NULLIFIER│                       │
│          └─────────────────┘                       │
└──────────────────────────────────────────────────┘
```

### 扩展交易类型（EIP-2718）

| 类型 | 操作码 | 说明 | 在本项目中的应用 |
|------|--------|------|----------------|
| Type 100 (Shield) | `0x64` | 公开→私有：锁定公开代币，铸造隐私票据 | 用户充值后隐私化资产 |
| Type 101 (Transfer) | `0x65` | 完全隐私转账（类似 Zcash Sapling） | 隐私订单的资金转移 |
| Type 102 (Unshield) | `0x66` | 私有→公开：销毁票据，提取到 EOA | 撮合成交后结算提现 |
| Type 103 (Contract Call) | `0x67` | 花隐私币调用 EVM 合约 | **核心**：订单提交到 Registry SC |

### 预编译合约

| 合约 | 地址 | 功能 | 本项目用途 |
|------|------|------|-----------|
| VERIFY_PROOF | `0x0100` | PLONK 证明验证 + 双线性配对检查 | Settlement SC 验证撮合正确性证明 |
| NULLIFIER_CHECK | `0x0103` | Nullifier 防双花检查 + 并发冲突处理 | 订单唯一性校验 |

### 接入关键参数

| 参数 | 说明 |
|------|------|
| **链 ID** | 待确认（需从官方文档获取） |
| **RPC 端点** | 待确认（主网 RPC 地址） |
| **WebSocket** | 待确认（事件订阅端点） |
| **区块链浏览器** | 待确认 |
| **Gas Token** | ANUB（原生代币） |
| **跨链桥** | 支持以太坊、BSC 等主流公链 |

### 隐私技术栈

- **Note 票据系统**：基于 Pedersen 承诺，加密资产全生命周期管理
- **隐身地址 (EIP-5564)**：ECDH 密钥交换生成一次性地址，斩断身份关联
- **View Key 权限**：用户自主选择性披露，实现分级隐私
- **稀疏默克尔树 (SMT)**：存储票据承诺，支持高效证明
- **Nullifier 机制**：防双花 + 三层扫描优化（视图标签过滤 → 链上哈希校验 → 并行化分片）

---

## 结论：高度可复用（核心撮合逻辑 ~80% 可直接复用）

撮合引擎的**匹配算法、订单簿数据结构、行情生成算法**是纯计算逻辑，无基础设施依赖。主要替换的是**数据输入输出通道**（MySQL → 链上事件，RabbitMQ → WebSocket/链上事件，Redis → RocksDB，S3 → IPFS/链上状态根）。

**关键洞察**：Anubis Chain 已原生提供隐私基础设施（Note 系统、隐身地址、ZK 验证预编译合约），因此本项目的隐私层可以**大幅简化**——不再需要从零构建隐私协议，而是**适配 Anubis 的原生隐私能力**。

---

## 一、复用 vs 替换总览

### 直接复用（纯逻辑，无需改动）

| 文件 | 内容 |
|------|------|
| `match/matcher.go` | 全部撮合算法：matchMarket、matchLimit、matchFok、matchCancel、matchBatchCancel、STP 引擎（5 种模式）、熔断保护、finalizeTaker |
| `match/order_book.go` | 红黑树订单簿：Enqueue、Dequeue、Peek、Clone、Find、Cache |
| `match/order.go` | Order 状态机、Comparator（价格-时间优先）、fillAmount、所有枚举常量 |
| `l2quote/kline.go` | K线生成算法：updateKline、getCurrentKline、OHLCV 累加、价格前向传播 |
| `l2quote/market.go` | 24 小时滑动窗口：buildMarket24Hour、updateMarketDetail24Hour |
| `l2quote/trade.go` | 成交明细生成：方向推导、TradeDetail 创建 |
| `l2quote/ticker.go` | Ticker 结构体和 Change/ChangePercent 计算 |
| `market/depth.go` | 深度聚合算法：buildDepth、buildDepthPercent10 |

### 需要字段适配（逻辑不变，加字段）

| 文件 | 改动 |
|------|------|
| `match/order.go` | UserId int64 → UserAddress common.Address；新增 TxHash、BlockNumber、Signature、EncryptedPrice、ZKProof、Salt、Deadline 字段 |
| `match/matcher.go` | MatchResult 新增 TxHash、BlockNumber；UserId 比较改为 Address 比较 |

### 需要整套替换（基础设施依赖）

| 包 | 集中式实现 | DEX 替代 |
|----|----------|----------|
| `puller/` | MySQL 轮询 `aibit_spot_sequence_%s` | **链上事件订阅**（Solidity 事件 → WebSocket） |
| `persistence/` | MySQL 批量 INSERT | **链上结算合约**（Settlement SC）+ 本地 RocksDB 归档 |
| `rabbitmq/` | AMQP 发布/订阅 | **WebSocket 扇出**（客户端）+ **链上事件**（链上消费者） |
| `redis/` | K线缓存 | **RocksDB**（嵌入式 KV 存储） |
| `snapshotter/` | gob 文件 + S3 | **RocksDB** + **IPFS/Arweave** + 链上状态根 |

---

## 二、改造后架构

```
                  ANUBIS Chain Layer 1
 ┌────────────────────────────────────────────────────────────┐
 │                    区块头                                  │
 │  ┌──────────────────┐  ┌──────────────────┐               │
 │  │ 私有状态根 (SMT)  │  │ 公开状态根 (MPT)  │               │
 │  └────────┬─────────┘  └────────┬─────────┘               │
 │           │                     │                          │
 │  ┌────────▼─────────┐  ┌────────▼──────────┐              │
 │  │ 私有层 (UTXO)     │  │ 公开层 (EVM)       │              │
 │  │                  │  │                    │              │
 │  │ · Note 加密订单   │  │ OrderBookRegistry  │              │
 │  │ · Nullifier 唯一性│  │    SC              │              │
 │  │ · View Key 解密  │  │ Settlement SC      │              │
 │  │                  │  │ LeverageManager SC │              │
 │  │                  │  │ DarkPoolRouter SC  │              │
 │  │                  │  │ ZKKYC SC           │              │
 │  │                  │  │ LPMiningRewards SC │              │
 │  └────────┬─────────┘  └──────┬─────┬───────┘              │
 │           │                   │     │                       │
 │           │       预编译合约    │     │                       │
 │           │  0x0100:VERIFY ◄───┘     │                       │
 │           │  0x0103:NULLIFIER_CHECK   │                       │
 └───────────┼───────────────────────────┼───────────────────────┘
             │                           │
      Type 100/101/103           Solidity Events
      隐私订单提交               + 公开结算交易
             │                           │
             ▼                           ▼
 ┌───────────────────────────────────────────────────────────┐
 │             Off-Chain Go Engine (AnuBookDEX)               │
 │                                                            │
 │  ┌─────────────────┐  ┌──────────────────┐                │
 │  │ chain/           │  │ match/ (复用)     │                │
 │  │ subscriber.go    │──▶ matcher.go       │──────────┐     │
 │  │ (订阅链上事件+解密)│  │ order_book.go    │          │     │
 │  │ settlement.go   │  │ order.go (+字段) │          │     │
 │  │ (批量提交ZK结算)  │  └──────────────────┘          │     │
 │  └─────────────────┘                                 │     │
 │                                                      ▼     │
 │                                       ┌──────────────┐    │
 │                                       │ privacy/      │    │
 │                                       │ decrypt.go    │    │
 │                                       │ (ViewKey解密) │    │
 │                                       └──────┬───────┘    │
 │                                              │             │
 │                      ┌───────────────────────▼───┐         │
 │                      │ l2quote/ (复用)           │         │
 │                      │ kline.go · ticker.go     │────────┐│
 │                      │ trade.go · market.go     │        ││
 │                      └──────────────────────────┘        ││
 │                                              │            ││
 │                      ┌───────────────────────▼───┐        ││
 │                      │ ws/ (新建)                │        ││
 │                      │ hub.go · client.go       │        ││
 │                      │ (WebSocket 行情扇出)      │        ││
 │                      └──────────────────────────┘        ││
 │                                              │            ││
 │  ┌───────────────────┐  ┌──────────────────┐ │            ││
 │  │ rocksdb/ (新建)    │  │ ai/ (新建)        │ │            ││
 │  │ kline_store.go    │  │ engine.go        │ │            ││
 │  │ snapshot_store.go  │  │ iceberg.go       │ │            ││
 │  │ (嵌入式 KV 存储)   │  │ risk.go          │ │            ││
 │  └───────────────────┘  └──────────────────┘ │            ││
 └───────────────────────────────────────────────┼────────────┘│
                                                 │             │
         ┌───────────────────────────────────────┘             │
         ▼                                                     ▼
 ┌───────────────┐                              ┌─────────────────┐
 │   WebSocket    │                              │  RocketSwap DEX  │
 │   客户端 (Web)  │                              │  (链上 AMM 路由)  │
 └───────────────┘                              └─────────────────┘
```

**核心设计原则**：
1. 撮合在内存中进行（<10ms），订单仅在持久化和网络层加密
2. **隐私委托给 Anubis 原生层**：利用 Type 100-103 交易 + Note 系统 + View Key，不在匹配热路径上做同态加密
3. **链上只存哈希/证明**：订单明文仅在链下引擎内存中短暂存在，链上只存储 Note 承诺和 ZK 证明
4. **选择性披露**：小额交易完全隐私，大额交易自动触发 ZK-KYC 合规验证

---

## 三、分阶段实施计划

### Phase 1：基础迁移（Month 1-2）— 最小可运行 DEX

**目标**：撮合引擎在链上事件驱动下工作，复用全部撮合逻辑。

| 任务 | 内容 | 关键文件 |
|------|------|----------|
| 1.1 模块重命名 | `go.mod` 改为 `github.com/AnuBookDEX/engine`，Go 版本 1.21+ | `go.mod` |
| 1.2 链上事件订阅 | 新建 `chain/subscriber.go`，订阅 OrderBook Registry SC 事件 | 替换 `puller/puller.go` |
| 1.3 启动流程改造 | 原 `market-match.go` → `cmd/engine/main.go`，`startMatcher` goroutine 保持不变 | `market-match.go` |
| 1.4 链上结算适配 | 新建 `chain/settlement.go`，批量提交 MatchResult 到 Settlement SC | 替换 `persistence/persistence.go` |
| 1.5 本地 RocksDB | 新建 `rocksdb/`，替代 Redis（K线缓存）和 S3（快照） | 替换 `redis/`、`snapshotter/` |
| 1.6 WebSocket 扇出 | 新建 `ws/hub.go`，替代 RabbitMQ 发布行情 | 替换 `rabbitmq/` |
| 1.7 配置迁移 | 从链上 Registry SC 读取交易对参数，替代 viper YAML | `common/config.go` |

**验收**：撮合引擎处理链上事件产生撮合结果，K线正确更新，WebSocket 客户端收到行情。`match/` 包全部测试通过。

### Phase 2：隐私层（Month 2-3）— 适配 Anubis 原生隐私

**核心策略**：Anubis Chain 已提供 Note 系统 + 隐身地址 + ZK 验证预编译合约，本项目只需适配而非重建。

| 任务 | 内容 | Anubis 原生能力复用 |
|------|------|-------------------|
| 2.1 链上隐私订单 | 通过 Anubis Type 103 (Contract Call) 提交加密订单到 Registry SC，触发事件 | Type 103 花隐私币调合约，仅披露最少数据 |
| 2.2 View Key 解密 | 新建 `privacy/decrypt.go`，引擎使用 View Key 解密 Note 中提取的订单数据 | Note 票据系统 + View Key 权限控制 |
| 2.3 ZK 撮合证明 | 新建 `privacy/zk_prover.go`，用 gnark 生成 PLONK 证明，传入 Settlement SC 的 `0x0100` 预编译验证 | 0x0100 VERIFY_PROOF 预编译合约（<10ms 验证） |
| 2.4 Nullifier 防重放 | 每个订单绑定唯一 Nullifier，通过 `0x0103` 预编译检查防止重复提交 | 0x0103 NULLIFIER_CHECK 预编译合约 |
| 2.5 Order 结构体适配 | 新增 UserAddress、NoteCommitment、Nullifier、ZKProof、ViewTag、Deadline 字段 | 隐身地址 (EIP-5564)、Note 字段映射 |
| 2.6 分级隐私模型 | 小额交易完全隐私 (Type 101)，大额交易 ZK-KYC (Type 103 + ZK 身份证明) | ZK-KYC 合规框架 (ICAO/MiCA/FATF) |

### Phase 3：AI 策略引擎（Month 2-3，与 Phase 2 并行）

| 任务 | 内容 |
|------|------|
| AI 行情研判 | 新建 `ai/engine.go`，链下独立进程，分析盘口/资金流向/舆情 |
| AI 冰山拆分 | 新建 `ai/iceberg.go`，大单自动拆分算法 |
| AI 风控 | 新建 `ai/risk.go`，7×24 监控杠杆仓位，自动减仓逻辑 |

### Phase 4：杠杆交易（Month 3-4）

- 新建 `chain/leverage.go` → Leverage Manager SC 适配层
- 支持 1-10x 杠杆，保证金加密，强平自动触发

### Phase 5：暗池 MPC（Month 4-5）

- 新建 `privacy/mpc/` → SPDZ/Shamir 秘密分享协议
- 新建 `match/dark_pool_book.go` → 基于 `OrderBook` 的特殊化暗池订单簿

### Phase 6：ZK-KYC 合规（Month 4-5，与 Phase 5 并行）

- 新建 `privacy/kyc.go`，分级隐私验证（小额匿名 / 大额 ZK-KYC）

### Phase 7：RocketSwap 互通 + LP 挖矿（Month 5-6）

- 新建 `chain/router.go` → AI 路由选择 AnuBook 订单簿 vs RocketSwap AMM
- 新建 `chain/lp_mining.go` → LP 质押和奖励

### Phase 8：NFT 权益 + 生产加固（Month 6）

- 新建 `chain/nft.go` → NFT 持有自动识别，手续费折扣
- 添加 Prometheus 指标、RPC 重连、验证节点

---

## 四、关键接口定义

### 4.1 链上事件订阅（替代 puller）

```go
// Anubis 事件订阅器
type ChainSubscriber interface {
    // Subscribe 订阅指定交易对的链上订单事件（Solidity event → channel）
    Subscribe(symbol string) (<-chan *DecryptedOrder, error)
    // Unsubscribe 取消订阅
    Unsubscribe(symbol string) error
}

// 从 Anubis Note 系统解密后的订单
type DecryptedOrder struct {
    *match.Order                                         // 复用现有 Order 结构
    UserAddress   common.Address  `json:"user-address"`  // Anubis 地址
    TxHash        common.Hash     `json:"tx-hash"`        // 链上交易哈希
    BlockNumber   uint64          `json:"block-number"`    // 区块号
    NoteCommitment []byte         `json:"note-commitment"` // Anubis Note 承诺
    Nullifier     []byte          `json:"nullifier"`       // 防双花标识
    ViewTag       []byte          `json:"view-tag"`        // 视图标签（加速扫描）
    Signature     []byte          `json:"signature"`       // ECDSA 签名
    Deadline      uint64          `json:"deadline"`        // 订单过期区块号
}
```

### 4.2 链上结算（替代 persistence）

```go
type SettlementTarget interface {
    // SubmitBatch 批量提交撮合结果到 Settlement SC
    // 包含 ZK 证明用于 0x0100 预编译验证
    SubmitBatch(mrs []*match.MatchResult, zkProof []byte) (common.Hash, error)
    // GetTransactionReceipt 查询结算交易回执
    GetTransactionReceipt(txHash common.Hash) (*SettlementReceipt, error)
}
```

### 4.3 行情广播（替代 rabbitmq）

```go
type MarketDataBroadcaster interface {
    BroadcastDepth(symbol string, depth *market.QuoteDepths)
    BroadcastKline(symbol string, interval string, kline *l2quote.KLine)
    BroadcastTrade(symbol string, trade *l2quote.TradeDetail)
    // Subscribe 客户端订阅行情频道
    Subscribe(clientID string, channels []string) error
}
```

### 4.4 本地存储（替代 redis + snapshotter）

```go
type KlineStore interface {
    Save(symbol string, klineType string, k *l2quote.KLine) error
    LoadLatest(symbol string, klineType string) (*l2quote.KLine, error)
    LoadRange(symbol string, klineType string, from, to int64) ([]*l2quote.KLine, error)
}

type SnapshotStore interface {
    Save(symbol string, book *match.OrderBook) (stateRoot [32]byte, error)
    LoadLatest(symbol string) (*match.OrderBook, error)
    // PruneOld 清理过期快照
    PruneOld(symbol string, keepN int) error
}
```

### 4.5 隐私解密

```go
type PrivacyDecryptor interface {
    // DecryptOrder 使用 View Key 解密 Anubis Note 中的订单数据
    DecryptOrder(noteCommitment []byte, viewKey *ViewKey) (*match.Order, error)
    // GenerateZKProof 生成撮合正确性的 PLONK 证明
    GenerateZKProof(mrs []*match.MatchResult, circuitInput *CircuitInput) ([]byte, error)
}
```

---

## 五、Anubis Chain 接入步骤

### 5.1 接入流程

```
1. 获取 Anubis RPC 端点
   ├── HTTP RPC: 待确认 (用于交易提交)
   ├── WebSocket: 待确认 (用于事件订阅)
   └── Chain ID: 待确认

2. 部署 Solidity 合约套件
   ├── 使用 Hardhat/Foundry 编译
   ├── 配置 Anubis 网络到 hardhat.config.ts
   └── 部署验证（需 ANUB 原生代币作为 Gas）

3. 链下引擎连接
   ├── chain/subscriber.go → WebSocket 订阅 Registry SC 事件
   ├── chain/settlement.go  → HTTP RPC 提交结算交易
   └── 配置 Anubis RPC 重连策略

4. 隐私功能启用
   ├── 生成/导入 View Key 用于 Note 解密
   ├── 集成 gnark PLONK 证明生成
   └── 对接 0x0100/0x0103 预编译合约

5. 端到端测试
   ├── Anubis 测试网验证
   ├── 隐私交易完整链路测试
   └── 性能基准测试（撮合延迟 + ZK 证明生成）
```

### 5.2 Go 端与 Anubis 交互的 SDK 选型

```go
// 方案 A: 使用 go-ethereum (推荐，Anubis 100% EVM 兼容)
import (
    "github.com/ethereum/go-ethereum/ethclient"
    "github.com/ethereum/go-ethereum/accounts/abi/bind"
)

// 连接 Anubis Chain
client, _ := ethclient.Dial("wss://<anubis-rpc-ws-endpoint>")
// 或 HTTP
client, _ := ethclient.Dial("https://<anubis-rpc-endpoint>")

// 方案 B: 直接使用 Anubis 官方 Go SDK（如果提供）
// 待 Anubis 官方发布 Go SDK 后评估
```

### 5.3 合约事件监听（go-ethereum）

```go
// 订阅 OrderBookRegistry 的 OrderSubmitted 事件
registry, _ := NewOrderBookRegistry(contractAddress, client)
opts := &bind.WatchOpts{Start: nil, Context: context.Background()}
eventChan := make(chan *OrderBookRegistryOrderSubmitted)
sub, _ := registry.WatchOrderSubmitted(opts, eventChan, nil)

for event := range eventChan {
    // 解密 Note → 提取订单 → 送入 match goroutine
    order := decryptOrderFromNote(event.NoteCommitment, viewKey)
    orderSeqChan <- order
}
```

### 5.4 交易提交

```go
// 向 Settlement SC 提交批量撮合结果 + ZK 证明
auth, _ := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
settlement, _ := NewSettlement(contractAddress, client)
tx, _ := settlement.SubmitBatch(auth, matchResults, zkProof)
receipt, _ := bind.WaitMined(context.Background(), client, tx)
```

### 5.5 待确认的关键参数

接入前必须从 Anubis 官方获取：

| 参数 | 重要性 | 获取方式 |
|------|--------|---------|
| Chain ID | **必须** | 官方文档 / `eth_chainId` RPC 调用 |
| RPC HTTP 端点 | **必须** | 官方文档 |
| RPC WebSocket 端点 | **必须**（事件订阅） | 官方文档 |
| ANUB 代币合约地址 | **必须**（Gas 费用估算） | 区块链浏览器 |
| 预编译合约 ABI | **重要**（0x0100/0x0103） | Anubis 白皮书 |
| View Key SDK | **重要**（Note 解密） | 官方 GitHub |
| PLONK 电路参数 | **重要**（ZK 证明生成） | Anubis 技术文档 |
| 测试网信息 | **重要**（开发测试） | 官方文档 |

---

## 六、需要编写的 Solidity 合约

### 6.1 合约清单

| 合约 | 职责 | Anubis 特性利用 |
|------|------|----------------|
| **OrderBookRegistry** | 交易对注册、接收 Type 103 隐私订单事件、参数管理 | 公开状态层，emit OrderSubmitted(NoteCommitment, ViewTag, Deadline) |
| **Settlement** | 批量撮合结果验证、Token 转账、费用分配 | **调用 0x0100 预编译**验证 ZK 证明；使用 Type 102 (Unshield) 提现 |
| **LeverageManager** | 保证金管理、持仓跟踪、强平自动触发 | 保证金以 Note 形式锁定在私有层 |
| **DarkPoolRouter** | MPC 轮次协调、暗池结算记录 | Type 101 (Transfer) 隐私转账完成暗池结算 |
| **ZKKYC** | 分级隐私验证、Merkle 树管理、合规审计 | ZK-KYC 框架，ICAO/MiCA/FATF 合规 |
| **LiquidityRouter** | AnuBook 订单簿 ↔ RocketSwap AMM 路由 | 直接调用 RocketSwap 合约，跨协议流动性同步 |
| **LPMiningRewards** | LP 代币质押、手续费分红、生态代币奖励 | 公开层标准 ERC-20 质押逻辑 |

### 6.2 Settlement 合约核心逻辑（伪代码）

```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract Settlement {
    // Anubis 预编译合约地址
    address constant VERIFY_PROOF = address(0x0100);
    address constant NULLIFIER_CHECK = address(0x0103);

    struct MatchResult {
        uint64  id;
        bytes32 orderId;
        address user;
        uint256 price;
        uint256 amount;
        bytes32 nullifier;
        string  role;  // "maker" or "taker"
    }

    event BatchSettled(uint64 indexed batchId, bytes32 stateRoot);

    function submitBatch(
        MatchResult[] calldata results,
        bytes calldata zkProof
    ) external returns (bytes32 stateRoot) {
        // 1. ZK 证明验证 (调用 Anubis 0x0100 预编译)
        (bool success, ) = VERIFY_PROOF.staticcall(zkProof);
        require(success, "ZK proof verification failed");

        // 2. Nullifier 防双花检查 (调用 Anubis 0x0103 预编译)
        for (uint i = 0; i < results.length; i++) {
            (bool valid, ) = NULLIFIER_CHECK.staticcall(
                abi.encodePacked(results[i].nullifier)
            );
            require(valid, "Duplicate nullifier detected");
        }

        // 3. Token 结算（从私有层 Unshield 到公开层）
        for (uint i = 0; i < results.length; i++) {
            if (keccak256(bytes(results[i].role)) == keccak256("maker")) {
                // 成交方收款
                _unshieldAndTransfer(results[i].user, results[i].amount);
            }
        }

        // 4. 更新状态根
        stateRoot = _computeStateRoot(results);
        emit BatchSettled(uint64(block.number), stateRoot);
    }
}
```

### 6.3 与 RocketSwap 的集成

Anubis Chain 生态已有 RocketSwap DEX，本项目通过 `LiquidityRouter` 合约实现：

```
用户下单 → AI Router 判断：
  ├── 订单簿深度充足 → AnuBookDEX 撮合（低滑点）
  └── 订单簿深度不足 → 路由至 RocketSwap AMM（流动性保障）
```

---

## 七、验证方案

### 7.1 撮合正确性
- `match/` 包现有测试全部通过 —— `go test -count=1 ./match/`
- 撮合结果与原集中式引擎对比一致（历史数据回放验证）

### 7.2 行情数据正确性
- Kline/Ticker/Trade 生成结果与改造前对比一致
- 深度快照恢复后订单簿状态完全一致

### 7.3 端到端流程（Anubis 测试网）
```
Anubis 测试网提交 Type 103 加密订单
  → chain/subscriber 接收链上事件
    → privacy/decrypt 用 View Key 解密 Note
      → match goroutine 内存撮合
        → privacy/zk_prover 生成 PLONK 证明
          → chain/settlement 提交至 Settlement SC
            → 0x0100 预编译验证 ZK 证明
              → 0x0103 预编译校验 Nullifier
                → Token 结算 + WebSocket 推送行情
```

### 7.4 隐私性验证
- Type 101 隐私转账：发送方/接收方/金额在链上不可见
- Type 103 合约调用：仅披露该笔交易必需的最小数据
- ZK 证明在 `0x0100` 预编译验证通过
- Merge 后双状态根一致（私有 SMT + 公开 MPT = 全局状态根）

### 7.5 性能基准
| 指标 | 目标 | 依赖 |
|------|------|------|
| 撮合延迟 | < 10ms | 纯内存计算，无外部依赖 |
| ZK 证明生成 | < 3s | gnark PLONK 证明生成 |
| ZK 证明验证（链上） | < 10ms | Anubis 0x0100 预编译 |
| 出块确认 | ~2s | Anubis PoSWA 共识 |
| 事件监听到撮合启动 | < 100ms | WebSocket 订阅延迟 |

### 7.6 合规验证
- ZK-KYC 对 100 ETH+ 大额交易自动触发身份验证
- ZK-KYC 对 < 1 ETH 小额交易不触发（隐私自由）
- AML/FATF/MiCA 审计接口可查询合规状态（不泄露交易细节）

---

## 八、风险与待确认事项

| 风险/事项 | 影响 | 建议 |
|----------|------|------|
| Anubis Chain RPC 稳定性 | 主网刚上线（2026.4），可能不稳定 | 添加指数退避重连 + 多端点 fallback |
| View Key SDK 可用性 | 无 SDK 则需自行实现 Note 解密逻辑 | 提前研究 Anubis Note 数据结构 |
| PLONK 电路参数未公开 | 无法生成兼容的 ZK 证明 | 直接联系 Anubis 团队获取技术文档 |
| 0x0100/0x0103 预编译 ABI | 无 ABI 无法调用 | 从 Anubis 白皮书/GitHub 获取 |
| RocketSwap 合约接口 | 对接需要其 Router 合约地址和 ABI | 从 RocketSwap 官方文档获取 |
| 跨链桥资产隐私化 | 从以太坊/BSC 跨链的资产需要 Shield | 验证跨链桥是否支持 Type 100 交易 |
| ANUB 代币获取 | 部署合约 + Gas 费用需要 ANUB | 通过交易所或测试网水龙头获取 |
