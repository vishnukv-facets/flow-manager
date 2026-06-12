import { useMemo, useState } from 'react'
import { Link } from 'wouter'
import { Archive, Bot, ChevronDown, ChevronRight, Loader2, Pause, Play, ScrollText, Search, TimerReset, Zap } from 'lucide-react'
import { apiPost } from '../lib/api'
import { queryClient, useOwner, useOwners } from '../lib/query'
import { useDocumentTitle } from '../lib/useDocumentTitle'
import { confirmAction } from '../lib/confirm'
import { pushToast } from '../lib/toast'
import { ago, dateTime } from '../lib/format'
import { EmptyState, ErrorNote, Loading, ProviderIcon } from '../components/ui'
import type { OwnerTaskRow, OwnerView } from '../lib/types'

const STATUS_FILTERS = [
  { v: '', label: 'All' },
  { v: 'active', label: 'Active' },
  { v: 'paused', label: 'Paused' },
  { v: 'retired', label: 'Retired' },
] as const

export function Owners() {
  useDocumentTitle('Owners')
  const [q, setQ] = useState('')
  const [status, setStatus] = useState('')
  const [showArchived, setShowArchived] = useState(false)
  const { data, isLoading, error } = useOwners({ status, include_archived: showArchived })

  const owners = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return (data ?? [])
      .filter((o) => {
        if (!needle) return true
        return (
          o.name.toLowerCase().includes(needle) ||
          o.slug.toLowerCase().includes(needle) ||
          o.work_dir.toLowerCase().includes(needle) ||
          (o.project_slug ?? '').toLowerCase().includes(needle)
        )
      })
      .slice()
      .sort((a, b) => {
        if (a.next_due !== b.next_due) return a.next_due ? -1 : 1
        return Date.parse(b.updated_at) - Date.parse(a.updated_at)
      })
  }, [data, q])

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <div className="eyebrow">autonomous operations</div>
          <h1 className="h-xl">Owners</h1>
        </div>
      </div>

      {!isLoading && !error && data && data.length > 0 && (
        <div className="row gap wrap" style={{ marginBottom: 18, gap: 14, alignItems: 'center' }}>
          <div className="input-icon" style={{ maxWidth: 280 }}>
            <Search size={14} className="dim" />
            <input
              className="input"
              placeholder="Filter owners..."
              value={q}
              onChange={(e) => setQ(e.target.value)}
            />
          </div>
          <div className="segmented">
            {STATUS_FILTERS.map((s) => (
              <button key={s.v || 'all'} className={status === s.v ? 'active' : ''} onClick={() => setStatus(s.v)}>
                {s.label}
              </button>
            ))}
          </div>
          <div className="chips">
            <button
              className={`chip${showArchived ? ' active' : ''}`}
              aria-pressed={showArchived}
              onClick={() => setShowArchived((v) => !v)}
            >
              Archived
            </button>
          </div>
        </div>
      )}

      {isLoading ? (
        <Loading rows={5} />
      ) : error ? (
        <ErrorNote error={error} />
      ) : !data || data.length === 0 ? (
        <EmptyState icon={<Bot size={30} />} title="No owners" hint="Create one with flow add owner." />
      ) : owners.length === 0 ? (
        <EmptyState icon={<Bot size={30} />} title="No owners match" hint="Adjust the filter." />
      ) : (
        <div className="card" style={{ padding: '6px 14px 4px' }}>
          <table className="tbl fixed">
            <colgroup>
              <col />
              <col style={{ width: 104 }} />
              <col style={{ width: 86 }} />
              <col style={{ width: 130 }} />
              <col style={{ width: 130 }} />
              <col style={{ width: 150 }} />
              <col style={{ width: 158 }} />
            </colgroup>
            <thead>
              <tr>
                <th>Owner</th>
                <th>Status</th>
                <th>Cadence</th>
                <th>Harness</th>
                <th>Next</th>
                <th>Last tick</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {owners.map((owner) => (
                <OwnerRow key={owner.slug} owner={owner} />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

function OwnerRow({ owner }: { owner: OwnerView }) {
  const [busy, setBusy] = useState<string | null>(null)
  const [expanded, setExpanded] = useState(false)
  const tickRunning = typeof owner.tick_pid === 'number'
  const project = owner.project_slug ? (
    <Link className="link-subtle" href={`/project/${encodeURIComponent(owner.project_slug)}`}>
      {owner.project_slug}
    </Link>
  ) : (
    <span>ad-hoc</span>
  )

  const mutate = async (action: 'start' | 'pause' | 'retire' | 'next' | 'tick', body: unknown = {}) => {
    setBusy(action)
    try {
      const updated = await apiPost<OwnerView>(`/api/owners/${encodeURIComponent(owner.slug)}/${action}`, body)
      queryClient.setQueriesData<OwnerView[]>({ queryKey: ['owners'] }, (old) =>
        old?.map((o) => (o.slug === updated.slug ? updated : o)),
      )
      queryClient.setQueryData(['owner', owner.slug], updated)
      pushToast('ok', ownerToast(action))
      queryClient.invalidateQueries({ queryKey: ['owners'] })
      queryClient.invalidateQueries({ queryKey: ['owner', owner.slug] })
    } catch (e) {
      pushToast('error', e instanceof Error ? e.message : 'owner update failed')
    } finally {
      setBusy(null)
    }
  }

  const retire = async () => {
    const ok = await confirmAction({
      title: 'Retire this owner?',
      body: `"${owner.name}" will leave the active owner list. You can include archived owners to inspect it later.`,
      confirmLabel: 'Retire',
      danger: true,
    })
    if (ok) mutate('retire')
  }

  return (
    <>
    <tr style={{ cursor: 'default' }} className={expanded ? 'owner-row-open' : undefined}>
      <td>
        <div className="owner-name-cell">
          <button
            type="button"
            className="owner-expand-btn"
            aria-expanded={expanded}
            aria-label={expanded ? 'Collapse owner detail' : 'Expand owner detail'}
            onClick={() => setExpanded((v) => !v)}
          >
            {expanded ? <ChevronDown size={15} /> : <ChevronRight size={15} />}
          </button>
          <Bot size={15} className="dim" />
          <div style={{ minWidth: 0 }}>
            <button type="button" className="owner-name-link strong truncate" onClick={() => setExpanded((v) => !v)}>
              {owner.name}
            </button>
            <div className="subtle truncate">
              {owner.slug} · {project} · {owner.workdir_known?.name || owner.work_dir}
            </div>
          </div>
        </div>
      </td>
      <td>
        <div className="col" style={{ gap: 4, alignItems: 'flex-start' }}>
          <span className={`chip${owner.next_due ? ' warn' : ''}`}>{owner.status}</span>
          {tickRunning ? <span className="chip active">tick running</span> : null}
        </div>
      </td>
      <td>{owner.every || '—'}</td>
      <td>
        <span className="row gap subtle" style={{ gap: 6 }}>
          <ProviderIcon provider={owner.harness} size={14} />
          {owner.harness}
        </span>
      </td>
      <td title={owner.next_wake_at ? dateTime(owner.next_wake_at) : undefined}>
        <span className={owner.next_due ? 'warn-text' : ''}>{owner.next_wake_at ? ago(owner.next_wake_at) : '—'}</span>
      </td>
      <td title={tickRunning ? `pid ${owner.tick_pid}` : owner.last_tick_at ? dateTime(owner.last_tick_at) : undefined}>
        {tickRunning ? (
          <>
            <div className="warn-text">running</div>
            <div className="subtle truncate">{owner.tick_started ? `since ${ago(owner.tick_started)}` : `pid ${owner.tick_pid}`}</div>
          </>
        ) : (
          <>
            <div>{owner.last_tick_at ? ago(owner.last_tick_at) : '—'}</div>
            {owner.last_tick_status ? <div className="subtle truncate">{owner.last_tick_status}</div> : null}
          </>
        )}
      </td>
      <td>
        <div className="row wrap" style={{ justifyContent: 'flex-end', gap: 6 }}>
          {owner.status === 'active' ? (
            <button className="btn ghost sm" title="Pause owner" onClick={() => mutate('pause')} disabled={!!busy}>
              {busy === 'pause' ? <Loader2 size={14} className="spin" /> : <Pause size={14} />} Pause
            </button>
          ) : (
            <button className="btn ghost sm" title="Start owner" onClick={() => mutate('start')} disabled={!!busy}>
              {busy === 'start' ? <Loader2 size={14} className="spin" /> : <Play size={14} />} Start
            </button>
          )}
          <button className="btn ghost sm" title="Next tick in 1 hour" onClick={() => mutate('next', { in: '1h' })} disabled={!!busy}>
            {busy === 'next' ? <Loader2 size={14} className="spin" /> : <TimerReset size={14} />} +1h
          </button>
          <button className="btn ok sm" title="Dispatch a headless owner tick now" onClick={() => mutate('tick')} disabled={!!busy || owner.status !== 'active' || tickRunning}>
            {busy === 'tick' ? <Loader2 size={14} className="spin" /> : <Zap size={14} />} Tick now
          </button>
          <button className="btn ghost sm danger" title="Retire owner" onClick={retire} disabled={!!busy || owner.status === 'retired'}>
            {busy === 'retire' ? <Loader2 size={14} className="spin" /> : <Archive size={14} />} Retire
          </button>
        </div>
      </td>
    </tr>
    {expanded ? (
      <tr className="owner-detail-row">
        <td colSpan={7}>
          <OwnerDetailPanel slug={owner.slug} tickRunning={tickRunning} />
        </td>
      </tr>
    ) : null}
    </>
  )
}

function OwnerDetailPanel({ slug, tickRunning }: { slug: string; tickRunning: boolean }) {
  const { data, isLoading, error } = useOwner(slug, { poll: tickRunning })
  if (isLoading && !data) {
    return (
      <div className="owner-detail muted">
        <Loader2 size={14} className="spin" /> loading owner detail…
      </div>
    )
  }
  if (error) return <div className="owner-detail"><ErrorNote error={error} /></div>
  if (!data) return null

  // Defensive: tolerate an older server build that doesn't yet return these.
  const tasks = data.tasks ?? []
  const journal = data.journal ?? []
  const latest = journal[0]
  const earlier = journal.slice(1)
  return (
    <div className="owner-detail">
      {tickRunning ? (
        <section className="owner-detail-block">
          <div className="owner-detail-head">
            <span className="dot waiting" /> Tick activity · live
          </div>
          {data.tick_log_tail ? (
            <pre className="owner-tick-log live">{data.tick_log_tail}</pre>
          ) : (
            <div className="muted">
              <Loader2 size={13} className="spin" /> tick starting — activity will stream here…
            </div>
          )}
        </section>
      ) : null}

      <section className="owner-detail-block">
        <div className="owner-detail-head">
          Owned tasks <span className="dim">{tasks.length}</span>
        </div>
        {tasks.length === 0 ? (
          <div className="muted">No tasks tagged for this owner yet.</div>
        ) : (
          <div className="owner-task-list">
            {tasks.map((task) => (
              <OwnerTaskItem key={task.slug} task={task} />
            ))}
          </div>
        )}
      </section>

      <section className="owner-detail-block">
        <div className="owner-detail-head">
          <ScrollText size={13} />
          Last tick {data.last_tick_at ? `· ${ago(data.last_tick_at)}` : ''}
          {tickRunning ? <span className="chip active">running</span> : null}
        </div>
        {latest ? (
          <article className="owner-journal">
            <div className="owner-journal-meta">{latest.filename}</div>
            <pre className="owner-journal-body">{latest.content}</pre>
          </article>
        ) : (
          <div className="muted">No journal notes yet — the next tick will record what it dispatched here.</div>
        )}
        {earlier.length > 0 ? (
          <details className="owner-journal-more">
            <summary>{earlier.length} earlier note{earlier.length === 1 ? '' : 's'}</summary>
            {earlier.map((note) => (
              <article className="owner-journal" key={note.path}>
                <div className="owner-journal-meta">{note.filename}</div>
                <pre className="owner-journal-body">{note.content}</pre>
              </article>
            ))}
          </details>
        ) : null}
      </section>

      {!tickRunning && data.tick_log_tail ? (
        <details className="owner-detail-block">
          <summary className="owner-detail-head">Tick log (latest)</summary>
          <pre className="owner-tick-log">{data.tick_log_tail}</pre>
        </details>
      ) : null}
    </div>
  )
}

function OwnerTaskItem({ task }: { task: OwnerTaskRow }) {
  const running = task.auto_run_status === 'running' || task.status === 'in-progress'
  const done = task.status === 'done' || task.auto_run_status === 'completed'
  const dead = task.auto_run_status === 'dead'
  const tone = task.is_question ? 'warn' : dead ? 'danger' : running ? 'ok' : done ? 'info' : ''
  const label = task.is_question ? 'question' : dead ? 'dead' : running ? 'running' : done ? 'done' : task.status
  const inner = (
    <>
      <span className={`chip ${tone}`}>{label}</span>
      <span className="owner-task-name truncate" title={task.name}>{task.name}</span>
      <span className="owner-task-slug dim truncate">{task.slug}</span>
    </>
  )
  return task.has_session ? (
    <Link className="owner-task-item link" href={`/session/${encodeURIComponent(task.slug)}`}>
      {inner}
    </Link>
  ) : (
    <div className="owner-task-item">{inner}</div>
  )
}

function ownerToast(action: string): string {
  if (action === 'start') return 'owner started'
  if (action === 'pause') return 'owner paused'
  if (action === 'next') return 'owner scheduled'
  if (action === 'tick') return 'owner tick dispatched'
  if (action === 'retire') return 'owner retired'
  return 'owner updated'
}
