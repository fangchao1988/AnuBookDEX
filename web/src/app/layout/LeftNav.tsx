import { NavLink } from 'react-router-dom'
import { useUi } from '../../stores/ui'
import {
  IconAssets,
  IconBell,
  IconChart,
  IconLock,
  IconLp,
  IconSettings,
  IconStrategy,
} from '../../components/ui/icons'

const NAV_ITEMS: { view: string; to: string; label: string; icon: (p: { className?: string }) => JSX.Element; dot?: boolean }[] = [
  { view: 'trade', to: '/trade/BTCUSDT', label: '交易', icon: IconChart },
  { view: 'assets', to: '/assets', label: '资产', icon: IconAssets },
  { view: 'lp', to: '/lp', label: '流动性', icon: IconLp },
  { view: 'strategy', to: '/strategy', label: 'AI策略', icon: IconStrategy },
  { view: 'darkpool', to: '/darkpool', label: '暗池', icon: IconLock },
  { view: 'activity', to: '/activity', label: '活动', icon: IconBell, dot: true },
]

// 左侧 64px 图标导航：对齐原型 #nav
export function LeftNav() {
  const openModal = useUi((s) => s.openModal)
  return (
    <nav className="w-16 shrink-0 h-full bg-bg-secondary border-r border-line flex flex-col items-center pt-3 z-[300]">
      <div className="w-9 h-9 rounded-lg bg-blue flex items-center justify-center text-sm font-bold text-white mb-3 cursor-pointer">
        A
      </div>
      {NAV_ITEMS.map((item) => {
        const Icon = item.icon
        return (
          <NavLink
            key={item.view}
            to={item.to}
            className={({ isActive }) =>
              `relative w-12 flex flex-col items-center justify-center gap-0.5 rounded-md py-1.5 my-px cursor-pointer transition-colors ${
                isActive ? 'text-blue bg-blue-bg' : 'text-text-muted hover:text-text-primary hover:bg-bg-tertiary'
              }`
            }
          >
            <Icon className="w-5 h-5" />
            <span className="text-[9px] leading-none whitespace-nowrap">{item.label}</span>
            {item.dot && <span className="absolute top-1.5 right-1.5 w-[5px] h-[5px] bg-down rounded-full" />}
          </NavLink>
        )
      })}
      <div className="flex-1" />
      <button
        className="w-12 flex flex-col items-center justify-center gap-0.5 rounded-md py-1.5 text-text-muted mb-3 hover:text-text-primary hover:bg-bg-tertiary cursor-pointer"
        onClick={() => openModal('settings')}
        title="设置"
      >
        <IconSettings className="w-5 h-5" />
        <span className="text-[9px] leading-none">设置</span>
      </button>
    </nav>
  )
}
