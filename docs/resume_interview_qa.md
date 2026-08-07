# AnuBookDEX 简历面试题库 — 核心职责深挖

> 本文档完全围绕简历中的 6 条"核心职责与成果"组织，每题对应一条职责，按**难度递增**排列（初级 → 中级 → 高级 → 资深/陷阱）。
>
> ⚠️ **诚实说明**：当前代码库处于 MVP 阶段。部分简历描述的功能（WebSocket 订阅、链上交易提交、gnark PLONK 证明、RocketSwap 路由等）在 Go 侧为 stub 占位实现，Solidity 侧部分函数（如实际 token 转账、RocketSwap swap）为注释掉的代码。参考答案中会明确标记【已实现】【MVP 占位】【路线图】——**面试中主动承认 MVP 状态并讲清楚演进路线，比硬撑"全部生产就绪"得分更高。**
>
> 代码位置引用格式：`file.go:line`

---

## 目录

- [一、撮合引擎（channel 无锁并发 + 7 种订单 + 5 种 STP + 熔断）](#sec-1)
  - [Q1【初级】每个交易对一个 goroutine，为什么不用互斥锁保护订单簿？](#q1)
  - [Q2【中级】5 种 STP（自成交预防）模式里，DC 和 CB 的本质区别是什么？做市商在什么场景下会选 DC 而非 CB？](#q2)
  - [Q3【中级】市价单熔断（CircuitRate）的两个分支，对于卖单来说 `≥ 上界` 那个分支会触发吗？](#q3)
  - [Q4【高级】你的 channel 管道里，如果撮合速度跟不上订单生产速度，会发生什么？你怎么监控？怎么降级？](#q4)
  - [Q5【资深/陷阱】你用了 `shopspring/decimal` 并设置 `DivisionPrecision=37`。为什么 37？一次撮合里最复杂的精度场景是什么？](#q5)
- [二、链上集成（subscriber.go + settlement.go 闭环）](#sec-2)
  - [Q6【初级】你的 subscriber.go 当前是轮询（polling）还是 WebSocket 订阅？为什么？](#q6)
  - [Q7【中级】如果 subscriber 断连 10 分钟，期间产生了 1000 个订单，重连后如何保证不重不丢？](#q7)
  - [Q8【中级】settlement.go 的批量提交流程是怎样的？批量为什么需要两个触发条件？](#q8)
  - [Q9【高级/陷阱】你有 3 个 submitWorker 并发提交交易，但没有 Nonce 管理——会出什么问题？怎么设计？](#q9)
  - [Q10【资深】ZK 证明生成很慢（比如 PLONK 一批 100 笔需要 5 秒），而撮合是每毫秒级别的，如何解耦？](#q10)
- [三、隐私层（加密提交 → 解密 → Nullifier → ZK → 链上结算）](#sec-3)
  - [Q11【初级】ECIES + AES-256-GCM 混合加密的流程是什么？为什么不直接用 ECIES 或直接用 AES？](#q11)
  - [Q12【中级】Nullifier 为什么要链下和链上两层都检查？](#q12)
  - [Q13【高级】View Key 在引擎手里，引擎理论上能看到所有订单——怎么防止引擎作恶或被攻破？这是隐私 DEX 的核心信任问题。](#q13)
  - [Q14【资深/陷阱】你在 zk_prover.go 里注释掉了 gnark 电路代码。如果让你真的写 PLONK 电路证明"我正确撮合了这批订单"，电路的公开输入和私有输入分别是什么？约束有哪些？](#q14)
- [四、智能合约（7 个 Solidity 合约设计）](#sec-4)
  - [Q15【初级】为什么拆成 7 个合约，而不是一个大的 DEX 合约？](#q15)
  - [Q16【中级】Settlement.sol 里 `submitBatch` 为什么先 ZK 验证、再 Nullifier 检查、最后转账？顺序能不能换？](#q16)
  - [Q17【中级】LeverageManager 的 `liquidate()` 是 permissionless（任何人都能调用）——为什么？这样做不担心抢跑吗？](#q17)
  - [Q18【高级/陷阱】LPMiningRewards 用了 MasterChef 式 `rewardDebt` 算法，原理是什么？你代码里有什么 bug？](#q18)
  - [Q19【高级】ZKKYC 的分级隐私模型，L0/L1/L2/L3 用户分别能做什么？ZK-KYC 比传统 KYC 好在哪里？](#q19)
  - [Q20【资深】DarkPoolRouter 为什么需要 MPC？如果单纯用加密订单不就行了？MPC 节点合谋怎么办？](#q20)
- [五、AI 策略引擎（行情研判 + 冰山拆分 + 风控）](#sec-5)
  - [Q21【初级】5 级交易信号是怎么算出来的？](#q21)
  - [Q22【中级】冰山订单的随机抖动（Jitter）为什么要 ±20%？防探测的原理是什么？](#q22)
  - [Q23【中级】4 级风控是怎么分级的？MarginCall → 减仓 → 强平的触发条件？](#q23)
  - [Q24【高级/陷阱】IcebergEngine 在 Tick() 里持锁调用 onSlice 回调——会有什么问题？](#q24)
  - [Q25【资深】幌骗（Spoofing）检测你用的是 cancels/orders > 80% 阈值——这个方法有什么缺陷？在生产级交易所你会怎么做？](#q25)
- [六、行情广播（自研 WebSocket Hub 替代 RabbitMQ）](#sec-6)
  - [Q26【初级】自研 WebSocket 而不是用 gorilla/websocket，你具体实现了什么？为什么这么做？](#q26)
  - [Q27【中级】Hub 的 broadcast channel 缓冲 10000，client.send 缓冲 256，慢消费者怎么处理？为什么不用阻塞？](#q27)
  - [Q28【中级】Hub 里 register/unregister/broadcast 用了三种不同的并发模式——为什么不统一？](#q28)
  - [Q29【高级/陷阱】你的 hub 广播在遍历 `h.subs[channel]` 时，如果某个 client 刚好 Unsubscribe 怎么办？会 panic 吗？](#q29)
  - [Q30【资深】你提到 fan-out 11 路 goroutine 生成 9 种 K 线 + Ticker + 成交明细，但主循环 `wg.Wait()` 等所有 goroutine 完成——这不是把 fan-out 变回串行瓶颈了吗？](#q30)
- [面试官评分维度（给候选人自评参考）](#rating)
- [面试加分项（主动说出来会加分）](#bonus)

---

<a id="sec-1"></a>

## 一、撮合引擎（channel 无锁并发 + 7 种订单 + 5 种 STP + 熔断）

<a id="q1"></a>

### Q1【初级】：每个交易对一个 goroutine，为什么不用互斥锁保护订单簿？

**参考答案**：

代码位置：`internal/dex/runner/runner.go` `StartMatcher` 主循环、`internal/core/match/order_book.go`。

核心原因有三个：
1. **订单簿是复杂复合结构**：红黑树（gods/treeset）+ `cache map[int64]*Order`，锁要覆盖整棵树的读写，高并发下锁粒度粗且极易出现死锁/锁遗忘
2. **价格-时间优先要求严格顺序**：同交易对的订单必须按 SeqId 严格顺序撮合，单 goroutine 的 `select` 天然是串行队列，无需额外同步原语
3. **channel 自带背压**：订单 channel 缓冲为 5000（`make(chan *match.Order, 5000)`），消费慢时生产者自动阻塞，不需要额外的限流令牌桶

每个交易对的 goroutine 栈约 4KB，100 个交易对仅 ~400KB，goroutine 不是瓶颈。

**追问**：那跨交易对呢？会不会相互影响？

不会。每个交易对的 goroutine、channel、orderBook 实例完全独立，互不共享内存。唯一共享的是日志/metrics 组件（本身线程安全）。

---

<a id="q2"></a>

### Q2【中级】：5 种 STP（自成交预防）模式里，DC 和 CB 的本质区别是什么？做市商在什么场景下会选 DC 而非 CB？

**参考答案**：

代码位置：`internal/core/match/matcher.go:817-913` `matchAmountBasedOrderSelfTrade`。

| 模式 | 全称 | 等量相遇时 | 新单 > 旧单 | 新单 < 旧单 |
|------|------|-----------|-----------|-----------|
| AST | Allow Self-Trade | 成交 | 成交 | 成交 |
| DC | Decrease and Cancel | **双方都取消**（与 CB 等价） | 新单减量，旧单取消 | 新单取消，旧单减量 |
| CO | Cancel Old | 取消旧单，新单保留 | 取消旧单 | 取消旧单 |
| CN | Cancel New | **仅取消新单**，旧单保留 | 取消新单 | 取消新单 |
| CB | Cancel Both | 双方都取消 | 双方都取消 | 双方都取消 |

做市商选 DC 的典型场景：**做市商希望维持挂单深度，但又不能自成交付手续费。**

> 举例：做市商在 100 USDT 挂买单 10 ETH（提供深度），同时用另一账户要卖出 3 ETH（平多仓）。
> - CB：两单全取消 → 深度消失，做市商要重新挂单（再付一次手续费）
> - DC：3 ETH 的卖单取消，买单减到 7 ETH → 深度仍在 7 ETH，避免自成交且无需重新挂单

**陷阱点**：`matchCashAmountBasedOrderSelfTrade`（市价买单，`matcher.go:942-1073`）的 DC 逻辑比 token 数量模式更复杂——因为市价买单的 `UnfilledAmount` 是**现金金额**，要先除以 `unitPrice` 转成 token 数量再比较，中间涉及 `Truncate(0)` 精度处理，容易出 bug。

---

<a id="q3"></a>

### Q3【中级】：市价单熔断（CircuitRate）的两个分支，对于卖单来说 `≥ 上界` 那个分支会触发吗？

**参考答案**：

代码位置：`internal/core/match/matcher.go:293-299`。

```go
((decimal.New(1,0).Sub(order.CircuitRate).Mul(*results[0].Price)).GreaterThanOrEqual(oppoOrder.Price) ||
 ((decimal.New(1,0).Add(order.CircuitRate).Mul(*results[0].Price)).LessThanOrEqual(oppoOrder.Price)))
```

**不会触发**。对于卖单吃买盘（买盘按价格从高到低排列）：
- 第一笔成交价 = 最优买价 = `results[0].Price`（最高买价）
- 后续档位价格 ≤ `results[0].Price`
- `results[0].Price * (1 + circuitRate)` 是比第一笔价更高的价格 → 永远 ≥ 后续任意买价 → 上界条件永不成立

代码用 OR 连接两个条件是为了**代码复用**——同一段逻辑同时服务买单和卖单，各自只命中一个分支。买单命中上界（涨价熔断），卖单命中下界（跌价熔断）。

**加分回答**：这其实是个可读性债点，可以用 `if order.Side == Buy { checkUpperBound } else { checkLowerBound }` 显式展开，消除死分支。但保留 OR 形式减少了重复代码。

---

<a id="q4"></a>

### Q4【高级】：你的 channel 管道里，如果撮合速度跟不上订单生产速度，会发生什么？你怎么监控？怎么降级？

**参考答案**：

当前实现：
- 订单 channel 缓冲 5000，满了之后 `ch <- order` 会阻塞，puller 卡住 → 这是**背压**，不会丢单
- 监控：`runner.go:103-104` 在 `reportTicker` 周期里上报 `len(ch)`，同时 `statistics.SetMatchTag(order.SeqId)` 记录当前处理到的 SeqId，对比 MySQL/链上最新 SeqId 可得"撮合延迟"
- `OrderBook` 上有 `FromId` 字段（`order_book.go:212` 通过 dogstatsd 上报），监控 `currentId` 曲线斜率即处理速率

**降级方案（这是资深考察点，需要自己设计）**：
1. **优先级 channel**：把 IOC/FOK 订单和普通订单分不同 channel，普通订单满时可以拒绝（返回 `OrderBookFull` 错误），但 IOC 必须优先处理
2. **非阻塞写入 + 丢弃**：用 `select { case ch<-order: default: reject }` 对非关键订单快速失败，避免阻塞 puller 导致 MySQL 连接堆积
3. **水平扩展**：按交易对分片到不同机器（已经在 `deploy/user-data.sh` 里有 shard index 设计）
4. **熔断**：监控订单 channel 占用率，超过 80% 持续 10 秒 → 暂时拒绝新挂单，只允许撤单

**追问**：撤单和新单在同一个 channel 里吗？如果在同一个 channel，撤单被阻塞会有什么后果？

如果同一个 channel，撮合慢时撤单排队 → 已经不存在的订单继续在对手盘等待成交 → 用户在爆仓边缘撤不掉单 → 资损。**生产级做法是撤单用更高优先级的独立 channel**，或者双 channel + `select` 优先处理撤单。

---

<a id="q5"></a>

### Q5【资深/陷阱】：你用了 `shopspring/decimal` 并设置 `DivisionPrecision=37`。为什么 37？一次撮合里最复杂的精度场景是什么？

**参考答案**：

代码位置：`cmd/engine/main.go:30`。

37 是 `shopspring/decimal` 库的默认精度上限，也是金融计算的事实标准（Java `BigDecimal` 默认 34 位、Swift `Decimal` 38 位）。它的意义：
- 2 个 18 位小数的 token 数量相乘 = 36 位小数 → 37 位足够
- 做 `amount * price / total` 这种连算时不会因中间精度截断出可被套利的偏差

最复杂的精度场景是**市价买单**（现金驱动）：
```go
// matcher.go:1107-1113
unitPrice := common.LOWPRECISION.Mul(oppoOrder.Price)            // 最小可成交单位的价格
if unitPrice.GreaterThan(order.UnfilledAmount) { ... }            // 买不起 1 单位 → PrecisionCanceled
orderAmount := order.UnfilledAmount.Div(unitPrice).Truncate(0).Mul(common.LOWPRECISION)
matchAmount := decimal.Min(orderAmount, oppoOrder.UnfilledAmount)
```

陷阱：`Truncate(0)` 向下取整。如果出现 `1.99999999999` 个单位（因为浮点式精度误差），会被截成 1 而不是 2，损失了购买力。项目用 `unitPrice > UnfilledAmount` 的 guard 保证至少能成交 1 个 `LOWPRECISION` 单位（0.01），避免死锁。

**生产级优化方向**：用整数最小单位（如 all amounts in `int64` 聪），避免 decimal 运算。但这要求每个 token 的小数位在配置里明确，且撮合逻辑按 token 维度泛化，实现复杂度更高。

---

<a id="sec-2"></a>

## 二、链上集成（subscriber.go + settlement.go 闭环）

<a id="q6"></a>

### Q6【初级】：你的 subscriber.go 当前是轮询（polling）还是 WebSocket 订阅？为什么？

**参考答案**：

代码位置：`internal/dex/chain/subscriber.go:93-130 eventLoop`。

**【MVP 占位，要诚实回答】** 当前实现是**200ms 间隔的 HTTP 轮询**（`time.NewTicker`），不是 WebSocket 订阅。原因：
1. Anubis Chain 的 WebSocket 端点（`chain.rpc-ws-endpoint`）在开发环境不稳定，MVP 阶段优先用简单可靠的轮询
2. 轮询代码路径少、容易重连（下一个 tick 自动重试），适合早期验证

**生产演进方向**：
- 切换为 `go-ethereum` 的 `ethclient.Client.SubscribeFilterLogs` 或 `bind.WatchOpts{Start: lastBlock}` 做 WebSocket 订阅
- 保留轮询作为降级：检测到 WebSocket 断连（ping/pong 超时）→ 切换轮询 → 指数退避重连 WebSocket
- 本地持久化 `lastProcessedBlock`（存 RocksDB），重启后从 `lastProcessedBlock - N`（N 为可回滚深度，比如 20 个区块以应对轻微重组）重放

---

<a id="q7"></a>

### Q7【中级】：如果 subscriber 断连 10 分钟，期间产生了 1000 个订单，重连后如何保证不重不丢？

**参考答案**：

这是链下事件驱动系统的经典 exactly-once 问题。三层保障：

1. **区块高度追踪 + 断点续拉**：
   - `lastBlock` 持久化到 RocksDB（【当前 MVP】只在内存，`subscriber.go:102`，未持久化）
   - 重连时从 `lastProcessedBlock - safeReorgDepth`（如 20 块）重新拉 `eth_getLogs`
2. **幂等处理**：
   - 订单通过 `Nullifier` 唯一标识。`privacy/nullifier.go:40 CheckAndMarkNullifier` 是内存中的 spent set，重复 nullifier 直接跳过
   - 链上 `0x0103` 预编译是最终幂等屏障——即使链下引擎重复提交，链上也会拒绝
3. **结果校验**：
   - 启动时对比链上 `OrderSubmitted` 事件计数与本地 FromId 的差值，如果差太大触发告警

**【诚实说明】** 当前 MVP 版本没有持久化 `lastBlock`，重启后会从 0 开始重放。Nullifier 也是内存 map，重启清空。这两个都是生产前必须补的点。

---

<a id="q8"></a>

### Q8【中级】：settlement.go 的批量提交流程是怎样的？批量为什么需要两个触发条件？

**参考答案**：

代码位置：`internal/dex/chain/settlement.go`。

提交流程：
1. `SubmitBatch(symbol, mrs)` 把撮合结果追加到 `pending[symbol]`（`settlement.go:71`）
2. 两个触发条件：
   - **量触发**：pending 长度 ≥ `batchSize`（默认 100）→ 立即 flush（`settlement.go:78-80`）
   - **时触发**：`batchTicker` 每 500ms 把所有非空 symbol flush 一次（`settlement.go:104-114`）
3. flush 后封装成 `settlementBatch`（原子递增 `batchSeq`）送 `submitCh`
4. 3 个 `submitWorker` goroutine 从 channel 消费，调用 `submitToChain` → ZK 证明生成 → 【MVP stub 返回假 hash】

为什么双触发：
- 量触发保证**高流量时吞吐量**（批量越大 Gas 摊销越好）
- 时触发保证**低流量时延迟**（如果交易清淡，单笔订单不能一直等，500ms 必须上链）
- 这是 L2 Rollup 的经典 batch 策略（Optimism/Arbitrum 都用类似的 size + timeout 双触发）

**配置参数权衡**：
- batch size 大 → Gas 便宜但单笔等待上链时间长 → 用户体验差
- batch size 小 → 用户快但手续费高
- 当前默认 100 笔/批、500ms 超时，对应 200 TPS 峰值（100/0.5s）

---

<a id="q9"></a>

### Q9【高级/陷阱】：你有 3 个 submitWorker 并发提交交易，但没有 Nonce 管理——会出什么问题？怎么设计？

**参考答案**：

**【诚实暴露问题】** 当前代码确实有问题——`settlement.go:130` 启动 3 个 worker，但 `submitToChain` 里没有 Nonce 管理（`settlement.go:162` 整个链上提交是 stub）。

如果直接并发提交，会发生：
1. 3 个 worker 同时向节点请求 `pendingNonce` → 拿到相同的 nonce N
2. 3 笔交易都用 nonce N 广播 → 只有 1 笔上链成功，其他 2 笔因 "nonce too low" 被节点丢弃
3. 更糟：如果节点 nonce 缓存不同步，可能出现 nonce 间隔（N, N+2 成功，N+1 丢失）导致后续所有交易卡住

**正确的 Nonce 管理**：
- **单 goroutine 分配器模式**：一个独立的 `nonceManager` goroutine 维护 `currentNonce`，worker 提交前通过 channel 申请 nonce，分配后才签名广播
- **链上确认回收**：每笔交易上链确认后再推进 nonce（不能只看本地递增，要处理 `nonce too low`/`replacement underpriced` 报错回滚）
- **备选简化方案**：把 worker 数改成 1，牺牲并发换简单——对于 DEX 结算来说 1 个 worker 已经足够（每批 Gas 大、确认慢，并发没有意义）

**追问**：如果交易因为 Gas 不足在 mempool 卡住，nonce 会一直停在那里，后续所有交易都阻塞。怎么办？

需要加 **Gas 价格管理**：每次提交前 `eth_gasPrice` + 溢价；超时未确认（比如 2 分钟）→ 用同一个 nonce 替换（替换交易要求 Gas Price 提高 ≥ 10% 以满足 Geth/Besu 的替换规则）；同时要有 stuck-transaction 检测和告警。

---

<a id="q10"></a>

### Q10【资深】：ZK 证明生成很慢（比如 PLONK 一批 100 笔需要 5 秒），而撮合是每毫秒级别的，如何解耦？

**参考答案**：

代码位置：`internal/dex/privacy/zk_prover.go:48 GenerateMatchProof`。

**【诚实说明】** 当前 MVP 用 SHA256 哈希代替 ZK 证明（`zk_prover.go:46-47` 明确注释），生成时间微秒级。接入 gnark 后需要处理证明生成的延迟，否则会阻塞撮合管道。

**正确的流水线架构**（多 goroutine + channel 分层）：

```
matcher → pendingSettlements 缓冲 ─┬─→ prover-1 ─┐
                                  ├─→ prover-2 ─┼─→ submitCh ─→ submitWorker ─→ 链
                                  └─→ prover-3 ─┘
```

关键设计点：
1. **证明生成独立 worker 池**：撮合 goroutine 只负责把 MR 放到 pending buffer（不阻塞），N 个 prover worker 并行生成证明
2. **状态快照**：生成证明时需要撮合后的状态根（StateRootBefore/After），必须在撮合完成时就快照下来（map 的 hash 或 Merkle root），不能等证明完成时再取——否则订单簿已变
3. **批次 ID 单调递增**：`batchSeq` 标识每批结果，证明和链上提交都绑定 batchSeq，避免乱序提交
4. **WAL（Write-Ahead Log）**：prover 崩溃后，重启从 WAL 恢复未提交的批次（【当前 MVP 缺失】`pendingSettlements` 只在内存）

**追问**：证明生成和提交是异步的，这期间又有新撮合产生，状态根已经变了——怎么保证 ZK 证明的 state root 和实际上链的 state root 一致？

每批撮合结束时**立即计算该批的 state root delta**，作为 proof 的 public input。提交时合约校验 `newRoot == currentRoot + delta`，否则 reject。这就是 StarkWare/zkSync 的 Validium/Rollup 经典设计——proof 里的 state transition 是自包含的，不依赖外部时序。

---

<a id="sec-3"></a>

## 三、隐私层（加密提交 → 解密 → Nullifier → ZK → 链上结算）

<a id="q11"></a>

### Q11【初级】：ECIES + AES-256-GCM 混合加密的流程是什么？为什么不直接用 ECIES 或直接用 AES？

**参考答案**：

代码位置：`internal/dex/privacy/encryption.go:31-77 Encrypt`。

混合加密流程：
1. 一次性生成临时 ECDH 密钥对（ephemeral key，P-256 曲线）
2. 用临时私钥和 View Key 公钥做 ECDH，得到共享密钥 → SHA256 派生对称密钥（32 字节）
3. 生成 12 字节随机 IV，用 AES-256-GCM 加密订单明文
4. 取共享密钥前 4 字节作为 **ViewTag**（快速过滤）
5. NoteCommitment = SHA256(nullifier ‖ ciphertext ‖ ephemeralPK)
6. 最终密文打包：ephemeralPK（33 字节压缩公钥）+ IV（12 字节）+ ViewTag（4 字节）+ ciphertext + tag

为什么不单独用：
- **不直接用 ECIES（ECIES 本身就用混合）**：ECDH 输出 32 字节，无法直接加密长消息（订单 JSON 几百字节），必须配合对称加密
- **不直接用 AES**：AES 是对称加密，需要把密钥传给引擎——但用户不能把订单密钥明文上链（所有人可见），必须用非对称封装

**ViewTag 的作用（`encryption.go:60`）**：每个区块可能有几百笔 Note，引擎无需每笔都做 ECDH 解密尝试（ECDH 是椭圆曲线点乘，慢），先用 4 字节 ViewTag 快速过滤——只有 ViewTag 匹配才做 ECDH。这是 Zcash/Tornado Cash 的经典优化。

---

<a id="q12"></a>

### Q12【中级】：Nullifier 为什么要链下和链上两层都检查？

**参考答案**：

代码位置：
- 链下：`internal/dex/privacy/nullifier.go:40 CheckAndMarkNullifier`（内存 map，`sync.RWMutex` 保护）
- 链上：`Settlement.sol:146-148` 调用 `0x0103` 预编译

**两层职责不同，不是冗余**：
- **链下检查（性能优化）**：避免对已处理过的 Nullifier 重复解密、撮合、生成 ZK 证明（这些都是 CPU 密集操作）。在解密后、送 matcher 之前就挡掉
- **链上预编译（最终权威）**：链下检查无法防恶意——如果用户把同一笔订单加密后双发到两个引擎实例，或者引擎本身作恶，链下缓存不共享；只有 `0x0103` 预编译在全节点层面做了 Nullifier 唯一性承诺，是结算时的最终防线

**纵深防御（defense-in-depth）**：类似 Web 应用里前端验证 + 后端验证都要做——前端挡粗心用户，后端挡恶意用户。

**追问**：【当前 MVP 的问题】链下 Nullifier 是内存 map，重启清空。怎么恢复？

启动时从 `Settlement.sol` 扫描历史 `BatchSettled` 事件，把所有已结算 Nullifier 重建到本地 map。或者定期把 usedNullifiers 快照到 RocksDB（类似订单簿快照机制）。

---

<a id="q13"></a>

### Q13【高级】：View Key 在引擎手里，引擎理论上能看到所有订单——怎么防止引擎作恶或被攻破？这是隐私 DEX 的核心信任问题。

**参考答案**：

这是个诚实面对的设计问题，不能绕开：

**当前 MVP 阶段的信任模型**（**必须主动承认**）：
- 引擎是**半可信（semi-honest）**的——假设它会正确执行撮合逻辑，不会主动偷钱或做恶，但 View Key 确实在引擎手里，引擎被攻破会泄漏订单
- 这是行业过渡方案——大部分隐私 DEX（包括早期 Aztec、Railgun 的 relayer）都有这个阶段

**多层缓解**：
1. **ZK 证明约束行为边界**：引擎即使能看到订单，也无法伪造撮合结果——因为每批结果必须配 ZK 证明（"我按价格-时间优先撮合"的算术化电路），`0x0100` 验证不通过会拒绝结算
2. **欺诈证明（路线图）**：如果引擎提交错误的状态根，任何人可以在挑战期内提交 fraud proof 罚没引擎保证金
3. **MPC 暗池（合约已就位 DarkPoolRouter.sol）**：大额订单不走主引擎，而是进入 MPC 网络，引擎本身看不到明文（【当前 MPC 协调器是单节点，路线图做多节点阈值签名】）
4. **TEE 可选方案**：用 Intel SGX/AWS Nitro Enclave 跑引擎，即使主机 root 也无法 dump 内存（但引入了硬件厂商信任）

**你要理解的 trade-off**：完全隐私（像 Penumbra 那样全节点 MPC 撮合）代价是性能（秒级延迟、低 TPS）；半可信链下引擎 + ZK 验证的方案性能好（毫秒级），但有一个最小化的信任点。AnuBookDEX 选后者，面向专业交易者（延迟敏感）。

---

<a id="q14"></a>

### Q14【资深/陷阱】：你在 zk_prover.go 里注释掉了 gnark 电路代码。如果让你真的写 PLONK 电路证明"我正确撮合了这批订单"，电路的公开输入和私有输入分别是什么？约束有哪些？

**参考答案**：

代码位置：`internal/dex/privacy/zk_prover.go:109-135`（注释掉的 `MatchCircuit` 骨架）。

**公开输入（public inputs，写入合约 storage，所有人可见）**：
- `StateRootBefore`：撮合前订单簿的 Merkle/RAM 根
- `StateRootAfter`：撮合后的根
- `BatchID`：批次号（防重放）
- `MatchResultHash`：撮合结果的 hash（成交价、量、买卖方 nullifier 列表的 commitment）

**私有输入（witness，只有引擎知道）**：
- 所有买单（明文 price/amount/orderId/nullifier）
- 所有卖单（明文 price/amount/orderId/nullifier）
- 撮合过程中每一笔成交的明细（matchId, makerOrderId, takerOrderId, price, amount）
- StateRootBefore 和 StateRootAfter 对应的 Merkle 路径

**核心约束**（这些是难点）：
1. **价格优先约束**：对每个 taker 订单，所有被成交的 maker 订单价格满足：买单 ≥ 卖价，且是最优价（没有更优的 maker 订单未成交）
2. **时间优先约束**：同价位 maker 按 SeqId 顺序成交（需要在电路里做 SeqId 排序验证，或用 Merkle tree in-order 位置证明）
3. **数量守恒约束**：`sum(makerSells) == sum(takerBuys)`（不含手续费）
4. **余额更新约束**：每个 maker/taker 的余额变化 = ± 成交金额 - 手续费
5. **Nullifier 唯一约束**：所有成交订单的 nullifier 集合与公开输入的 nullifier list 一一对应
6. **状态根更新约束**：用 batch 中的订单路径 + 撮合路径更新 Merkle 树，最终根 == StateRootAfter

**工程难点**：
- 排序（时间优先）和 Merkle 路径验证在电路里很昂贵（约束数爆炸）。实际做法是像 zkSync 那样用专门的 storage-proof 电路（hashchain 替代 Merkle）或者分批多次证明
- 电路里不能用浮点，所有价格用定点数（decimal 放大 10^18 变整数）
- 一批 100 笔的撮合证明，约束数估计在 500 万-2000 万，证明时间在 5-30 秒，需要 GPU 加速或专用 prover 集群

**加分回答**：【诚实】MVP 阶段我没有完整实现这个电路——电路设计是 ZK 工程师的专项工作。我目前实现的是 ZK 证明的**框架接口**（`GenerateMatchProof`/`VerifyMatchProof` 签名 + 公开输入/私有输入结构定义），MVP 用 SHA256 承诺做占位，后续和专门的 ZK 工程师协作补电路。

---

<a id="sec-4"></a>

## 四、智能合约（7 个 Solidity 合约设计）

<a id="q15"></a>

### Q15【初级】：为什么拆成 7 个合约，而不是一个大的 DEX 合约？

**参考答案**：

7 个合约的职责划分：

| 合约 | 职责 | 可升级性 |
|------|------|---------|
| `OrderBookRegistry.sol` | 交易对注册 + 加密订单入口 | 低（订单提交是核心协议） |
| `Settlement.sol` | 批量结算 + ZK 验证 | 中（验证逻辑可能升级） |
| `LeverageManager.sol` | 杠杆头寸 + 保证金 + 强平 | 中（风控参数迭代） |
| `DarkPoolRouter.sol` | 大额暗池订单协调 | 高（MPC 网络在演进） |
| `ZKKYC.sol` | 分级隐私 + ZK-KYC 验证 | 中（合规规则会变） |
| `LiquidityRouter.sol` | AnuBook ↔ RocketSwap AMM 路由 | 高（会接更多 AMM） |
| `LPMiningRewards.sol` | LP 质押挖矿 + 手续费分红 | 高（激励方案可调） |

拆分原因：
1. **关注点分离**：结算逻辑和 KYC 逻辑完全无关，放一起增加审计面
2. **Gas 优化**：一个合约太大部署 Gas 贵，且调用时的 bytecode lookup 开销大
3. **权限隔离**：不同合约的 owner 可以不同（比如 ZKKYC 可以由合规多签管，其他由治理多签管）
4. **独立升级**：【路线图】每个合约可以独立升级（UUPS proxy 模式），不会全部冻结

---

<a id="q16"></a>

### Q16【中级】：Settlement.sol 里 `submitBatch` 为什么先 ZK 验证、再 Nullifier 检查、最后转账？顺序能不能换？

**参考答案**：

代码位置：`Settlement.sol:126-209 submitBatch`。

顺序是 **Checks-Effects-Interactions** 模式：
1. **ZK 验证（staticcall 0x0100）**：先验证证明是合法的，这是"这批结果是引擎按规则算的"的根本保证。不修改任何状态（staticcall），失败直接 revert，gas 消耗最小
2. **Nullifier 检查（staticcall 0x0103）**：逐笔检查 nullifier 没被花过。在 ZK 验证之后才做——因为如果 ZK 证明无效，这一步白做（浪费 Gas）
3. **Effects**：标记 `settledNullifiers[n] = true`、更新 `currentStateRoot`、`batchSeq++`
4. **Interactions**：`_unshield()` 分发 token 和手续费（【MVP 是 emit 事件，真实 token 转账待接入】）

颠倒会出问题：
- 如果先标记 nullifier 再做 ZK 验证 → ZK 验证失败 revert 整个交易，所有状态回滚 → 看起来也安全，但消耗了更多 Gas（写 storage 后 revert 也要付 Gas）
- 如果先转账再验证 → **重入攻击风险**，且验证失败无法回滚真实转账（如果是 call 而非 sendValue）
- 当前顺序的另一个好处：0x0100 和 0x0103 都是 precompile，用 `staticcall` 天然防重入（静态上下文不允许状态修改）

---

<a id="q17"></a>

### Q17【中级】：LeverageManager 的 `liquidate()` 是 permissionless（任何人都能调用）——为什么？这样做不担心抢跑吗？

**参考答案**：

代码位置：`LeverageManager.sol:211-246 liquidate`。

这是**与 DeFi 清算标准一致的设计**（Compound/Aave/GMX 都这样）：
- 任何地址都能调用 `liquidate(account, symbol)`，触发强平
- 清算人获得 `liquidatorRewardBps` = 1.25% 的奖励（`LeverageManager.sol:80`）
- 因为有奖励，机器人（keeper）会主动监控链上价格、竞相触发强平 → 形成**竞争市场**，确保坏账被及时清算
- 如果清算权限只给 owner（中心化），owner 离线 → 坏账累积 → 协议破产

**抢跑 / MEV 不是 bug，是设计**：
- 在 Anubis Chain（不是 ETH L1），MEV 可能相对弱，但仍存在
- 标准缓解：清算奖励设计成与强平罚金成正比（`penalty=2.5%, reward=1.25%`），清算人竞争让奖励趋向于合理 Gas 成本
- **但当前代码有个 bug（要能自己发现说出来很加分）**：`liquidate` 计算了 `liquidatorReward` 但实际只 transfer 了这部分给 `msg.sender`，剩下 1.25%（penalty - reward）没有转给任何地址——资金卡在合约里（`LeverageManager.sol` 的强平逻辑）。生产环境应该把剩余部分转入保险基金。

---

<a id="q18"></a>

### Q18【高级/陷阱】：LPMiningRewards 用了 MasterChef 式 `rewardDebt` 算法，原理是什么？你代码里有什么 bug？

**参考答案**：

代码位置：`AnuBookDEX-contracts/contracts/LPMiningRewards.sol`。

**rewardDebt 算法原理**（Synthetix/MasterChef 经典算法）：
- 全局维护 `accRewardPerShare`（每单位 LP 累积的奖励数，精度 1e18）
- 每次奖励发放/用户存取时更新 `accRewardPerShare`
- 用户质押时记录 `s.rewardDebt = s.amount * accRewardPerShare / PRECISION`（用户"已结算"的基线）
- 用户提取奖励时：`pending = s.amount * accRewardPerShare / PRECISION - s.rewardDebt`（只领质押后新增的部分）
- 关键不变量：每次用户触发动作（stake/unstake/claim）前先 update 全局 accumulator

**⚠️ 我代码里的 bug（主动说出来展现 code review 能力）**：

`_updateAccumulators` 在周期结束后使用 `block.timestamp - periodFinish` 作为 elapsed 时间（`LPMiningRewards.sol:271-278`），而 `_pendingReward` 函数在计算用户 pending reward 时又单独加了一次"周期外奖励"——导致**双计**。如果用户在 `updateRewards` modifier 已经更新过 accumulator 后再调用 `claimReward`，`_pendingReward` 又追加一次，会多发奖励。

修复方案：要么 accumulator 处理完整个时间窗口（包括过期后），`_pendingReward` 不再单独加；要么 accumulator 只处理到 `periodFinish`，过期部分由 `_pendingReward` 补。两者不能同时做。

**另一个 bug**：`depositFees` 在 `totalStaked == 0` 时把 fee 存进 `pendingFeePool`，但 `pendingFeePool` 里的 fee 永远不会被分配（没有代码在后续 stake 时把它补到 `accFeePerShare`）→ 这部分 fee 永久卡在池里。

---

<a id="q19"></a>

### Q19【高级】：ZKKYC 的分级隐私模型，L0/L1/L2/L3 用户分别能做什么？ZK-KYC 比传统 KYC 好在哪里？

**参考答案**：

代码位置：
- Go 侧：`internal/dex/privacy/kyc.go:15-44 PrivacyTier` + `:92 ClassifyOrder`
- Solidity 侧：`ZKKYC.sol:67 thresholds`（`anonymousThreshold=0.1 ETH`, `pseudonymousThreshold=1 ETH`, `zkVerifiedThreshold=100 ETH`）

| 级别 | 额度 | 身份要求 |
|------|------|---------|
| L0 匿名 | < 0.1 ETH/单 | 无（地址即身份） |
| L1 半实名 | < 1 ETH/单 | 邮箱/手机（链下） |
| L2 ZK-KYC | < 100 ETH/单 | 第三方 KYC 机构签发 ZK 证明（不泄露身份信息） |
| L3 完全合规 | ≥ 100 ETH/单 | 完整 KYC（姓名、证件），审计日志可追溯 |

ZK-KYC 的核心价值：
- 传统 KYC：用户上传护照扫描件 → 交易所中心化存储 → 数据库被拖库（像 Binance 2019、FTX 都泄漏过）
- ZK-KYC：持牌 KYC 机构在验证用户身份后，签一个 ZK 证明："此人满足 L2 要求（有效证件 + 不在制裁名单）"。链上合约只验证证明是否成立，**永远看不到护照号、姓名、国籍**
- 密码学保证：证明无法伪造（基于 0x0100 预编译的 PLONK 验证），且 Nullifier 防止同一个 KYC 证明被反复使用

**【诚实说明】** 当前 MVP 的 `SanctionCheck` 是 stub（`privacy/kyc.go:142` 直接 return true），真实制裁名单需要接入 Chainalysis 或本地 OFAC 名单 Merkle tree。

---

<a id="q20"></a>

### Q20【资深】：DarkPoolRouter 为什么需要 MPC？如果单纯用加密订单不就行了？MPC 节点合谋怎么办？

**参考答案**：

代码位置：`DarkPoolRouter.sol`。

普通加密订单（主撮合引擎模式）的问题：
- 引擎持 View Key，能看到所有订单明文——**对大额订单（≥100k USDT）来说，即使引擎不主动作恶，被黑客攻陷或内部作恶的风险太高**
- 单笔 100k+ USDT 的订单一旦泄漏，会被抢跑/front-run，损失远超手续费

MPC（安全多方计算）解决的问题：
- 暗池订单拆分给 N 个 MPC 节点，每个节点只看到加密分片，单个节点无法解密完整订单
- N 个节点通过 MPC 协议（比如 SPDZ 或 Groth16 + 阈值解密）共同计算"是否有匹配的对手单"，计算过程中没有任何节点看到明文
- 匹配完成后只输出"成交/不成交"+成交结果，不泄漏订单细节

**MPC 合谋风险**：
- 阈值假设：N 个节点中最多 t 个作恶（比如 N=7, t=2，即 5 个诚实即可）
- 节点由不同机构/质押者运行，作恶会被罚没保证金（`mpcCoordinator` 可以 slash）
- **【诚实评估】** 当前 MVP 的 `DarkPoolRouter.sol` 是**单协调器模式**（`onlyCoordinator` 权限），真正的多节点 MPC 是路线图，需要先部署节点网络

---

<a id="sec-5"></a>

## 五、AI 策略引擎（行情研判 + 冰山拆分 + 风控）

<a id="q21"></a>

### Q21【初级】：5 级交易信号是怎么算出来的？

**参考答案**：

代码位置：`internal/dex/ai/engine.go:223-275 evaluateSignal`。

5 级信号：`StrongBuy / Buy / Hold / Sell / StrongSell`。

多因子加权评分（范围 [-1, 1]）：
1. **盘口失衡因子（权重 0.4）**：`ImbalanceRatio = (totalBidVol - totalAskVol) / (totalBidVol + totalAskVol)`。|ImbalanceRatio| ≤ 0.3 时此因子为 0（噪声过滤）；超过 0.3 才计入
2. **深度加权因子（权重 0.3）**：`DepthBias`，按档位距离加权（近档权重高，远档权重线性衰减）
3. **情绪因子（权重 0.3）**：外部 sentiment（[-1, 1]），可选（`SetSentiment` 注入）

阈值分桶：
- score > 0.50 → StrongBuy
- score > 0.15 → Buy
- score < -0.50 → StrongSell
- score < -0.15 → Sell
- 其他 → Hold

**防抖**：`signalCooldown = 10s`，信号变化后 10 秒内不重复触发，避免噪声导致信号抖动（`engine.go:230-234, 267`）。

---

<a id="q22"></a>

### Q22【中级】：冰山订单的随机抖动（Jitter）为什么要 ±20%？防探测的原理是什么？

**参考答案**：

代码位置：`internal/dex/ai/iceberg.go:82-90 defaults, :172-179 jitter logic`。

**冰山订单的目的**：把大额订单（比如 100 ETH）拆成小单（默认 1 ETH/单，每 30 秒一次），避免在订单簿上显露大额挂单被对手 front-run。

**如果固定大小、固定间隔，会被探测出来**：
- 对手方可以统计"每 30 秒精确出现一个 1 ETH 的买单" → 推断背后有冰山单 → 提前买盘推高价格
- 这在传统金融里叫 "pattern detection" 或 "sniffing algorithm"

**抖动设计**：
- **大小抖动**：`sliceAmt = baseSlice * (1 ± 20%)`，即默认 1 ETH 的单会在 0.8-1.2 ETH 之间随机
- **时间抖动**：【代码里目前没有独立时间 jitter，固定 SliceInterval=30s，这是可以改进的点】
- **末单合并**：`iceberg.go:182-184`，当剩余量 < sliceSize * 0.5 时一次性全部发出，避免最后一笔过小暴露行踪

**为什么 ±20%，不是 ±50% 或 ±5%**：
- 5% 太小：统计假设检验（KS 检验）仍能以高置信度识别出是固定算法下的单
- 50% 太大：违反了用户对执行节奏的预期，可能在某些价格区间错过成交
- 20% 是业界常用值（Bloomberg EMSX、Coinbase Institutional 都用这个量级）

**【诚实说明】**：当前代码三个策略枚举（TWAP/VWAP/Adaptive）在 `Tick()` 里走同一代码路径（`iceberg.go:145-216`），**TWAP/VWAP 没有差异化实现**。Adaptive 策略目前是"固定 5%"（`computeAdaptiveSliceSize`，`iceberg.go:239-253`），todo.md 明确标注为 MVP 占位，计划后续用 ML 根据盘口深度动态调整切片。

---

<a id="q23"></a>

### Q23【中级】：4 级风控是怎么分级的？MarginCall → 减仓 → 强平的触发条件？

**参考答案**：

代码位置：`internal/dex/ai/risk.go:229-265 assessRisk`。

以**距离强平价的百分比**为度量（long 仓位: `(markPrice - liqPrice) / markPrice`）：

| 距离 | 级别 | 动作 |
|------|------|------|
| > 10% | Low | 正常持仓 |
| 5% ~ 10% | Medium | **MarginCall**：通知用户追加保证金（`onMarginCall`，给 1 小时期限） |
| 1% ~ 5% | High | **Auto-Reduce**：自动减仓 50%（`AutoReducePct=0.5`）降低风险敞口 |
| < 1% | Critical | **Liquidation**：立即强平（`onLiquidation`），扣 2.5% 罚金 |

强平价公式（`risk.go:271-280 CalcLiquidationPrice`）：
- Long: `liqPrice = entryPrice * (1 - 1/leverage - maintenanceMargin)`
- Short: `liqPrice = entryPrice * (1 + 1/leverage - maintenanceMargin)`
- 例：10x long，maintenance=0.5% → `liqPrice = entry * (1 - 0.1 - 0.005) = entry * 0.895`，跌 10.5% 触发强平

**【诚实暴露缺陷 + 改进】**：
- **5 秒检查间隔**（`RiskCheckInterval=5s`）+ 从 5% 跳到 1% 没有缓冲：极端行情下价格 1 秒跳 4%，可能直接越过 Auto-Reduce 窗口到强平 → 改进：在 High 区间更密集检查（1 秒间隔）
- MarginCall 的 1 小时 deadline 当前只是存储（`leverage.go:201`），没有 sweep goroutine 检查过期 → 需要加定时器巡逻
- 没有**保险基金**机制：如果强平价格滑点导致穿仓（亏损超过保证金），谁来承担？生产级（像 GMX/FTX 旧版）需要保险基金 + ADL（自动减仓，按盈利排序吃掉盈利用户头寸）

---

<a id="q24"></a>

### Q24【高级/陷阱】：IcebergEngine 在 Tick() 里持锁调用 onSlice 回调——会有什么问题？

**参考答案**：

代码位置：`internal/dex/ai/iceberg.go:146 (e.mu.Lock)` 到 `:206 (e.onSlice(slice))` 再到 `:216 (e.mu.Unlock())`。

**严重问题：回调在锁内执行，存在死锁风险**：
- `onSlice` 回调会把切片订单送入撮合 channel → 撮合完成后可能回调 IcebergEngine 更新状态（比如订单成交后剩余量更新 `UpdateRemaining`）→ `UpdateRemaining` 也需要获取 `e.mu.Lock()`
- 同一 goroutine 不可重入锁（Go 的 `sync.Mutex` 不是可重入锁）→ **死锁**
- 即使不死锁，如果回调慢（比如撮合 channel 满了阻塞），锁一直持有，所有 SubmitIceberg/CancelOrder 都会卡住

**修复方案**：
1. 把需要回调的数据先拷贝到局部变量，释放锁后再调用回调
2. 或者用 channel 异步派发：`Tick` 把 slice 放到 channel，另一个 goroutine 消费 channel 调回调
3. 或者用 `sync.RWMutex` 把读操作（如 slice 计算）和写操作（状态更新）分离，回调在锁外

**类似的问题在 RiskEngine 里也存在**：`UpdateMarkPrice` 在持写锁时同步调用 `onMarginCall/onAutoReduce/onLiquidation` 回调（`risk.go:207-224`），而回调里可能再调用 `OpenPosition/ClosePosition`（也要锁）→ 同样的死锁风险。

---

<a id="q25"></a>

### Q25【资深】：幌骗（Spoofing）检测你用的是 cancels/orders > 80% 阈值——这个方法有什么缺陷？在生产级交易所你会怎么做？

**参考答案**：

代码位置：`internal/dex/ai/engine.go:279-291 DetectSpoofing`。

当前实现（MVP 占位，`todo.md` 也明确记录）：
```go
func (e *Engine) DetectSpoofing(recentCancels, recentOrders []int64) []int64 {
    if len(recentOrders) == 0 { return nil }
    if float64(len(recentCancels))/float64(len(recentOrders)) > 0.80 {
        return recentCancels
    }
    return nil
}
```

**缺陷**（这题考察你对真实反 spoofing 的理解）：
1. **没有时间窗口**：函数不感知时间，调用方传多少算多少——如果传 24 小时数据，80% 阈值没意义（正常做市商日 cancel/order 比也很高）
2. **不区分价格档位**：真实 spoof 是"挂在远离成交价的档位制造虚假深度、价格接近前撤单"，需要看挂单位置
3. **不区分单量**：小单频繁 cancel 不算 spoof，大单假挂才是
4. **没有自成交预防/对手分析**：spoof 的目的是诱导对手方向，需要看挂单方向和后续成交的关联
5. **不分 maker/taker**：spoof 单几乎永远是 maker 挂单、taker 撤单（实际吃单的是对手方）

**生产级做法**：
- **多维度特征**：单量、挂单时长、距成交价的 tick 数、撤单时市场方向（是否 price 向挂单移动）、对手方关联账户聚类
- **行为画像**：每个账户维护 spoof score，加权累加（大单短时挂单+撤单 加分），达到阈值标记 → 人工审核或自动限制
- **和行情研判耦合**：虚假深度会导致 DepthBias 失衡，可以用信号质量回测来检测 spoofing 污染
- **配合链上数据**：DEX 场景下地址资金实力、是否关联已知恶意地址

**【诚实表态】** 这个算法 MVP 只是骨架（只有 10 行代码），我知道生产级反 spoofing 是专门的团队方向（比如 Jump/Citadel 都有专门的 market surveillance 团队），当前先做最小可检测版本，后续接入专门的风控引擎。

---

<a id="sec-6"></a>

## 六、行情广播（自研 WebSocket Hub 替代 RabbitMQ）

<a id="q26"></a>

### Q26【初级】：自研 WebSocket 而不是用 gorilla/websocket，你具体实现了什么？为什么这么做？

**参考答案**：

代码位置：
- `internal/dex/ws/websocket.go`（232 行）：手写 RFC 6455 帧解析
- `internal/dex/ws/hub.go`（197 行）：Hub 主循环
- `internal/dex/ws/client.go`（97 行）：read/write pump

**具体实现的 RFC 6455 子集**：
- Frame 解析：opcode（text/binary/ping/pong/close）、masking/unmasking（客户端必须 mask）、extended length（16 位和 64 位长度）
- HTTP Upgrade：用 `http.Hijacker` 接口从标准库 `net/http` 劫持 TCP 连接，**没有用 gorilla**
- Ping/Pong 心跳：writePump 每 54 秒发 ping（`client.go:82-89`），readPump 处理 pong
- Close 握手：收到 close frame 后清理资源

**为什么零依赖**：
1. **学习/控制**：撮合引擎这种核心组件依赖越少越稳定，避免第三方库出现 CVE 或无人维护
2. **精简**：gorilla/websocket 有 5000+ 行代码，支持 permessage-deflate 等大量我们不需要的扩展；自己写 500 行足够用
3. **性能定制**：可以直接把 `bufio.Reader` 读缓冲和我们的消息帧对齐，减少一次 copy

**要承认的代价**：
- 手写协议栈风险高（mask 算错、分片处理错都会导致诡异 bug）
- 没有 permessage-deflate 压缩（带宽占用更高）
- **在生产环境还是建议换成 gorilla/websocket**——框架成熟度和 bug fix 速度远比自己写重要。自研版本是为了证明我理解 WebSocket 协议底层

---

<a id="q27"></a>

### Q27【中级】：Hub 的 broadcast channel 缓冲 10000，client.send 缓冲 256，慢消费者怎么处理？为什么不用阻塞？

**参考答案**：

代码位置：
- Hub channel 大小：`hub.go:40-42`（broadcast=10000, register=100, unregister=100）
- Client send 大小：`hub.go:190` 或 `client.go` 创建 client 时（256）
- 广播逻辑：`hub.go:74-88`，关键是非阻塞发送：

```go
for clientID := range h.subs[msg.channel] {
    if client, ok := h.clients[clientID]; ok {
        select {
        case client.send <- msg.data:
        default:  // 慢消费者直接丢弃
            // 这里其实没做任何事，消息静默丢失
        }
    }
}
```

**为什么要丢消息而不是阻塞**：
- 行情推送是"最新值语义"（latest-value semantics）——深度快照、最新 ticker 这种数据，用户只关心最新状态，老消息没价值
- 如果一个客户端慢（手机网络差），阻塞广播会影响**所有其他客户端**——这是"一个慢消费者拖垮所有人"的反模式
- 对比 RabbitMQ 模式：RabbitMQ 会在消费者断开前一直缓存，容易导致 broker 内存爆掉

**256 缓冲能撑多久**：
- 每个深度快照约 2KB，每秒约 10 次推送（高活跃时）→ 256 缓冲 ≈ 25 秒数据
- 客户端消费延迟超过 25 秒开始丢消息 → 这是合理的（25 秒前的深度完全过时）

**【诚实暴露缺陷】** 当前 `default` 分支没有任何统计——应该打 metrics（记录慢客户端丢消息数），超过阈值主动断开，避免僵尸连接占用资源。

---

<a id="q28"></a>

### Q28【中级】：Hub 里 register/unregister/broadcast 用了三种不同的并发模式——为什么不统一？

**参考答案**：

代码位置：`internal/dex/ws/hub.go:50-91`。

三种不同处理：
1. **register/unregister**：通过 channel（`chan *Client`）送到 hub 主 goroutine 串行处理
2. **Subscribe/Unsubscribe**：直接从 client readPump 调用，持有 `h.mu.Lock()`（`hub.go:134-161`）
2. **broadcast**：通过 channel 送到 hub 主 goroutine，但 fan-out 到 client 时在 hub goroutine 同步遍历

这看起来不一致，原因：
- register/unregister 操作频率极低（每秒几次），走 channel 无所谓
- **Subscribe/Unsubscribe 频率也低**（用户订阅切换），直接加锁更简单直观，不需要 channel 转发（少一次 channel 拷贝开销）
- **broadcast 频率很高**（每秒几十上百次），必须走主 goroutine 单线程，避免在持锁时遍历 client（持锁期间如果 client 的 send channel 阻塞会拖慢所有人）

**更优雅的统一方案**：把 Subscribe/Unsubscribe 也变成 channel 消息送进 hub 主 select，彻底去掉 `sync.Mutex`，所有对 `h.clients/h.subs` 的访问都在单 goroutine 内，消除锁。代价是多一点代码（定义 SubscribeCmd/UnsubscribeCmd 类型）。当前用锁是为了代码简洁。

---

<a id="q29"></a>

### Q29【高级/陷阱】：你的 hub 广播在遍历 `h.subs[channel]` 时，如果某个 client 刚好 Unsubscribe 怎么办？会 panic 吗？

**参考答案**：

代码位置：`hub.go:74-88`。

当前实现在广播时：
```go
for clientID := range h.subs[msg.channel] {
    if client, ok := h.clients[clientID]; ok {
        select {
        case client.send <- msg.data:
        default:
        }
    }
}
```

**风险点**：`for ... range h.subs[msg.channel]` 是在遍历 map，而 `Unsubscribe` 在另一个 goroutine 里会**删除 map 中的 clientID**（通过 `h.mu.Lock`）→ Go 规范明确：**map 在并发迭代 + 删除时会 panic（concurrent map iteration and map write）**。

我当前的实现用 `h.mu` 保护了 Subscribe/Unsubscribe 对 map 的写，但 `h.run()` 里的广播循环**没有拿 `h.mu.Lock`** → 这是**真正的并发 bug**。

**正确做法三选一**：
1. **方案一（推荐）**：所有对 `h.subs/h.clients` 的访问都放在 hub 主 goroutine 里——Subscribe/Unsubscribe 通过 channel 发送到 hub 主循环，让 hub 自己改 map。这样广播循环不用加锁，因为它和 map 修改在同一个 goroutine（参考前面 Q28 的"更优雅方案"）
2. **方案二**：广播循环也加 `h.mu.RLock`，Unsubscribe 加 `h.mu.Lock`。简单，但广播持读锁时间长（遍历所有 client 发消息），会阻塞 Subscribe/Unsubscribe
3. **方案三**：每次广播前拷贝一份 client 列表，释放锁后再发送。这是大部分生产级 hub 的做法（gorilla/websocket chat example 就是这样）

**加分回答**：还需要考虑 client 在 `select` 发送过程中被 unregister + `close(client.send)` 的情况——向已关闭 channel 发送会 panic。当前 unregister 逻辑会 `close(client.send)`（`hub.go:63`），广播时如果刚好遇到这个 client 从 map 中删除但还是在 range 里，`client.send` 可能已关闭。防范：`client, ok := h.clients[clientID]` 之后再加一个 `client.isClosing` 原子标志检查，或者用 recover 兜底。

---

<a id="q30"></a>

### Q30【资深】：你提到 fan-out 11 路 goroutine 生成 9 种 K 线 + Ticker + 成交明细，但主循环 `wg.Wait()` 等所有 goroutine 完成——这不是把 fan-out 变回串行瓶颈了吗？

**参考答案**：

代码位置：`internal/core/l2quote/l2quote.go:159-189 fan-out` 和 `:300 wg.Wait`。

**这确实是一个设计问题（要能自己发现）**：

```go
// 当前实现
for i := 0; i < 11; i++ {
    chs[i] <- &mrAB.Mr     // 给 11 个 goroutine 发数据（缓冲 1）
}
wg.Wait()                   // 等待全部 11 个处理完
```

问题在于：
- 11 个 goroutine **是**并行处理的（都在各自跑 updateKline/buildTicker）
- 但主 l2quote 循环要**等最慢的一个**完成才能接收下一个 MR
- 任何一个慢消费者（比如 Redis 写入超时、MQ 发送阻塞）会拖慢整个 l2quote → 进一步反压到 matcher goroutine（l2quote 的输入 channel 满了 → matcher 阻塞在发送 → 整个交易对撮合停摆）

**这是有意还是 bug？**
- 有意部分：保证 K 线时序正确性——MR 必须按顺序处理，不能出现"后一个 MR 已经更新到 1min K 线了，前一个 MR 才开始更新 5min K 线"的错乱
- 有问题的部分：Redis/MQ 这种外部 I/O 不应该放在关键路径上

**正确的改造方向**：
1. **K 线计算和持久化解耦**：11 个 goroutine 只做内存中的 K 线聚合（快，微秒级），做完立即 wg.Done()；Redis/MQ 发送改为异步 batch（单独的 goroutine + channel 缓冲）
2. **快照备份同理**：快照生成不能阻塞主路径，用 `singleflight` + 异步 goroutine
3. **对 MQ 发送失败降级**：如果 MQ 连接断开，应该丢消息（行情是实时数据，补发无意义），而不是阻塞主循环

**【路线图】** 当前 DEX 模式下已经通过 `redisClient == nil` 检查（`redis_ops.go:16`）跳过了 Redis 持久化（`DEX 模式下两者均跳过`），所以实际 wg.Wait 等待的只有内存 K 线计算，速度很快。集中式模式下因为有 Redis/MQ 才会遇到瓶颈。

---

<a id="rating"></a>

## 面试官评分维度（给候选人自评参考）

| 等级 | 能回答 | 表现 |
|------|--------|------|
| **能过简历面** | Q1, Q6, Q11, Q15, Q21, Q26 | 基本职责说清楚，能讲"我做了什么" |
| **中级工程师（P5-P6）** | + Q2, Q3, Q7, Q8, Q12, Q16, Q17, Q22, Q23, Q27 | 懂核心技术点，能讲"为什么这样设计"，理解简单 bug |
| **高级工程师（P6-P7）** | + Q4, Q9, Q13, Q18, Q19, Q24, Q28, Q29, Q30 | 能发现设计缺陷，提出改进方案，理解并发/安全细节 |
| **资深/架构师（P8+）** | + Q5, Q10, Q14, Q20, Q25 | 能跨层权衡（ZK 电路/MPC 信任模型/MEV/反欺诈），理解技术选型的经济/安全 trade-off，对 MVP 现状和路线图有清晰判断 |

---

<a id="bonus"></a>

## 面试加分项（主动说出来会加分）

1. **主动指出 MVP 占位**：哪些是 stub、哪些是路线图，比硬吹"全做完了"强 10 倍
2. **指出自己代码里的 bug**：LPMiningRewards 双计 bug、RiskEngine 回调持锁、Hub 并发 map panic、l2quote waitGroup 瓶颈——主动暴露说明你有 code review 能力
3. **能比较行业方案**：Hyperliquid（全链上）vs dYdX（链下撮合）vs AnuBookDEX（链下撮合+ZK），知道各自 trade-off
4. **能算数字**：100 goroutine 内存、256 缓冲撑多久、10x 杠杆强平价、ZK proof 时间约束——量化思维比空谈架构更有说服力
5. **安全意识**：Checks-Effects-Interactions、重入、precompile staticcall 安全性、nonce 管理——合约细节见功底
