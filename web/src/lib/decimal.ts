// decimal.js 封装：对齐后端 shopspring/decimal（除法精度 37），禁止 Number 运算价格
import Decimal from 'decimal.js'

Decimal.set({ precision: 37 })

export function toDecimal(v: string | number): Decimal {
  return new Decimal(v)
}

export function decimalMul(a: string | number, b: string | number): Decimal {
  return new Decimal(a).mul(new Decimal(b))
}

export function decimalDiv(a: string | number, b: string | number): Decimal {
  return new Decimal(a).div(new Decimal(b))
}

export function decimalSub(a: string | number, b: string | number): Decimal {
  return new Decimal(a).sub(new Decimal(b))
}
