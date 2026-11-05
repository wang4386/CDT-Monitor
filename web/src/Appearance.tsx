import { useEffect, useId, useRef, useState } from 'react'
import type { KeyboardEvent } from 'react'
import { Check, Monitor, Moon, Sun } from 'lucide-react'
import { useTheme } from './theme'
import './appearance.css'

const modes = [
  { value: 'light', label: '浅色', description: '始终使用浅色外观', icon: Sun },
  { value: 'dark', label: '暗色', description: '始终使用暗色外观', icon: Moon },
  { value: 'system', label: '跟随系统', description: '与设备外观保持一致', icon: Monitor },
] as const

export function ThemeMenu() {
  const { preferences, updatePreferences } = useTheme()
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const menuId = useId()
  const selected = modes.find((mode) => mode.value === preferences.mode) ?? modes[2]
  const Icon = selected.icon

  useEffect(() => {
    if (!open) return
    menuRef.current?.querySelector<HTMLElement>('[aria-checked="true"]')?.focus()
    const dismiss = (event: PointerEvent) => { if (!rootRef.current?.contains(event.target as Node)) setOpen(false) }
    const blur = (event: FocusEvent) => { if (!rootRef.current?.contains(event.target as Node)) setOpen(false) }
    document.addEventListener('pointerdown', dismiss)
    document.addEventListener('focusin', blur)
    return () => {
      document.removeEventListener('pointerdown', dismiss)
      document.removeEventListener('focusin', blur)
    }
  }, [open])

  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); setOpen(false); triggerRef.current?.focus(); return }
    const items = Array.from(menuRef.current?.querySelectorAll<HTMLButtonElement>('button') ?? [])
    const index = items.indexOf(document.activeElement as HTMLButtonElement)
    let target = -1
    if (event.key === 'ArrowDown') target = (index + 1) % items.length
    if (event.key === 'ArrowUp') target = (index - 1 + items.length) % items.length
    if (event.key === 'Home') target = 0
    if (event.key === 'End') target = items.length - 1
    if (target >= 0) { event.preventDefault(); items[target]?.focus() }
  }

  return <div className="theme-menu" ref={rootRef}>
    <button ref={triggerRef} type="button" className="icon-button" aria-label={`外观：${selected.label}`} title={`外观：${selected.label}`} aria-haspopup="menu" aria-expanded={open} aria-controls={open ? menuId : undefined} onClick={() => setOpen(!open)} onKeyDown={(event) => {
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); setOpen(true) }
    }}><Icon /></button>
    {open && <div ref={menuRef} id={menuId} className="theme-menu__popover" role="menu" aria-label="选择外观" onKeyDown={onKeyDown}>
      <div className="theme-menu__heading"><span>外观模式</span><small>即时应用</small></div>
      <div className="theme-menu__options">
        {modes.map(({ value, label, description, icon: ModeIcon }) => <button key={value} type="button" role="menuitemradio" aria-checked={preferences.mode === value} tabIndex={preferences.mode === value ? 0 : -1} onClick={() => { updatePreferences({ mode: value }); setOpen(false); triggerRef.current?.focus() }}>
          <span className="theme-menu__icon"><ModeIcon /></span><span className="theme-menu__copy"><b>{label}</b><small>{description}</small></span>{preferences.mode === value && <Check className="theme-menu__check" />}
        </button>)}
      </div>
    </div>}
  </div>
}
