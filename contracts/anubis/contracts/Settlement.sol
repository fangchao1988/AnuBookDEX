// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import "@openzeppelin/contracts/utils/ReentrancyGuard.sol";

/**
 * @title Settlement
 * @notice 批量撮合结果验证 + ERC-20 真实结算
 * @dev 链下引擎提交 MatchResult[]，合约按结果在**金库内真实划转**买卖双方余额。
 *
 * 结算模型（v3）：
 *   1. 用户 `deposit(token, amount)` 入金到合约金库（需先 approve 本合约），
 *      记入 `vaultBalance[token][user]`。
 *   2. 引擎 `submitBatch(results, zkProof, newRoot)`：
 *      - requireProof=false 时校验 keccak 状态根一致性（MVP 承诺哈希模式）
 *      - requireProof=true 时走 0x0100 PLONK 验证（待 Anubis 公开电路参数）
 *      - 每条结果按 side 划转：买方付 quote 收 base，卖方付 base 收 quote
 *      - takerFeeBps 从卖方 quote 收入中扣除，计入 protocolFees
 *   3. 用户 `withdraw(token, amount)` 提现（合约 → 用户真实转账）。
 *
 * 守恒：单笔成交的买方与卖方可合并视为一次原子交换 ——
 *   quote: -cost + (cost - fee) = -fee（进 protocolFees）
 *   base:  +amount - amount      = 0
 *
 * 关于 _unshield：v3 起为**真实 ERC-20 转账**（协议费提取用），
 * 不再是仅发事件的占位实现。
 */
contract Settlement is ReentrancyGuard {
    using SafeERC20 for IERC20;

    /// @notice 撮合结果（单笔，代表一方在该笔成交中的收付）
    struct MatchResult {
        uint64  id;
        bytes32 orderId;
        address user;
        address baseToken;   // 基础资产（如 ETH）
        address quoteToken;  // 计价资产（如 USDT）
        uint256 price;       // 成交价（最小精度单位，审计/证明用）
        uint256 amount;      // base 数量（该用户收或付）
        uint256 cost;        // quote 金额 = price * amount（引擎按精度换算后传入）
        uint8   side;        // 0=buy（收 base 付 quote），1=sell（付 base 收 quote）
        bytes32 nullifier;   // 订单唯一标识（防双花）
        string  role;        // "maker" | "taker"（审计用）
    }

    // ─── 事件 ─────────────────────────────────
    event BatchSettled(
        uint64 indexed batchId,
        bytes32 stateRoot,
        uint256 totalVolume,
        uint256 feeCollected
    );

    event FeeDistributed(address indexed recipient, uint256 amount, string feeType);

    /// @notice 金库内划转（成交结算）
    event TradeSettled(
        bytes32 indexed orderId,
        address indexed user,
        address payToken,
        uint256 payAmount,
        address receiveToken,
        uint256 receiveAmount
    );

    /// @notice 真实链上转账（提现/协议费提取）
    event Unshielded(address indexed user, address indexed token, uint256 amount, bytes32 settlementId);

    event Deposited(address indexed user, address indexed token, uint256 amount);
    event Withdrawn(address indexed user, address indexed token, uint256 amount);

    event EngineAuthorized(address indexed engine, bool authorized);
    event BatchSizeUpdated(uint256 oldSize, uint256 newSize);
    event ProofGateUpdated(bool requireProof);

    // ─── Anubis 预编译合约 ──────────────────────
    // 实测（Anubis 主网 2026-09）：0x0100/0x0103/0x0104 对任意输入返回空 returndata，
    // 故 requireProof=false（默认）时不依赖预编译，仅本地校验状态根一致性。
    address constant VERIFY_PROOF   = address(0x0100);
    address constant NULLIFIER_CHECK = address(0x0104); // 指南 0x0104=NULLIFIER（0x0103=STEALTH）

    // ─── 状态 ─────────────────────────────────
    address public owner;
    uint64  public batchSeq;
    bytes32 public currentStateRoot;
    mapping(bytes32 => bool) public settledNullifiers;

    /// @notice 用户金库余额：token => user => amount
    mapping(address => mapping(address => uint256)) public vaultBalance;
    /// @notice 累计协议费（留在合约，可由 owner 提取）
    mapping(address => uint256) public protocolFees;

    bool public requireProof;

    uint256 public makerFeeBps;
    uint256 public takerFeeBps;
    uint256 public protocolFeeBps;

    mapping(address => bool) public authorizedEngines;
    mapping(address => uint256) public engineBatchCount;

    uint256 public maxBatchSize = 200;
    uint256 public constant MAX_FEE_BPS = 500; // 5%
    uint256 public constant BPS_DENOMINATOR = 10000;

    modifier onlyOwner() {
        require(msg.sender == owner, "Only owner");
        _;
    }

    modifier onlyEngine() {
        require(authorizedEngines[msg.sender], "Not authorized engine");
        _;
    }

    constructor(uint256 _makerFeeBps, uint256 _takerFeeBps, uint256 _protocolFeeBps) {
        require(_makerFeeBps <= MAX_FEE_BPS && _takerFeeBps <= MAX_FEE_BPS, "Fee too high");
        owner = msg.sender;
        makerFeeBps = _makerFeeBps;
        takerFeeBps = _takerFeeBps;
        protocolFeeBps = _protocolFeeBps;
    }

    // ─── 金库：入金 / 提现 ──────────────────────

    /**
     * @notice 入金到金库（下单前需先 approve 本合约）
     * @param token  ERC-20 地址
     * @param amount 数量（最小精度单位）
     */
    function deposit(address token, uint256 amount) external nonReentrant {
        require(token != address(0), "zero token");
        require(amount > 0, "amount=0");
        // 按实际到账量记账（防转账税/通缩代币少到账）
        uint256 before = IERC20(token).balanceOf(address(this));
        IERC20(token).safeTransferFrom(msg.sender, address(this), amount);
        uint256 received = IERC20(token).balanceOf(address(this)) - before;
        require(received > 0, "no tokens received");

        vaultBalance[token][msg.sender] += received;
        emit Deposited(msg.sender, token, received);
    }

    /**
     * @notice 从金库提现（合约 → 用户，真实 ERC-20 转账）
     */
    function withdraw(address token, uint256 amount) external nonReentrant {
        require(amount > 0, "amount=0");
        uint256 bal = vaultBalance[token][msg.sender];
        require(bal >= amount, "insufficient balance");
        // 先减账再转账（checks-effects-interactions）
        vaultBalance[token][msg.sender] = bal - amount;

        IERC20(token).safeTransfer(msg.sender, amount);
        emit Withdrawn(msg.sender, token, amount);
    }

    // ─── 引擎管理 ────────────────────────────

    function authorizeEngine(address engine, bool authorized) external onlyOwner {
        authorizedEngines[engine] = authorized;
        emit EngineAuthorized(engine, authorized);
    }

    function setMaxBatchSize(uint256 size) external onlyOwner {
        require(size >= 10 && size <= 500, "Invalid batch size");
        emit BatchSizeUpdated(maxBatchSize, size);
        maxBatchSize = size;
    }

    function setFees(uint256 _makerBps, uint256 _takerBps, uint256 _protocolBps) external onlyOwner {
        require(_makerBps <= MAX_FEE_BPS && _takerBps <= MAX_FEE_BPS, "Fee too high");
        makerFeeBps = _makerBps;
        takerFeeBps = _takerBps;
        protocolFeeBps = _protocolBps;
    }

    function setRequireProof(bool _require) external onlyOwner {
        requireProof = _require;
        emit ProofGateUpdated(requireProof);
    }

    /// @notice 提取协议费（真实链上转账）
    function withdrawProtocolFees(address token, uint256 amount) external onlyOwner nonReentrant {
        require(protocolFees[token] >= amount, "insufficient fees");
        protocolFees[token] -= amount;
        _unshield(token, owner, amount, bytes32(0));
    }

    // ─── 批量结算（核心） ──────────────────────

    /**
     * @notice 提交批量撮合结果并在金库内划转
     * @param results 撮合结果数组（每笔成交含买卖双方各一条）
     * @param zkProof PLONK 证明（requireProof=true 时使用）
     * @param newRoot 撮合后新状态根
     */
    function submitBatch(
        MatchResult[] calldata results,
        bytes calldata zkProof,
        bytes32 newRoot
    ) external onlyEngine returns (bytes32) {
        require(results.length > 0, "Empty batch");
        require(results.length <= maxBatchSize, "Batch too large");

        // ─── 0. 证明门槛 ───
        if (requireProof) {
            require(_verifyZKProof(zkProof, results, newRoot), "ZK proof failed");
        } else {
            bytes32 resultsHash = keccak256(abi.encode(results));
            require(
                newRoot == keccak256(abi.encodePacked(resultsHash, currentStateRoot)),
                "State root mismatch"
            );
        }

        // ─── 1. Nullifier 防双花 ───
        for (uint256 i = 0; i < results.length; i++) {
            require(!settledNullifiers[results[i].nullifier], "Duplicate nullifier");
            if (requireProof) {
                (bool nullifierOk, ) = NULLIFIER_CHECK.staticcall(abi.encode(results[i].nullifier));
                require(nullifierOk, "Nullifier check failed");
            }
            settledNullifiers[results[i].nullifier] = true;
        }

        // ─── 2. 金库内真实划转 ───
        uint256 totalVolume;
        uint256 feeCollected;

        for (uint256 i = 0; i < results.length; i++) {
            MatchResult calldata mr = results[i];
            require(mr.amount > 0, "zero amount");
            require(mr.cost > 0, "zero cost");
            require(mr.baseToken != address(0) && mr.quoteToken != address(0), "zero token");

            if (mr.side == 0) {
                // 买方：付 quote(cost)，收 base(amount)
                _vaultDebit(mr.quoteToken, mr.user, mr.cost, "insufficient quote balance");
                vaultBalance[mr.baseToken][mr.user] += mr.amount;
                emit TradeSettled(mr.orderId, mr.user, mr.quoteToken, mr.cost, mr.baseToken, mr.amount);
            } else {
                // 卖方：付 base(amount)，收 quote(cost - fee)
                uint256 fee = (mr.cost * takerFeeBps) / BPS_DENOMINATOR;
                uint256 proceeds = mr.cost - fee;

                _vaultDebit(mr.baseToken, mr.user, mr.amount, "insufficient base balance");
                vaultBalance[mr.quoteToken][mr.user] += proceeds;
                if (fee > 0) {
                    protocolFees[mr.quoteToken] += fee;
                    feeCollected += fee;
                }
                emit TradeSettled(mr.orderId, mr.user, mr.baseToken, mr.amount, mr.quoteToken, proceeds);

                totalVolume += mr.cost; // 每笔成交只按卖方计一次量，避免双算
            }
        }

        // ─── 3. 费用分配（记录） ───
        if (feeCollected > 0) {
            uint256 protocolShare = (feeCollected * protocolFeeBps) / BPS_DENOMINATOR;
            uint256 lpShare = feeCollected - protocolShare;
            if (protocolShare > 0) emit FeeDistributed(address(this), protocolShare, "protocol");
            if (lpShare > 0) emit FeeDistributed(address(this), lpShare, "lp_reward");
        }

        // ─── 4. 更新状态根 ───
        currentStateRoot = newRoot;
        batchSeq++;
        engineBatchCount[msg.sender]++;

        emit BatchSettled(batchSeq, newRoot, totalVolume, feeCollected);
        return newRoot;
    }

    // ─── 内部函数 ────────────────────────────

    /// @dev 金库扣账（余额不足则 revert；对手方入账由调用处直接 `+=`）
    function _vaultDebit(address token, address from, uint256 amount, string memory errMsg) internal {
        uint256 bal = vaultBalance[token][from];
        require(bal >= amount, errMsg);
        vaultBalance[token][from] = bal - amount;
    }

    /**
     * @dev 真实 ERC-20 转账（v3：不再是事件占位）
     */
    function _unshield(address token, address user, uint256 amount, bytes32 settlementId) internal {
        IERC20(token).safeTransfer(user, amount);
        emit Unshielded(user, token, amount, settlementId);
    }

    function _verifyZKProof(
        bytes calldata zkProof,
        MatchResult[] calldata results,
        bytes32 newRoot
    ) internal view returns (bool) {
        bytes32 resultsHash = keccak256(abi.encode(results));
        bytes memory publicInputs = abi.encode(newRoot, resultsHash, currentStateRoot);
        bytes memory data = abi.encodePacked(zkProof, publicInputs);
        (bool success, bytes memory returnData) = VERIFY_PROOF.staticcall(data);
        if (!success || returnData.length < 32) {
            return false;
        }
        return abi.decode(returnData, (bool));
    }

    // ─── 查询 ───────────────────────────────

    function isSettled(bytes32 nullifier) external view returns (bool) {
        return settledNullifiers[nullifier];
    }

    function getStateRoot() external view returns (bytes32) {
        return currentStateRoot;
    }
}
