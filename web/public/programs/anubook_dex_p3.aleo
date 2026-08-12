program anubook_dex_p3.aleo;

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

mapping settled_commit:
    key as u64.public;
    value as field.public;

function mint:
    input r0 as u64.private;
    input r1 as u32.private;
    cast self.signer r0 r1 into r2 as Token.record;
    output r2 as Token.record;

function place_order:
    input r0 as Token.record;
    input r1 as u128.private;
    input r2 as u8.private;
    input r3 as u64.private;
    input r4 as u64.private;
    input r5 as u32.private;
    input r6 as u32.private;
    input r7 as u32.private;
    input r8 as address.private;
    is.eq r2 0u8 into r9;
    is.eq r0.token_id r6 into r10;
    and r9 r10 into r11;
    mul r3 r4 into r12;
    is.eq r0.amount r12 into r13;
    and r11 r13 into r14;
    is.eq r2 1u8 into r15;
    is.eq r0.token_id r5 into r16;
    and r15 r16 into r17;
    is.eq r0.amount r4 into r18;
    and r17 r18 into r19;
    or r14 r19 into r20;
    assert.eq r20 true;
    cast r8 r1 r2 r3 r4 r5 r6 r7 into r21 as Order.record;
    output r21 as Order.record;

function settle:
    input r0 as Order.record;
    input r1 as Order.record;
    input r2 as u64.private;
    input r3 as u64.private;
    assert.eq r0.side 0u8;
    assert.eq r1.side 1u8;
    is.eq r0.base_token r1.base_token into r4;
    assert.eq r4 true;
    is.eq r0.quote_token r1.quote_token into r5;
    assert.eq r5 true;
    lte r2 r0.price into r6;
    assert.eq r6 true;
    gte r2 r1.price into r7;
    assert.eq r7 true;
    is.eq r3 r0.amount into r8;
    assert.eq r8 true;
    is.eq r3 r1.amount into r9;
    assert.eq r9 true;
    mul r2 r3 into r10;
    cast r0.owner r3 r0.base_token into r11 as Token.record;
    mul r0.price r0.amount into r12;
    sub r12 r10 into r13;
    cast r0.owner r13 r0.quote_token into r14 as Token.record;
    cast r1.owner r10 r1.quote_token into r15 as Token.record;
    sub r1.amount r3 into r16;
    cast r1.owner r16 r1.base_token into r17 as Token.record;
    async settle r2 r3 into r18;
    output r11 as Token.record;
    output r14 as Token.record;
    output r15 as Token.record;
    output r17 as Token.record;
    output r18 as anubook_dex_p3.aleo/settle.future;

finalize settle:
    input r0 as u64.public;
    input r1 as u64.public;
    mul r0 r1 into r2;
    hash.bhp256 r2 into r3 as field;
    cast block.height into r4 as u64;
    set r3 into settled_commit[r4];

function cancel_order:
    input r0 as Order.record;
    mul r0.price r0.amount into r1;
    is.eq r0.side 0u8 into r2;
    ternary r2 r0.quote_token r0.base_token into r3;
    ternary r2 r1 r0.amount into r4;
    cast r0.owner r4 r3 into r5 as Token.record;
    async cancel_order r0.deadline into r6;
    output r5 as Token.record;
    output r6 as anubook_dex_p3.aleo/cancel_order.future;

finalize cancel_order:
    input r0 as u32.public;
    lte block.height r0 into r1;
    assert.eq r1 true;

constructor:
    assert.eq edition 0u16;
