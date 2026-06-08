import { useState, type ReactNode } from 'react'
import { useAction } from '../lib/query'
import type { SettingField } from '../lib/types'

// Reusable settings primitives shared by the Settings screen (generic config,
// grouped by Group) and the Connectors screen (config filtered per connector).
// Keeping the field renderer + draft/diff/save logic here means a connector
// card and a Settings group stage and persist edits the exact same way.

const BOOL_TRUE = ['1', 'true', 'yes', 'y', 'on']
export const isBoolOn = (s: string) => BOOL_TRUE.includes(s.trim().toLowerCase())

export function SettingsSection({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
  return (
    <section className="settings-section">
      <div className="settings-section-head">
        <span className="eyebrow">{title}</span>
        {hint && <span className="settings-section-hint">{hint}</span>}
      </div>
      {children}
    </section>
  )
}

export function SettingsPanel({ title, icon, children }: { title: string; icon: ReactNode; children: ReactNode }) {
  return (
    <section className="settings-card">
      <div className="settings-card-head">
        <span>{icon}</span>
        <h2>{title}</h2>
      </div>
      <div className="settings-card-body">{children}</div>
    </section>
  )
}

// One editable setting. `draft` is the staged value (undefined → show the stored
// value); `onChange` stages an edit. Secrets render as a password field whose
// blank value means "leave the stored secret unchanged".
export function ConfigField({
  field,
  draft,
  onChange,
}: {
  field: SettingField
  draft: string | undefined
  onChange: (v: string) => void
}) {
  const checked = draft !== undefined ? isBoolOn(draft) : isBoolOn(field.value)
  return (
    <div className="config-field">
      <div className="config-field-head">
        <label className="setting-label" htmlFor={field.key}>
          {field.label}
        </label>
        {field.source !== 'default' && <span className={`config-src ${field.source}`}>{field.source}</span>}
      </div>
      {field.type === 'bool' ? (
        <label className="config-toggle">
          <input id={field.key} type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked ? 'true' : 'false')} />
          <span>{checked ? 'On' : 'Off'}</span>
        </label>
      ) : field.type === 'enum' ? (
        <select id={field.key} className="input" value={draft ?? field.value} onChange={(e) => onChange(e.target.value)}>
          {(field.options ?? []).map((o) => (
            <option key={o} value={o}>
              {o}
            </option>
          ))}
        </select>
      ) : field.type === 'secret' ? (
        <input
          id={field.key}
          className="input mono"
          type="password"
          autoComplete="off"
          placeholder={field.set ? '•••••••• (set — blank keeps it)' : 'not set'}
          value={draft ?? ''}
          onChange={(e) => onChange(e.target.value)}
        />
      ) : (
        <input
          id={field.key}
          className="input"
          type={field.type === 'int' ? 'number' : 'text'}
          value={draft ?? field.value}
          placeholder={field.default || ''}
          onChange={(e) => onChange(e.target.value)}
        />
      )}
      {field.help && <div className="config-help">{field.help}</div>}
    </div>
  )
}

// useConfigDraft owns the stage → diff → save lifecycle for a set of settings.
// Callers render ConfigFields bound to `draft`/`setField`, compute `changesFor`
// to know if a group is dirty, and call `save` to persist only the changed keys
// (empty secrets are skipped → keep the stored value). Saved keys are dropped
// from the draft on success so the form reflects the now-stored values.
export function useConfigDraft() {
  const action = useAction()
  const [draft, setDraft] = useState<Record<string, string>>({})

  const setField = (key: string, v: string) => setDraft((d) => ({ ...d, [key]: v }))

  const changesFor = (fields: SettingField[]) => {
    const out: Record<string, string> = {}
    for (const f of fields) {
      const v = draft[f.key]
      if (v === undefined) continue
      if (f.type === 'secret') {
        if (v.trim() !== '') out[f.key] = v
      } else if (v !== f.value) {
        out[f.key] = v
      }
    }
    return out
  }

  const save = (fields: SettingField[]) => {
    const changes = changesFor(fields)
    if (Object.keys(changes).length === 0) return
    action.mutate(
      { kind: 'update-settings', settings: changes },
      {
        onSuccess: () =>
          setDraft((d) => {
            const next = { ...d }
            for (const k of Object.keys(changes)) delete next[k]
            return next
          }),
      },
    )
  }

  return { draft, setField, changesFor, save, isPending: action.isPending }
}
