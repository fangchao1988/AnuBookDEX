import { StatCard } from '../components/ui/StatCard'

// 流动性挖矿：对齐原型 #lp-view
export default function LpPage() {
  return (
    <div className="p-5 overflow-y-auto h-full">
      <h2 className="text-xl font-semibold mb-5">流动性挖矿</h2>
      <div className="grid gap-3 grid-cols-[repeat(auto-fill,minmax(240px,1fr))]">
        <StatCard title="LP 总价值" value="$ 9,000.00" sub="BTC/USDT 池" valueClassName="text-blue" />
        <StatCard title="累计手续费分红" value="+$ 320.50" sub="APR 12.8%" valueClassName="text-up" />
        <StatCard title="生态代币奖励" value="+1,280 ALEO" sub="≈ $49.92" valueClassName="text-purple" />
      </div>

      <h3 className="text-[15px] text-text-secondary mt-6 mb-3 border-b border-line pb-2">流动性池</h3>

      <PoolCard
        pair="BTC / USDT"
        tvl="TVL: $12,450,000 · 24h成交量: $3.2M"
        share="你的份额: 0.072%"
        apr="12.8%"
        progress={72}
        note="AI 建议做市配比: BTC 0.066 | USDT 4,500 · 预计优化收益率 +2.3%"
      />
      <div className="bg-bg-secondary border border-line rounded-lg p-5 flex justify-between items-center opacity-60">
        <div>
          <div className="text-base font-bold">ETH / USDT</div>
          <div className="text-xs text-text-muted mt-0.5">TVL: $8,900,000 · 24h成交量: $1.8M</div>
          <div className="mt-2 text-xs">未加入</div>
        </div>
        <div className="text-right">
          <div className="text-2xl font-bold text-up">9.5%</div>
          <div className="text-[11px] text-text-muted">年化收益率</div>
          <button className="bg-blue text-white border-none px-6 py-2.5 rounded-md font-semibold text-sm cursor-pointer hover:opacity-90 mt-2">
            添加流动性
          </button>
        </div>
      </div>

      <h3 className="text-[15px] text-text-secondary mt-6 mb-3 border-b border-line pb-2">RocketSwap 流动性互通</h3>
      <div className="bg-bg-secondary border border-line rounded-lg p-4">
        <div className="flex justify-between items-center">
          <div>
            <div className="font-bold">AleoBook 订单簿 ↔ RocketSwap AMM</div>
            <div className="text-xs text-text-muted mt-1">AI路由统一调度 · 全域资金利用率提升 18% · 双向流动性互通</div>
          </div>
          <span className="text-[11px] px-2 py-0.5 rounded bg-up-bg text-up font-semibold">已连接</span>
        </div>
        <div className="mt-3 flex gap-2 text-[11px] text-text-muted">
          <span>路由状态: 活跃</span>·<span>最优路径延迟: 3.2ms</span>·<span>今日路由交易: 1,234笔</span>
        </div>
      </div>
    </div>
  )
}

function PoolCard({
  pair,
  tvl,
  share,
  apr,
  progress,
  note,
}: {
  pair: string
  tvl: string
  share: string
  apr: string
  progress: number
  note: string
}) {
  return (
    <div className="bg-bg-secondary border border-line rounded-lg p-5 flex justify-between items-center mb-3">
      <div className="flex-1 pr-4">
        <div className="text-base font-bold">{pair}</div>
        <div className="text-xs text-text-muted mt-0.5">{tvl}</div>
        <div className="mt-2 text-xs">
          {share}
        </div>
        <div className="h-2 bg-bg-tertiary rounded mt-2">
          <div className="h-full bg-blue rounded" style={{ width: `${progress}%` }} />
        </div>
        <div className="text-[11px] text-text-muted mt-1.5">{note}</div>
      </div>
      <div className="text-right shrink-0">
        <div className="text-2xl font-bold text-up">{apr}</div>
        <div className="text-[11px] text-text-muted">年化收益率</div>
        <div className="mt-2 flex gap-1.5 justify-end">
          <button className="bg-blue text-white border-none px-6 py-2.5 rounded-md font-semibold text-sm cursor-pointer hover:opacity-90">
            添加流动性
          </button>
          <button className="bg-down text-white border-none px-6 py-2.5 rounded-md font-semibold text-sm cursor-pointer hover:opacity-90">
            移除
          </button>
        </div>
      </div>
    </div>
  )
}
