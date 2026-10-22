import { expect } from "chai";
import { ethers } from "hardhat";
import { loadFixture } from "@nomicfoundation/hardhat-toolbox/network-helpers";

describe("OrderBookRegistry", function () {
  async function deployFixture() {
    const [owner, engine] = await ethers.getSigners();
    const Registry = await ethers.getContractFactory("OrderBookRegistry");
    const registry = await Registry.deploy();
    return { registry, owner, engine };
  }

  it("should deploy with correct owner", async function () {
    const { registry, owner } = await loadFixture(deployFixture);
    expect(await registry.owner()).to.equal(owner.address);
  });

  it("should register a pair", async function () {
    const { registry } = await loadFixture(deployFixture);
    await registry.registerPair("ETH_USDT", "ETH", "USDT", 2, 6, 100, 100_000);

    const pair = await registry.pairs("ETH_USDT");
    expect(pair.baseAsset).to.equal("ETH");
    expect(pair.quoteAsset).to.equal("USDT");
    expect(pair.priceScale).to.equal(2);
    expect(pair.amountScale).to.equal(6);
    expect(pair.active).to.be.true;
  });

  it("should revert on duplicate pair", async function () {
    const { registry } = await loadFixture(deployFixture);
    await registry.registerPair("ETH_USDT", "ETH", "USDT", 2, 6, 100, 100_000);
    await expect(
      registry.registerPair("ETH_USDT", "ETH", "USDT", 2, 6, 100, 100_000)
    ).to.be.revertedWith("Pair exists");
  });

  it("should set pair active status", async function () {
    const { registry } = await loadFixture(deployFixture);
    await registry.registerPair("ETH_USDT", "ETH", "USDT", 2, 6, 100, 100_000);
    await registry.setPairActive("ETH_USDT", false);

    const pair = await registry.pairs("ETH_USDT");
    expect(pair.active).to.be.false;
  });

  it("should revert pair operations from non-owner", async function () {
    const { registry, engine } = await loadFixture(deployFixture);
    await expect(
      registry.connect(engine).registerPair("LINK_USDT", "LINK", "USDT", 2, 6, 100, 100_000)
    ).to.be.revertedWith("Only owner");
  });
});

describe("Settlement", function () {
  const TAKER_BPS = 15n;
  const BPS = 10000n;
  const STRUCT =
    "tuple(uint64 id,bytes32 orderId,address user,address baseToken,address quoteToken,uint256 price,uint256 amount,uint256 cost,uint8 side,bytes32 nullifier,string role)[]";

  async function deployFixture() {
    const [owner, engine, alice, bob] = await ethers.getSigners();
    const Settlement = await ethers.getContractFactory("Settlement");
    const settlement = await Settlement.deploy(5, Number(TAKER_BPS), 30);
    await settlement.authorizeEngine(engine.address, true);

    // 测试代币：0 位小数 —— 引擎/前端按整数单位下单，1 单位 = 1 代币，无换算
    const TestToken = await ethers.getContractFactory("TestToken");
    const base = await TestToken.deploy("Test ETH", "TETH", 0, 1_000_000);
    const quote = await TestToken.deploy("Test USDT", "TUSDT", 0, 1_000_000);
    const settlementAddr = await settlement.getAddress();

    for (const t of [base, quote]) {
      await t.mint(alice.address, 100_000);
      await t.mint(bob.address, 100_000);
      await t.connect(alice).approve(settlementAddr, ethers.MaxUint256);
      await t.connect(bob).approve(settlementAddr, ethers.MaxUint256);
    }
    return { settlement, owner, engine, alice, bob, base, quote };
  }

  function computeRoot(results: unknown[], prev: string | Promise<string>) {
    return Promise.resolve(prev).then((p) => {
      const coder = new ethers.AbiCoder();
      const resultsHash = ethers.keccak256(coder.encode([STRUCT], [results]));
      return ethers.keccak256(ethers.solidityPacked(["bytes32", "bytes32"], [resultsHash, p]));
    });
  }

  /** 提交一批结果（自动按当前 stateRoot 计算 newRoot） */
  async function submit(settlement: any, engine: any, results: unknown[]) {
    const newRoot = await computeRoot(results, await settlement.getStateRoot());
    return settlement.connect(engine).submitBatch(results, "0x", newRoot);
  }

  /** 构造一笔成交的两条结果（买方 + 卖方），price=200, amount=5 → cost=1000 */
  function trade(s: any, buyer: string, seller: string, baseAddr: string, quoteAddr: string, price = 200, amount = 5) {
    const cost = price * amount;
    return [
      { id: 1, orderId: ethers.randomBytes(32), user: buyer, baseToken: baseAddr, quoteToken: quoteAddr,
        price, amount, cost, side: 0, nullifier: ethers.randomBytes(32), role: "taker" },
      { id: 1, orderId: ethers.randomBytes(32), user: seller, baseToken: baseAddr, quoteToken: quoteAddr,
        price, amount, cost, side: 1, nullifier: ethers.randomBytes(32), role: "maker" },
    ];
  }

  // ── 基础配置 ──────────────────────────────

  it("should deploy with correct fees", async function () {
    const { settlement } = await loadFixture(deployFixture);
    expect(await settlement.makerFeeBps()).to.equal(5);
    expect(await settlement.takerFeeBps()).to.equal(TAKER_BPS);
    expect(await settlement.protocolFeeBps()).to.equal(30);
  });

  it("should authorize engine", async function () {
    const { settlement, engine } = await loadFixture(deployFixture);
    expect(await settlement.authorizedEngines(engine.address)).to.be.true;
  });

  it("should reject unauthorized engine", async function () {
    const { settlement, alice } = await loadFixture(deployFixture);
    await expect(
      settlement.connect(alice).submitBatch([], "0x00", ethers.ZeroHash)
    ).to.be.revertedWith("Not authorized engine");
  });

  it("should reject empty batch", async function () {
    const { settlement, engine } = await loadFixture(deployFixture);
    await expect(
      settlement.connect(engine).submitBatch([], "0x00", ethers.ZeroHash)
    ).to.be.revertedWith("Empty batch");
  });

  it("should update fees", async function () {
    const { settlement } = await loadFixture(deployFixture);
    await settlement.setFees(10, 20, 40);
    expect(await settlement.makerFeeBps()).to.equal(10);
  });

  it("should reject fee above max", async function () {
    const { settlement } = await loadFixture(deployFixture);
    await expect(settlement.setFees(600, 20, 40)).to.be.revertedWith("Fee too high");
  });

  it("should update max batch size", async function () {
    const { settlement } = await loadFixture(deployFixture);
    await settlement.setMaxBatchSize(50);
    expect(await settlement.maxBatchSize()).to.equal(50);
  });

  it("should return zero state root initially", async function () {
    const { settlement } = await loadFixture(deployFixture);
    expect(await settlement.getStateRoot()).to.equal(ethers.ZeroHash);
  });

  it("requireProof should default to false", async function () {
    const { settlement } = await loadFixture(deployFixture);
    expect(await settlement.requireProof()).to.be.false;
  });

  it("owner can toggle requireProof", async function () {
    const { settlement } = await loadFixture(deployFixture);
    await settlement.setRequireProof(true);
    expect(await settlement.requireProof()).to.be.true;
    await expect(settlement.setRequireProof(true)).to.emit(settlement, "ProofGateUpdated").withArgs(true);
    await settlement.setRequireProof(false);
    expect(await settlement.requireProof()).to.be.false;
  });

  // ── 金库：入金 / 提现 ──────────────────────

  it("deposit credits vault balance", async function () {
    const { settlement, alice, base } = await loadFixture(deployFixture);
    const sAddr = await settlement.getAddress();
    await settlement.connect(alice).deposit(await base.getAddress(), 500);

    expect(await settlement.vaultBalance(await base.getAddress(), alice.address)).to.equal(500);
    expect(await base.balanceOf(sAddr)).to.equal(500);
  });

  it("deposit reverts without approve", async function () {
    const { settlement, bob, base } = await loadFixture(deployFixture);
    const TestToken = await ethers.getContractFactory("TestToken");
    const fresh = await TestToken.deploy("Fresh", "FRS", 0, 1000);
    await fresh.mint(bob.address, 100);
    await expect(settlement.connect(bob).deposit(await fresh.getAddress(), 100))
      .to.be.reverted; // ERC20InsufficientAllowance
  });

  it("withdraw transfers tokens out (real ERC-20 transfer)", async function () {
    const { settlement, alice, base } = await loadFixture(deployFixture);
    const bAddr = await base.getAddress();
    await settlement.connect(alice).deposit(bAddr, 500);
    const before = await base.balanceOf(alice.address);

    await expect(settlement.connect(alice).withdraw(bAddr, 200))
      .to.emit(settlement, "Withdrawn").withArgs(alice.address, bAddr, 200);

    expect(await base.balanceOf(alice.address)).to.equal(before + 200n);
    expect(await settlement.vaultBalance(bAddr, alice.address)).to.equal(300);
  });

  it("withdraw more than balance reverts", async function () {
    const { settlement, alice, base } = await loadFixture(deployFixture);
    const bAddr = await base.getAddress();
    await settlement.connect(alice).deposit(bAddr, 100);
    await expect(settlement.connect(alice).withdraw(bAddr, 101))
      .to.be.revertedWith("insufficient balance");
  });

  // ── 结算：金库内真实划转 ────────────────────

  it("settles a trade by moving real token balances", async function () {
    const { settlement, engine, alice, bob, base, quote } = await loadFixture(deployFixture);
    const bAddr = await base.getAddress();
    const qAddr = await quote.getAddress();

    // alice 买方（备 1000 quote），bob 卖方（备 5 base）
    await settlement.connect(alice).deposit(qAddr, 1000);
    await settlement.connect(bob).deposit(bAddr, 5);

    const results = trade(settlement, alice.address, bob.address, bAddr, qAddr, 200, 5);
    await expect(submit(settlement, engine, results)).to.emit(settlement, "BatchSettled");

    // 买方：quote -1000，base +5
    expect(await settlement.vaultBalance(qAddr, alice.address)).to.equal(0);
    expect(await settlement.vaultBalance(bAddr, alice.address)).to.equal(5);
    // 卖方：base -5，quote +1000-1(fee=1000*15/10000=1)
    expect(await settlement.vaultBalance(bAddr, bob.address)).to.equal(0);
    expect(await settlement.vaultBalance(qAddr, bob.address)).to.equal(999);
    // 手续费累计
    expect(await settlement.protocolFees(qAddr)).to.equal(1);
  });

  it("conserves tokens across a trade (contract holds exactly what users deposited)", async function () {
    const { settlement, engine, alice, bob, base, quote } = await loadFixture(deployFixture);
    const bAddr = await base.getAddress();
    const qAddr = await quote.getAddress();
    await settlement.connect(alice).deposit(qAddr, 1000);
    await settlement.connect(bob).deposit(bAddr, 5);

    await submit(settlement, engine, trade(settlement, alice.address, bob.address, bAddr, qAddr));

    // 金库内余额之和 + 协议费 == 合约实际持有的代币
    const sAddr = await settlement.getAddress();
    const vaultBase = (await settlement.vaultBalance(bAddr, alice.address)) + (await settlement.vaultBalance(bAddr, bob.address));
    const vaultQuote = (await settlement.vaultBalance(qAddr, alice.address)) + (await settlement.vaultBalance(qAddr, bob.address));
    expect(await base.balanceOf(sAddr)).to.equal(vaultBase);
    expect(await quote.balanceOf(sAddr)).to.equal(vaultQuote + (await settlement.protocolFees(qAddr)));
  });

  it("reverts when payer has insufficient vault balance", async function () {
    const { settlement, engine, alice, bob, base, quote } = await loadFixture(deployFixture);
    const bAddr = await base.getAddress();
    const qAddr = await quote.getAddress();
    await settlement.connect(alice).deposit(qAddr, 999); // 差 1
    await settlement.connect(bob).deposit(bAddr, 5);

    const results = trade(settlement, alice.address, bob.address, bAddr, qAddr);
    await expect(submit(settlement, engine, results)).to.be.revertedWith("insufficient quote balance");
  });

  it("rejects batch with wrong state root", async function () {
    const { settlement, engine, alice, bob, base, quote } = await loadFixture(deployFixture);
    const results = trade(settlement, alice.address, bob.address, await base.getAddress(), await quote.getAddress());
    await expect(settlement.connect(engine).submitBatch(results, "0x", ethers.randomBytes(32)))
      .to.be.revertedWith("State root mismatch");
  });

  it("rejects hash-mode batch with duplicate nullifier", async function () {
    const { settlement, engine, alice, bob, base, quote } = await loadFixture(deployFixture);
    const bAddr = await base.getAddress();
    const qAddr = await quote.getAddress();
    await settlement.connect(alice).deposit(qAddr, 1000);
    await settlement.connect(bob).deposit(bAddr, 5);

    const results = trade(settlement, alice.address, bob.address, bAddr, qAddr);
    await submit(settlement, engine, results);
    // stateRoot 已推进；重算 newRoot 合法，但 nullifier 已标记 → 拒绝
    await expect(submit(settlement, engine, results)).to.be.revertedWith("Duplicate nullifier");
  });

  it("withdrawProtocolFees performs a real transfer", async function () {
    const { settlement, engine, owner, alice, bob, base, quote } = await loadFixture(deployFixture);
    const bAddr = await base.getAddress();
    const qAddr = await quote.getAddress();
    await settlement.connect(alice).deposit(qAddr, 1000);
    await settlement.connect(bob).deposit(bAddr, 5);
    await submit(settlement, engine, trade(settlement, alice.address, bob.address, bAddr, qAddr));

    const before = await quote.balanceOf(owner.address);
    await expect(settlement.withdrawProtocolFees(qAddr, 1))
      .to.emit(settlement, "Unshielded");
    expect(await quote.balanceOf(owner.address)).to.equal(before + 1n);
    expect(await settlement.protocolFees(qAddr)).to.equal(0);
  });

  it("only owner can withdraw protocol fees", async function () {
    const { settlement, alice, quote } = await loadFixture(deployFixture);
    await expect(
      settlement.connect(alice).withdrawProtocolFees(await quote.getAddress(), 1)
    ).to.be.revertedWith("Only owner");
  });
});

describe("LeverageManager", function () {
  async function deployFixture() {
    const [owner, engine, alice] = await ethers.getSigners();

    const Settlement = await ethers.getContractFactory("Settlement");
    const settlement = await Settlement.deploy(5, 15, 30);

    const Leverage = await ethers.getContractFactory("LeverageManager");
    const leverage = await Leverage.deploy(await settlement.getAddress());
    await leverage.authorizeOracle(engine.address, true);

    // 设置初始价格
    await leverage.connect(engine).updateOraclePrice("ETH_USDT", ethers.parseEther("3000"));

    return { leverage, owner, engine, alice };
  }

  it("should deploy with correct params", async function () {
    const { leverage } = await loadFixture(deployFixture);
    expect(await leverage.maintenanceMarginBps()).to.equal(50);
    expect(await leverage.liquidationPenaltyBps()).to.equal(250);
  });

  it("should open position with sufficient margin", async function () {
    const { leverage, alice } = await loadFixture(deployFixture);
    const size = ethers.parseEther("1");    // 1 ETH
    const price = ethers.parseEther("3000");
    const leverageRatio = 5n;
    const margin = (price * size) / (leverageRatio * 10n ** 18n);

    await leverage.connect(alice).openPosition("ETH_USDT", true, size, Number(leverageRatio), {
      value: margin,
    });

    const pos = await leverage.getPosition(alice.address, "ETH_USDT");
    expect(pos.active).to.be.true;
    expect(pos.leverage).to.equal(5);
  });

  it("should reject leverage above max", async function () {
    const { leverage, alice } = await loadFixture(deployFixture);
    await expect(
      leverage.connect(alice).openPosition("ETH_USDT", true, ethers.parseEther("1"), 15, {
        value: ethers.parseEther("1000"),
      })
    ).to.be.revertedWith("Invalid leverage");
  });

  it("should reject insuffient margin", async function () {
    const { leverage, alice } = await loadFixture(deployFixture);
    await expect(
      leverage.connect(alice).openPosition("ETH_USDT", true, ethers.parseEther("1"), 10, {
        value: ethers.parseEther("1"), // way too little
      })
    ).to.be.revertedWith("Insufficient margin");
  });
});

describe("ZKKYC", function () {
  async function deployFixture() {
    const [owner, verifier, alice] = await ethers.getSigners();
    const ZKKYC = await ethers.getContractFactory("ZKKYC");
    const zkkyc = await ZKKYC.deploy();
    await zkkyc.authorizeVerifier(verifier.address, true);
    return { zkkyc, owner, verifier, alice };
  }

  it("should deploy with default thresholds", async function () {
    const { zkkyc } = await loadFixture(deployFixture);
    const t = await zkkyc.thresholds();
    expect(t.anonymousThreshold).to.equal(ethers.parseEther("0.1"));
    expect(t.pseudonymousThreshold).to.equal(ethers.parseEther("1"));
    expect(t.zkVerifiedThreshold).to.equal(ethers.parseEther("100"));
  });

  it("should classify L0 for small orders", async function () {
    const { zkkyc, alice } = await loadFixture(deployFixture);
    const [level, needsKYC] = await zkkyc.classifyOrder(alice.address, ethers.parseEther("0.05"));
    expect(level).to.equal(0);
    expect(needsKYC).to.be.false;
  });

  it("should add to sanction list", async function () {
    const { zkkyc, alice } = await loadFixture(deployFixture);
    await zkkyc.updateSanctionList(alice.address, true);
    expect(await zkkyc.sanctionList(alice.address)).to.be.true;
  });

  it("should update thresholds", async function () {
    const { zkkyc } = await loadFixture(deployFixture);
    await zkkyc.setThresholds(
      ethers.parseEther("0.5"),
      ethers.parseEther("5"),
      ethers.parseEther("500")
    );
    const t = await zkkyc.thresholds();
    expect(t.anonymousThreshold).to.equal(ethers.parseEther("0.5"));
  });

  it("should revert threshold order violation", async function () {
    const { zkkyc } = await loadFixture(deployFixture);
    await expect(
      zkkyc.setThresholds(
        ethers.parseEther("10"),   // L0L1 > L1L2 → invalid
        ethers.parseEther("5"),
        ethers.parseEther("500")
      )
    ).to.be.revertedWith("Invalid order");
  });
});
