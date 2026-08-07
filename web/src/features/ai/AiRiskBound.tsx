// AI 自动减仓边界控制（原型方案4）：enabled 由 AI 设置弹窗中的开关控制
export function AiRiskBound({ enabled }: { enabled: boolean }) {
  return (
    <div className={`mt-2.5 p-3 bg-bg-tertiary rounded-md border-l-[3px] border-l-purple transition-opacity ${enabled ? '' : 'opacity-35 pointer-events-none'}`}>
      <div className="text-xs font-semibold text-purple mb-0.5">⚙ 自动减仓边界控制</div>
      <div className="text-[10px] text-text-muted mb-2.5">仅当以下条件全部满足时才触发自动减仓，避免误操作与过度干预</div>
      <BoundRow label="触发风险率阈值" defaultValue={90} min={50} max={100} step={1} unit="% (≥维持保证金率)" />
      <BoundRow label="单次最大减仓比例" defaultValue={30} min={5} max={100} step={5} unit="% (占当前仓位)" />
      <BoundRow label="每日减仓次数上限" defaultValue={5} min={1} max={20} step={1} unit="次 / 24h" />
      <BoundRow label="最低保留仓位" defaultValue={0.1} min={0} max={1} step={0.05} unit="BTC (不低于)" />
      <BoundSelect label="减仓执行方式" options={['市价对冲（推荐）', '限价挂单', '对手价吃单']} />
      <BoundSelect label="允许执行时段" options={['7×24 全时段', '仅高波动时段', '自定义']} />
    </div>
  )
}

function BoundRow({
  label,
  defaultValue,
  min,
  max,
  step,
  unit,
}: {
  label: string
  defaultValue: number
  min: number
  max: number
  step: number
  unit: string
}) {
  return (
    <div className="flex justify-between items-center my-2 text-xs">
      <label className="text-text-muted">{label}</label>
      <span className="flex items-center gap-1">
        <input
          type="number"
          defaultValue={defaultValue}
          min={min}
          max={max}
          step={step}
          className="w-[88px] bg-bg-secondary border border-line text-text-primary px-1.5 py-1 rounded text-xs font-mono focus:border-blue focus:outline-none"
        />
        <span className="text-[10px] text-text-muted">{unit}</span>
      </span>
    </div>
  )
}

function BoundSelect({ label, options }: { label: string; options: string[] }) {
  return (
    <div className="flex justify-between items-center my-2 text-xs">
      <label className="text-text-muted">{label}</label>
      <select className="w-[88px] bg-bg-secondary border border-line text-text-primary px-1.5 py-1 rounded text-xs font-mono focus:border-blue focus:outline-none">
        {options.map((o) => (
          <option key={o}>{o}</option>
        ))}
      </select>
    </div>
  )
}

export function AiRiskBoundToggle({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="flex items-center gap-1.5 cursor-pointer text-[10px]">
      <input
        type="checkbox"
        className="sr-only"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
      />
      <span
        className={`relative w-11 h-6 rounded-full transition-colors border ${
          checked ? 'bg-purple border-purple' : 'bg-bg-tertiary border-line'
        }`}
      >
        <span
          className={`absolute top-[2px] left-[2px] w-[18px] h-[18px] bg-white rounded-full transition-transform ${
            checked ? 'translate-x-5' : ''
          }`}
        />
      </span>
      <span className="text-[11px] text-purple font-semibold">{checked ? '已启用' : '未启用'}</span>
    </label>
  )
}
