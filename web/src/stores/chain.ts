import { create } from 'zustand'
import {
  EVMWallet,
  WalletPickerRequired,
  evmConfigFor,
  type Eip6963ProviderDetail,
  type EVMChainConfig,
} from '../lib/wallet/evm'
import { useWallet } from './wallet'
import { wsClient } from '../lib/ws/client'

// 链选择（双链：Aleo 隐私 DEX / Anubis EVM DEX）
// - Aleo：Leo 记录隐私 + 钱包端 place_order（web/src/lib/wallet/aleo.ts）
// - Anubis：链下订单池 + ECIES 隐私载荷 + Settlement v2 上链结算
//
// 链切换会重置钱包连接状态（两套钱包互不相通）。

export type ChainKind = 'aleo' | 'anubis'

interface ChainState {
  chain: ChainKind
  evmNetwork: 'testnet' | 'mainnet'
  evmConfig: EVMChainConfig
  evmWallet: EVMWallet
  address: string | null // 当前链的钱包地址
  balances: { aleo: string; usdt: string; base: string; usdcx: string; aleoPublic: string; usdcxPublic: string } | null
  evmBalance: string // Anubis 原生 DAI 余额
  error: string
  connecting: boolean
  /** 多钱包待选（EIP-6963）：非空时 UI 弹钱包选择器 */
  walletChoices: Eip6963ProviderDetail[] | null

  setChain: (c: ChainKind) => Promise<void>
  setEvmNetwork: (n: 'testnet' | 'mainnet') => void
  connect: () => Promise<void>
  connectWithWallet: (rdns: string) => Promise<void>
  cancelWalletPicker: () => void
  disconnect: () => void
  restore: () => Promise<void>
  refreshBalances: (baseSymbol?: string) => Promise<void>
  clearError: () => void
}

const STORAGE_CHAIN = 'anubook_chain'
const STORAGE_EVM_NET = 'anubook_evm_network'

function readChain(): ChainKind {
  if (typeof localStorage === 'undefined') return 'aleo'
  return localStorage.getItem(STORAGE_CHAIN) === 'anubis' ? 'anubis' : 'aleo'
}

function readEvmNetwork(): 'testnet' | 'mainnet' {
  if (typeof localStorage === 'undefined') return 'testnet'
  return localStorage.getItem(STORAGE_EVM_NET) === 'mainnet' ? 'mainnet' : 'testnet'
}

const initialNetwork = readEvmNetwork()
// 首屏按持久化的链设置行情来源（避免 Anubis 链拿 Aleo 引擎的行情）
const initialChain = readChain()
wsClient.setPath(initialChain === 'anubis' ? '/anubis/ws' : '/ws')

export const useChain = create<ChainState>()((set, get) => ({
  chain: readChain(),
  evmNetwork: initialNetwork,
  evmConfig: evmConfigFor(initialNetwork),
  evmWallet: new EVMWallet(evmConfigFor(initialNetwork)),
  address: null,
  balances: null,
  evmBalance: '0',
  error: '',
  connecting: false,
  walletChoices: null,

  setChain: async (c) => {
    if (get().chain === c) return
    localStorage.setItem(STORAGE_CHAIN, c)
    // 双链引擎独立部署：行情 WS 也要切到对应引擎
    wsClient.setPath(c === 'anubis' ? '/anubis/ws' : '/ws')
    // 切链即断开当前钱包（Aleo 与 EVM 钱包互不相通）
    set({ chain: c, address: null, balances: null, evmBalance: '0', error: '' })
  },

  setEvmNetwork: (n) => {
    localStorage.setItem(STORAGE_EVM_NET, n)
    const cfg = evmConfigFor(n)
    const w = get().evmWallet
    w.disconnect()
    set({ evmNetwork: n, evmConfig: cfg, evmWallet: new EVMWallet(cfg), address: null, evmBalance: '0', error: '' })
  },

  connect: async () => {
    set({ error: '', connecting: true })
    try {
      if (get().chain === 'anubis') {
        const addr = await get().evmWallet.connect()
        set({ address: addr, walletChoices: null })
        await get().refreshBalances()
      } else {
        await useWallet.getState().connect()
        set({ address: useWallet.getState().address })
      }
    } catch (e) {
      // 多钱包共存：转成选择器状态而非报错（OKX 与 MetaMask 同时注入时
      // window.ethereum 存在注入竞态，必须让用户明确选一个）
      if (e instanceof WalletPickerRequired) {
        set({ walletChoices: e.wallets, error: '' })
        return
      }
      set({ error: e instanceof Error ? e.message : String(e) })
      throw e
    } finally {
      set({ connecting: false })
    }
  },

  /** 用户选定钱包后连接 */
  connectWithWallet: async (rdns: string) => {
    set({ error: '', connecting: true, walletChoices: null })
    try {
      await get().evmWallet.selectWallet(rdns)
      const addr = await get().evmWallet.connect()
      set({ address: addr })
      await get().refreshBalances()
    } catch (e) {
      if (e instanceof WalletPickerRequired) {
        set({ walletChoices: e.wallets })
        return
      }
      set({ error: e instanceof Error ? e.message : String(e) })
      throw e
    } finally {
      set({ connecting: false })
    }
  },

  cancelWalletPicker: () => set({ walletChoices: null }),

  disconnect: () => {
    if (get().chain === 'anubis') {
      get().evmWallet.disconnect()
    } else {
      // Aleo 钱包在独立 store 中同步断开（无异步依赖）
      useWallet.getState().disconnect()
    }
    set({ address: null, balances: null, evmBalance: '0' })
  },

  // 恢复上次会话：已授权的 MetaMask 静默恢复（不弹框）
  restore: async () => {
    if (get().chain !== 'anubis') {
      await useWallet.getState().restore()
      set({ address: useWallet.getState().address })
      return
    }
    try {
      const w = get().evmWallet
      if (await w.hasAuthorizedAccount()) {
        set({ address: w.getAddress() })
        await get().refreshBalances()
      }
    } catch {
      // 静默失败：未授权保持未连接态
    }
  },

  refreshBalances: async (baseSymbol = 'ETH') => {
    if (get().chain === 'anubis') {
      try {
        const b = await get().evmWallet.getBalance()
        set({ evmBalance: b })
      } catch {
        set({ evmBalance: '0' })
      }
      return
    }
    await useWallet.getState().refreshBalances(baseSymbol)
    set({ balances: useWallet.getState().balances, address: useWallet.getState().address })
  },

  clearError: () => set({ error: '' }),
}))
