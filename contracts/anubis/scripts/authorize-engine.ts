import { ethers } from "hardhat";

/**
 * 在 Settlement 上授权 / 撤销链下撮合引擎。
 *
 * 用法:
 *   ENGINE_ADDRESS=0x... [AUTHORIZED=true|false] [CONTRACT_ADDRESS=0x...] \
 *     npx hardhat run scripts/authorize-engine.ts --network anubisMainnet
 *
 * 默认 AUTHORIZED=true(授权)。设 AUTHORIZED=false 可撤销。Settlement 地址
 * 默认为已部署的主网地址，可用 CONTRACT_ADDRESS 覆盖。
 */
async function main() {
  const engine = process.env.ENGINE_ADDRESS;
  if (!engine) throw new Error("Set ENGINE_ADDRESS env var");
  const auth = (process.env.AUTHORIZED ?? "true").toLowerCase() !== "false";
  const settlementAddr = process.env.CONTRACT_ADDRESS || "0xe1E70C426fAcd33E3BABF526a0876553bb565965";

  const [caller] = await ethers.getSigners();
  const s = await ethers.getContractAt("Settlement", settlementAddr);

  const ownerOnChain = await s.owner();
  console.log("Settlement:       ", settlementAddr);
  console.log("Caller:            ", caller.address);
  console.log("Contract owner:    ", ownerOnChain);
  if (caller.address.toLowerCase() !== ownerOnChain.toLowerCase()) {
    throw new Error("Caller is not the contract owner - cannot authorize engine");
  }

  const before = await s.authorizedEngines(engine);
  console.log("Engine:            ", engine);
  console.log("Authorized before: ", before);
  console.log("Target authorized: ", auth);

  if (before === auth) {
    console.log("Already in target state - nothing to do.");
    return;
  }

  const tx = await s.connect(caller).authorizeEngine(engine, auth);
  console.log("tx hash:           ", tx.hash);
  await tx.wait();
  console.log("Authorized after:  ", await s.authorizedEngines(engine));
}

main().catch((e) => { console.error(e); process.exitCode = 1; });
