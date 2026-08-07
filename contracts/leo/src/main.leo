// AnuBookDEX Aleo 程序（Phase 2：record 私有模型）
//
// record 默认私有：链上只见密文，仅 owner 用 view key 可解密。
// record 声明在 program 块内（struct 在块外）；`let` 默认可变 + 类型推断。
// Leo 4.4 finalization：`return final { ... };` 内联。
// MVP：settle 全额成交；资金锁定：买单锁 quote 金额（price*amount）、卖单锁 base 数量。
// 程序名 anubook_dex_p2（Phase 1 公开 mapping 版 anubook_dex 已部署且 @noupgrade 不可覆盖）。

program anubook_dex_p2.aleo {

    record Token {
        owner: address,
        amount: u64,
        token_id: u32,   // 1=ETH, 2=USDT ...
    }

    record Order {
        owner: address,          // 订单持有者（MVP：operator 托管）
        order_id: u128,
        side: u8,                // 0=buy, 1=sell
        price: u64,
        amount: u64,             // 剩余量（MVP：挂单量）
        base_token: u32,
        quote_token: u32,
        deadline: u32,
    }

    // 公开审计承诺（不含订单细节）
    mapping settled_commit: u64 => field;

    @noupgrade
    constructor() {}

    // 演示用：铸造测试 Token（生产环境由资产发行方/桥控制铸造权限）
    fn mint(amount: u64, token_id: u32) -> (Token, Final) {
        return (
            Token { owner: std::ctx::signer(), amount: amount, token_id: token_id },
            final {},
        );
    }

    // 下单：用户锁仓 Token，铸出归 operator 的 Order record
    fn place_order(
        fund: Token,
        order_id: u128,
        side: u8,
        price: u64,
        amount: u64,
        base_token: u32,
        quote_token: u32,
        deadline: u32,
        operator: address,
    ) -> (Order, Final) {
        // 锁仓匹配：买单锁 quote 金额 price*amount，卖单锁 base 数量 amount
        assert_eq(
            (side == 0u8 && fund.token_id == quote_token && fund.amount == price * amount)
                || (side == 1u8 && fund.token_id == base_token && fund.amount == amount),
            true
        );
        return (
            Order {
                owner: operator,
                order_id: order_id,
                side: side,
                price: price,
                amount: amount,
                base_token: base_token,
                quote_token: quote_token,
                deadline: deadline,
            },
            final {},
        );
    }

    // 撮合结算：消费 maker（买单）+ taker（卖单），产出双方 Token（含找零）
    // 约定：调用方保证 maker 是买单、taker 是卖单
    fn settle(
        maker: Order,
        taker: Order,
        match_price: u64,
        match_amount: u64,
    ) -> (Token, Token, Token, Token, Final) {
        // 不变量 1：方向（maker=买、taker=卖）
        assert_eq(maker.side, 0u8);
        assert_eq(taker.side, 1u8);
        // 不变量 2：交易对一致
        assert_eq(maker.base_token == taker.base_token, true);
        assert_eq(maker.quote_token == taker.quote_token, true);
        // 不变量 3：成交价在双方限价内（买单价 >= 成交价 >= 卖单价）
        assert_eq(match_price <= maker.price, true);
        assert_eq(match_price >= taker.price, true);
        // 不变量 4：成交量不超双方剩余量（MVP 全额成交）
        assert_eq(match_amount == maker.amount, true);
        assert_eq(match_amount == taker.amount, true);
        // 不变量 5：token 守恒（成交额 = price * amount）
        let quote_out = match_price * match_amount;
        // buyer（maker）付 quote 得 base；seller（taker）付 base 得 quote
        return (
            Token { owner: maker.owner, amount: match_amount,            token_id: maker.base_token },
            Token { owner: maker.owner, amount: maker.price * maker.amount - quote_out, token_id: maker.quote_token },  // 买单找零
            Token { owner: taker.owner, amount: quote_out,               token_id: taker.quote_token },
            Token { owner: taker.owner, amount: taker.amount - match_amount, token_id: taker.base_token },  // 卖单找零（全额=0）
            final {
                // 公开审计承诺：hash 成交额（不含订单/双方细节，链上可审计不可还原）
                let h = BHP256::hash_to_field(match_price * match_amount);
                settled_commit.set(std::ctx::block_height() as u64, h);
            },
        );
    }

    // 撤单：消费 Order，返还锁定资产（买单返 quote 金额、卖单返 base 数量）
    // deadline 校验在 final 块（proof context 读不到链上 block.height）
    fn cancel_order(order: Order) -> (Token, Final) {
        let refund_amount = order.price * order.amount;
        let token_id = order.base_token;
        if order.side == 0u8 {
            token_id = order.quote_token;
        } else {
            refund_amount = order.amount;
        }
        // final 块不能捕获 record，提前提取字段
        let dl = order.deadline;
        return (
            Token {
                owner: order.owner,
                amount: refund_amount,
                token_id: token_id,
            },
            final {
                assert_eq(std::ctx::block_height() <= dl, true);
            },
        );
    }
}
