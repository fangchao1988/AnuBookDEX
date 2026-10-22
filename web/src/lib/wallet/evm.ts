// EVM 钱包适配器（Anubis Chain）：
// - MetaMask（window.ethereum）：连接、切链校验、下单签名、余额查询
// - DevEVMWallet：无钱包本地联调降级（模拟地址），保证 UI 全流程可走通。
//
// 与 Aleo 侧的差异：Anubis 订单不锁 record custody —— 引擎做链下撮合 + Settlement v2 上链，
// 前端只需签名订单（可选）并把载荷 POST /order。

export const ANUBIS_TESTNET = {
  chainId: '0x31769', // 202601
  chainName: 'Anubis Cheras Testnet',
  nativeCurrency: { name: 'DAI', symbol: 'DAI', decimals: 18 },
  rpcUrls: ['https://cheras-rpc.anubispace.org/rpc'],
  blockExplorerUrls: ['https://browser.anubispace.org'],
} as const

export const ANUBIS_MAINNET = {
  chainId: '0x1a3a', // 6714
  chainName: 'Anubis Mainnet',
  nativeCurrency: { name: 'DAI', symbol: 'DAI', decimals: 18 },
  rpcUrls: ['https://rpc.anubispace.org'],
  blockExplorerUrls: ['https://browser.anubispace.org'],
} as const

export type EvmNetwork = typeof ANUBIS_TESTNET | typeof ANUBIS_MAINNET

interface Eip1193Provider {
  request(args: { method: string; params?: unknown[] | object }): Promise<unknown>
  on?(event: string, cb: (...a: unknown[]) => void): void
  removeListener?(event: string, cb: (...a: unknown[]) => void): void
}

export interface EVMChainConfig {
  network: 'testnet' | 'mainnet'
  chainId: string
  chainName: string
  rpcUrl: string
  decimals: number
}

export function evmConfigFor(network: 'testnet' | 'mainnet'): EVMChainConfig {
  const n = network === 'mainnet' ? ANUBIS_MAINNET : ANUBIS_TESTNET
  return {
    network,
    chainId: n.chainId,
    chainName: n.chainName,
    rpcUrl: n.rpcUrls[0],
    decimals: n.nativeCurrency.decimals,
  }
}

/** 惰性检测注入的 EVM provider（不缓存 —— 扩展注入晚于页面 JS）。
 *  注意：仅作最后兜底。多钱包共存时 window.ethereum 是「注入竞态」的赢家
 *  （OKX 与 MetaMask 同装时可能拿到 OKX），应优先用 EIP-6963 发现。 */
export function detectEvmProvider(): Eip1193Provider | null {
  if (typeof window === 'undefined') return null
  const w = window as unknown as { ethereum?: Eip1193Provider }
  return w.ethereum ?? null
}

// ── EIP-6963：多注入钱包发现 ──────────────────────────────
// 多个扩展共存时各自 announce，页面可选择指定钱包，避免 window.ethereum 竞态。

const EIP6963_ANNOUNCE = 'eip6963:announceProvider'
const EIP6963_REQUEST = 'eip6963:requestProvider'

export interface Eip6963ProviderInfo {
  uuid: string
  name: string // 'MetaMask' | 'OKX Wallet' ...
  icon: string
  rdns: string // 'io.metamask' | 'com.okex.wallet' ...
}

export interface Eip6963ProviderDetail {
  info: Eip6963ProviderInfo
  provider: Eip1193Provider
}

/** 已知钱包的 rdns（用于偏好排序） */
export const RDNS_METAMASK = 'io.metamask'

/** 钱包选择持久化 key */
const STORAGE_WALLET_RDNS = 'anubook_evm_wallet_rdns'

/**
 * 发现所有注入的 EVM 钱包（EIP-6963）。
 * 必须等待一个事件循环窗口：各扩展在收到 requestProvider 后异步 announce。
 */
export function discoverEvmWallets(timeoutMs = 300): Promise<Eip6963ProviderDetail[]> {
  if (typeof window === 'undefined') return Promise.resolve([])
  const found = new Map<string, Eip6963ProviderDetail>()

  const onAnnounce = (e: Event) => {
    const detail = (e as CustomEvent<Eip6963ProviderDetail>).detail
    if (detail?.info?.uuid && detail.provider) found.set(detail.info.uuid, detail)
  }
  window.addEventListener(EIP6963_ANNOUNCE, onAnnounce as EventListener)
  window.dispatchEvent(new Event(EIP6963_REQUEST))

  return new Promise((resolve) => {
    window.setTimeout(() => {
      window.removeEventListener(EIP6963_ANNOUNCE, onAnnounce as EventListener)
      // 兜底：未实现 EIP-6963 的旧钱包走 window.ethereum.providers 数组
      const legacy = legacyProviders()
      for (const p of legacy) {
        const rdns = guessRdns(p)
        const uuid = `legacy:${rdns}`
        if (!found.has(uuid)) {
          found.set(uuid, { info: { uuid, name: rdnsToName(rdns), icon: '', rdns }, provider: p })
        }
      }
      resolve([...found.values()])
    }, timeoutMs)
  })
}

/** 旧式多 provider 数组（MetaMask 等注入 window.ethereum.providers） */
function legacyProviders(): Eip1193Provider[] {
  const w = window as unknown as { ethereum?: Eip1193Provider & { providers?: Eip1193Provider[] } }
  const eth = w.ethereum
  if (!eth) return []
  return Array.isArray(eth.providers) ? eth.providers : [eth]
}

function guessRdns(p: Eip1193Provider): string {
  // 注意：仅作 EIP-6963 缺失时的兜底。部分钱包（OKX 等）会伪装 isMetaMask=true
  // 以兼容老 dApp，故 isMetaMask 不足以判定身份，优先信 EIP-6963 的 rdns。
  const x = p as { isMetaMask?: boolean; isOkxWallet?: boolean; isOKExWallet?: boolean; isRabby?: boolean }
  if (x.isOkxWallet || x.isOKExWallet) return 'com.okex.wallet'
  if (x.isMetaMask) return RDNS_METAMASK
  if (x.isRabby) return 'io.rabby'
  return 'unknown'
}

function rdnsToName(rdns: string): string {
  if (rdns === RDNS_METAMASK) return 'MetaMask'
  if (rdns === 'com.okex.wallet') return 'OKX Wallet'
  if (rdns === 'io.rabby') return 'Rabby'
  return '浏览器钱包'
}

/** 从候选中挑默认钱包：已保存偏好 > 唯一候选 > null（要求用户选择） */
export function pickDefaultWallet(list: Eip6963ProviderDetail[]): Eip6963ProviderDetail | null {
  if (list.length === 0) return null
  const saved = typeof localStorage !== 'undefined' ? localStorage.getItem(STORAGE_WALLET_RDNS) : null
  if (saved) {
    const hit = list.find((d) => d.info.rdns === saved || d.info.name === saved)
    if (hit) return hit
  }
  // 多钱包共存时不自动挑选：window.ethereum 存在注入竞态（OKX/MetaMask 谁后注入谁赢），
  // 静默选中可能弹错钱包。交由 UI 让用户明确选择，之后记忆该选择。
  return list.length === 1 ? list[0] : null
}

export function rememberWallet(rdns: string) {
  try {
    localStorage.setItem(STORAGE_WALLET_RDNS, rdns)
  } catch {
    /* 隐私模式下 localStorage 可能不可用 */
  }
}

function hexToBigInt(hex: string | null | undefined): bigint {
  if (!hex) return 0n
  return BigInt(hex)
}

/** 提取 EIP-1193 错误码（4001 用户拒绝 / 4902 未知网络） */
function eip1193Code(e: unknown): number | null {
  if (typeof e === 'object' && e !== null && 'code' in e) {
    const c = (e as { code?: unknown }).code
    if (typeof c === 'number') return c
  }
  return null
}

function errMsg(e: unknown): string {
  if (typeof e === 'object' && e !== null && 'message' in e) {
    return String((e as { message?: unknown }).message ?? e)
  }
  return String(e)
}

export class EVMWallet {
  readonly kind = 'evm' as const
  private address: string | null = null
  private provider: Eip1193Provider | null
  private chainId: string | null = null
  private cfg: EVMChainConfig
  /** 用户显式选择 / 自动选定的钱包 rdns */
  private rdns: string | null = null
  /** 上次发现的钱包列表（供 UI 展示选择器） */
  private discovered: Eip6963ProviderDetail[] = []

  constructor(cfg: EVMChainConfig) {
    this.cfg = cfg
    this.provider = null
  }

  isConnected() {
    return this.address !== null
  }

  getAddress() {
    return this.address
  }

  getChainId() {
    return this.chainId
  }

  config() {
    return this.cfg
  }

  /** 当前使用的钱包 rdns（未连接时为 null） */
  getWalletRdns() {
    return this.rdns
  }

  /** 最近一次发现的钱包列表（UI 选择器用） */
  getDiscovered() {
    return this.discovered
  }

  /**
   * 解析并缓存 provider（EIP-6963 发现，避免 window.ethereum 注入竞态）。
   * 多钱包且无法自动判定时抛 WalletPickerRequired，由 UI 让用户选。
   */
  private async resolveProvider(forceRediscover = false): Promise<Eip1193Provider> {
    if (this.provider && !forceRediscover) return this.provider

    const list = await discoverEvmWallets()
    this.discovered = list
    if (list.length === 0) {
      throw new Error(
        '未检测到 EVM 钱包扩展。请安装 MetaMask（或其它 EVM 钱包）并解锁后刷新页面。' +
          '若已安装仍不出现，请检查扩展的「网站访问权限」是否允许本站点。',
      )
    }
    const chosen = this.rdns ? list.find((d) => d.info.rdns === this.rdns) : null
    const def = chosen ?? pickDefaultWallet(list)
    if (!def) {
      throw new WalletPickerRequired(list)
    }
    this.provider = def.provider
    this.rdns = def.info.rdns
    return this.provider
  }

  /** 显式选择钱包（UI 选择器回调） */
  async selectWallet(rdns: string): Promise<void> {
    const list = this.discovered.length ? this.discovered : await discoverEvmWallets()
    this.discovered = list
    const hit = list.find((d) => d.info.rdns === rdns)
    if (!hit) throw new Error(`未找到钱包：${rdns}`)
    this.provider = hit.provider
    this.rdns = hit.info.rdns
    rememberWallet(hit.info.rdns)
  }

  async connect(): Promise<string> {
    const p = await this.resolveProvider()
    const accounts = (await p.request({ method: 'eth_requestAccounts' })) as string[]
    if (!accounts?.length) throw new Error('钱包未返回账户')
    this.address = accounts[0]
    rememberWallet(this.rdns ?? '')

    this.chainId = (await p.request({ method: 'eth_chainId' })) as string
    if (this.chainId !== this.cfg.chainId) {
      await this.switchNetwork(p)
    }
    return this.address
  }

  /**
   * 切到目标网络。分两种情况（EIP-3085/3326）：
   *   - MetaMask 未知该网络（switch 抛 4902）-> wallet_addEthereumChain 添加后再切
   *   - 已知但用户拒绝（4001）-> 给出可操作提示，不静默失败
   */
  private async switchNetwork(p: Eip1193Provider): Promise<void> {
    const target = this.cfg.chainId
    try {
      await p.request({ method: 'wallet_switchEthereumChain', params: [{ chainId: target }] })
      this.chainId = target
      return
    } catch (e) {
      // 4902 = Unrecognized chain ID（MetaMask 没有这个网络，需先添加）
      if (eip1193Code(e) === 4902) {
        try {
          await p.request({
            method: 'wallet_addEthereumChain',
            params: [this.addChainParam()],
          })
          // 添加后 MetaMask 通常自动切链；仍再显式切一次以确保
          try {
            await p.request({ method: 'wallet_switchEthereumChain', params: [{ chainId: target }] })
          } catch {
            /* 已添加且可能已自动切换 */
          }
          this.chainId = target
          return
        } catch (addErr) {
          if (eip1193Code(addErr) === 4001) {
            throw new Error(`已取消添加 ${this.cfg.chainName} 网络。请在 MetaMask 中手动添加后重试。`)
          }
          throw new Error(`添加 ${this.cfg.chainName} 失败：${errMsg(addErr)}`)
        }
      }
      if (eip1193Code(e) === 4001) {
        throw new Error(
          `已取消切换到 ${this.cfg.chainName}（chainId ${target}）。` +
            `请在 MetaMask 顶部网络下拉中手动选择后重试。`,
        )
      }
      throw new Error(`切换到 ${this.cfg.chainName} 失败：${errMsg(e)}`)
    }
  }

  /** wallet_addEthereumChain 参数（EIP-3085 + chainId 3326） */
  private addChainParam() {
    const n = this.cfg.network === 'mainnet' ? ANUBIS_MAINNET : ANUBIS_TESTNET
    return {
      chainId: this.cfg.chainId,
      chainName: this.cfg.chainName,
      nativeCurrency: n.nativeCurrency,
      rpcUrls: [...n.rpcUrls],
      blockExplorerUrls: [...n.blockExplorerUrls],
    }
  }

  disconnect() {
    this.address = null
    this.chainId = null
  }

  /** 原生币余额（gas 代币 DAI，18 位小数） */
  async getBalance(): Promise<string> {
    if (!this.address) return '0'
    const p = await this.resolveProvider()
    const hex = (await p.request({
      method: 'eth_getBalance',
      params: [this.address, 'latest'],
    })) as string
    const wei = hexToBigInt(hex)
    const s = wei.toString().padStart(19, '0')
    return `${s.slice(0, -18)}.${s.slice(-18)}`.replace(/0+$/, '').replace(/\.$/, '')
  }

  /**
   * 对订单摘要签名（personal_sign）。
   * 签名可选：引擎不强制校验，但可作为下单者身份的链下凭证。
   */
  async signOrder(message: string): Promise<string> {
    if (!this.address) throw new Error('钱包未连接')
    const p = await this.resolveProvider()
    return (await p.request({
      method: 'personal_sign',
      params: [message, this.address],
    })) as string
  }

  /** 账户是否已在钱包中授权（静默探测，不弹框） */
  async hasAuthorizedAccount(): Promise<boolean> {
    let p: Eip1193Provider
    try {
      p = await this.resolveProvider()
    } catch {
      // 多钱包待选 / 无钱包：静默视为未授权（restore 不应弹选择器）
      return false
    }
    const accounts = (await p.request({ method: 'eth_accounts' })) as string[]
    if (accounts?.length) {
      this.address = accounts[0]
      this.chainId = (await p.request({ method: 'eth_chainId' })) as string
      return true
    }
    return false
  }

  // ── ERC-20（Settlement 金库入金/提现）──────────────────────
  // 说明：Anubis 的 gas 代币 DAI 本身也是 ERC-20，且原生币不可转账
  //（节点会拒绝 value>0 的交易）。

  /** 读 ERC-20 余额（balanceOf） */
  async erc20Balance(token: string, owner?: string): Promise<bigint> {
    const who = owner ?? this.address
    if (!who) return 0n
    const p = await this.resolveProvider()
    const data = ERC20_SELECTORS.balanceOf + addrArg(who)
    const res = (await p.request({
      method: 'eth_call',
      params: [{ to: token, data }, 'latest'],
    })) as string
    return res && res !== '0x' ? BigInt(res) : 0n
  }

  /** 读 ERC-20 授权额度（allowance） */
  async erc20Allowance(token: string, spender: string, owner?: string): Promise<bigint> {
    const who = owner ?? this.address
    if (!who) return 0n
    const p = await this.resolveProvider()
    const data = ERC20_SELECTORS.allowance + addrArg(who) + addrArg(spender)
    const res = (await p.request({
      method: 'eth_call',
      params: [{ to: token, data }, 'latest'],
    })) as string
    return res && res !== '0x' ? BigInt(res) : 0n
  }

  /** approve：授权 spender 花费 token（入金前必须先授权） */
  async erc20Approve(token: string, spender: string, amount: bigint): Promise<string> {
    const p = await this.resolveProvider()
    const data = ERC20_SELECTORS.approve + addrArg(spender) + uintArg(amount)
    return this.sendTx(p, token, data)
  }

  /** deposit：把 token 存入 Settlement 金库（合约按实际到账记账） */
  async vaultDeposit(settlement: string, token: string, amount: bigint): Promise<string> {
    const p = await this.resolveProvider()
    const data = SETTLEMENT_SELECTORS.deposit + addrArg(token) + uintArg(amount)
    return this.sendTx(p, settlement, data)
  }

  /** withdraw：从 Settlement 金库提现 */
  async vaultWithdraw(settlement: string, token: string, amount: bigint): Promise<string> {
    const p = await this.resolveProvider()
    const data = SETTLEMENT_SELECTORS.withdraw + addrArg(token) + uintArg(amount)
    return this.sendTx(p, settlement, data)
  }

  /** 读用户在 Settlement 金库的余额（vaultBalance） */
  async vaultBalance(settlement: string, token: string, owner?: string): Promise<bigint> {
    const who = owner ?? this.address
    if (!who) return 0n
    const p = await this.resolveProvider()
    const data = SETTLEMENT_SELECTORS.vaultBalance + addrArg(token) + addrArg(who)
    const res = (await p.request({
      method: 'eth_call',
      params: [{ to: settlement, data }, 'latest'],
    })) as string
    return res && res !== '0x' ? BigInt(res) : 0n
  }

  /** 通过钱包发交易（eth_sendTransaction），返回 tx hash */
  private async sendTx(p: Eip1193Provider, to: string, data: string): Promise<string> {
    if (!this.address) throw new Error('钱包未连接')
    return (await p.request({
      method: 'eth_sendTransaction',
      params: [{ from: this.address, to, data }],
    })) as string
  }
}

// ── ABI 编码辅助（避免引入 ethers/viem 依赖）─────────────────
// ERC-20 + Settlement 用到的方法选择器（keccak256(sig)[:4]）

const ERC20_SELECTORS = {
  balanceOf: '0x70a08231', // balanceOf(address)
  allowance: '0xdd62ed3e', // allowance(address,address)
  approve: '0x095ea7b3', // approve(address,uint256)
} as const

const SETTLEMENT_SELECTORS = {
  deposit: '0x47e7ef24', // deposit(address,uint256)
  withdraw: '0xf3fef3a3', // withdraw(address,uint256)
  vaultBalance: '0x152bceca', // vaultBalance(address,address)
} as const

/** address -> 32 字节左补零 hex（不含 0x） */
function addrArg(addr: string): string {
  return addr.toLowerCase().replace(/^0x/, '').padStart(64, '0')
}

/** uint256 -> 32 字节 hex（不含 0x） */
function uintArg(v: bigint): string {
  return v.toString(16).padStart(64, '0')
}

/**
 * 需要用户选择钱包（多个扩展共存且无保存偏好）。
 * UI 捕获后展示钱包列表，调用 wallet.selectWallet(rdns) 再重试 connect。
 */
export class WalletPickerRequired extends Error {
  readonly wallets: Eip6963ProviderDetail[]
  constructor(wallets: Eip6963ProviderDetail[]) {
    super(`检测到 ${wallets.length} 个钱包，请选择要连接的钱包`)
    this.name = 'WalletPickerRequired'
    this.wallets = wallets
  }
}
