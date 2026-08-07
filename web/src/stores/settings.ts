import { create } from 'zustand'

export type TradingMode = 'spot' | 'perp'
export type Direction = 'long' | 'short'
export type OrderType = 'limit' | 'market' | 'stop-limit' | 'stop-market'
export type PrivacyMode = 'standard' | 'privacy' | 'darkpool'
export type MarginMode = 'cross' | 'isolated'

interface SettingsState {
  tradingMode: TradingMode
  direction: Direction
  orderType: OrderType
  privacyMode: PrivacyMode
  pair: string
  leverage: number
  marginMode: MarginMode
  simpleMode: boolean
  privacyOff: boolean
  setTradingMode: (m: TradingMode) => void
  setDirection: (d: Direction) => void
  setOrderType: (t: OrderType) => void
  setPrivacyMode: (m: PrivacyMode) => void
  setPair: (p: string) => void
  setLeverage: (l: number) => void
  setMarginMode: (m: MarginMode) => void
  toggleSimpleMode: () => void
  togglePrivacyOff: () => void
}

export const useSettings = create<SettingsState>()((set) => ({
  tradingMode: 'spot',
  direction: 'long',
  orderType: 'limit',
  privacyMode: 'standard',
  pair: 'BTC/USDT',
  leverage: 3,
  marginMode: 'cross',
  simpleMode: false,
  privacyOff: false,
  // 切换到现货时方向重置为买入（对齐原型 setMode）
  setTradingMode: (tradingMode) =>
    set((s) => ({
      tradingMode,
      direction: tradingMode === 'spot' ? 'long' : s.direction,
    })),
  setDirection: (direction) => set({ direction }),
  setOrderType: (orderType) => set({ orderType }),
  setPrivacyMode: (privacyMode) => set({ privacyMode }),
  setPair: (pair) => set({ pair }),
  setLeverage: (leverage) => set({ leverage }),
  setMarginMode: (marginMode) => set({ marginMode }),
  toggleSimpleMode: () => set((s) => ({ simpleMode: !s.simpleMode })),
  togglePrivacyOff: () => set((s) => ({ privacyOff: !s.privacyOff })),
}))
