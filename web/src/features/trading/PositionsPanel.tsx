import { useState } from 'react'
import { useSettings } from '../../stores/settings'
import { useWallet } from '../../stores/wallet'
import { toChannelSymbol } from '../../lib/symbol'
import { pairMode } from '../../lib/tokens'

// 持仓列表：对齐原型 #positions-panel（现货卡片 + 杠杆卡片 + 风险条）
// P3：钱包连接后展示链上余额（requestRecords 聚合），未连接展示演示数据
export function PositionsPanel() {
  const tradingMode = useSettings((s) => s.tradingMode)
  const isPerp = tradingMode === 'perp'
  // p4 真实币对（ALEO/USDCX）：余额只显示 ALEO 与 USDCX，不显示 p2 铸币（ETH/USDT）
  const pair = useSettings((s) => s.pair)
  const isP4 = pairMode(toChannelSymbol(pair)) === 'p4-real'
  const walletAddress = useWallet((s) => s.address)
  const balances = useWallet((s) => s.balances)
  const walletMint = useWallet((s) => s.mintToken)
  const walletDeploy = useWallet((s) => s.deployProgram)
  const walletGetCredentials = useWallet((s) => s.getCredentials)
  const [mintBusy, setMintBusy] = useState(false)
  const [mintMsg, setMintMsg] = useState('')
  const [mintAmount, setMintAmount] = useState('100000')
  const [credsBusy, setCredsBusy] = useState(false)
  const [credsMsg, setCredsMsg] = useState('')

  // 领取 USDCX 合规凭证（get_credentials + freezelist 非包含证明）。
  // p5 隐私买单需要 Credentials record，无凭证报 "No record matching constraints"；
  // 领取后凭证归当前钱包地址，requestRecords 可见，即可正常下单
  const getCredentials = async () => {
    setCredsBusy(true)
    setCredsMsg('')
    try {
      await walletGetCredentials()
      setCredsMsg('凭证已领取（链上确认），可正常下单买入')
    } catch (e) {
      setCredsMsg(e instanceof Error ? `领取失败: ${e.message}` : '领取失败')
    } finally {
      setCredsBusy(false)
    }
  }

  // 部署合约：Shield executeDeployment（钱包 prover + ALEO 付部署费）
  const deploy = async () => {
    setMintBusy(true)
    setMintMsg('')
    try {
      const txId = await walletDeploy()
      setMintMsg(`部署交易已提交: ${txId.slice(0, 20)}…，等待链上确认`)
    } catch (e) {
      setMintMsg(e instanceof Error ? `部署失败: ${e.message}` : '部署失败')
    } finally {
      setMintBusy(false)
    }
  }

  // 铸测试币：金额可自定义（合约 MVP 锁仓断言 fund.amount == price*amount 严格相等，
  // 下单前需铸一枚恰好匹配锁仓量的币，如下单 1800×1 需铸 1800 USDT）
  const mint = async (tokenId: number) => {
    setMintBusy(true)
    setMintMsg('')
    try {
      const amt = Number(mintAmount.replace(/,/g, ''))
      if (!amt || amt <= 0) throw new Error('请输入有效金额')
      await walletMint(tokenId, amt)
      setMintMsg('铸币成功，等待链上确认后刷新余额')
    } catch (e) {
      setMintMsg(e instanceof Error ? `铸币失败: ${e.message}` : '铸币失败')
    } finally {
      setMintBusy(false)
    }
  }

  return (
    <div className="bg-bg-secondary border-r border-line flex-1 overflow-y-auto min-h-0">
      <div className="px-3 py-2 text-[11px] font-semibold text-text-secondary uppercase tracking-wide border-b border-line flex items-center gap-1.5">
        持仓
        <span className="text-[10px] text-text-muted ml-auto">({isPerp ? 1 : 3})</span>
      </div>
      {walletAddress && balances && (
        <div className="px-2.5 py-2 border-b border-line bg-bg-tertiary/50">
          <div className="text-[10px] text-text-muted mb-1">钱包余额</div>
          <div className="flex justify-between text-[11px] mb-0.5">
            <span className="text-text-muted">ALEO</span>
            <span className="font-mono">{balances.aleo}</span>
          </div>
          {isP4 ? (
            <>
              <div className="flex justify-between text-[11px]">
                <span className="text-text-muted">USDCX</span>
                <span className="font-mono">{balances.usdcx}</span>
              </div>
              {/* 隐私买单需要合规凭证（Credentials record）；无凭证下单选不到 record 报
                  "No record matching constraints"。领取一次凭证即可持续下单（凭证在
                  place_order 转账时重新铸回用户，不消耗） */}
              <button
                className="w-full mt-2 py-1 border border-cyan/40 rounded bg-cyan/5 text-cyan cursor-pointer text-[10px] hover:bg-cyan/10 disabled:opacity-50"
                disabled={credsBusy}
                onClick={() => void getCredentials()}
              >
                {credsBusy ? '领取中（链上确认约 30-60s）…' : '领取合规凭证（下单买入需凭证）'}
              </button>
              {credsMsg && <div className={`mt-1 text-[9px] ${credsMsg.startsWith('领取失败') ? 'text-down' : 'text-up'}`}>{credsMsg}</div>}
            </>
          ) : (
            <>
              <div className="flex justify-between text-[11px] mb-0.5">
                <span className="text-text-muted">USDT</span>
                <span className="font-mono">{balances.usdt}</span>
              </div>
              <div className="flex justify-between text-[11px]">
                <span className="text-text-muted">ETH</span>
                <span className="font-mono">{balances.base}</span>
              </div>
              <div className="mt-2 flex gap-1.5 items-center">
                <input
                  type="text"
                  value={mintAmount}
                  onChange={(e) => setMintAmount(e.target.value)}
                  className="w-20 bg-bg-tertiary border border-line text-text-primary px-1.5 py-1 rounded text-[10px] font-mono focus:border-blue focus:outline-none"
                  placeholder="金额"
                  title="铸币金额（下单锁仓需精确匹配 price×amount）"
                />
                <span className="text-[9px] text-text-muted">铸币金额（匹配下单锁仓）</span>
              </div>
              <div className="flex gap-1.5 mt-1.5">
                <button
                  className="flex-1 py-1 border border-line rounded bg-bg-tertiary text-text-secondary cursor-pointer text-[10px] hover:text-text-primary hover:border-blue disabled:opacity-50"
                  disabled={mintBusy}
                  onClick={() => void mint(2)}
                >
                  铸 USDT
                </button>
                <button
                  className="flex-1 py-1 border border-line rounded bg-bg-tertiary text-text-secondary cursor-pointer text-[10px] hover:text-text-primary hover:border-blue disabled:opacity-50"
                  disabled={mintBusy}
                  onClick={() => void mint(1)}
                >
                  铸 ETH 测试币
                </button>
                <button
                  className="flex-1 py-1 border border-purple/40 rounded bg-purple/5 text-purple cursor-pointer text-[10px] hover:bg-ai-glow disabled:opacity-50"
                  disabled={mintBusy}
                  onClick={() => void deploy()}
                >
                  部署合约
                </button>
              </div>
              {mintMsg && <div className="mt-1.5 text-[10px] text-text-muted break-all">{mintMsg}</div>}
            </>
          )}
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
