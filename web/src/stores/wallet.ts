import { create } from 'zustand'
import { createWallet } from '../lib/wallet/aleo'
import type { AleoWallet, WalletBalances } from '../lib/wallet/types'

// 钱包状态（P3）：连接即登录（地址身份）；签名挑战验签待后端 Aleo 验签能力（无 Go SDK，后续接）
interface WalletState {
  wallet: AleoWallet
  address: string | null
  balances: WalletBalances | null
  error: string
  connect: () => Promise<void>
  disconnect: () => void
  refreshBalances: (baseSymbol: string) => Promise<void>
}

const wallet = createWallet()

export const useWallet = create<WalletState>()((set, get) => ({
  wallet,
  address: null,
  balances: null,
  error: '',

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

  refreshBalances: async (baseSymbol: string) => {
    try {
      const balances = await wallet.getBalances(baseSymbol)
      set({ balances })
    } catch {
      // 余额获取失败不阻断 UI
    }
  },
}))
