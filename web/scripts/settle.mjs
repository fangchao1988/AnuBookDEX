#!/usr/bin/env node
// 链上结算（settle）执行脚本：用 @provablehq/sdk 广播 settle transition。
//
// 背景：leo CLI 的 execute 受 InvalidCharacter bug 影响不可用；settle 需要 operator
// 私钥（Order record owner 是 operator，用户钱包无权 spend），本脚本在带
// ALEO_PRIVATE_KEY 的终端运行。
//
// 用法：
//   ALEO_PRIVATE_KEY=... node web/scripts/settle.mjs \
//     --maker-tx at1... --taker-tx at1... --price 1700 --amount 1
//   （--dry-run 不广播；--program 指定本地产物路径）
//
// 说明：maker = 买单、taker = 卖单（settle 合约断言）；price/amount 为撮合成交价量。
import { Account, ProgramManager, Program } from '@provablehq/sdk'
import { readFileSync } from 'node:fs'

const arg = (name, dflt) => {
  const i = process.argv.indexOf(name)
  return i >= 0 ? process.argv[i + 1] : dflt
}
const has = (name) => process.argv.includes(name)

const MAKER_TX = arg('--maker-tx', '')
const TAKER_TX = arg('--taker-tx', '')
const PRICE = Number(arg('--price', '0'))
const AMOUNT = Number(arg('--amount', '0'))
const DRY_RUN = has('--dry-run')
const ENDPOINT = arg('--endpoint', 'https://api.explorer.provable.com/v1')
const PROGRAM_PATH = arg(
  '--program',
  '/Users/fangchao/GolandProjects/AnuBookDEX/contracts/leo/build/anubook_dex_p2/anubook_dex_p2.aleo',
)

if (!MAKER_TX || !TAKER_TX || !PRICE || !AMOUNT) {
  console.error('用法: ALEO_PRIVATE_KEY=... node web/scripts/settle.mjs --maker-tx at1... --taker-tx at1... --price 1700 --amount 1 [--dry-run]')
  process.exit(1)
}
const PRIV = process.env.ALEO_PRIVATE_KEY
if (!PRIV) {
  console.error('缺少 ALEO_PRIVATE_KEY 环境变量（operator 私钥，settle 需 spend Order record）')
  process.exit(1)
}

// 从链上交易提取 Order record ciphertext（record1 新格式 / ciphertext1 旧格式兼容）
async function extractRecordCiphertext(txId) {
  const res = await fetch(`${ENDPOINT}/testnet/transaction/${txId}`)
  if (!res.ok) throw new Error(`查询交易失败: HTTP ${res.status}`)
  const tx = await res.json()
  for (const t of tx?.execution?.transitions ?? []) {
    for (const o of t?.outputs ?? []) {
      if (o.type === 'record' && typeof o.value === 'string') {
        return o.value
      }
    }
  }
  throw new Error(`交易 ${txId} 无 record 输出`)
}

console.log('📦 加载合约产物:', PROGRAM_PATH)
const program = Program.fromString(readFileSync(PROGRAM_PATH, 'utf8'))
console.log('  程序:', String(program.id()))

console.log('🔍 提取订单 record 密文...')
const makerCT = await extractRecordCiphertext(MAKER_TX)
const takerCT = await extractRecordCiphertext(TAKER_TX)
console.log('  maker:', MAKER_TX, '->', makerCT.slice(0, 30) + '…')
console.log('  taker:', TAKER_TX, '->', takerCT.slice(0, 30) + '…')

const account = new Account({ privateKey: PRIV })
const pm = new ProgramManager(ENDPOINT)
pm.setAccount(account)
console.log('  结算账户:', account.address().to_string())

const inputs = [makerCT, takerCT, `${PRICE}u64`, `${AMOUNT}u64`]
const execOpts = {
  programName: 'anubook_dex_p2.aleo',
  program,
  functionName: 'settle',
  priorityFee: 0, // 加急费（base fee 自动计算）；费用不足时调整
  privateFee: false,
  inputs,
}
console.log('⚙️  执行 settle:', `settle(${PRICE}u64, ${AMOUNT}u64)`, DRY_RUN ? '（dry-run，不广播）' : '（广播）')

if (DRY_RUN) {
  // dry-run：构建交易但不广播
  const tx = await pm.buildExecutionTransaction(execOpts)
  console.log('✅ dry-run 构建成功（未广播）')
  console.log('  交易:', tx.to_string().slice(0, 80) + '…')
} else {
  const txId = await pm.execute(execOpts)
  console.log('🎉 settle 已广播, txId:', txId)
  console.log('  区块浏览器:', `${ENDPOINT.replace('/v1', '')}/testnet/transaction/${txId}`)
}
