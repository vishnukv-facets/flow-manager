import { Link, useLocation } from 'wouter'
import { useState, type ReactNode } from 'react'
import {
  Activity,
  AlertTriangle,
  ArrowUpRight,
  FileText,
  GitBranch,
  Info,
  ListChecks,
  Loader2,
  ScrollText,
  SendHorizontal,
  ShieldAlert,
  TerminalSquare,
} from 'lucide-react'
import { StatusDot } from '../ui'
import { Modal } from '../Modal'
import { confirmAction } from '../../lib/confirm'
import { useBrainGraphAction, useBrainGraphNodeDetail, useTaskTranscript } from '../../lib/query'
import { dateTime } from '../../lib/format'
import type {
  BrainGraphActionSpec,
  BrainGraphAuditView,
  BrainGraphApprovalDetail,
  BrainGraphEvidenceDetail,
  BrainGraphNode,
  BrainGraphPolicyView,
  BrainGraphRunDetail,
  BrainGraphTaskDetail,
  BrainGraphWarning,
  TranscriptEntry,
} from '../../lib/types'

function actionByKey(actions: BrainGraphActionSpec[], key: string) {
  return actions.find((action) => action.key === key)
}

function nodeTone(status: string) {
  switch (status) {
    case 'approval_required':
    case 'blocked':
    case 'waiting':
      return 'warn'
    case 'dead':
    case 'error':
    case 'failed':
      return 'danger'
    case 'running':
    case 'in-progress':
      return 'ok'
    case 'done':
    case 'completed':
      return 'info'
    default:
      return ''
  }
}

function errorText(error: unknown) {
  return error instanceof Error ? error.message : 'detail unavailable'
}

function displayJSON(value: unknown) {
  if (value == null) return ''
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function truncate(value: string, max = 700) {
  if (value.length <= max) return value
  return `${value.slice(0, max - 1)}…`
}

function modelLabel(run: BrainGraphRunDetail) {
  return run.resolved_model || run.requested_model || run.requested_tier || ''
}

export function BrainGraphInspector({
  selected,
  policy,
  actions,
  warnings,
}: {
  selected: BrainGraphNode | null
  policy: BrainGraphPolicyView
  actions: BrainGraphActionSpec[]
  warnings: BrainGraphWarning[]
}) {
  const nodeWarnings = selected ? warnings.filter((warning) => warning.node_id === selected.id) : []
  const detailQuery = useBrainGraphNodeDetail(selected?.id ?? null)
  const detail = detailQuery.data?.id === selected?.id ? detailQuery.data : undefined
  const detailLoading = Boolean(selected) && (detailQuery.isLoading || (detailQuery.isFetching && !detail))

  return (
    <aside className="brain-inspector">
      <div className="brain-inspector-section">
        <div className="brain-inspector-head">
          <Info size={15} />
          <span>Inspector</span>
        </div>
        {selected ? (
          <div className="brain-inspector-body">
            <NodeSummary selected={selected} actions={actions} warnings={nodeWarnings} />
            <DetailState loading={detailLoading} error={detailQuery.error} />
            {detail?.task ? <TaskDetail detail={detail.task} /> : null}
            {detail?.run ? <RunDetail detail={detail.run} /> : null}
            {detail?.approval ? <ApprovalDetail detail={detail.approval} audit={detail.audit} /> : null}
            {detail?.evidence ? <EvidenceDetail detail={detail.evidence} /> : null}
            {detail && !detail.approval && detail.audit.length > 0 ? <AuditSection audit={detail.audit} /> : null}
          </div>
        ) : (
          <div className="brain-inspector-empty">No node selected</div>
        )}
      </div>

      <PolicySummary policy={policy} warnings={warnings} />
    </aside>
  )
}

function NodeSummary({
  selected,
  actions,
  warnings,
}: {
  selected: BrainGraphNode
  actions: BrainGraphActionSpec[]
  warnings: BrainGraphWarning[]
}) {
  return (
    <>
      <div>
        <div className="brain-inspector-title">{selected.label}</div>
        <div className="brain-inspector-sub">
          <span className={`badge ${nodeTone(selected.status)}`}>
            <StatusDot status={selected.status} />
            {selected.status}
          </span>
          <span className="badge">{String(selected.type).replace(/_/g, ' ')}</span>
        </div>
      </div>

      {selected.summary ? <div className="brain-inspector-summary">{selected.summary}</div> : null}

      <div className="brain-kv">
        <KV k="id" v={selected.id} />
        {selected.task_slug ? <KV k="task" v={selected.task_slug} /> : null}
        {selected.owner_slug ? <KV k="owner" v={selected.owner_slug} /> : null}
        {selected.parent_task_slug ? <KV k="parent" v={selected.parent_task_slug} /> : null}
        {selected.provider ? <KV k="provider" v={selected.provider} /> : null}
        {selected.harness ? <KV k="harness" v={selected.harness} /> : null}
        {selected.permission_mode ? <KV k="permission" v={selected.permission_mode} /> : null}
        {selected.model ? <KV k="model" v={selected.model} /> : null}
      </div>

      {selected.ref ? <NodeRef node={selected} /> : null}
      {selected.actions && selected.actions.length > 0 ? <NodeActions node={selected} actions={actions} /> : null}
      {selected.metadata && Object.keys(selected.metadata).length > 0 ? <MetadataTable metadata={selected.metadata} /> : null}
      {warnings.length > 0 ? <Warnings warnings={warnings} /> : null}
    </>
  )
}

function DetailState({ loading, error }: { loading: boolean; error: unknown }) {
  if (loading) {
    return (
      <div className="brain-detail-state">
        <Loader2 size={14} className="spin" />
        <span>loading detail</span>
      </div>
    )
  }
  if (error) {
    return (
      <div className="brain-detail-state danger">
        <AlertTriangle size={14} />
        <span>{errorText(error)}</span>
      </div>
    )
  }
  return null
}

function TaskDetail({ detail }: { detail: BrainGraphTaskDetail }) {
  return (
    <>
      <DetailSection title="Task" icon={<FileText size={14} />}>
        <div className="brain-kv">
          <KV k="slug" v={detail.slug} />
          <KV k="status" v={detail.status} />
          <KV k="priority" v={detail.priority} />
          <KV k="project" v={detail.project_slug} />
          <KV k="parent" v={detail.parent_slug} />
          <KV k="work dir" v={detail.work_dir} />
          <KV k="worktree" v={detail.worktree_path} />
        </div>
      </DetailSection>
      <DetailSection title="Session" icon={<TerminalSquare size={14} />}>
        <div className="brain-kv">
          <KV k="provider" v={detail.session_provider} />
          <KV k="harness" v={detail.harness} />
          <KV k="permission" v={detail.permission_mode} />
          <KV k="model" v={detail.model} />
          <KV k="session" v={detail.session_id} />
          <KV k="path" v={detail.session_path} />
          {detail.transcript ? <KV k="transcript" v={detail.transcript.available ? 'available' : detail.transcript.message || 'unavailable'} /> : null}
        </div>
      </DetailSection>
      <DetailSection title="Files" icon={<ScrollText size={14} />}>
        <div className="brain-kv">
          <KV k="brief" v={detail.brief_path} />
        </div>
        {detail.updates.length > 0 ? (
          <div className="brain-detail-list">
            {detail.updates.map((update) => (
              <div className="brain-detail-list-row" key={update.path} title={update.path}>
                <span className="clip">{update.filename}</span>
                <strong>{dateTime(update.mtime)}</strong>
              </div>
            ))}
          </div>
        ) : null}
      </DetailSection>
      <TaskTranscriptPreview slug={detail.slug} enabled={Boolean(detail.session_id)} />
    </>
  )
}

function RunDetail({ detail }: { detail: BrainGraphRunDetail }) {
  return (
    <>
      <DetailSection title="Run" icon={<Activity size={14} />}>
        <div className="brain-kv">
          <KV k="run id" v={detail.run_id} />
          <KV k="task" v={detail.task_name ? `${detail.task_name} · ${detail.task_slug}` : detail.task_slug} />
          <KV k="family" v={detail.family_slug} />
          <KV k="role" v={detail.role} />
          <KV k="provider" v={detail.provider} />
          <KV k="status" v={detail.status} />
          <KV k="permission" v={detail.permission_mode} />
          <KV k="model" v={modelLabel(detail)} />
          <KV k="session" v={detail.session_id} />
          <KV k="log" v={detail.log_path} />
          <KV k="started" v={detail.started_at ? dateTime(detail.started_at) : ''} />
          <KV k="finished" v={detail.finished_at ? dateTime(detail.finished_at) : ''} />
        </div>
      </DetailSection>
      {detail.input_summary ? <TextBlock label="input" value={detail.input_summary} /> : null}
      {detail.error_text ? <TextBlock label="error" value={detail.error_text} tone="danger" /> : null}
      <JsonPreview label="output" value={detail.output_json} />
      <JsonPreview label="evidence" value={detail.evidence_json} />
    </>
  )
}

function ApprovalDetail({ detail, audit }: { detail: BrainGraphApprovalDetail; audit: BrainGraphAuditView[] }) {
  return (
    <>
      <DetailSection title="Approval" icon={<ShieldAlert size={14} />}>
        <div className="brain-kv">
          <KV k="action" v={detail.action} />
          <KV k="task" v={detail.task_name ? `${detail.task_name} · ${detail.task_slug}` : detail.task_slug} />
          <KV k="policy" v={detail.policy_mode} />
        </div>
      </DetailSection>
      {audit.length > 0 ? <AuditSection audit={audit} /> : null}
    </>
  )
}

function AuditSection({ audit }: { audit: BrainGraphAuditView[] }) {
  return (
    <DetailSection title="Audit" icon={<ListChecks size={14} />}>
      <div className="brain-audit-list">
        {audit.map((item) => (
          <div className="brain-audit-row" key={item.id}>
            <div className="brain-audit-top">
              <span className={`badge ${item.result === 'allowed' || item.result === 'sent' || item.result === 'opened' ? 'ok' : item.result === 'blocked' ? 'warn' : item.result === 'error' ? 'danger' : ''}`}>{item.result}</span>
              <strong>{item.action}</strong>
              <span className="faint">{dateTime(item.created_at)}</span>
            </div>
            <div className="brain-audit-sub">
              {item.actor} · {item.policy}
            </div>
            <JsonPreview label="evidence" value={item.evidence_json} compact />
            {item.error_text ? <TextBlock label="error" value={item.error_text} tone="danger" /> : null}
          </div>
        ))}
      </div>
    </DetailSection>
  )
}

function EvidenceDetail({ detail }: { detail: BrainGraphEvidenceDetail }) {
  return (
    <DetailSection title="Evidence" icon={<ScrollText size={14} />}>
      <div className="brain-kv">
        <KV k="kind" v={detail.kind} />
        <KV k="task" v={detail.task_slug} />
        <KV k="ref" v={detail.ref_id} />
        <KV k="state" v={detail.available ? 'available' : detail.message || 'unavailable'} />
        <KV k="path" v={detail.path} />
        <KV k="url" v={detail.url} />
      </div>
    </DetailSection>
  )
}

function DetailSection({ title, icon, children }: { title: string; icon: ReactNode; children: ReactNode }) {
  return (
    <div className="brain-detail-section">
      <div className="brain-detail-heading">
        {icon}
        <span>{title}</span>
      </div>
      {children}
    </div>
  )
}

function TextBlock({ label, value, tone }: { label: string; value: string; tone?: 'danger' }) {
  return (
    <div className={`brain-detail-text ${tone ?? ''}`}>
      <div className="eyebrow">{label}</div>
      <div>{value}</div>
    </div>
  )
}

function JsonPreview({ label, value, compact = false }: { label: string; value: unknown; compact?: boolean }) {
  const text = truncate(displayJSON(value), compact ? 240 : 700)
  if (!text) return null
  return (
    <div className={`brain-json-preview${compact ? ' compact' : ''}`}>
      <div className="eyebrow">{label}</div>
      <pre>{text}</pre>
    </div>
  )
}

function KV({ k, v }: { k: string; v?: string | number | null }) {
  const text = v == null || v === '' ? '—' : String(v)
  return (
    <div className="brain-kv-row">
      <span>{k}</span>
      <strong title={text}>{text}</strong>
    </div>
  )
}

function NodeRef({ node }: { node: BrainGraphNode }) {
  if (!node.ref) return null
  if (node.ref.kind === 'task') {
    return (
      <Link className="brain-ref-link" href={`/session/${encodeURIComponent(node.ref.id)}`}>
        <GitBranch size={14} />
        <span className="clip">{node.ref.id}</span>
        <ArrowUpRight size={13} />
      </Link>
    )
  }
  if (node.ref.url) {
    return (
      <a className="brain-ref-link" href={node.ref.url} target="_blank" rel="noreferrer">
        <GitBranch size={14} />
        <span className="clip">{node.ref.id}</span>
        <ArrowUpRight size={13} />
      </a>
    )
  }
  return (
    <div className="brain-ref-static">
      <GitBranch size={14} />
      <span className="clip">{node.ref.kind}:{node.ref.id}</span>
    </div>
  )
}

function NodeActions({ node, actions }: { node: BrainGraphNode; actions: BrainGraphActionSpec[] }) {
  const graphAction = useBrainGraphAction()
  const [, navigate] = useLocation()
  const [promptAction, setPromptAction] = useState<BrainGraphActionSpec | null>(null)
  const [prompt, setPrompt] = useState('')
  const pendingAction = graphAction.isPending ? graphAction.variables?.action : ''

  const run = async (key: string, action?: BrainGraphActionSpec) => {
    const enabled = action?.enabled ?? true
    if (!enabled || graphAction.isPending) return
    if (key === 'seed' || key === 'send_event') {
      setPrompt('')
      setPromptAction(action ?? { key, label: key.replace(/_/g, ' '), risky: false, enabled: true })
      return
    }
    let confirm = false
    if (key === 'approve') {
      const ok = await confirmAction({
        title: action?.label || 'Approve action',
        body: `Set ${node.metadata?.action || 'this action'} to auto for the Brain policy.`,
        confirmLabel: 'Approve',
        cancelLabel: 'Cancel',
        danger: true,
      })
      if (!ok) return
      confirm = true
    }
    try {
      const resp = await graphAction.mutateAsync({ action: key, node_id: node.id, confirm })
      if ((key === 'open_session' || key === 'resume') && resp.action_response?.bridge) {
        const slug = resp.action_response.agent?.slug || node.task_slug
        if (slug) navigate(`/session/${encodeURIComponent(slug)}`)
      }
    } catch {
      // The hook emits the toast; keep the click handler quiet.
    }
  }
  const submitPromptAction = async () => {
    const text = prompt.trim()
    if (!promptAction || !text || graphAction.isPending) return
    try {
      await graphAction.mutateAsync({ action: promptAction.key, node_id: node.id, prompt: text })
      setPrompt('')
      setPromptAction(null)
    } catch {
      // The hook emits the toast; keep the modal open so the text can be retried.
    }
  }

  return (
    <>
      <div className="brain-action-list">
        {(node.actions ?? []).map((key) => {
          const action = actionByKey(actions, key)
          const enabled = action?.enabled ?? true
          const pending = pendingAction === key
          return (
            <button
              type="button"
              className={`brain-action-button ${action?.risky ? 'risky' : ''}`}
              key={key}
              title={action?.disabled_reason || undefined}
              disabled={!enabled || graphAction.isPending}
              aria-busy={pending}
              onClick={() => void run(key, action)}
            >
              {pending ? <Loader2 size={13} className="spin" /> : null}
              <span>{action?.label ?? key.replace(/_/g, ' ')}</span>
            </button>
          )
        })}
      </div>
      <Modal
        open={Boolean(promptAction)}
        onClose={() => {
          if (graphAction.isPending) return
          setPromptAction(null)
          setPrompt('')
        }}
        title={promptAction?.label ?? 'Send input'}
        width={560}
        footer={
          <>
            <button type="button" className="btn" disabled={graphAction.isPending} onClick={() => setPromptAction(null)}>
              Cancel
            </button>
            <button type="button" className="btn primary" disabled={!prompt.trim() || graphAction.isPending} onClick={submitPromptAction}>
              {graphAction.isPending ? <Loader2 size={14} className="spin" /> : <SendHorizontal size={14} />}
              Send
            </button>
          </>
        }
      >
        <div className="brain-action-modal">
          <div className="brain-action-modal-target">
            <span>task</span>
            <strong>{node.task_slug || node.id}</strong>
          </div>
          <textarea
            className="textarea brain-action-prompt"
            aria-label={promptAction?.key === 'seed' ? 'Seed input' : 'Session event'}
            rows={6}
            value={prompt}
            disabled={graphAction.isPending}
            placeholder={promptAction?.key === 'seed' ? 'Seed input for this task session…' : 'Event to send into this task session…'}
            onChange={(e) => setPrompt(e.target.value)}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
                e.preventDefault()
                void submitPromptAction()
              }
            }}
          />
        </div>
      </Modal>
    </>
  )
}

function TaskTranscriptPreview({ slug, enabled }: { slug: string; enabled: boolean }) {
  const { data, isLoading, error } = useTaskTranscript(slug, enabled)
  if (!enabled) return null
  if (isLoading) {
    return (
      <DetailSection title="Transcript" icon={<ScrollText size={14} />}>
        <div className="brain-detail-state">
          <Loader2 size={14} className="spin" />
          <span>loading transcript</span>
        </div>
      </DetailSection>
    )
  }
  if (error) {
    return (
      <DetailSection title="Transcript" icon={<ScrollText size={14} />}>
        <div className="brain-detail-state danger">
          <AlertTriangle size={14} />
          <span>{errorText(error)}</span>
        </div>
      </DetailSection>
    )
  }
  if (!data?.available || data.entries.length === 0) {
    return (
      <DetailSection title="Transcript" icon={<ScrollText size={14} />}>
        <div className="brain-detail-text">{data?.message || 'No transcript captured yet.'}</div>
      </DetailSection>
    )
  }
  const entries = data.entries.slice(-6)
  return (
    <DetailSection title="Transcript" icon={<ScrollText size={14} />}>
      <div className="brain-transcript-list">
        {entries.map((entry, index) => (
          <div className={`brain-transcript-row ${entry.is_error ? 'danger' : ''}`} key={`${entry.byte_offset}:${index}`}>
            <div className="brain-transcript-meta">
              <span>{entry.type}</span>
              {entry.timestamp ? <strong>{dateTime(entry.timestamp)}</strong> : null}
            </div>
            <div className="brain-transcript-text">{transcriptEntryText(entry)}</div>
          </div>
        ))}
      </div>
    </DetailSection>
  )
}

function transcriptEntryText(entry: TranscriptEntry) {
  if (entry.type === 'tool_use') return `${entry.tool_name ?? 'tool'} ${entry.tool_input_summary ?? ''}`.trim()
  if (entry.type === 'tool_result') return entry.tool_result_text || ''
  return entry.text || ''
}

function MetadataTable({ metadata }: { metadata: Record<string, string> }) {
  return (
    <div className="brain-metadata">
      {Object.entries(metadata).map(([key, value]) => (
        <KV key={key} k={key} v={value} />
      ))}
    </div>
  )
}

function Warnings({ warnings }: { warnings: BrainGraphWarning[] }) {
  return (
    <div className="brain-warning-list">
      {warnings.map((warning) => (
        <div className="brain-warning" key={`${warning.code}:${warning.node_id}:${warning.message}`}>
          <AlertTriangle size={14} />
          <span>{warning.message}</span>
        </div>
      ))}
    </div>
  )
}

function PolicySummary({ policy, warnings }: { policy: BrainGraphPolicyView; warnings: BrainGraphWarning[] }) {
  return (
    <div className="brain-inspector-section">
      <div className="brain-inspector-head">
        <ShieldAlert size={15} />
        <span>Policy</span>
      </div>
      <div className="brain-kv">
        <KV k="mode" v={policy.full_auto ? 'full_auto' : 'approval_gated'} />
        <KV k="review" v={`${policy.approval_required.length} actions`} />
        <KV k="whitelist" v={`${policy.risky_whitelist.length} actions`} />
        {policy.last_decision_at ? <KV k="decision" v={dateTime(policy.last_decision_at)} /> : null}
        {policy.last_decision_state ? <KV k="state" v={policy.last_decision_state} /> : null}
        <KV k="warnings" v={String(warnings.length)} />
      </div>
      {warnings.length > 0 ? <Warnings warnings={warnings.slice(0, 3)} /> : null}
    </div>
  )
}
