import { useEffect, useState } from 'react'
import { useWallet } from '../stores/wallet'
import { useMarket } from '../stores/marketStore'
import { useMarketChannels } from '../hooks/useMarketChannels'
import { formatNumber, dateTimeStr } from '../lib/format'
import { fetchTrades, type TradeRecord } from '../lib/api/orders'

// ALEO 估值单价（USDT）：链上无 ALEO 行情源，暂用常量估算（标注估）
const ALEO_PRICE = 0.5

// 资产总览：真实钱包数据（链上余额 + 行情估值）
export default function AssetsPage() {
  const walletAddress = useWallet((s) => s.address)
  const balances = useWallet((s) => s.balances)

  // ETH 实时价（引擎行情）
  useMarketChannels('ETH/USDT')
  const ticker = useMarket((s) => s.getTicker('ETH_USDT'))
  const ethPrice = ticker ? Number(ticker.close) : 0

  const num = (v: string | undefined) => {
    if (!v || v === '--') return 0
    return Number(v.replace(/,/g, '')) || 0
  }
  const usdt = num(balances?.usdt)
  const eth = num(balances?.base)
  const aleo = num(balances?.aleo)
  const totalUsd = usdt + eth * ethPrice + aleo * ALEO_PRICE

  // 我的成交记录（引擎 /api/v1/trades，按钱包身份过滤）
  const [trades, setTrades] = useState<TradeRecord[]>([])
  useEffect(() => {
    if (!walletAddress) return
    let alive = true
    const load = async () => {
      try {
        const list = await fetchTrades({ trader: walletAddress, limit: 50 })
        if (alive) setTrades(list)
      } catch {
        // 成交加载失败不阻断
      }
    }
    void load()
    const timer = window.setInterval(load, 5000)
    return () => {
      alive = false
      window.clearInterval(timer)
    }
  }, [walletAddress])

  return (
    <div className="p-5 overflow-y-auto h-full">
      <h2 className="text-xl font-semibold mb-5">资产总览</h2>

      {!walletAddress ? (
        <div className="bg-bg-secondary border border-line rounded-lg p-10 text-center">
          <div className="text-[15px] text-text-secondary mb-2">连接钱包查看链上真实资产</div>
          <div className="text-xs text-text-muted">余额、估值均来自 Aleo 链上 record 与实时行情</div>
        </div>
      ) : (
        <>
          <div className="grid gap-3 grid-cols-[repeat(auto-fill,minmax(240px,1fr))]">
            <div className="bg-bg-secondary border border-line rounded-lg p-4">
              <div className="text-[11px] text-text-muted uppercase tracking-wide mb-1.5">总资产估值</div>
              <div className="text-[22px] font-bold text-up">$ {formatNumber(totalUsd, 2)}</div>
              <div className="text-[11px] text-text-secondary mt-1">
                ≈ {formatNumber(aleo, 2)} ALEO
              </div>
            </div>
            <div className="bg-bg-secondary border border-line rounded-lg p-4">
              <div className="text-[11px] text-text-muted uppercase tracking-wide mb-1.5">USDT 余额</div>
              <div className="text-[22px] font-bold">{formatNumber(usdt, 2)}</div>
              <div className="text-[11px] text-text-secondary mt-1">链上 Token record</div>
            </div>
            <div className="bg-bg-secondary border border-line rounded-lg p-4">
              <div className="text-[11px] text-text-muted uppercase tracking-wide mb-1.5">ETH 余额</div>
              <div className="text-[22px] font-bold">{formatNumber(eth, 4)}</div>
              <div className="text-[11px] text-text-secondary mt-1">
                折合 $ {formatNumber(eth * ethPrice, 2)} {ethPrice > 0 ? `· ETH 价 ${formatNumber(ethPrice, 2)}` : ''}
              </div>
            </div>
            <div className="bg-bg-secondary border border-line rounded-lg p-4">
              <div className="text-[11px] text-text-muted uppercase tracking-wide mb-1.5">ALEO 余额</div>
              <div className="text-[22px] font-bold">{formatNumber(aleo, 2)}</div>
              <div className="text-[11px] text-text-secondary mt-1">
                折合 $ {formatNumber(aleo * ALEO_PRICE, 2)} <span className="text-text-muted">(估)</span>
              </div>
            </div>
          </div>

          <h3 className="text-[15px] text-text-secondary mt-6 mb-3 border-b border-line pb-2">资产明细（链上）</h3>
          <div className="bg-bg-secondary border border-line rounded-lg overflow-hidden">
            <table className="w-full border-collapse text-[13px]">
              <thead>
                <tr>
                  {['币种', '余额', '折合 USDT', '来源'].map((h) => (
                    <th key={h} className="bg-bg-tertiary text-text-muted font-normal px-4 py-2 text-left text-[11px]">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                <tr className="hover:bg-bg-hover">
                  <td className="px-4 py-2 border-t border-line font-semibold">USDT</td>
                  <td className="px-4 py-2 border-t border-line font-mono">{formatNumber(usdt, 2)}</td>
                  <td className="px-4 py-2 border-t border-line font-mono">$ {formatNumber(usdt, 2)}</td>
                  <td className="px-4 py-2 border-t border-line text-[11px] text-text-muted">anubook_dex_p2 Token record</td>
                </tr>
                <tr className="hover:bg-bg-hover">
                  <td className="px-4 py-2 border-t border-line font-semibold">ETH</td>
                  <td className="px-4 py-2 border-t border-line font-mono">{formatNumber(eth, 4)}</td>
                  <td className="px-4 py-2 border-t border-line font-mono">$ {formatNumber(eth * ethPrice, 2)}</td>
                  <td className="px-4 py-2 border-t border-line text-[11px] text-text-muted">anubook_dex_p2 Token record</td>
                </tr>
                <tr className="hover:bg-bg-hover">
                  <td className="px-4 py-2 border-t border-line font-semibold">ALEO</td>
                  <td className="px-4 py-2 border-t border-line font-mono">{formatNumber(aleo, 2)}</td>
                  <td className="px-4 py-2 border-t border-line font-mono">$ {formatNumber(aleo * ALEO_PRICE, 2)}</td>
                  <td className="px-4 py-2 border-t border-line text-[11px] text-text-muted">credits.aleo（公开 + shielded）</td>
                </tr>
              </tbody>
            </table>
          </div>
          <div className="mt-3 text-xs text-text-muted">
            地址: <span className="font-mono text-text-secondary">{walletAddress}</span> · 余额来自链上 record 实时聚合，ETH 估值用实时行情价，ALEO 估值用常量价（估）
          </div>

          <h3 className="text-[15px] text-text-secondary mt-6 mb-3 border-b border-line pb-2">成交记录</h3>
          <div className="bg-bg-secondary border border-line rounded-lg overflow-hidden">
            <table className="w-full border-collapse text-[13px]">
              <thead>
                <tr>
                  {['时间', '交易对', '方向', '价格', '数量', '对手方'].map((h) => (
                    <th key={h} className="bg-bg-tertiary text-text-muted font-normal px-4 py-2 text-left text-[11px]">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {trades.length === 0 ? (
                  <tr>
                    <td colSpan={6} className="px-4 py-6 text-center text-text-muted text-[13px]">
                      暂无成交记录
                    </td>
                  </tr>
                ) : (
                  trades.map((t) => (
                    <tr key={`${t.order_id}-${t.ts}`} className="hover:bg-bg-hover">
                      <td className="px-4 py-2 border-t border-line text-text-muted">{dateTimeStr(t.ts)}</td>
                      <td className="px-4 py-2 border-t border-line">{t.symbol.replace('_', '/')}</td>
                      <td className={`px-4 py-2 border-t border-line ${t.side === 'buy' ? 'text-up' : 'text-down'}`}>
                        {t.side === 'buy' ? '买入' : '卖出'}
                      </td>
                      <td className="px-4 py-2 border-t border-line font-mono">{t.price}</td>
                      <td className="px-4 py-2 border-t border-line font-mono">{t.amount}</td>
                      <td className="px-4 py-2 border-t border-line font-mono text-text-muted">{t.taker.slice(0, 10)}…</td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </>
      )}
    </div>
  )
}
