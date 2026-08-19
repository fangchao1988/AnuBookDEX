// 钱包抽象（docs/前端实现方案.md §7）：链无关 UI 依赖此接口，具体链实现注入。
// P3 先实现 Aleo 侧（Leo Wallet SDK / DevWallet 降级），Anubis(EVM) 适配器后续补充。

export interface PlaceOrderParams {
  symbol: string // 交易对（ALEO_USDCX / ETH_USDT），决定 p2/p4 下单构造与精度缩放
  orderId: number
  side: 0 | 1 // 0=buy, 1=sell
  price: string
  amount: string
  baseToken: number
  quoteToken: number
  deadline: number // 秒
  operator: string // Order record owner（引擎 operator 地址）
  // p6 下单路径：standard=公开余额托管（place_order_*_public，无 record 输入）；
  // privacy=record 托管（place_order_*_private，uid 精确定位 record）。缺省 privacy
  mode?: 'standard' | 'privacy'
}

export interface PlacedOrder {
  txId: string
  ciphertext: string // Order record ciphertext（p2 POST /order 用；p4 引擎从 tx 提取，可为空）
}

export interface WalletBalances {
  aleo: string // ALEO 总余额（public + shielded）
  usdt: string // anubook_dex_p2 Token record（测试币）
  base: string // base 币种（ETH）Token record
  usdcx: string // USDCX 总余额（public + shielded，6 位最小单位换算后）
  // p6 公开余额（标准下单用：公开余额 -> transfer_public 托管，无需 record/凭证）
  aleoPublic: string // 公开 ALEO（credits.aleo account mapping）
  usdcxPublic: string // 公开 USDCX（balances mapping）
}

// 隐私余额 = 总余额 - 公开余额：钱包层只聚合总量与公开量，
// 隐私（record 托管）余额由差值导出，供隐私/暗池下单模式展示。
// 返回 '--' 表示无可展示余额（0 或数据缺失）。
export function privateBalance(total: string | undefined, pub: string | undefined): string {
  const toNum = (v?: string) => (v && v !== '--' ? Number(String(v).replace(/,/g, '')) : 0)
  const priv = toNum(total) - toNum(pub)
  return priv > 0 ? String(Math.round(priv * 1e6) / 1e6) : '--'
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
  // 铸测试币（anubook_dex_p2.aleo mint；Token 归执行者，dev 模拟）
  mintToken(tokenId: number, amount: number): Promise<void>
  // 部署合约（Shield executeDeployment，用户 ALEO 付部署费；dev 模拟返回占位 txId）
  deployProgram(): Promise<string>
  // 领取 USDCX 合规凭证（test_usdcx_stablecoin get_credentials；凭证归签名者，dev 模拟）
  getCredentials(): Promise<void>
}

// 引擎 operator 地址（Order record owner；生产由链配置提供，本地联调占位）
export const OPERATOR_ADDRESS = 'aleo1operator-placeholder'
