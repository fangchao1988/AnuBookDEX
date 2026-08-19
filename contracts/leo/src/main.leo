import credits.aleo;
import test_usdcx_stablecoin.aleo;

// anubook_dex_p7.aleo：公开/隐私混合下单（四种组合一次做全）。
// 设计文档：docs/Aleo公开隐私混合下单方案.md
//   - 标准下单（place_order_*_public）：公开余额 -> transfer_public 托管到 operator + public_orders mapping 记账（参数链上透明，无需 view key）
//   - 隐私下单（place_order_*_private）：record 锁仓（p5 逻辑），Order record 加密，trader 字段修正资产流向
//   - 结算 4 组合：settle_pp（双公开）/ settle_vp（买隐私×卖公开）/ settle_pv（买公开×卖隐私）/ settle_vv（双隐私）
//   - 撤单 4 变体：公开撤单退公开余额，隐私撤单退 record
// 公开订单（参数链上透明，存 mapping 不铸 record）
struct PublicOrder {
    user: address,      // 下单用户（收益/退回接收人）
    operator: address,  // 托管 operator（撤单权限校验用）
    side: u8,
    price: u64,
    amount: u64,
    deadline: u32,
    status: u8,         // 0=active, 1=settled, 2=canceled（防重放状态机）
}

program anubook_dex_p7.aleo {

    // 隐私订单 record（p5 基础上加 trader 字段：结算收益与撤单退回的接收人）。
    // owner 为 operator（record 归属，operator 签名 spend 做 settle/cancel）
    record Order {
        owner: address,     // operator
        trader: address,    // 下单用户（收益/退回接收人）
        order_id: u128,
        side: u8,           // 0=buy(付 USDCX 得 ALEO), 1=sell(付 ALEO 得 USDCX)
        price: u64,         // 单价(最小单位,6 位)
        amount: u64,        // 数量(最小单位,6 位)
        deadline: u32,
    }

    mapping public_orders: field => PublicOrder; // order_id -> 公开订单

    @noupgrade
    constructor() {}

    // ============ 标准（公开）下单 ============

    // 标准买：用户公开 USDCX -> operator 公开托管（from=caller 用户），mapping 记账。
    // transfer_public 在 final 块中 run：余额不足整笔回滚。
    // 标准买（两阶段）：用户先直调 test_usdcx_stablecoin.aleo::transfer_public(operator, needed)
    // 托管公开 USDCX（await 外部 transfer_public 在 testnet 不可行——finalize 输入错位，
    // 实测 rejected），本函数只记 mapping。needed = price*amount/1e6 由调用者保证与转账一致。
    fn place_order_buy_public(order_id: u128, price: u64, amount: u64, deadline: u32, operator: address) -> Final {
        assert(amount > 0u64);
        let caller: address = std::ctx::caller();
        return
        final {
            // contains 断言防 order_id 冲突覆盖
            assert_eq(public_orders.contains(order_id as field), false);
            public_orders.set(order_id as field, PublicOrder { user: caller, operator: operator, side: 0u8, price: price, amount: amount, deadline: deadline, status: 0u8 });
        };
    }

    // 标准卖：用户公开 ALEO credits -> operator 公开托管
    // 标准卖（两阶段）：用户先直调 credits.aleo::transfer_public(operator, amount) 托管公开 ALEO，
    // 本函数只记 mapping
    fn place_order_sell_public(order_id: u128, price: u64, amount: u64, deadline: u32, operator: address) -> Final {
        assert(amount > 0u64);
        let caller: address = std::ctx::caller();
        return
        final {
            assert_eq(public_orders.contains(order_id as field), false);
            public_orders.set(order_id as field, PublicOrder { user: caller, operator: operator, side: 1u8, price: price, amount: amount, deadline: deadline, status: 0u8 });
        };
    }

    // ============ 隐私下单（p5 逻辑 + trader 字段） ============

    // 隐私买：用户 USDCX Token + Credentials -> operator 托管（transfer_private_with_creds），铸 Order
    fn place_order_buy_private(
        fund: test_usdcx_stablecoin.aleo::Token,
        creds: test_usdcx_stablecoin.aleo::Credentials,
        order_id: u128,
        price: u64,
        amount: u64,
        deadline: u32,
        operator: address,
    ) -> (Order, test_usdcx_stablecoin.aleo::ComplianceRecord, test_usdcx_stablecoin.aleo::Token, test_usdcx_stablecoin.aleo::Token, test_usdcx_stablecoin.aleo::Credentials, Final) {
        // 锁 USDCX 金额 = price * amount（USDCX u128）；支持 fund 大于托管额（多出部分找零回用户）
        let needed = ((price * amount) / 1000000u64) as u128;
        assert(fund.amount >= needed);
        let f = test_usdcx_stablecoin.aleo::transfer_private_with_creds(operator, needed, fund, creds);
        return (
            Order { owner: operator, trader: std::ctx::caller(), order_id: order_id, side: 0u8, price: price, amount: amount, deadline: deadline },
            f.0, f.1, f.2, f.3,
            final { f.4.run(); },
        );
    }

    // 隐私卖：用户 ALEO credits -> operator 托管（credits transfer_private），铸 Order
    fn place_order_sell_private(
        fund: credits.aleo::credits,
        order_id: u128,
        price: u64,
        amount: u64,
        deadline: u32,
        operator: address,
    ) -> (Order, credits.aleo::credits, credits.aleo::credits) {
        // 支持 fund 大于托管额（多出部分找零回用户，f.1 即找零）
        assert(fund.microcredits >= amount);
        let f = credits.aleo::transfer_private(fund, operator, amount);
        return (
            Order { owner: operator, trader: std::ctx::caller(), order_id: order_id, side: 1u8, price: price, amount: amount, deadline: deadline },
            f.0, f.1,
        );
    }

    // ============ 结算（4 组合；maker=买、taker=卖） ============

    // settle_pp（买公开 × 卖公开，两阶段）：operator 已先直调
    // credits.aleo::transfer_public(maker.user, amount) + test_usdcx_stablecoin.aleo::transfer_public(taker.user, quote_out)
    // （await 外部 transfer_public 在 testnet 不可行，实测 rejected）；本函数只校验并标记 status。
    // maker_owner/taker_owner 由 operator 传入，final 块校验与 mapping 一致（防篡改收款人）
    fn settle_pp(
        maker_order_id: u128,
        taker_order_id: u128,
        maker_owner: address,
        taker_owner: address,
        match_price: u64,
        match_amount: u64,
    ) -> Final {
        assert(match_amount > 0u64);
        return
        final {
            let maker: PublicOrder = public_orders.get(maker_order_id as field);
            let taker: PublicOrder = public_orders.get(taker_order_id as field);
            assert_eq(maker.user, maker_owner);
            assert_eq(taker.user, taker_owner);
            assert_eq(maker.side, 0u8);
            assert_eq(taker.side, 1u8);
            assert_eq(maker.status, 0u8);
            assert_eq(taker.status, 0u8);
            assert_eq(match_price <= maker.price, true);
            assert_eq(match_price >= taker.price, true);
            assert_eq(match_amount <= maker.amount, true);
            assert_eq(match_amount <= taker.amount, true);
            let m: PublicOrder = maker;
            m.status = 1u8;
            let t: PublicOrder = taker;
            t.status = 1u8;
            public_orders.set(maker_order_id as field, m);
            public_orders.set(taker_order_id as field, t);
        };
    }

    // settle_vp（买隐私 × 卖公开）：transition 消费 maker Order（隐私）+ op_usdcx + op_creds
    // 私有转 USDCX 给 taker；final 块校验公开 taker + 公开 ALEO 给 maker.trader
    fn settle_vp(
        maker: Order,
        op_usdcx: test_usdcx_stablecoin.aleo::Token,
        op_creds: test_usdcx_stablecoin.aleo::Credentials,
        taker_order_id: u128,
        taker_owner: address,
        match_price: u64,
        match_amount: u64,
    ) -> (test_usdcx_stablecoin.aleo::ComplianceRecord, test_usdcx_stablecoin.aleo::Token, test_usdcx_stablecoin.aleo::Token, test_usdcx_stablecoin.aleo::Credentials, Final) {
        assert_eq(maker.side, 0u8);
        assert_eq(match_price <= maker.price, true);
        assert_eq(match_amount <= maker.amount, true);
        let quote_out: u128 = ((match_price * match_amount) / 1000000u64) as u128;
        // 卖方收隐私 USDCX（带 operator 凭证）；taker_owner 由 operator 传入，final 块校验与 mapping 一致
        let f = test_usdcx_stablecoin.aleo::transfer_private_with_creds(taker_owner, quote_out, op_usdcx, op_creds);
        // 买方收公开 ALEO（两阶段）：operator 已先直调 credits.aleo::transfer_public(maker.trader, match_amount)
        return (f.0, f.1, f.2, f.3, final {
            let taker: PublicOrder = public_orders.get(taker_order_id as field);
            assert_eq(taker.user, taker_owner);   // 防 operator 篡改收款人
            assert_eq(taker.side, 1u8);
            assert_eq(taker.status, 0u8);
            assert_eq(match_price >= taker.price, true);
            assert_eq(match_amount <= taker.amount, true);
            f.4.run();
            let t: PublicOrder = taker;
            t.status = 1u8;
            public_orders.set(taker_order_id as field, t);
        });
    }

    // settle_pv（买公开 × 卖隐私）：transition 消费 taker Order（隐私）+ op_aloe，
    // 私有转 credits 给 taker.trader；final 块校验公开 maker + USDCX 公开给 taker.trader
    fn settle_pv(
        taker: Order,
        op_aloe: credits.aleo::credits,
        maker_order_id: u128,
        maker_owner: address,
        match_price: u64,
        match_amount: u64,
    ) -> (credits.aleo::credits, credits.aleo::credits, Final) {
        assert_eq(taker.side, 1u8);
        assert_eq(match_price >= taker.price, true);
        assert_eq(match_amount <= taker.amount, true);
        let f = credits.aleo::transfer_private(op_aloe, taker.trader, match_amount);
        let quote_out: u128 = ((match_price * match_amount) / 1000000u64) as u128;
        let taker_trader: address = taker.trader;
        let usdcx_out: Final = test_usdcx_stablecoin.aleo::transfer_public(taker_trader, quote_out); // 卖方收公开 USDCX（from=operator 托管余额）
        return (f.0, f.1, final {
            let maker: PublicOrder = public_orders.get(maker_order_id as field);
            assert_eq(maker.user, maker_owner);   // 防 operator 篡改收款人
            assert_eq(maker.side, 0u8);
            assert_eq(maker.status, 0u8);
            assert_eq(match_price <= maker.price, true);
            assert_eq(match_amount <= maker.amount, true);
            usdcx_out.run();
            let m: PublicOrder = maker;
            m.status = 1u8;
            public_orders.set(maker_order_id as field, m);
        });
    }

    // settle_vv（买隐私 × 卖隐私）：p5 路径，收益地址改用 trader
    fn settle_vv(
        maker: Order,
        taker: Order,
        op_aloe: credits.aleo::credits,
        op_usdcx: test_usdcx_stablecoin.aleo::Token,
        op_creds: test_usdcx_stablecoin.aleo::Credentials,
        match_price: u64,
        match_amount: u64,
    ) -> (credits.aleo::credits, credits.aleo::credits, test_usdcx_stablecoin.aleo::ComplianceRecord, test_usdcx_stablecoin.aleo::Token, test_usdcx_stablecoin.aleo::Token, test_usdcx_stablecoin.aleo::Credentials, Final) {
        // 不变量：方向、价格区间、数量、守恒
        assert_eq(maker.side, 0u8);
        assert_eq(taker.side, 1u8);
        assert_eq(match_price <= maker.price, true);
        assert_eq(match_price >= taker.price, true);
        assert_eq(match_amount <= maker.amount, true);
        assert_eq(match_amount <= taker.amount, true);
        // 成交额（USDCX）= price * amount
        let quote_out = (match_price * match_amount) / 1000000u64;
        // 买方得 ALEO（私有，给 maker.trader）
        let f1 = credits.aleo::transfer_private(op_aloe, maker.trader, match_amount);
        // 卖方得 USDCX（私有，带 operator 凭证，给 taker.trader）
        let f2 = test_usdcx_stablecoin.aleo::transfer_private_with_creds(taker.trader, (quote_out) as u128, op_usdcx, op_creds);
        return (
            f1.0, f1.1, f2.0, f2.1, f2.2, f2.3,
            final { f2.4.run(); },
        );
    }

    // ============ 撤单（4 变体） ============

    // 标准买单撤销（两阶段）：operator 已先直调 test_usdcx_stablecoin.aleo::transfer_public(user, needed)
    // 退回公开 USDCX；本函数只校验并标记 status=2。user/operator/needed 由调用者传入校验
    fn cancel_buy_public(order_id: u128, user: address, operator: address, needed: u128) -> Final {
        let caller: address = std::ctx::caller();
        return
        final {
            let o: PublicOrder = public_orders.get(order_id as field);
            assert_eq(o.user, user);
            assert_eq(o.operator, operator);
            assert_eq(o.status, 0u8);
            assert(caller == o.user || caller == o.operator);
            let oc: PublicOrder = o;
            oc.status = 2u8;
            public_orders.set(order_id as field, oc);
        };
    }

    // 标准卖单撤销（两阶段）：operator 已先直调 credits.aleo::transfer_public(user, amount)
    // 退回公开 ALEO；本函数只校验并标记 status=2
    fn cancel_sell_public(order_id: u128, user: address, operator: address, amount: u64) -> Final {
        let caller: address = std::ctx::caller();
        return
        final {
            let o: PublicOrder = public_orders.get(order_id as field);
            assert_eq(o.user, user);
            assert_eq(o.operator, operator);
            assert_eq(o.amount, amount);
            assert_eq(o.status, 0u8);
            assert(caller == o.user || caller == o.operator);
            let oc: PublicOrder = o;
            oc.status = 2u8;
            public_orders.set(order_id as field, oc);
        };
    }

    // 隐私买单撤销：operator 用 Order record + 托管 Token + 凭证全额退回用户（transfer_private_with_creds）
    fn cancel_buy_private(
        order: Order,
        op_usdcx: test_usdcx_stablecoin.aleo::Token,
        op_creds: test_usdcx_stablecoin.aleo::Credentials,
    ) -> (test_usdcx_stablecoin.aleo::ComplianceRecord, test_usdcx_stablecoin.aleo::Token, test_usdcx_stablecoin.aleo::Token, test_usdcx_stablecoin.aleo::Credentials, Final) {
        assert_eq(order.side, 0u8);
        let needed: u128 = ((order.price * order.amount) / 1000000u64) as u128;
        let f = test_usdcx_stablecoin.aleo::transfer_private_with_creds(order.trader, needed, op_usdcx, op_creds);
        return (f.0, f.1, f.2, f.3, final { f.4.run(); });
    }

    // 隐私卖单撤销：operator 用 Order record + 托管 credits 全额退回用户
    fn cancel_sell_private(
        order: Order,
        op_aloe: credits.aleo::credits,
    ) -> (credits.aleo::credits, credits.aleo::credits) {
        assert_eq(order.side, 1u8);
        let f = credits.aleo::transfer_private(op_aloe, order.trader, order.amount);
        return (f.0, f.1);
    }
}
