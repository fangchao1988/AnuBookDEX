# Aleo 公开/隐私混合下单方案（四种组合一次做全）

> 状态：设计方案（待确认后实施）。对应合约新程序名 **anubook_dex_p6.aleo**（Aleo 不允许同名重部署，p5 已占名）。
> 前身：[Aleo改造方案.md](Aleo改造方案.md)（p2-p4）、当前线上 p5 合约 `contracts/leo/src/main.leo`。

## 0. 背景与目标

**设计方向（已与用户确认）**：标准下单使用**公开余额**，隐私下单使用**隐私余额**。

当前痛点：
1. 用户公开余额（balances mapping，如 USDCX 81.24）无法用于下单——p5 的 `place_order_buy` 需要 Token record + Credentials record 锁仓，公开余额不是 record，报错 `No record matching constraints for test_usdcx_stablecoin.aleo/Token`。
2. p5 `Order.owner` 字段存的是 operator 地址，settle 时资产转回 operator（E2E 时 operator==用户才没暴露此问题）。
3. p5 链上无 `cancel_order` transition（引擎 cancel.go 实际调不到）。
4. p5 只支持隐私路径，公开路径缺失。

本方案一次设计全部 **4 种下单路径**（买/卖 × 公开/隐私）与对应的 **4 种结算配对**、**4 种撤单**，并修正 p5 资产流向缺陷。

## 1. 组合矩阵

| 下单路径 | 前端钱包构造 | 托管形态（operator 名下） | 订单参数位置 | 引擎获取参数 |
|---|---|---|---|---|
| **标准买** place_order_buy_public | 无 record 输入（纯公开参数） | 公开 USDCX 余额 | `public_orders` mapping | 交易公开 inputs（无需 view key） |
| **隐私买** place_order_buy_private | Token record + Credentials record | Token record + Credentials record | 加密 Order record | view key 解密（现有） |
| **标准卖** place_order_sell_public | 无 record 输入 | 公开 ALEO（credits）余额 | `public_orders` mapping | 交易公开 inputs |
| **隐私卖** place_order_sell_private | credits record | credits record | 加密 Order record | view key 解密（现有） |

结算配对（撮合时买方=maker、卖方=taker，与 p5 settle 断言一致）：

| 配对 | 结算 transition | 买方收 ALEO | 卖方收 USDCX |
|---|---|---|---|
| 买公开 × 卖公开 | `settle_pp` | 公开（credits transfer_public） | 公开（USDCX transfer_public） |
| 买公开 × 卖隐私 | `settle_pv` | 隐私 record（消费 op_aloe） | 公开（USDCX transfer_public） |
| 买隐私 × 卖公开 | `settle_vp` | 公开（credits transfer_public） | 隐私 record（消费 op_usdcx+creds） |
| 买隐私 × 卖隐私 | `settle_vv` | 隐私 record（现有 p5 路径） | 隐私 record（现有 p5 路径） |

**核心规则：收益形态 = 对手方托管形态**——买家收到的 ALEO 形态取决于卖家的托管形态，卖家收到的 USDCX 形态取决于买家的托管形态。

## 2. p6 合约设计（anubook_dex_p6.aleo）

### 2.1 数据结构

```leo
import credits.aleo;
import test_usdcx_stablecoin.aleo;

program anubook_dex_p6.aleo {

    // 隐私订单 record（p5 基础上加 trader 字段，修正资产流向）
    record Order {
        owner: address,     // operator（record 归属，operator 才能 spend 做 settle）
        trader: address,    // 下单用户（p6 新增：结算收益接收人）
        order_id: u128,
        side: u8,           // 0=buy(付 USDCX 得 ALEO), 1=sell(付 ALEO 得 USDCX)
        price: u64,         // 单价(最小单位,6 位)
        amount: u64,        // 数量(最小单位,6 位)
        deadline: u32,
    }

    // 公开订单（参数链上透明，存 mapping，不铸 record）
    struct PublicOrder {
        owner: address,     // 下单用户
        side: u8,
        price: u64,
        amount: u64,
        deadline: u32,
        status: u8,         // 0=active, 1=settled, 2=canceled（防重放状态机）
    }

    mapping public_orders:
        key as field.public;          // order_id as field
        value as PublicOrder.public;

    @noupgrade
    constructor() {}
```

### 2.2 四个下单 transition

**place_order_buy_public（标准买）**：async，公开 USDCX → operator 公开余额托管 + mapping 记账。

```leo
    transition place_order_buy_public(order_id: u128, price: u64, amount: u64, deadline: u32, operator: address) -> Future {
        let needed: u128 = ((price * amount) / 1000000u64) as u128; // 锁 USDCX = price*amount
        assert(needed > 0u128);
        return finalize_place_order_buy_public(self.caller, order_id as field, operator, needed, price, amount, deadline);
    }

    async function finalize_place_order_buy_public(caller: address, oid: field, operator: address, needed: u128, price: u64, amount: u64, deadline: u32) {
        // contains 断言防 order_id 冲突覆盖
        assert_eq(public_orders.contains(oid), false);
        test_usdcx_stablecoin.aleo::transfer_public(operator, needed); // from=caller(用户)，余额不足整笔回滚
        public_orders[oid] = PublicOrder { owner: caller, side: 0u8, price: price, amount: amount, deadline: deadline, status: 0u8 };
    }
```

**place_order_sell_public（标准卖）**：async，公开 ALEO credits → operator 公开余额托管。

```leo
    transition place_order_sell_public(order_id: u128, price: u64, amount: u64, deadline: u32, operator: address) -> Future {
        assert(amount > 0u64);
        return finalize_place_order_sell_public(self.caller, order_id as field, operator, price, amount, deadline);
    }

    async function finalize_place_order_sell_public(caller: address, oid: field, operator: address, price: u64, amount: u64, deadline: u32) {
        assert_eq(public_orders.contains(oid), false);
        credits.aleo::transfer_public(operator, amount); // from=caller(用户)，microcredits
        public_orders[oid] = PublicOrder { owner: caller, side: 1u8, price: price, amount: amount, deadline: deadline, status: 0u8 };
    }
```

**place_order_buy_private / place_order_sell_private（隐私买/卖）**：沿用 p5 现有逻辑，仅 Order record 增加 `trader: self.caller` 字段（用户地址），其余不变（Token+Creds → transfer_private_with_creds / credits transfer_private 托管给 operator）。

### 2.3 四个 settle transition

共同断言（与 p5 一致）：`maker.side==0`、`taker.side==1`、`match_price∈[taker.price, maker.price]`、`match_amount≤双方amount`。公开订单侧额外断言 `status==0` 并置 `status=1`（防重放；隐私侧靠 Order record 一次性消费天然防重放）。

**settle_pp（买公开 × 卖公开）**：纯 async，无 record 输入。

```leo
    transition settle_pp(maker_order_id: u128, taker_order_id: u128, match_price: u64, match_amount: u64) -> Future {
        return finalize_settle_pp(maker_order_id, taker_order_id, match_price, match_amount);
    }

    async function finalize_settle_pp(maker_id: u128, taker_id: u128, match_price: u64, match_amount: u64) {
        let maker: PublicOrder = public_orders[maker_id as field];
        let taker: PublicOrder = public_orders[taker_id as field];
        assert_eq(maker.side, 0u8);
        assert_eq(taker.side, 1u8);
        assert_eq(maker.status, 0u8);
        assert_eq(taker.status, 0u8);
        assert_eq(match_price <= maker.price, true);
        assert_eq(match_price >= taker.price, true);
        assert_eq(match_amount <= maker.amount, true);
        assert_eq(match_amount <= taker.amount, true);
        let quote_out: u128 = ((match_price * match_amount) / 1000000u64) as u128;
        credits.aleo::transfer_public(maker.owner, match_amount);          // from=caller(operator) 公开托管余额
        test_usdcx_stablecoin.aleo::transfer_public(taker.owner, quote_out);
        public_orders[maker_id as field].status = 1u8;
        public_orders[taker_id as field].status = 1u8;
    }
```

**settle_vp（买隐私 × 卖公开）**：transition 消费 maker Order（隐私）+ op_usdcx Token + op_creds，私有转给 taker；finalize 校验公开 taker + 公开转给 maker。

```leo
    transition settle_vp(maker: Order, op_usdcx: test_usdcx_stablecoin.aleo::Token, op_creds: test_usdcx_stablecoin.aleo::Credentials,
                         taker_order_id: u128, taker_owner: address, match_price: u64, match_amount: u64)
        -> (test_usdcx_stablecoin.aleo::ComplianceRecord, test_usdcx_stablecoin.aleo::Token, test_usdcx_stablecoin.aleo::Token, test_usdcx_stablecoin.aleo::Credentials, Future) {
        assert_eq(maker.side, 0u8);
        assert_eq(match_price <= maker.price, true);
        assert_eq(match_amount <= maker.amount, true);
        let quote_out: u128 = ((match_price * match_amount) / 1000000u64) as u128;
        // 卖方收隐私 USDCX（带 operator 凭证）；taker_owner 由 operator 传入，finalize 校验与 mapping 一致
        let f = test_usdcx_stablecoin.aleo::transfer_private_with_creds(taker_owner, quote_out, op_usdcx, op_creds);
        return (f.0, f.1, f.2, f.3, finalize_settle_vp(maker.trader, taker_order_id, taker_owner, match_price, match_amount));
    }

    async function finalize_settle_vp(maker_trader: address, taker_order_id: u128, taker_owner: address, match_price: u64, match_amount: u64) {
        let taker: PublicOrder = public_orders[taker_order_id as field];
        assert_eq(taker.owner, taker_owner);   // 防 operator 篡改收款人
        assert_eq(taker.side, 1u8);
        assert_eq(taker.status, 0u8);
        assert_eq(match_price >= taker.price, true);
        assert_eq(match_amount <= taker.amount, true);
        credits.aleo::transfer_public(maker_trader, match_amount);  // 买方收公开 ALEO（from=operator 托管余额）
        public_orders[taker_order_id as field].status = 1u8;
    }
```

**settle_pv（买公开 × 卖隐私）**：与 vp 镜像。transition 消费 taker Order（隐私）+ op_aloe credits，私有转给 maker.trader；finalize 校验公开 maker + `test_usdcx_stablecoin.aleo::transfer_public(taker.owner, quote_out)`（USDCX 公开转卖家）。

**settle_vv（买隐私 × 卖隐私）**：p5 现有 settle 原样，仅收益地址从 `maker.owner`/`taker.owner` 改为 `maker.trader`/`taker.trader`。

### 2.4 四个 cancel transition

| transition | 输入 | 退回路径 |
|---|---|---|
| `cancel_buy_public(order_id)` | async，operator 签名 | USDCX transfer_public(owner, 托管额) + mapping status=2 |
| `cancel_sell_public(order_id)` | async，operator 签名 | credits transfer_public(owner, amount) + mapping status=2 |
| `cancel_buy_private(order CT + op_usdcx CT + op_creds CT)` | operator 签名 | transfer_private_with_creds 全额退回用户 + Order 消费 |
| `cancel_sell_private(order CT + op_aloe CT)` | operator 签名 | credits transfer_private 全额退回用户 + Order 消费 |

（p5 链上无 cancel_order，p6 一次补齐；公开 cancel 只退全额，不涉及部分成交——部分成交的剩余处理见 §6 范围外。）

### 2.5 资产流转汇总表

| 下单 | 锁仓从（用户） | 托管到（operator） | 结算输出（买方←卖方托管形态 / 卖方←买方托管形态） |
|---|---|---|---|
| 公开买 | 公开 USDCX | operator 公开 USDCX | 卖方收公开 USDCX |
| 隐私买 | Token+Creds records | operator Token+Creds records | 卖方收隐私 Token record（含凭证派生） |
| 公开卖 | 公开 credits | operator 公开 credits | 买方收公开 ALEO |
| 隐私卖 | credits record | operator credits record | 买方收隐私 credits record |

## 3. 引擎改动（internal/dex/chain/aleo/）

### 3.1 提取（extract.go）

- `ExtractAndDecryptOrder` 按 function 分派：
  - `place_order_buy_public` / `place_order_sell_public`：**无需 view key 解密**。订单参数（order_id/price/amount/deadline）与用户地址在 dex transition 公开 inputs（`GetTransaction` 的 `execution.transitions[dex].inputs`，type=public 的字段）。OrderCT/OpFund/Creds 置空。
  - 现有 buy/sell 保持 view key 解密路径。
- `OrderPayload` 增加 `Mode`（public/private）；`PooledOrder` 同步增加 Mode + 公开订单参数（settle 时按 OrderId 回查）。

### 3.2 结算路由（settlement.go）

- `executeSettle` 按 `(makerMode, takerMode)` 路由 4 个 transition（leo execute 参数）：

| 组合 | leo execute 参数 |
|---|---|
| pp | `settle_pp maker_id taker_id price amount` |
| vp | `settle_vp makerCT op_usdcx op_creds taker_id taker_owner price amount` |
| pv | `settle_pv takerCT op_aloe maker_id maker_owner price amount` |
| vv | `settle makerCT takerCT op_aloe op_usdcx op_creds price amount`（现有） |

- settling/failed 状态流、10s 重结算循环、幂等判定保持不变（mode 随订单持久化到 trade 记录，重试时按存储的 mode 路由）。
- 撮合结果 `MatchResult` 需携带 maker/taker 的 mode（或结算时从 pool 回查——注意 settle 发生在撮合完成之后，pool 条目可能已被 Complete 清理，**建议 mode 写入撮合结果/成交记录持久化**，不要依赖 pool 回查）。

### 3.3 撤单（cancel.go）

- 按订单 mode 路由：公开 → `cancel_order_public`（仅 order_id，无 CT）；隐私 → `cancel_order_private`（Order CT + 托管 record CT，需要 pool 保存托管 CT——现有 pool 已存 OpFund/Creds）。
- 链下撮合队列移除逻辑不变（Cancel 订单进 matcher）。

### 3.4 配置

- `chain.aleo.program-id` → `anubook_dex_p6.aleo`；`chain.aleo.program-dir` 不变。
- 新增可选 `chain.aleo.order-timeout`（公开订单 deadline 过期的链下清理，若实现范围外则暂不加）。

## 4. 前端改动（web/src/）

### 4.1 钱包下单（lib/wallet/shield.ts）

- `placeOrderP4` 按「标准/隐私」分发（**根治 "No record matching constraints"**）：
  - **标准**：`place_order_buy_public(order_id, price, amount, deadline, operator)` / `place_order_sell_public(...)` —— 纯公开参数，**不调 requestRecords**，无需 Token/Credentials/credits record。
  - **隐私**：现有 p4 路径（Token+Creds / credits records 过滤）。
- 下单后统一提交 tx_id 给引擎（不变）。

### 4.2 余额显示（PositionsPanel）

- 标准模式：公开余额——ALEO `credits.aleo/account` mapping（现有 fetchAleoBalance）+ USDCX `balances` mapping（现有 fetchUsdcxPublicBalance）。
- 隐私模式：record 余额（requestRecords 求和，现有）。
- 标准/隐私切换时余额区随动，标注「公开余额/隐私余额」。

### 4.3 下单表单与委托列表

- OrderForm 隐私下单前校验 record 充足并给出「请使用标准模式（公开余额）」引导；标准下单校验公开余额充足。
- 委托列表可选加「公开/隐私」标识列（成交记录同）。

## 5. p5 遗留问题修正（随 p6 一并落地）

| 问题 | 现状 | p6 修正 |
|---|---|---|
| 资产流向 | Order.owner 存 operator，settle 资产转回 operator（E2E 时 operator==用户掩盖） | Order 加 trader 字段，settle/cancel 收益与退回均转 trader |
| cancel_order 缺失 | cancel.go 调 `leo execute cancel_order` 但链上无此 transition，撤单实际失败 | p6 提供 4 个 cancel transition |
| 公开余额不可下单 | balances mapping 无 record 锁仓路径 | 公开路径走 transfer_public 托管 + mapping 记账 |

## 6. 风险与限制

1. **托管信任假设**：无论公开（operator 名下公开余额）还是隐私（operator 持 records）托管，operator 都持有用户资产，属中心化托管方（诚实假设）。公开托管余额理论上任何 operator 签名交易可动用，与隐私托管风险同级。
2. **部分成交的剩余托管**：settle 只转出 match_amount/quote_out，剩余部分留在 operator（找零 record 或公开余额零头）。p5 现状即如此（Order record 一次性消费）。**剩余挂单重建（新 Order + 新托管）不在本方案内**，留待后续版本；文档先行说明。
3. **公开订单参数链上透明**：标准模式订单价格/数量/用户地址链上可见（合规叙事）；隐私模式保持加密。两者在订单簿上混合展示时，标准订单可被前端/对手方识别。
4. **order_id 冲突**：u128 前端生成（毫秒时间戳），mapping contains 断言防覆盖；引擎侧 order_id 唯一性校验保留。
5. **u64 乘法溢出**：`price*amount` 需 6 位精度乘积 < u64::MAX（1.8e19），即 price*amount < 1.8e13 微单位²——实际量级安全，但 Leo 编译会带溢出检查，测试覆盖。
6. **公开转账余额不足**：transfer_public finalize 内 sub 下溢整笔回滚（天然原子性），前端预校验余额给出友好提示。
7. **Aleo async 跨程序调用**：`credits.aleo::transfer_public` / `test_usdcx_stablecoin.aleo::transfer_public` 均确认存在（已从链上程序源码核对），caller 语义 = 交易发起者（用户下单 / operator 结算），与设计一致；合约需 `leo build` + 本地 `leo test` 实测。
8. **公开托管与隐私托管混合簿**：撮合引擎不感知 mode（match.Order 无差异），仅结算路由感知——保证撮合逻辑零改动。

## 7. 测试矩阵（E2E）

| # | 场景 | 预期 |
|---|---|---|
| 1 | 标准买（公开 USDCX）挂单 | operator 公开 USDCX 增加，mapping 有单，引擎免解密入簿 |
| 2 | 隐私买挂单 | 现有行为不变 |
| 3 | 标准卖 / 隐私卖挂单 | 同上 |
| 4 | 买公开 × 卖公开成交 | settle_pp：双方公开余额变动正确 |
| 5 | 买隐私 × 卖公开成交 | settle_vp：买方收公开 ALEO、卖方收隐私 USDCX Token |
| 6 | 买公开 × 卖隐私成交 | settle_pv：买方收隐私 credits、卖方收公开 USDCX |
| 7 | 买隐私 × 卖隐私成交 | settle_vv：现有行为，资产进 trader 而非 operator |
| 8 | 4 种撤单 | 资产全额退回用户，订单簿移除 |
| 9 | 公开订单重复 settle / 重复 cancel | status 状态机拒绝（失败回滚） |
| 10 | 公开余额不足下单 | 链上回滚 + 前端提示 |

## 8. 实施顺序

1. **p6 合约**：按 §2 写 main.leo（改程序名 p6）→ `leo build` → 本地 `leo test`（4 下单 + 4 settle + 4 cancel）→ testnet 部署
2. **引擎**：extract.go 公开路径 → orderpool mode → settlement 路由 → cancel 路由 → 单测（用实测公开下单交易）→ `go build ./...`
3. **前端**：shield.ts 公开下单 → PositionsPanel 余额随模式切换 → OrderForm 引导 → `npm run build`
4. **E2E**：§7 矩阵逐项过（用户两个 testnet 账户 A/B 模拟买卖对手方）
5. 更新 README 实施进度表
