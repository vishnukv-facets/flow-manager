import { CircleAlert, GitPullRequestArrow, Search, ShieldAlert } from 'lucide-react'
import type { BrainGraphCounts } from '../../lib/types'

export function BrainGraphToolbar({
  counts,
  q,
  includeDone,
  expandedCount,
  onQ,
  onIncludeDone,
}: {
  counts?: BrainGraphCounts
  q: string
  includeDone: boolean
  expandedCount: number
  onQ: (value: string) => void
  onIncludeDone: (value: boolean) => void
}) {
  return (
    <div className="brain-toolbar">
      <div className="brain-toolbar-title">
        <div className="eyebrow">workspace</div>
        <h1 className="h-xl">Graph</h1>
      </div>
      <div className="brain-toolbar-counts">
        <span className="badge"><GitPullRequestArrow size={13} />{counts?.total_tasks ?? 0} tasks</span>
        <span className="badge ok">{counts?.running ?? 0} running</span>
        <span className="badge warn"><ShieldAlert size={13} />{counts?.approval_needed ?? 0} gates</span>
        <span className="badge danger"><CircleAlert size={13} />{counts?.failed ?? 0} failed</span>
        {expandedCount > 0 ? <span className="badge info">{expandedCount} expanded</span> : null}
      </div>
      <div className="brain-toolbar-actions">
        <div className="input-icon brain-search">
          <Search size={14} className="dim" />
          <input
            className="input"
            aria-label="Search Graph"
            placeholder="Search graph..."
            value={q}
            onChange={(event) => onQ(event.target.value)}
          />
        </div>
        <label className={`chip${includeDone ? ' active' : ''}`}>
          <input
            type="checkbox"
            checked={includeDone}
            onChange={(event) => onIncludeDone(event.target.checked)}
          />
          Done
        </label>
      </div>
    </div>
  )
}
