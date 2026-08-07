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
import { fetchOperatorAddress } from '../api/orders'

export const PROGRAM_ID = 'anubook_dex_p2.aleo'

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

  // 链上余额：requestRecords 聚合 Token record（token_id 1=ETH, 2=USDT，合约 main.leo）
  // amount 为合约 u64 整数单位；需真钱包实测 record 明文结构后校准换算
  async getBalances(_baseSymbol: string): Promise<WalletBalances> {
    if (!this.adapter) throw new Error('Shield 钱包未连接')
    const records = await this.adapter.requestRecords(PROGRAM_ID, true)
    const sum = { 1: 0, 2: 0 } // token_id -> 总量（1=ETH, 2=USDT）
    for (const rec of records as unknown as { data?: Record<string, unknown> }[]) {
      const d = rec.data ?? {}
      const tid = Number(d.token_id ?? -1)
      if (tid === 1 || tid === 2) sum[tid] += Number(d.amount ?? 0)
    }
    return {
      usdt: sum[2] > 0 ? String(sum[2]) : '--',
      base: sum[1] > 0 ? String(sum[1]) : '--',
    }
  }

  // place_order：锁仓 Token record -> executeTransaction -> txId -> 引擎代理换 ciphertext
  async placeOrder(params: PlaceOrderParams): Promise<PlacedOrder> {
    if (!this.adapter) throw new Error('Shield 钱包未连接')
    // operator 地址来自引擎配置（chain.aleo.address），place_order 的 Order record 归 operator
    const operator = params.operator || (await fetchOperatorAddress())
    // inputs（Aleo Wallet Standard）：fund record + 9 个字面量参数
    // 注：fund record 需从 requestRecords 选择未花费 Token record（买单锁 quote=price*amount）；
    // record 选择逻辑与微单位换算需真钱包实测
    const inputs: TransactionInput[] = [
      // TODO: fund record（买单锁 quote=price*amount 的 USDT record；卖单锁 base 的 ETH record）
      //   { type: 'record', record: fundRecordCiphertext }，需从 requestRecords 选择未花费记录
      `${params.orderId}u128`,
      `${params.side}u8`,
      `${Math.round(Number(params.price))}u64`,
      `${Math.round(Number(params.amount))}u64`,
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
    // 引擎代理：txId -> Order record ciphertext（record owner 为 operator，需节点查询输出）
    const res = await fetch(`/order/tx/${encodeURIComponent(result.transactionId)}`)
    if (!res.ok) throw new Error(`获取 Order record 失败: ${await res.text()}`)
    const data = (await res.json()) as { ciphertext: string }
    return { txId: result.transactionId, ciphertext: data.ciphertext }
  }
}
