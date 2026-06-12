import { Bot, CircleDotDashed, Zap } from 'lucide-react'
import type { NodeProps } from '@xyflow/react'
import { StatusDot } from '../ui'
import type { BrainGraphOwnerView } from '../../lib/types'

export function OwnerBoundary({ owners, selectedOwner }: { owners: BrainGraphOwnerView[]; selectedOwner?: string }) {
  const rows = owners.filter((owner) => owner.task_count > 0 || owner.slug === selectedOwner)

  return (
    <div className="brain-owner-strip" aria-label="Owner summary">
      {rows.length === 0 ? (
        <div className="brain-owner-empty">
          <CircleDotDashed size={14} />
          <span>0 owners</span>
        </div>
      ) : (
        rows.map((owner) => (
          <div className={`brain-owner-pill${owner.slug === selectedOwner ? ' active' : ''}`} key={owner.id || owner.slug}>
            <Bot size={14} />
            <span className="clip">{owner.name || owner.slug}</span>
            <span className="mono">{owner.task_count}</span>
            <OwnerMetricBadges owner={owner} compact />
          </div>
        ))
      )}
    </div>
  )
}

export interface OwnerGroupData extends Record<string, unknown> {
  owner: BrainGraphOwnerView
}

function ownerTitle(owner: BrainGraphOwnerView) {
  return owner.name || owner.slug || 'Unowned'
}

function OwnerMetricBadges({ owner, compact = false }: { owner: BrainGraphOwnerView; compact?: boolean }) {
  return (
    <div className={`brain-owner-metrics${compact ? ' compact' : ''}`}>
      {!compact ? <span className="badge">{owner.task_count} tasks</span> : null}
      {owner.running_count > 0 || !compact ? (
        <span className={`badge ${owner.running_count > 0 ? 'ok' : ''}`}>
          <Zap size={11} />
          {owner.running_count} run
        </span>
      ) : null}
    </div>
  )
}

export function OwnerGroupNode({ data, selected }: NodeProps) {
  const owner = (data as OwnerGroupData).owner

  return (
    <section className={`brain-owner-group${selected ? ' selected' : ''}`} aria-label={`Owner ${ownerTitle(owner)}`}>
      <div className="brain-owner-group-head">
        <div className="brain-owner-group-title">
          <Bot size={15} />
          <div className="brain-owner-group-name">
            <strong title={ownerTitle(owner)}>{ownerTitle(owner)}</strong>
            <span title={owner.slug}>{owner.slug}</span>
          </div>
        </div>
        <span className="badge brain-owner-status">
          <StatusDot status={owner.status} />
          {owner.status || 'unknown'}
        </span>
      </div>
      <OwnerMetricBadges owner={owner} />
    </section>
  )
}
