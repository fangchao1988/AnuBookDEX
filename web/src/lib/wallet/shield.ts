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
import { fetchAleoBalance, fetchOperatorAddress, fetchUsdcxPublicBalance } from '../api/orders'
import { pairMode } from '../tokens'

export const PROGRAM_ID = 'anubook_dex_p2.aleo'
// p4 真实币对（ALEO/USDCX）：跨程序托管（USDCX Token + credits.aleo）
export const PROGRAM_ID_P4 = 'anubook_dex_p5.aleo'
export const USDCX_PROGRAM_ID = 'test_usdcx_stablecoin.aleo'

// 解析 Aleo 类型化字符串（'123u64' -> 123；'2u32' -> 2；'1262u128.private' -> 1262；
// '...group' -> 0；纯数字原样返回）
function parseTyped(v: string | undefined): number {
  if (!v) return 0
  // 类型后缀（u64/u128/u32）与可见性后缀（.private/.public）都可选，
  // 链上明文 record 字段的标准格式是 '80000000u64.private'
  const n = Number(String(v).replace(/(u\d+)?(\.(private|public))?$/, ''))
  return Number.isFinite(n) ? n : 0
}

// Shield requestRecords 返回结构未定型（unknown[]，适配器直接透传），按候选路径提取字段：
// recordView.fields / data.plaintext / 顶层 plaintext（对象或 Aleo 明文字符串）。
// 都取不到时序列化整个 record 按 key 兜底。返回数值（0 = 不存在）。
function extractField(rec: unknown, key: string): number {
  const env = (rec ?? {}) as Record<string, unknown>
  const candidates: unknown[] = [
    (env.recordView as { fields?: unknown } | undefined)?.fields,
    (env.data as { plaintext?: unknown } | undefined)?.plaintext,
    env.plaintext,
  ]
  for (const c of candidates) {
    if (!c) continue
    if (typeof c === 'object' && !Array.isArray(c)) {
      const v = (c as Record<string, unknown>)[key]
      if (v !== undefined) {
        const n = parseTyped(String(v))
        if (n > 0) return n
      }
    } else if (typeof c === 'string') {
      // Aleo record 明文字符串（'  amount: 1262u128.private, ...'）
      const m = new RegExp(`(?:^|[\\s{,{])${key}\\s*:\\s*"?([0-9]+)`).exec(c)
      if (m) return Number(m[1])
    }
  }
  // 最后兜底：JSON 序列化后按 "key": "123..." 提取
  const m = new RegExp(`"${key}"\\s*:\\s*"?([0-9]+)`).exec(JSON.stringify(rec ?? ''))
  return m ? Number(m[1]) : 0
}

// record 类型名匹配（recordView.recordname 可能是 'Token' 或完整 'test_usdcx_stablecoin.aleo/Token'）
function recordIs(rec: unknown, name: string): boolean {
  return JSON.stringify(rec ?? '').includes(`"${name}"`)
}

// 人类单位 -> 最小单位（BigInt 避免 u64*u64 溢出 Number）。
// toUnits('0.015784', 6) -> 15784n；toUnits('63.3553', 6) -> 63355300n
function toUnits(v: string, decimals: number): bigint {
  const n = v.replace(/,/g, '').trim()
  const [int, frac = ''] = n.split('.')
  if (!/^\d+$/.test(int) || (frac !== '' && !/^\d+$/.test(frac)) || int === '') {
    throw new Error(`无效数值: ${v}`)
  }
  const scale = 10n ** BigInt(decimals)
  return BigInt(int) * scale + BigInt((frac + '0'.repeat(decimals)).slice(0, decimals) || '0')
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
    const account = await this.adapter.connect(this.network, WalletDecryptPermission.UponRequest, [
      PROGRAM_ID,
      PROGRAM_ID_P4,
      USDCX_PROGRAM_ID,
      // 卖单需要钱包从 credits.aleo 选 credits record 作输入（InputRequest grant 校验），
      // 缺省会报 "record input refused for credits.aleo"
      'credits.aleo',
    ])
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
    // ALEO shielded records（credits.aleo 的 Credits record）。
    // statusFilter='unspent' 只聚合未花费记录：autojoin 会消费小 credits records
    // 合并成大 record，默认 'all' 会把已消费的旧 records 一起计入导致余额虚高
    let aleoMicro = 0
    try {
      const credits = await this.adapter.requestRecords('credits.aleo', true, 'unspent')
      if (import.meta.env.DEV) {
        console.log('[shield] credits.aleo records:', JSON.stringify(credits).slice(0, 3000))
      }
      for (const rec of credits) {
        aleoMicro += extractField(rec, 'microcredits')
      }
    } catch {
      // credits.aleo 未授权/无记录：忽略，只用公开余额
    }
    aleo += aleoMicro / 1e6

    // anubook_dex_p2 Token records（测试币 USDT/ETH）
    const sum: Record<number, number> = {}
    try {
      const records = await this.adapter.requestRecords(PROGRAM_ID, true, 'unspent')
      if (import.meta.env.DEV) {
        console.log('[shield] anubook_dex_p2 records:', JSON.stringify(records).slice(0, 3000))
      }
      for (const rec of records) {
        const tid = extractField(rec, 'token_id')
        if (tid === 1 || tid === 2) {
          sum[tid] = (sum[tid] ?? 0) + extractField(rec, 'amount')
        }
      }
    } catch {
      // 无测试币记录：余额为空
    }

    // USDCX：公开余额（balances mapping）+ 隐私 Token records（p4 真实币对 quote；
    // 6 位最小单位 -> 人类单位），与 ALEO 同模式（公开 + 私有总额）
    let usdcx = 0
    try {
      usdcx += await fetchUsdcxPublicBalance(this.address)
    } catch (e) {
      if (import.meta.env.DEV) console.log('[shield] usdcx public balance query failed:', e)
    }
    try {
      const records = await this.adapter.requestRecords(USDCX_PROGRAM_ID, true, 'unspent')
      if (import.meta.env.DEV) {
        console.log('[shield] usdcx records:', JSON.stringify(records).slice(0, 3000))
      }
      for (const rec of records) {
        // 只聚合 Token record：ComplianceRecord 也带 amount 字段，不过滤会重复计入
        if (!recordIs(rec, 'Token')) continue
        usdcx += extractField(rec, 'amount')
      }
    } catch {
      // 无 USDCX 记录：余额为空
    }
    return {
      aleo: aleo > 0 ? String(Math.round(aleo * 1e6) / 1e6) : '--',
      usdt: sum[2] !== undefined ? String(sum[2]) : '--',
      base: sum[1] !== undefined ? String(sum[1]) : '--',
      usdcx: usdcx > 0 ? String(usdcx / 1e6) : '--',
    }
  }

  // place_order：按交易对模式分发（p4 真实币对 ALEO/USDCX / p2 铸币 ETH_USDT）。
  // p2：inputs[0] 为 fund Token record —— 钱包自动选择未花费记录
  // （Aleo Wallet Standard InputRequest：filters 按 token_id 匹配；买单锁 quote、卖单锁 base）
  // 注：合约 MVP 断言 fund.amount == price*amount（严格相等），用户需持有恰好金额的 Token record；
  //     record 自动选择 + 微单位换算待真钱包实测确认
  async placeOrder(params: PlaceOrderParams): Promise<PlacedOrder> {
    if (!this.adapter) throw new Error('Shield 钱包未连接')
    if (pairMode(params.symbol) === 'p4-real') {
      return this.placeOrderP4(params)
    }
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
    const onchainId = await this.executeAndAwait({
      program: PROGRAM_ID,
      function: 'place_order',
      inputs,
      fee: 0, // Shield 覆盖部分 gas（免费转账模式）；place_order 费用模式待实测
    })
    // 引擎代理：onchain txId -> Order record ciphertext（record owner 为 operator）。
    // 链上确认后节点索引可能有延迟，查询重试最多 30s；symbol 用于引擎按交易对路由程序 id
    let lastErr = ''
    for (let i = 0; i < 15; i++) {
      const res = await fetch(`/order/tx/${encodeURIComponent(onchainId)}?symbol=${encodeURIComponent(params.symbol)}`)
      if (res.ok) {
        const data = (await res.json()) as { ciphertext: string }
        return { txId: onchainId, ciphertext: data.ciphertext }
      }
      lastErr = await res.text()
      await new Promise((r) => setTimeout(r, 2000))
    }
    throw new Error(`获取 Order record 失败: ${lastErr || '超时'}`)
  }

  // place_order_buy/sell（p4 真实币对 ALEO/USDCX，6 位最小单位）：
  // - 买单：USDCX Token（锁定 (price*amount)/1e6）+ Credentials（USDCX 合规凭证）
  // - 卖单：credits.aleo credits（锁定 amount microcredits）
  // 链上确认后由引擎从 tx_id 提取+解密（Order/托管资产/凭证），不再走 /order/tx 换 ciphertext。
  private async placeOrderP4(params: PlaceOrderParams): Promise<PlacedOrder> {
    if (!this.adapter) throw new Error('Shield 钱包未连接')
    const operator = params.operator || (await fetchOperatorAddress())
    const priceU = toUnits(params.price, 6)
    const amountU = toUnits(params.amount, 6)
    if (priceU <= 0n || amountU <= 0n) throw new Error('价格/数量无效')
    // 隐私 record 选择：不依赖钱包 auto-fill（filters 匹配在钱包扩展上不可靠，
    // 实测报 "No record matching constraints"），改为 requestRecords 拿 record
    // 列表 -> 挑金额足够的最大 record -> 用规范定义的 uid 精确定位（uid 与
    // filters 互斥；旧钱包无 uid 时回退 filters）。
    // p5 合约无自动 join，单条 record 必须覆盖锁定金额（多 record 合计不足时
    // 提示用户先 join 合并）。
    if (params.side === 0) {
      const needed = (priceU * amountU) / 1000000n
      const recs = await this.adapter.requestRecords(USDCX_PROGRAM_ID, true, 'unspent')
      const tokenRecs = recs.filter((r) => recordIs(r, 'Token'))
      const total = tokenRecs.reduce<number>((s, r) => s + extractField(r, 'amount'), 0)
      // 挑单条金额足够的最大 Token record（uid 优先）
      let best: { uid?: string; amount: number } | null = null
      for (const r of tokenRecs) {
        const amt = extractField(r, 'amount')
        if (amt >= Number(needed) && (!best || amt > best.amount)) {
          best = { uid: (r as { uid?: string }).uid, amount: amt }
        }
      }
      if (!best) {
        throw new Error(
          `隐私 USDCX 不足：共 ${(total / 1e6).toFixed(6)} USDCX（${tokenRecs.length} 条 record），` +
            `买单需单条 record 锁定 ${(Number(needed) / 1e6).toFixed(6)} USDCX` +
            (total >= Number(needed) ? '；多条 record 需先 join 合并' : '')
        )
      }
      const tokenReq: TransactionInput = best.uid
        ? { type: 'record', program: USDCX_PROGRAM_ID, recordname: 'Token', uid: best.uid }
        : {
            type: 'record',
            program: USDCX_PROGRAM_ID,
            recordname: 'Token',
            filters: { amount: { gte: `${Number(needed)}u128` } },
          }
      // 合规凭证同样 requestRecords + uid 精确定位（auto-fill 在钱包扩展上
      // 不可靠，Token 用 uid 解决后只剩 Credentials 仍失败——同一根因）
      const credsRecs = recs.filter((r) => recordIs(r, 'Credentials'))
      if (credsRecs.length === 0) {
        throw new Error(
          `未找到合规凭证 Credentials record（共 ${recs.length} 条 record，无 Credentials）——` +
            `需先用 test_usdcx_stablecoin 的 get_credentials 领取合规凭证`
        )
      }
      const credsUid = (credsRecs[0] as { uid?: string }).uid
      const credsReq: TransactionInput = credsUid
        ? { type: 'record', program: USDCX_PROGRAM_ID, recordname: 'Credentials', uid: credsUid }
        : { type: 'record', program: USDCX_PROGRAM_ID, recordname: 'Credentials' }
      const inputs: TransactionInput[] = [
        tokenReq,
        credsReq,
        `${params.orderId}u128`,
        `${priceU}u64`,
        `${amountU}u64`,
        `${params.deadline}u32`,
        operator,
      ]
      const onchainId = await this.executeAndAwait({
        program: PROGRAM_ID_P4,
        function: 'place_order_buy',
        inputs,
        fee: 0, // Shield 覆盖 gas 模式；p4 费用模式待实测
      })
      return { txId: onchainId, ciphertext: '' }
    }

    // 卖单：credits record 挑 microcredits 足够的最大 record
    const recs = await this.adapter.requestRecords('credits.aleo', true, 'unspent')
    const total = recs.reduce<number>((s, r) => s + extractField(r, 'microcredits'), 0)
    let best: { uid?: string; micro: number } | null = null
    for (const r of recs) {
      const micro = extractField(r, 'microcredits')
      if (micro >= Number(amountU) && (!best || micro > best.micro)) {
        best = { uid: (r as { uid?: string }).uid, micro }
      }
    }
    if (!best) {
      const amounts = recs.map((r) => extractField(r, 'microcredits'))
      throw new Error(
        `隐私 ALEO 不足：共 ${total / 1e6} ALEO（${recs.length} 条 record：${amounts.join(', ') || '无'} microcredits），` +
          `卖单需单条 record 锁定 ${Number(amountU) / 1e6} ALEO` +
          (total >= Number(amountU) ? '；多条 record 需先 autojoin 合并' : '')
      )
    }
    const creditsReq: TransactionInput = best.uid
      ? { type: 'record', program: 'credits.aleo', recordname: 'credits', uid: best.uid }
      : {
          type: 'record',
          program: 'credits.aleo',
          recordname: 'credits',
          filters: { microcredits: { gte: `${Number(amountU)}u64` } },
        }
    const onchainId = await this.executeAndAwait({
      program: PROGRAM_ID_P4,
      function: 'place_order_sell',
      inputs: [
        creditsReq,
        `${params.orderId}u128`,
        `${priceU}u64`,
        `${amountU}u64`,
        `${params.deadline}u32`,
        operator,
      ],
      fee: 0, // Shield 覆盖 gas 模式；p4 费用模式待实测
    })
    return { txId: onchainId, ciphertext: '' }
  }

  // executeTransaction + 轮询链上确认（Shield 返回内部 ID shield_...，
  // status=accepted 才上链；期间 transactionId 可能返回但链上不可见）
  private async executeAndAwait(tx: { program: string; function: string; inputs: TransactionInput[]; fee: number }): Promise<string> {
    if (!this.adapter) throw new Error('Shield 钱包未连接')
    const result = await this.adapter.executeTransaction(tx)
    const shieldId = result.transactionId
    for (let i = 0; i < 60; i++) {
      const status = await this.adapter.transactionStatus(shieldId)
      if (status.status === 'accepted' && status.transactionId) {
        return status.transactionId
      }
      if (status.status === 'failed' || status.status === 'rejected') {
        throw new Error(`${tx.function} 交易失败: ${status.status}${status.error ? `: ${status.error}` : ''}`)
      }
      await new Promise((r) => setTimeout(r, 2000))
    }
    throw new Error('等待链上确认超时（120s），请稍后查询委托状态')
  }

  // 领取 USDCX 合规凭证：test_usdcx_stablecoin get_credentials + 非包含证明。
  // proof 是 freezelist 树（仅哨兵叶子、root 固定）的通用非包含证明：leaf_index=1,1
  // 触发断言短路（signer 区间检查被 leaf0==leaf1 短路），对任意地址有效；
  // 链上实测（operator 广播）accepted+confirmed，finalize root 校验通过。
  // 树状态变化（update_freeze_list）后需重新生成（当前 MVP 树固定）。
  private static readonly CREDENTIALS_PROOF =
    '[{siblings: [0field, 0field, 0field, 0field, 0field, 0field, 0field, 0field, 0field, 0field, 0field, 0field, 0field, 0field, 0field, 0field], leaf_index: 1u32}, {siblings: [0field, 0field, 0field, 0field, 0field, 0field, 0field, 0field, 0field, 0field, 0field, 0field, 0field, 0field, 0field, 0field], leaf_index: 1u32}]'

  async getCredentials(): Promise<void> {
    if (!this.adapter) throw new Error('Shield 钱包未连接')
    await this.executeAndAwait({
      program: USDCX_PROGRAM_ID,
      function: 'get_credentials',
      inputs: [ShieldWalletAdapter.CREDENTIALS_PROOF],
      fee: 0, // Shield 覆盖 gas 模式
    })
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
