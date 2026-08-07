# Anubis Network 开发者指南

> 基于 [Anubis Network 白皮书](https://anubis-network.gitbook.io/anubis-network) 整理，面向 EVM 开发者的入门与进阶参考。

---

## 目录

1. [Anubis 是什么](#一anubis-是什么)
2. [核心架构](#二核心架构)
3. [隐私分级体系](#三隐私分级体系)
4. [交易类型](#四交易类型)
5. [预编译合约](#五预编译合约)
6. [Note 系统（UTXO 模型）](#六note-系统utxo-模型)
7. [阈值加密内存池（TEM）](#七阈值加密内存池tem)
8. [共识机制](#八共识机制)
9. [开发者工具链](#九开发者工具链)
10. [开发者快速上手指南](#十开发者快速上手指南)
11. [与 AnuBookDEX 的关系](#十一与-anubookdex-的关系)
12. [参考链接](#十二参考链接)

---

## 一、Anubis 是什么

**Anubis Chain** 是一条以**选择性隐私（Selective Privacy）**为核心创新的 Layer 1 公链，已于 2026 年 4 月 7 日上线主网。

| 关键参数 | 值 |
|---------|-----|
| Chain ID | 6714 |
| 原生 Gas 代币 | gasDAI（DAI 锚定的稳定 Gas） |
| 共识机制 | PoS + IBFT 2.0 变体 |
| 出块时间 | 2 秒 |
| TPS | > 1000 |
| 治理组织 | ANUBI Foundation（2020 年成立） |
| RPC 端点 | `rpc.anubispace.org` |
| 区块浏览器 | `browser.anubispace.org` |

**核心定位**：让数百万 Solidity 开发者在不学习 Rust、Circom 等电路描述语言的前提下，直接构建隐私应用。隐私被封装为"服务"，通过预编译合约像调用普通库函数一样使用。

**解决的核心痛点**：

| 痛点 | Anubis 方案 |
|------|-----------|
| EVM 生态隔离（隐私链自研 VM/语言） | 100% EVM 兼容，Solidity 直接部署 |
| "全有或全无"的隐私困境 | 选择性隐私，L0-L3 四级可配 |
| 隐私合约交互悖论 | Type 103 交易 — 隐私身份 + 公开合约逻辑 |

---

## 二、核心架构

### 2.1 三层架构

```
┌──────────────────────────────────────────────┐
│  Layer 3: 应用层                              │
│  DApps、钱包、Anubis SDK（离线交易构造+ZK证明）│
├──────────────────────────────────────────────┤
│  Layer 2: 隐私执行层                          │
│  Anubis EVM 扩展：Note 系统、隐身地址、       │
│  ZK 预编译合约                                │
├──────────────────────────────────────────────┤
│  Layer 1: 核心协议层                          │
│  共识(PoS) + 双账本同步(State Trie + Note Tree)│
└──────────────────────────────────────────────┘
```

### 2.2 混合状态模型（最关键的架构创新）

Anubis 同时维护两棵树，在同一个区块内**原子提交**：

| 状态层 | 数据结构 | 存储内容 |
|--------|---------|---------|
| **账户状态（公开）** | Patricia Merkle Trie | 合约代码、公开余额、合约存储 |
| **Note 状态（隐私）** | Sparse Merkle Tree（仅追加） | Note Commitments + Nullifier Set |

**同步机制**：EVM 执行区块时同时更新两棵树。例如一笔隐私合约调用（Type 103）会：

1. 标记旧 Note 为已花费（插入 Nullifier）
2. 在公开层增加临时余额
3. 执行合约逻辑，变更 Storage
4. 插入新 Note Commitment
5. 所有状态更改原子提交，要么全部成功要么全部回滚

### 2.3 Anubis EVM（Modified Geth）

基于 **Go-Ethereum 深度定制**，100% EVM 兼容。

**完全支持**：

- 所有 EVM 操作码
- 所有以太坊预编译合约
- EIP-1559、EIP-2930 交易类型
- Solidity 0.8.x、Vyper
- Hardhat、Foundry、Remix、Truffle
- Web3.js、Ethers.js、Viem、Wagmi
- MetaMask（自定义网络连接）

**扩展支持**：

- 6 个隐私预编译合约（见第五章）
- 4 种隐私交易类型（见第四章）
- View Key RPC 接口

---

## 三、隐私分级体系

| 等级 | 名称 | 发送者 | 接收者 | 金额 | 适用场景 |
|------|------|--------|--------|------|---------|
| **L0** | 完全公开 (Transparent) | 公开 | 公开 | 公开 | DAO 投票、NFT 展示、公开捐赠 |
| **L1** | 发送者隐私 | **隐藏** | 公开 | 公开 | 匿名捐赠、工资发放 |
| **L2** | 选择性隐私（默认） | **隐藏** | 公开 | 公开 | DEX 交易、借贷、流动性挖矿 |
| **L3** | 完全隐私（暗池模式） | **隐藏** | **隐藏** | **隐藏** | 大额 OTC、商业机密支付 |

---

## 四、交易类型

基于 **EIP-2718** 扩展，在标准以太坊交易类型之外新增 4 种隐私交易类型：

### 4.1 类型总览

| 类型 ID | 助记符 | 功能 | 资金流向 | 隐私等级 |
|---------|--------|------|---------|---------|
| `0x00` | Legacy | 标准以太坊交易 | 公开 → 公开 | L0 |
| `0x02` | EIP-1559 | 标准 Gas 市场交易 | 公开 → 公开 | L0 |
| **0x70** (100) | `TX_SHIELD` | 公开资产锁入隐私池，铸造 Note | 公开 → 隐私 | L3 |
| **0x71** (101) | `TX_TRANSFER` | 隐私池内转账 | 隐私 → 隐私 | L3 |
| **0x72** (102) | `TX_PRIV_CALL` | 以隐私身份调用任意 EVM 合约 | 隐私 → 公开交互 | L2 |
| **0x73** (103) | `TX_UNSHIELD` | 销毁 Note，资产提回公开地址 | 隐私 → 公开 | L0 |

### 4.2 Type 0x70: 屏蔽交易 (Shield)

资产从公开世界进入隐私世界的单向通道。

**数据载荷**：

| 字段 | 类型 | 说明 |
|------|------|------|
| `chainId` | uint256 | 防跨链重放 |
| `nonce` | uint256 | EOA 交易计数器 |
| `asset` | address | 锁定的资产地址（0x0 代表原生 ANB） |
| `amount` | uint256 | 锁定数量 |
| `recipientPublic` | bytes32 | 接收者隐私公钥（Stealth Meta-Address） |
| `encryptedNote` | bytes | Note 密文（ECIES 加密） |
| `signature` | bytes | EOA 的 ECDSA 签名 |

**执行逻辑**：验证 EOA 余额 → 转账至隐私池 → 插入 Note Commitment 至 Merkle Tree。不要求 ZK 证明，因为资金来源公开可验证。

### 4.3 Type 0x71: 隐私转账 (Private Transfer)

L3 完全隐私转账，类似 Zcash 的 Sapling 交易。

**数据载荷**：

| 字段 | 类型 | 说明 |
|------|------|------|
| `nullifiers` | bytes32[] | 输入 Note 的 Nullifier 列表 |
| `commitments` | bytes32[] | 输出 Note 的 Commitment 列表 |
| `proof` | bytes | PLONK ZK-SNARK 证明 |
| `encryptedNotes` | bytes | 接收者加密元数据（ECIES） |
| `ephemeralKey` | bytes32 | 用于隐身地址推导的临时公钥 |

**电路约束**：∑输入 = ∑输出 + Gas 费，输入/输出数量固定（如 2 进 2 出），差额用零值"虚 Note"补齐，防止交易关联分析。

### 4.4 ⭐ Type 0x72: 隐私合约调用 (Privacy Contract Call)

Anubis 的**核心创新**。以隐私身份调用 Uniswap、Aave 等标准 EVM 合约。

**数据载荷**：

| 字段 | 类型 | 说明 |
|------|------|------|
| `target` | address | 目标合约地址（如 Uniswap Router） |
| `calldata` | bytes | 对目标合约的调用数据（**明文**） |
| `publicAmountIn` | uint256 | 公开的注入金额 |
| `publicAmountOut` | uint256 | 预期最小返回金额 |
| `proof` | bytes | 绑定 calldata hash 的 ZK 证明 |

**执行流程**：

1. **预验证**：验证 ZK Proof，确认用户拥有资产且金额守恒
2. **临时注资**：系统创建**虚拟临时账户**（Ephemeral Account），从隐私池铸造资产至该账户
3. **EVM 调用**：以临时账户为 `msg.sender` 执行 `targetContract.call(calldata)`
4. **隐私边界**：目标合约仅看到临时账户地址，无法追溯真实用户
5. **数据透明**：Calldata 公开，体现"交易逻辑公开，交易者匿名"的设计哲学
6. **状态清理**：调用结束后剩余资产重新封装为输出 Note

**防重放攻击**：电路计算 `hash(target, calldata, amount)` 并作为公开输入，恶意中继者无法将 ZK Proof 重定向到其他合约。

### 4.5 Type 0x73: 解屏交易 (Unshield)

将隐私资产提回公开地址。

**数据载荷**：

| 字段 | 类型 | 说明 |
|------|------|------|
| `nullifiers` | bytes32 | 要销毁的 Note 的 Nullifier |
| `recipient` | address | 接收资金的公开以太坊地址 |
| `amount` | uint256 | 公开披露的提现金额 |
| `proof` | bytes | 证明拥有被销毁 Note 且金额匹配 |

**隐私分析**：接收地址和金额公开，但输入 Note 通过 Nullifier 引用，外部无法知道资金来自哪笔 Shield/Transfer 交易，实现**发送者匿名**。

---

## 五、预编译合约

所有 ZK 原语暴露为**固定地址的预编译合约**，以 Go 原生实现（绕过 EVM 字节码解释器，利用底层硬件加速）：

| 地址 | 标识符 | 功能 | Gas 估算 | 权限类型 |
|------|--------|------|---------|---------|
| `0x0100` | VERIFY_PROOF | 验证 PLONK 零知识证明 | ~180k+ 动态 | 无状态，完全开放 |
| `0x0101` | PEDERSEN | Pedersen 承诺与哈希 | ~5,000 | 无状态，完全开放 |
| `0x0102` | MIMC_HASH | MiMC ZK 友好哈希函数 | ~3,000 | 无状态，完全开放 |
| `0x0103` | STEALTH | 隐身地址计算 (EIP-5564) | ~8,500 | 无状态，完全开放 |
| `0x0104` | NULLIFIER | Nullifier 状态检查与标记 | ~10,000 | **有状态，受限访问** |
| `0x0105` | ENCRYPT | ECIES 加密 Note 数据 | ~2,500 | 无状态，完全开放 |

### 5.1 安全边界

**无状态预编译（`0x0100`/`0x0101`/`0x0103`/`0x0105`）**：纯函数，无副作用，任何合约或 EOA 均可调用。

**有状态预编译（`0x0104`）**：涉及全局状态读写，有三层防护：

| 防护层 | 机制 |
|--------|------|
| 经济壁垒 | `isSpent()` 基础 Gas 设为 5,000（远大于 SLOAD 的 100-2100），暴力枚举不可行 |
| 速率限制 | 单笔交易最多 10 次 `isSpent()` 调用；若在 ZK Proof 验证失败上下文中调用，Gas 指数增长 |
| 写入权限 | `markSpent()` 仅系统地址可调用，且必须在 ZK Proof 验证成功后原子写入 |

```solidity
// markSpent 访问控制伪代码（EVM 层实现）
function markSpent(bytes32 nullifier) internal {
    if (msg.sender != SYSTEM_ENTRY_POINT) {
        revert("Access Denied: Only protocol can mark nullifiers");
    }
    if (!GlobalVerifyState.isProofVerified()) {
        revert("Invariant Violation: Proof not verified");
    }
    _nullifierSet[nullifier] = true;
}
```

---

## 六、Note 系统（UTXO 模型）

### 6.1 Note 结构

```solidity
struct Note {
    bytes32 commitment;    // Pedersen 承诺: C = g^amount * h^blinding
    bytes32 nullifier;     // Nullifier，防止双重花费
    address assetType;     // 资产类型（如 USDC 地址），公开可见
    uint256 amount;        // 金额，在承诺中隐藏，或在选择性隐私模式下公开
    bytes   encryptedData; // 加密元数据（接收者、Memo 等），仅 View Key 持有者可见
}
```

### 6.2 Note 生命周期

```
创建 (Minting/Shielding)
  → 持有（在 Merkle Tree 中静态存在）
  → 花费（生成 ZK 证明 + 公开 Nullifier）
  → 销毁（Nullifier 记录到集合，永久不可再花费）
```

### 6.3 状态膨胀优化

| 优化措施 | 说明 |
|---------|------|
| Sparse Merkle Tree | 深度 32，理论容量约 43 亿 Note，支持高效非包含证明 |
| Bloom Filter | 内存中维护，加速 Nullifier 去重，显著减少磁盘 I/O |
| 历史归档 | 超一定区块高度的历史数据可通过 Merkle Path 归档至冷存储 |
| 无状态客户端 | 轻客户端只需最新 Tree Root，Merkle Path 由全节点按需提供 |

### 6.4 JoinSplit 机制

为**防止粉尘攻击**导致电路输入溢出（PLONK 电路上限 4 个输入 Note），SDK 自动在后台执行 JoinSplit：将多个小额 Note 合并为一个大额 Note，确保与主流 DeFi 协议的交互不因输入溢出而失败。

---

## 七、阈值加密内存池（TEM）

### 7.1 核心流程

```
用户提交加密交易
  → 提议者盲排（看不到交易内容，无法进行 MEV 提取）
  → 验证者投票确认区块顺序（2/3+ 共识）
  → 共识确认后，区块顺序锁定
  → 阈值解密（收集 t 个解密分片后还原明文交易列表）
  → EVM 按既定顺序执行
```

### 7.2 密码学原语

| 参数 | 值 |
|------|-----|
| 曲线 | BLS12-381（配对友好，适合门限签名/加密） |
| 密钥生成 | 每 Epoch（100 块）由验证者集合执行一次 DKG（分布式密钥生成） |
| 联合公钥 | `PK_joint`，全网广播 |
| 私钥分片 | 每个验证者仅持有 `sk_i` 分片 |

### 7.3 活性与安全性权衡

| 模式 | 触发条件 | 策略 | 结果 |
|------|---------|------|------|
| 正常模式 | 网络健康 | 阈值 `t = 2n/3` | 强抗审查，强隐私 |
| 超时降级 | 解密延迟 > 4s | 触发视图变更，下一提议者重新发起解密请求 | 轻微延迟，安全不变 |
| 紧急模式 | 连续 5 块解密失败 | 阈值降至 `t = n/2+1`，标记无响应节点 | 优先活性，抗合谋能力降低 |
| 回退模式 | 极端网络瘫痪 | 暂停 TEM，允许明文交易（L0） | 隐私降级，确保网络持续运行 |

---

## 八、共识机制

| 参数 | 值 |
|------|-----|
| 算法 | IBFT 2.0 变体（流水线化） |
| 出块时间 | 2 秒 |
| 活跃验证者上限 | 100 |
| 选举周期 | 每 Epoch（100 块）通过 VRF（可验证随机函数）重新选举 |
| 最终性 | **单区块最终性**（2/3+ 验证者签名即不可逆） |

### 8.1 递归证明

区块提议者运行**聚合电路**：将区块内所有隐私交易的 ZK Proof 及其 Merkle Root 更新操作作为输入，生成单一简洁递归证明 `π_block`。

- **极简验证**：外部观察者（如以太坊主网的跨链桥合约）只需验证 `π_block`，即可确认整个区块所有隐私资产转移合法且 Note Tree 状态转换正确
- **去信任跨链桥**：无需信任任何中继者

---

## 九、开发者工具链

### 9.1 SDK 与插件

```bash
npm install @anubis/contracts anubis.js hardhat-anubis-plugin
```

- **anubis.js**：封装 `shield()` / `privateTransfer()` / `unshield()` 等高级 API
- **hardhat-anubis-plugin**：Hardhat 开发环境集成
- **@anubis/contracts**：预编译合约 Solidity 接口

### 9.2 Solidity 开发示例

```solidity
// 引入 Anubis 预编译接口
import "@anubis/contracts/Precompiles.sol";

contract PrivacyVault {
    // 存款函数：验证 ZK 证明并记录 Commitment
    function deposit(bytes calldata proof, bytes32 commitment) public payable {
        // 构造公开输入
        bytes32[] memory publicInputs = new bytes32[](3);
        publicInputs[0] = AnubisState.currentRoot();
        publicInputs[1] = AnubisState.calculateNullifier(msg.sender);
        publicInputs[2] = bytes32(msg.value);

        // 调用预编译合约 0x0100 验证证明
        bool isValid = AnubisPrecompiles.verifyProof(proof, publicInputs);
        require(isValid, "Invalid ZK Proof: Deposit verification failed");

        // 验证通过后继续业务逻辑
        emit DepositEvent(commitment);
    }
}
```

### 9.3 隐身地址注册（EIP-5564）

```solidity
contract StealthAddressRegistry {
    mapping(address => StealthMetaAddress) public metaAddresses;

    struct StealthMetaAddress {
        bytes32 spendingPubKey;  // 消费公钥：用于生成隐身地址
        bytes32 viewingPubKey;   // 查看公钥：用于生成共享密钥（ECDH）
    }

    function register(bytes32 spendPubKey, bytes32 viewPubKey) external;

    function generateStealthAddress(
        address recipient,
        bytes32 ephemeralPubKey
    ) external view returns (address);
}
```

### 9.4 客户端证明生成

**本地生成**（最安全）：

- 桌面设备：约 1-3 秒（PLONK 证明系统）
- 移动设备：可能超过 10 秒

**委托证明**（移动端优化）：

- 用户加密发送 Witness 至可信证明服务节点
- 节点生成证明后返回
- **隐私权衡**：私钥安全，但 Witness 包含交易元数据（金额、Merkle Path、接收者公钥等），披露给了证明服务节点
- 建议仅使用自托管或高度信任的证明服务

**轻量电路**：约束数减少 50%，牺牲部分功能（输入 Note 从 4 降至 2）换取移动端性能。

---

## 十、开发者快速上手指南

### Step 1：配置网络

#### 主网

```json
{
  "chainId": 6714,
  "chainName": "Anubis Mainnet",
  "rpcUrls": ["https://rpc.anubispace.org"],
  "blockExplorerUrls": ["https://browser.anubispace.org"],
  "nativeCurrency": {
    "name": "gasDAI",
    "symbol": "DAI",
    "decimals": 18
  }
}
```

#### 测试网

> ⚠️ 测试网配置参数**尚未在官方白皮书或公开文档中发布**。主网刚于 2026 年 4 月上线，测试网可能仍处于邀请制阶段。以下为基于 EVM 兼容链惯例的推测配置，**使用前请通过官方社区确认**：

**获取测试网信息的推荐渠道**：

| 渠道 | 地址 |
|------|------|
| Telegram 社区 | `t.me/annubischain_global` |
| Telegram 公告 | `t.me/annubischainofficial` |
| Twitter | `twitter.com/AnubisChain` |
| 官网 | `anubischain.com` |

**推测配置**（未经官方确认）：

```json
{
  "chainId": "待官方公布（推测 6715）",
  "chainName": "Anubis Testnet",
  "rpcUrls": ["待官方公布"],
  "blockExplorerUrls": ["待官方公布"],
  "nativeCurrency": {
    "name": "gasDAI",
    "symbol": "DAI",
    "decimals": 18
  }
}
```

> 建议在 Telegram 社区中直接询问测试网的 Chain ID、RPC 端点和水龙头地址。

### Step 2：安装依赖

```bash
npm install @anubis/contracts anubis.js hardhat-anubis-plugin
```

### Step 3：选择隐私级别

| 需求 | 方案 |
|------|------|
| 只需代币隐私转账 | 使用 `anubis.js` 的 `shield()` / `privateTransfer()` / `unshield()` |
| 需要隐私 DeFi 交互 | 使用 Type 0x72 (Privacy Contract Call) 模式 |
| 现有合约渐进式迁移 | 先在 L0 部署标准 EVM 合约，再逐步添加隐私功能 |

### Step 4：理解关键约束

| 约束 | 说明 |
|------|------|
| 证明生成时间 | 桌面 ~1-3 秒，移动端建议使用委托证明或轻量电路 |
| 电路输入上限 | 最多 4 个输入 Note（超过需 SDK 自动 JoinSplit） |
| L2 与 L3 区别 | L2 暴露交易对和金额（DeFi 必需），L3 完全隐藏 |
| Nullifier 查询 | `0x0104` 预编译受限，单笔交易最多 10 次调用 |
| View Key | 可选择性向审计者披露历史交易，**不暴露私钥** |
| 推荐面额 | 建议使用标准面额（10/100/1000）存款以减少金额特征分析 |

---

## 十一、与 AnuBookDEX 的关系

本项目 `AnuBookDEX` 中的 `internal/dex/chain/` 模块是 Anubis Chain 的链上事件订阅与结算组件，与白皮书概念的对应关系：

| AnuBookDEX 模块 | 白皮书对应概念 |
|----------------|---------------|
| `subscriber.go`（链上事件订阅） | 交易生命周期 §7.3.1 — Note 发现与同步（扫描 EncryptedNote 事件日志 + 尝试解密 + 状态检查） |
| `settlement.go`（链上 ZK 结算） | §4.4 Type 0x72 — 隐私合约调用，以隐私身份调用结算合约 |
| `matcher.go`（链下撮合） | §5.3.1 — 链下撮合 + 链上 ZK 证明结算 |

**建议关注的技术集成点**：

1. **TEM 集成**：利用加密内存池防止撮合结果在链上结算时被抢跑
2. **预编译合约 `0x0104`**：在结算时验证 Nullifier 防双重花费
3. **递归证明**：如果撮合涉及批量结算，可参考递归 proof 聚合思路
4. **View Key 审计**：为监管合规保留选择性披露能力
5. **隐身地址**：基于 EIP-5564，无需实时点对点协商即可生成一次性收款地址

---

## 十二、参考链接

| 资源 | 地址 |
|------|------|
| 白皮书 GitBook | https://anubis-network.gitbook.io/anubis-network |
| 主网区块浏览器 | https://browser.anubispace.org |
| 主网 RPC | https://rpc.anubispace.org |
| 官网 | https://anubischain.com |
| Twitter | https://twitter.com/AnubisChain |
| Telegram 社区 | https://t.me/annubischain_global |
| Telegram 公告 | https://t.me/annubischainofficial |
| 主网启动公告 | http://www.defispeak.com/blog/archives/4414 |
| 混合状态模型分析 | http://fxh.ai/news/12319436/ |
