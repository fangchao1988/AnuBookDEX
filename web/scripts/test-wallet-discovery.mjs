// EIP-6963 多钱包发现测试（模拟 OKX + MetaMask 共存场景）
// 运行：node scripts/test-wallet-discovery.mjs
//
// 背景：多个钱包扩展都会注入 window.ethereum，谁最后注入谁赢（实测 OKX 抢到了
// MetaMask 的位置）。正确做法是 EIP-6963 枚举全部并让用户选。本测试固定该行为。

import { discoverEvmWallets, pickDefaultWallet, EVMWallet, WalletPickerRequired, evmConfigFor } from '../src/lib/wallet/evm.ts'

// ── 最小化浏览器环境 mock ──────────────────────────────
class MockProvider {
  constructor(rdns, name) {
    this.rdns = rdns
    this.name = name
    this.requests = []
  }
  async request({ method }) {
    this.requests.push(method)
    if (method === 'eth_requestAccounts' || method === 'eth_accounts') {
      return [this.rdns === 'io.metamask' ? '0xMETA' : '0xOKX']
    }
    if (method === 'eth_chainId') return '0x1' // 故意错误链，触发切链分支
    if (method === 'eth_getBalance') return '0x1bc16d674ec80000' // 2 DAI
    return null
  }
}

const listeners = new Map()
const mockWindow = {
  addEventListener: (t, cb) => listeners.set(t, [...(listeners.get(t) ?? []), cb]),
  removeEventListener: (t, cb) => listeners.set(t, (listeners.get(t) ?? []).filter((f) => f !== cb)),
  dispatchEvent: (e) => {
    // 模拟各扩展收到 requestProvider 后异步 announce
    if (e.type === 'eip6963:requestProvider') {
      const mm = new MockProvider('io.metamask', 'MetaMask')
      const okx = new MockProvider('com.okex.wallet', 'OKX Wallet')
      for (const [uuid, name, provider] of [
        ['uuid-mm', 'MetaMask', mm],
        ['uuid-okx', 'OKX Wallet', okx],
      ]) {
        setTimeout(() => {
          for (const cb of listeners.get('eip6963:announceProvider') ?? []) {
            cb({ type: 'eip6963:announceProvider', detail: { info: { uuid, name, icon: '', rdns: provider.rdns }, provider } })
          }
        }, 5)
      }
    }
    return true
  },
  setTimeout: (fn, ms) => setTimeout(fn, ms),
}

const store = new Map()
globalThis.window = mockWindow
globalThis.localStorage = {
  getItem: (k) => store.get(k) ?? null,
  setItem: (k, v) => store.set(k, String(v)),
  removeItem: (k) => store.delete(k),
}
globalThis.CustomEvent = class {
  constructor(type, init) {
    this.type = type
    Object.assign(this, init)
  }
}

// ── 断言 ──────────────────────────────────────────────
let failed = 0
const check = (name, cond, extra = '') => {
  console.log(`${cond ? '✓' : '✗'} ${name}${extra ? ' — ' + extra : ''}`)
  if (!cond) failed++
}

const wallets = await discoverEvmWallets(100)
check('发现 2 个钱包（而非仅 window.ethereum 的 1 个）', wallets.length === 2, `got ${wallets.length}`)
const rdnss = wallets.map((w) => w.info.rdns).sort()
check('包含 MetaMask 与 OKX', rdnss.join(',') === 'com.okex.wallet,io.metamask', rdnss.join(','))

// 多钱包且无保存偏好 -> 必须返回 null（要求用户选择），不能静默选中
check('多钱包时 pickDefaultWallet 返回 null（弹选择器）', pickDefaultWallet(wallets) === null)

// 单钱包 -> 自动选中
check('单钱包时自动选中', pickDefaultWallet([wallets[0]])?.info.rdns === wallets[0].info.rdns)

// 保存偏好后 -> 命中偏好
store.set('anubook_evm_wallet_rdns', 'io.metamask')
check('保存偏好后命中 MetaMask（不被 OKX 抢占）', pickDefaultWallet(wallets)?.info.rdns === 'io.metamask')

// ── 端到端：connect 流程（这正是用户遇到的场景）────────────────
store.clear() // 清空偏好，模拟首次访问

const w1 = new EVMWallet(evmConfigFor('testnet'))
let pickerErr = null
try {
  await w1.connect()
} catch (e) {
  pickerErr = e
}
check('首次连接（多钱包）抛 WalletPickerRequired 而非连错钱包', pickerErr instanceof WalletPickerRequired)
check('选择器携带 2 个候选', pickerErr?.wallets.length === 2)

// 用户选择 MetaMask
await w1.selectWallet('io.metamask')
const addr = await w1.connect()
check('选定 MetaMask 后连接成功', addr === '0xMETA', `got ${addr}`)
check('连接记录 rdns 供后续复用', w1.getWalletRdns() === 'io.metamask')
check('偏好已持久化', store.get('anubook_evm_wallet_rdns') === 'io.metamask')

// 第二个实例（模拟刷新页面）应直接命中偏好，不再弹选择器
const w2 = new EVMWallet(evmConfigFor('testnet'))
const addr2 = await w2.connect()
check('刷新后直接连 MetaMask（不再弹选择器）', addr2 === '0xMETA', `got ${addr2}`)

// ── DAI 余额查询（wei -> 人类单位）─────────────────────────
// 2 DAI = 2e18 wei = 0x1bc16d674ec80000
const bal = await w2.getBalance()
check('DAI 余额查询与换算正确（2.0）', bal === '2', `got ${bal}`)

// 余额格式边界：0、1 wei、大额
const cases = [
  ['0x0', '0'],
  ['0x1', '0.000000000000000001'],
  ['0xde0b6b3a7640000', '1'], // 1 DAI
]
let convOk = true
for (const [hex, want] of cases) {
  const got = await w2.getBalance()
  void got
  // 直接调用底层换算逻辑：通过 mock provider 返回值验证
  const inst = new EVMWallet(evmConfigFor('testnet'))
  inst.address = '0xMETA'
  inst.provider = { request: async () => hex }
  const r = await inst.getBalance()
  if (r !== want) {
    convOk = false
    console.log(`   ✗ ${hex} -> ${r}（期望 ${want}）`)
  }
}
check('余额换算边界正确（0 / 1 wei / 1 DAI）', convOk)

console.log(failed === 0 ? '\n全部通过' : `\n${failed} 项失败`)
process.exit(failed === 0 ? 0 : 1)
