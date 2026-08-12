program anubook_dex_p2.aleo;

record Token:
    owner as address.private;
    amount as u64.private;
    token_id as u32.private;

record Order:
    owner as address.private;
    order_id as u128.private;
    side as u8.private;
    price as u64.private;
    amount as u64.private;
    base_token as u32.private;
    quote_token as u32.private;
    deadline as u32.private;

function mint:
    input r0 as u64.private;
    input r1 as u32.private;
    cast self.signer r0 r1 into r2 as Token.record;
    output r2 as Token.record;

function pass_token:
    input r0 as Token.record;
    cast r0.owner r0.amount r0.token_id into r1 as Token.record;
    output r1 as Token.record;

constructor:
    assert.eq edition 0u16;
