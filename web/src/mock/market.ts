// P1 mock 行情数据：照搬原型 JS 生成器（genDepth/genTrades），K线用固定种子随机游走
import type { UTCTimestamp } from 'lightweight-charts'

export interface DepthRow {
  price: string
  amt: string
  total: string
  pct: number
}

export interface TradeRow {
  price: string
  amt: string
  time: string
  side: 'buy' | 'sell'
}

export interface Candle {
  time: UTCTimestamp
  open: number
  high: number
  low: number
  close: number
  volume: number
}

export const LAST_PRICE = 68245.3
export const TICKER = {
  last: LAST_PRICE,
  change: '+1,234.50 (+1.84%)',
  high: '68,890.00',
  low: '66,520.00',
  vol: '72,341 BTC',
  mark: '68,250.00',
  funding: '0.01%',
}

// 原型 genDepth
export function genDepth(base: number, count: number, isAsk: boolean): DepthRow[] {
  const rows: DepthRow[] = []
  let cumTotal = 0
  const maxAmt = 3.5
  for (let i = 0; i < count; i++) {
    const offset = isAsk ? i * 5 + Math.random() * 10 : -(i * 5 + Math.random() * 10)
    const price = (base + offset).toFixed(2)
    const amt = (Math.random() * 3 + 0.1).toFixed(4)
    cumTotal += parseFloat(amt)
    const pct = Math.min((parseFloat(amt) / maxAmt) * 100, 100).toFixed(0)
    rows.push({ price, amt, total: cumTotal.toFixed(4), pct: parseInt(pct, 10) })
  }
  return rows
}

// 原型 genTrades
export function genTrades(base: number, count: number): TradeRow[] {
  const rows: TradeRow[] = []
  const now = Date.now()
  for (let i = 0; i < count; i++) {
    const offset = (Math.random() - 0.5) * 20
    const price = (base + offset).toFixed(2)
    const amt = (Math.random() * 2 + 0.01).toFixed(4)
    const isBuy = Math.random() > 0.5
    const time = new Date(now - (i * 3 + Math.floor(Math.random() * 3)) * 1000)
    const p = (n: number) => String(n).padStart(2, '0')
    rows.push({
      price,
      amt,
      time: `${p(time.getHours())}:${p(time.getMinutes())}:${p(time.getSeconds())}`,
      side: isBuy ? 'buy' : 'sell',
    })
  }
  return rows
}

// 固定种子随机数（模块级常量，保证重渲染稳定）
function mulberry32(seed: number) {
  let a = seed
  return () => {
    a |= 0
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

function generateCandles(): Candle[] {
  const rnd = mulberry32(42)
  const count = 240
  const candles: Candle[] = []
  let price = 58000
  const now = Date.now()
  const hour = 3600_000
  for (let i = 0; i < count; i++) {
    const t = now - (count - i) * hour
    const drift = 68245 - 58000
    const target = 58000 + (drift * (i + 1)) / count
    const noise = (rnd() - 0.5) * 90
    const close = price + noise + (target - price) * 0.06
    const open = price
    const wick = (rnd() - 0.5) * 60
    const high = Math.max(open, close) + Math.abs(wick)
    const low = Math.min(open, close) - Math.abs(wick)
    candles.push({
      time: (t / 1000) as UTCTimestamp,
      open: +open.toFixed(2),
      high: +high.toFixed(2),
      low: +low.toFixed(2),
      close: +close.toFixed(2),
      volume: +(50 + rnd() * 350).toFixed(1),
    })
    price = close
  }
  return candles
}

export const MOCK_CANDLES: Candle[] = generateCandles()

export const MA_RANGES: Record<string, number> = { MA5: 5, MA10: 10, MA30: 30 }

// 通用 MA 计算：接受 {time, close} 形状（实时 store 与 mock 均满足）
export function calcMA(
  candles: { time: number; close: number }[],
  period: number,
): { time: UTCTimestamp; value: number }[] {
  const out: { time: UTCTimestamp; value: number }[] = []
  let sum = 0
  for (let i = 0; i < candles.length; i++) {
    sum += candles[i].close
    if (i >= period) sum -= candles[i - period].close
    if (i >= period - 1) {
      out.push({ time: candles[i].time as UTCTimestamp, value: +(sum / period).toFixed(2) })
    }
  }
  return out
}

// AI 点位（P1 mock：支撑线 + 买入标记，P5 接后端 AI 引擎）
export const AI_LEVELS = {
  support: 67800,
  buyMarker: { index: 150, price: 68150, text: 'AI 买入' },
}
