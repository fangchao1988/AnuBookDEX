// Aleo 钱包适配器：
// - ShieldWalletAdapter：官方钱包（window.shield，见 shield.ts）
// - LeoWalletAdapter：旧版扩展（window.leoWallet）
// - DevWallet：无钱包/本地联调降级（模拟地址 + 占位 ciphertext），保证 UI 全流程可走通。
import type { AleoWallet, PlaceOrderParams, PlacedOrder, WalletBalances } from './types'
import { ShieldWalletAdapter } from './shield'
import { pairMode } from '../tokens'

const DEV_ADDRESS = 'aleo1dev-wallet-placeholder'

// ============ Leo Wallet 适配器 ============

interface LeoWalletWindow {
  leoWallet?: {
    address?: () => Promise<string>
    requestRecords?: (program: string) => Promise<unknown[]>
    requestTransaction?: (tx: unknown) => Promise<string>
    requestSignature?: (message: string) => Promise<unknown>
  }
}

export class LeoWalletAdapter implements AleoWallet {
  readonly kind = 'leo' as const
  private address: string | null = null
  private wallet: NonNullable<LeoWalletWindow['leoWallet']> | null = null

  constructor() {
    this.wallet = (window as LeoWalletWindow).leoWallet ?? null
  }

  isConnected() {
    return this.address !== null
  }

  getAddress() {
    return this.address
  }

  async connect(): Promise<string> {
    if (!this.wallet) throw new Error('未检测到 Leo Wallet 扩展，请安装 Leo Wallet 或使用演示钱包')
    if (this.wallet.address) {
      this.address = await this.wallet.address()
    } else {
      throw new Error('Leo Wallet 未暴露 address() API')
    }
    return this.address
  }

  disconnect() {
    this.address = null
  }

  async getBalances(_baseSymbol: string): Promise<WalletBalances> {
    // 链上余额：requestRecords(program) 聚合 Token record（token_id 1=ETH, 2=USDT）
    // 注：需真钱包实测 record 结构与解密；当前返回占位
    return { aleo: '--', usdt: '--', base: '--', usdcx: '--', aleoPublic: '--', usdcxPublic: '--' }
  }

  async mintToken(_tokenId: number, _amount: number): Promise<void> {
    // Leo Wallet 旧版：mint 支持待实测（Shield 已实现）
    throw new Error('Leo Wallet mint 未实现，请使用 Shield 钱包')
  }

  async deployProgram(): Promise<string> {
    // Leo Wallet 旧版：部署支持待实测（Shield 已实现）
    throw new Error('Leo Wallet 部署未实现，请使用 Shield 钱包')
  }

  async getCredentials(): Promise<void> {
    // Leo Wallet 旧版：get_credentials 支持待实测（Shield 已实现）
    throw new Error('Leo Wallet 领取凭证未实现，请使用 Shield 钱包')
  }

  async placeOrder(params: PlaceOrderParams): Promise<PlacedOrder> {
    if (!this.wallet?.requestTransaction) throw new Error('Leo Wallet 未暴露 requestTransaction()')
    // p4 真实币对（ALEO/USDCX）需要跨程序 record 选择（USDCX Token+Credentials / credits），
    // Leo Wallet 旧版 API 无法表达 filters，请使用 Shield 钱包
    if (pairMode(params.symbol) === 'p4-real') {
      throw new Error('ALEO/USDCX 下单请使用 Shield 钱包（Leo Wallet 不支持跨程序 record 选择）')
    }
    // 1) 构建 place_order transition：fund Token record（requestRecords）+ 9 参数
    //    Transaction.createTransaction(publicKey, network, program, 'place_order', inputs, fee)
    // 2) requestTransaction 广播 -> txId
    // 3) 引擎代理 GET /order/tx/{txId} 提取 Order record ciphertext
    //
    // 说明：inputs 需要锁仓 Token record（买单锁 quote=price*amount，卖单锁 base=amount），
    // record 选择与 transition 构建依赖 @provablehq/sdk，需真钱包环境实测后完善。
    const txId = await this.wallet.requestTransaction({
      program: 'anubook_dex_p2.aleo',
      function: 'place_order',
      inputs: [params], // TODO: Transaction.createTransaction 构建（需 SDK + 真钱包实测）
      fee: 0,
    })
    // 通过引擎代理换 ciphertext
    const res = await fetch(`/order/tx/${encodeURIComponent(txId)}`)
    if (!res.ok) throw new Error(`获取 Order record 失败: ${await res.text()}`)
    const data = (await res.json()) as { ciphertext: string }
    return { txId, ciphertext: data.ciphertext }
  }
}

// ============ Dev 钱包（本地联调降级） ============

export class DevWallet implements AleoWallet {
  readonly kind = 'dev' as const
  private address: string | null = null
  // 竞态补救：创建时未检测到真钱包（扩展注入晚于页面 JS），connect() 时若
  // 已注入则切换 delegate 透传 —— store 持有的 DevWallet 引用无需重建
  private delegate: AleoWallet | null = null

  isConnected() {
    return this.address !== null
  }

  getAddress() {
    return this.address
  }

  async connect(): Promise<string> {
    // 先实时重查注入状态：扩展 content script 注入晚于页面 JS 时，
    // createWallet() 当时降级到了本 DevWallet —— 此刻检测到真钱包则透传
    const real = resolveWallet()
    if (real) {
      this.delegate = real
      this.address = await real.connect()
      return this.address
    }
    // 浏览器钱包扩展（Shield/Leo）的 content script 只在 localhost/127.0.0.1 与 HTTPS
    // 页面注入（扩展安全设计，防止 HTTP 明文页面被中间人攻击注入钱包）。
    // Dev 钱包仅限本地联调（localhost / dev 环境变量）；生产域名下静默降级会显示
    // 占位地址误导用户 —— 改为明确报错并给出指引。
    const isLocal = ['localhost', '127.0.0.1'].includes(window.location.hostname)
    if (!isLocal && !import.meta.env.DEV) {
      throw new Error(
        '未检测到 Aleo 钱包扩展（Shield）。请确认：1) 已安装并解锁 Shield 钱包；' +
        '2) 扩展「网站访问权限」允许本站点（chrome://extensions → Shield → 详情）；' +
        '3) 解锁后硬刷新页面（Ctrl+Shift+R）。'
      )
    }
    this.address = DEV_ADDRESS
    return this.address
  }

  disconnect() {
    this.address = null
  }

  async getBalances(baseSymbol: string): Promise<WalletBalances> {
    // 已透传真钱包：走真实链上查询；否则联调占位
    // （公开/隐私拆分模拟与真实聚合口径一致：隐私 = 总 - 公开）
    if (this.delegate) return this.delegate.getBalances(baseSymbol)
    return { aleo: '80.00', usdt: '128,456.78', base: '0.5234', usdcx: '200.00', aleoPublic: '50.00', usdcxPublic: '120.00' }
  }

  async placeOrder(params: PlaceOrderParams): Promise<PlacedOrder> {
    if (this.delegate) return this.delegate.placeOrder(params)
    // 联调占位：与灌单脚本/OrderForm dev 降级一致
    return {
      txId: `dev-tx-${params.orderId}`,
      ciphertext: 'ciphertext1dev-placeholder',
    }
  }

  async mintToken(tokenId: number, amount: number): Promise<void> {
    if (this.delegate) return this.delegate.mintToken(tokenId, amount)
    // Dev 模式模拟铸币（无链上操作）
  }

  async deployProgram(): Promise<string> {
    if (this.delegate) return this.delegate.deployProgram()
    // Dev 模式模拟部署
    return 'dev-deploy-placeholder'
  }

  async getCredentials(): Promise<void> {
    if (this.delegate) return this.delegate.getCredentials()
    // Dev 模式模拟领取凭证（无链上操作）
  }
}

// 工厂：优先真钱包（Shield -> Leo），缺失时 DevWallet（本地联调可走通全流程）。
// 注意：钱包扩展的 content script 注入时机与页面 JS 加载存在竞态（扩展常在
// document_idle 才注入），模块加载时一次性检测会漏检 —— 因此这里只做静态选择，
// connect() 内部再通过 resolveWallet() 实时重查 window.shield / window.leoWallet。
export function createWallet(): AleoWallet {
  return resolveWallet() ?? new DevWallet()
}

// 连接时实时解析钱包类型：DevWallet 是可变代理 —— 若此刻检测到真钱包，
// 替换自身 delegate 后透传（已创建的 store 引用不变，行为切换为真钱包）
function detectInjected(): 'shield' | 'leo' | null {
  if (typeof window === 'undefined') return null
  if ((window as unknown as { shield?: unknown }).shield) return 'shield'
  if ((window as LeoWalletWindow).leoWallet) return 'leo'
  return null
}

function resolveWallet(): AleoWallet | null {
  const kind = detectInjected()
  if (kind === 'shield') return new ShieldWalletAdapter()
  if (kind === 'leo') return new LeoWalletAdapter()
  return null
}
