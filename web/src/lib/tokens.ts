// 链上 token 注册表（u32 token_id）：
// 以 contracts/leo/src/main.leo 注释为准（1=ETH, 2=USDT），
// 其他资产待合约注册后补充（BTC 等）。
// 用法：baseToken('ETH_USDT') -> 1, quoteToken('ETH_USDT') -> 2

export const TOKEN_IDS: Record<string, number> = {
  ETH: 1,
  USDT: 2,
}

export const DEFAULT_TOKEN = 0

// 交易对（后端 symbol 命名 ETH_USDT）-> [base_token, quote_token]
export function pairTokens(symbol: string): { base: number; quote: number } {
  const [base, quote] = symbol.split('_')
  return {
    base: TOKEN_IDS[base] ?? DEFAULT_TOKEN,
    quote: TOKEN_IDS[quote] ?? DEFAULT_TOKEN,
  }
}

// ── 交易对模式（p2 铸币 / p4 真实币对）──────────────────────────────
// p2-mint：anubook_dex_p2.aleo place_order，Token record 锁仓，u32 token_id（ETH/USDT、BTC/USDT）
// p4-real：anubook_dex_p4.aleo place_order_buy/sell，买单 USDCX Token+Credentials、
//          卖单 credits.aleo credits，价格/数量为 6 位最小单位（ALEO/USDCX）
export type PairMode = 'p2-mint' | 'p4-real'

export const PAIR_MODES: Record<string, PairMode> = {
  ETH_USDT: 'p2-mint',
  BTC_USDT: 'p2-mint',
  ALEO_USDCX: 'p4-real',
}

export function pairMode(symbol: string): PairMode {
  return PAIR_MODES[symbol] ?? 'p2-mint'
}

// 最小单位精度：p4 真实币对 6 位（价格 15784u64 = 0.015784 USDCX，
// 数量 63355300u64 = 63.3553 ALEO）；p2 铸币模式为整数（价格 100 = 100 USDT）
export function pairDecimals(symbol: string): number {
  return pairMode(symbol) === 'p4-real' ? 6 : 0
}

// 最小单位 -> 人类单位（行情/委托显示用；p2 原样返回）
export function scalePairValue(symbol: string, value: unknown): string {
  const d = pairDecimals(symbol)
  const s = String(value)
  if (d === 0 || s === '') return s
  const n = Number(s)
  if (!Number.isFinite(n)) return s
  return (n / 10 ** d).toFixed(d).replace(/\.?0+$/, '')
}
