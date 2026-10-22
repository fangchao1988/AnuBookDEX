import { ethers } from "hardhat";

/**
 * 单独部署 Settlement 到 Anubis Chain（主网 / 测试网通用）
 *
 * 用法:
 *   npx hardhat run scripts/deploy-settlement.ts --network anubisMainnet
 *
 * 构造参数 (makerFeeBps, takerFeeBps, protocolFeeBps) 可用环境变量覆盖:
 *   MAKER_FEE_BPS / TAKER_FEE_BPS / PROTOCOL_FEE_BPS
 * 默认 5 / 15 / 30（与 deploy.ts 一致；均 ≤ 5%，可后续用 setFees 修改）。
 *
 * 如需在部署时授权引擎地址，设置环境变量 ENGINE_ADDRESS；
 * 否则部署后用 authorizeEngine(<engine>, true) 单独授权。
 */
async function main() {
  const [deployer] = await ethers.getSigners();
  const balance = await ethers.provider.getBalance(deployer.address);

  const makerFeeBps   = BigInt(process.env.MAKER_FEE_BPS   ?? "5");
  const takerFeeBps   = BigInt(process.env.TAKER_FEE_BPS   ?? "15");
  const protocolFeeBps = BigInt(process.env.PROTOCOL_FEE_BPS ?? "30");

  console.log("Network:        ", (await ethers.provider.getNetwork()).chainId.toString());
  console.log("Deployer:       ", deployer.address);
  console.log("Balance:        ", ethers.formatEther(balance), "ANUB");
  console.log("Fees (bps):     ", { makerFeeBps, takerFeeBps, protocolFeeBps }, "\n");

  // ─── 部署 Settlement ────────────────────────────
  const Settlement = await ethers.getContractFactory("Settlement");
  const settlement = await Settlement.deploy(makerFeeBps, takerFeeBps, protocolFeeBps);
  await settlement.waitForDeployment();
  const settlementAddr = await settlement.getAddress();

  console.log("\n✅ Settlement deployed:", settlementAddr);
  console.log("   tx hash:", settlement.deploymentTransaction()?.hash);
  console.log("   owner:  ", await settlement.owner());
  console.log("   makerFeeBps   :", (await settlement.makerFeeBps()).toString());
  console.log("   takerFeeBps   :", (await settlement.takerFeeBps()).toString());
  console.log("   protocolFeeBps:", (await settlement.protocolFeeBps()).toString());

  // ─── （可选）授权引擎 ────────────────────────────
  const engineAddress = process.env.ENGINE_ADDRESS;
  if (engineAddress) {
    await settlement.authorizeEngine(engineAddress, true);
    console.log("   engine authorized:", engineAddress);
  } else {
    console.log("   (no ENGINE_ADDRESS set; call authorizeEngine(<engine>, true) later)");
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
