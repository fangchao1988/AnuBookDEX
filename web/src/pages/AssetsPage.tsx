import { useEffect, useState } from 'react'
import { useWallet } from '../stores/wallet'
import { useChain } from '../stores/chain'
import { fetchAnubisTrades, settleLabel, tradePerspective, type AnubisTradeRecord } from '../lib/api/anubis'
import { useMarket } from '../stores/marketStore'
import { useMarketChannels } from '../hooks/useMarketChannels'
import { formatNumber, dateTimeStr } from '../lib/format'
import { fetchTrades, type TradeRecord } from '../lib/api/orders'
import { scalePairValue } from '../lib/tokens'
import { privateBalance } from '../lib/wallet/types'

// ALEO 估值单价（USDT）：链上无 ALEO 行情源，暂用常量估算（标注估）
const ALEO_PRICE = 0.5

// 资产总览：真实钱包数据（链上余额 + 行情估值）
export default function AssetsPage() {
  const chain = useChain((s) => s.chain)
  // 双链分流：Anubis（EVM/DAI）与 Aleo（records）资产模型完全不同，
  // 拆成独立组件而非在同一张表里混显（避免展示无关链的资产）
  if (chain === 'anubis') return <AnubisAssets />
  return <AleoAssets />
}

/** Anubis（EVM）资产：gas/结算币 DAI + 本链成交 */
function AnubisAssets() {
  const address = useChain((s) => s.address)
  const config = useChain((s) => s.evmConfig)
  const balance = useChain((s) => s.evmBalance)
  const refreshBalances = useChain((s) => s.refreshBalances)
  const [trades, setTrades] = useState<AnubisTradeRecord[]>([])

  useEffect(() => {
    if (!address) return
    let alive = true
    const load = async () => {
      try {
        const list = await fetchAnubisTrades({ limit: 50 })
        // 后端返回该币对所有成交（含双方），此处按「我的地址」过滤 + 解析方向，
        // 否则一笔成交会以买卖两条的形式重复展示
        if (alive) setTrades(list.filter((t) => tradePerspective(t, address).mine))
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
  }, [address])

  const dai = Number(balance.replace(/,/g, '')) || 0

  return (
    <div className="p-5 overflow-y-auto h-full">
      <h2 className="text-xl font-semibold mb-5">资产总览</h2>

      {!address ? (
        <div className="bg-bg-secondary border border-line rounded-lg p-10 text-center">
          <div className="text-[15px] text-text-secondary mb-2">连接钱包查看 Anubis 链上资产</div>
          <div className="text-xs text-text-muted">
            当前网络：{config.chainName}（chainId {config.chainId}）
          </div>
        </div>
      ) : (
        <>
          <div className="grid gap-3 grid-cols-[repeat(auto-fill,minmax(240px,1fr))]">
            <div className="bg-bg-secondary border border-line rounded-lg p-4">
              <div className="text-[11px] text-text-muted uppercase tracking-wide mb-1.5">DAI 余额</div>
              <div className="text-[22px] font-bold">{formatNumber(dai, 4)}</div>
              <div className="text-[11px] text-text-secondary mt-1">Anubis 原生 gas 代币</div>
            </div>
            <div className="bg-bg-secondary border border-line rounded-lg p-4">
              <div className="text-[11px] text-text-muted uppercase tracking-wide mb-1.5">网络</div>
              <div className="text-[15px] font-semibold">{config.chainName}</div>
              <div className="text-[11px] text-text-muted mt-1 font-mono">chainId {config.chainId}</div>
            </div>
            <div className="bg-bg-secondary border border-line rounded-lg p-4">
              <div className="text-[11px] text-text-muted uppercase tracking-wide mb-1.5">成交笔数</div>
              <div className="text-[22px] font-bold">{trades.length}</div>
              <div className="text-[11px] text-text-secondary mt-1">链上结算由引擎提交</div>
            </div>
          </div>

          <h3 className="text-[15px] text-text-secondary mt-6 mb-3 border-b border-line pb-2">我的成交</h3>
          <div className="bg-bg-secondary border border-line rounded-lg overflow-hidden">
            <table className="w-full border-collapse text-[13px]">
              <thead>
                <tr>
                  {['订单号', '交易对', '方向', '价格', '数量', '结算状态', '交易哈希'].map((h) => (
                    <th key={h} className="bg-bg-tertiary text-text-muted font-normal px-4 py-2 text-left text-[11px]">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {trades.length === 0 ? (
                  <tr>
                    <td colSpan={7} className="px-4 py-6 text-center text-text-muted text-xs">
                      暂无成交
                    </td>
                  </tr>
                ) : (
                  trades.map((t, i) => {
                    const p = tradePerspective(t, address)
                    return (
                    <tr key={`${t.order_id}-${i}`} className="hover:bg-bg-hover">
                      <td className="px-4 py-2 border-t border-line font-mono">{t.order_id}</td>
                      <td className="px-4 py-2 border-t border-line">{t.symbol.replace('_', '/')}</td>
                      <td className={`px-4 py-2 border-t border-line ${p.side === 'buy' ? 'text-up' : 'text-down'}`}>
                        {p.side === 'buy' ? '买入' : '卖出'}
                      </td>
                      <td className="px-4 py-2 border-t border-line font-mono">{scalePairValue(t.symbol, t.price)}</td>
                      <td className="px-4 py-2 border-t border-line font-mono">{scalePairValue(t.symbol, t.amount)}</td>
                      <td className="px-4 py-2 border-t border-line">
                        {(() => {
                          // 此前直接渲染原始英文串，且高亮判断 `=== 'confirmed'` 恒不成立
                          //（后端旧值写作 sent）→ 已结算的也显示为进行中
                          const lab = settleLabel(t.settle_status)
                          const color =
                            lab.cls === 'green' ? 'text-up' : lab.cls === 'red' ? 'text-down' : 'text-orange'
                          return <span className={color}>{lab.text}</span>
                        })()}
                      </td>
                      <td className="px-4 py-2 border-t border-line font-mono text-[11px] text-text-muted">
                        {t.tx_hash ? `${t.tx_hash.slice(0, 12)}…` : '--'}
                      </td>
                    </tr>
                    )
                  })
                )}
              </tbody>
            </table>
          </div>

          <div className="mt-4 text-[11px] text-text-muted">
            地址: <span className="font-mono text-text-secondary">{address}</span> · 余额来自 Anubis RPC
            <button
              className="ml-2 text-text-muted hover:text-text-primary cursor-pointer bg-transparent border-none p-0 underline"
              onClick={() => void refreshBalances()}
            >
              刷新
            </button>
          </div>
        </>
      )}
    </div>
  )
}

/** Aleo 资产：records（ALEO/USDCX/测试币） */
function AleoAssets() {
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
  const usdcx = num(balances?.usdcx)
  // USDCX 为 USDT 计价测试稳定币，估值按 1:1
  const totalUsd = usdt + eth * ethPrice + aleo * ALEO_PRICE + usdcx

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
            <div className="bg-bg-secondary border border-line rounded-lg p-4">
              <div className="text-[11px] text-text-muted uppercase tracking-wide mb-1.5">USDCX 余额</div>
              <div className="text-[22px] font-bold">{formatNumber(usdcx, 6)}</div>
              <div className="text-[11px] text-text-secondary mt-1">折合 $ {formatNumber(usdcx, 2)}</div>
            </div>
          </div>

          <h3 className="text-[15px] text-text-secondary mt-6 mb-3 border-b border-line pb-2">资产明细（链上）</h3>
          <div className="bg-bg-secondary border border-line rounded-lg overflow-hidden">
            <table className="w-full border-collapse text-[13px]">
              <thead>
                <tr>
                  {['币种', '总余额', '公开', '隐私', '折合 USDT', '来源'].map((h) => (
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
                  <td className="px-4 py-2 border-t border-line font-mono text-text-muted">--</td>
                  <td className="px-4 py-2 border-t border-line font-mono">{formatNumber(usdt, 2)}</td>
                  <td className="px-4 py-2 border-t border-line font-mono">$ {formatNumber(usdt, 2)}</td>
                  <td className="px-4 py-2 border-t border-line text-[11px] text-text-muted">anubook_dex_p2 Token record</td>
                </tr>
                <tr className="hover:bg-bg-hover">
                  <td className="px-4 py-2 border-t border-line font-semibold">ETH</td>
                  <td className="px-4 py-2 border-t border-line font-mono">{formatNumber(eth, 4)}</td>
                  <td className="px-4 py-2 border-t border-line font-mono text-text-muted">--</td>
                  <td className="px-4 py-2 border-t border-line font-mono">{formatNumber(eth, 4)}</td>
                  <td className="px-4 py-2 border-t border-line font-mono">$ {formatNumber(eth * ethPrice, 2)}</td>
                  <td className="px-4 py-2 border-t border-line text-[11px] text-text-muted">anubook_dex_p2 Token record</td>
                </tr>
                <tr className="hover:bg-bg-hover">
                  <td className="px-4 py-2 border-t border-line font-semibold">ALEO</td>
                  <td className="px-4 py-2 border-t border-line font-mono">{formatNumber(aleo, 2)}</td>
                  <td className="px-4 py-2 border-t border-line font-mono">{balances?.aleoPublic !== '--' ? formatNumber(num(balances?.aleoPublic), 2) : '--'}</td>
                  <td className="px-4 py-2 border-t border-line font-mono">{privateBalance(balances?.aleo, balances?.aleoPublic)}</td>
                  <td className="px-4 py-2 border-t border-line font-mono">$ {formatNumber(aleo * ALEO_PRICE, 2)}</td>
                  <td className="px-4 py-2 border-t border-line text-[11px] text-text-muted">credits.aleo（公开 mapping + shielded）</td>
                </tr>
                <tr className="hover:bg-bg-hover">
                  <td className="px-4 py-2 border-t border-line font-semibold">USDCX</td>
                  <td className="px-4 py-2 border-t border-line font-mono">{formatNumber(usdcx, 6)}</td>
                  <td className="px-4 py-2 border-t border-line font-mono">{balances?.usdcxPublic !== '--' ? formatNumber(num(balances?.usdcxPublic), 6) : '--'}</td>
                  <td className="px-4 py-2 border-t border-line font-mono">{privateBalance(balances?.usdcx, balances?.usdcxPublic)}</td>
                  <td className="px-4 py-2 border-t border-line font-mono">$ {formatNumber(usdcx, 2)}</td>
                  <td className="px-4 py-2 border-t border-line text-[11px] text-text-muted">test_usdcx_stablecoin（balances mapping + shielded）</td>
                </tr>
              </tbody>
            </table>
          </div>
          <div className="mt-3 text-xs text-text-muted">
            地址: <span className="font-mono text-text-secondary">{walletAddress}</span> · 余额来自链上 record 实时聚合，ETH 估值用实时行情价，ALEO 估值用常量价（估），USDCX 估值按 1:1 USDT
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
                      {/* p4 真实币对（ALEO/USDCX）为 6 位最小单位，按币对精度换算为人类单位 */}
                      <td className="px-4 py-2 border-t border-line font-mono">{scalePairValue(t.symbol, t.price)}</td>
                      <td className="px-4 py-2 border-t border-line font-mono">{scalePairValue(t.symbol, t.amount)}</td>
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
