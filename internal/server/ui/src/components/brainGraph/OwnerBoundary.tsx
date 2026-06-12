import { Bot, CircleDotDashed } from 'lucide-react'
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
            {owner.running_count > 0 ? <span className="badge ok">{owner.running_count} run</span> : null}
            {owner.approval_count > 0 ? <span className="badge warn">{owner.approval_count} gate</span> : null}
          </div>
        ))
      )}
    </div>
  )
}
