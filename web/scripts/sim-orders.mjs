#!/usr/bin/env node
// Aleo 本地联调灌单脚本：向引擎 POST /order 灌双向限价单，驱动 撮合 -> WS 行情链路
// 用法：node web/scripts/sim-orders.mjs [--symbol ETH_USDT] [--interval 300] [--duration 30] [--url http://localhost:9000]
// 注意：与合约 token 注册表一致（contracts/leo/src/main.leo: 1=ETH, 2=USDT）

const args = process.argv.slice(2)
const opt = (name, dflt) => {
  const i = args.indexOf(name)
  return i >= 0 ? args[i + 1] : dflt
}

const SYMBOL = opt('--symbol', 'ETH_USDT')
const INTERVAL = Number(opt('--interval', '300')) // ms
const DURATION = Number(opt('--duration', '30')) // 秒，0=无限
const URL = opt('--url', 'http://localhost:9000')
const BASE_PRICE = SYMBOL.startsWith('BTC') ? 65000 : 1800

const TOKENS = { ETH: 1, USDT: 2, BTC: 3 }
const [base, quote] = SYMBOL.split('_')
const BASE_TOKEN = TOKENS[base] ?? 0
const QUOTE_TOKEN = TOKENS[quote] ?? 0

let orderId = Date.now()
let price = BASE_PRICE
let buy = true
const t0 = Date.now()

async function sendOrder(side, price, amount) {
  orderId += 1
  const body = {
    order_id: orderId,
    symbol: SYMBOL, // 委托记录交易对（与前端 OrderForm 一致）
    side, // 0=buy, 1=sell
    price: price.toFixed(4),
    amount: amount.toFixed(6),
    base_token: BASE_TOKEN,
    quote_token: QUOTE_TOKEN,
    deadline: Math.floor(Date.now() / 1000) + 3600,
    trader: 'aleo1dev-simulator',
    // 本地联调占位密文（非空即可过 OrderPool 校验；生产环境由钱包 place_order 生成）
    ciphertext: 'ciphertext1dev-sim-placeholder',
  }
  const res = await fetch(`${URL}/order`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    console.error('order rejected:', orderId, await res.text())
    return false
  }
  return true
}

console.log(`[sim] ${SYMBOL} base=${BASE_TOKEN} quote=${QUOTE_TOKEN} every ${INTERVAL}ms for ${DURATION || '∞'}s`)

const timer = setInterval(async () => {
  price += (Math.random() - 0.5) * 30
  const spread = 1.5 + Math.random() * 6
  const p = buy ? price - spread : price + spread
  const amt = 0.05 + Math.random() * 0.5
  await sendOrder(buy ? 0 : 1, p, amt)
  buy = !buy
}, INTERVAL)

const done = () => {
  clearInterval(timer)
  console.log('[sim] stopped')
  process.exit(0)
}
if (DURATION > 0) setTimeout(done, DURATION * 1000)
process.on('SIGINT', done)
