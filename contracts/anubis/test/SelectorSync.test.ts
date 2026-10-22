import { expect } from "chai";
import { ethers } from "hardhat";
import * as fs from "fs";
import * as path from "path";

/**
 * 前端手写的函数选择器必须与编译产物 ABI 一致。
 *
 * 背景：web/src/lib/wallet/evm.ts 为避免引入 ethers/viem 依赖，手写了
 * ERC-20 / Settlement 方法选择器。曾经把 vaultBalance 写成 0x8b8e6b8a
 * （实际 0x152bceca），这类错误不会有编译报错，只会在链上调用时静默失败。
 * 本测试把 web 源码里的常量与 ABI 对账。
 */
describe("前端选择器与 ABI 同步", function () {
  const WEB_EVM = path.join(__dirname, "..", "..", "..", "web", "src", "lib", "wallet", "evm.ts");

  function readSelectors(): Record<string, string> {
    const src = fs.readFileSync(WEB_EVM, "utf-8");
    const out: Record<string, string> = {};
    // 匹配形如  name: '0x12345678', // signature
    const re = /(\w+):\s*'(0x[0-9a-fA-F]{8})',\s*\/\/\s*([\w(),\[\]]+)/g;
    let m: RegExpExecArray | null;
    while ((m = re.exec(src)) !== null) {
      out[m[3]] = m[2].toLowerCase(); // 以签名为键
    }
    return out;
  }

  it("ERC-20 与 Settlement 选择器与 ABI 一致", async function () {
    const selectors = readSelectors();
    expect(Object.keys(selectors).length).to.be.greaterThan(0, "未从 evm.ts 解析到任何选择器");

    // Settlement ABI（编译产物）
    const settlementArtifact = await ethers.getContractFactory("Settlement");
    const settlementIface = settlementArtifact.interface;
    // ERC-20 标准方法（以 OZ IERC20 为准）
    const erc20Iface = new ethers.Interface([
      "function balanceOf(address) view returns (uint256)",
      "function allowance(address,address) view returns (uint256)",
      "function approve(address,uint256) returns (bool)",
    ]);

    let checked = 0;
    for (const [sig, got] of Object.entries(selectors)) {
      let want: string | null = null;
      try {
        want = settlementIface.getFunction(sig)?.selector ?? null;
      } catch {
        /* 不在 Settlement ABI 中 */
      }
      if (!want) {
        try {
          want = erc20Iface.getFunction(sig)?.selector ?? null;
        } catch {
          /* 不在 ERC-20 中 */
        }
      }
      expect(want, `签名 ${sig} 未在 ABI 中找到`).to.not.equal(null);
      expect(got, `${sig} 选择器不一致`).to.equal(want!.toLowerCase());
      checked++;
    }
    console.log(`  已校验 ${checked} 个选择器`);
  });
});
