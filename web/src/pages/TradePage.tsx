import { useEffect } from 'react'
import { useParams } from 'react-router-dom'
import { OrderBook } from '../features/market/OrderBook'
import { ChartPanel } from '../features/market/ChartPanel'
import { OrderForm } from '../features/trading/OrderForm'
import { PositionsPanel } from '../features/trading/PositionsPanel'
import { OrdersPanel } from '../features/trading/OrdersPanel'
import { useSettings } from '../stores/settings'
import { parseRouteSymbol } from '../lib/symbol'

// 交易页：对齐原型 #trading-view 网格布局（260px | 1fr | 340px，底部 200px）
export default function TradePage() {
  const { symbol: routeSymbol } = useParams<{ symbol: string }>()
  const pair = useSettings((s) => s.pair)
  const setPair = useSettings((s) => s.setPair)

  // 路由参数（BTCUSDT）同步到 store pair（BTC/USDT）
  useEffect(() => {
    if (routeSymbol) {
      const parsed = parseRouteSymbol(routeSymbol)
      if (parsed !== pair) setPair(parsed)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [routeSymbol])

  return (
    <div
      className="grid gap-px bg-line h-full"
      style={{
        gridTemplateColumns: '260px minmax(0,1fr) 340px',
        gridTemplateRows: 'minmax(0,1fr) 200px',
      }}
    >
      <OrderBook symbol={pair} />
      <ChartPanel symbol={pair} />
      <div className="flex flex-col min-h-0 col-start-3 row-start-1 row-end-3">
        <OrderForm />
        <PositionsPanel />
      </div>
      <OrdersPanel symbol={pair} />
    </div>
  )
}
