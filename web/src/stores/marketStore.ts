import { create } from 'zustand'
import { wsClient, type WsStatus, type WsPayload } from '../lib/ws/client'

// 实时行情 store（P2）：订阅频道 -> 增量维护订单簿/K线/成交/Ticker。
// 频道命名沿用后端：depth.BTC_USDT / kline.BTC_USDT.1min / trade.BTC_USDT / ticker.BTC_USDT

export interface DepthRow {
  price: string
  qty: string
  total: string
  pct: number
}

export interface TradeRow {
  price: string
  qty: string
  time: number
  side: 'buy' | 'sell'
}

export interface Candle {
  time: number // 秒
  open: number
  high: number
  low: number
  close: number
  volume: number
}

export interface TickerData {
  open: string
  close: string
  high: string
  low: string
  vol: string
  turnOver: string
  change: string
  changePercent: string
  bidPrice: string
  askPrice: string
}

interface MarketState {
  status: WsStatus
  depths: Record<string, { bids: Map<string, string>; asks: Map<string, string> }>
  trades: Record<string, TradeRow[]> // 最新在前，上限 100
  klines: Record<string, Record<string, Candle[]>> // symbol -> interval -> candles（旧->新）
  tickers: Record<string, TickerData>
  setStatus: (s: WsStatus) => void
  hasDepth: (symbol: string) => boolean
  getDepthRows: (symbol: string, side: 'bids' | 'asks', limit: number) => DepthRow[]
  getTrades: (symbol: string, limit: number) => TradeRow[]
  getKlines: (symbol: string, interval: string) => Candle[]
  getTicker: (symbol: string) => TickerData | null
  getLastPrice: (symbol: string) => string | null
}

const MAX_TRADES = 100

export const useMarket = create<MarketState>()((set, get) => ({
  status: 'idle',
  depths: {},
  trades: {},
  klines: {},
  tickers: {},

  setStatus: (status) => set({ status }),

  hasDepth: (symbol) => {
    const d = get().depths[symbol]
    return !!(d && (d.bids.size > 0 || d.asks.size > 0))
  },

  getDepthRows: (symbol, side, limit) => {
    const d = get().depths[symbol]
    if (!d) return []
    const map = side === 'bids' ? d.bids : d.asks
    const rows: DepthRow[] = []
    let total = 0
    let maxQty = 0
    const entries = [...map.entries()]
    // bids 价高在前，asks 价高在前（产品要求卖盘从高到低）
    entries.sort((a, b) => +b[0] - +a[0])
    for (const [price, qty] of entries) {
      total += Number(qty)
      maxQty = Math.max(maxQty, Number(qty))
      rows.push({ price, qty, total: total.toFixed(4), pct: 0 })
      if (rows.length >= limit) break
    }
    // 深度条宽度按档位最大量归一
    for (const r of rows) r.pct = Math.min((Number(r.qty) / maxQty) * 100, 100)
    return rows
  },

  getTrades: (symbol, limit) => get().trades[symbol]?.slice(0, limit) ?? [],
  getKlines: (symbol, interval) => get().klines[symbol]?.[interval] ?? [],
  getTicker: (symbol) => get().tickers[symbol] ?? null,
  getLastPrice: (symbol) => {
    const t = get().tickers[symbol]
    if (t && t.close) return t.close
    const trades = get().trades[symbol]
    return trades && trades.length > 0 ? trades[0].price : null
  },
}))

// ============ 频道订阅与数据入库 ============

// 订阅/退订一组频道（幂等，WsClient 内部 Set 去重）
export function subscribeChannels(channels: string[]) {
  wsClient.subscribe(channels)
}

export function unsubscribeChannels(channels: string[]) {
  wsClient.unsubscribe(channels)
}

// 单一全局消息 handler：按 payload.type 分发到对应 store 更新
wsClient.onMessage(({ payload }) => {
  const symbol = payload.pairCode
  if (!symbol) return
  switch (payload.type) {
    case 'market.orderBook':
      applyDepth(symbol, payload)
      break
    case 'market.fills':
      applyTrade(symbol, payload)
      break
    case 'market.candles':
      if (payload.interval) applyKline(symbol, payload.interval, payload)
      break
    case 'market.ticker':
      applyTicker(symbol, payload)
      break
  }
})

// 连接状态同步到 store（供 UI 显示 LIVE/断线标识）
wsClient.onStatus((status) => useMarket.getState().setStatus(status))

function applyDepth(symbol: string, p: WsPayload) {
  const bids = (p.bids as [string, string][]) ?? []
  const asks = (p.asks as [string, string][]) ?? []
  if (bids.length === 0 && asks.length === 0) return
  useMarket.setState((s) => {
    // 后端广播为全量快照帧（QuoteDepths.bids/asks），整体替换，顺序由后端保证
    const next = { bids: new Map<string, string>(), asks: new Map<string, string>() }
    for (const [price, qty] of bids) next.bids.set(String(price), String(qty))
    for (const [price, qty] of asks) next.asks.set(String(price), String(qty))
    return { depths: { ...s.depths, [symbol]: next } }
  })
}

function applyTrade(symbol: string, p: WsPayload) {
  const list = (p.data as { price: string; vol: string; ts: number; direction: string }[]) ?? []
  if (list.length === 0) return
  const rows: TradeRow[] = list.map((t) => ({
    price: String(t.price),
    qty: String(t.vol),
    time: Number(t.ts),
    side: t.direction === 'buy' ? 'buy' : 'sell',
  }))
  useMarket.setState((s) => {
    const prev = s.trades[symbol] ?? []
    return { trades: { ...s.trades, [symbol]: [...rows.reverse(), ...prev].slice(0, MAX_TRADES) } }
  })
}

function applyKline(symbol: string, interval: string, p: WsPayload) {
  const k = (p.data as Record<string, unknown>) ?? {}
  const ts = Number(k.id ?? 0)
  if (!ts) return
  const candle: Candle = {
    time: ts,
    open: Number(k.open),
    high: Number(k.high),
    low: Number(k.low),
    close: Number(k.close),
    volume: Number(k.vol ?? k.turnOver ?? 0),
  }
  useMarket.setState((s) => {
    const byInterval = s.klines[symbol] ?? {}
    const prev = byInterval[interval] ?? []
    // 同一周期时间戳则更新最后一根，否则追加（上限 1440 根）
    let next: Candle[]
    if (prev.length > 0 && prev[prev.length - 1].time === ts) {
      next = [...prev.slice(0, -1), candle]
    } else {
      next = [...prev, candle].slice(-1440)
    }
    return { klines: { ...s.klines, [symbol]: { ...byInterval, [interval]: next } } }
  })
}

function applyTicker(symbol: string, p: WsPayload) {
  const t = (p.data as Record<string, unknown>) ?? {}
  if (!t.close) return
  useMarket.setState((s) => ({
    tickers: {
      ...s.tickers,
      [symbol]: {
        open: String(t.open ?? ''),
        close: String(t.close),
        high: String(t.high ?? ''),
        low: String(t.low ?? ''),
        vol: String(t.vol ?? ''),
        turnOver: String(t.turnOver ?? ''),
        change: String(t.change ?? ''),
        changePercent: String(t.changePercent ?? ''),
        bidPrice: String(t.bidPrice ?? ''),
        askPrice: String(t.askPrice ?? ''),
      },
    },
  }))
}
