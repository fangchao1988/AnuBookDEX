import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ColorType, createChart, LineStyle, type IChartApi, type ISeriesApi, type UTCTimestamp } from 'lightweight-charts'
import { AI_LEVELS, MOCK_CANDLES, MA_RANGES, calcMA } from '../../mock/market'
import { useMarket, type Candle, type DepthRow } from '../../stores/marketStore'
import { useMarketChannels } from '../../hooks/useMarketChannels'
import { useSettings } from '../../stores/settings'
import { toChannelSymbol } from '../../lib/symbol'
import { formatNumber } from '../../lib/format'

// 前端周期 -> 后端 interval（后端 l2quote 无 3min，按 KLINETYPES 对齐）
export const INTERVALS: { label: string; backend: string }[] = [
  { label: '1m', backend: '1min' },
  { label: '5m', backend: '5min' },
  { label: '15m', backend: '15min' },
  { label: '30m', backend: '30min' },
  { label: '1H', backend: '60min' },
  { label: '4H', backend: '4hour' },
  { label: '1D', backend: '1day' },
  { label: '1W', backend: '1week' },
  { label: '1M', backend: '1mon' },
]

const MA_COLORS: Record<string, string> = { MA5: '#f0b90b', MA10: '#1e80ff', MA30: '#a371f7' }

// 中央面板：对齐原型 #center-panel（AI 风控条 + 图表工具栏 + K线/深度图）
export function ChartPanel({ symbol }: { symbol: string }) {
  const navigate = useNavigate()
  const simpleMode = useSettings((s) => s.simpleMode)
  const [riskOpen, setRiskOpen] = useState(true)
  const [intervalIdx, setIntervalIdx] = useState(0)
  const [mode, setMode] = useState<'candle' | 'depth'>('candle')
  const [overlays, setOverlays] = useState<string[]>(['MA5', 'MA10'])

  const interval = INTERVALS[intervalIdx].backend
  const channelSymbol = toChannelSymbol(symbol)
  useMarketChannels(symbol, [interval])

  const toggleOverlay = (key: string) =>
    setOverlays((prev) => (prev.includes(key) ? prev.filter((k) => k !== key) : [...prev, key]))

  return (
    <div className={`flex flex-col bg-bg-primary min-h-0 col-start-2 row-start-1 ${simpleMode ? 'opacity-60 pointer-events-none' : ''}`}>
      {/* AI 风控条 */}
      <div className="px-2.5 py-1 text-[11px] bg-bg-tertiary flex gap-3 items-center border-b border-line shrink-0">
        <span className="text-text-muted cursor-pointer select-none" onClick={() => setRiskOpen((v) => !v)}>
          AI 风控 {riskOpen ? '▾' : '▸'}
        </span>
        {riskOpen && (
          <>
            <span className="flex items-center gap-1">
              <span className="w-1.5 h-1.5 rounded-full bg-up" /> 安全
            </span>
            <span className="flex items-center gap-1 ml-2">
              <span className="w-1.5 h-1.5 rounded-full bg-orange" /> BTC多头2: 82%
            </span>
          </>
        )}
        <span className="flex-1" />
        <span className="text-purple cursor-pointer" onClick={() => navigate('/strategy')}>
          详情 →
        </span>
      </div>

      {/* 图表工具栏 */}
      <div className="flex items-center px-3 py-1.5 gap-0.5 border-b border-line shrink-0">
        {INTERVALS.map((it, i) => (
          <button
            key={it.backend}
            className={`px-2 py-0.5 rounded text-xs border-none bg-transparent cursor-pointer ${
              intervalIdx === i ? 'text-text-primary font-semibold bg-bg-tertiary' : 'text-text-secondary'
            }`}
            onClick={() => setIntervalIdx(i)}
          >
            {it.label}
          </button>
        ))}
        <span className="flex-1" />
        <button
          className={`px-2 py-0.5 rounded text-xs border-none bg-transparent cursor-pointer ${
            mode === 'candle' ? 'text-text-primary font-semibold bg-bg-tertiary' : 'text-text-secondary'
          }`}
          onClick={() => setMode('candle')}
        >
          K线
        </button>
        <button
          className={`px-2 py-0.5 rounded text-xs border-none bg-transparent cursor-pointer ${
            mode === 'depth' ? 'text-text-primary font-semibold bg-bg-tertiary' : 'text-text-secondary'
          }`}
          onClick={() => setMode('depth')}
        >
          深度图
        </button>
        <span className="text-purple text-[11px] ml-1">AI 点位</span>
        <LiveBadge symbol={channelSymbol} />
      </div>

      {/* 图表区 */}
      <div className="flex-1 relative min-h-0">
        {mode === 'candle' ? (
          <CandleChart symbol={channelSymbol} interval={interval} overlays={overlays} />
        ) : (
          <DepthChart symbol={channelSymbol} />
        )}
        {/* 指标 overlay */}
        <div className="absolute bottom-2.5 left-2.5 flex gap-1.5 z-10">
          {['MA5', 'MA10', 'MA30', '布林带', 'AI 信号'].map((k) => (
            <button
              key={k}
              className={`px-2.5 py-0.5 rounded bg-bg-tertiary border text-[11px] cursor-pointer ${
                overlays.includes(k) ? 'text-text-primary border-blue' : 'text-text-secondary border-line'
              } ${k === 'AI 信号' ? 'text-purple border-purple' : ''}`}
              onClick={() => ['MA5', 'MA10', 'MA30'].includes(k) && toggleOverlay(k)}
            >
              {k}
            </button>
          ))}
        </div>
      </div>
    </div>
  )
}

// 连接状态标识：LIVE（绿）/ 离线（红）
function LiveBadge({ symbol }: { symbol: string }) {
  const status = useMarket((s) => s.status)
  const hasLive = useMarket((s) => s.hasDepth(symbol))
  if (!hasLive) return null
  return (
    <span className={`ml-1 text-[9px] px-1 py-px rounded font-semibold ${status === 'open' ? 'bg-up-bg text-up' : 'bg-down-bg text-down'}`}>
      {status === 'open' ? 'LIVE' : '离线'}
    </span>
  )
}

function toChartCandle(c: Candle) {
  return { time: c.time as UTCTimestamp, open: c.open, high: c.high, low: c.low, close: c.close }
}

// ============ K线图（Lightweight Charts，实时增量更新） ============
function CandleChart({
  symbol,
  interval,
  overlays,
}: {
  symbol: string
  interval: string
  overlays: string[]
}) {
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)
  const seriesRef = useRef<ISeriesApi<'Candlestick'> | null>(null)
  const lineRefs = useRef<Record<string, ISeriesApi<'Line'>>>({})
  // 已应用到图表的最后一个数据状态（避免重复 setData）。
  // key 必须含 symbol：切换交易对（interval 不变）时也要整体 setData，
  // 否则用 update 续传会因时间倒退抛错崩溃（lightweight-charts 要求时间递增）
  const appliedRef = useRef({ key: '', len: 0, lastTime: 0, lastClose: 0 })

  const liveCandles = useMarket((s) => s.getKlines(symbol, interval))
  const candles: Candle[] = liveCandles.length > 0 ? liveCandles : MOCK_CANDLES

  // 初始化图表（仅一次）
  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const chart = createChart(el, {
      layout: {
        background: { type: ColorType.Solid, color: 'transparent' },
        textColor: '#848e9c',
        fontSize: 11,
      },
      grid: {
        vertLines: { color: 'rgba(43,49,57,0.5)' },
        horzLines: { color: 'rgba(43,49,57,0.5)' },
      },
      rightPriceScale: { borderColor: '#2b3139' },
      timeScale: { borderColor: '#2b3139', timeVisible: true, secondsVisible: false },
    })
    chartRef.current = chart

    const candlesSeries = chart.addCandlestickSeries({
      upColor: '#0ecb81',
      downColor: '#f6465d',
      wickUpColor: '#0ecb81',
      wickDownColor: '#f6465d',
      borderVisible: false,
    })
    seriesRef.current = candlesSeries

    // AI 点位：支撑线 + 买入标记（P5 接后端 AI 引擎）
    candlesSeries.createPriceLine({
      price: AI_LEVELS.support,
      color: 'rgba(163,113,247,0.4)',
      lineWidth: 1,
      lineStyle: LineStyle.Dashed,
      axisLabelVisible: true,
      title: 'AI 支撑',
    })
    candlesSeries.setMarkers([
      { time: MOCK_CANDLES[AI_LEVELS.buyMarker.index].time, position: 'belowBar', color: '#a371f7', shape: 'arrowUp', text: AI_LEVELS.buyMarker.text },
    ])

    chart.timeScale().fitContent()

    const ro = new ResizeObserver(() => {
      const w = el.clientWidth
      const h = el.clientHeight
      if (w > 0 && h > 0) chart.resize(w, h)
    })
    ro.observe(el)

    return () => {
      ro.disconnect()
      chart.remove()
      chartRef.current = null
      seriesRef.current = null
      lineRefs.current = {}
    }
  }, [])

  // 数据同步：切换周期/交易对 -> setData；实时 tick -> update 最后一根
  useEffect(() => {
    const series = seriesRef.current
    if (!series) return
    const last = candles[candles.length - 1]
    const app = appliedRef.current
    const key = `${symbol}:${interval}`
    if (app.key !== key) {
      series.setData(candles.map(toChartCandle))
      chartRef.current?.timeScale().fitContent()
      app.key = key
      app.len = candles.length
      app.lastTime = last?.time ?? 0
      app.lastClose = last?.close ?? 0
    } else if (candles.length !== app.len || last?.time !== app.lastTime || last?.close !== app.lastClose) {
      if (last) series.update(toChartCandle(last))
      app.len = candles.length
      app.lastTime = last?.time ?? 0
      app.lastClose = last?.close ?? 0
    }
  }, [candles, interval, symbol])

  // MA 叠加线：数据或开关变化时重建
  useEffect(() => {
    const chart = chartRef.current
    if (!chart) return
    Object.values(lineRefs.current).forEach((s) => chart.removeSeries(s))
    lineRefs.current = {}
    for (const key of overlays) {
      const period = MA_RANGES[key]
      if (!period) continue
      const line = chart.addLineSeries({
        color: MA_COLORS[key],
        lineWidth: 1,
        priceLineVisible: false,
        lastValueVisible: false,
      })
      line.setData(calcMA(candles, period))
      lineRefs.current[key] = line
    }
  }, [candles, overlays])

  return <div ref={containerRef} className="absolute inset-0" />
}

// ============ 深度图（canvas 自绘，实时深度） ============
function DepthChart({ symbol }: { symbol: string }) {
  const ref = useRef<HTMLCanvasElement>(null)
  const liveBids = useMarket((s) => s.getDepthRows(symbol, 'bids', 100))
  const liveAsks = useMarket((s) => s.getDepthRows(symbol, 'asks', 100))
  const hasLive = useMarket((s) => s.hasDepth(symbol))

  useEffect(() => {
    const canvas = ref.current
    if (!canvas) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return

    const dpr = window.devicePixelRatio || 1
    const W = canvas.clientWidth
    const H = canvas.clientHeight
    canvas.width = W * dpr
    canvas.height = H * dpr
    ctx.scale(dpr, dpr)

    // 构造累积深度曲线：无实时数据时用合成曲线兜底。
    // 曲线按价格升序累积（x 从左到右），与订单簿显示顺序（asks 高到低）解耦
    const mkCurve = (rows: DepthRow[]): [number, number][] => {
      let cum = 0
      return [...rows]
        .sort((a, b) => +a.price - +b.price)
        .map((r) => {
          cum += Number(r.qty)
          return [+r.price, cum]
        })
    }
    let bidPts = hasLive ? mkCurve(liveBids) : synthCurve(68245, 40, false)
    let askPts = hasLive ? mkCurve(liveAsks) : synthCurve(68245, 40, true)
    // 空数据保护
    if (bidPts.length === 0) bidPts = synthCurve(68245, 40, false)
    if (askPts.length === 0) askPts = synthCurve(68245, 40, true)

    const allPts = [...bidPts, ...askPts]
    const priceMin = Math.min(...allPts.map((p) => p[0]))
    const priceMax = Math.max(...allPts.map((p) => p[0]))
    const maxTotal = Math.max(...allPts.map((p) => p[1])) * 1.05
    const x = (price: number) => ((price - priceMin) / (priceMax - priceMin)) * W
    const y = (total: number) => H - (total / maxTotal) * (H - 30)

    const drawSide = (pts: [number, number][], color: string) => {
      ctx.beginPath()
      ctx.moveTo(x(pts[0][0]), y(pts[0][1]))
      for (const [p, t] of pts) ctx.lineTo(x(p), y(t))
      ctx.strokeStyle = color
      ctx.lineWidth = 1.5
      ctx.stroke()
      const grad = ctx.createLinearGradient(0, 0, 0, H)
      grad.addColorStop(0, color + '33')
      grad.addColorStop(1, color + '00')
      ctx.lineTo(x(pts[pts.length - 1][0]), H)
      ctx.lineTo(x(pts[0][0]), H)
      ctx.closePath()
      ctx.fillStyle = grad
      ctx.fill()
    }

    ctx.clearRect(0, 0, W, H)
    drawSide(bidPts, '#0ecb81')
    drawSide(askPts, '#f6465d')

    // 中间价线
    const midPrice = hasLive && bidPts.length > 0 ? bidPts[bidPts.length - 1][0] : 68245
    ctx.beginPath()
    ctx.moveTo(x(midPrice), 0)
    ctx.lineTo(x(midPrice), H)
    ctx.strokeStyle = 'rgba(234,236,239,0.3)'
    ctx.lineWidth = 1
    ctx.setLineDash([4, 4])
    ctx.stroke()
    ctx.setLineDash([])
    ctx.fillStyle = '#848e9c'
    ctx.font = '10px "SF Mono", monospace'
    ctx.fillText(formatNumber(midPrice, 2), x(midPrice) + 6, 12)
    ctx.fillText('买方深度', 10, H - 10)
    ctx.fillText('卖方深度', W - 70, H - 10)
  }, [liveBids, liveAsks, hasLive, symbol])

  return <canvas ref={ref} className="w-full h-full" />
}

// 合成深度曲线（无实时数据兜底）
function synthCurve(mid: number, steps: number, isAsk: boolean): [number, number][] {
  const pts: [number, number][] = []
  let total = 0
  for (let i = 0; i < steps; i++) {
    const amt = 0.05 + (i / steps) * 3.5 * 0.2
    total += amt
    pts.push([mid + (isAsk ? 1 : -1) * i * 12, total])
  }
  return pts
}
