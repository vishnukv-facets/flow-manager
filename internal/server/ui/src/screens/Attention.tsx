import { useState } from 'react'
import { Check, ListPlus, Share2 } from 'lucide-react'
import { useAction, useAttention } from '../lib/query'
import { useDocumentTitle } from '../lib/useDocumentTitle'
import { EmptyState, ErrorNote, Loading, SourceIcon } from '../components/ui'
import type { AttentionItem } from '../lib/types'

const STATUSES = ['new', 'acted', 'dismissed', 'all'] as const

export function Attention() {
  useDocumentTitle('Attention')
  const [status, setStatus] = useState<string>('new')
  const { data, isLoading, error } = useAttention(status)
  const action = useAction()

  const act = (item: AttentionItem, verb: string) => {
    if (action.isPending) return
    action.mutate({ kind: 'attention-act', target: item.id, attention_action: verb })
  }

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <div className="eyebrow">attention</div>
          <h1 className="h-xl">Attention Feed</h1>
        </div>
        <div className="spacer" />
        <div className="row gap">
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
    </div>
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
