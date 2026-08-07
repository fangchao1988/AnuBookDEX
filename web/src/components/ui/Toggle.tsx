// 开关：对齐原型 .toggle（选中紫色滑块）
export function Toggle({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="relative inline-block w-11 h-6 cursor-pointer shrink-0">
      <input
        type="checkbox"
        className="sr-only"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
      />
      <span
        className={`absolute inset-0 rounded-full transition-colors border ${
          checked ? 'bg-purple border-purple' : 'bg-bg-tertiary border-line'
        }`}
      />
      <span
        className={`absolute top-[2px] left-[2px] w-[18px] h-[18px] bg-white rounded-full transition-transform ${
          checked ? 'translate-x-5' : ''
        }`}
      />
    </label>
  )
}
