import { useMemo, useRef, useState } from 'react'
import { useSettings, type OrderType, type PrivacyMode } from '../../stores/settings'
import { useTrade } from '../../stores/trade'
import { useUi } from '../../stores/ui'
import { toDecimal } from '../../lib/decimal'
import { formatNumber } from '../../lib/format'
import { toChannelSymbol } from '../../lib/symbol'
import { pairMode, pairTokens } from '../../lib/tokens'
import { nextOrderId, submitAleoOrder, submitPrivacyOrder, submitPublicOrder, submitTxOrder } from '../../lib/api/orders'
import { privateBalance } from '../../lib/wallet/types'

// 人类单位 -> 6 位最小单位（BigInt，与隐私路径链上 record 单位一致）
function toUnits6(v: string): bigint {
  const n = v.replace(/,/g, '').trim()
  const [int, frac = ''] = n.split('.')
  const scale = 10n ** 6n
  return BigInt(int) * scale + BigInt((frac + '000000').slice(0, 6) || '0')
}
import { useNavigate } from 'react-router-dom'
import { useWallet } from '../../stores/wallet'

// 提交状态：'idle' | 'submitting' | 'ok' | 'error'
type SubmitState = 'idle' | 'submitting' | 'ok' | 'error'

// 隐私模式三档（原型方案1）：说明 + 指标，常量表驱动
const PRIVACY_MODES: Record<
  PrivacyMode,
  { label: string; desc: JSX.Element; metrics: [string, string, 'fast' | 'mid' | 'slow'][] }
> = {
  standard: {
    label: '标准',
    desc: (
      <>
        <b className="text-text-secondary">标准（明文）：</b>订单直接进入公开订单簿，对手方可见价格与数量。成交最快，但订单信息完全暴露，易被 MEV / 嗅探识别。
      </>
    ),
    metrics: [
      ['成交速度', '即时', 'fast'],
      ['信息暴露', '完全', 'slow'],
      ['MEV 防护', '无', 'slow'],
    ],
  },
  privacy: {
    label: '隐私',
    desc: (
      <>
        <b className="text-text-secondary">隐私（加密）：</b>订单以 Note 承诺加密入簿，对手方仅见承诺不见明文。成交稍慢（需解密匹配），防 MEV 与前置交易。
      </>
    ),
    metrics: [
      ['成交速度', '稍慢', 'mid'],
      ['信息暴露', '仅承诺', 'fast'],
      ['MEV 防护', '有', 'fast'],
    ],
  },
  darkpool: {
    label: '暗池',
    desc: (
      <>
        <b className="text-text-secondary">暗池（MPC）：</b>订单不进公开簿，经 MPC 加密撮合，零信息泄露。大额优先，成交可能延迟，适合机构大单。
      </>
    ),
    metrics: [
      ['成交速度', '可能延迟', 'slow'],
      ['信息暴露', '零暴露', 'fast'],
      ['MEV 防护', '完全', 'fast'],
    ],
  },
}

const METRIC_COLOR: Record<'fast' | 'mid' | 'slow', string> = {
  fast: 'text-up',
  mid: 'text-orange',
  slow: 'text-down',
}

const ORDER_TYPES: { key: OrderType; label: string }[] = [
  { key: 'limit', label: '限价' },
  { key: 'market', label: '市价' },
  { key: 'stop-limit', label: '止损限价' },
  { key: 'stop-market', label: '止损市价' },
]

// 下单面板：对齐原型 #order-form-panel（余额/隐私模式/方向/类型/价格数量/杠杆/TP-SL）
export function OrderForm() {
  const {
    tradingMode,
    direction,
    setDirection,
    orderType,
    setOrderType,
    privacyMode,
    setPrivacyMode,
    pair,
    leverage,
    setLeverage,
    marginMode,
  } = useSettings()
  const { price, amount, stopPrice, tp, sl, setPrice, setAmount, setStopPrice, setTp, setSl } = useTrade()
  const openModal = useUi((s) => s.openModal)
  const navigate = useNavigate()
  const wallet = useWallet((s) => s.wallet)
  const walletAddress = useWallet((s) => s.address)
  const [submitState, setSubmitState] = useState<SubmitState>('idle')
  const [submitError, setSubmitError] = useState('')
  const timerRef = useRef<number | null>(null)

  // 三种下单模式：
  // - 标准：钱包链上锁仓 -> 引擎下单（p2 明文 / p4 tx_id 提取）
  // - 隐私：钱包链上锁仓（链上加密 record）-> POST /order/privacy（仅 tx_id，不发明文；
  //         引擎用 operator view key 解密 Order record 后撮合 —— Aleo 原生隐私）
  // - 暗池：跳转暗池页（暗池撮合暂未实现）
  // p4 真实币对（ALEO/USDCX）：标准与隐私都走 tx_id —— 引擎统一提取+解密
  // （订单参数只存在于链上加密 record，HTTP 不传价格/数量）
  const submit = async () => {
    if (submitState === 'submitting') return

    if (privacyMode === 'darkpool') {
      navigate('/darkpool')
      return
    }

    const channelSymbol = toChannelSymbol(pair)
    const isP4 = pairMode(channelSymbol) === 'p4-real'
    const tokens = pairTokens(channelSymbol)
    const clean = (v: string) => v.replace(/,/g, '').trim()
    const priceVal = clean(price)
    const amountVal = clean(amount)
    if (!priceVal || !amountVal || Number(priceVal) <= 0 || Number(amountVal) <= 0) {
      setSubmitError('请输入有效的价格与数量')
      setSubmitState('error')
      return
    }
    const orderId = nextOrderId()
    const trader = walletAddress ?? localStorage.getItem('aleo_address') ?? 'aleo1dev-placeholder'
    setSubmitState('submitting')
    setSubmitError('')

    let placedTxId = ''
    let ciphertext: string | undefined
    try {
      if (wallet.isConnected()) {
        // 链上锁仓：钱包执行 place_order（p2）/place_order_buy/sell（p4）-> 链上 txId；
        // operator 地址由适配器从引擎 /api/v1/operator 获取（chain.aleo.address 配置）
        const placed = await wallet.placeOrder({
          symbol: channelSymbol,
          orderId,
          side: direction === 'long' ? 0 : 1,
          price: priceVal,
          amount: amountVal,
          baseToken: tokens.base,
          quoteToken: tokens.quote,
          deadline: Math.floor(Date.now() / 1000) + 3600,
          operator: '',
          // p6：标准模式走公开余额托管（place_order_*_public，无 record 输入），
          // 隐私模式走 record 托管（place_order_*_private，uid 定位）
          mode: isP4 ? (privacyMode === 'standard' ? 'standard' : 'privacy') : undefined,
        })
        placedTxId = placed.txId
        ciphertext = placed.ciphertext
      } else {
        // 未连接：p4 真实币对与隐私模式必须真实钱包（链上托管+解密撮合）
        if (privacyMode === 'privacy') throw new Error('隐私下单需要真实钱包（链上加密+解密撮合）')
        if (isP4) throw new Error('ALEO/USDCX 下单需要 Shield 钱包（链上托管+引擎提取）')
        ciphertext = 'ciphertext1dev-ui-placeholder'
        if (!import.meta.env.DEV && wallet.kind !== 'dev') {
          throw new Error('请先连接钱包')
        }
      }
    } catch (e) {
      setSubmitState('error')
      setSubmitError(e instanceof Error ? e.message : String(e))
      return
    }

    const res = isP4
      ? privacyMode === 'standard'
        ? await submitPublicOrder({
            order_id: orderId,
            symbol: channelSymbol,
            side: direction === 'long' ? 0 : 1,
            // 明文提交须与链上/撮合单位一致：6 位最小单位（0.016 -> 16000，1 -> 1000000）
            price: toUnits6(priceVal).toString(),
            amount: toUnits6(amountVal).toString(),
            deadline: Math.floor(Date.now() / 1000) + 3600,
            trader,
          })
        : await submitTxOrder({ tx_id: placedTxId, symbol: channelSymbol, trader })
      : privacyMode === 'privacy'
        ? await submitPrivacyOrder({ tx_id: placedTxId, symbol: channelSymbol, trader })
        : await submitAleoOrder({
            order_id: orderId,
            side: direction === 'long' ? 0 : 1,
            price: priceVal,
            amount: amountVal,
            symbol: channelSymbol,
            base_token: tokens.base,
            quote_token: tokens.quote,
            deadline: Math.floor(Date.now() / 1000) + 3600,
            trader,
            ciphertext: ciphertext ?? '',
          })
    setSubmitState(res.ok ? 'ok' : 'error')
    if (!res.ok) setSubmitError(res.error ?? '提交失败')
    if (timerRef.current) window.clearTimeout(timerRef.current)
    timerRef.current = window.setTimeout(() => setSubmitState('idle'), 2000)
  }

  const base = pair.split('/')[0]
  const quote = pair.split('/')[1]
  const isPerp = tradingMode === 'perp'
  //const balance = isPerp ? '15,133.45 USDT' : '128,456.78 USDT'
  const balances = useWallet((s) => s.balances)
  const isP4 = pairMode(toChannelSymbol(pair)) === 'p4-real'
  // 余额按下单模式区分（p4 真实币对）：
  // - 标准 tab：显示公开余额（transfer_public 托管，无需 record/凭证）
  // - 隐私/暗池 tab：显示隐私余额（record 托管，总 - 公开）
  // p2 铸币币对无公开/隐私之分（Token record 唯一形态），各 tab 一致
  const quoteBalance = !isP4
    ? balances?.usdt ?? '--'
    : privacyMode === 'standard'
      ? balances?.usdcxPublic ?? '--'
      : privateBalance(balances?.usdcx, balances?.usdcxPublic)
  // 强平价估算：liq = entry * (1 - 1/lev)（对齐原型 updateLiquidationEstimate，decimal 精确计算）
  const liqPrice = useMemo(() => {
    const entry = toDecimal(68245)
    const liq = entry.mul(toDecimal(1).sub(toDecimal(1).div(leverage)))
    return formatNumber(liq.toNumber(), 2)
  }, [leverage])
  const m = PRIVACY_MODES[privacyMode]

  const inputCls =
    'w-full bg-bg-tertiary border border-line text-text-primary px-2.5 py-[7px] rounded text-[13px] font-mono focus:border-blue focus:outline-none'

  return (
    <div className="bg-bg-secondary border-r border-line p-3 border-b border-line">
      <div className="flex justify-between mb-2.5 text-xs">
        <span className="text-text-muted">
          {isPerp ? '账户权益' : isP4 ? (privacyMode === 'standard' ? '钱包余额 · 公开' : '钱包余额 · 隐私') : '钱包余额'}
        </span>
        <span className="text-text-primary font-semibold">{quoteBalance} {quote}</span>
      </div>

      {/* 隐私模式选择器 */}
      <div className="mb-2.5 p-2 bg-bg-tertiary border border-line rounded-md">
        <div className="flex items-center gap-1.5 text-[11px] text-cyan font-semibold mb-1.5">
          🛡 下单模式 <span className="text-text-muted font-normal">· 隐私优先</span>
        </div>
        <div className="flex gap-1 mb-1.5">
          {(Object.keys(PRIVACY_MODES) as PrivacyMode[]).map((k) => (
            <button
              key={k}
              className={`flex-1 py-1.5 px-0.5 border rounded text-[11px] font-semibold cursor-pointer transition-colors text-center ${
                privacyMode === k ? ACTIVE_CLS[k] : 'bg-bg-secondary text-text-secondary border-line'
              }`}
              onClick={() => setPrivacyMode(k)}
            >
              {PRIVACY_MODES[k].label}
            </button>
          ))}
        </div>
        <div className="text-[10px] text-text-muted leading-relaxed pt-0.5">{m.desc}</div>
        <div className="flex gap-1.5 mt-1.5 pt-1.5 border-t border-line">
          {m.metrics.map(([label, value, color]) => (
            <div key={label} className="flex-1">
              <span className="text-text-muted text-[9px] block">{label}</span>
              <span className={`font-bold text-[11px] ${METRIC_COLOR[color]}`}>{value}</span>
            </div>
          ))}
        </div>
      </div>

      {/* 方向切换 */}
      <div className="flex gap-1 mb-2.5">
        <button
          className={`flex-1 py-[7px] border rounded text-[13px] font-semibold cursor-pointer transition-colors ${
            direction === 'long'
              ? 'bg-up-bg text-up border-up'
              : 'bg-bg-tertiary text-text-secondary border-line'
          }`}
          onClick={() => setDirection('long')}
        >
          {isPerp ? '开多' : '买入'}
        </button>
        <button
          className={`flex-1 py-[7px] border rounded text-[13px] font-semibold cursor-pointer transition-colors ${
            direction === 'short'
              ? 'bg-down-bg text-down border-down'
              : 'bg-bg-tertiary text-text-secondary border-line'
          }`}
          onClick={() => setDirection('short')}
        >
          {isPerp ? '开空' : '卖出'}
        </button>
      </div>

      {/* 订单类型 */}
      <div className="flex gap-0.5 mb-2.5 border-b border-line">
        {ORDER_TYPES.map((t) => (
          <button
            key={t.key}
            className={`pb-1.5 pt-0.5 text-[11px] border-none bg-transparent cursor-pointer border-b-2 transition-colors ${
              orderType === t.key ? 'text-text-primary border-b-blue' : 'text-text-muted border-b-transparent'
            }`}
            onClick={() => setOrderType(t.key)}
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* 价格 / 触发价 / 数量 */}
      {(orderType === 'limit' || orderType === 'stop-limit') && (
        <div className="mb-2">
          <label className="block text-[11px] text-text-muted mb-0.5">价格 ({quote})</label>
          <input className={inputCls} value={price} onChange={(e) => setPrice(e.target.value)} />
        </div>
      )}
      {(orderType === 'stop-limit' || orderType === 'stop-market') && (
        <div className="mb-2">
          <label className="block text-[11px] text-text-muted mb-0.5">触发价格 ({quote})</label>
          <input
            className={inputCls}
            value={stopPrice}
            placeholder="触发价格"
            onChange={(e) => setStopPrice(e.target.value)}
          />
        </div>
      )}
      <div className="mb-2">
        <label className="block text-[11px] text-text-muted mb-0.5">数量 ({base})</label>
        <div className="relative">
          <input className={inputCls} value={amount} onChange={(e) => setAmount(e.target.value)} />
          <span className="absolute right-2.5 top-1/2 -translate-y-1/2 text-[11px] text-text-muted">{base}</span>
        </div>
      </div>

      {/* 快捷比例 */}
      <div className="flex gap-1 mb-2">
        {[0.25, 0.5, 0.75, 1].map((pct) => (
          <button
            key={pct}
            className="flex-1 py-1 border border-line rounded bg-bg-tertiary text-text-secondary cursor-pointer text-[11px] hover:text-text-primary hover:border-blue"
            onClick={() => {
              const baseAmount = isPerp ? 0.738 : 1.884
              setAmount((baseAmount * pct).toFixed(4))
            }}
          >
            {pct * 100}%
          </button>
        ))}
      </div>

      {/* 杠杆（永续） */}
      {isPerp && (
        <>
          <div className="flex items-center gap-2 mb-2">
            <label className="text-[11px] text-text-muted whitespace-nowrap">杠杆</label>
            <input
              type="range"
              min={1}
              max={125}
              value={leverage}
              onChange={(e) => setLeverage(Number(e.target.value))}
              className="flex-1 accent-orange"
            />
            <span className="text-[13px] font-bold text-orange min-w-8 text-center">{leverage}x</span>
            <button
              className="bg-transparent border border-line text-text-secondary px-2 py-0.5 rounded text-[11px] cursor-pointer"
              onClick={() => openModal('leverage')}
            >
              编辑
            </button>
          </div>
          <div className="flex gap-1.5 mb-2">
            <div className="flex-1">
              <label className="block text-[11px] text-text-muted mb-0.5">止盈 (USDT)</label>
              <input className={inputCls} value={tp} placeholder="止盈价" onChange={(e) => setTp(e.target.value)} />
            </div>
            <div className="flex-1">
              <label className="block text-[11px] text-text-muted mb-0.5">止损 (USDT)</label>
              <input className={inputCls} value={sl} placeholder="止损价" onChange={(e) => setSl(e.target.value)} />
            </div>
          </div>
          <div className="flex justify-between text-[11px] text-text-muted mb-2">
            <span>{marginMode === 'cross' ? '全仓' : '逐仓'}</span>
            <span>
              强平: <span className="text-orange">{liqPrice}</span>
            </span>
          </div>
        </>
      )}

      {/* 提交（Aleo 链下订单通道 POST /order） */}
      <button
        className={`w-full py-2.5 border-none rounded font-bold text-sm cursor-pointer text-white hover:opacity-85 ${
          direction === 'long' ? 'bg-up' : 'bg-down'
        } ${submitState === 'submitting' ? 'opacity-60' : ''}`}
        onClick={submit}
        disabled={submitState === 'submitting'}
      >
        {submitState === 'submitting'
          ? '提交中…'
          : submitState === 'ok'
            ? '已提交 ✓'
            : `${direction === 'long' ? '买入' : '卖出'}${isPerp ? '/开' + (direction === 'long' ? '多' : '空') : ''} ${base}`}
      </button>
      {submitState === 'error' && (
        <div className="mt-1.5 text-[11px] text-down break-all">提交失败: {submitError}</div>
      )}
    </div>
  )
}

const ACTIVE_CLS: Record<PrivacyMode, string> = {
  standard: 'bg-up-bg text-up border-up',
  privacy: 'bg-cyan/10 text-cyan border-cyan',
  darkpool: 'bg-ai-glow text-purple border-purple',
}
