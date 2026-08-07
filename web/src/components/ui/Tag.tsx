import type { ReactNode } from 'react'

export type TagVariant = 'green' | 'red' | 'blue' | 'purple' | 'orange'

const VARIANTS: Record<TagVariant, string> = {
  green: 'bg-up-bg text-up',
  red: 'bg-down-bg text-down',
  blue: 'bg-blue-bg text-blue',
  purple: 'bg-ai-glow text-purple',
  orange: 'bg-orange-bg text-orange',
}

export function Tag({
  variant,
  children,
  className = '',
}: {
  variant: TagVariant
  children: ReactNode
  className?: string
}) {
  return (
    <span className={`inline-block px-2 py-0.5 rounded text-[11px] font-semibold ${VARIANTS[variant]} ${className}`}>
      {children}
    </span>
  )
}
