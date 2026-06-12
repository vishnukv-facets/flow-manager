import { GitBranch, ShieldCheck, TerminalSquare } from 'lucide-react'
import { Handle, Position } from '@xyflow/react'
import { ProviderIcon, StatusDot } from '../ui'
import type { BrainGraphNode as BrainGraphNodeView } from '../../lib/types'

const TYPE_LABEL: Record<string, string> = {
  task: 'task',
  worker_run: 'worker',
  validator_run: 'validator',
  steward_run: 'steward',
  approval: 'approval',
  transcript_ref: 'transcript',
  github_ref: 'github',
  event: 'event',
  transcript: 'transcript',
  log: 'log',
  pr: 'pr',
  closeout: 'closeout',
  owner: 'owner',
}

function statusTone(status: string) {
  switch (status) {
    case 'running':
    case 'in-progress':
    case 'available':
    case 'linked':
      return 'ok'
    case 'approval_required':
    case 'blocked':
    case 'waiting':
      return 'warn'
    case 'dead':
    case 'error':
    case 'failed':
      return 'danger'
    case 'done':
    case 'completed':
      return 'info'
    default:
      return ''
  }
}

function shortType(type: string) {
  return TYPE_LABEL[type] ?? type.replace(/_/g, ' ')
}

function displayMeta(node: BrainGraphNodeView) {
  const values = [
    node.provider,
    node.harness,
    node.permission_mode,
    node.model,
  ].filter(Boolean) as string[]
  return Array.from(new Set(values))
}

export function BrainGraphNode({ data, selected }: { data: BrainGraphNodeView; selected?: boolean }) {
  const meta = displayMeta(data)
  const badges = (data.badges ?? []).slice(0, 4)
  const hiddenBadges = Math.max(0, (data.badges?.length ?? 0) - badges.length)

  return (
    <div className={`brain-node brain-node-${data.type}${selected ? ' selected' : ''}`}>
      <Handle type="target" position={Position.Left} className="brain-node-handle" />
      <Handle type="source" position={Position.Right} className="brain-node-handle" />
      <div className="brain-node-top">
        <span className={`badge ${statusTone(data.status)}`}>
          <StatusDot status={data.status} />
          {shortType(data.type)}
        </span>
        {data.priority ? <span className={`prio ${data.priority}`}>{data.priority}</span> : null}
      </div>

      <div className="brain-node-title" title={data.label}>{data.label}</div>
      {data.summary ? <div className="brain-node-summary" title={data.summary}>{data.summary}</div> : null}

      {meta.length > 0 ? (
        <div className="brain-node-meta">
          {data.provider ? (
            <span className="brain-node-meta-chip">
              <ProviderIcon provider={data.provider} size={13} />
              {data.provider}
            </span>
          ) : null}
          {meta.filter((m) => m !== data.provider).slice(0, 3).map((m) => (
            <span className="brain-node-meta-chip" key={m}>
              {m === data.harness ? <TerminalSquare size={12} /> : m === data.permission_mode ? <ShieldCheck size={12} /> : <GitBranch size={12} />}
              {m}
            </span>
          ))}
        </div>
      ) : null}

      {badges.length > 0 ? (
        <div className="brain-node-badges">
          {badges.map((badge) => (
            <span className="badge" key={badge}>{badge}</span>
          ))}
          {hiddenBadges > 0 ? <span className="badge">+{hiddenBadges}</span> : null}
        </div>
      ) : null}
    </div>
  )
}
