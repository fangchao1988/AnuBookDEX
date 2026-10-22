import { useEffect, useRef, useState } from 'react'
import { useSettings } from '../../stores/settings'
import { useUi } from '../../stores/ui'
import { TICKER } from '../../mock/market'
import { useMarket } from '../../stores/marketStore'
import { useMarketChannels } from '../../hooks/useMarketChannels'
import { toChannelSymbol } from '../../lib/symbol'
import { formatNumber, formatPrice, truncateAddress } from '../../lib/format'
import { useWallet } from '../../stores/wallet'
import { useChain } from '../../stores/chain'
import {
  IconBell,
  IconBolt,
  IconChevronDown,
  IconEyeOff,
  IconLock,
  IconStar,
} from '../../components/ui/icons'

const PAIRS = [
  { symbol: 'ALEO/USDCX', starred: true },
  { symbol: 'ETH/USDT', starred: true },
  { symbol: 'BTC/USDT', starred: true },
  { symbol: 'SOL/USDT', starred: false },
]

// 顶栏：对齐原型 #topbar（模式切换/交易对/Ticker/工具按钮/钱包）
export function TopBar() {
  const {
    tradingMode,
    setTradingMode,
    pair,
    setPair,
    simpleMode,
    toggleSimpleMode,
    privacyOff,
    togglePrivacyOff,
  } = useSettings()
  const openModal = useUi((s) => s.openModal)
  // 双链：Anubis 用 EVM 钱包（MetaMask），Aleo 用 Shield/Leo 钱包
  const chain = useChain((s) => s.chain)
  const setChain = useChain((s) => s.setChain)
  const evmNetwork = useChain((s) => s.evmNetwork)
  const evmBalance = useChain((s) => s.evmBalance)
  const chainConnect = useChain((s) => s.connect)
  const walletChoices = useChain((s) => s.walletChoices)
  const connectWithWallet = useChain((s) => s.connectWithWallet)
  const cancelWalletPicker = useChain((s) => s.cancelWalletPicker)
  const chainDisconnect = useChain((s) => s.disconnect)
  const chainError = useChain((s) => s.error)
  const chainClearError = useChain((s) => s.clearError)
  // 注意：两个 store 的 hook 必须无条件调用（Rules of Hooks），
  // 再按当前链选取生效的那一套，避免切链时 hook 顺序变化。
  const evmAddress = useChain((s) => s.address)
  const aleoAddress = useWallet((s) => s.address)
  const aleoConnect = useWallet((s) => s.connect)
  const aleoDisconnect = useWallet((s) => s.disconnect)
  const aleoError = useWallet((s) => s.error)
  const aleoClearError = useWallet((s) => s.clearError)

  const isAnubis = chain === 'anubis'
  const walletAddress = isAnubis ? evmAddress : aleoAddress
  const walletConnect = isAnubis ? chainConnect : aleoConnect
  const walletDisconnect = isAnubis ? chainDisconnect : aleoDisconnect
  const walletError = isAnubis ? chainError : aleoError
  const walletClearError = isAnubis ? chainClearError : aleoClearError
  const [pairOpen, setPairOpen] = useState(false)
  const [walletOpen, setWalletOpen] = useState(false)
  const [chainOpen, setChainOpen] = useState(false)
  const pairRef = useRef<HTMLDivElement>(null)
  const walletRef = useRef<HTMLDivElement>(null)
  const chainRef = useRef<HTMLDivElement>(null)

  // 实时行情：Ticker + 连接状态（未连接时回退 mock）
  const channelSymbol = toChannelSymbol(pair)
  useMarketChannels(pair)
  const ticker = useMarket((s) => s.getTicker(channelSymbol))
  const lastTrade = useMarket((s) => s.getTrades(channelSymbol, 1))
  const wsStatus = useMarket((s) => s.status)
  const hasLive = ticker !== null

  const lastPrice = hasLive ? ticker!.close : lastTrade[0]?.price ?? TICKER.last
  const changeText = hasLive
    ? `${formatNumber(Number(ticker!.change), 2)} (${formatNumber(Number(ticker!.changePercent) * 100, 2)}%)`
    : TICKER.change
  const changeUp = hasLive ? Number(ticker!.change) >= 0 : true
  const high = hasLive ? ticker!.high : TICKER.high
  const low = hasLive ? ticker!.low : TICKER.low
  const vol = hasLive ? `${formatNumber(Number(ticker!.vol), 2)} ${pair.split('/')[0]}` : TICKER.vol

  useEffect(() => {
    const onClick = (e: MouseEvent) => {
      if (pairRef.current && !pairRef.current.contains(e.target as Node)) setPairOpen(false)
      if (walletRef.current && !walletRef.current.contains(e.target as Node)) setWalletOpen(false)
    }
    document.addEventListener('click', onClick)
    return () => document.removeEventListener('click', onClick)
  }, [])

  return (
    <header className="h-14 shrink-0 bg-bg-secondary border-b border-line flex items-center px-4 gap-3 z-[100]">
      {/* 模式切换 */}
      <div className="flex bg-bg-tertiary rounded-md p-0.5">
        {(['spot', 'perp'] as const).map((mode) => (
          <button
            key={mode}
            className={`px-3.5 py-1.5 rounded border-none text-xs font-semibold cursor-pointer whitespace-nowrap transition-colors ${
              tradingMode === mode ? 'bg-bg-primary text-text-primary' : 'bg-transparent text-text-secondary'
            }`}
            onClick={() => setTradingMode(mode)}
          >
            {mode === 'spot' ? '现货' : '永续合约'}
          </button>
        ))}
      </div>

      {/* 交易对选择 */}
      <div className="pair-selector relative cursor-pointer" ref={pairRef}>
        <div className="text-[15px] font-bold flex items-center gap-1" onClick={() => setPairOpen((v) => !v)}>
          {pair}
          <IconChevronDown className="w-3.5 h-3.5 text-text-muted" />
        </div>
        {pairOpen && (
          <div className="absolute top-full left-0 bg-bg-secondary border border-line rounded-md min-w-[160px] z-50 mt-1 shadow-dropdown">
            {PAIRS.map((p) => (
              <div
                key={p.symbol}
                className={`px-3.5 py-2 text-[13px] flex items-center gap-2 cursor-pointer hover:bg-bg-hover ${
                  p.symbol === pair ? 'text-text-primary' : ''
                }`}
                onClick={() => {
                  setPair(p.symbol)
                  setPairOpen(false)
                }}
              >
                {p.starred ? <IconStar className="w-3 h-3 text-orange" /> : <span className="w-3 h-3" />}
                {p.symbol}
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Ticker（WS 实时数据，未连接时回退 mock） */}
      <div className="flex gap-4 items-center">
        <span className={`text-lg font-bold ${changeUp ? 'text-up' : 'text-down'}`}>{formatPrice(lastPrice)}</span>
        <span className={`text-xs ${changeUp ? 'text-up' : 'text-down'}`}>{changeText}</span>
        <span className="text-xs">
          <span className="text-text-muted">24H高</span> <span className="text-text-secondary">{formatPrice(high)}</span>
        </span>
        <span className="text-xs">
          <span className="text-text-muted">24H低</span> <span className="text-text-secondary">{formatPrice(low)}</span>
        </span>
        <span className="text-xs">
          <span className="text-text-muted">24H量</span> <span className="text-text-secondary">{vol}</span>
        </span>
        <span
          className={`w-1.5 h-1.5 rounded-full ${hasLive && wsStatus === 'open' ? 'bg-up' : wsStatus === 'connecting' ? 'bg-orange' : 'bg-down'}`}
          title={`WS 状态: ${wsStatus}${hasLive ? ' · 实时数据' : ' · 演示数据'}`}
        />
        {tradingMode === 'perp' && (
          <>
            <span className="text-xs">
              <span className="text-text-muted">标记价</span> <span className="text-text-secondary text-orange">{TICKER.mark}</span>
            </span>
            <span className="text-xs">
              <span className="text-text-muted">资金费率</span> <span className="text-up">{TICKER.funding}</span>
            </span>
          </>
        )}
      </div>

      <div className="flex-1" />

      {/* 工具按钮 */}
      <button
        className="relative w-8 h-8 rounded-md border-none bg-transparent text-text-muted hover:text-text-primary hover:bg-bg-tertiary flex items-center justify-center cursor-pointer"
        title="公告 (3)"
        onClick={() => openModal('announce')}
      >
        <IconBell className="w-4 h-4" />
        <span className="absolute top-1 right-1 w-1.5 h-1.5 bg-down rounded-full" />
      </button>
      <button
        className="w-8 h-8 rounded-md border-none bg-transparent text-purple hover:bg-ai-glow flex items-center justify-center cursor-pointer"
        title="AI 策略设置"
        onClick={() => openModal('ai')}
      >
        <IconBolt className="w-4 h-4" />
      </button>
      <button
        className={`w-8 h-8 rounded-md border-none bg-transparent flex items-center justify-center cursor-pointer ${
          privacyOff ? 'text-text-muted' : 'text-cyan'
        }`}
        title="隐私模式"
        onClick={togglePrivacyOff}
      >
        <IconLock className="w-4 h-4" />
      </button>
      <button
        className="w-8 h-8 rounded-md border-none bg-transparent text-text-muted hover:text-text-primary hover:bg-bg-tertiary flex items-center justify-center cursor-pointer"
        title="简易模式"
        onClick={toggleSimpleMode}
      >
        <IconEyeOff className={`w-4 h-4 ${simpleMode ? 'text-up' : ''}`} />
      </button>
      {/* 链切换：Aleo（Leo 隐私） / Anubis（EVM） */}
      <div className="relative" ref={chainRef}>
        <button
          className="border border-line bg-bg-tertiary text-text-primary px-3 py-[7px] rounded-md font-semibold text-xs cursor-pointer hover:opacity-90 flex items-center gap-1"
          onClick={() => setChainOpen((v) => !v)}
          title="切换链"
        >
          {isAnubis ? 'Anubis' : 'Aleo'} <IconChevronDown className="w-3 h-3" />
        </button>
        {chainOpen && (
          <div className="absolute top-full right-0 bg-bg-secondary border border-line rounded-md min-w-[220px] z-50 mt-1 shadow-dropdown">
            <div
              className={`px-3.5 py-2.5 text-xs cursor-pointer hover:bg-bg-hover ${isAnubis ? 'text-up' : 'text-text-primary'}`}
              onClick={() => {
                void setChain('aleo')
                setChainOpen(false)
              }}
            >
              Aleo <span className="text-text-muted text-[10px]">· Leo record 隐私</span>
            </div>
            <div
              className={`px-3.5 py-2.5 text-xs cursor-pointer hover:bg-bg-hover border-t border-line ${isAnubis ? 'text-up' : 'text-text-primary'}`}
              onClick={() => {
                void setChain('anubis')
                setChainOpen(false)
              }}
            >
              Anubis <span className="text-text-muted text-[10px]">· EVM · {evmNetwork === 'mainnet' ? '主网' : 'Cheras 测试网'}</span>
            </div>
          </div>
        )}
      </div>
      {/* 钱包：未连接 -> 连接按钮；已连接 -> 地址下拉（断开连接） */}
      <div className="relative" ref={walletRef}>
        <button
          className={`border-none px-4 py-[7px] rounded-md font-semibold text-xs cursor-pointer hover:opacity-90 ${
            walletAddress ? 'bg-bg-tertiary text-text-primary border border-line' : 'bg-blue text-white'
          }`}
          onClick={
            walletAddress
              ? () => setWalletOpen((v) => !v)
              : () => void walletConnect().catch(() => {})
          }
        >
          {walletAddress ? `${truncateAddress(walletAddress)} ▾` : '连接钱包'}
        </button>
        {/* 多钱包选择器（EIP-6963）：OKX/MetaMask 等共存时让用户明确选择 */}
        {walletChoices && walletChoices.length > 0 && (
          <div className="absolute top-full right-0 mt-1 bg-bg-secondary border border-line rounded-md min-w-[240px] z-50 shadow-dropdown">
            <div className="px-3.5 py-2 border-b border-line text-[10px] text-text-muted">
              检测到 {walletChoices.length} 个钱包，请选择
            </div>
            {walletChoices.map((w) => (
              <div
                key={w.info.uuid}
                className="px-3.5 py-2.5 text-xs text-text-primary cursor-pointer hover:bg-bg-hover flex items-center gap-2"
                onClick={() => void connectWithWallet(w.info.rdns).catch(() => {})}
              >
                {w.info.icon && (
                  <img src={w.info.icon} alt="" className="w-4 h-4 rounded-sm" />
                )}
                <span>{w.info.name}</span>
              </div>
            ))}
            <div
              className="px-3.5 py-2 text-[10px] text-text-muted cursor-pointer hover:text-text-primary border-t border-line"
              onClick={cancelWalletPicker}
            >
              取消
            </div>
          </div>
        )}
        {walletError && (
          <div className="absolute top-full right-0 mt-1 bg-bg-secondary border border-down/40 rounded-md min-w-[260px] max-w-[340px] z-50 shadow-dropdown">
            <div className="px-3 py-2 text-[11px] text-down leading-relaxed break-all">{walletError}</div>
            <div
              className="px-3 pb-2 text-[10px] text-text-muted cursor-pointer hover:text-text-primary"
              onClick={() => walletClearError()}
            >
              关闭
            </div>
          </div>
        )}
        {walletOpen && walletAddress && (
          <div className="absolute top-full right-0 bg-bg-secondary border border-line rounded-md min-w-[220px] z-50 mt-1 shadow-dropdown">
            <div className="px-3.5 py-2.5 border-b border-line">
              <div className="text-[10px] text-text-muted mb-0.5">
                已连接 {chain === 'anubis' ? (evmNetwork === 'mainnet' ? 'Anubis Mainnet' : 'Anubis Cheras 测试网') : 'Aleo 网络'}
                {chain === 'anubis' && evmBalance !== '0' ? ` · ${evmBalance} DAI` : ''}
              </div>
              <div className="font-mono text-xs text-text-secondary break-all">{walletAddress}</div>
            </div>
            <div
              className="px-3.5 py-2 text-xs text-down cursor-pointer hover:bg-bg-hover"
              onClick={() => {
                walletDisconnect()
                setWalletOpen(false)
              }}
            >
              断开连接
            </div>
          </div>
        )}
      </div>
    </header>
  )
}
