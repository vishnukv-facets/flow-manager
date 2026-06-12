import { useEffect, useMemo, useState, type ComponentType } from 'react'
import {
  Background,
  BackgroundVariant,
  Controls,
  MarkerType,
  MiniMap,
  ReactFlow,
  type Edge,
  type Node,
  type NodeMouseHandler,
  type NodeProps,
  type ReactFlowInstance,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { BrainGraphNode } from './BrainGraphNode'
import type { BrainGraphEdge, BrainGraphNode as BrainGraphNodeView, BrainGraphOwnerView } from '../../lib/types'

type FlowNode = Node<BrainGraphNodeView, 'brain'>
type FlowEdge = Edge<{ edgeType: string }>

const NODE_W = 286
const NODE_H = 126
const OWNER_X = 366
const ROW_Y = 156
const CHILD_X = 332
const CHILD_Y = 132

const STATUS_RANK: Record<string, number> = {
  'approval_required': 0,
  'in-progress': 1,
  running: 1,
  backlog: 2,
  available: 3,
  linked: 3,
  done: 4,
  completed: 4,
  dead: 5,
  error: 5,
}
const TYPE_RANK: Record<string, number> = {
  task: 0,
  approval: 1,
  worker_run: 2,
  validator_run: 3,
  steward_run: 4,
  transcript_ref: 5,
  github_ref: 6,
}
const EDGE_COLOR: Record<string, string> = {
  parent: 'var(--accent-line)',
  depends_on: 'var(--warn)',
  run_of: 'var(--info)',
  external_ref: 'var(--text-3)',
  blocks: 'var(--danger)',
}

const nodeTypes = {
  brain: BrainGraphNode as unknown as ComponentType<NodeProps>,
}

function rankNode(node: BrainGraphNodeView) {
  return (STATUS_RANK[node.status] ?? 9) * 10 + (TYPE_RANK[node.type] ?? 8)
}

function ownerOrder(owners: BrainGraphOwnerView[], nodes: BrainGraphNodeView[]) {
  const ownerSet = new Set(nodes.map((node) => node.owner_slug || 'unowned'))
  const ordered: string[] = []
  const orderedSet = new Set<string>()
  for (const owner of owners) {
    if (ownerSet.has(owner.slug)) {
      ordered.push(owner.slug)
      orderedSet.add(owner.slug)
    }
  }
  for (const slug of ownerSet) {
    if (!orderedSet.has(slug)) ordered.push(slug)
  }
  return ordered.length ? ordered : ['unowned']
}

function layoutNodes(nodes: BrainGraphNodeView[], owners: BrainGraphOwnerView[], selectedId?: string | null): FlowNode[] {
  const ownerIndex = new Map(ownerOrder(owners, nodes).map((owner, index) => [owner, index]))
  const taskNodes = nodes
    .filter((node) => node.type === 'task')
    .slice()
    .sort((a, b) => {
      const ax = ownerIndex.get(a.owner_slug || 'unowned') ?? 0
      const bx = ownerIndex.get(b.owner_slug || 'unowned') ?? 0
      if (ax !== bx) return ax - bx
      const ar = rankNode(a)
      const br = rankNode(b)
      if (ar !== br) return ar - br
      return a.label.localeCompare(b.label)
    })

  const placed = new Map<string, { x: number; y: number }>()
  const ownerRows = new Map<string, number>()
  for (const task of taskNodes) {
    const owner = task.owner_slug || 'unowned'
    const row = ownerRows.get(owner) ?? 0
    ownerRows.set(owner, row + 1)
    placed.set(task.id, {
      x: (ownerIndex.get(owner) ?? 0) * OWNER_X,
      y: row * ROW_Y,
    })
  }

  const childRows = new Map<string, number>()
  const orphanRows = new Map<string, number>()
  for (const node of nodes) {
    if (placed.has(node.id)) continue
    const taskId = node.task_slug ? `task:${node.task_slug}` : ''
    const parent = taskId ? placed.get(taskId) : undefined
    if (parent) {
      const row = childRows.get(taskId) ?? 0
      childRows.set(taskId, row + 1)
      placed.set(node.id, {
        x: parent.x + CHILD_X,
        y: parent.y + row * CHILD_Y,
      })
      continue
    }
    const owner = node.owner_slug || 'unowned'
    const row = orphanRows.get(owner) ?? 0
    orphanRows.set(owner, row + 1)
    placed.set(node.id, {
      x: (ownerIndex.get(owner) ?? 0) * OWNER_X + CHILD_X,
      y: row * CHILD_Y,
    })
  }

  return nodes.map((node) => ({
    id: node.id,
    type: 'brain',
    data: node,
    position: placed.get(node.id) ?? { x: 0, y: 0 },
    width: NODE_W,
    height: NODE_H,
    selected: node.id === selectedId,
  }))
}

function flowEdges(edges: BrainGraphEdge[]): FlowEdge[] {
  return edges.map((edge) => {
    const color = EDGE_COLOR[edge.type] ?? 'var(--border-strong)'
    return {
      id: edge.id,
      source: edge.source,
      target: edge.target,
      label: edge.label || undefined,
      type: 'smoothstep',
      data: { edgeType: edge.type },
      markerEnd: { type: MarkerType.ArrowClosed, color },
      style: { stroke: color, strokeWidth: edge.status === 'blocked' ? 2 : 1.4 },
      labelStyle: { fill: 'var(--text-3)', fontSize: 10, fontFamily: 'var(--font-mono)' },
      labelBgStyle: { fill: 'var(--bg-2)', fillOpacity: 0.9 },
    }
  })
}

function miniMapColor(node: Node) {
  const data = node.data as unknown as BrainGraphNodeView
  if (data.type === 'approval' || data.status === 'approval_required') return 'var(--warn)'
  if (data.status === 'dead' || data.status === 'error') return 'var(--danger)'
  if (data.status === 'running' || data.status === 'in-progress') return 'var(--ok)'
  if (data.type === 'task') return 'var(--accent)'
  return 'var(--info)'
}

export function BrainGraphCanvas({
  nodes,
  edges,
  owners,
  selectedId,
  onSelectNode,
  onClearSelection,
}: {
  nodes: BrainGraphNodeView[]
  edges: BrainGraphEdge[]
  owners: BrainGraphOwnerView[]
  selectedId?: string | null
  onSelectNode: (node: BrainGraphNodeView) => void
  onClearSelection: () => void
}) {
  const [instance, setInstance] = useState<ReactFlowInstance<FlowNode, FlowEdge> | null>(null)
  const flowNodes = useMemo(() => layoutNodes(nodes, owners, selectedId), [nodes, owners, selectedId])
  const flowEdgeList = useMemo(() => flowEdges(edges), [edges])

  useEffect(() => {
    if (!instance || flowNodes.length === 0) return
    const timer = window.setTimeout(() => {
      instance.fitView({ padding: 0.18, duration: 240 })
    }, 40)
    return () => window.clearTimeout(timer)
  }, [instance, flowNodes.length, edges.length])

  const onNodeClick: NodeMouseHandler<FlowNode> = (_event, node) => onSelectNode(node.data)

  return (
    <div className="brain-canvas">
      <ReactFlow<FlowNode, FlowEdge>
        nodes={flowNodes}
        edges={flowEdgeList}
        nodeTypes={nodeTypes}
        onInit={setInstance}
        onNodeClick={onNodeClick}
        onPaneClick={onClearSelection}
        fitView
        minZoom={0.22}
        maxZoom={1.25}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable
        proOptions={{ hideAttribution: true }}
      >
        <Background variant={BackgroundVariant.Dots} gap={18} size={1} />
        <Controls showInteractive={false} />
        <MiniMap
          pannable
          zoomable
          nodeColor={miniMapColor}
          nodeStrokeWidth={2}
          maskColor="color-mix(in srgb, var(--bg) 72%, transparent)"
        />
      </ReactFlow>
    </div>
  )
}
