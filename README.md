# AnuBookDEX — 隐私订单簿 DEX 撮合引擎

基于 Go 的加密货币现货交易撮合引擎，支持**集中式**和 **DEX（Anubis Chain）** 双模式运行。

- **集中式模式**：MySQL 轮询订单 → 撮合 → MySQL 持久化 + RabbitMQ 发布
- **DEX 模式**：Anubis Chain 事件订阅 → 隐私解密 → 撮合 → 链上 ZK 结算 + WebSocket 行情广播

## 构建与运行

```bash
# 集中式模式（产出 exchange.bin）
make mac tag=<release-tag>

# DEX 模式（产出 engine.bin）
make dex tag=<release-tag>

# Linux 构建（Docker 交叉编译）
make tag=<release-tag>

# 构建 Docker 镜像
make docker tag=<release-tag>

# 运行全部测试
make test
```

### 启动

```bash
# Docker Compose（推荐）
docker compose up -d --build

# 本地运行
./engine.bin                 # 默认读取 ./conf/config.yaml，HTTP :9000

# 自定义配置
CONFIG_FILE=./conf/dev.yaml ./engine.bin
```

### WebSocket 行情订阅

```bash
# 连接（需要 token 鉴权）
wscat -c "ws://localhost:9000/ws?token=dev-token"

# 订阅频道
{"cmd":"subscribe","channels":["depth.ETH_USDT","kline.ETH_USDT.1min","trade.ETH_USDT"]}
```

### 健康检查

```bash
curl http://localhost:9000/health
# → AnuBookDEX engine running
```

### ABI 同步

```bash
# 在 AnuBookDEX-contracts 中编译并导出 ABI
cd ../AnuBookDEX-contracts
npm run export-abi            # 复制 ABI JSON → Go 项目 contracts/abi/

# 在 Go 项目中生成绑定（需安装 abigen）
cd ../AnuBookDEX
abigen --abi=contracts/abi/Settlement.json --pkg=bindings --out=contracts/bindings/settlement_gen.go
```

## 架构

### 集中式模式（cmd/exchange/）

```
MySQL 序列表 -> puller -> orderSeqChan -> matcher    -> mrChan        -> l2quote（K线/Ticker/成交明细）
                                          -> perch         -> persistence（MySQL 批量写入）
                                          -> publishChan   -> rabbitmq（撮合结果广播）
                                          -> snapshotChan  -> snapshotter（gob 本地 + S3）
                                           depth tickers   -> market（订单簿深度推送）
```

### DEX 模式（cmd/engine/）

```
                AnuBookDEX-contracts/ (Hardhat)
                ────────────┬─────────────
                            │ deploy
                            ▼
Anubis Chain (Registry Smart Contract / Settlement Smart Contract / LeverageManager Smart Contract)
         │                                          ▲
         │ OrderSubmitted 事件                       │ submitBatch + ZKProof
         ▼                                          │
chain/subscriber ──解密──▶ runner.StartMatcher ─────┘
                              │
                              ├──▶ l2quote（K线/成交明细/Ticker）
                              ├──▶ market（深度聚合）
                              ├──▶ rocksdb（快照 + K线持久化）
                              └──▶ chain/settlement ──ZK Proof──▶ Settlement SC
         │
         ▼
WebSocket 客户端（行情订阅）
```

每个交易对独立 goroutine，channel 驱动，无锁设计。

## 订单簿

`internal/core/match/order_book.go` — 两棵红黑树（`gods/treeset`）+ `map[int64]*Order` O(1) 查找：

- **BuySet**：价格从高到低，同价 SeqId 从小到大
- **SellSet**：价格从低到高，同价 SeqId 从小到大

## 撮合逻辑

`internal/core/match/matcher.go` — 支持限价单（Limit）、市价单（Market）、IOC、FOK、撤单（Cancel）、批量撤单（BatchCancel）、限价做市单（LimitMaker）。自成交预防（STP：CB/CO/CN/DC/AST 5 种模式），市价单熔断保护。

## 隐私层（Phase 2）

`internal/dex/privacy/` — Anubis Chain 原生隐私适配：

| 文件 | 功能 |
|------|------|
| `internal/dex/privacy/encryption.go` | ECIES (P256) + AES-256-GCM 混合加密，Note 承诺生成，ECDSA 签名 |
| `internal/dex/privacy/decrypt.go` | View Key 解密链上 Note → match.Order，ViewTag 三层扫描优化 |
| `internal/dex/privacy/zk_prover.go` | 撮合正确性 ZK 证明生成（MVP: SHA256 承诺，目标: gnark PLONK） |
| `internal/dex/privacy/nullifier.go` | Nullifier 防双花标识生成与校验（本地 + 链上 0x0103 预编译） |
| `internal/dex/privacy/kyc.go` | 4 级隐私模型：匿名 / 假名 / ZK-KYC / 合规披露 |

## AI 策略引擎（Phase 3）

`internal/dex/ai/` — 链下独立进程，为交易决策和风控提供智能信号：

| 文件 | 功能 |
|------|------|
| `internal/dex/ai/engine.go` | 行情研判：盘口失衡度 + 深度偏向 + 资金流向 + 舆情 → 5 级信号（HOLD/BUY/SELL/STRONG_BUY/STRONG_SELL），含冷却和幌骗检测 |
| `internal/dex/ai/iceberg.go` | 冰山拆分：TWAP / VWAP / Adaptive 三种策略，随机抖动防探测，限速控制，进度回调 |
| `internal/dex/ai/risk.go` | 风控引擎：1-10x 杠杆监控，强平价格计算，四级风险评级（LOW/MEDIUM/HIGH/CRITICAL），自动减仓 + 强平事件记录 |

## 目录结构

```
AnuBookDEX/
├── cmd/
│   ├── engine/            # DEX 模式入口
│   └── exchange/          # 集中式模式入口
│
├── internal/
│   ├── core/              # 撮合引擎核心（双模式复用）
│   │   ├── match/         #   订单簿 + 撮合 + Order 类型
│   │   ├── l2quote/       #   K线 / Ticker / 成交明细
│   │   └── market/        #   深度聚合
│   │
│   ├── dex/               # DEX 模式专用
│   │   ├── chain/         #   链上事件订阅 + ZK 结算 + 杠杆适配
│   │   ├── privacy/       #   加密 / 解密 / ZK 证明 / Nullifier / KYC
│   │   ├── ai/            #   行情研判 / 冰山拆分 / 风控
│   │   ├── ws/            #   WebSocket Hub + Client（零依赖）
│   │   ├── rocksdb/       #   本地 KV 存储
│   │   ├── runner/        #   共享撮合主循环
│   │   └── auth/          #   HTTP/WS 认证中间件
│   │
│   ├── infra/             # 共享基础设施（双模式共用）
│   │   ├── common/        #   日志 / 配置加载 / 工具函数
│   │   ├── config/        #   Viper 包装层
│   │   ├── scheduler/     #   快照 + 上报定时器
│   │   ├── statistics/    #   撮合统计
│   │   └── dogstatsd/     #   Datadog 指标（可选）
│   │
│   └── centralized/       # 集中式专用
│       ├── puller/        #   MySQL 序列表轮询
│       ├── persistence/   #   MySQL 批量写入
│       ├── rabbitmq/      #   AMQP 发布订阅
│       ├── redis/         #   Redis 客户端
│       ├── snapshotter/   #   Gob 快照 + S3
│       ├── validate/      #   恢复校验
│       └── assign/        #   订单分配
│
├── conf/                  # 配置文件
├── contracts/             # ABI JSON + Go 绑定
├── docs/                  # 方案文档 + 架构图
├── static/                # 前端原型
│
├── go.mod / go.sum
├── makefile
├── Dockerfile
├── docker-compose.yml
├── README.md
├── CLAUDE.md
└── .gitignore
```

## 杠杆交易（Phase 4）

`internal/dex/chain/leverage.go` — Anubis LeverageManager SC 适配层，内聚 `internal/dex/ai` RiskEngine：

| 功能 | 说明 |
|------|------|
| 开仓 | 1-10x 杠杆，保证金加密后提交 SC + 本地 RiskEngine 登记 |
| 追加保证金 | 动态重算强平价格，链上追加 |
| 自动追保 | 风险 MEDIUM → MarginCall 通知（1 小时内追加 50% 保证金） |
| 自动减仓 | 风险 HIGH → 自动平掉 50% 仓位 |
| 自动强平 | 风险 CRITICAL → 执行强平（2.5% 罚金），记录 LiquidationEvent |
| 资金费率 | 每 8 小时更新，`CalculateFundingPayment` 计算应收/应付 |

## 测试

```bash
# 全量测试
make test

# 仅撮合核心
go test -count=1 ./internal/core/match/

# 指定用例
go test -count=1 -run TestMatchLimit ./internal/core/match/
```

## 实施进度

| Phase | 内容 | 状态 |
|-------|------|------|
| Phase 1 | 基础迁移（模块重命名、chain/ws/rocksdb、入口改造） | ✅ 完成 |
| Phase 2 | 隐私层（加密/解密/ZK 证明/Nullifier/KYC） | ✅ 完成 |
| Phase 3 | AI 策略引擎（行情研判/冰山拆分/风控） | ✅ 完成 |
| Phase 4 | 杠杆交易（LeverageManager SC 适配层） | ✅ 完成 |
| Phase 5-8 | 暗池 MPC / ZK-KYC / RocketSwap / NFT | 待实施 |

## 关键依赖

- `shopspring/decimal` — 高精度十进制（禁止 float64）
- `emirpasic/gods` — 红黑树订单簿
- `json-iterator/go` — 高性能 JSON
- `spf13/viper` — 配置管理
- `go.uber.org/zap` — 结构化日志
- `file-rotatelogs` — 日志按小时轮转
