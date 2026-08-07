import { useCallback, useEffect, useState } from 'react'
import { useSettings } from '../../stores/settings'
import { useWallet } from '../../stores/wallet'
import { Tag } from '../../components/ui/Tag'
import { fetchOrders, fetchTrades, STATUS_LABEL, type OrderRecord, type TradeRecord } from '../../lib/api/orders'
import { toChannelSymbol } from '../../lib/symbol'
import { dateTimeStr } from '../../lib/format'

type BottomTab = 'open' | 'history' | 'trades' | 'funding' | 'ai'

// 底部面板：对齐原型 #bottombar（当前委托/历史委托/成交记录/资金费率/AI 日志）
export function OrdersPanel({ symbol }: { symbol: string }) {
  const tradingMode = useSettings((s) => s.tradingMode)
  const isPerp = tradingMode === 'perp'
  const walletAddress = useWallet((s) => s.address)
  const [tab, setTab] = useState<BottomTab>('open')
  const [orders, setOrders] = useState<OrderRecord[]>([])
  const [trades, setTrades] = useState<TradeRecord[]>([])
  const [ordersError, setOrdersError] = useState('')

  // P3 委托/成交真实数据：加载 + 5s 轮询（trader 未连接时查全部）
  const loadOrders = useCallback(async () => {
    try {
      const trader = walletAddress ?? localStorage.getItem('aleo_address') ?? undefined
      const [list, tradeList] = await Promise.all([
        fetchOrders({ trader, symbol: toChannelSymbol(symbol), limit: 50 }),
        fetchTrades({ trader, symbol: toChannelSymbol(symbol), limit: 50 }),
      ])
      setOrders(list)
      setTrades(tradeList)
      setOrdersError('')
    } catch {
      setOrdersError('委托/成交加载失败')
    }
  }, [walletAddress, symbol])

  useEffect(() => {
    void loadOrders()
    const timer = window.setInterval(loadOrders, 5000)
    return () => window.clearInterval(timer)
  }, [loadOrders])

  const openOrders = orders.filter((o) => o.status === 'waiting' || o.status === 'partial')
  const historyOrders = orders.filter((o) => o.status === 'filled')

  const tabs: { key: BottomTab; label: string; ai?: boolean; perpOnly?: boolean }[] = [
    { key: 'open', label: `当前委托 (${openOrders.length})` },
    { key: 'history', label: `历史委托 (${historyOrders.length})` },
    { key: 'trades', label: '成交记录' },
    { key: 'funding', label: '资金费率', perpOnly: true },
    { key: 'ai', label: 'AI 日志', ai: true },
  ]

  return (
    <div className="bg-bg-secondary min-h-0 flex flex-col col-start-2 row-start-2">
      <div className="flex border-b border-line shrink-0">
        {tabs
          .filter((t) => !t.perpOnly || isPerp)
          .map((t) => (
            <button
              key={t.key}
              className={`px-4 py-[7px] border-none bg-transparent text-xs cursor-pointer ${
                tab === t.key ? 'text-text-primary border-b-2 border-b-blue' : 'text-text-secondary'
              } ${t.ai ? 'text-purple' : ''}`}
              onClick={() => setTab(t.key)}
            >
              {t.label}
            </button>
          ))}
      </div>

      <div className="flex-1 overflow-y-auto">
        {tab === 'open' && (
          <OrderTable
            heads={['时间', '交易对', '方向', '类型', '价格', '数量', '已成交', '状态']}
            rows={openOrders.map((o) => ({
              cells: [
                dateTimeStr(o.create_at),
                o.symbol.replace('_', '/'),
                <span className={o.side === 'buy' ? 'text-up' : 'text-down'}>{o.side === 'buy' ? '买入' : '卖出'}</span>,
                o.type,
                o.price,
                o.amount,
                o.filled,
                <Tag variant={STATUS_LABEL[o.status].cls}>{STATUS_LABEL[o.status].text}</Tag>,
              ],
            }))}
          />
        )}
        {tab === 'history' && (
          <OrderTable
            heads={['时间', '交易对', '方向', '类型', '价格', '数量', '已成交', '状态']}
            rows={historyOrders.map((o) => ({
              cells: [
                dateTimeStr(o.create_at),
                o.symbol.replace('_', '/'),
                <span className={o.side === 'buy' ? 'text-up' : 'text-down'}>{o.side === 'buy' ? '买入' : '卖出'}</span>,
                o.type,
                o.price,
                o.amount,
                o.filled,
                <Tag variant={STATUS_LABEL[o.status].cls}>{STATUS_LABEL[o.status].text}</Tag>,
              ],
            }))}
          />
        )}
        {ordersError && (
          <div className="px-3 py-2 text-[11px] text-down">{ordersError}</div>
        )}
        {tab !== 'trades' && tab !== 'funding' && tab !== 'ai' && orders.length === 0 && !ordersError && (
          <div className="p-5 text-center text-text-muted text-[13px]">暂无委托</div>
        )}
        {tab === 'trades' && (
          <OrderTable
            heads={['时间', '交易对', '方向', '价格', '数量', '对手方']}
            rows={trades.map((t) => ({
              cells: [
                dateTimeStr(t.ts),
                t.symbol.replace('_', '/'),
                <span className={t.side === 'buy' ? 'text-up' : 'text-down'}>{t.side === 'buy' ? '买入' : '卖出'}</span>,
                t.price,
                t.amount,
                <span className="text-text-muted">{t.taker.slice(0, 10)}…</span>,
              ],
            }))}
          />
        )}
        {tab === 'funding' && isPerp && (
          <OrderTable
            heads={['时间', '交易对', '资金费率', '方向', '金额']}
            rows={[
              { cells: ['16:00', 'BTC/USDT', <span className="text-up">0.01%</span>, <span className="text-up">收取</span>, '12.50 USDT'] },
              { cells: ['08:00', 'BTC/USDT', <span className="text-up">0.01%</span>, <span className="text-up">收取</span>, '12.48 USDT'] },
            ]}
          />
        )}
        {tab === 'ai' && <AiLogTable />}
      </div>

      {tab !== 'ai' && (
        <div className="px-3 py-1 text-[10px] text-text-muted border-t border-line shrink-0">
          可链上溯源 · ZK 加密原始订单 · <a href="#" className="text-blue">批量撤销</a>
        </div>
      )}
    </div>
  )
}

function OrderTable({
  heads,
  rows,
}: {
  heads: string[]
  rows: { cells: (string | JSX.Element)[] }[]
}) {
  return (
    <table className="w-full border-collapse text-xs">
      <thead>
        <tr>
          {heads.map((h, i) => (
            <th
              key={h}
              className={`text-text-muted font-normal px-3 py-1 text-[11px] sticky top-0 bg-bg-secondary whitespace-nowrap ${
                i === 0 ? 'text-left' : 'text-right'
              }`}
            >
              {h}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((r, i) => (
          <tr key={i} className="hover:bg-bg-hover">
            {r.cells.map((c, j) => (
              <td
                key={j}
                className={`px-3 py-1 font-mono whitespace-nowrap ${j === 0 ? 'text-left text-text-muted font-sans' : 'text-right'}`}
              >
                {c}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}

// AI 自动操作日志（原型方案5）
const AI_LOGS: { cells: (string | JSX.Element)[] }[] = [
  { cells: ['14:32:15', 'BTC/USDT', <span className="text-orange text-[11px]">风险率超阈值</span>, <span className="font-bold text-down">92.4%</span>, '67,820.00', <span className="text-down">平多</span>, '0.1800', '0.8200 BTC', <Tag variant="purple">自动减仓</Tag>, <Tag variant="green">已执行</Tag>] },
  { cells: ['14:05:08', 'ETH/USDT', <span className="text-orange text-[11px]">接近强平线</span>, <span className="font-bold text-orange">88.1%</span>, '3,465.50', <span className="text-up">平空</span>, '1.2000', '2.0000 ETH', <Tag variant="purple">对冲减仓</Tag>, <Tag variant="green">已执行</Tag>] },
  { cells: ['13:40:22', 'BTC/USDT', <span className="text-orange text-[11px]">波动率激增</span>, <span className="font-bold text-orange">85.7%</span>, '68,910.00', <span className="text-down">平多</span>, '0.0500', '0.9500 BTC', <Tag variant="orange">预警减仓</Tag>, <Tag variant="green">已执行</Tag>] },
  { cells: ['11:22:40', 'SOL/USDT', <span className="text-orange text-[11px]">保证金不足</span>, <span className="font-bold text-down">91.0%</span>, '142.30', <span className="text-down">平多</span>, '12.0000', '88.0000 SOL', <Tag variant="purple">自动减仓</Tag>, <Tag variant="green">已执行</Tag>] },
  { cells: ['09:15:03', 'BTC/USDT', <span className="text-orange text-[11px]">资金费率异常</span>, <span className="font-bold">76.3%</span>, '67,540.00', <span className="text-text-muted">—</span>, '—', '1.0000 BTC', <Tag variant="blue">仅预警</Tag>, <Tag variant="orange">已通知</Tag>] },
]

function AiLogTable() {
  return (
    <>
      <table className="w-full border-collapse text-xs">
        <thead>
          <tr>
            {['时间', '交易对', '触发原因', '触发时风险率', '当时价格', '减仓方向', '减仓数量', '减仓后仓位', '操作类型', '状态'].map((h, i) => (
              <th
                key={h}
                className={`text-text-muted font-normal px-2.5 py-1 text-[11px] sticky top-0 bg-bg-secondary whitespace-nowrap ${
                  i === 0 ? 'text-left' : 'text-right'
                }`}
              >
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {AI_LOGS.map((r, i) => (
            <tr key={i} className="hover:bg-bg-hover">
              {r.cells.map((c, j) => (
                <td
                  key={j}
                  className={`px-2.5 py-1 font-mono whitespace-nowrap ${j === 0 ? 'text-left text-text-muted font-sans' : 'text-right'}`}
                >
                  {c}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      <div className="px-3 py-1.5 text-[10px] text-text-muted border-t border-line flex justify-between">
        <span>
          自动操作全程加密留痕 · 支持机构对账与审计回溯 · <a href="#" className="text-blue">导出日志</a> · <a href="#" className="text-blue">链上存证</a>
        </span>
        <span>共 5 条 · 近 24h</span>
      </div>
    </>
  )
}
