import { useEffect, type ReactNode } from 'react'

// 模态框：对齐原型 .modal-overlay / .modal，Esc 或点击遮罩关闭
export function Modal({
  open,
  onClose,
  title,
  wide,
  children,
}: {
  open: boolean
  onClose: () => void
  title: string
  wide?: boolean
  children: ReactNode
}) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null
  return (
    <div
      className="fixed inset-0 bg-black/70 flex items-center justify-center z-[400]"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div
        className={`bg-bg-secondary border border-line rounded-xl w-[520px] max-h-[80vh] overflow-y-auto ${
          wide ? 'w-[680px]' : ''
        }`}
      >
        <div className="px-5 py-4 border-b border-line flex justify-between items-center">
          <h3 className="text-base font-semibold text-text-primary">{title}</h3>
          <button
            className="text-text-secondary text-xl leading-none hover:text-text-primary cursor-pointer bg-transparent border-none"
            onClick={onClose}
          >
            ✕
          </button>
        </div>
        <div className="p-5">{children}</div>
      </div>
    </div>
  )
}

// 设置行：对齐原型 .setting-row
export function SettingRow({
  title,
  desc,
  control,
}: {
  title: ReactNode
  desc?: ReactNode
  control: ReactNode
}) {
  return (
    <div className="flex justify-between items-center py-3 border-b border-line">
      <div>
        <div className="text-[13px] font-semibold text-text-primary">{title}</div>
        {desc && <div className="text-[11px] text-text-muted mt-0.5">{desc}</div>}
      </div>
      {control}
    </div>
  )
}
