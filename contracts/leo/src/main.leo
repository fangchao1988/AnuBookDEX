import credits.aleo;
import test_usdcx_stablecoin.aleo;

program anubook_dex_p5.aleo {

    record Order {
        owner: address,
        order_id: u128,
        side: u8,        // 0=buy(付 USDCX 得 ALEO), 1=sell(付 ALEO 得 USDCX)
        price: u64,      // 单价(最小单位,6 位)
        amount: u64,     // 数量(最小单位,6 位)
        deadline: u32,
    }

    @noupgrade
    constructor() {}

    // 买单：用户 USDCX Token + Credentials -> operator 托管（transfer_private_with_creds），铸 Order
    fn place_order_buy(
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
            Order { owner: operator, order_id: order_id, side: 0u8, price: price, amount: amount, deadline: deadline },
            f.0, f.1, f.2, f.3,
            final { f.4.run(); },
        );
    }

    // 卖单：用户 ALEO credits -> operator 托管（credits transfer_private），铸 Order
    fn place_order_sell(
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
            Order { owner: operator, order_id: order_id, side: 1u8, price: price, amount: amount, deadline: deadline },
            f.0, f.1,
        );
    }

    // settle（maker=买、taker=卖）：双向私有分配
    fn settle(
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
        // 买方得 ALEO（私有）
        let f1 = credits.aleo::transfer_private(op_aloe, maker.owner, match_amount);
        // 卖方得 USDCX（私有，带 operator 凭证）
        let f2 = test_usdcx_stablecoin.aleo::transfer_private_with_creds(taker.owner, (quote_out) as u128, op_usdcx, op_creds);
        return (
            f1.0, f1.1, f2.0, f2.1, f2.2, f2.3,
            final { f2.4.run(); },
        );
    }
}
