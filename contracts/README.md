# contracts

AnuBookDEX 双链合约仓库内布局：

| 目录 | 内容 |
|------|------|
| `anubis/` | **Anubis（EVM）Solidity** hardhat 工程（`contracts/*.sol`、`scripts/`、`test/`）——从独立兄弟仓库 `AnuBookDEX-contracts` 移入，后续在此开发 |
| `leo/` | **Aleo（Leo/snarkVM）** 程序（`src/main.leo` + `build/*`） |
| `abi/` | 编译后导出的 ABI JSON（供 Go 引擎读取/`abigen`） |
| `bindings/` | Go 绑定代码（`abigen` 生成或手写占位） |

## Anubis 合约（`anubis/`）

7 个 Solidity 合约：

| 合约 | 职责 |
|------|------|
| `OrderBookRegistry` | 交易对注册 + 订单提交事件（nullifier/deadline 审计锚定） |
| `Settlement` | 批量撮合结算 + 0x0100 ZK 证明验证（v2：`requireProof` 可关 = 承诺哈希模式） |
| `LeverageManager` | 杠杆 1-10x + 保证金 + 强平 |
| `DarkPoolRouter` | MPC 暗池轮次协调 |
| `ZKKYC` | 4 级隐私 KYC + 合规审计 |
| `LiquidityRouter` | AnuBook ↔ RocketSwap AMM 路由 |
| `LPMiningRewards` | LP 质押 + 手续费分红 + ANUB 挖矿 |

## 工作流

```bash
# 1. 编译合约（在 contracts/anubis/ 内）
cd contracts/anubis
npx hardhat compile
npx hardhat test            # 本地测试

# 2. 导出 ABI 到 Go 项目
npm run export-abi          # 输出到 ../abi/（anubis/package.json 的 export-abi 脚本）
# 或手动：
#   cp contracts/anubis/artifacts/contracts/*.sol/*.json ../abi/

# 3. 生成 Go 绑定
cd ../../
abigen --abi=contracts/abi/Settlement.json --pkg=bindings --out=contracts/bindings/settlement.go
# ... 其余合约同理

# 4. 部署到 Anubis 主网（chainId 6714）
cd contracts/anubis
npx hardhat run scripts/deploy-settlement.ts --network anubisMainnet
```

> 说明：`abi/` 与 `bindings/` 是 Go 引擎读取/编译用，`anubis/` 是 Solidity 源码工程（含 `node_modules/`）。
> `contracts/abi/*.json` 与 `contracts/bindings/*.go` 的路径被 Go 侧注释/文档引用，保持稳定。