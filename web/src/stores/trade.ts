import { create } from 'zustand'

// 下单表单状态（P1 mock；P2+ 接 WS/REST 后扩展为服务端驱动）
interface TradeState {
  price: string
  amount: string
  stopPrice: string
  tp: string
  sl: string
  setPrice: (v: string) => void
  setAmount: (v: string) => void
  setStopPrice: (v: string) => void
  setTp: (v: string) => void
  setSl: (v: string) => void
}

export const useTrade = create<TradeState>()((set) => ({
  price: '0.016', // ALEO/USDCX 默认价格（0.016 USDCX，p4 6 位精度 = 16000 微单位）；切交易对由 settings.setPair 重置
  amount: '1.0000',
  stopPrice: '',
  tp: '',
  sl: '',
  setPrice: (price) => set({ price }),
  setAmount: (amount) => set({ amount }),
  setStopPrice: (stopPrice) => set({ stopPrice }),
  setTp: (tp) => set({ tp }),
  setSl: (sl) => set({ sl }),
}))
