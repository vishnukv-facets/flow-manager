import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { AlertTriangle, Bell, CheckCircle2, Database, ExternalLink, Globe2, Link2, Loader2, MonitorCog, Moon, PlugZap, Save, Settings as SettingsIcon, SlidersHorizontal, Sun } from 'lucide-react'
import { useAction, useHealth, useIngressStatus, useSettings, useUiData } from '../lib/query'
import { useDocumentTitle } from '../lib/useDocumentTitle'
import { getTheme, onThemeChange, toggleTheme, type Theme } from '../lib/theme'
import { ErrorNote, Loading, ProviderIcon, SourceIcon } from '../components/ui'
import { SlackConnect } from '../components/SlackConnect'
import { WatchedChannels } from '../components/WatchedChannels'
import { AutonomyPanel } from '../components/AutonomyPanel'
import type { IngressStatus, SettingField, ToolCapability } from '../lib/types'
import { useMascotPrefs, setMascotPrefs, NAP_OPTIONS } from '../lib/mascot'

type BrowserNotificationPermission = NotificationPermission | 'unsupported'

function notificationPermission(): BrowserNotificationPermission {
  if (typeof window === 'undefined' || !('Notification' in window)) return 'unsupported'
  return Notification.permission
}

export function Settings() {
  useDocumentTitle('Settings')
  const { data: ui, isLoading, error } = useUiData()
  const { data: health } = useHealth()
  const [theme, setTheme] = useState<Theme>(() => getTheme())
  const [permission, setPermission] = useState<BrowserNotificationPermission>(() => notificationPermission())
  const mascot = useMascotPrefs()

  useEffect(() => onThemeChange(setTheme), [])

  const enableNotifications = async () => {
    if (typeof window === 'undefined' || !('Notification' in window)) {
      setPermission('unsupported')
      return
    }
    setPermission(await Notification.requestPermission())
  }

  if (isLoading) return <div className="page"><Loading rows={5} /></div>
  if (error) return <div className="page"><ErrorNote error={error} /></div>

  const db = ui?.FLOWDB
  const user = ui?.USER
  const caps = ui?.CAPABILITIES
  const headerPills = [...(caps?.providers ?? []), ...(caps?.integrations ?? [])]

  return (
    <div className="page">
      <div className="page-head mc-head">
        <div>
          <div className="eyebrow">workspace</div>
          <h1 className="h-xl">Settings</h1>
          <div className="page-sub">Integrations, agents, and workspace — tuned without touching a shell.</div>
        </div>
        <div className="spacer" />
        <div className="mc-env-pills">
          {headerPills.map((c) => (
            <span key={c.id} className={`env-pill${c.available ? '' : ' off'}`} title={c.reason || c.status || ''}>
              <span className={`dot ${c.available ? 'running' : 'idle'}`} />
              {c.id === 'claude' || c.id === 'codex' ? (
                <ProviderIcon provider={c.id} size={13} />
              ) : (
                <SourceIcon source={c.id === 'gh' ? 'github' : c.id} size={12} />
              )}
              {c.label}
            </span>
          ))}
        </div>
      </div>

      <div className="settings-summary card">
        <SummaryItem label="User" value={user?.full_name || user?.name || user?.username || 'unknown'} />
        <SummaryItem label="Flow root" value={health?.flow_root || '—'} mono />
        <SummaryItem label="Version" value={health?.version || 'dev'} mono />
        <SummaryItem label="Database" value={db?.human_size || '—'} />
        <SummaryItem label="DB status" value={db?.exists ? 'available' : 'missing'} />
      </div>

      <SettingsSection title="Preferences">
        <div className="settings-grid">
          <SettingsPanel title="Appearance & alerts" icon={<SettingsIcon size={17} />}>
            <div className="setting-row">
              <div>
                <div className="setting-label">Theme</div>
                <div className="setting-value">{theme}</div>
              </div>
              <button type="button" className="btn" onClick={() => setTheme(toggleTheme())}>
                {theme === 'dark' ? <Sun size={15} /> : <Moon size={15} />}
                {theme === 'dark' ? 'Light' : 'Dark'}
              </button>
            </div>
            <div className="setting-row">
              <div>
                <div className="setting-label">Desktop alerts</div>
                <div className="setting-value">{permission}</div>
              </div>
              {permission === 'default' && (
                <button type="button" className="btn" onClick={enableNotifications}>
                  <Bell size={15} /> Enable
                </button>
              )}
            </div>
          </SettingsPanel>
          <SettingsPanel title="Mascot" icon={<SlidersHorizontal size={17} />}>
            <div className="setting-row">
              <div>
                <div className="setting-label">Sidebar mascot</div>
                <div className="setting-value">{mascot.enabled ? 'on' : 'off'}</div>
              </div>
              <button type="button" className="btn" onClick={() => setMascotPrefs({ enabled: !mascot.enabled })}>
                {mascot.enabled ? 'Hide' : 'Show'}
              </button>
            </div>
            <div className="setting-row">
              <div>
                <div className="setting-label">Naps when idle for</div>
                <div className="setting-value">no activity this long → it sleeps</div>
              </div>
              <select className="input" value={mascot.napSec} onChange={(e) => setMascotPrefs({ napSec: Number(e.target.value) })}>
                {NAP_OPTIONS.map((o) => (
                  <option key={o.sec} value={o.sec}>{o.label}</option>
                ))}
              </select>
            </div>
          </SettingsPanel>
          <SettingsPanel title="Database" icon={<Database size={17} />}>
            <KeyValue label="Path" value={db?.display_path || db?.path || 'unknown'} mono />
            <KeyValue label="Size" value={db?.human_size || 'unknown'} />
            <KeyValue label="Status" value={db?.exists ? 'available' : 'missing'} />
          </SettingsPanel>
        </div>
      </SettingsSection>

      <SettingsSection title="Slack" hint="Three steps, one approval — reactions become sessions.">
        <SlackConnect />
      </SettingsSection>

      <SettingsSection title="Steering" hint="What the attention router watches beyond DMs and mentions.">
        <WatchedChannels />
        <AutonomyPanel />
      </SettingsSection>

      <SettingsSection title="Configuration" hint="Applied live — secrets stay on this machine.">
        <div className="settings-grid">
          <ConfigPanels />
        </div>
      </SettingsSection>

      <SettingsSection title="Environment">
        <div className="settings-grid">
          <CapabilityPanel title="Agents" icon={<MonitorCog size={17} />} items={caps?.providers ?? []} />
          <CapabilityPanel title="Terminals" icon={<PlugZap size={17} />} items={caps?.terminals ?? []} />
          <CapabilityPanel title="Integrations" icon={<CheckCircle2 size={17} />} items={caps?.integrations ?? []} />
        </div>
      </SettingsSection>
    </div>
  )
}

function SettingsSection({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
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

function SummaryItem({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="sum-item">
      <div className="sum-label">{label}</div>
      <div className={`sum-value clip${mono ? ' mono' : ''}`}>{value}</div>
    </div>
  )
}

function SettingsPanel({ title, icon, children }: { title: string; icon: ReactNode; children: ReactNode }) {
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

const BOOL_TRUE = ['1', 'true', 'yes', 'y', 'on']
const isBoolOn = (s: string) => BOOL_TRUE.includes(s.trim().toLowerCase())

// ConfigPanels renders one editable card per setting group, sourced from the
// server registry. Edits are staged in `draft`; Save submits only the changed
// keys for that group (empty secret fields are skipped → keep the stored value).
function ConfigPanels() {
  const { data } = useSettings()
  const action = useAction()
  const [draft, setDraft] = useState<Record<string, string>>({})
  // These settings have dedicated controls in the Steering section, so skip
  // them in the generic form to avoid two controls for one key.
  const fields = useMemo(
    () =>
      (data?.fields ?? []).filter(
        (f) => f.key !== 'FLOW_STEERING_WATCH_CHANNELS' && f.key !== 'FLOW_STEERING_AUTONOMY',
      ),
    [data?.fields],
  )

  const groups = useMemo(() => {
    const order: string[] = []
    const byGroup: Record<string, SettingField[]> = {}
    for (const f of fields) {
      if (!byGroup[f.group]) {
        byGroup[f.group] = []
        order.push(f.group)
      }
      byGroup[f.group].push(f)
    }
    return order.map((group) => ({ group, fields: byGroup[group] }))
  }, [fields])

  const changesFor = (gfields: SettingField[]) => {
    const out: Record<string, string> = {}
    for (const f of gfields) {
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

  const saveGroup = (gfields: SettingField[]) => {
    const changes = changesFor(gfields)
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

  if (fields.length === 0) return null

  return (
    <>
      {groups.map(({ group, fields: gfields }) => {
        const dirty = Object.keys(changesFor(gfields)).length > 0
        return (
          <SettingsPanel key={group} title={group} icon={<SlidersHorizontal size={17} />}>
            <div className="config-form">
              {group === 'Ingress' && <IngressStatusPanel />}
              {gfields.map((f) => (
                <ConfigField key={f.key} field={f} draft={draft[f.key]} onChange={(v) => setDraft((d) => ({ ...d, [f.key]: v }))} />
              ))}
              <div className="config-actions">
                <button type="button" className="btn primary" disabled={!dirty || action.isPending} onClick={() => saveGroup(gfields)}>
                  {action.isPending ? <Loader2 size={14} className="spin" /> : <Save size={14} />}
                  Save {group}
                </button>
              </div>
            </div>
          </SettingsPanel>
        )
      })}
    </>
  )
}

function IngressStatusPanel() {
  const { data: ingress, isLoading } = useIngressStatus()
  if (isLoading && !ingress) {
    return (
      <div className="ingress-runtime waiting">
        <div className="ingress-runtime-head">
          <span className="ingress-state waiting"><Loader2 size={13} className="spin" /> checking</span>
        </div>
      </div>
    )
  }
  if (!ingress) return null

  return (
    <div className={`ingress-runtime ${ingressRuntimeState(ingress)}`}>
      <div className="ingress-runtime-head">
        <div className="setting-label">Runtime status</div>
        <span className={`ingress-state ${ingressRuntimeState(ingress)}`}>
          {ingress.running ? <CheckCircle2 size={13} /> : ingress.last_error ? <AlertTriangle size={13} /> : <Globe2 size={13} />}
          {ingressRuntimeLabel(ingress)}
        </span>
      </div>
      <div className="ingress-kv">
        <IngressRuntimeValue label="Provider" value={ingress.provider || 'none'} />
        {ingress.provider === 'zrok' && <IngressRuntimeValue label="zrok env" value={ingress.env_enabled ? 'enabled' : 'not enabled'} />}
        {ingress.share_name && <IngressRuntimeValue label="Share" value={ingress.share_name} mono />}
        {ingress.base_url ? <IngressRuntimeLink label="Public URL" value={ingress.base_url} /> : <IngressRuntimeValue label="Public URL" value="not created yet" />}
        {ingress.github_webhook_url && <IngressRuntimeLink label="GitHub webhook" value={ingress.github_webhook_url} />}
        {ingress.last_error && <IngressRuntimeValue label="Last error" value={ingress.last_error} mono warn />}
      </div>
    </div>
  )
}

function ingressRuntimeState(ingress: IngressStatus): 'online' | 'failed' | 'waiting' | 'off' {
  if (ingress.running) return 'online'
  if (ingress.last_error) return 'failed'
  if (ingress.provider === 'none') return 'off'
  return 'waiting'
}

function ingressRuntimeLabel(ingress: IngressStatus): string {
  const state = ingressRuntimeState(ingress)
  if (state === 'online') return 'public URL live'
  if (state === 'failed') return 'share failed'
  if (state === 'off') return 'off'
  return 'waiting for URL'
}

function IngressRuntimeValue({ label, value, mono = false, warn = false }: { label: string; value: string; mono?: boolean; warn?: boolean }) {
  return (
    <div className="ingress-row">
      <div className="setting-label">{label}</div>
      <div className={`ingress-value${mono ? ' mono' : ''}${warn ? ' warn' : ''}`} title={value}>{value}</div>
    </div>
  )
}

function IngressRuntimeLink({ label, value }: { label: string; value: string }) {
  return (
    <div className="ingress-row">
      <div className="setting-label">{label}</div>
      <a className="ingress-value link mono" href={value} target="_blank" rel="noreferrer" title={value}>
        <Link2 size={12} />
        <span>{value}</span>
        <ExternalLink size={11} />
      </a>
    </div>
  )
}

function ConfigField({ field, draft, onChange }: { field: SettingField; draft: string | undefined; onChange: (v: string) => void }) {
  const checked = draft !== undefined ? isBoolOn(draft) : isBoolOn(field.value)
  return (
    <div className="config-field">
      <div className="config-field-head">
        <label className="setting-label" htmlFor={field.key}>{field.label}</label>
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
            <option key={o} value={o}>{o}</option>
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

function CapabilityPanel({ title, icon, items }: { title: string; icon: ReactNode; items: ToolCapability[] }) {
  return (
    <SettingsPanel title={title} icon={icon}>
      <div className="cap-list">
        {items.length === 0 ? (
          <div className="setting-value">none reported</div>
        ) : (
          items.map((item) => (
            <div key={item.id} className="cap-row">
              <span className={`cap-dot ${item.available ? 'on' : 'off'}`} />
              <div className="lrow-main">
                <div className="cap-title">{item.label || item.id}</div>
                <div className="cap-sub clip">{item.path || item.reason || item.status || item.id}</div>
              </div>
              <span className="tag">{item.available ? 'ready' : 'off'}</span>
            </div>
          ))
        )}
      </div>
    </SettingsPanel>
  )
}

function KeyValue({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="setting-row compact">
      <div className="setting-label">{label}</div>
      <div className={`setting-value clip${mono ? ' mono' : ''}`}>{value}</div>
    </div>
  )
}
