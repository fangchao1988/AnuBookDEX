import { useCallback, useEffect, useState } from 'react'
import { useChain } from '../../stores/chain'
import { fetchChainTokens, formatToken, parseToken, type TokenInfo } from '../../lib/api/anubis'

/** uint256 最大值（无限授权） */
const MAX_UINT256 = (1n << 256n) - 1n

/**
 * Anubis 金库面板：真实代币入金/提现。
 *
 * 结算模型（Settlement v3）：下单前需先把代币存入合约金库，
 * 撮合时合约在双方金库余额间真实划转。
 * 入金流程：approve（授权合约花费）→ deposit（转入金库）。
 *
 * DAI 是 gas 代币，无需入金（链上结算 gas 直接扣）。
 */
export function AnubisVaultPanel() {
  const address = useChain((s) => s.address)
  const wallet = useChain((s) => s.evmWallet)
  const [settleContract, setSettleContract] = useState('')

  const [tokens, setTokens] = useState<TokenInfo[]>([])
  const [walletBal, setWalletBal] = useState<Record<string, bigint>>({})
  const [vaultBal, setVaultBal] = useState<Record<string, bigint>>({})
  const [amounts, setAmounts] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState<string>('')
  const [msg, setMsg] = useState<{ text: string; ok: boolean } | null>(null)
  const [loadErr, setLoadErr] = useState('')

  // 拉取代币清单（去重：同一 USDT 会出现在多个币对）
  useEffect(() => {
    let alive = true
    fetchChainTokens()
      .then((data) => {
        if (!alive) return
        // 去重：同一 USDT 出现在多个币对
        const seen = new Map<string, TokenInfo>()
        for (const p of data.pairs) {
          for (const t of [p.base, p.quote]) seen.set(t.address.toLowerCase(), t)
        }
        setTokens([...seen.values()])
        setSettleContract(data.settlement)
        setLoadErr('')
      })
      .catch((e) => alive && setLoadErr(e instanceof Error ? e.message : String(e)))
    return () => {
      alive = false
    }
  }, [])

  // 读余额（钱包 + 金库）
  const refresh = useCallback(async () => {
    if (!address || tokens.length === 0) return
    const w: Record<string, bigint> = {}
    const v: Record<string, bigint> = {}
    for (const t of tokens) {
      try {
        w[t.address] = await wallet.erc20Balance(t.address)
        v[t.address] = settleContract ? await wallet.vaultBalance(settleContract, t.address) : 0n
      } catch {
        w[t.address] = 0n
        v[t.address] = 0n
      }
    }
    setWalletBal(w)
    setVaultBal(v)
  }, [address, tokens, wallet, settleContract])

  useEffect(() => {
    void refresh()
    const timer = window.setInterval(() => void refresh(), 10000)
    return () => window.clearInterval(timer)
  }, [refresh])

  /** 入金：必要时先 approve，再 deposit */
  const deposit = async (t: TokenInfo) => {
    if (!settleContract) {
      setMsg({ text: '未配置结算合约地址（VITE_ANUBIS_SETTLEMENT）', ok: false })
      return
    }
    const amt = parseToken(amounts[t.address] ?? '', t.decimals)
    if (amt <= 0n) {
      setMsg({ text: '请输入有效数量', ok: false })
      return
    }
    setBusy(t.address)
    setMsg(null)
    try {
      // 一次性无限授权：后续入金只需 1 笔交易（否则每次都要 approve + deposit 两笔）
      const allowance = await wallet.erc20Allowance(t.address, settleContract)
      if (allowance < amt) {
        setMsg({ text: `授权 ${t.symbol} 中…（钱包确认，仅首次需要）`, ok: true })
        await wallet.erc20Approve(t.address, settleContract, MAX_UINT256)
      }
      setMsg({ text: `入金 ${t.symbol} 中…（钱包确认）`, ok: true })
      await wallet.vaultDeposit(settleContract, t.address, amt)
      setMsg({ text: `${t.symbol} 入金已提交，等待链上确认`, ok: true })
      setTimeout(() => void refresh(), 4000)
    } catch (e) {
      setMsg({ text: e instanceof Error ? e.message : String(e), ok: false })
    } finally {
      setBusy('')
    }
  }

  /** 提现：从金库取回钱包 */
  const withdraw = async (t: TokenInfo) => {
    if (!settleContract) return
    const amt = parseToken(amounts[t.address] ?? '', t.decimals)
    if (amt <= 0n) {
      setMsg({ text: '请输入有效数量', ok: false })
      return
    }
    setBusy(t.address)
    setMsg(null)
    try {
      await wallet.vaultWithdraw(settleContract, t.address, amt)
      setMsg({ text: `${t.symbol} 提现已提交，等待链上确认`, ok: true })
      setTimeout(() => void refresh(), 4000)
    } catch (e) {
      setMsg({ text: e instanceof Error ? e.message : String(e), ok: false })
    } finally {
      setBusy('')
    }
  }

  if (!address) {
    return <div className="p-4 text-center text-text-muted text-[13px]">连接 EVM 钱包后管理金库</div>
  }
  if (loadErr) {
    return <div className="p-4 text-center text-down text-[12px]">代币清单加载失败：{loadErr}</div>
  }
  if (tokens.length === 0) {
    return <div className="p-4 text-center text-text-muted text-[13px]">该链未配置交易币对</div>
  }

  return (
    <div className="p-3">
      <div className="text-[11px] text-text-muted mb-2 leading-relaxed">
        下单前需将代币存入结算金库，撮合时合约在双方金库间真实划转。
        首次入金需先授权（approve）。
      </div>

      {tokens.map((t) => (
        <div key={t.address} className="mb-2.5 p-2 bg-bg-tertiary border border-line rounded-md">
          <div className="flex justify-between text-[11px] mb-1">
            <span className="font-semibold text-text-primary">{t.symbol}</span>
            <span className="text-text-muted font-mono text-[10px]">
              钱包 {formatToken(walletBal[t.address] ?? 0n, t.decimals)} · 金库{' '}
              {formatToken(vaultBal[t.address] ?? 0n, t.decimals)}
            </span>
          </div>
          <div className="flex gap-1.5">
            <input
              type="text"
              value={amounts[t.address] ?? ''}
              onChange={(e) => setAmounts((a) => ({ ...a, [t.address]: e.target.value }))}
              placeholder="数量"
              className="flex-1 bg-bg-secondary border border-line text-text-primary px-1.5 py-1 rounded text-[11px] font-mono focus:border-blue focus:outline-none"
            />
            <button
              className="px-2.5 py-1 border border-up/40 rounded bg-up/5 text-up cursor-pointer text-[10px] hover:bg-up/10 disabled:opacity-50"
              disabled={busy === t.address}
              onClick={() => void deposit(t)}
            >
              {busy === t.address ? '处理中…' : '入金'}
            </button>
            <button
              className="px-2.5 py-1 border border-line rounded bg-bg-secondary text-text-secondary cursor-pointer text-[10px] hover:text-text-primary disabled:opacity-50"
              disabled={busy === t.address}
              onClick={() => void withdraw(t)}
            >
              提现
            </button>
          </div>
        </div>
      ))}

      {msg && (
        <div className={`mt-1 text-[10px] break-all ${msg.ok ? 'text-up' : 'text-down'}`}>{msg.text}</div>
      )}
    </div>
  )
}
