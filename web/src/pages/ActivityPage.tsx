import { useState } from 'react'
import { Toggle } from '../components/ui/Toggle'

// 活动中心 & 公告：对齐原型 #activity-view
export default function ActivityPage() {
  const [versionNotif, setVersionNotif] = useState(true)
  const [riskNotif, setRiskNotif] = useState(true)
  const [promoNotif, setPromoNotif] = useState(true)
  const [aiNotif, setAiNotif] = useState(false)

  const notices = [
    {
      icon: '📢',
      title: 'AleoBook V2.0 主网上线公告',
      desc: '全量AI智能策略引擎、机构暗池完整版、NFT权益体系正式上线。新增隐私衍生品交易预览。',
      time: '2026-05-28 12:00 · 版本更新',
    },
    {
      icon: '⚠',
      title: '风控提示: ALEO 网络升级维护',
      desc: '6月1日 02:00-04:00 UTC进行网络升级，期间部分功能暂停。请提前调整杠杆仓位。',
      time: '2026-05-27 18:00 · 系统通知',
    },
    {
      icon: '🎉',
      title: '交易挖矿: 双倍 ALEO 奖励周',
      desc: '本周交易量前100名用户共享 50,000 ALEO 奖池。暗池交易额外 +20% 奖励。',
      time: '2026-05-25 ~ 2026-06-01 · 活动',
    },
  ]

  const campaigns = [
    { icon: '🎯', title: '新手引导计划', desc: 'AI功能 · 隐私设置 · 杠杆教程', btn: '开始学习' },
    { icon: '🏆', title: 'AI体验活动', desc: '使用AI策略引擎，赢取ALEO奖励', btn: '立即参与' },
    { icon: '🤝', title: '机构合作', desc: '暗池免服务费30天 · 专属后台', btn: '申请白名单' },
    { icon: '💎', title: 'LP 流动性激励', desc: '添加流动性 · 双倍ALEO', btn: '添加LP' },
  ]

  return (
    <div className="p-5 overflow-y-auto h-full">
      <h2 className="text-xl font-semibold mb-5">活动中心 & 公告</h2>

      <h3 className="text-[15px] text-text-secondary mt-6 mb-3 border-b border-line pb-2">系统公告</h3>
      {notices.map((n) => (
        <div key={n.title} className="bg-bg-secondary rounded-lg p-3.5 mb-2 flex gap-3 items-start">
          <div className="w-8 h-8 rounded-lg bg-ai-glow flex items-center justify-center text-sm shrink-0">{n.icon}</div>
          <div>
            <div className="font-semibold">{n.title}</div>
            <div className="text-xs text-text-muted mt-0.5">{n.desc}</div>
            <div className="text-[11px] text-text-muted mt-0.5">{n.time}</div>
          </div>
        </div>
      ))}

      <h3 className="text-[15px] text-text-secondary mt-6 mb-3 border-b border-line pb-2">运营活动</h3>
      <div className="grid gap-3 grid-cols-[repeat(auto-fill,minmax(240px,1fr))]">
        {campaigns.map((c) => (
          <div key={c.title} className="bg-bg-secondary border border-line rounded-lg p-4">
            <div className="text-2xl mb-2">{c.icon}</div>
            <div className="font-bold">{c.title}</div>
            <div className="text-xs text-text-muted my-1">{c.desc}</div>
            <button className="bg-blue text-white border-none px-3 py-1 rounded-md font-semibold text-[11px] cursor-pointer hover:opacity-90">
              {c.btn}
            </button>
          </div>
        ))}
      </div>

      <h3 className="text-[15px] text-text-secondary mt-6 mb-3 border-b border-line pb-2">通知设置</h3>
      <div className="bg-bg-secondary border border-line rounded-lg px-4">
        <SettingToggle label="版本更新通知" checked={versionNotif} onChange={setVersionNotif} />
        <SettingToggle label="风控提示推送" checked={riskNotif} onChange={setRiskNotif} />
        <SettingToggle label="活动优惠提醒" checked={promoNotif} onChange={setPromoNotif} />
        <SettingToggle label="AI策略运行报告" checked={aiNotif} onChange={setAiNotif} />
      </div>
    </div>
  )
}

function SettingToggle({
  label,
  checked,
  onChange,
}: {
  label: string
  checked: boolean
  onChange: (v: boolean) => void
}) {
  return (
    <div className="flex justify-between items-center py-3 border-b border-line last:border-b-0">
      <label className="text-[13px]">{label}</label>
      <Toggle checked={checked} onChange={onChange} />
    </div>
  )
}
