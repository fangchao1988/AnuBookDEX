# AnuBookDEX — 隐私订单簿 DEX 撮合引擎

基于 Go 的加密货币现货交易撮合引擎，支持**集中式**与 **DEX 双链（Anubis + Aleo）** 多模式运行。

- **集中式模式**：MySQL 序列表轮询 → 撮合 → MySQL 持久化 + RabbitMQ 发布实时行情
- **DEX 模式（Anubis Chain）**：事件订阅 → 隐私解密 → 撮合 → 链上 ZK 结算 + WebSocket 行情广播
- **DEX 模式（Aleo）**：链上托管下单（Leo 合约 + 加密 Order record）→ view key 解密 → 链下撮合 → `leo execute settle` 链上结算（隐私 DeFi，黑客松主力方向）

撮合核心三模式共享；链后端通过 `internal/dex/chain` 的 `ChainAdapter` 接口（`OrderSource` / `SettlementSink`）注入，实现位于 `internal/dex/chain/{anubis,aleo}`。

## 构建与运行

```bash
# macOS 本地构建（集中式模式，产出 exchange.bin）
make mac tag=<release-tag>

# macOS 本地构建（DEX - Anubis 链，产出 engine-anubis.bin）
make anubis tag=<release-tag>

# macOS 本地构建（DEX - Aleo 链，产出 engine-aleo.bin）
make aleo tag=<release-tag>

# Linux 构建（Docker 交叉编译）
make tag=<release-tag>

# 运行全部测试
make test

# 运行单个包 / 指定用例
go test -count=1 ./internal/core/match/
go test -count=1 -run TestMatchLimit ./internal/core/match/
```

### 启动引擎

```bash
# DEX - Aleo 链（注意：必须 env -u 清除环境变量，否则 viper BindEnv
# 会用 shell 环境的 ALEO_PRIVATE_KEY/ALEO_VIEW_KEY 覆盖 config.yaml）
env -u ALEO_PRIVATE_KEY -u ALEO_VIEW_KEY ./engine-aleo.bin

# DEX - Anubis 链
./engine-anubis.bin

# 集中式
./exchange.bin

# 自定义配置
CONFIG_FILE=./conf/dev.yaml ./engine-aleo.bin
```

配置从 `./conf/config.yaml` 加载（`chain.anubis.*` / `chain.aleo.*` 分段）。HTTP 服务默认监听 9000 端口。

### 健康检查

```bash
curl http://localhost:9000/health
# → AnuBookDEX engine running
```

### WebSocket 行情订阅

```bash
wscat -c "ws://localhost:9000/ws?token=dev-token"
{"cmd":"subscribe","channels":["depth.ALEO_USDCX","kline.ALEO_USDCX.1min","trade.ALEO_USDCX"]}
```

## 前端（web/）

React 18 + Vite + TypeScript + Tailwind + zustand，Shield 钱包（Provable 官方，Aleo Wallet Standard）签名下单。

```bash
cd web
npm install
npm run dev        # http://localhost:5173（vite 代理 /order、/api、/ws → :9000）
npm run build      # tsc + vite build → dist/
```

- **交易对**：`ALEO/USDCX`（p4/p5 真实合约，6 位精度，跨程序托管 USDCX Token + credits + 合规凭证）、`ETH/USDT`、`BTC/USDT`（p2 铸币模式）
- **下单模式**：标准（明文入簿）/ 隐私（链上加密 record，引擎 view key 解密后撮合）/ 暗池（规划中）
- **外网访问**：Shield 钱包扩展只在 `localhost` / `127.0.0.1` / `HTTPS` 页面注入，HTTP 外网域名（如花生壳）无法连接钱包——需用 HTTPS 隧道，如 `ngrok http --url=<你的静态域名> 5173`，并把域名加入 `web/vite.config.ts` 的 `server.allowedHosts`

### ABI 同步（Anubis 链）

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

每个交易对在独立 goroutine 中运行，channel 驱动，无锁设计。

### DEX 模式（cmd/engine/{anubis,aleo}）

```
前端（Shield 钱包）place_order* 链上托管 ──▶ 交易对链（Anubis / Aleo）
       │                                        │
       │ POST /order {tx_id}                    │ OrderSubmitted 事件（Anubis）
       ▼                                        │ 链上记录提取 + view key 解密（Aleo）
runner.StartMatcher ◀── chain.OrderSource ──────┘
       │
       ├──▶ l2quote（K线/成交明细/Ticker）──▶ WebSocket Hub（行情订阅）
       ├──▶ market（深度聚合）
       ├──▶ rocksdb（订单簿快照）
       └──▶ chain.SettlementSink ──ZK 结算──▶ 链上合约（settle transition）
```

### Aleo 链 DEX 流程（隐私 DeFi 核心链路）

1. **链上托管下单**：钱包执行 Leo 合约（`anubook_dex_p5.aleo`）`place_order_buy` / `place_order_sell`——Order record（加密，归 operator）+ 托管资产（买单锁 USDCX Token + 合规凭证 Credentials，卖单锁 ALEO credits）+ 找零，订单参数只在链上加密 record 中
2. **引擎提取解密**：前端只提交 `tx_id`；引擎从链上交易提取 record ciphertext，用 operator view key 解密出订单参数与托管资产
3. **链下撮合**：与集中式共享的订单簿/撮合核心（价格-时间优先），订单簿以加密视角隔离，对手方不可见明文
4. **链上结算**：引擎 shell out `leo execute settle <maker_ct> <taker_ct> <托管资产> <凭证> <price> <amount> --broadcast`——Order record 一次性消费、资产原子互换；带 3 次重试 + "already exists" 幂等判定 + 30s 后台重结算循环
5. **行情**：K线/深度/成交通过 WebSocket 实时广播

合约源码在 `contracts/leo/`（Leo 语言，`--no-local` 链上版本构建证明）。

## 订单簿

`internal/core/match/order_book.go` — 两棵红黑树（`gods/treeset`）+ `map[int64]*Order` O(1) 查找：

- **BuySet**：价格从高到低，同价 SeqId 从小到大（价格-时间优先）
- **SellSet**：价格从低到高，同价 SeqId 从小到大

## 撮合逻辑

`internal/core/match/matcher.go` — 支持限价单（Limit）、市价单（Market）、IOC、FOK、撤单（Cancel）、批量撤单（BatchCancel）、限价做市单（LimitMaker）。自成交预防（STP：CB/CO/CN/DC/AST 5 种模式），市价单多档熔断保护。

## 隐私层

| 链 | 方案 | 位置 |
|----|------|------|
| Anubis | ECIES (P256) + AES-256-GCM 混合加密、Note 承诺、View Key 解密、Nullifier 防双花、ZK 撮合证明（gnark PLONK 目标） | `internal/dex/privacy/` |
| Aleo | 原生隐私：链上加密 record + view key 解密 + `leo execute` 零知识结算（无需自研密码学） | `internal/dex/chain/aleo/privacy.go` `api_tx.go` |

## AI 策略引擎（Anubis 侧 Phase 3）

`internal/dex/ai/` — 链下独立进程，为交易决策和风控提供智能信号：

| 文件 | 功能 |
|------|------|
| `internal/dex/ai/engine.go` | 行情研判：盘口失衡度 + 深度偏向 + 资金流向 + 舆情 → 5 级信号（HOLD/BUY/SELL/STRONG_BUY/STRONG_SELL），含冷却和幌骗检测 |
| `internal/dex/ai/iceberg.go` | 冰山拆分：TWAP / VWAP / Adaptive 三种策略，随机抖动防探测，限速控制，进度回调 |
| `internal/dex/ai/risk.go` | 风控引擎：1-10x 杠杆监控，强平价格计算，四级风险评级（LOW/MEDIUM/HIGH/CRITICAL），自动减仓 + 强平事件记录 |

## 杠杆交易（Anubis 侧 Phase 4）

`internal/dex/chain/leverage.go` — Anubis LeverageManager SC 适配层，内聚 `internal/dex/ai` RiskEngine：

| 功能 | 说明 |
|------|------|
| 开仓 | 1-10x 杠杆，保证金加密后提交 SC + 本地 RiskEngine 登记 |
| 追加保证金 | 动态重算强平价格，链上追加 |
| 自动追保 | 风险 MEDIUM → MarginCall 通知（1 小时内追加 50% 保证金） |
| 自动减仓 | 风险 HIGH → 自动平掉 50% 仓位 |
| 自动强平 | 风险 CRITICAL → 执行强平（2.5% 罚金），记录 LiquidationEvent |
| 资金费率 | 每 8 小时更新，`CalculateFundingPayment` 计算应收/应付 |

## 目录结构

```
AnuBookDEX/
├── cmd/
│   ├── engine/            # DEX 模式入口（anubis/ + aleo/ 两个链后端）
│   └── exchange/          # 集中式模式入口
│
├── internal/
│   ├── core/              # 撮合引擎核心（三模式复用）
│   │   ├── match/         #   订单簿 + 撮合 + Order 类型
│   │   ├── l2quote/       #   K线 / Ticker / 成交明细
│   │   └── market/        #   深度聚合
│   │
│   ├── dex/               # DEX 模式专用
│   │   ├── chain/         #   ChainAdapter 接口 + anubis/ + aleo/（事件订阅/解密/结算）
│   │   ├── privacy/       #   Anubis 隐私层（加密/解密/ZK 证明/Nullifier/KYC）
│   │   ├── ai/            #   行情研判 / 冰山拆分 / 风控
│   │   ├── ws/            #   WebSocket Hub + Client（零依赖）
│   │   ├── rocksdb/       #   本地订单簿快照存储
│   │   ├── runner/        #   共享撮合主循环
│   │   └── auth/          #   HTTP/WS 认证中间件
│   │
│   ├── infra/             # 共享基础设施
│   │   ├── common/        #   日志 / 配置加载 / 工具函数
│   │   ├── config/        #   Viper 包装层
│   │   ├── scheduler/     #   快照 + 上报定时器
│   │   ├── statistics/    #   撮合统计
│   │   └── dogstatsd/     #   Datadog 指标（可选）
│   │
│   └── centralized/       # 集中式专用（puller/persistence/rabbitmq/redis/snapshotter/validate/assign）
│
├── conf/                  # 配置文件（config.yaml + config.example.yaml）
├── contracts/
│   ├── leo/               # Aleo Leo 合约（anubook_dex_p5.aleo 等）
│   └── abi/ bindings/     # Anubis ABI JSON + Go 绑定
├── web/                   # 前端（React + Vite + Tailwind + Shield 钱包）
├── docs/                  # 方案文档 + 架构图
│
├── go.mod / go.sum / vendor/  # Go 1.21，依赖已 vendor 化
├── makefile / Dockerfile / docker-compose.yml
├── README.md / CLAUDE.md / .gitignore
└── deploy/                # 部署脚本（Docker Compose）
```

## 实施进度

| 链路 | Phase | 内容 | 状态 |
|------|-------|------|------|
| Anubis | 1-4 | 基础迁移 / 隐私层 / AI 策略引擎 / 杠杆交易 | ✅ 完成 |
| Aleo（黑客松主线） | 1 | 双链引擎骨架（ChainAdapter 抽象、Leo 合约脚手架） | ✅ 完成 |
| Aleo | 2 | 链上托管下单 + tx_id 提取解密（view key） | ✅ 完成 |
| Aleo | 3 | 链下撮合 + 链上 settle 结算（幂等/重试/后台重结算） | ✅ 完成 |
| Aleo | 4 | 前端钱包下单（Shield 标准/隐私模式，ALEO/USDCX E2E 已跑通） | ✅ 完成 |
| 后续 | 5+ | 暗池 MPC / ZK-KYC / RocketSwap / NFT | 待实施 |

## 关键依赖

- `shopspring/decimal` — 高精度十进制（禁止 float64，除法精度 37）
- `emirpasic/gods` — 红黑树订单簿
- `json-iterator/go` — 高性能 JSON
- `spf13/viper` — 配置管理
- `go.uber.org/zap` + `file-rotatelogs` — 结构化日志（按小时轮转）
- Leo / `leo` CLI — Aleo 合约构建与结算广播（`contracts/leo/`）
- Shield 钱包（Aleo Wallet Standard）— 前端签名与链上托管下单
