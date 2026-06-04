# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

基于 Go 1.21 编写的加密货币现货交易**撮合引擎**，支持**集中式**和 **DEX（Anubis Chain）** 双模式运行。
- **集中式模式**：从 MySQL 序列表中轮询订单，使用价格-时间优先的订单簿进行撮合，结果持久化到 MySQL，并通过 RabbitMQ 发布实时行情（深度、K线、成交明细、Ticker）。订单簿状态通过 gob 编码快照（本地磁盘 + S3）进行恢复。
- **DEX 模式**：Anubis Chain 事件订阅 → 隐私解密 → 撮合 → 链上 ZK 结算 + WebSocket 行情广播。

## 构建与运行

```bash
# macOS 本地构建（集中式模式，产出 exchange.bin）
make mac tag=<release-tag>

# macOS 本地构建（DEX 模式，产出 engine.bin）
make dex tag=<release-tag>

# Linux 构建（Docker 交叉编译）
make tag=<release-tag>

# 运行全部测试
make test

# 运行单个包的测试
go test -count=1 ./internal/core/match/

# 运行指定测试用例
go test -count=1 -run TestMatchLimit ./internal/core/match/
```

入口点位于 `cmd/exchange/main.go`（集中式）和 `cmd/engine/main.go`（DEX）。HTTP 健康检查默认监听 9000 端口。配置从 `./conf/config.yaml` 加载，可通过 `CONFIG_FILE` 环境变量覆盖。

## 架构

### 数据管道（每个交易对独立 goroutine，channel 驱动，无锁设计）

```
MySQL 序列表 -> puller -> orderSeqChan -> matcher    -> mrChan        -> l2quote（K线/Ticker/成交明细）
                                          -> perch         -> persistence（MySQL 批量写入）
                                          -> publishChan   -> rabbitmq（撮合结果广播）
                                          -> snapshotChan  -> snapshotter（gob 本地 + S3）
                                           depth tickers   -> market（订单簿深度推送）
```

每个交易对在 `startMatcher()` 中运行独立 goroutine，通过 `select` 监听：订单 channel、快照定时器、深度定时器、上报定时器。所有订单簿变更都在同一 goroutine 中完成（无需锁）。

### 订单簿（internal/core/match/order_book.go）

- **BuySet**：红黑树（gods/treeset），按价格从高到低排序，同价按 SeqId 从小到大（价格-时间优先）
- **SellSet**：红黑树，按价格从低到高排序，同价按 SeqId 从小到大
- **cache**：`map[int64]*Order`，OrderId O(1) 查找
- **FromId**：该交易对已处理的最后 SeqId

全局 `match.OrderBookMap`（`map[string]*OrderBook`）按交易对名称管理所有订单簿。

### 撮合逻辑（internal/core/match/matcher.go）

支持订单类型：限价单（Limit）、市价单（Market）、IOC、FOK、撤单（Cancel）、系统撤单、批量撤单（BatchCancel）、限价做市单（LimitMaker）。市价单有熔断保护（`CircuitRate`），多档成交价偏离首个成交价超过比例会触发熔断。自成交预防支持 5 种模式：AST（允许）、DC（减少并取消）、CO（取消旧）、CN（取消新）、CB（都取消）。

入口方法：`book.GenMatchResult(order)` 返回 `*MatchResultWithAskBid`（包含撮合结果 + 当前买一卖一）。

### 行情数据（internal/core/l2quote/）

接收 matcher 发送的 `MatchResultWithAskBid`，fan-out 到 11 个 goroutine 并行生成：9 种 K线周期（1min/5min/15min/30min/60min/4hour/1day/1week/1mon）、Ticker（24h OHLCV + 买一卖一）、成交明细。K线缓存到 Redis（list 结构），所有行情通过 RabbitMQ 发布。支持周期性的本地快照 + S3 备份恢复。

### 快照恢复（internal/centralized/snapshotter/）

启动流程：从本地磁盘加载最新 gob 编码的订单簿快照 -> 从快照的 FromId+1 开始重放 MySQL 订单 -> 与 `persistence` 中存储的历史结果对比验证（`internal/centralized/validate/`）。快照同时上传至 S3 作为备份。S3 恢复路径会下载两个快照（baseBook + checkBook），重放后对比验证。

### 持久化（internal/centralized/persistence/）

接收 matcher 撮合结果的 JSON 字节，按 `persistence.batch-size` 批量组装 INSERT 语句，写入 `t_exchange_match_result_<symbol>` 表（`insert ignore` 保证幂等）。采用 insertion-sort 保证 `f_id` 顺序。同时提供 `GetMatchResult(fromId, toId)` 用于恢复时的结果验证。

## 关键约定

- **精度计算**：所有价格/数量使用 `shopspring/decimal`（禁止 float64）。除法精度在 `init()` 中设置为 37
- **JSON 序列化**：性能敏感路径使用 `json-iterator/go`
- **配置访问**：通过 `config.GetString(key, default)` / `config.GetInt(key, default)` 等访问，底层是 viper，但禁止直接调用 viper
- **日志**：`common.Trace/Debug/Info/Warn/Error/Fatal`，底层是 zap + file-rotatelogs 按小时轮转
- **依赖管理**：已 vendor 化（`vendor/` 目录）

## 配置结构

`conf/config.yaml`：主要段包括 `symbols`（交易对列表）、`symbol-info`（精度、深度档位）、`rabbitmq`、`redis`、`mysql`、`persistence`、`snapshot`、`scheduler`、`market`、`l2quote`、`aws`、`log`。`config.validate()` 会加载 symbol-info 并为全部默认值设 fallback。
