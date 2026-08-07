# AnuBookDEX 面试题库（区块链后端工程师）

## 目录

- [一、Go 并发与撮合引擎（5 题）](#sec-1)
  - [Q1：项目中每个交易对使用独立 goroutine + channel 驱动，为什么不用共享内存 + 互斥锁？](#q1)
  - [Q2：订单簿用红黑树（gods/treeset）而不是 Go 原生 `container/heap` 或 B-Tree，为什么？](#q2)
  - [Q3：这段代码有什么问题？（现场代码审查）](#q3)
  - [Q4：项目的 channel 数据管道中，如果 puller 生产速度快于 matcher 消费速度，会发生什么？如何监控？](#q4)
  - [Q5：项目使用 `shopspring/decimal` 禁止 float64。如果某处代码不小心用了 float64 做金额计算，会出什么问题？](#q5)
- [二、DEX 架构与链上交互（4 题）](#sec-2)
  - [Q6：项目采用"链下撮合 + ZK 证明链上结算"模式，相比 Hyperliquid 的"全链上撮合"，各有什么优劣？](#q6)
  - [Q7：`chain/subscriber.go` 通过 WebSocket 订阅 Anubis Chain 事件，如果 WebSocket 断连，如何保证不丢订单？](#q7)
  - [Q8：多个链下引擎实例同时监听同一交易对的事件会怎样？如何防止重复撮合？](#q8)
  - [Q9：Settlement 合约的 `submitBatch` 为什么要用 `staticcall` 验证 ZK 证明，而不是在合约中直接验证？](#q9)
- [三、撮合算法细节（3 题）](#sec-3)
  - [Q10：自成交预防 5 种模式中，DC（减少并取消）在数量相等时的处理和 CN（取消新单）有什么本质区别？在什么场景下做市商会选择 DC 而不是 CB？](#q10)
  - [Q11：市价单的 CircuitRate 熔断保护同时检查了 `≤ 下界` 和 `≥ 上界`，两个条件同时为 OR。对于卖单来说，`≥ 上界` 这个条件在正常排序的订单簿中真的会触发吗？](#q11)
  - [Q12：项目使用 `shopspring/decimal` 设置了 `DivisionPrecision = 37`。为什么是 37？如果改成 18 会有什么影响？](#q12)
- [四、隐私与安全（3 题）](#sec-4)
  - [Q13：用户提交加密订单到链上 → 引擎解密撮合 → ZK 证明结算，这个链路中哪些环节可能泄漏交易信息？](#q13)
  - [Q14：Nullifier 防双花在 Go 侧（本地缓存）和 Solidity 侧（0x0103 预编译）都做了检查，为什么需要两层？](#q14)
  - [Q15：`ZKKYC.sol` 的分级隐私模型中，L2（ZK-KYC）用户如何在不泄露身份的前提下证明"我满足 ≥100 ETH 交易资格"？](#q15)
- [五、智能合约（2 题）](#sec-5)
  - [Q16：`Settlement.sol` 的 `submitBatch` 为什么先验证 ZK 证明再逐一检查 Nullifier？如果颠倒顺序有什么问题？](#q16)
  - [Q17：`LPMiningRewards` 的 `rewardDebt` 和 `feeDebt` 是做什么的？如果去掉会怎样？](#q17)
- [六、系统设计（3 题）](#sec-6)
  - [Q18：如果日交易量从 10 万笔增长到 1000 万笔，你会如何改造系统？请分层说明。](#q18)
  - [Q19：系统启动时如何从故障中恢复订单簿状态？请描述 DEX 模式下的完整恢复链路。](#q19)
  - [Q20：如果要为这个系统设计一个集成测试，验证"下单→撮合→结算→行情推送"的完整链路，你会怎么设计？](#q20)
- [面试官评分参考](#rating)

---

<a id="sec-1"></a>

## 一、Go 并发与撮合引擎（5 题）

<a id="q1"></a>

### Q1：项目中每个交易对使用独立 goroutine + channel 驱动，为什么不用共享内存 + 互斥锁？

**参考答案**：

项目在 `internal/dex/runner/runner.go:52-125` 的 `StartMatcher` 函数中实现：每个交易对在主循环中通过 `select` 同时监听 6 个 channel（订单 channel、快照定时器、深度定时器、上报定时器等），所有订单簿读写都在**同一 goroutine** 中完成。

选择这个模型的原因：
1. **避免锁竞争**：订单簿（红黑树 + HashMap）是复杂数据结构，加锁意味着每次撮合都要持有锁，高并发下锁成为瓶颈
2. **确定性执行**：同一交易对的订单严格按 SeqId 顺序处理，单 goroutine 天然保证顺序性，不需要额外同步
3. **channel 天然背压**：`make(chan *match.Order, 5000)` 设置了缓冲区大小，当处理速度跟不上时可以自然限流
4. **goroutine 成本极低**：每个交易对的 goroutine 约 4KB 栈空间，即使 100 个交易对也只占用 ~400KB

**追问**：如果有 100 个交易对，这种方式有什么潜在问题？如何解决？

参考：100 个交易对 = 100 个 goroutine + 600 个 channel，单机完全没问题。但每个 goroutine 中 fan-out 到 l2quote 还要再开 11 个 goroutine（生成 9 种 K 线 + Ticker + 成交明细），总计 100 × (1 + 11) = 1200 个 goroutine。需要关注的是 GC 压力而非 goroutine 数量。

<a id="q2"></a>

### Q2：订单簿用红黑树（gods/treeset）而不是 Go 原生 `container/heap` 或 B-Tree，为什么？

**参考答案**：

代码位于 `internal/core/match/order_book.go:48-53`。

| 数据结构 | 插入 | 删除 | 查找最优价 | 查找指定 OrderId | 遍历 |
|---------|------|------|-----------|-----------------|------|
| 红黑树 + HashMap | O(log n) | O(log n) | O(1) (Peek) | O(1) (cache map) | O(n) |
| heap | O(log n) | O(n) | O(1) | O(n) | O(n log n) |
| B-Tree | O(log n) | O(log n) | O(log n) | O(n) | O(n) |

选择红黑树的核心原因：
1. **HashMap 加速单点查找**：`cache map[int64]*Order` 提供 O(1) 的 OrderId 查找，heap 做不到
2. **价格-时间优先排序**：`Comparator` 函数（`order.go:277-302`）精确定义了 Buy 盘价格从高到低、Sell 盘价格从低到高、同价按 SeqId 从早到晚的排序规则
3. **频繁的中间删除**：撮合中对手单可能被部分成交留在树上、也可能被完全成交后 `Dequeue`。红黑树删除是 O(log n)，heap 需要 O(n) 遍历
4. **快照序列化**：`TreeSet` 实现了 `gob` 的 `BinaryMarshaler/Unmarshaler` 接口（`order_book.go:19-42`），可直接序列化到快照文件

<a id="q3"></a>

### Q3：这段代码有什么问题？（现场代码审查）

```go
// matcher.go:1107-1113
unitPrice := common.LOWPRECISION.Mul(oppoOrder.Price)
if unitPrice.GreaterThan(order.UnfilledAmount) {
    order.State = PrecisionCanceled
    return nil
}

orderAmount := order.UnfilledAmount.Div(unitPrice).Truncate(0).Mul(common.LOWPRECISION)
matchAmount := decimal.Min(orderAmount, oppoOrder.UnfilledAmount)
```

**参考答案**：

这里处理的是**市价买单**（以现金金额驱动撮合）的核心精度问题。

问题点：`Truncate(0)` 直接截断到整数，可能因为精度损失导致订单无法完全成交。

> 例：`unitPrice = 0.1`（即 `10 * 0.01`），`order.UnfilledAmount = 0.1999`（剩余 0.1999 单位现金）
> `orderAmount = 0.1999 / 0.1 = 1.999，Truncate(0) = 1, 1 * 0.01 = 0.01`
> 结果：剩余 0.1999 现金只能成交 0.01 金额，损失了 ~95% 的购买力

但因为 `unitPrice > order.UnfilledAmount` 的 guard（`0.1 > 0.1999` 为 false，不触发），`orderAmount` 最少为 `LOWPRECISION`（即 0.01），所以 `matchAmount = min(0.01, oppoAmount)` 仍能正确处理。

**真正的边界风险**：当 `order.UnfilledAmount < unitPrice` 时触发 `PrecisionCanceled`。这是正确设计——如果剩余现金买不起 1 最小单位 token，市价单余额取消。

**追问**：如果你要优化这个精度处理，会怎么做？

参考方案：使用 `Ceil`（向上取整）代替 `Truncate`，配合额外检查确保不超额成交。或者将 `unitPrice` 的精度从 `LOWPRECISION` 提高到与订单价格精度一致。

<a id="q4"></a>

### Q4：项目的 channel 数据管道中，如果 puller 生产速度快于 matcher 消费速度，会发生什么？如何监控？

**参考答案**：

项目使用**有缓冲 channel**：
```go
ch := make(chan *match.Order, 5000) // runner.go 或 cmd 入口文件
```

当 channel 满时，puller 的写入会**阻塞**，形成天然的**背压机制**。不会丢失数据，但会导致 puller 延迟增加。

监控方式：
1. **channel 长度监控**：`len(ch)` 在 `reportTicker` 中周期性上报（`runner.go:103-104`）
2. **SeqId 跟踪**：`statistics.SetMatchTag(order.SeqId)` 记录当前处理到的 SeqId，与 MySQL 最新 SeqId 对比可得延迟
3. **Datadog/statsd 指标**：`dogstatsd.GaugeBySymbol("orderbook.currentId", ...)` （`order_book.go:212`）

**追问**：如果要实现优雅降级（channel 满时丢弃非关键订单），怎么改？

参考：在 `select` 的 `default` 分支中处理（用非阻塞写入 + 丢弃逻辑），或引入优先级 channel。

<a id="q5"></a>

### Q5：项目使用 `shopspring/decimal` 禁止 float64。如果某处代码不小心用了 float64 做金额计算，会出什么问题？

**参考答案**：

```go
// 坏例子（项目中不存在，仅为说明）
price := 0.1 + 0.2  // Go 中 = 0.30000000000000004

// 好例子（项目实现）
price := decimal.NewFromFloat(0.1).Add(decimal.NewFromFloat(0.2)) // = 0.3
```

后果：
1. **撮合错误**：`matchAble`（`matcher.go:192-198`）中价格比较用 `GreaterThanOrEqual`/`LessThanOrEqual`，float64 的精度误差可能导致本应成交的订单误判为不可成交
2. **快照不一致**：gob 序列化 float64 在不同架构间有差异（ARM vs x86），恢复后订单簿状态可能与原状态不同
3. **资金安全**：最终用户收到的金额可能比应得的少 0.0000000000001 token

项目保障措施：`init()` 中设置 `decimal.DivisionPrecision = 37`，且在 `CheckOrderScale`（`order.go:325-338`）中对所有订单做精度校验。

---

<a id=”sec-2”></a>

## 二、DEX 架构与链上交互（4 题）

<a id=”q6”></a>

### Q6：项目采用”链下撮合 + ZK 证明链上结算”模式，相比 Hyperliquid 的”全链上撮合”，各有什么优劣？

**参考答案**：

| | 链下撮合 + ZK 证明 | 全链上撮合（Hyperliquid） |
|---|---|---|
| **撮合延迟** | < 10ms（内存） | ~200ms（共识） |
| **去中心化** | 需信任链下引擎诚实（ZK 事后验证） | 完全去中心化（共识过程 = 撮合过程） |
| **吞吐量** | 单机 10K-50K TPS | 全网 4K+ TPS |
| **隐私** | 可加密订单（引擎专属解密） | 全网可见 |
| **Gas 成本** | 仅结算批上链 | 每笔订单上链 |
| **故障恢复** | 需快照 + 外部源重放 | 链上状态天然恢复 |
| **MEV 抵抗** | 加密订单 + ZK 证明 | 公开信息，存在 MEV |

AnuBookDEX 的选择更适合**隐私优先 + 高频交易**场景，Hyperliquid 更适合**信任最小化**场景。

<a id="q7"></a>

### Q7：`chain/subscriber.go` 通过 WebSocket 订阅 Anubis Chain 事件，如果 WebSocket 断连，如何保证不丢订单？

**参考答案**：

从代码看，当前 `chain.NewSubscriber(wsEndpoint)` 创建时仅传入 WebSocket URL，没有显式的重连逻辑。但可以从以下维度设计保障：

1. **区块高度追踪**：`OrderSubmitted` 事件包含 `blockNumber`。断连恢复后从最后处理的 blockNumber + 1 开始重订阅（`bind.WatchOpts{Start: lastBlock}`）
2. **幂等处理**：通过 `SeqId` 或 `Nullifier` 保证同一链上事件不被重复撮合。`OrderBook.FromId` 记录最后处理的 SeqId，天然过滤重复
3. **心跳 + 指数退避重连**：goroutine 中维护 WebSocket 连接，ping/pong 检测断开，1s/2s/4s/8s... 退避重连
4. **最终一致性检查**：定期对比链上 Registry SC 的事件计数与引擎已处理的订单数

**追问**：如果断连期间订单已过期（`deadline < currentBlock`），如何处理？

应在 `submitOrder` 前检查 `deadline > block.number`，这在 Solidity 合约层已经做了（`OrderBookRegistry.sol`）。

<a id="q8"></a>

### Q8：多个链下引擎实例同时监听同一交易对的事件会怎样？如何防止重复撮合？

**参考答案**：

这是项目的**核心架构约束**——当前设计中同一交易对只能由一个引擎实例处理。方案：
1. **交易对分片**：在部署层面通过配置将不同交易对分配给不同实例，ALB 按 symbol 路由（`deploy/user-data.sh` 中通过 shard index 选择交易对列表）
2. **Nullifier 唯一性**：链上 `0x0103` 预编译确保每个 Nullifier 只被处理一次。即使两个引擎同时解密了同一订单，只有第一个提交结算的能通过 Nullifier 校验
3. **未来改进**：引入**分布式锁**（etcd/consul）做引擎选举，或使用 Anubis Chain 上的 `Registry SC` 记录引擎认领关系

<a id="q9"></a>

### Q9：Settlement 合约的 `submitBatch` 为什么要用 `staticcall` 验证 ZK 证明，而不是在合约中直接验证？

**参考答案**：

```solidity
// Settlement.sol
(bool ok, bytes memory data) = VERIFY_PROOF.staticcall(abi.encodePacked(zkProof, publicInputs));
```

原因：
1. **Anubis 0x0100 预编译**是原生实现的 PLONK 验证器，执行双线性配对（bilinear pairing）数学运算——这些运算在 EVM 字节码层面的 Gas 成本极高（`ECPAIRING` 每个配对 ~45000 gas），而 **native 预编译实现 < 10ms 且 Gas 固定**
2. **Gas 经济性**：如果 `submitBatch` 中 500 笔撮合结果每条都要做配对验证，Gas 会爆炸。预编译将配对计算下沉到节点本地，只返回 `true/false` 给 EVM
3. **`staticcall` 安全性**：不允许修改状态，不会引入重入攻击面

---

<a id="sec-3"></a>

## 三、撮合算法细节（3 题）

<a id="q10"></a>

### Q10：自成交预防 5 种模式中，DC（减少并取消）在数量相等时的处理和 CN（取消新单）有什么本质区别？在什么场景下做市商会选择 DC 而不是 CB？

**参考答案**：

代码位置：`matcher.go:817-913` 的 `matchAmountBasedOrderSelfTrade`

| 场景 | DC 处理 | CB 处理 | CN 处理 |
|------|--------|--------|--------|
| 新单量 = 旧单量 | **双方都取消**（等同于 CB） | 双方都取消 | **仅取消新单**（旧单保留） |
| 新单量 > 旧单量 | 新单减量（减少 matchAmount），旧单取消 | 双方都取消 | 新单取消（旧单保留） |
| 新单量 < 旧单量 | 新单取消，旧单减量 | 双方都取消 | 新单取消（旧单保留） |

做市商选择 DC 的场景：**希望维持挂单深度，不想自成交浪费手续费，但也不想完全清空挂单**

> 例：做市商在 100 挂买单 10 ETH，同时另一账户要卖 3 ETH
> - 用 CB：两个订单都取消，做市商深度消失，需要重新挂单（交两次 Gas）
> - 用 DC：新卖单取消，买单减量到 7 ETH，做市商深度仍然存在

**追问**：为什么 `matchCashAmountBasedOrderSelfTrade`（`matcher.go:942-1073`）的 DC 逻辑比 `matchAmountBasedOrderSelfTrade` 更复杂？

因为市价买单的 `UnfilledAmount` 是**现金金额**而非 token 数量，需要先通过 `orderAmount = cash / unitPrice` 转换为等效 token 数量，才能与对手单的 `UnfilledAmount`（token 数量）做比较。这引入了额外的精度处理（`Truncate(0)`）和溢出检查。

<a id="q11"></a>

### Q11：市价单的 CircuitRate 熔断保护同时检查了 `≤ 下界` 和 `≥ 上界`，两个条件同时为 OR。对于卖单来说，`≥ 上界` 这个条件在正常排序的订单簿中真的会触发吗？

**参考答案**：

不会。代码在 `matcher.go:293-299`：

```go
if len(results) > 0 &&
    !order.CircuitRate.Equals(decimal.Zero) &&
    ((decimal.New(1, 0).Sub(order.CircuitRate).
        Mul(*results[0].Price)).GreaterThanOrEqual(oppoOrder.Price) ||
        ((decimal.New(1, 0).Add(order.CircuitRate).
            Mul(*results[0].Price)).LessThanOrEqual(oppoOrder.Price))) {
```

对于**卖单**匹配买单（价格降序排列）：
- 第一笔成交价 = 最优买价（最高）→ `results[0].Price`
- 后续买单价格 ≤ 第一笔成交价
- `firstPrice * (1 + circuitRate)` > `firstPrice` ≥ 后续任何买单价格
- 所以 `firstPrice * 1.1 <= oppoPrice` **永远不成立**

两个条件用 OR 的原因是**代码复用**——同一段代码同时服务买单和卖单，各自的“正确”分支不同，OR 保证了两种方向都能正确触发。

**这是一个有趣的代码审查点**：虽然功能正确，但 OR 条件中的一个分支是死代码（取决于买卖方向），对代码可读性有影响。更好的写法可能是显式分买卖方向处理（但会增加代码冗余）。

<a id="q12"></a>

### Q12：项目使用 `shopspring/decimal` 设置了 `DivisionPrecision = 37`。为什么是 37？如果改成 18 会有什么影响？

**参考答案**：

代码位置：`cmd/engine/main.go:30`

```go
func init() {
    decimal.DivisionPrecision = 37
}
```

37 的来源：
1. **uint256 最大值兼容**：`2^256 ≈ 1.15 × 10^77`，需要至少 78 位十进制精度。37 位是 `decimal` 库的默认精度上限，提供一个合理的中间精度（不溢出且不过度消耗内存）
2. **金融计算精度**：Swift/Java 的 `BigDecimal` 默认 34-40 位，37 是业界常见选择
3. **性能平衡**：更高精度 = 更多内存 + 更慢的数学运算

若改为 18：
- token（18 位小数）× token（18 位小数）= 36 位小数，`accFilledAmountValue = amount * price`（如 1.5 ETH × $2000.123456789012345678）需要足 36 位，18 位精度不够 → 撮合舍入错误 → 资金损失

---

<a id="sec-4"></a>

## 四、隐私与安全（3 题）

<a id="q13"></a>

### Q13：用户提交加密订单到链上 → 引擎解密撮合 → ZK 证明结算，这个链路中哪些环节可能泄漏交易信息？

**参考答案**：

全链路分析：

| 环节 | 泄漏风险 | 缓解措施 |
|------|---------|---------|
| 用户→链上（Type 103） | **低** | Pedersen 承诺 + Note 加密，仅 ViewTag（4 bytes）可见用于加速扫描 |
| 链上事件广播 | **低** | 所有验证者只能看到 NoteCommitment + Nullifier，无法解密 |
| 引擎解密 | **中** | 引擎持有 View Key，是唯一解密点。若引擎被攻破，所有待撮合订单泄漏 |
| 撮合过程（内存） | **中** | 订单明文在内存中短存（< 热点路径 ~100ms 内完成撮合），但内存 dump 可被取证 |
| ZK 证明生成 | **低** | 证明仅包含“存在一组有效撮合结果”而不泄露具体订单 |
| 链上结算 | **低** | Settlement SC 只收到 matchResults（已去隐私化）和 zkProof |

最大风险点：**引擎内存**是隐私的瓶颈。改进方向：
- 使用 TEE（Intel SGX / AWS Nitro Enclaves）运行引擎，防止内存 dump
- Phase 5 的 MPC 暗池方案将撮合也移入安全多方计算，引擎不再接触明文

<a id="q14"></a>

### Q14：Nullifier 防双花在 Go 侧（本地缓存）和 Solidity 侧（0x0103 预编译）都做了检查，为什么需要两层？

**参考答案**：

1. **链上检查（0x0103）**是**最终权威**：防止用户将同一个加密订单通过 Type 103 重复提交到链上
2. **链下缓存（`usedNullifiers` mapping）**是**性能优化**：避免对已处理的 Nullifier 重复解密和撮合（减少不必要的 View Key 解密开销 + ZK 证明生成）

两层不是冗余，而是**纵深防御**：链上保证没有人能双花，链下保证引擎不浪费计算资源。

**追问**：如果链下缓存因重启丢失，会有什么后果？如何恢复？

恢复后引擎重新同步链上事件，可能遇到已结算的 Nullifier。此时 `Settlement.sol` 的 `settledNullifiers` mapping 会拒绝重复结算（`NullifierAlreadySettled`），但引擎会在解密和撮合上浪费计算。**
恢复方案**：启动时扫描 Settlement 合约的 `BatchSettled` 事件，重建本地 Nullifier 集合。

<a id=”q15”></a>

### Q15：`ZKKYC.sol` 的分级隐私模型中，L2（ZK-KYC）用户如何在不泄露身份的前提下证明”我满足 ≥100 ETH 交易资格”？

**参考答案**：

```solidity
function verifyKYC(...) 验证了三个条件而不获取用户身份：
1. ZK 证明格式正确（0x0100 预编译返回 true）
2. 用户不在制裁名单（sanctionList 检查）
3. Nullifier 未被使用过
```

ZK-KYC 的核心：
- 用户持有政府颁发的身份证明（护照/驾照）→ 链下生成 ZK 证明"我有一份有效期内的有效证件"（不泄露护照号、姓名、国籍）
- 合规官 `approveKYC(user, L2)` 批准后，用户获得 L2 级别的交易限额
- 大额交易时 `classifyOrder` 检查 orderValue 是否在 L2 阈值范围内

与传统 KYC 的区别：**传统 KYC 需要上传证件复印件 → 中心化服务器存储 → 数据泄漏风险。ZK-KYC 只存储 proofHash 和验证级别，完全去身份化。**

---

<a id="sec-5"></a>

## 五、智能合约（2 题）

<a id="q16"></a>

### Q16：`Settlement.sol` 的 `submitBatch` 为什么先验证 ZK 证明再逐一检查 Nullifier？如果颠倒顺序有什么问题？

**参考答案**：

```solidity
// 当前顺序（正确）：
1. ZK 证明验证（staticcall 0x0100）
2. for batch: Nullifier 检查（staticcall 0x0103）+ 结算

// 颠倒顺序（错误）：
1. for batch: Nullifier 检查 + 结算
2. ZK 证明验证
```

颠倒的风险：
- ZK 证明验证失败时，Nullifier 已经被标记为已使用（`settledNullifiers[n] = true`），但结算未执行
- 合法用户想重新提交时，Nullifier 被“污染”无法使用 → 订单永久丢失
- **正确的设计**：先验证 ZK 证明（轻量操作，不需要修改状态），确认有效后再修改状态

这是典型的 **“检查-效果-交互”模式（Checks-Effects-Interactions）** 在合约中的应用。

<a id="q17"></a>

### Q17：`LPMiningRewards` 的 `rewardDebt` 和 `feeDebt` 是做什么的？如果去掉会怎样？

**参考答案**：

```solidity
// stake 时更新债务：
s.rewardDebt = s.amount * accRewardPerShare / PRECISION;
s.feeDebt = s.amount * accFeePerShare / PRECISION;

// claimReward 时：
reward = s.amount * accRewardPerShare / PRECISION - s.rewardDebt;
```

`rewardDebt` 是**已结算的奖励基线**。它确保用户在 claim 时只领取**质押后新产生的奖励**，而不是全局累计的所有奖励。

如果没有它：
- 用户 A 质押 100 LP → `accRewardPerShare = 10`
- 用户 B 质押 50 LP → `accRewardPerShare = 30`（因为又分配了奖励）
- B claim 得到 `50 * 30 / 1e18 = 1500`（错误！B 质押后实际只产生了 `50 * 20 / 1e18 = 1000` 新奖励）
- 多余的 500 来自 A 质押期间的奖励 → **B 窃取了 A 的奖励**

有 `rewardDebt`：
- A 质押时：`A.rewardDebt = 100 * 10 = 1000`
- B 质押时：`B.rewardDebt = 50 * 30 = 1500`
- B claim：`50 * 30 - 1500 = 0`（正确！B 刚质押，还没有产生任何新奖励）

这是 **MasterChef 式收益分配算法**，Uniswap/Sushiswap 等主流 DeFi 协议均使用此模式。

---

<a id="sec-6"></a>

## 六、系统设计（3 题）

<a id="q18"></a>

### Q18：如果日交易量从 10 万笔增长到 1000 万笔，你会如何改造系统？请分层说明。

**参考答案**：

详见 `deployment_analysis.md`。核心改造分 4 层：

**撮合层**：高频交易对独立部署（ETH_USDT → t3.large 单实例），低频对共享实例

**行情层**：引入 Redis Pub/Sub 作为 WebSocket 实例间总线 → 撮合实例只负责匹配，行情广播由专用的 WS 集群处理

**结算层**：增大 `settlement-batch-size` 到 500+，并使用 ZK Rollup 聚合证明 → 一次链上交易验证 1000+ 笔撮合

**存储层**：RocksDB 改为增量 WAL + 定期全量快照，减少快照 I/O 压力

<a id="q19"></a>

### Q19：系统启动时如何从故障中恢复订单簿状态？请描述 DEX 模式下的完整恢复链路。

**参考答案**：

```go
// cmd/engine/main.go:114-119
book, err := snapshotStore.LoadLatest(symbol)
if book == nil {
    book = match.InitOrderBook(0, symbol)
}
```

恢复链路：
1. **RocksDB 加载最新快照**：`SnapshotStore.LoadLatest(symbol)` → 反序列化 gob 格式的快照（包含 FromId + 红黑树 + HashMap）
2. **链上事件重放**：从 `book.FromId + 1` 开始，通过 chain Subscriber 拉取遗漏的 `OrderSubmitted` 事件
3. **交叉校验（集中式模式）**：重放结果与 `persistence` 中存储的历史 MatchResult 逐一对比（`validate/validate.go`）
4. **启动撮合循环**：`runner.StartMatcher(book, orderCh, ...)` 开始正常撮合

恢复时间 < 5 秒（本地 RocksDB）+ 事件重放时间（取决于断线时长）。

<a id=”q20”></a>

### Q20：如果要为这个系统设计一个集成测试，验证”下单→撮合→结算→行情推送”的完整链路，你会怎么设计？

**参考答案**：

```
1. Mock Anubis Chain（使用 Hardhat localhost 或 Anvil）
2. 部署 7 个合约
3. 启动 Go engine（指向 localhost RPC）
4. 提交加密订单（Type 103）→ OrderBookRegistry
5. 等待链下引擎监听事件 → 解密 → 撮合
6. 验证：
   a. Settlement 合约收到 submitBatch 调用
   b. WebSocket 订阅客户端收到 depth/kline/trade 推送
   c. RocksDB 中有对应的快照文件
   d. 买卖双方的余额变化正确
7. 模拟故障：kill engine → 重启 → 验证从快照恢复后订单簿状态一致
```

关键点：签名的 `PRIVATE_KEY` 不能用真实链的；需要使用 Anvil 的 `--prank` 模拟用户签名；测试应在 CI 中自动化（`go test` + `forge test` + integration script）。

---

<a id="rating"></a>

## 面试官评分参考

| 等级 | 标准 |
|------|------|
| **初级** | 能回答 Q1-Q5（Go 并发 + 数据结构）；知道 Go 的 goroutine/channel 基本用法 |
| **中级** | 能回答 Q6-Q12（DEX 架构 + 撮合细节 + ZK）；理解链上链下协作原理 |
| **高级** | 能回答 Q13-Q17（隐私 + 合约 + 安全）；理解零知识证明在实际系统中的应用 |
| **资深** | 能回答 Q18-Q20（系统设计 + 恢复 + 测试）；能独立设计 10 万→1000 万扩展方案 |
