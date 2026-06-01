import { Lock, Zap, Ban } from 'lucide-react'
import { ProviderIcon } from './ui'

const PERMS = [
  { v: 'default', label: 'default', Icon: Lock, title: 'Permissions: default · prompt to run' },
  { v: 'auto', label: 'auto', Icon: Zap, title: 'Permissions: auto · accept edits' },
  { v: 'bypass', label: 'bypass', Icon: Ban, title: 'Permissions: bypass · skip all prompts' },
]

// Universal permission selector — lock / zap / ban, used in both the new-task
// form and the session toolbar so the control reads the same everywhere.
export function PermissionPicker({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <div className="segmented perm-seg">
      {PERMS.map(({ v, label, Icon, title }) => (
        <button key={v} type="button" className={value === v ? 'active' : ''} title={title} onClick={() => onChange(v)}>
          <Icon size={14} /> {label}
        </button>
      ))}
    </div>
  )
}

const PRIOS = [
  { v: 'low', label: 'low' },
  { v: 'medium', label: 'medium' },
  { v: 'high', label: 'high' },
]

// Priority selector — low / medium / high as text segments.
export function PriorityPicker({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <div className="segmented prio-seg">
      {PRIOS.map(({ v, label }) => (
        <button key={v} type="button" className={value === v ? 'active' : ''} onClick={() => onChange(v)}>
          <span className={`prio ${v}`} /> {label}
        </button>
      ))}
    </div>
  )
}

// Agent selector as a segmented switch (Claude / Codex) — one connected control
// that reads as a toggle, matching the permission segmented control beside it.
// The active provider's segment is accent-filled. Unavailable providers (binary
// not on PATH) render greyed-out and unclickable with the reason as a tooltip.
export function AgentPicker({
  value,
  onChange,
  providers,
}: {
  value: string
  onChange: (v: string) => void
  providers: { id: string; label: string; available?: boolean; reason?: string }[]
}) {
  const list = providers.length ? providers : [{ id: 'claude', label: 'Claude Code', available: true }]
  return (
    <div className="segmented agent-seg" role="radiogroup" aria-label="Agent">
      {list.map((p) => {
        const disabled = p.available === false
        const active = value === p.id
        return (
          <button
            key={p.id}
            type="button"
            role="radio"
            aria-checked={active}
            disabled={disabled}
            className={active ? 'active' : ''}
            title={disabled ? p.reason || `${p.label} is not installed` : p.label}
            onClick={() => !disabled && onChange(p.id)}
          >
            <ProviderIcon provider={p.id} size={14} />
            <span className="clip">{p.label}</span>
          </button>
        )
      })}
    </div>
  )
}
