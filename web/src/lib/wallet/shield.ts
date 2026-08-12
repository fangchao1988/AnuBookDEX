// Shield 钱包适配器（Provable 官方钱包，window.shield 注入，Aleo Wallet Standard）。
// 基于官方包 @provablehq/aleo-wallet-adaptor-shield，API 源码已核对：
//   connect(network, decryptPermission, programs) -> {address}
//   requestRecords(program, includePlaintext) / decrypt(ciphertext)
//   executeTransaction({program, function, inputs, fee}) -> {transactionId}
//   transactionStatus(txId)
//
// 注意：交易输入格式（record 选择、u64 微单位、fee 模式）需真钱包实测后微调，
// 本地无 Shield 环境，先按 Aleo Wallet Standard 约定实现。
import { ShieldWalletAdapter as ShieldAdapter } from '@provablehq/aleo-wallet-adaptor-shield'
import { WalletDecryptPermission } from '@provablehq/aleo-wallet-standard'
import type { Network, TransactionInput } from '@provablehq/aleo-types'
import type { AleoWallet, PlaceOrderParams, PlacedOrder, WalletBalances } from './types'
import { fetchAleoBalance, fetchOperatorAddress } from '../api/orders'

export const PROGRAM_ID = 'anubook_dex_p2.aleo'

// 解析 Aleo 类型化字符串（'123u64' -> 123；'2u32' -> 2；'...group' -> 0；纯数字原样返回）
function parseTyped(v: string | undefined): number {
  if (!v) return 0
  // 类型后缀为字母+数字（u64/u32/...），需整体剥掉：/[a-z]+\d+$/ 匹配 'u64'/'u32'
  const n = Number(String(v).replace(/[a-z]+\d+$/i, ''))
  return Number.isFinite(n) ? n : 0
}

export class ShieldWalletAdapter implements AleoWallet {
  readonly kind = 'shield' as const
  private adapter: ShieldAdapter | null = null
  private address: string | null = null
  private network: Network

  constructor(network: 'mainnet' | 'testnet' = 'testnet') {
    this.network = (network === 'mainnet' ? 'mainnet' : 'testnet') as Network
    if (typeof window !== 'undefined' && (window as unknown as { shield?: unknown }).shield) {
      this.adapter = new ShieldAdapter()
    }
  }

  isConnected() {
    return this.address !== null
  }

  getAddress() {
    return this.address
  }

  async connect(): Promise<string> {
    if (!this.adapter) throw new Error('未检测到 Shield 钱包扩展，请安装 Shield 钱包（shields.app）')
    if (this.adapter.readyState !== 'Installed') {
      throw new Error('Shield 钱包未就绪，请打开扩展并解锁')
    }
    const account = await this.adapter.connect(this.network, WalletDecryptPermission.UponRequest, [PROGRAM_ID])
    this.address = account.address
    return this.address
  }

  disconnect() {
    void this.adapter?.disconnect()
    this.address = null
  }

  // 链上余额：
  //  - ALEO：公开余额（引擎代理 credits.aleo account mapping）+ Shield 私有 Credits record 聚合
  //  - USDT/ETH：anubook_dex_p2 Token record（recordView.fields = { amount: '123u64', token_id: '2u32' }）
  async getBalances(_baseSymbol: string): Promise<WalletBalances> {
    if (!this.adapter || !this.address) throw new Error('Shield 钱包未连接')

    // ALEO 公开余额（无需钱包授权，链上公开数据）
    let aleo = 0
    try {
      const pub = await fetchAleoBalance(this.address)
      aleo += pub.aleo
    } catch (e) {
      if (import.meta.env.DEV) console.log('[shield] public balance query failed:', e)
    }
    // ALEO shielded records（credits.aleo 的 Credits record）
    try {
      const credits = await this.adapter.requestRecords('credits.aleo', true)
      if (import.meta.env.DEV) {
        console.log('[shield] credits.aleo records:', JSON.stringify(credits).slice(0, 3000))
      }
      for (const rec of credits) {
        const env = rec as unknown as { recordView?: { fields?: Record<string, string> }; data?: { plaintext?: Record<string, string>; microcredits?: string } }
        const fields = env.recordView?.fields ?? env.data?.plaintext ?? {}
        aleo += parseTyped(fields.microcredits ?? env.data?.microcredits) / 1e6
      }
    } catch {
      // credits.aleo 未授权/无记录：忽略，只用公开余额
    }

    // anubook_dex_p2 Token records（测试币 USDT/ETH）
    const sum: Record<number, number> = {}
    try {
      const records = await this.adapter.requestRecords(PROGRAM_ID, true)
      if (import.meta.env.DEV) {
        console.log('[shield] anubook_dex_p2 records:', JSON.stringify(records).slice(0, 3000))
      }
      for (const rec of records) {
        const env = rec as unknown as { recordView?: { fields?: Record<string, string> }; data?: { plaintext?: Record<string, string>; token_id?: string; amount?: string } }
        const fields = env.recordView?.fields ?? env.data?.plaintext ?? {}
        const tid = parseTyped(fields.token_id ?? env.data?.token_id)
        if (tid === 1 || tid === 2) {
          sum[tid] = (sum[tid] ?? 0) + parseTyped(fields.amount ?? env.data?.amount)
        }
      }
    } catch {
      // 无测试币记录：余额为空
    }
    return {
      aleo: aleo > 0 ? String(aleo) : '--',
      usdt: sum[2] !== undefined ? String(sum[2]) : '--',
      base: sum[1] !== undefined ? String(sum[1]) : '--',
    }
  }

  // place_order：inputs[0] 为 fund Token record —— 钱包自动选择未花费记录
  // （Aleo Wallet Standard InputRequest：filters 按 token_id 匹配；买单锁 quote、卖单锁 base）
  // 注：合约 MVP 断言 fund.amount == price*amount（严格相等），用户需持有恰好金额的 Token record；
  //     record 自动选择 + 微单位换算待真钱包实测确认
  async placeOrder(params: PlaceOrderParams): Promise<PlacedOrder> {
    if (!this.adapter) throw new Error('Shield 钱包未连接')
    // operator 地址来自引擎配置（chain.aleo.address），place_order 的 Order record 归 operator
    const operator = params.operator || (await fetchOperatorAddress())
    const neededToken = params.side === 0 ? params.quoteToken : params.baseToken
    // 锁仓量：买单 = price×amount（quote 金额），卖单 = amount（base 数量）
    const neededAmount =
      params.side === 0
        ? Math.floor(Number(params.price) * Number(params.amount))
        : Math.floor(Number(params.amount))
    const inputs: TransactionInput[] = [
      {
        type: 'record',
        program: PROGRAM_ID,
        recordname: 'Token',
        // 精确匹配：token_id + 金额（合约 MVP 断言 fund.amount == price*amount 严格相等，
        // 钱包自动选择必须锁定金额，否则会选到任意金额的 record 导致断言失败）
        filters: {
          token_id: { eq: `${neededToken}u32` },
          amount: { eq: `${neededAmount}u64` },
        },
      },
      `${params.orderId}u128`,
      `${params.side}u8`,
      `${Math.floor(Number(params.price))}u64`,
      `${Math.floor(Number(params.amount))}u64`,
      `${params.baseToken}u32`,
      `${params.quoteToken}u32`,
      `${params.deadline}u32`,
      operator,
    ]
    const result = await this.adapter.executeTransaction({
      program: PROGRAM_ID,
      function: 'place_order',
      inputs,
      fee: 0, // Shield 覆盖部分 gas（免费转账模式）；place_order 费用模式待实测
    })
    // Shield 返回内部 ID（shield_...），经 transactionStatus 轮询等到链上确认
    // （status=accepted 才上链；期间 transactionId 可能返回但链上不可见）
    const shieldId = result.transactionId
    let onchainId = ''
    for (let i = 0; i < 60; i++) {
      const status = await this.adapter.transactionStatus(shieldId)
      if (status.status === 'accepted' && status.transactionId) {
        onchainId = status.transactionId
        break
      }
      if (status.status === 'failed' || status.status === 'rejected') {
        throw new Error(`place_order 交易失败: ${status.status}${status.error ? `: ${status.error}` : ''}`)
      }
      await new Promise((r) => setTimeout(r, 2000))
    }
    if (!onchainId) throw new Error('等待链上确认超时（120s），请稍后查询委托状态')
    // 引擎代理：onchain txId -> Order record ciphertext（record owner 为 operator）。
    // 链上确认后节点索引可能有延迟，查询重试最多 30s
    let lastErr = ''
    for (let i = 0; i < 15; i++) {
      const res = await fetch(`/order/tx/${encodeURIComponent(onchainId)}`)
      if (res.ok) {
        const data = (await res.json()) as { ciphertext: string }
        return { txId: onchainId, ciphertext: data.ciphertext }
      }
      lastErr = await res.text()
      await new Promise((r) => setTimeout(r, 2000))
    }
    throw new Error(`获取 Order record 失败: ${lastErr || '超时'}`)
  }

  // 铸测试币：mint(amount: u64, token_id: u32)，Token record 归执行者（合约 main.leo）
  async mintToken(tokenId: number, amount: number): Promise<void> {
    if (!this.adapter) throw new Error('Shield 钱包未连接')
    await this.adapter.executeTransaction({
      program: PROGRAM_ID,
      function: 'mint',
      inputs: [`${Math.floor(amount)}u64`, `${tokenId}u32`],
      fee: 0, // Shield 覆盖 gas 模式；mint 费用模式待实测
    })
  }

  // 部署合约：钱包内置 prover 生成证书，ALEO 付部署费（Aleo Wallet Standard executeDeployment）。
  // 注意：程序名 <10 字符触发 namespace 溢价（hello43=1000+ ALEO）；anubook_dex_p2（14 字符）无溢价
  async deployProgram(): Promise<string> {
    if (!this.adapter || !this.address) throw new Error('Shield 钱包未连接')
    const res = await fetch('/programs/anubook_dex_p2.aleo')
    if (!res.ok) throw new Error(`加载合约程序失败: HTTP ${res.status}`)
    const program = await res.text()
    const result = await this.adapter.executeDeployment({
      program,
      address: this.address,
      priorityFee: 100000, // 官方 SDK 文档示例值（microcredits，0.1 ALEO）
      privateFee: false,
    })
    return result.transactionId
  }
}
