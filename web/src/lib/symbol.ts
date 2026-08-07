// 交易对命名映射：路由 BTCUSDT（展示 BTC/USDT） ↔ 后端频道 BTC_USDT
// 后端 symbols 配置使用下划线（config.example.yaml: ETH_USDT / BTC_USDT）

// 展示名 -> 后端频道名（/ -> _）
export function toChannelSymbol(symbol: string): string {
  return symbol.replace('/', '_')
}

// 后端频道名 -> 展示名（_ -> /）
export function toDisplaySymbol(symbol: string): string {
  return symbol.replace('_', '/')
}

// 展示名 -> 路由名（去掉 /）
export function toRouteSymbol(symbol: string): string {
  return symbol.replace('/', '')
}

// 路由名 -> 展示名（BTCUSDT -> BTC/USDT，按 quote 币种后缀插入 /）
export function parseRouteSymbol(route: string): string {
  if (route.includes('/')) return route
  const match = route.match(/^([A-Z0-9]+?)(USDT|USDC|USD)$/i)
  if (match) return `${match[1]}/${match[2].toUpperCase()}`
  return route
}
