// Aleo 链下订单通道（Phase 2b）：POST /order
// 后端契约见 internal/dex/chain/aleo/orderpool.go HandleOrder：
// {order_id, side(0买/1卖), price, amount, base_token, quote_token, deadline, trader, ciphertext}
// 成功返回 "ok"；失败 400 + 错误消息。

export interface AleoOrderRequest {
  order_id: number
  symbol?: string // 交易对（ETH_USDT），委托记录用
  side: number // 0=buy, 1=sell
  price: string
  amount: string
  base_token: number
  quote_token: number
  deadline: number // 过期秒数（Unix 秒）
  trader: string
  ciphertext: string // Order record ciphertext（生产必填；dev 开关允许空）
}

export interface AleoOrderResult {
  ok: boolean
  error?: string
}

// 前端生成订单 ID（毫秒时间戳；后续钱包流程可与链上 order_id 对齐）
export function nextOrderId(): number {
  return Date.now()
}

export async function submitAleoOrder(req: AleoOrderRequest): Promise<AleoOrderResult> {
  try {
    const res = await fetch('/order', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
    })
    const text = await res.text()
    if (res.ok) return { ok: true }
    return { ok: false, error: text || `HTTP ${res.status}` }
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : String(e) }
  }
}

// 后端订单状态记录（P3 委托列表）：GET /api/v1/orders?trader=&symbol=&limit=
export interface OrderRecord {
  order_id: number
  symbol: string
  trader: string
  side: 'buy' | 'sell'
  type: string
  price: string
  amount: string
  filled: string
  status: 'waiting' | 'partial' | 'filled' | 'canceled'
  create_at: number
}

export async function fetchOrders(params: { trader?: string; symbol?: string; limit?: number } = {}): Promise<OrderRecord[]> {
  const qs = new URLSearchParams()
  if (params.trader) qs.set('trader', params.trader)
  if (params.symbol) qs.set('symbol', params.symbol)
  if (params.limit) qs.set('limit', String(params.limit))
  const res = await fetch(`/api/v1/orders?${qs.toString()}`)
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return (await res.json()) as OrderRecord[]
}

// 状态显示映射（后端 -> 前端 UI）
export const STATUS_LABEL: Record<OrderRecord['status'], { text: string; cls: 'orange' | 'blue' | 'green' | 'red' }> = {
  waiting: { text: '等待中', cls: 'orange' },
  partial: { text: '部分成交', cls: 'blue' },
  filled: { text: '已完成', cls: 'green' },
  canceled: { text: '已撤销', cls: 'red' },
}

// 结算状态显示（链上 settle）
export const SETTLE_LABEL: Record<string, { text: string; cls: 'green' | 'orange' | 'red' }> = {
  settled: { text: '已结算', cls: 'green' },
  settling: { text: '结算中', cls: 'orange' },
  pending: { text: '待结算', cls: 'orange' },
  failed: { text: '结算失败', cls: 'red' },
}

// 链上撤单：POST /order/cancel
export async function cancelOrder(orderId: number): Promise<AleoOrderResult> {
  try {
    const res = await fetch('/order/cancel', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ order_id: orderId }),
    })
    const text = await res.text()
    if (res.ok) return { ok: true }
    return { ok: false, error: text || `HTTP ${res.status}` }
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : String(e) }
  }
}

// 成交记录（P3 真实数据）：GET /api/v1/trades?trader=&symbol=&limit=
export interface TradeRecord {
  order_id: number
  symbol: string
  side: 'buy' | 'sell'
  price: string
  amount: string
  trader: string
  taker: string
  ts: number
  settle_status?: 'pending' | 'settled' | 'failed'
}

// 隐私下单：不发送明文订单，仅提交链上交易 ID（引擎用 operator view key 解密撮合）
export async function submitPrivacyOrder(req: {
  tx_id: string
  symbol: string
  trader: string
}): Promise<AleoOrderResult> {
  try {
    const res = await fetch('/order/privacy', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
    })
    const text = await res.text()
    if (res.ok) return { ok: true }
    return { ok: false, error: text || `HTTP ${res.status}` }
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : String(e) }
  }
}

// 统一 tx_id 下单（p4 真实币对 ALEO/USDCX，标准/隐私下单共用）：
// 前端只提交链上交易 id（订单参数全部在链上加密 record 中），
// 引擎从交易提取 + operator view key 解密（POST /order tx_id 模式）
export async function submitTxOrder(req: {
  tx_id: string
  symbol: string
  trader: string
}): Promise<AleoOrderResult> {
  try {
    const res = await fetch('/order', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
    })
    const text = await res.text()
    if (res.ok) return { ok: true }
    return { ok: false, error: text || `HTTP ${res.status}` }
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : String(e) }
  }
}

// 市场最近成交历史（引擎 Hub 缓存回放，market.fills 原始帧数组，最新在前）
export async function fetchMarketTrades(symbol: string, limit = 50): Promise<WsTradeFrame[]> {
  const res = await fetch(`/api/v1/market/trades?symbol=${encodeURIComponent(symbol)}&limit=${limit}`)
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return (await res.json()) as WsTradeFrame[]
}

export interface WsTradeFrame {
  type: string
  pairCode: string
  data: { vol: string; ts: number; id: number; price: string; direction: string }[]
}

export async function fetchTrades(params: { trader?: string; symbol?: string; limit?: number } = {}): Promise<TradeRecord[]> {
  const qs = new URLSearchParams()
  if (params.trader) qs.set('trader', params.trader)
  if (params.symbol) qs.set('symbol', params.symbol)
  if (params.limit) qs.set('limit', String(params.limit))
  const res = await fetch(`/api/v1/trades?${qs.toString()}`)
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return (await res.json()) as TradeRecord[]
}

// 引擎 operator 地址（place_order 的 Order record owner），来自 chain.aleo.address 配置
export async function fetchOperatorAddress(): Promise<string> {
  const res = await fetch('/api/v1/operator')
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  const data = (await res.json()) as { address: string }
  return data.address
}

// 链上 ALEO 公开余额（引擎代理 snarkOS credits.aleo account mapping）
export interface AleoBalance {
  aleo: number
  microcredits: number
}

export async function fetchAleoBalance(address: string): Promise<AleoBalance> {
  // 优先引擎代理；失败或返回 0（旧引擎解析 bug）时直连 snarkOS（CORS 开放，作为兜底）
  try {
    const res = await fetch(`/api/v1/balance/${encodeURIComponent(address)}`)
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    const data = (await res.json()) as AleoBalance
    if (data.aleo > 0) return data
    throw new Error('engine balance returned 0')
  } catch {
    const direct = await fetch(
      `https://api.explorer.provable.com/v1/testnet/program/credits.aleo/mapping/account/${encodeURIComponent(address)}`,
    )
    if (!direct.ok) throw new Error(`HTTP ${direct.status}`)
    const raw = (await direct.text()).replace(/"/g, '').replace(/u64$/, '')
    const microcredits = Number(raw)
    return { aleo: microcredits / 1e6, microcredits }
  }
}

// 链上 USDCX 公开余额（test_usdcx_stablecoin balances mapping，u128 微单位，
// 1 USDCX = 1e6 微单位）。返回微单位整数；地址无记录（null）视为 0。
export async function fetchUsdcxPublicBalance(address: string): Promise<number> {
  const res = await fetch(
    `https://api.explorer.provable.com/v1/testnet/program/test_usdcx_stablecoin.aleo/mapping/balances/${encodeURIComponent(address)}`,
  )
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  const raw = (await res.text()).trim()
  if (raw === 'null') return 0
  // 响应格式为裸值："324960597u128"
  const micro = Number(raw.replace(/"/g, '').replace(/u128$/, ''))
  return Number.isFinite(micro) ? micro : 0
}
