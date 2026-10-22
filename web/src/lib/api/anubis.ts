// Anubis 链下订单通道：POST /order + 隐私载荷加密。
//
// 后端契约见 internal/dex/chain/anubis/orderpool.go：
//   - mode=public  ：明文字段直传 {order_id, symbol, side, price, amount, deadline, trader}
//   - mode=private ：ciphertext = hex(EncryptedOrder JSON)，引擎用 view key 解密
//   - 两者都可带 signature（personal_sign 结果，可选）与 nullifier（防重放）
//
// 引擎结算：Settlement v2 submitBatch（requireProof=false 承诺哈希模式）。

import { deriveNullifier, encryptOrder, fetchEciesPubkey, orderPlaintext } from '../crypto/ecies'

/**
 * Anubis 引擎路径前缀。
 * 部署时与 Aleo 引擎并存（各自独立容器）：nginx 把 /anubis/* 去掉前缀后
 * 转发给 Anubis 引擎，因此前端所有 Anubis 请求都要带此前缀。
 * 本地开发由 vite proxy 转发（见 vite.config.ts）。
 */
export const ANUBIS_BASE = '/anubis'

export interface AnubisOrderRequest {
  order_id: number
  symbol: string
  side: 0 | 1
  price: string
  amount: string
  deadline: number
  trader: string
  mode: 'public' | 'private'
  ciphertext?: string
  signature?: string
  nullifier?: string
}

export interface OrderResult {
  ok: boolean
  error?: string
  nullifier?: string
}

async function post(path: string, body: unknown): Promise<OrderResult> {
  try {
    const res = await fetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    const text = await res.text()
    if (res.ok) return { ok: true }
    return { ok: false, error: text || `HTTP ${res.status}` }
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : String(e) }
  }
}

const bytesToHex = (b: Uint8Array) =>
  Array.from(b)
    .map((x) => x.toString(16).padStart(2, '0'))
    .join('')

/**
 * 提交 Anubis 订单。
 * @param sign 可选签名函数（EVM 钱包 personal_sign）；提供则为订单摘要签名
 */
export async function submitAnubisOrder(
  req: Omit<AnubisOrderRequest, 'mode' | 'ciphertext' | 'signature' | 'nullifier'> & {
    privacy: boolean
    sign?: (msg: string) => Promise<string>
  },
): Promise<OrderResult> {
  const base = {
    order_id: req.order_id,
    symbol: req.symbol,
    side: req.side,
    deadline: req.deadline,
    trader: req.trader,
  }

  // 可选签名：对确定性摘要签名（非加密载荷，保证幂等重试用同一摘要）
  let signature: string | undefined
  if (req.sign) {
    const msg = [
      'AnuBookDEX Anubis order',
      `order_id=${req.order_id}`,
      `symbol=${req.symbol}`,
      `side=${req.side === 0 ? 'buy' : 'sell'}`,
      `price=${req.price}`,
      `amount=${req.amount}`,
      `trader=${req.trader}`,
      `deadline=${req.deadline}`,
    ].join('\n')
    try {
      signature = await req.sign(msg)
    } catch {
      signature = undefined // 签名失败不阻断下单（引擎不强制校验）
    }
  }

  // 隐私模式：ECIES 加密订单明文
  if (req.privacy) {
    const pub = await fetchEciesPubkey()
    if (!pub.enabled || !pub.pubkey) {
      return { ok: false, error: '引擎未启用隐私模式（未配置 view key），请改用标准下单' }
    }
    const nullifier = await deriveNullifier(req.order_id, req.trader)
    const plain = orderPlaintext({
      orderId: req.order_id,
      side: req.side,
      price: req.price,
      amount: req.amount,
      deadline: req.deadline,
    })
    const payload = await encryptOrder(pub.pubkey, plain, nullifier, req.deadline)
    // 后端期望 hex(EncryptedOrder JSON)
    const json = JSON.stringify(payload)
    const ciphertext = `0x${bytesToHex(new TextEncoder().encode(json))}`
    return {
      ...(await post(`${ANUBIS_BASE}/order`, {
        ...base,
        mode: 'private',
        ciphertext,
        signature,
        nullifier: bytesToHex(nullifier),
      })),
      nullifier: bytesToHex(nullifier),
    }
  }

  // 标准模式：明文直传
  return post(`${ANUBIS_BASE}/order`, {
    ...base,
    mode: 'public',
    price: req.price,
    amount: req.amount,
    signature,
  })
}

// 撤单：POST /order/cancel（MVP 链下撤单）
export async function cancelAnubisOrder(orderId: number): Promise<OrderResult> {
  return post(`${ANUBIS_BASE}/order/cancel`, { order_id: orderId })
}

// 委托列表：GET /api/v1/orders?trader=&symbol=&limit=
export interface AnubisOrderRecord {
  order_id: number
  symbol: string
  side: number
  price: string
  amount: string
  trader: string
  mode: string
  state: string // submitted | matched | canceled
  create_at: number
  deadline: number
  nullifier?: string
  tx_hash?: string
}

export async function fetchAnubisOrders(params: { trader?: string; symbol?: string; limit?: number } = {}): Promise<AnubisOrderRecord[]> {
  const qs = new URLSearchParams()
  if (params.trader) qs.set('trader', params.trader)
  if (params.symbol) qs.set('symbol', params.symbol)
  if (params.limit) qs.set('limit', String(params.limit))
  const res = await fetch(`${ANUBIS_BASE}/api/v1/orders?${qs.toString()}`)
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return (await res.json()) as AnubisOrderRecord[]
}

// 成交记录：GET /api/v1/trades?symbol=&limit=
export interface AnubisTradeRecord {
  order_id: number
  symbol: string
  side: number // taker 方向：0=buy, 1=sell
  price: string
  amount: string
  buyer: string // 买方地址
  seller: string // 卖方地址
  ts: number
  settle_status: string // queued（待结算）| confirmed（已上链确认，旧值 sent 兼容）
  tx_hash?: string
}

/** 从「我的地址」视角解读一笔成交：方向 + 对手方。 */
export function tradePerspective(
  t: AnubisTradeRecord,
  myAddress: string,
): { side: 'buy' | 'sell'; counterparty: string; mine: boolean } {
  const me = myAddress.toLowerCase()
  const isBuyer = t.buyer.toLowerCase() === me
  const isSeller = t.seller.toLowerCase() === me
  if (isBuyer) return { side: 'buy', counterparty: t.seller, mine: true }
  if (isSeller) return { side: 'sell', counterparty: t.buyer, mine: true }
  // 非本人参与的成交：按 taker 方向展示
  return {
    side: t.side === 0 ? 'buy' : 'sell',
    counterparty: t.side === 0 ? t.seller : t.buyer,
    mine: false,
  }
}

export async function fetchAnubisTrades(params: { symbol?: string; limit?: number } = {}): Promise<AnubisTradeRecord[]> {
  const qs = new URLSearchParams()
  if (params.symbol) qs.set('symbol', params.symbol)
  if (params.limit) qs.set('limit', String(params.limit))
  const res = await fetch(`${ANUBIS_BASE}/api/v1/trades?${qs.toString()}`)
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return (await res.json()) as AnubisTradeRecord[]
}

export const STATE_LABEL: Record<string, string> = {
  submitted: '等待中',
  matched: '已撮合',
  canceled: '已撤销',
}

/**
 * Anubis 结算状态 → 展示标签。
 * 语义：后端只在**回执 status==1（已上链确认）**后才把状态从 queued 改为 confirmed，
 * 因此 confirmed 表示「已结算」而非「已广播」。
 *   queued    已记账、尚未确认（可能未提交，或已广播但未等到回执）
 *   confirmed 链上确认成功
 * 保留 sent 兼容旧数据。
 */
export const ANUBIS_SETTLE_LABEL: Record<string, { text: string; cls: 'green' | 'orange' | 'red' }> = {
  queued: { text: '待结算', cls: 'orange' },
  sent: { text: '已结算', cls: 'green' },
  confirmed: { text: '已结算', cls: 'green' },
  failed: { text: '结算失败', cls: 'red' },
}

/** 取结算状态标签（未知值原样返回，避免误标为已结算） */
export function settleLabel(raw: string | undefined) {
  const hit = ANUBIS_SETTLE_LABEL[raw ?? 'queued']
  return hit ?? { text: raw ?? '--', cls: 'orange' as const }
}


// ── 金库入金/提现（Settlement v3 真实代币结算）────────────────

export interface TokenInfo {
  address: string
  symbol: string
  decimals: number
}

export interface PairTokens {
  symbol: string
  base: TokenInfo
  quote: TokenInfo
}

export interface ChainTokens {
  settlement: string // 结算合约地址（approve/deposit 目标）
  pairs: PairTokens[]
}

/** 结算合约地址 + 币对代币清单（引擎补链上 symbol/decimals） */
export async function fetchChainTokens(): Promise<ChainTokens> {
  const res = await fetch(`${ANUBIS_BASE}/api/v1/tokens`)
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return (await res.json()) as ChainTokens
}

/** 把代币数量格式化为人类可读（按 decimals 缩放） */
export function formatToken(raw: bigint, decimals: number): string {
  if (decimals === 0) return raw.toString()
  const s = raw.toString().padStart(decimals + 1, '0')
  const int = s.slice(0, -decimals)
  const frac = s.slice(-decimals).replace(/0+$/, '')
  return frac ? `${int}.${frac}` : int
}

/** 人类可读数量 -> 最小单位（按 decimals 缩放） */
export function parseToken(human: string, decimals: number): bigint {
  const clean = human.replace(/,/g, '').trim()
  if (!clean || Number(clean) <= 0) return 0n
  const [int, frac = ''] = clean.split('.')
  const scale = 10n ** BigInt(decimals)
  return BigInt(int) * scale + BigInt((frac + '0'.repeat(decimals)).slice(0, decimals) || '0')
}
