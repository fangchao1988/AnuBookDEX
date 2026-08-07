import { useSettings } from '../../stores/settings'
import { useWallet } from '../../stores/wallet'

// 持仓列表：对齐原型 #positions-panel（现货卡片 + 杠杆卡片 + 风险条）
// P3：钱包连接后展示链上余额（requestRecords 聚合），未连接展示演示数据
export function PositionsPanel() {
  const tradingMode = useSettings((s) => s.tradingMode)
  const isPerp = tradingMode === 'perp'
  const walletAddress = useWallet((s) => s.address)
  const balances = useWallet((s) => s.balances)

  return (
    <div className="bg-bg-secondary border-r border-line flex-1 overflow-y-auto min-h-0">
      <div className="px-3 py-2 text-[11px] font-semibold text-text-secondary uppercase tracking-wide border-b border-line flex items-center gap-1.5">
        持仓
        <span className="text-[10px] text-text-muted ml-auto">({isPerp ? 1 : 3})</span>
      </div>
      {walletAddress && balances && (
        <div className="px-2.5 py-2 border-b border-line bg-bg-tertiary/50">
          <div className="text-[10px] text-text-muted mb-1">钱包余额（链上真实数据）</div>
          <div className="flex justify-between text-[11px] mb-0.5">
            <span className="text-text-muted">ALEO</span>
            <span className="font-mono">{balances.aleo}</span>
          </div>
          <div className="flex justify-between text-[11px] mb-0.5">
            <span className="text-text-muted">USDT</span>
            <span className="font-mono">{balances.usdt}</span>
          </div>
          <div className="flex justify-between text-[11px]">
            <span className="text-text-muted">ETH</span>
            <span className="font-mono">{balances.base}</span>
          </div>
        </div>
      )}
      {/* 钱包已连接：只显示真实数据（无演示卡片） */}
      {walletAddress && !balances && (
        <div className="p-5 text-center text-text-muted text-[13px]">余额加载中…</div>
      )}
      {!walletAddress && !isPerp && (
        <>
          <SpotCard pair="BTC/USDT" pnl="+543.40" amt="0.52 BTC" avg="67,200.00" />
          <SpotCard pair="ETH/USDT" pnl="+128.00" amt="3.20 ETH" avg="3,520.00" />
        </>
      )}
      {!walletAddress && (
        <div className="p-2.5 border-b border-line cursor-pointer hover:bg-bg-hover border-l-2 border-l-orange">
          <div className="flex justify-between items-center mb-0.5">
            <span className="font-semibold">
              BTC/USDT <span className="text-[10px] px-1.5 py-px rounded bg-orange-bg text-orange">3x 多</span>
            </span>
            <span className="font-mono font-semibold text-down">-555.00</span>
          </div>
          <div className="flex justify-between text-[11px] text-text-muted mb-0.5">
            <span>数量</span>
            <span className="font-mono">1.00 BTC</span>
          </div>
          <div className="flex justify-between text-[11px] text-text-muted mb-0.5">
            <span>开仓 / 标记</span>
            <span className="font-mono">68,800 / 68,245</span>
          </div>
          <div className="h-1 bg-bg-tertiary rounded-sm mt-1">
            <div className="h-full bg-orange rounded-sm" style={{ width: '62%' }} />
          </div>
          <div className="text-[10px] text-orange mt-0.5">强平: 60,500.00 (84%风险线)</div>
        </div>
      )}
      {walletAddress && isPerp && <div className="p-5 text-center text-text-muted text-[13px]">暂无持仓</div>}
    </div>
  )
}

function SpotCard({ pair, pnl, amt, avg }: { pair: string; pnl: string; amt: string; avg: string }) {
  return (
    <div className="p-2.5 border-b border-line cursor-pointer hover:bg-bg-hover">
      <div className="flex justify-between items-center mb-0.5">
        <span className="font-semibold">{pair}</span>
        <span className="font-mono font-semibold text-up">{pnl}</span>
      </div>
      <div className="flex justify-between text-[11px] text-text-muted mb-0.5">
        <span>数量</span>
        <span className="font-mono">{amt}</span>
      </div>
      <div className="flex justify-between text-[11px] text-text-muted">
        <span>均价</span>
        <span className="font-mono">{avg}</span>
      </div>
    </div>
  )
}
