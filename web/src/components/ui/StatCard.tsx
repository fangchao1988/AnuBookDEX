import type { ReactNode } from 'react'

// 统计卡片：对齐原型 .card（title + value + sub）
export function StatCard({
  title,
  value,
  sub,
  valueClassName = '',
  className = '',
  children,
}: {
  title: ReactNode
  value?: ReactNode
  sub?: ReactNode
  valueClassName?: string
  className?: string
  children?: ReactNode
}) {
  return (
    <div className={`bg-bg-secondary border border-line rounded-lg p-4 ${className}`}>
      <div className="text-[11px] text-text-muted uppercase tracking-wide mb-1.5">{title}</div>
      {value !== undefined && <div className={`text-[22px] font-bold leading-tight ${valueClassName}`}>{value}</div>}
      {sub !== undefined && <div className="text-[11px] text-text-secondary mt-1">{sub}</div>}
      {children}
    </div>
  )
}
