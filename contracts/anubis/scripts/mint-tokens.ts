import { ethers } from "hardhat";

/**
 * 给指定地址铸测试币（水龙头）。
 *
 * 用法:
 *   TOKENS=0xAAA,0xBBB TO=0x用户地址 AMOUNT=10000 \
 *     npx hardhat run scripts/mint-tokens.ts --network anubisTestnet
 *
 * 默认 TO 为部署者地址；TOKENS 缺省时读取 conf 中的常用地址（需手动传入）。
 * 每个代币铸 AMOUNT（默认 10000，0 位小数即 10000 枚）。
 */
async function main() {
  const [deployer] = await ethers.getSigners();
  const to = process.env.TO || deployer.address;
  const amount = BigInt(process.env.AMOUNT ?? "10000");
  const tokens = (process.env.TOKENS ?? "").split(",").map((s) => s.trim()).filter(Boolean);

  if (tokens.length === 0) {
    throw new Error(
      "Set TOKENS=0x...,0x... （测试币地址，见 deploy-tokens.ts 输出）\n" +
        "可选：TO=0x用户地址 AMOUNT=10000",
    );
  }

  console.log("Mint to: ", to);
  console.log("Amount:  ", amount.toString(), "每币种\n");

  const TestToken = await ethers.getContractFactory("TestToken");
  for (const addr of tokens) {
    const c = TestToken.attach(addr);
    const sym = await c.symbol();
    const tx = await c.mint(to, amount);
    await tx.wait();
    console.log(`✅ ${sym.padEnd(6)} ${addr} → ${to}  mint ${amount}`);
    console.log(`   balance now: ${(await c.balanceOf(to)).toString()}`);
  }
}

main().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
