import { create } from 'zustand'
import { createWallet } from '../lib/wallet/aleo'
import type { AleoWallet, WalletBalances } from '../lib/wallet/types'

// 钱包状态（P3）：连接即登录（地址身份）；签名挑战验签待后端 Aleo 验签能力（无 Go SDK，后续接）
interface WalletState {
  wallet: AleoWallet
  address: string | null
  balances: WalletBalances | null
  error: string
  restore: () => Promise<void>
  connect: () => Promise<void>
  disconnect: () => void
  clearError: () => void
  refreshBalances: (baseSymbol: string) => Promise<void>
  mintToken: (tokenId: number, amount: number) => Promise<void>
  deployProgram: () => Promise<string>
}

const wallet = createWallet()

export const useWallet = create<WalletState>()((set, get) => ({
  wallet,
  address: null,
  balances: null,
  error: '',

  // 初始化时自动恢复上次会话（刷新页面后钱包保持连接）。
  // 钱包扩展已授权时 connect 不会重复弹框；未解锁/未授权则静默失败（保持未连接态）
  restore: async () => {
    const stored = localStorage.getItem('aleo_address')
    if (!stored) return
    try {
      const address = await wallet.connect()
      if (address === stored) {
        set({ address })
        await get().refreshBalances('ETH')
      }
    } catch {
      // 静默失败：钱包未解锁/未授权，保持未连接态（用户可手动连接）
    }
  },

  connect: async () => {
    set({ error: '' })
    try {
      const address = await wallet.connect()
      // 会话身份（OrderForm trader / 委托查询）
      localStorage.setItem('aleo_address', address)
      set({ address })
      await get().refreshBalances('ETH')
    } catch (e) {
      set({ error: e instanceof Error ? e.message : String(e) })
      throw e
    }
  },

  disconnect: () => {
    wallet.disconnect()
    localStorage.removeItem('aleo_address')
    set({ address: null, balances: null })
  },

  clearError: () => set({ error: '' }),

  refreshBalances: async (baseSymbol: string) => {
    try {
      const balances = await wallet.getBalances(baseSymbol)
      set({ balances })
    } catch {
      // 余额获取失败不阻断 UI
    }
  },

  mintToken: async (tokenId: number, amount: number) => {
    await wallet.mintToken(tokenId, amount)
    // 铸币后刷新余额
    await get().refreshBalances('ETH')
  },

  deployProgram: async () => {
    const txId = await wallet.deployProgram()
    // 部署后刷新余额（部署费已扣）
    await get().refreshBalances('ETH')
    return txId
  },
}))
