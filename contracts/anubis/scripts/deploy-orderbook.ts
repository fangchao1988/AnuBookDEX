import { ethers } from "hardhat";

/**
 * 单独部署 OrderBookRegistry 到 Anubis Chain（主网 / 测试网通用）
 *
 * 用法:
 *   npx hardhat run scripts/deploy-orderbook.ts --network anubisMainnet
 *
 * 部署后 owner 即为 deployer。如需注册初始交易对，在脚本末尾取消注释。
 */
async function main() {
  const [deployer] = await ethers.getSigners();
  const balance = await ethers.provider.getBalance(deployer.address);

  console.log("Network:        ", (await ethers.provider.getNetwork()).chainId.toString());
  console.log("Deployer:       ", deployer.address);
  console.log("Balance:        ", ethers.formatEther(balance), "ANUB\n");

  // ─── 部署 OrderBookRegistry ─────────────────────
  const Registry = await ethers.getContractFactory("OrderBookRegistry");
  const registry = await Registry.deploy();
  await registry.waitForDeployment();
  const registryAddr = await registry.getAddress();

  console.log("\n✅ OrderBookRegistry deployed:", registryAddr);
  console.log("   tx hash:", registry.deploymentTransaction()?.hash);
  console.log("   owner:  ", await registry.owner());

  // ─── （可选）注册初始交易对 ────────────────────
  // 取消下方注释以在部署时注册默认交易对：
  //
  // await registry.registerPair("ETH_USDT", "ETH", "USDT", 2, 6, 100, 100_000n);
  // console.log("   Pair: ETH_USDT registered");
  // await registry.registerPair("BTC_USDT", "BTC", "USDT", 1, 8, 10, 10_000n);
  // console.log("   Pair: BTC_USDT registered");
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
