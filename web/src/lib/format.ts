// 数字格式化：千分位 + 精度。价格/数量一律以字符串往返，禁止 float64 计算
// （docs/前端实现方案.md §8，对齐后端 shopspring/decimal 37 位精度语义）

export function formatNumber(v: string | number, decimals = 2): string {
  const n = Number(v)
  if (!Number.isFinite(n)) return '--'
  return n.toLocaleString('en-US', {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  })
}

export function formatPrice(v: string | number, decimals = 2): string {
  return formatNumber(v, decimals)
}

export function formatAmount(v: string | number, decimals = 4): string {
  return formatNumber(v, decimals)
}

export function formatUsd(v: string | number, decimals = 2): string {
  return '$ ' + formatNumber(v, decimals)
}

export function truncateAddress(addr: string): string {
  if (addr.length <= 12) return addr
  return `${addr.slice(0, 6)}...${addr.slice(-4)}`
}

export function timeStr(ts: number): string {
  const d = new Date(ts)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

export function dateTimeStr(ts: number): string {
  const d = new Date(ts)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
