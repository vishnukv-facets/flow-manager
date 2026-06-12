import { useEffect, useMemo, useState } from 'react'
import { Link } from 'wouter'
import { AlertTriangle, ArrowUpRight, Boxes, GitBranch, Info, ShieldAlert } from 'lucide-react'
import { BrainGraphCanvas } from '../components/brainGraph/BrainGraphCanvas'
import { BrainGraphLegend } from '../components/brainGraph/BrainGraphLegend'
import { BrainGraphToolbar } from '../components/brainGraph/BrainGraphToolbar'
import { OwnerBoundary } from '../components/brainGraph/OwnerBoundary'
import { EmptyState, ErrorNote, Loading, StatusDot } from '../components/ui'
import { useBrainGraph } from '../lib/query'
import { useDocumentTitle } from '../lib/useDocumentTitle'
import { dateTime } from '../lib/format'
import type { BrainGraphActionSpec, BrainGraphNode, BrainGraphPolicyView, BrainGraphWarning } from '../lib/types'

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

export function BrainGraph() {
  useDocumentTitle('Brain Graph')
  const [q, setQ] = useState('')
  const [includeDone, setIncludeDone] = useState(false)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const expand = useMemo(() => [...expanded].sort(), [expanded])
  const { data, isLoading, error, isFetching } = useBrainGraph({ q, includeDone, expand })

  const selected = useMemo(
    () => data?.nodes.find((node) => node.id === selectedId) ?? null,
    [data?.nodes, selectedId],
  )

  useEffect(() => {
    if (!data || !selectedId) return
    if (!data.nodes.some((node) => node.id === selectedId)) setSelectedId(null)
  }, [data, selectedId])

  const selectNode = (node: BrainGraphNode) => {
    setSelectedId(node.id)
    if (node.type !== 'task' || expanded.has(node.id)) return
    setExpanded((prev) => {
      if (prev.has(node.id)) return prev
      const next = new Set(prev)
      next.add(node.id)
      return next
    })
  }

  return (
    <div className="page brain-page">
      <BrainGraphToolbar
        counts={data?.counts}
        q={q}
        includeDone={includeDone}
        expandedCount={expanded.size}
        onQ={setQ}
        onIncludeDone={setIncludeDone}
      />

      {isLoading ? (
        <Loading label="loading graph" />
      ) : error ? (
        <ErrorNote error={error} />
      ) : !data || data.nodes.length === 0 ? (
        <EmptyState icon={<Boxes size={30} />} title="No graph nodes" hint="No visible Brain graph nodes match the current filters." />
      ) : (
        <div className="brain-shell">
          <div className="brain-main">
            <div className="brain-surface">
              <div className="brain-surface-head">
                <OwnerBoundary owners={data.owners} selectedOwner={selected?.owner_slug} />
                <div className="brain-freshness">
                  <span className={`dot ${isFetching ? 'waiting' : 'done'}`} />
                  {isFetching ? 'refreshing' : data.freshness}
                </div>
              </div>
              <BrainGraphCanvas
                nodes={data.nodes}
                edges={data.edges}
                owners={data.owners}
                selectedId={selectedId}
                onSelectNode={selectNode}
                onClearSelection={() => setSelectedId(null)}
              />
            </div>
            <BrainGraphLegend />
          </div>

          <BrainGraphInspector
            selected={selected}
            policy={data.policy}
            actions={data.selected_actions}
            warnings={data.warnings}
          />
        </div>
      )}
    </div>
  )
}

function BrainGraphInspector({
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

  return (
    <aside className="brain-inspector">
      <div className="brain-inspector-section">
        <div className="brain-inspector-head">
          <Info size={15} />
          <span>Inspector</span>
        </div>
        {selected ? (
          <div className="brain-inspector-body">
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
            {nodeWarnings.length > 0 ? <Warnings warnings={nodeWarnings} /> : null}
          </div>
        ) : (
          <div className="brain-inspector-empty">No node selected</div>
        )}
      </div>

      <PolicySummary policy={policy} warnings={warnings} />
    </aside>
  )
}

function KV({ k, v }: { k: string; v: string }) {
  return (
    <div className="brain-kv-row">
      <span>{k}</span>
      <strong title={v}>{v}</strong>
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
  return (
    <div className="brain-action-list">
      {(node.actions ?? []).map((key) => {
        const action = actionByKey(actions, key)
        return (
          <span className={`badge ${action?.risky ? 'warn' : ''}`} key={key} title={action?.disabled_reason || undefined}>
            {action?.label ?? key.replace(/_/g, ' ')}
          </span>
        )
      })}
    </div>
  )
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
  const approvalRequired = policy.approval_required ?? []
  const riskyWhitelist = policy.risky_whitelist ?? []
  return (
    <div className="brain-inspector-section">
      <div className="brain-inspector-head">
        <ShieldAlert size={15} />
        <span>Policy</span>
      </div>
      <div className="brain-kv">
        <KV k="mode" v={policy.full_auto ? 'full_auto' : 'approval_gated'} />
        <KV k="review" v={`${approvalRequired.length} actions`} />
        <KV k="whitelist" v={`${riskyWhitelist.length} actions`} />
        {policy.last_decision_at ? <KV k="decision" v={dateTime(policy.last_decision_at)} /> : null}
        {policy.last_decision_state ? <KV k="state" v={policy.last_decision_state} /> : null}
        <KV k="warnings" v={String(warnings.length)} />
      </div>
      {warnings.length > 0 ? <Warnings warnings={warnings.slice(0, 3)} /> : null}
    </div>
  )
}
