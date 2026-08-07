# AnuBookDEX Assistant 服务设计文档

> 版本：v0.1　|　日期：2026-07-13　|　状态：设计稿

## 一、背景与定位

### 1.1 为什么要做 Assistant

AnuBookDEX 双模式运行中，**DEX 模式的本地数据底座非常薄弱**：

- DEX 模式**完全不依赖 MySQL / Redis / RabbitMQ**（已确认 `internal/dex/` 与 `cmd/engine/` 下无相关引用），数据流是「链上事件订阅 → 隐私解密 → 撮合 → 链上 ZK 结算 + WebSocket 广播」。
- 本地存储仅 [internal/dex/rocksdb/](../internal/dex/rocksdb/)，且名为 rocksdb 实为文件存储：[kline_store.go](../internal/dex/rocksdb/kline_store.go) 只存**最新一根** K线，`LoadRange` 是空实现（TODO）；[snapshot_store.go](../internal/dex/rocksdb/snapshot_store.go) 只存订单簿快照用于故障恢复。
- 链上链路当前为 STUB：[subscriber.go](../internal/dex/chain/subscriber.go) 的 `fetchOrders`、[settlement.go](../internal/dex/chain/settlement.go) 的 `submitToChain` 均未真正接入 Anubis Chain。

这导致两个缺口：

1. **历史行情不可查**——没有集中式模式下 MySQL 那样的历史库。
2. **知识不可对话**——产品规则、架构、订单类型散落在文档里，无法自然语言问答。

Assistant 服务即用于补齐这两个缺口，定位为**与 `cmd/engine` 平级的独立旁路进程 `cmd/assistant`**，提供两大能力：

| 能力 | 解决问题 | 状态 |
|---|---|---|
| **能力一：路径 B 旁路时序落盘** | 把 WS 广播的公开行情落盘到本地时序库，补齐历史查询 | 现在可做 |
| **能力二：层次一文档问答 RAG** | 基于项目文档做检索增强问答 | 现在可做 |

将来层次二（数据自然语言查询）与层次三（交易决策辅助）挂在同一进程上，本文档暂不展开。

### 1.2 设计原则

- **零侵入撮合主链路**：assistant 对 engine 的全部改动仅一项——在 [auth.go](../internal/dex/auth/auth.go) 增加一个内部 service token。撮合循环、结算、行情线程均不动。
- **复用项目基础设施**：配置走 `config.GetString`（禁直接调 viper）、日志走 `common.Info`、JSON 用 `json-iterator`，与现有约定一致。
- **轻量、少依赖**：契合 DEX 单节点部署形态（见 [deployment_analysis.md](deployment_analysis.md)），不引入额外中间件。
- **精度铁律**：所有金额/数量用 `decimal` 字符串存取，禁止 float64。

---

## 二、整体架构

### 2.1 进程与数据流

```
                    ┌─────────────────────────── cmd/engine (撮合主进程) ───────────────────────────┐
                    │  链上事件 → 解密 → 撮合 → 链上结算                                         │
                    │                        │                                                   │
                    │              WS Hub 广播公开行情                                             │
                    │   depth.{symbol} / kline.{symbol}.{interval} / trade.{symbol}              │
                    └──────────────────────────────┬──────────────────────────────────────────────┘
                                                   │ WS 订阅 (持 service token)
                                                   ▼
        ┌────────────────────────────── cmd/assistant (旁路进程) ──────────────────────────────┐
        │                                                                                       │
        │  ┌── 能力一：路径 B 时序落盘 ──┐    ┌── 能力二：层次一 文档问答 RAG ──┐                  │
        │  │ ingest (WS 订阅落盘)        │    │ ingest (docs 切分入库)          │                  │
        │  │ DuckDB: depth/kline/trade  │    │ DuckDB: doc_chunks              │                  │
        │  └─────────────┬───────────────┘    └─────────────┬──────────────────┘                  │
        │                │                                  │                                       │
        │                ▼                                  ▼                                       │
        │         retrieval (时序查询)              retrieval (向量检索 + LLM 生成)                │
        │                │                                  │                                       │
        │                └──────────────┬───────────────────┘                                       │
        │                               ▼                                                           │
        │                    HTTP 服务 (:9100)                                                       │
        │            /ask  /docs/reindex  /docs/stats  /health                                       │
        └───────────────────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 共享基础设施

三大能力面共用以下组件，避免重复造轮子：

| 组件 | 说明 |
|---|---|
| `cmd/assistant` 进程 | 单二进制，承载所有能力 |
| DuckDB | 单文件嵌入式库，路径 B 与层次一各用各的表，同库不同表 |
| LLM/Embedding Provider 接口 | `internal/assistant/llm/provider.go`，OpenAI 兼容协议通吃通义/DeepSeek/Moonshot |
| 配置 | [conf/config.yaml](../conf/config.yaml) 新增 `assistant` 段，同时承载两者配置 |
| 日志 / JSON | 复用 `common` / `json-iterator` |

### 2.3 与 engine 的边界

| 接触面 | 方向 | 说明 |
|---|---|---|
| WebSocket 端口 | engine → assistant | assistant 作为 WS 客户端订阅行情 |
| auth service token | assistant → engine | 路径 B 唯一对 engine 的改动：加一个内部 token 白名单条目 |
| 文档文件 | assistant ← 文件系统 | 层次一直读 `docs/` 与 `CLAUDE.md`，engine 不参与 |

**零侵入证明**：除 auth token 一项外，assistant 崩溃/重启不影响撮合；engine 升级不依赖 assistant。

---

## 三、技术选型与决策记录

### 3.1 存储选型：DuckDB

| 候选 | 结论 | 理由 |
|---|---|---|
| **DuckDB（采用）** | ✅ | 列式存储，时序聚合查询快；嵌入式单文件无运维；原生 SQL 便于将来 Text-to-SQL；多进程只读挂载 |
| 现有文件存储（kline_store.go） | ❌ | 覆盖写、无范围查询、无聚合能力，设计上不适合历史数据 |
| SQLite + WAL | 备选 | 更通用更稳，但时序聚合性能弱于 DuckDB；团队不熟 DuckDB 时退此 |

### 3.2 LLM/Embedding 框架：Go 原生 + Provider 接口（不用 LangChain）

**决策：层次一不使用 LangChain，用 Go 原生轻量实现。**

核心矛盾在于项目是纯 Go 单语言工程，而 LangChain 主力在 Python（Go 侧 LangChainGo 成熟度不足）。三个备选方案对本项目的权衡：

| 方案 | 利 | 弊 |
|---|---|---|
| A. Python + LangChain 重写 assistant | 全生态（切分器/向量库/reranker/agent 现成） | 技术栈割裂；config/log/DuckDB 得重搭；DEX 部署多一套 Python runtime；与路径 B 的 Go 进程争 DuckDB 写权限 |
| B. Go + LangChainGo | 技术栈统一、单二进制 | 功能不全，切分/reranker 要自己补；本质只是 LLM 客户端封装，价值有限 |
| C. Go 网关 + Python RAG 微服务 | 各取所长 | 两进程、跨语言接口、DuckDB 读写协调；对层次一量级过度架构 |

**层次一全部逻辑 = markdown 切分 → embedding → 全量 cosine → LLM 拼 prompt → 返回，Go 原生约 300–500 行可完成**，引入 LangChain 是大炮打蚊子，省下的开发时间不够补运维成本。

但务必把 LLM/embedding 抽象为 `Provider` 接口：

```
internal/assistant/llm/
  provider.go        // interface { Embed(text) ([]float32); Chat(prompt) (string) }
  openai_compat.go   // OpenAI 兼容实现，一套通吃通义/DeepSeek/Moonshot
```

**分水岭**：真正值得为 LangChain 付运维代价的是**层次二（Text-to-SQL + 验证）和层次三（agent 工具调用）**。届时评估方案 C（Go 网关 + Python LangChain 微服务）。Provider 接口保证「现在 Go 原生、将来 LangChain 微服务」是平滑演进而非推倒重来。

### 3.3 向量检索：全量 brute-force cosine

**不需要专业向量库。** 量化论证：

- 语料总量约 250KB markdown，切完预估 800–1500 chunk。
- embedding 1536 维：1500 × 1536 × 4B ≈ **9MB**，全量驻内存无压力。
- 全量 cosine：1500 × 1536 ≈ 230 万次乘加，Go 单核 **< 5ms**。
- 涨到 10000 chunk（~60MB 内存）仍 < 50ms。

结论：**DuckDB 只做 embedding 持久化，启动时全量加载到内存 `[][]float32`，检索在内存算**。等语料破 5 万 chunk 再考虑换 ANN（Qdrant）。

---

## 四、能力一：路径 B 旁路时序落盘

### 4.1 目标与边界

**目标**：为 DEX 模式提供本地可查的历史行情库，替代集中式模式下 MySQL 的角色，并补齐 [kline_store.go:101](../internal/dex/rocksdb/kline_store.go#L101) `LoadRange` 空实现的痛点。

| 该做 | 不做 |
|---|---|
| 只落 WS 广播的公开聚合行情（depth/kline/trade） | 不落 `MatchResult` 私域明细（含 UserAddress/Taker/Maker，走路径 A 链上回查） |
| 结构化时序存储 + 范围查询 | 不碰向量库（向量是层次一的事） |
| 作为 WS 客户端旁路订阅 | 不改 engine 主链路 |

### 4.2 数据源与订阅方式

assistant 作为普通 WS 客户端连接 engine 的 WS 端口（[hub.go HandleWebSocket](../internal/dex/ws/hub.go#L164)）。订阅频道（频道格式来自 [hub.go](../internal/dex/ws/hub.go#L94-L131)）：

| 频道 | 格式 | 数据结构 | 来源 | 落盘表 |
|---|---|---|---|---|
| 深度 | `depth.{symbol}` | [QuoteDepths](../internal/core/market/depth.go#L19) | [BuildAndReportDepth](../internal/core/market/depth.go#L33) | `depth_snapshot` |
| K线 | `kline.{symbol}.{interval}` | [QuoteKline](../internal/core/l2quote/kline.go#L15) | klineUpdater | `kline_history` |
| 成交 | `trade.{symbol}` | [TradeDetail](../internal/core/l2quote/trade.go#L21) | trade goroutine | `trade_history` |

**auth**：assistant 持内部 service token，经 [auth.ValidateToken](../internal/dex/ws/hub.go#L170) 校验。需在 [auth.go](../internal/dex/auth/auth.go) 增加一个内部服务 token 白名单条目（路径 B 唯一对 engine 的改动）。

**关键事实**：`MatchResult`（[matcher.go:107](../internal/core/match/matcher.go#L107)）**不在 WS 广播**，WS 只发 depth/kline/trade 三个公开行情。MatchResult 在 DEX 上链，且含私域字段——所以路径 B 天然拿不到也不会去拿，隐私边界因此清晰。

### 4.3 表结构设计

字段直接映射现有结构体，避免反序列化损耗。所有金额/数量用 `VARCHAR` 存 `decimal.String()`，查询时还原（精度铁律）。

```sql
-- 深度快照（高频，需采样，见 4.4）
CREATE TABLE depth_snapshot (
    symbol      VARCHAR,
    interval    VARCHAR,      -- 档位名，对应 QuoteDepths.Interval
    ts          BIGINT,       -- 毫秒
    seq_id      BIGINT,
    bids        JSON,         -- [[price,amount],...]
    asks        JSON,
    PRIMARY KEY(symbol, interval, ts)
);

-- K线历史（9 周期，补齐原 LoadRange TODO）
CREATE TABLE kline_history (
    symbol       VARCHAR,
    interval     VARCHAR,     -- 1min/5min/15min/30min/60min/4hour/1day/1week/1mon
    ts           BIGINT,       -- K线开盘时间
    seq_id       BIGINT,
    open         VARCHAR,
    close        VARCHAR,
    high         VARCHAR,
    low          VARCHAR,
    vol          VARCHAR,
    turnover     VARCHAR,
    count        BIGINT,
    PRIMARY KEY(symbol, interval, ts)   -- 同一根多次广播 upsert 取最终值
);

-- 成交明细（公开，不含用户身份）
CREATE TABLE trade_history (
    symbol       VARCHAR,
    ts           BIGINT,
    trade_id     BIGINT,
    price        VARCHAR,
    vol          VARCHAR,
    direction    VARCHAR,      -- buy/sell
    PRIMARY KEY(symbol, trade_id)
);
```

按时间分区（DuckDB partition by 或按月分表），便于 TTL 清理。

### 4.4 写入策略

| 数据 | 频率特征 | 策略 |
|---|---|---|
| depth | 极高频（100ms 级，有撮合就推） | **采样落盘**：每秒最多 1 条快照 + 价格跳变超阈值时加落一条。全量落盘会写爆磁盘 |
| kline | 每根 K线持续更新 | `UPSERT`，以 `(symbol, interval, ts)` 为主键，收盘后取最终值 |
| trade | 每笔成交一条 | 全量落盘，`INSERT OR IGNORE` 幂等（trade_id 去重） |

批量写入：内存攒批（500 条或 500ms 刷一次）单事务提交。断点续传：记录每频道已落盘的最大 `seq_id`，重启续传（类似 [snapshot_store.go](../internal/dex/rocksdb/snapshot_store.go) 的 FromId 思路）。

### 4.5 读取接口设计

这一层就是把 [kline_store.go:101](../internal/dex/rocksdb/kline_store.go#L101) 的 `LoadRange` 真正实现并扩展，给 RAG 上层用，返回现有结构体（零认知成本）：

```
// 时序查询
LoadKlineRange(symbol, interval, from, to) -> []QuoteKline
LoadTradeRange(symbol, from, to) -> []TradeDetail
LoadDepthAt(symbol, ts) -> QuoteDepths

// 聚合查询（层次二核心，Text-to-SQL 直接生成）
AggregateVolume(symbol, from, to, groupBy)
AggregateOHLCV(symbol, interval, from, to)
CountTradesInRange(symbol, from, to, priceMin, priceMax)

// 实时上下文（不走 DB，内存最近 N 分钟）
LatestDepth(symbol) / LatestIndicators(symbol)
```

### 4.6 数据保留与隐私

**TTL**：按月分区，depth 保留 7 天、kline 全量保留、trade 保留 90 天（可配），超期直接 drop 分区。

**隐私边界（关键）**：

- 路径 B 只落公开行情，不含用户身份字段。`TradeDetail` 本身只有 price/vol/direction（[trade.go:12-19](../internal/core/l2quote/trade.go#L12-L19)），可安全进库供任意查询。
- `MatchResult` 含 `UserAddress`/`Taker`/`Maker`（[matcher.go:107-126](../internal/core/match/matcher.go#L107-L126)），且不在 WS 广播——路径 B 天然拿不到。这类私域明细走路径 A 链上回查 + ViewKey 授权。
- 路径 B 存储**不需要 ViewKey 授权**即可服务于公开行情问答，是 RAG 最先能落地的数据底座。

---

## 五、能力二：层次一 文档问答 RAG

### 5.1 目标与边界

**目标**：基于项目文档做检索增强问答，回答产品规则、架构、订单类型、运维问题。例："FOK 和 IOC 的区别"、"撮合引擎重启后从哪个 SeqId 恢复"、"DEX 模式行情为什么不走 RabbitMQ"。

| 该做 | 不做 |
|---|---|
| 文档检索 + LLM 生成 + 来源溯源 | 不接行情数据（那是路径 B / 层次二） |
| 语料筛选，只纳产品/技术文档 | 不全盘吞 docs（排除面试题/简历/外部榜单） |

### 5.2 语料范围与筛选

docs/ 并非所有文件都该进知识库：

| 文件 | 纳入 | 理由 |
|---|---|---|
| [AnuBook产品需求文档.md](AnuBook产品需求文档.md) | ✅ | 产品定义 |
| [Anubis_Network_开发者指南.md](Anubis_Network_开发者指南.md) | ✅ | 链/隐私体系 |
| [DEX改造方案.md](DEX改造方案.md) | ✅ | 架构决策 |
| [订单类型.md](订单类型.md) | ✅ | 交易规则核心 |
| [architecture_diagram.md](architecture_diagram.md) | ✅ | 架构 |
| [deployment_analysis.md](deployment_analysis.md) | ✅ | 运维部署 |
| [HyperLiquid & AnuBookDEX 技术栈对比.md](HyperLiquid%20&%20AnuBookDEX%20技术栈对比.md) | ✅ | 技术背景 |
| [CLAUDE.md](../CLAUDE.md) | ✅ | 架构约定/构建命令（运维高频问答源） |
| interview_questions.md | ❌ | 面试题库，非产品知识 |
| resume_interview_qa.md | ❌ | 个人简历材料 |
| 热门skills.md | ❌ | 外部榜单，与项目无关 |
| prototype.html / prototype-mobile.html | ❌ | HTML 原型，非文档 |

纳入/排除做成配置白名单 + 黑名单，未来新增文档默认不进，显式声明才进——避免噪音语料灌库。

### 5.3 文档处理与切分策略

文档均为中文 markdown、二级标题结构清晰（`## 一、...`），按标题切最自然。

**切分单元**：以 `##` 二级标题为边界切 chunk，保留完整标题路径作为上下文。

**chunk 元数据**：

```
chunk_id      : hash(source_file + heading_path + text)
source_file   : docs/订单类型.md
heading_path  : 订单类型.md > 三、配套：5 种自成交预防（STP）模式
text          : 该段正文
tokens        : 估算 token 数
```

**切分规则**：

1. 单 chunk 目标 300–500 token，超出按段落二次切分，保留 1–2 句重叠窗口防语义断裂。
2. **表格/对照表整段保留**（[订单类型.md](订单类型.md) 的 STP 对照表、订单类型对照表）——高频问答目标，不能切断。
3. 代码块整段保留（[CLAUDE.md](../CLAUDE.md) 的 make 命令、数据管道图）。
4. 标题路径注入 chunk 文本前缀，让 embedding 带上语义定位。

### 5.4 检索与生成流程

```
用户问题
  │
  ▼
embed(question) ──► 向量
  │
  ▼
内存全量 cosine ──► top-K (K=5) 候选 chunk
  │
  ▼
（可选）BM25 关键词混合检索 ──► 重排合并
  │      ▲
  │      └ 缩写词(FOK/IOC/STP/CB)向量易漏，BM25 补关键词命中
  ▼
拼 prompt：系统提示 + 检索片段(带来源) + 问题 + "必须基于片段回答，附来源"
  │
  ▼
LLM 生成
  │
  ▼
{ answer, sources: [{file, heading, chunk_id}] }
```

**Prompt 设计要点**：

- 系统提示约束"只能基于检索到的片段回答，不在片段中则说不知道"——防幻觉（金融规则问答不能编）。
- 每个检索片段带 `来源标记 [1][2]`，要求答案末尾附引用，可溯源到具体文件 + 标题。
- 保留原文表格/代码块格式原样喂给 LLM。

**BM25 混合（可选增强，P3 再做）**：交易术语缩写（FOK/IOC/STP/CB/CN/CO/DC/AST）向量检索易漏，加一层关键词匹配补充。中文小语料，BM25 权重 0.3 + 向量 0.7 融合即可。

### 5.5 表结构设计

```sql
CREATE TABLE doc_chunks (
    chunk_id     VARCHAR PRIMARY KEY,   -- 内容 hash
    source_file  VARCHAR,
    heading_path VARCHAR,                -- "订单类型.md > 三、STP 模式"
    text         TEXT,                    -- chunk 正文
    embedding    JSON,                   -- 1536 维 float 数组（持久化用）
    token_count  INT,
    file_mtime   BIGINT,                 -- 源文件修改时间，增量更新用
    content_hash VARCHAR,                 -- 内容 hash，判断是否需重建
    updated_at   BIGINT
);

CREATE INDEX idx_chunks_file ON doc_chunks(source_file);
```

**检索方式**：DuckDB 存 `embedding` 列，启动时全量加载到内存 `[][]float32`，检索在内存算 cosine（见 3.3 量化论证）。DuckDB 只做持久化。

### 5.6 索引构建与增量更新

**启动时**：

1. 扫描白名单文档，计算每个文件 `mtime` + `content_hash`。
2. 与库中 `file_mtime`/`content_hash` 对比：
   - 文件未变 → 跳过，复用已有 embedding。
   - 新增/变更 → 删除该文件旧 chunk，重新切分 + embedding + 写库。
3. 全量加载 `embedding` 到内存构建检索索引。

**增量**：只对变化文件重算 embedding，省 API 调用。

**手动重建**：`POST /docs/reindex` 强制全量重建（换 embedding 模型时用）。

### 5.7 服务接口

```
POST /ask
  body: { "question": "FOK 单和 IOC 单有什么区别", "top_k": 5 }
  resp: {
    "answer": "...",
    "sources": [
      { "file": "docs/订单类型.md", "heading": "一、交易类订单", "chunk_id": "..." },
      ...
    ]
  }

POST /docs/reindex          # 手动重建索引
GET  /docs/stats            # 语料统计：chunk 数、各文件 chunk 数、最近更新
GET  /health                # 复用现有健康检查模式
```

### 5.8 评测与迭代

**金标问题集**：从文档抽 25 个典型问题，人工标注期望答案 + 期望命中文档：

| 问题 | 期望命中 |
|---|---|
| FOK 单撮合失败会怎样 | 订单类型.md |
| DEX 模式为什么不用 RabbitMQ | DEX改造方案.md / CLAUDE.md |
| 自成交 CO 模式是什么 | 订单类型.md |
| 重启后从哪个 SeqId 恢复 | CLAUDE.md / architecture_diagram.md |
| ViewKey 怎么解密订单 | Anubis_Network_开发者指南.md |

**指标**：检索召回率（top-K 是否含期望文档）、答案准确性（人工/LLM-as-judge 打 1–5 分）、幻觉率（是否引用不存在的片段）。

**迭代旋钮**：top-K、chunk 大小、overlap、prompt 约束强度、BM25 权重。

---

## 六、配置设计

在 [conf/config.yaml](../conf/config.yaml) 新增 `assistant` 段（与现有 15 个顶层段平级），同时承载路径 B 与层次一配置：

```yaml
assistant:
  enabled: true
  http-port: 9100

  # ===== 路径 B：时序落盘 =====
  ws:
    endpoint: "ws://127.0.0.1:9000/ws"   # engine 的 WS 端口
    service-token: "${WS_SERVICE_TOKEN}"  # 内部服务 token（环境变量）
    channels:
      symbols: ["BTC/USDT", "ETH/USDT"]   # 订阅的交易对
      kline-intervals: ["1min","5min","15min","30min","60min","4hour","1day","1week","1mon"]
  storage:
    type: "duckdb"
    path: "./data/assistant.duckdb"
    depth-sample-interval-ms: 1000        # 深度采样间隔
    depth-ttl-days: 7
    trade-ttl-days: 90
    batch-size: 500
    batch-flush-ms: 500

  # ===== 层次一：文档问答 =====
  docs:
    dir: "./docs"
    whitelist:
      - "docs/订单类型.md"
      - "docs/AnuBook产品需求文档.md"
      - "docs/Anubis_Network_开发者指南.md"
      - "docs/DEX改造方案.md"
      - "docs/architecture_diagram.md"
      - "docs/deployment_analysis.md"
      - "docs/HyperLiquid & AnuBookDEX 技术栈对比.md"
      - "CLAUDE.md"

  # ===== 共享：LLM / Embedding =====
  llm:
    provider: "openai-compatible"
    base-url: "https://api.deepseek.com/v1"   # 可切换通义/Moonshot
    api-key: "${LLM_API_KEY}"                  # 环境变量，不入库
    model: "deepseek-chat"
  embedding:
    provider: "openai-compatible"
    base-url: "https://api.deepseek.com/v1"
    api-key: "${EMBED_API_KEY}"
    model: "text-embedding-v1"
    dim: 1536
  retrieval:
    top-k: 5
    chunk-target-tokens: 400
    chunk-overlap-sentences: 2
    bm25-enabled: false                        # P3 开启
```

**敏感凭证**：API key、service token 一律走环境变量（项目已有 `caarlos0/env` 依赖可复用），不落配置文件——金融项目敏感凭证不落盘。

---

## 七、服务接口汇总

| 方法 | 路径 | 能力 | 阶段 |
|---|---|---|---|
| POST | `/ask` | 文档问答 | 层次一 P2 |
| POST | `/docs/reindex` | 重建文档索引 | 层次一 P3 |
| GET | `/docs/stats` | 语料统计 | 层次一 P1 |
| GET | `/health` | 健康检查 | 通用 |
| GET | `/kline/range` | K线范围查询（补齐 LoadRange） | 路径 B P2 |
| GET | `/trade/range` | 成交范围查询 | 路径 B P2 |
| GET | `/depth/at` | 某时刻盘口快照 | 路径 B P2 |

HTTP 服务复用 [runner.StartHTTPServer](../internal/dex/runner/runner.go#L24) 的模式，独立端口 9100（与 engine 9000 区分）。

---

## 八、分阶段实施计划

路径 B 与层次一可并行推进，互不阻塞。

### 路径 B（时序落盘）

| 阶段 | 内容 | 验收 |
|---|---|---|
| P1 | assistant 连 WS、订阅全频道、不丢 trade（seq 连续） | trade 落盘无缺口 |
| P2 | kline_history 9 周期 `LoadKlineRange` 返回完整历史（补齐原 TODO） | 历史可查 |
| P3 | 聚合查询：任意时间段成交量/均价，与链上结算对账误差 < 容差 | 数据可信 |
| P4 | depth 采样策略验证：磁盘增长可控 | 1 元数据量下可承载 |

### 层次一（文档问答）

| 阶段 | 内容 | 验收 |
|---|---|---|
| P1 | 语料筛选 + markdown 切分 + DuckDB `doc_chunks` 表 + embedding API + 内存全量 cosine | `/docs/stats` 返回正确 chunk 数；25 题召回率达标 |
| P2 | LLM 接入 + `/ask` 接口 + 来源溯源 + prompt 防幻觉约束 | 25 题答案准确率 ≥ 4/5，零幻觉 |
| P3 | 增量索引（mtime+hash） + BM25 混合检索 + 评测调参 | 增量重建只重算变更文件；缩写词召回率提升 |

---

## 九、关键约束与边界

1. **零侵入撮合主链路**：禁止把 embedding/LLM 调用、向量检索、DB 写入放进 [matcher.go](../internal/core/match/matcher.go) 或 [runner.go](../internal/dex/runner/runner.go) 的热路径。assistant 是旁路进程。
2. **隐私分级**：公开行情（depth/kline/trade）可任意查询；`MatchResult` 私域明细必须经 ViewKey 授权，路径 B 不碰。
3. **精度铁律**：所有金额/数量 `decimal` 字符串存取，禁止 float64（遵守 CLAUDE.md）。
4. **凭证不落盘**：API key / service token 走环境变量。
5. **防幻觉**：层次一 LLM 必须基于检索片段回答，不在片段中则回答"不知道"，答案附来源引用。

---

## 十、演进方向（本文档暂不实施）

| 方向 | 前置条件 | 说明 |
|---|---|---|
| 路径 A：链上事件回查 | `fetchOrders`/`submitToChain` 脱 STUB | MatchResult 私域明细走链上 `eth_getLogs` + ViewKey 解密回查 |
| 层次二：数据自然语言查询 | 路径 B 数据就绪 | Text-to-SQL，须做 schema 约束 + SQL 白名单 + 结果验证层；此时评估引入 LangChain 微服务（方案 C） |
| 层次三：交易决策辅助 | 谨慎 | 只做异步辅助信号，不进同步撮合/风控路径，规则兜底；todo.md 规划的判别式 ML 优于生成式 RAG |

**与 [internal/dex/ai/todo.md](../internal/dex/ai/todo.md) 的关系**：todo.md 规划的 ML 路线（RL 冰山拆分、GBDT 风控评分、LSTM 价格预测）属于交易决策层的判别式模型，与 assistant 的 RAG 生成式应用是两个范式，互不替代。
