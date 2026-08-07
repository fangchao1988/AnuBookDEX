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
