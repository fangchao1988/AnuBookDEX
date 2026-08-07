import { useMemo, useState } from 'react'
import { StatCard } from '../components/ui/StatCard'
import { Tag } from '../components/ui/Tag'
import { useMarket } from '../stores/marketStore'
import { useMarketChannels } from '../hooks/useMarketChannels'
import { formatUsd } from '../lib/format'

// 机构暗池撮合：对齐原型 #darkpool-view（实时撮合预估 = 原型方案2）
// 深度数据取实时订单簿（BTC_USDT），未连接时回退常量
const FALLBACK_DEPTH = { bid: 2400000, ask: 1800000 }

export default function DarkpoolPage() {
  const [amount, setAmount] = useState('100,000')
  const [slippage, setSlippage] = useState('0.01%')

  useMarketChannels('BTC/USDT')
  const depthBids = useMarket((s) => s.getDepthRows('BTC_USDT', 'bids', 100))
  const depthAsks = useMarket((s) => s.getDepthRows('BTC_USDT', 'asks', 100))
  const hasLive = useMarket((s) => s.hasDepth('BTC_USDT'))
  const lastPrice = useMarket((s) => s.getLastPrice('BTC_USDT'))

  // 实时深度总额（sum(qty * price)），无实时数据用常量
  const liveTotal = (rows: { price: string; qty: string }[]) =>
    rows.reduce((sum, r) => sum + Number(r.qty) * Number(r.price), 0)
  const depthBid = hasLive && depthBids.length > 0 ? liveTotal(depthBids) : FALLBACK_DEPTH.bid
  const depthAsk = hasLive && depthAsks.length > 0 ? liveTotal(depthAsks) : FALLBACK_DEPTH.ask

  const estimate = useMemo(() => {
    const raw = amount.replace(/[^0-9.]/g, '')
    const amt = parseFloat(raw) || 0
    const totalDepth = depthBid + depthAsk
    const prob = amt > 0 ? Math.max(15, Math.min(95, Math.round(95 - (amt / totalDepth) * 100))) : 0
    const durMin = amt > 0 ? Math.max(10, Math.round(amt / 5000)) : 0
    let hint: string | JSX.Element
    if (amt <= 0) hint = '请输入金额以预估撮合情况'
    else if (amt < 10000) hint = '⚠ 低于暗池最低限额 (10,000 USDT)，请使用普通下单'
    else if (amt >= 100000) {
      const slices = Math.min(10, Math.max(2, Math.ceil(amt / 35000)))
      hint = (
        <>
          💰 金额较大，建议 AI 冰山拆分为 <b>{slices}</b> 笔执行，降低冲击成本
        </>
      )
    } else hint = '✓ 金额适中，预计可一次性撮合，无需拆分'
    return {
      prob,
      duration: amt > 0 ? `${durMin} - ${durMin * 3} 秒` : '--',
      hint,
      totalDepth,
      mid: lastPrice ? Number(lastPrice) : 68245,
    }
  }, [amount, depthBid, depthAsk, lastPrice])

  return (
    <div className="p-5 overflow-y-auto h-full">
      <h2 className="text-xl font-semibold mb-5">机构暗池撮合</h2>
      <div className="flex gap-3 mb-5">
        <div className="flex-1">
          <StatCard title="MPC 网络状态" value="运行中" sub="零滑点撮合保障" valueClassName="text-cyan" />
        </div>
        <div className="flex-1">
          <StatCard title="今日暗池交易" value="8 笔" sub="总金额: $1,245,000" />
        </div>
        <div className="flex-1">
          <StatCard title="暗池服务费" value="$2,490" sub="固定费 + 超额佣金" valueClassName="text-orange" />
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3">
        {/* 提交暗池订单 */}
        <div className="bg-bg-secondary border border-line rounded-lg p-4">
          <h3 className="text-[15px] mb-3">提交暗池订单</h3>
          <label className="text-xs text-text-muted">交易方向</label>
          <div className="flex gap-2 my-2 mb-3">
            <button className="flex-1 py-2.5 bg-up text-white border-none rounded font-bold text-sm cursor-pointer hover:opacity-90">买入</button>
            <button className="flex-1 py-2.5 bg-bg-tertiary text-down border border-down rounded font-bold text-sm cursor-pointer hover:opacity-90">卖出</button>
          </div>
          <label className="text-xs text-text-muted">
            金额 (USDT) <span className="text-orange">≥ 10,000</span>
          </label>
          <input
            type="text"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            className="w-full bg-bg-tertiary border border-line text-text-primary px-2.5 py-2.5 rounded text-base font-mono my-1 mb-3 focus:border-blue focus:outline-none"
          />
          <label className="text-xs text-text-muted">滑点容忍度</label>
          <input
            type="text"
            value={slippage}
            onChange={(e) => setSlippage(e.target.value)}
            className="w-full bg-bg-tertiary border border-line text-text-primary px-2.5 py-2.5 rounded font-mono my-1 mb-3 focus:border-blue focus:outline-none"
          />

          {/* 实时撮合预估 */}
          <div className="p-3 bg-bg-tertiary border border-line rounded-md">
            <div className="text-xs font-semibold text-cyan mb-2.5">🛰 实时撮合预估</div>
            <div className="flex justify-between items-center my-1.5 text-xs">
              <span className="text-text-muted">撮合概率</span>
              <span className="font-mono font-semibold">{amount.replace(/[^0-9.]/g, '') ? `${estimate.prob}%` : '--'}</span>
            </div>
            <div className="h-1.5 bg-bg-secondary rounded overflow-hidden">
              <div
                className="h-full rounded bg-gradient-to-r from-down via-orange to-up transition-all duration-300"
                style={{ width: `${estimate.prob}%` }}
              />
            </div>
            <div className="flex justify-between my-1.5 text-xs mt-2">
              <span className="text-text-muted">预计撮合时长</span>
              <span className="font-mono font-semibold">{estimate.duration}</span>
            </div>
            <div className="flex justify-between my-1.5 text-xs">
              <span className="text-text-muted">对手单深度</span>
              <span className="font-mono font-semibold">{formatUsd(estimate.totalDepth / 1000000, 2)}M</span>
            </div>
            <div className="flex gap-2 mt-2.5">
              <div className="flex-1 px-2 py-1.5 rounded bg-up-bg text-[11px]">
                <span className="text-[10px] text-text-muted block">买方深度</span>
                <span className="font-bold font-mono">{formatUsd(depthBid / 1000000, 2)}M</span>
              </div>
              <div className="flex-1 px-2 py-1.5 rounded bg-down-bg text-[11px]">
                <span className="text-[10px] text-text-muted block">卖方深度</span>
                <span className="font-bold font-mono">{formatUsd(depthAsk / 1000000, 2)}M</span>
              </div>
            </div>
            <div className="text-[10px] text-text-muted mt-2 pt-2 border-t border-line">{estimate.hint}</div>
          </div>

          <div className="p-2.5 bg-down/5 rounded text-[11px] text-orange my-2">大额交易强制二次确认 · MPC加密撮合 · 不对外展示订单</div>
          <button className="w-full bg-purple text-white py-3.5 text-base border-none rounded font-bold cursor-pointer hover:opacity-90">
            提交暗池订单 (MPC加密)
          </button>
        </div>

        {/* 暗池交易记录 */}
        <div className="bg-bg-secondary border border-line rounded-lg p-4">
          <h3 className="text-[15px] mb-3">
            暗池交易记录 <span className="text-[11px] text-cyan">加密归档</span>
          </h3>
          <table className="w-full border-collapse text-xs">
            <thead>
              <tr>
                {['时间', '方向', '金额', '滑点', '费用'].map((h) => (
                  <th key={h} className="bg-bg-tertiary text-text-muted font-normal px-3 py-1.5 text-left text-[11px]">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {[
                ['14:00', <span className="text-up">买入</span>, '$200,000', '0.005%', '$400'],
                ['11:30', <span className="text-down">卖出</span>, '$150,000', '0.008%', '$300'],
                ['09:15', <span className="text-up">买入</span>, '$500,000', '0.003%', '$1,000'],
              ].map((r, i) => (
                <tr key={i} className="hover:bg-bg-hover">
                  {r.map((c, j) => (
                    <td key={j} className="px-3 py-1.5 border-t border-line font-mono text-right" style={j === 0 ? { textAlign: 'left', fontFamily: 'inherit', color: '#5e6673' } : undefined}>
                      {c}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
          <div className="mt-3 text-[11px] text-text-muted">支持机构合规对账 · 加密审计接口 · ZK分级隐私</div>
        </div>
      </div>

      {/* ZK-KYC */}
      <div className="bg-bg-secondary border border-line rounded-lg p-4 mt-3">
        <h3 className="text-[15px] mb-2">ZK-KYC 分级合规状态</h3>
        <div className="flex justify-between items-center py-3 border-b border-line">
          <div>
            <b>小额交易</b> (&lt; 10,000 USDT)
            <br />
            <span className="text-[11px] text-text-muted">完全匿名，无需KYC</span>
          </div>
          <Tag variant="green">活跃</Tag>
        </div>
        <div className="flex justify-between items-center py-3 border-b border-line">
          <div>
            <b>大额交易</b> (≥ 10,000 USDT)
            <br />
            <span className="text-[11px] text-text-muted">触发ZK-KYC引导，自主选择披露范围</span>
          </div>
          <Tag variant="orange">待触发</Tag>
        </div>
        <div className="text-[11px] text-text-muted mt-2">
          符合AML/GDPR · ALEO官方审计接口已预留 · 交易行为可追溯但不泄露策略与原始订单
        </div>
      </div>
    </div>
  )
}
