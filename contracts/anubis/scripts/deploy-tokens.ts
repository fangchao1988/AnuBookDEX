import { ethers } from "hardhat";

/**
 * 部署测试用 ERC-20（TestToken）到 Anubis 链。
 *
 * 用法:
 *   npx hardhat run scripts/deploy-tokens.ts --network anubisTestnet
 *
 * 说明:
 *   - decimals = 0：AnuBookDEX 引擎/前端按「整数单位」下单（1 单位 = 1 代币），
 *     0 位小数可让订单里的 price/amount 与链上代币单位一一对应，无需换算。
 *   - 任何人可 mint（测试网水龙头），受 maxSupply 上限约束。
 *   - 部署后把地址填入 conf/config.yaml 的 chain.anubis.pair-tokens。
 */
const TOKENS = [
  { name: "Test ETH", symbol: "TETH", decimals: 0, maxSupply: 1_000_000 },
  { name: "Test USDT", symbol: "TUSDT", decimals: 0, maxSupply: 10_000_000 },
  { name: "Test SOL", symbol: "TSOL", decimals: 0, maxSupply: 1_000_000 },
];

async function main() {
  const [deployer] = await ethers.getSigners();
  console.log("Network:  ", (await ethers.provider.getNetwork()).chainId.toString());
  console.log("Deployer: ", deployer.address);
  console.log("Balance:  ", ethers.formatEther(await ethers.provider.getBalance(deployer.address)), "DAI\n");

  const TestToken = await ethers.getContractFactory("TestToken");
  const deployed: Record<string, string> = {};

  for (const t of TOKENS) {
    const c = await TestToken.deploy(t.name, t.symbol, t.decimals, t.maxSupply);
    await c.waitForDeployment();
    const addr = await c.getAddress();
    deployed[t.symbol] = addr;
    console.log(`✅ ${t.symbol.padEnd(6)} ${addr}  (decimals=${t.decimals}, max=${t.maxSupply})`);
  }

  console.log("\n── 填入 conf/config.yaml ─────────────────────");
  console.log(`chain:
  anubis:
    pair-tokens:
      ETH_USDT:
        base:  "${deployed.TETH}"
        quote: "${deployed.TUSDT}"
      SOL_USDT:
        base:  "${deployed.TSOL}"
        quote: "${deployed.TUSDT}"`);
  console.log("──────────────────────────────────────────────");
  console.log("\n给用户发测试币: npx hardhat run scripts/mint-tokens.ts --network anubisTestnet");
}

main().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
