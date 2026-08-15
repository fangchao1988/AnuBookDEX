import { useEffect, useMemo, useState } from 'react'
import { genDepth, genTrades, LAST_PRICE, type TradeRow as MockTradeRow } from '../../mock/market'
import { useMarket, loadTradeHistory, type DepthRow } from '../../stores/marketStore'
import { useMarketChannels } from '../../hooks/useMarketChannels'
import { formatNumber, timeStr } from '../../lib/format'
import { toChannelSymbol } from '../../lib/symbol'
import { pairMode } from '../../lib/tokens'
import { useTrade } from '../../stores/trade'

interface RenderTrade {
  price: string
  qty: string
  timeText: string
  side: 'buy' | 'sell'
}

// 订单簿 / 最新成交：WS 实时数据优先，未连接时回退 mock（P1 行为）
export function OrderBook({ symbol }: { symbol: string }) {
  useMarketChannels(symbol)
  const [tab, setTab] = useState<'depth' | 'trades'>('depth')
  const setPrice = useTrade((s) => s.setPrice)

  // 刷新页面后回放最近成交历史（WS 实时继续累积）
  useEffect(() => {
    void loadTradeHistory(toChannelSymbol(symbol))
  }, [symbol])

  // store key 为频道名（BTC_USDT），与展示名（BTC/USDT）区分
  const channelSymbol = toChannelSymbol(symbol)
  // 价格精度：p4 真实币对（ALEO/USDCX）6 位小数；p2 铸币对保持 2 位
  // 注意 pairMode 表用下划线格式（ALEO_USDCX），传入须为 channelSymbol
  const priceDecimals = pairMode(channelSymbol) === 'p4-real' ? 6 : 2
  // 计价币种（表头：ALEO_USDCX -> USDCX，ETH_USDT -> USDT）
  const quoteToken = channelSymbol.split('_')[1] ?? 'USDT'
  const hasLive = useMarket((s) => s.hasDepth(channelSymbol))
  const liveBids = useMarket((s) => s.getDepthRows(channelSymbol, 'bids', 14))
  const liveAsks = useMarket((s) => s.getDepthRows(channelSymbol, 'asks', 14))
  const liveTrades = useMarket((s) => s.getTrades(channelSymbol, 25))
  const liveLast = useMarket((s) => s.getLastPrice(channelSymbol))

  // 无实时数据时用 mock 兜底（保持 UI 不空）；mock DepthRow -> store DepthRow 形状对齐。
  // asks 与实时数据一致：从高到低（mock 生成器为低到高，反转对齐）
  const mockBids = useMemo(
    () => genDepth(LAST_PRICE, 14, false).map((r) => ({ price: r.price, qty: r.amt, total: r.total, pct: r.pct })),
    [],
  )
  const mockAsks = useMemo(
    () =>
      genDepth(LAST_PRICE, 14, true)
        .map((r) => ({ price: r.price, qty: r.amt, total: r.total, pct: r.pct }))
        .reverse(),
    [],
  )
  const mockTrades = useMemo(() => genTrades(LAST_PRICE, 25), [])
  const mockLast = formatNumber(LAST_PRICE, 2)

  const bids = hasLive ? liveBids : mockBids
  const asks = hasLive ? liveAsks : mockAsks
  // 成交：实时数据时间戳转文本；mock 数据自带文本
  const trades: RenderTrade[] = hasLive
    ? liveTrades.map((t) => ({ price: t.price, qty: t.qty, timeText: timeStr(t.time), side: t.side }))
    : mockTrades.map((t: MockTradeRow) => ({ price: t.price, qty: t.amt, timeText: t.time, side: t.side }))
  const lastPrice = (liveLast ?? mockLast).replace(/,/g, '')

  const clickRow = (price: string) => setPrice(formatNumber(price, priceDecimals))

  return (
    <div className="bg-bg-secondary flex flex-col font-mono min-h-0 col-start-1 row-start-1 row-end-3 border-r border-line">
      {/* tabs */}
      <div className="flex border-b border-line shrink-0">
        <button
          className={`flex-1 py-[7px] border-none bg-transparent text-xs font-semibold cursor-pointer border-b-2 transition-colors ${
            tab === 'depth' ? 'text-text-primary border-b-blue' : 'text-text-muted border-b-transparent'
          }`}
          onClick={() => setTab('depth')}
        >
          订单簿
        </button>
        <button
          className={`flex-1 py-[7px] border-none bg-transparent text-xs font-semibold cursor-pointer border-b-2 transition-colors ${
            tab === 'trades' ? 'text-text-primary border-b-blue' : 'text-text-muted border-b-transparent'
          }`}
          onClick={() => setTab('trades')}
        >
          最新成交
        </button>
      </div>

      {tab === 'depth' ? (
        <>
          <div className="px-2.5 py-2 flex gap-2 border-b border-line items-center shrink-0">
            <span className="text-[11px] text-text-muted flex-1">价格 ({quoteToken})</span>
            <span className="text-[11px] text-text-muted flex-[0_0_72px] text-right">数量</span>
            <span className="text-[11px] text-text-muted flex-[0_0_72px] text-right">总额</span>
          </div>
          <div className="flex-1 overflow-y-auto">
            {asks.map((r) => (
              <DepthRow key={r.price} row={r} side="ask" decimals={priceDecimals} onClick={() => clickRow(r.price)} />
            ))}
          </div>
          <div className="py-1.5 px-2.5 text-xs font-semibold text-center border-y border-line bg-bg-tertiary flex items-center justify-center gap-1.5 text-text-primary">
            {formatNumber(lastPrice, priceDecimals)}
            <span className="text-[9px] text-cyan border border-cyan px-0.5 rounded-sm font-normal">ZK</span>
          </div>
          <div className="flex-1 overflow-y-auto">
            {bids.map((r) => (
              <DepthRow key={r.price} row={r} side="bid" decimals={priceDecimals} onClick={() => clickRow(r.price)} />
            ))}
          </div>
          <div className="py-1 px-2.5 text-[10px] text-text-muted border-t border-line flex justify-between shrink-0">
            <span>深度合并</span>
            <span>AI 拆分标记</span>
          </div>
        </>
      ) : (
        <>
          <div className="px-2.5 py-2 flex gap-2 border-b border-line items-center shrink-0">
            <span className="text-[11px] text-text-muted flex-1">价格 ({quoteToken})</span>
            <span className="text-[11px] text-text-muted flex-[0_0_72px] text-right">数量</span>
            <span className="text-[11px] text-text-muted flex-[0_0_72px] text-right">时间</span>
          </div>
          <div className="flex-1 overflow-y-auto text-xs">
            {trades.map((t, i) => (
              <div key={i} className="flex gap-2 px-2.5 py-px cursor-pointer hover:bg-bg-hover">
                <span className={`flex-1 font-semibold ${t.side === 'buy' ? 'text-up' : 'text-down'}`}>
                  {formatNumber(t.price, priceDecimals)}
                </span>
                <span className="flex-[0_0_72px] text-right">{t.qty}</span>
                <span className="flex-[0_0_72px] text-right text-text-muted">{t.timeText}</span>
              </div>
            ))}
          </div>
          <div className="py-1 px-2.5 text-[10px] text-text-muted border-t border-line flex justify-between shrink-0">
            <span>合计 {hasLive ? trades.length : 2456} 笔</span>
            <span>ZK 加密存证</span>
          </div>
        </>
      )}
    </div>
  )
}

function DepthRow({
  row,
  side,
  decimals,
  onClick,
}: {
  row: DepthRow
  side: 'ask' | 'bid'
  decimals: number
  onClick: () => void
}) {
  return (
    <div
      className={`relative flex gap-2 px-2.5 py-px text-xs cursor-pointer hover:bg-bg-hover ${side === 'ask' ? 'ask' : 'bid'}`}
      onClick={onClick}
    >
      <span
        className={`absolute right-0 inset-y-0 opacity-[0.08] pointer-events-none ${side === 'ask' ? 'bg-down' : 'bg-up'}`}
        style={{ width: `${row.pct}%` }}
      />
      <span className={`flex-1 relative z-10 ${side === 'ask' ? 'text-down' : 'text-up'}`}>{formatNumber(row.price, decimals)}</span>
      <span className="flex-[0_0_72px] text-right relative z-10">{row.qty}</span>
      <span className="flex-[0_0_72px] text-right relative z-10 text-text-muted">{row.total}</span>
    </div>
  )
}
