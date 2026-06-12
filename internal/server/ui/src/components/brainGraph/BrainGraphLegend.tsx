import { GitCommitHorizontal, Link2, Route, ShieldAlert } from 'lucide-react'

const EDGES = [
  { label: 'parent', icon: <Route size={13} />, className: 'parent' },
  { label: 'depends', icon: <GitCommitHorizontal size={13} />, className: 'depends_on' },
  { label: 'run', icon: <Link2 size={13} />, className: 'run_of' },
  { label: 'gate', icon: <ShieldAlert size={13} />, className: 'blocks' },
]

export function BrainGraphLegend() {
  return (
    <div className="brain-legend">
      <div className="brain-legend-row">
        <span className="badge ok">active</span>
        <span className="badge warn">gated</span>
        <span className="badge danger">failed</span>
        <span className="badge info">done</span>
      </div>
      <div className="brain-legend-row">
        {EDGES.map((edge) => (
          <span className={`brain-edge-key ${edge.className}`} key={edge.label}>
            {edge.icon}
            {edge.label}
          </span>
        ))}
      </div>
    </div>
  )
}
