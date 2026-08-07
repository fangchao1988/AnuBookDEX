import { useMemo, useState } from 'react'
import { useUi } from '../../stores/ui'
import { useSettings } from '../../stores/settings'
import { Modal, SettingRow } from '../../components/ui/Modal'
import { Toggle } from '../../components/ui/Toggle'
import { AiRiskBound } from '../../features/ai/AiRiskBound'
import { toDecimal } from '../../lib/decimal'
import { formatNumber } from '../../lib/format'

// 模态框宿主：AI 策略设置 / 全局设置 / 公告 / 杠杆调整
export function ModalHost() {
  const modal = useUi((s) => s.modal)
  const close = useUi((s) => s.closeModal)
  return (
    <>
      <AiModal open={modal === 'ai'} onClose={close} />
      <SettingsModal open={modal === 'settings'} onClose={close} />
      <AnnounceModal open={modal === 'announce'} onClose={close} />
      <LeverageModal open={modal === 'leverage'} onClose={close} />
    </>
  )
}

// ============ AI 策略设置 ============
function AiModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [autoReduce, setAutoReduce] = useState(false)
  const [aiOn, setAiOn] = useState(true)
  const [pointOn, setPointOn] = useState(true)
  const [icebergOn, setIcebergOn] = useState(false)
  const [routeOn, setRouteOn] = useState(true)
  return (
    <Modal open={open} onClose={onClose} title="AI 智能策略引擎设置">
      <SettingRow
        title={<b>AI 行情研判</b>}
        desc={<>实时分析盘口、资金流向、舆情</>}
        control={<Toggle checked={aiOn} onChange={setAiOn} />}
      />
      <SettingRow
        title={<b>AI 点位推荐</b>}
        desc={<>自动计算买卖/止盈止损位</>}
        control={<Toggle checked={pointOn} onChange={setPointOn} />}
      />
      <SettingRow
        title={<b>AI 冰山拆单</b>}
        desc={<>大额订单自动拆分，防MEV狙击</>}
        control={<Toggle checked={icebergOn} onChange={setIcebergOn} />}
      />
      <SettingRow
        title={
          <b>
            AI 自动减仓 <span className="text-[11px] text-orange">· 默认关闭，需手动启用</span>
          </b>
        }
        desc={<>风险率超阈值时自动减仓</>}
        control={<Toggle checked={autoReduce} onChange={setAutoReduce} />}
      />
      <AiRiskBound enabled={autoReduce} />
      <SettingRow
        title={<b>AI 智能路由</b>}
        desc={<>AleoBook + RocketSwap 最优路径</>}
        control={<Toggle checked={routeOn} onChange={setRouteOn} />}
      />
      <SettingRow
        title={<b>策略隐私托管</b>}
        desc={<>策略离线运行，不上链不泄露</>}
        control={
          <button className="bg-blue text-white border-none px-3 py-1.5 rounded text-[11px] font-semibold cursor-pointer hover:opacity-90">
            上传策略
          </button>
        }
      />
    </Modal>
  )
}

// ============ 全局设置 ============
function SettingsModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [globalPrivacy, setGlobalPrivacy] = useState(true)
  const [sessionTimeout, setSessionTimeout] = useState(true)
  const [nftDetect, setNftDetect] = useState(true)
  return (
    <Modal open={open} onClose={onClose} title="设置">
      <SettingRow
        title={<b>全局隐私模式</b>}
        desc={<>隐藏地址、订单、持仓</>}
        control={<Toggle checked={globalPrivacy} onChange={setGlobalPrivacy} />}
      />
      <SettingRow
        title={<b>多钱包管理</b>}
        desc={<>连接多个ALEO生态钱包</>}
        control={
          <button className="bg-transparent border border-line text-text-secondary px-3 py-1.5 rounded text-[11px] cursor-pointer hover:text-text-primary">
            添加钱包
          </button>
        }
      />
      <SettingRow
        title={<b>会话超时自动登出</b>}
        desc={<>30分钟无操作自动断开</>}
        control={<Toggle checked={sessionTimeout} onChange={setSessionTimeout} />}
      />
      <SettingRow
        title={<b>NFT权益识别</b>}
        desc={<>自动识别ALEO生态NFT</>}
        control={<Toggle checked={nftDetect} onChange={setNftDetect} />}
      />
      <div className="mt-3 p-2.5 bg-purple/5 rounded border border-purple/20 text-xs">
        当前: <b className="text-purple">ALEO OG NFT</b> · 手续费-20% · AI折扣 · 杠杆利率优惠
      </div>
    </Modal>
  )
}

// ============ 公告 ============
function AnnounceModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const items = [
    { icon: '📢', title: 'V2.0 主网上线', desc: '全量AI功能、暗池、NFT权益体系上线' },
    { icon: '⚠', title: '网络升级维护', desc: '6月1日 02:00-04:00 UTC' },
    { icon: '🎉', title: '双倍ALEO奖励周', desc: '共享 50,000 ALEO 奖池' },
  ]
  return (
    <Modal open={open} onClose={onClose} title="公告详情">
      {items.map((it) => (
        <div key={it.title} className="py-3 border-b border-line last:border-b-0 flex gap-3 items-start">
          <div className="w-8 h-8 rounded-lg bg-bg-tertiary flex items-center justify-center text-sm shrink-0">
            {it.icon}
          </div>
          <div>
            <div className="font-semibold">{it.title}</div>
            <div className="text-xs text-text-muted mt-0.5">{it.desc}</div>
          </div>
        </div>
      ))}
    </Modal>
  )
}

// ============ 杠杆调整（原型方案3：分层 + AI 推荐） ============
const TIERS = [
  { key: 'conservative', name: '保守型', range: '1x - 3x', min: 1, max: 3, mm: '0.50%', color: 'text-up', feats: '维持保证金率 0.50% · 强平距离远 · 适合新手稳态持仓' },
  { key: 'balanced', name: '稳健型', range: '5x - 10x', min: 5, max: 10, mm: '1.00%', color: 'text-blue', feats: '维持保证金率 1.00% · 收益与风险平衡 · 推荐日常交易' },
  { key: 'aggressive', name: '激进型', range: '25x - 50x', min: 25, max: 50, mm: '2.50%', color: 'text-orange', feats: '维持保证金率 2.50% · 强平距离近 · 需密切盯盘' },
  { key: 'pro', name: '专业型', range: '75x - 125x', min: 75, max: 125, mm: '5.00%', color: 'text-down', feats: '维持保证金率 5.00% · 极高强平风险 · 仅限专业交易者', badge: '需认证' },
]
const PRESETS = [1, 2, 3, 5, 10, 25, 50, 100]

function tierByLev(lev: number) {
  if (lev >= 75) return 'pro'
  if (lev >= 25) return 'aggressive'
  if (lev >= 5) return 'balanced'
  return 'conservative'
}

function LeverageModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { leverage, setLeverage, setMarginMode } = useSettings()
  const [marginMode, setMargin] = useState<'cross' | 'isolated'>('cross')

  const estimates = useMemo(() => {
    const equity = toDecimal(15133.45)
    const entry = toDecimal(68245)
    const maxSize = equity.mul(leverage).div(entry)
    const liq = entry.mul(toDecimal(1).sub(toDecimal(1).div(leverage)))
    return {
      maxSize: maxSize.toFixed(2),
      liq: liq.toFixed(2),
      mm: TIERS.find((t) => t.key === tierByLev(leverage))?.mm ?? '0.50%',
    }
  }, [leverage])

  const confirm = () => {
    setLeverage(leverage)
    setMarginMode(marginMode)
    onClose()
  }

  return (
    <Modal open={open} onClose={onClose} title="调整杠杆">
      <div className="flex items-center gap-3 mb-3">
        <input
          type="range"
          min={1}
          max={125}
          value={leverage}
          onChange={(e) => setLeverage(Number(e.target.value))}
          className="flex-1 accent-orange"
        />
        <span className="text-xl font-bold text-orange min-w-12 text-center">{leverage}x</span>
      </div>

      <div className="flex flex-col gap-1.5 my-3">
        {TIERS.map((t) => (
          <div
            key={t.key}
            className={`p-2.5 border rounded-md cursor-pointer transition-all bg-bg-tertiary ${
              tierByLev(leverage) === t.key
                ? 'border-orange bg-orange/5 shadow-[0_0_0_1px_#f0b90b]'
                : 'border-line hover:border-line-light'
            }`}
            onClick={() => setLeverage(t.min)}
          >
            <div className="flex justify-between items-center">
              <span className={`text-xs font-bold ${t.color}`}>{t.name}</span>
              <span className="text-[11px] text-text-muted font-mono">
                {t.range}
                {t.badge && <span className="ml-1.5 text-[9px] px-1 py-px rounded bg-bg-secondary text-text-muted">{t.badge}</span>}
              </span>
            </div>
            <div className="text-[10px] text-text-muted mt-0.5 leading-relaxed">{t.feats}</div>
          </div>
        ))}
      </div>

      <div className="mb-2 p-2.5 bg-ai-glow border border-dashed border-purple rounded-md text-[11px] text-purple flex items-center gap-1.5">
        <span>🤖 AI 智能风控</span>
        <span>基于当前 2 个持仓与 24h 波动率(2.1%)，推荐 <b>3x · 保守型</b></span>
        <button
          className="ml-auto bg-purple text-white border-none px-2.5 py-0.5 rounded text-[10px] cursor-pointer"
          onClick={() => setLeverage(3)}
        >
          应用
        </button>
      </div>

      <div className="flex gap-1.5 my-3 flex-wrap">
        {PRESETS.map((p) => (
          <button
            key={p}
            className={`flex-1 min-w-[44px] py-1.5 border rounded text-[13px] font-semibold cursor-pointer ${
              leverage === p ? 'text-orange border-orange bg-orange/10' : 'bg-bg-tertiary text-text-secondary border-line'
            }`}
            onClick={() => setLeverage(p)}
          >
            {p}x
          </button>
        ))}
      </div>

      <div className="flex gap-1 my-2">
        {(['cross', 'isolated'] as const).map((m) => (
          <button
            key={m}
            className={`flex-1 py-1.5 border rounded text-xs font-semibold cursor-pointer ${
              marginMode === m ? 'text-text-primary border-blue bg-blue-bg' : 'bg-bg-tertiary text-text-secondary border-line'
            }`}
            onClick={() => setMargin(m)}
          >
            {m === 'cross' ? '全仓' : '逐仓'}
          </button>
        ))}
      </div>

      <div className="p-3 bg-bg-tertiary rounded-md">
        <InfoRow label="最大持仓量" value={`${estimates.maxSize} BTC`} />
        <InfoRow label="维持保证金率" value={estimates.mm} />
        <InfoRow label="预计强平价格" value={`${formatNumber(estimates.liq)} USDT`} valueClassName="text-orange" />
        <InfoRow label="初始保证金" value="15,133.45 USDT" />
      </div>
      <button
        className="w-full bg-blue text-white border-none rounded py-2.5 font-bold text-sm cursor-pointer hover:opacity-90 mt-4"
        onClick={confirm}
      >
        确认
      </button>
    </Modal>
  )
}

function InfoRow({ label, value, valueClassName = '' }: { label: string; value: string; valueClassName?: string }) {
  return (
    <div className="flex justify-between text-xs my-1">
      <span className="text-text-muted">{label}</span>
      <span className={`font-mono ${valueClassName}`}>{value}</span>
    </div>
  )
}
