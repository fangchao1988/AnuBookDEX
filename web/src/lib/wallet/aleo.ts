// Aleo 钱包适配器：
// - ShieldWalletAdapter：官方钱包（window.shield，见 shield.ts）
// - LeoWalletAdapter：旧版扩展（window.leoWallet）
// - DevWallet：无钱包/本地联调降级（模拟地址 + 占位 ciphertext），保证 UI 全流程可走通。
import type { AleoWallet, PlaceOrderParams, PlacedOrder, WalletBalances } from './types'
import { ShieldWalletAdapter } from './shield'

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
    return { aleo: '--', usdt: '--', base: '--' }
  }

  async mintToken(_tokenId: number, _amount: number): Promise<void> {
    // Leo Wallet 旧版：mint 支持待实测（Shield 已实现）
    throw new Error('Leo Wallet mint 未实现，请使用 Shield 钱包')
  }

  async deployProgram(): Promise<string> {
    // Leo Wallet 旧版：部署支持待实测（Shield 已实现）
    throw new Error('Leo Wallet 部署未实现，请使用 Shield 钱包')
  }

  async placeOrder(params: PlaceOrderParams): Promise<PlacedOrder> {
    if (!this.wallet?.requestTransaction) throw new Error('Leo Wallet 未暴露 requestTransaction()')
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

  isConnected() {
    return this.address !== null
  }

  getAddress() {
    return this.address
  }

  async connect(): Promise<string> {
    this.address = DEV_ADDRESS
    return this.address
  }

  disconnect() {
    this.address = null
  }

  async getBalances(_baseSymbol: string): Promise<WalletBalances> {
    return { aleo: '--', usdt: '128,456.78', base: '0.5234' }
  }

  async placeOrder(params: PlaceOrderParams): Promise<PlacedOrder> {
    // 联调占位：与灌单脚本/OrderForm dev 降级一致
    return {
      txId: `dev-tx-${params.orderId}`,
      ciphertext: 'ciphertext1dev-placeholder',
    }
  }

  async mintToken(_tokenId: number, _amount: number): Promise<void> {
    // Dev 模式模拟铸币（无链上操作）
  }

  async deployProgram(): Promise<string> {
    // Dev 模式模拟部署
    return 'dev-deploy-placeholder'
  }
}

// 工厂：优先真钱包（Shield -> Leo），缺失时 DevWallet（本地联调可走通全流程）
export function createWallet(): AleoWallet {
  if (typeof window !== 'undefined') {
    // Shield（Provable 官方，window.shield 注入，Aleo Wallet Standard）
    if ((window as unknown as { shield?: unknown }).shield) {
      return new ShieldWalletAdapter()
    }
    // Leo Wallet（旧版扩展，window.leoWallet 注入）
    if ((window as LeoWalletWindow).leoWallet) {
      return new LeoWalletAdapter()
    }
  }
  return new DevWallet()
}
