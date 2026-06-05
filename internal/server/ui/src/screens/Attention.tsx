import { useState } from 'react'
import { AlertTriangle, Check, Filter, Inbox, ListPlus, Share2 } from 'lucide-react'
import { useAction, useAttention, useAttentionTrace } from '../lib/query'
import { useDocumentTitle } from '../lib/useDocumentTitle'
import { EmptyState, ErrorNote, Loading, SourceIcon } from '../components/ui'
import type { AttentionItem, SteeringFunnel, SteeringTrace } from '../lib/types'

const STATUSES = ['new', 'acted', 'dismissed', 'all'] as const
const VIEWS = ['feed', 'trace'] as const
type View = (typeof VIEWS)[number]

export function Attention() {
  useDocumentTitle('Attention')
  const [view, setView] = useState<View>('feed')

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <div className="eyebrow">attention</div>
          <h1 className="h-xl">Attention Feed</h1>
        </div>
        <div className="spacer" />
        <div className="row gap">
          {VIEWS.map((v) => (
            <button
              key={v}
              type="button"
              className={`btn sm ${view === v ? 'primary' : 'ghost'}`}
              onClick={() => setView(v)}
            >
              {v}
            </button>
          ))}
        </div>
      </div>

      {view === 'feed' ? <FeedView /> : <TraceView />}
    </div>
  )
}

function FeedView() {
  const [status, setStatus] = useState<string>('new')
  const { data, isLoading, error } = useAttention(status)
  const action = useAction()

  const act = (item: AttentionItem, verb: string) => {
    if (action.isPending) return
    action.mutate({ kind: 'attention-act', target: item.id, attention_action: verb })
  }

  return (
    <>
      <div className="row gap" style={{ marginBottom: 16 }}>
        {STATUSES.map((s) => (
          <button
            key={s}
            type="button"
            className={`btn sm ${status === s ? 'primary' : 'ghost'}`}
            onClick={() => setStatus(s)}
          >
            {s}
          </button>
        ))}
      </div>

      {isLoading ? (
        <Loading label="loading attention feed" />
      ) : error ? (
        <ErrorNote error={error} />
      ) : (data ?? []).length === 0 ? (
        <EmptyState
          title="Nothing needs you"
          hint="The steerer surfaces messages worth your attention here — from watched channels, DMs, and mentions."
        />
      ) : (
        <div className="att-list">
          {(data ?? []).map((it) => (
            <AttentionCard key={it.id} item={it} disabled={action.isPending} onAct={act} />
          ))}
        </div>
      )}
    </>
  )
}

function AttentionCard({
  item,
  disabled,
  onAct,
}: {
  item: AttentionItem
  disabled: boolean
  onAct: (item: AttentionItem, verb: string) => void
}) {
  const urgent = item.urgency === 'urgent'
  return (
    <div className={`card att-card${urgent ? ' att-urgent' : ''}`}>
      <div className="att-head row gap">
        <SourceIcon source={item.source} />
        <span className="badge accent">{item.suggested_action.replace(/_/g, ' ')}</span>
        {item.urgency ? <span className={`badge ${urgent ? 'warn' : ''}`}>{item.urgency}</span> : null}
        {item.is_vip ? <span className="badge info">vip</span> : null}
        <span className="spacer" />
        <span className="num faint" title="confidence">{Math.round(item.confidence * 100)}%</span>
      </div>

      <div className="att-summary">{item.summary || <span className="faint">(no summary)</span>}</div>
      {item.reason ? <div className="att-reason dim">{item.reason}</div> : null}
      {item.matched_task ? <div className="att-meta mono faint">→ {item.matched_task}</div> : null}

      {item.draft ? (
        <div className="att-draft">
          <div className="eyebrow">drafted reply</div>
          <div className="att-draft-body">{item.draft}</div>
        </div>
      ) : null}

      {item.status === 'new' ? (
        <div className="att-actions row gap">
          <button type="button" className="btn primary sm" disabled={disabled} onClick={() => onAct(item, 'make-task')}>
            <ListPlus size={13} /> Make task
          </button>
          {item.matched_task ? (
            <button type="button" className="btn sm" disabled={disabled} onClick={() => onAct(item, 'forward')}>
              <Share2 size={13} /> Forward
            </button>
          ) : null}
          <button type="button" className="btn ghost sm" disabled={disabled} onClick={() => onAct(item, 'dismiss')}>
            <Check size={13} /> Dismiss
          </button>
        </div>
      ) : (
        <div className="att-resolved faint mono">
          {item.status}
          {item.acted_at ? ` · ${item.acted_at.slice(0, 10)}` : ''}
        </div>
      )}
    </div>
  )
}

// ----- Trace (decision-log) view -----------------------------------------
const WINDOWS = [
  { id: '1h', label: '1h', ms: 60 * 60 * 1000 },
  { id: '24h', label: '24h', ms: 24 * 60 * 60 * 1000 },
  { id: '7d', label: '7d', ms: 7 * 24 * 60 * 60 * 1000 },
] as const

function TraceView() {
  const [windowId, setWindowId] = useState<string>('24h')
  const win = WINDOWS.find((w) => w.id === windowId) ?? WINDOWS[1]
  const since = new Date(Date.now() - win.ms).toISOString()
  const { data, isLoading, error } = useAttentionTrace(since)
  const items = data?.items ?? []

  return (
    <>
      <div className="row gap" style={{ marginBottom: 16 }}>
        {WINDOWS.map((w) => (
          <button
            key={w.id}
            type="button"
            className={`btn sm ${windowId === w.id ? 'primary' : 'ghost'}`}
            onClick={() => setWindowId(w.id)}
          >
            {w.label}
          </button>
        ))}
      </div>

      {data ? <FunnelStrip funnel={data.funnel} /> : null}

      {isLoading ? (
        <Loading label="loading triage decisions" />
      ) : error ? (
        <ErrorNote error={error} />
      ) : items.length === 0 ? (
        <EmptyState
          title="No decisions yet"
          hint="No triage decisions in this window. The steerer logs every message it sees here."
        />
      ) : (
        <div className="trace-list">
          <div className="trace-row trace-head faint mono">
            <span>time</span>
            <span>origin</span>
            <span>disposition</span>
            <span>stage</span>
            <span className="trace-conf">conf</span>
            <span>channel</span>
            <span>detail</span>
          </div>
          {items.map((it) => (
            <TraceRow key={it.id} item={it} />
          ))}
        </div>
      )}
    </>
  )
}

function FunnelStrip({ funnel }: { funnel: SteeringFunnel }) {
  const cells: { key: keyof SteeringFunnel; label: string; mark?: string; tone?: string }[] = [
    { key: 'observed', label: 'Observed' },
    { key: 'dropped_stage0', label: 'Stage 0', mark: '✕' },
    { key: 'dropped_cache', label: 'Cache', mark: '✕' },
    { key: 'dropped_stage1', label: 'Stage 1', mark: '✕' },
    { key: 'dropped_stage2', label: 'Stage 2', mark: '✕' },
    { key: 'surfaced', label: 'Surfaced', mark: '✓', tone: 'accent' },
    { key: 'errors', label: 'Errors', mark: '⚠', tone: 'warn' },
  ]
  return (
    <div className="funnel-strip row gap">
      {cells.map((c, i) => {
        const n = funnel[c.key]
        // Only emphasize the error chip when there's actually something to flag.
        const tone = c.tone === 'warn' ? (n > 0 ? 'warn' : '') : c.tone ?? ''
        const icon =
          c.key === 'observed' ? (
            <Inbox size={12} />
          ) : c.key === 'errors' ? (
            <AlertTriangle size={12} />
          ) : c.key === 'surfaced' ? (
            <Check size={12} />
          ) : (
            <Filter size={12} />
          )
        return (
          <div key={c.key} className={`funnel-cell card${tone ? ` funnel-${tone}` : ''}`}>
            <div className="funnel-top row">
              <span className="funnel-icon faint">{icon}</span>
              <span className="num funnel-count">{n}</span>
            </div>
            <div className="funnel-label faint">
              {c.mark ? <span className="funnel-mark">{c.mark} </span> : null}
              {c.label}
            </div>
            {i < cells.length - 1 ? <span className="funnel-arrow faint">→</span> : null}
          </div>
        )
      })}
    </div>
  )
}

const DISPOSITION_TONE: Record<string, string> = {
  surfaced: 'badge accent',
  dropped: 'badge',
  error: 'badge warn',
}

function TraceRow({ item }: { item: SteeringTrace }) {
  const conf =
    item.final_confidence ?? item.stage2_confidence ?? item.stage3_confidence ?? undefined
  const detail =
    item.disposition === 'error'
      ? item.error
      : item.drop_reason || item.text_preview || ''
  const dispClass = DISPOSITION_TONE[item.disposition] ?? 'badge'
  const dimDetail = item.disposition === 'dropped' && !item.drop_reason
  // channel holds a slack channel id (or empty for DMs); fall back to the
  // channel_type ("dm"/"public") or source so the column is never blank.
  const where = item.channel || item.channel_type || item.source
  return (
    <div className={`trace-row trace-${item.disposition}`}>
      <span className="mono faint trace-time">{item.created_at.slice(11, 19)}</span>
      <span>
        <span className="badge">{item.origin}</span>
      </span>
      <span>
        <span className={dispClass}>{item.disposition}</span>
      </span>
      <span className="mono dim trace-stage">{item.stage_reached || '—'}</span>
      <span className="num faint trace-conf">
        {conf != null ? `${Math.round(conf * 100)}%` : ''}
      </span>
      <span className="mono faint trace-channel" title={item.thread_key || where}>
        {where}
      </span>
      <span className={`trace-detail ${dimDetail ? 'faint' : 'dim'}`} title={detail}>
        {detail || <span className="faint">—</span>}
      </span>
    </div>
  )
}
