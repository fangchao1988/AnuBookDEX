# AnuBookDEX 生产部署方案分析

## 系统特性摘要

| 特性 | DEX 模式（推荐生产） | 集中式模式 |
|------|---------------------|-----------|
| 入口 | `cmd/engine/main.go` | `cmd/exchange/main.go` |
| 二进制 | `CGO_ENABLED=0` 静态链接, ~20MB | 同 |
| 持久化 | RocksDB 嵌入式 | MySQL + Redis + S3 |
| 消息 | WebSocket 自研 Hub | RabbitMQ |
| 订单源 | Anubis Chain WebSocket 事件 | MySQL 序列表轮询 |
| 对外端口 | 9000 (HTTP + WS) | 9000 (HTTP) |
| 水平扩展 | 需按交易对分片 | 需按交易对分片 |
| 外部依赖 | Anubis Chain RPC 端点 | MySQL + Redis + RabbitMQ + S3 |

**核心约束**：撮合引擎每个交易对独立 goroutine + 内存订单簿 + RocksDB 本地快照。同一交易对不能由多个实例并行处理（会重复撮合）。

---

## 三种部署方案对比（以 DEX 模式为基准）

### 方案 A：单实例 EC2

```
                           ┌──────────────────────┐
  Internet ───────────────▶│  EC2 (t3.small)       │
                           │  Docker                │
  Anubis Chain ◀──────────▶│  engine.bin (2 pairs)  │
                           │  RocksDB (EBS 20GB)    │
                           │  EIP + Security Group  │
                           └──────────────────────┘
```

### 方案 B：ECS Fargate + EFS

```
                           ┌──────────────────────────────┐
  Internet ──▶ ALB ──────▶│  Fargate Service              │
                           │  ┌────────┐  ┌────────┐      │
  Anubis Chain ◀──────────│  │Task:2  │  │Task:2  │      │
                           │  │pairs A │  │pairs B │      │
                           │  └───┬────┘  └───┬────┘      │
                           │      └──────┬────┘           │
                           │        EFS (RocksDB)         │
                           └──────────────────────────────┘
```

### 方案 C：EC2 Auto Scaling + 交易对分片

```
                           ┌──────────────────────────────┐
  Internet ──▶ ALB ──────▶│  Auto Scaling Group           │
                           │  ┌──────────┐ ┌──────────┐   │
  Anubis Chain ◀──────────│  │EC2:      │ │EC2:      │   │
                           │  │ETH_USDT  │ │BTC_USDT  │   │
                           │  │RocksDB   │ │RocksDB   │   │
                           │  │(EBS 20G) │ │(EBS 20G) │   │
                           │  └──────────┘ └──────────┘   │
                           └──────────────────────────────┘
```

### 对比表

| 维度 | 方案 A: EC2 单实例 | 方案 B: ECS Fargate + EFS | 方案 C: EC2 ASG + 分片 |
|------|-------------------|--------------------------|----------------------|
| **月成本估算** | **$15-20** (t3.small ~$12 + 20GB EBS ~$3 + EIP ~$3) | **$37-55** (2×Fargate 0.25vCPU/0.5GB ~$24 + EFS ~$8 + ALB ~$18) | **$45-80** (2×t3.small ~$24 + ALB ~$18 + 2×EBS ~$6 + NAT ~$32) |
| **弹性扩缩** | ❌ 无，手动升级实例规格 | ✅ 自动扩缩 Task 数量（需预定义分片） | ✅ ASG 自动替换故障实例，增减实例需更新分片配置 |
| **运维复杂度** | ⭐ 最低：docker compose up | ⭐⭐⭐ 中：需管理 Task Definition + EFS 挂载 + ALB 配置 | ⭐⭐⭐⭐ 高：CloudInit 启动脚本 + 分片注册 + 健康检查协调 |
| **冷启动时间** | ~30s（EC2 启动 + Docker pull + 二进制启动） | ~60s（Fargate provision + 镜像拉取 + EFS mount） | ~45s（EC2 启动 + 用户数据脚本 + 分片发现） |
| **RocksDB 性能** | ⭐⭐⭐ EBS gp3 本地挂载，低延迟 | ⭐⭐ EFS 网络文件系统，延迟较高，不适合高频随机写 | ⭐⭐⭐ EBS gp3，与方案 A 相同 |
| **WebSocket 长连接** | ✅ 直接 TCP，无中间层 | ⚠️ ALB 需 sticky session + 超时配置 | ✅ ALB + sticky session |
| **故障恢复** | ❌ 手动重启，RocksDB 从 EBS 快照恢复 | ✅ Fargate 自动替换，EFS 数据保留 | ✅ ASG 自动替换 + EBS 快照恢复 |
| **适合的交易对规模** | 2-8 对（单进程内 goroutine） | 4-20 对（多 Task 分片，受 EFS IOPS 限制） | 10-100+ 对（按对分配 EC2，可水平扩展） |
| **适合的日交易量** | < 50 万笔 | 50-300 万笔 | 300-5000 万笔 |
| **安全组复杂度** | 低：仅开放 9000 + 22 | 中：ALB SG + Fargate SG | 中：ALB SG + 实例 SG |

---

## 推荐方案：方案 C（EC2 Auto Scaling + 交易对分片）

**选择理由**：

1. **成本适中**：$45-80/月，远低于 EKS（$100+/月），且无 NAT Gateway 可降至 $25-40/月
2. **RocksDB 性能最优**：EBS 本地挂载比 EFS 延迟低 10-50 倍，对撮合快照写入至关重要
3. **可线性扩展**：新增交易对 = 新增 EC2 实例，每实例独立 RocksDB，无状态共享竞争
4. **故障隔离**：单实例故障只影响分配的交易对，其余正常运行
5. **WebSocket 友好**：ALB 原生支持 WebSocket sticky session，无需额外配置
6. **符合撮合引擎架构**：同一交易对在单 goroutine 内无锁撮合的模型天然匹配单实例单对设计

### 架构演进路径

```
Phase 1 (MVP):     1×t3.small 跑 2-4 交易对           = $15/月
Phase 2 (增长):     2×t3.medium 跑 8-20 交易对          = $60/月
Phase 3 (规模):     N×t3.large 按对分片 + ALB           = $150+/月
```

每次扩展只需：启动新实例 → 配置该实例负责的交易对列表 → 加入 Target Group。

---

## 扩展性分析：日交易量 10 万 → 1000 万笔

### 瓶颈分解

当前架构每个交易对在单 goroutine 中完成撮合，核心限制是**单核 CPU 吞吐量**：

| 指标 | 10 万笔/天 (~1.2 TPS) | 1000 万笔/天 (~116 TPS) |
|------|----------------------|------------------------|
| 撮合延迟 | < 1ms | < 1ms（纯内存计算，未变） |
| 每对 CPU 使用 | < 5% | 30-50%（取决于订单复杂度） |
| K 线计算 | 无压力 | fan-out 11 goroutine，可能瓶颈 |
| WebSocket 扇出 | < 100 连接 | 1000+ 连接，Hub 需分片 |
| 链上结算批次 | 1-2 批/分钟 | 10-20 批/分钟，Gas 费用线性增长 |
| RocksDB 快照写入 | 60s/次 | 需调整为 30s/次或增量快照 |

### 需调整的架构层

#### 1. 撮合层：交易对拆分
```
10 万笔/天                     1000 万笔/天
┌─────────────┐              ┌──────────────────────────┐
│ EC2 × 1     │              │ EC2 × 4-8 (per pair)     │
│ ETH_USDT    │    ────▶     │ ├─ t3.large: ETH_USDT    │
│ BTC_USDT    │              │ ├─ t3.large: BTC_USDT    │
└─────────────┘              │ ├─ t3.large: SOL_USDT    │
                             │ └─ t3.medium: 其他低量对  │
                             └──────────────────────────┘
```
- **高流量交易对独立实例**（t3.large, 2 vCPU / 8 GB），多核支撑撮合 + K 线 + WebSocket
- **低流量交易对合并实例**（t3.medium），节省成本

#### 2. 行情层：WebSocket Hub 水平扩展

当前 `ws.Hub` 是单进程内存广播，10 万连接后会出现瓶颈：

- 引入 **Redis Pub/Sub** 作为 WebSocket 实例间总线：实例 A 撮合产出行情 → Redis Publish → 所有 WebSocket 实例 Subscribe → 扇出到各自客户端
- 或者按 channel 分片：`depth.*` 路由到实例 A，`kline.*` 路由到实例 B

```
[撮合实例]──▶ Redis Pub/Sub ──▶ [WS实例-0] ──▶ 客户端0-499
                              ├─ [WS实例-1] ──▶ 客户端500-999
                              └─ [WS实例-2] ──▶ 客户端1000+
```

#### 3. 结算层：批量优化 & Gas 管理

1000 万笔/天的链上结算（假设 50% 产生成交）：
- 当前 100 笔/批 → 需要 **5 万批次/天** = **~35 批/分钟**
- 优化：增大 `settlement-batch-size` 到 500+
- 进一步优化：使用 **ZK Rollup 聚合证明**（一次链上交易验证 1000+ 笔撮合结果）

#### 4. RocksDB 快照：增量 + WAL

- 当前每次全量快照（序列化整个订单簿），交易量提升后快照频率增加
- 改为 **增量 WAL 日志** + 定期全量快照（类似 LSM-Tree 的 compaction）
- 或将订单簿状态同步到 **Redis (AOF)** 作为热备份

#### 5. 监控 & 可观测性

- 引入 **Prometheus + Grafana** 替换纯 CloudWatch 日志
- 关键指标：`matcher_latency_p99`、`order_channel_depth`、`ws_connections_active`、`settlement_batch_latency`

### 调整汇总表

| 日交易量 | 实例数 | 规格 | 基础设施变更 |
|---------|-------|------|------------|
| 10 万 | 1-2 | t3.small | MVP，无 |
| 100 万 | 2-4 | t3.medium | 引入 Redis Pub/Sub for WS |
| 500 万 | 4-8 | t3.large | 高流量对独立实例 + batch-size 500 |
| 1000 万 | 8-16 | t3.xlarge 混合 | ZK Rollup 聚合证明 + Prometheus 监控 + Redis AOF 热备 |
