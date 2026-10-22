import { ethers } from "hardhat";

/**
 * AnuBookDEX 真实代币结算 E2E 前置：准备两个测试用户。
 *
 * 用法:
 *   npx hardhat run scripts/setup-e2e-users.ts --network anubisTestnet
 *
 * 做四件事（每个测试用户）：
 *   1. 从 operator 转少量 DAI 当 gas
 *   2. mint TETH / TUSDT 测试币
 *   3. approve Settlement 合约
 *   4. deposit 到 Settlement 金库（买方备 quote，卖方备 base）
 *
 * 测试私钥为公开的 Hardhat 测试账号（仅测试网使用）。
 * 结束后打印两个地址 + 下单命令，配合 curl 验证结算划转。
 */
const SETTLEMENT = process.env.SETTLEMENT ?? "0xB11c339001b8db3827aD3047BA8Beb07Ca60e9Af";
const TETH = process.env.TETH ?? "0x0Ac44Ef43ED7086895196D05d7B51fda3038Be99";
const TUSDT = process.env.TUSDT ?? "0x7233112f0FE25ae88b3dE0e0dFE5215E207a2aF0";

/**
 * Cheras 测试网的 gas 代币 DAI 是 **ERC-20**，不是可转账的原生币。
 * 实测：任何 value>0 的原生转账都会被节点拒绝
 *   "Native token not supported; use DAI for gas and send via ERC20 transfer."
 * 且 `eth_getBalance(addr)` 返回的正是该 ERC-20 的余额（两者数值一致）。
 * 所以要给测试账户发 gas，必须走 DAI.transfer()。
 */
const DAI = process.env.DAI ?? "0x83fd06F0846d9D90B3016bF670Efe2E0B11cDe14";
const DAI_ABI = [
  "function transfer(address to, uint256 amount) returns (bool)",
  "function balanceOf(address) view returns (uint256)",
];

// Hardhat 公开测试账号 #1 / #2（测试网专用，勿用于生产）
const KEYS = [
  "0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d",
  "0x5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a",
];

const GAS_DAI = ethers.parseEther("0.01"); // 每个用户 0.01 DAI 作 gas
const BASE_AMOUNT = 100n; // 卖方备 100 TETH
const QUOTE_AMOUNT = 20000n; // 买方备 20000 TUSDT（价格 200 × 100）

async function main() {
  const [operator] = await ethers.getSigners();
  const provider = ethers.provider;
  const net = await provider.getNetwork();
  console.log(`Network ${net.chainId} | operator ${operator.address}\n`);

  const settlement = await ethers.getContractAt("Settlement", SETTLEMENT);
  const teth = await ethers.getContractAt("TestToken", TETH);
  const tusdt = await ethers.getContractAt("TestToken", TUSDT);

  const users = KEYS.map((k) => new ethers.Wallet(k, provider));

  for (const [i, u] of users.entries()) {
    const label = i === 0 ? "买方(buyer)" : "卖方(seller)";
    console.log(`── 用户${i + 1} ${label}: ${u.address}`);

    // 1. gas（DAI 是 ERC-20，必须走 transfer；原生转账会被节点拒绝）
    const dai = new ethers.Contract(DAI, DAI_ABI, operator);
    const bal = await (dai.balanceOf as any)(u.address) as bigint;
    if (bal < GAS_DAI / 2n) {
      const need = GAS_DAI - bal;
      const tx = await (dai.transfer as any)(u.address, need);
      await tx.wait();
      console.log(`   ✓ gas 充值 ${ethers.formatEther(need)} DAI (ERC-20 transfer)`);
    } else {
      console.log(`   · gas 充足 (${ethers.formatEther(bal)} DAI)`);
    }

    // 2. mint 测试币
    for (const [t, amt] of [
      [teth, i === 0 ? 1000n : BASE_AMOUNT],
      [tusdt, i === 0 ? QUOTE_AMOUNT : 1000n],
    ] as const) {
      const sym = await t.symbol();
      if ((await t.balanceOf(u.address)) >= amt) {
        console.log(`   · ${sym} 余额已够，跳过 mint`);
        continue;
      }
      await (await t.mint(u.address, amt)).wait();
      console.log(`   ✓ mint ${amt} ${sym}`);
    }

    // 3. approve
    for (const t of [teth, tusdt]) {
      if ((await t.allowance(u.address, SETTLEMENT)) < ethers.MaxUint256 / 2n) {
        await (await t.connect(u).approve(SETTLEMENT, ethers.MaxUint256)).wait();
        console.log(`   ✓ approve ${await t.symbol()}`);
      }
    }

    // 4. deposit 到金库
    const depositTargets =
      i === 0
        ? ([[tusdt, QUOTE_AMOUNT]] as const) // 买方备 quote
        : ([[teth, BASE_AMOUNT]] as const); // 卖方备 base

    for (const [t, amt] of depositTargets) {
      const sym = await t.symbol();
      const addr = await t.getAddress();
      if ((await settlement.vaultBalance(addr, u.address)) >= amt) {
        console.log(`   · ${sym} 金库余额已够，跳过 deposit`);
        continue;
      }
      await (await settlement.connect(u).deposit(addr, amt)).wait();
      console.log(`   ✓ deposit ${amt} ${sym} → 金库`);
    }
    console.log();
  }

  // 汇总
  console.log("── 金库状态 ─────────────────────────────");
  for (const [i, u] of users.entries()) {
    const label = i === 0 ? "买方" : "卖方";
    console.log(
      `${label} ${u.address}\n` +
        `   TETH  ${await settlement.vaultBalance(TETH, u.address)}\n` +
        `   TUSDT ${await settlement.vaultBalance(TUSDT, u.address)}`,
    );
  }

  console.log("\n── 下单验证（引擎 9000）─────────────────");
  console.log(`curl -X POST http://localhost:9000/order -H 'Content-Type: application/json' \\
  -d '{"order_id":1,"symbol":"ETH_USDT","side":0,"price":"200","amount":"100","deadline":2000000000,"trader":"${users[0].address}","mode":"public"}'`);
  console.log(`curl -X POST http://localhost:9000/order -H 'Content-Type: application/json' \\
  -d '{"order_id":2,"symbol":"ETH_USDT","side":1,"price":"200","amount":"100","deadline":2000000000,"trader":"${users[1].address}","mode":"public"}'`);
}

main().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
