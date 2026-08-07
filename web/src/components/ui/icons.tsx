import type { ReactNode } from 'react'

// 图标：内联 SVG（currentColor），路径与原型一致
function Svg({ children, className = 'w-5 h-5' }: { children: ReactNode; className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
    >
      {children}
    </svg>
  )
}

export const IconChart = ({ className }: { className?: string }) => (
  <Svg className={className}>
    <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
  </Svg>
)

export const IconAssets = ({ className }: { className?: string }) => (
  <Svg className={className}>
    <rect x="2" y="4" width="20" height="16" rx="2" />
    <line x1="12" y1="1" x2="12" y2="23" />
  </Svg>
)

export const IconLp = ({ className }: { className?: string }) => (
  <Svg className={className}>
    <circle cx="12" cy="12" r="10" />
    <path d="M8 12l2 2 4-4" />
  </Svg>
)

export const IconStrategy = ({ className }: { className?: string }) => (
  <Svg className={className}>
    <path d="M12 2a10 10 0 1010 10" />
    <path d="M12 6v6l4 2" />
  </Svg>
)

export const IconLock = ({ className }: { className?: string }) => (
  <Svg className={className}>
    <rect x="3" y="11" width="18" height="11" rx="2" />
    <path d="M7 11V7a5 5 0 0110 0v4" />
  </Svg>
)

export const IconBell = ({ className }: { className?: string }) => (
  <Svg className={className}>
    <path d="M18 8A6 6 0 006 8c0 7-3 9-3 9h18s-3-2-3-9" />
    <path d="M13.73 21a2 2 0 01-3.46 0" />
  </Svg>
)

export const IconBolt = ({ className }: { className?: string }) => (
  <Svg className={className}>
    <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
  </Svg>
)

export const IconSettings = ({ className }: { className?: string }) => (
  <Svg className={className}>
    <circle cx="12" cy="12" r="3" />
    <path d="M12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42" />
  </Svg>
)

export const IconChevronDown = ({ className }: { className?: string }) => (
  <Svg className={className}>
    <polyline points="6 9 12 15 18 9" />
  </Svg>
)

export const IconClose = ({ className }: { className?: string }) => (
  <Svg className={className}>
    <line x1="18" y1="6" x2="6" y2="18" />
    <line x1="6" y1="6" x2="18" y2="18" />
  </Svg>
)

export const IconShield = ({ className }: { className?: string }) => (
  <Svg className={className}>
    <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
  </Svg>
)

export const IconEyeOff = ({ className }: { className?: string }) => (
  <Svg className={className}>
    <circle cx="12" cy="12" r="10" />
    <line x1="8" y1="12" x2="16" y2="12" />
  </Svg>
)

export const IconStar = ({ className }: { className?: string }) => (
  <svg viewBox="0 0 24 24" fill="currentColor" className={className}>
    <path d="M12 2l2.9 6.6 7.1.6-5.4 4.7 1.6 7-6.2-3.7L5.8 21l1.6-7L2 9.2l7.1-.6L12 2z" />
  </svg>
)
