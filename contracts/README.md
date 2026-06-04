# contracts

从 `AnuBookDEX-contracts` 编译后生成的 ABI 和 Go 绑定代码。

> Solidity 合约源码位于 `../AnuBookDEX-contracts/contracts/`

## 合约清单（7 个）

| 合约 | 职责 |
|------|------|
| `OrderBookRegistry` | 交易对注册 + Type 103 隐私订单事件 |
| `Settlement` | 批量撮合结算 + 0x0100 ZK 证明验证 |
| `LeverageManager` | 杠杆 1-10x + 保证金 + 强平 |
| `DarkPoolRouter` | MPC 暗池轮次协调 |
| `ZKKYC` | 4 级隐私 KYC + 合规审计 |
| `LiquidityRouter` | AnuBook ↔ RocketSwap AMM 路由 |
| `LPMiningRewards` | LP 质押 + 手续费分红 + ANUB 挖矿 |

## 工作流

```bash
# 1. 编译合约（在 AnuBookDEX-contracts 中）
cd ../AnuBookDEX-contracts
npx hardhat compile

# 2. 导出 ABI 到 Go 项目
npm run export-abi

# 3. 生成 Go 绑定
cd ../AnuBookDEX
abigen --abi=contracts/abi/OrderBookRegistry.json --pkg=bindings --out=contracts/bindings/order_book_registry.go
abigen --abi=contracts/abi/Settlement.json --pkg=bindings --out=contracts/bindings/settlement.go
abigen --abi=contracts/abi/LeverageManager.json --pkg=bindings --out=contracts/bindings/leverage_manager.go
abigen --abi=contracts/abi/DarkPoolRouter.json --pkg=bindings --out=contracts/bindings/dark_pool_router.go
abigen --abi=contracts/abi/ZKKYC.json --pkg=bindings --out=contracts/bindings/zkkyc.go
abigen --abi=contracts/abi/LiquidityRouter.json --pkg=bindings --out=contracts/bindings/liquidity_router.go
abigen --abi=contracts/abi/LPMiningRewards.json --pkg=bindings --out=contracts/bindings/lp_mining_rewards.go
```

## 目录

| 目录 | 内容 |
|------|------|
| `abi/` | 编译后的 ABI JSON 文件（由 AnuBookDEX-contracts 导出） |
| `bindings/` | `abigen` 生成的 Go 类型安全绑定代码 |
