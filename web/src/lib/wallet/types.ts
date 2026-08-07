// 钱包抽象（docs/前端实现方案.md §7）：链无关 UI 依赖此接口，具体链实现注入。
// P3 先实现 Aleo 侧（Leo Wallet SDK / DevWallet 降级），Anubis(EVM) 适配器后续补充。

export interface PlaceOrderParams {
  orderId: number
  side: 0 | 1 // 0=buy, 1=sell
  price: string
  amount: string
  baseToken: number
  quoteToken: number
  deadline: number // 秒
  operator: string // Order record owner（引擎 operator 地址）
}

export interface PlacedOrder {
  txId: string
  ciphertext: string // Order record ciphertext（POST /order 用）
}

export interface WalletBalances {
  usdt: string
  base: string // base 币种（如 ETH）余额
}

export interface AleoWallet {
  readonly kind: 'shield' | 'leo' | 'dev'
  isConnected(): boolean
  getAddress(): string | null
  connect(): Promise<string> // 返回地址
  disconnect(): void
  // 链上余额（requestRecords 聚合；dev 返回模拟值）
  getBalances(baseSymbol: string): Promise<WalletBalances>
  // place_order：锁仓 Token record -> transition -> txId -> Order record ciphertext
  placeOrder(params: PlaceOrderParams): Promise<PlacedOrder>
}

// 引擎 operator 地址（Order record owner；生产由链配置提供，本地联调占位）
export const OPERATOR_ADDRESS = 'aleo1operator-placeholder'
