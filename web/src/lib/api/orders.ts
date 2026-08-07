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
  status: 'waiting' | 'partial' | 'filled'
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
export const STATUS_LABEL: Record<OrderRecord['status'], { text: string; cls: 'orange' | 'blue' | 'green' }> = {
  waiting: { text: '等待中', cls: 'orange' },
  partial: { text: '部分成交', cls: 'blue' },
  filled: { text: '已完成', cls: 'green' },
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
