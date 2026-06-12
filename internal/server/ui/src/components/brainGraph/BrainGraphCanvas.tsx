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
  type OnSelectionChangeFunc,
  type ReactFlowInstance,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { BrainGraphNode } from './BrainGraphNode'
import { OwnerGroupNode, type OwnerGroupData } from './OwnerBoundary'
import type { BrainGraphEdge, BrainGraphNode as BrainGraphNodeView, BrainGraphOwnerView } from '../../lib/types'

type BrainFlowNode = Node<BrainGraphNodeView, 'brain'>
type OwnerFlowNode = Node<OwnerGroupData, 'ownerGroup'>
type FlowNode = BrainFlowNode | OwnerFlowNode
type FlowEdge = Edge<{ edgeType: string }>

const NODE_W = 286
const NODE_H = 126
const CHILD_X = 332
const CHILD_Y = 132
const ROW_GAP = 34
const OWNER_GAP_X = 96
const OWNER_PAD_X = 24
const OWNER_PAD_TOP = 88
const OWNER_PAD_BOTTOM = 24
const OWNER_MIN_W = 362
const OWNER_MIN_H = 280

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
  ownerGroup: OwnerGroupNode as unknown as ComponentType<NodeProps>,
}

function rankNode(node: BrainGraphNodeView) {
  return (STATUS_RANK[node.status] ?? 9) * 10 + (TYPE_RANK[node.type] ?? 8)
}

function effectiveOwner(node: BrainGraphNodeView, taskOwners: Map<string, string>) {
  if (node.owner_slug) return node.owner_slug
  if (node.task_slug) return taskOwners.get(node.task_slug) || 'unowned'
  return 'unowned'
}

function ownerOrder(owners: BrainGraphOwnerView[], ownerSet: Set<string>) {
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

function ownerSummary(
  slug: string,
  ownerBySlug: Map<string, BrainGraphOwnerView>,
  effectiveOwners: Map<string, string>,
  nodes: BrainGraphNodeView[],
): BrainGraphOwnerView {
  const existing = ownerBySlug.get(slug)
  const summary: BrainGraphOwnerView = existing
    ? { ...existing }
    : {
        id: `owner:${slug}`,
        slug,
        name: slug === 'unowned' ? 'Unowned' : slug,
        status: slug === 'unowned' ? 'active' : 'missing',
        task_count: 0,
        running_count: 0,
        blocked_count: 0,
        approval_count: 0,
      }

  if (!existing) {
    for (const node of nodes) {
      if (effectiveOwners.get(node.id) !== slug) continue
      if (node.type === 'task') summary.task_count++
      if (node.type === 'task' && (node.status === 'running' || node.status === 'in-progress')) summary.running_count++
      if (node.type === 'approval' || node.status === 'approval_required') summary.approval_count++
      if (node.status === 'blocked' || node.status === 'approval_required') summary.blocked_count++
    }
  }

  return summary
}

function taskSlugFromNode(node: BrainGraphNodeView) {
  if (node.type === 'task') return node.task_slug || node.id.replace(/^task:/, '')
  return node.task_slug || ''
}

function appendToBucket<K, V>(map: Map<K, V[]>, key: K, value: V) {
  const existing = map.get(key)
  if (existing) {
    existing.push(value)
    return
  }
  map.set(key, [value])
}

function layoutNodes(
  nodes: BrainGraphNodeView[],
  owners: BrainGraphOwnerView[],
  selectedId?: string | null,
  selectedOwner?: string | null,
): FlowNode[] {
  const taskOwners = new Map<string, string>()
  for (const node of nodes) {
    if (node.type !== 'task') continue
    taskOwners.set(taskSlugFromNode(node), node.owner_slug || 'unowned')
  }

  const effectiveOwners = new Map<string, string>()
  const visibleOwners = new Set<string>()
  for (const node of nodes) {
    const owner = effectiveOwner(node, taskOwners)
    effectiveOwners.set(node.id, owner)
    visibleOwners.add(owner)
  }

  const ownerBySlug = new Map(owners.map((owner) => [owner.slug, owner]))
  const orderedOwners = ownerOrder(owners, visibleOwners)
  const ownerIndex = new Map(orderedOwners.map((owner, index) => [owner, index]))

  const taskNodes = nodes
    .filter((node) => node.type === 'task')
    .slice()
    .sort((a, b) => {
      const ax = ownerIndex.get(effectiveOwners.get(a.id) || 'unowned') ?? 0
      const bx = ownerIndex.get(effectiveOwners.get(b.id) || 'unowned') ?? 0
      if (ax !== bx) return ax - bx
      const ar = rankNode(a)
      const br = rankNode(b)
      if (ar !== br) return ar - br
      return a.label.localeCompare(b.label)
    })

  const childrenByTask = new Map<string, BrainGraphNodeView[]>()
  const orphanNodes = new Map<string, BrainGraphNodeView[]>()
  const taskIdBySlug = new Map(taskNodes.map((node) => [taskSlugFromNode(node), node.id]))
  for (const node of nodes) {
    if (node.type === 'task') continue
    const taskSlug = node.task_slug || ''
    if (taskSlug && taskIdBySlug.has(taskSlug)) {
      const key = taskIdBySlug.get(taskSlug) || ''
      appendToBucket(childrenByTask, key, node)
      continue
    }
    const owner = effectiveOwners.get(node.id) || 'unowned'
    appendToBucket(orphanNodes, owner, node)
  }
  for (const children of childrenByTask.values()) {
    children.sort((a, b) => {
      const ar = rankNode(a)
      const br = rankNode(b)
      if (ar !== br) return ar - br
      return a.label.localeCompare(b.label)
    })
  }
  for (const children of orphanNodes.values()) {
    children.sort((a, b) => {
      const ar = rankNode(a)
      const br = rankNode(b)
      if (ar !== br) return ar - br
      return a.label.localeCompare(b.label)
    })
  }

  const placed = new Map<string, { owner: string; x: number; y: number }>()
  const ownerContent = new Map<string, { height: number; hasRightColumn: boolean }>()
  const tasksByOwner = new Map<string, BrainGraphNodeView[]>()
  for (const task of taskNodes) {
    const owner = effectiveOwners.get(task.id) || 'unowned'
    appendToBucket(tasksByOwner, owner, task)
  }

  for (const owner of orderedOwners) {
    let cursorY = OWNER_PAD_TOP
    let hasRightColumn = false
    for (const task of tasksByOwner.get(owner) ?? []) {
      const children = childrenByTask.get(task.id) ?? []
      const rowHeight = Math.max(NODE_H, children.length > 0 ? (children.length - 1) * CHILD_Y + NODE_H : NODE_H)
      placed.set(task.id, { owner, x: OWNER_PAD_X, y: cursorY })
      children.forEach((child, index) => {
        placed.set(child.id, { owner, x: OWNER_PAD_X + CHILD_X, y: cursorY + index * CHILD_Y })
      })
      hasRightColumn = hasRightColumn || children.length > 0
      cursorY += rowHeight + ROW_GAP
    }

    const orphans = orphanNodes.get(owner) ?? []
    orphans.forEach((node, index) => {
      placed.set(node.id, { owner, x: OWNER_PAD_X + CHILD_X, y: cursorY + index * CHILD_Y })
    })
    hasRightColumn = hasRightColumn || orphans.length > 0
    if (orphans.length > 0) {
      cursorY += (orphans.length - 1) * CHILD_Y + NODE_H + ROW_GAP
    }

    const contentHeight = Math.max(OWNER_MIN_H, cursorY - ROW_GAP + OWNER_PAD_BOTTOM)
    ownerContent.set(owner, { height: contentHeight, hasRightColumn })
  }

  const groupPositions = new Map<string, { x: number; y: number; width: number; height: number }>()
  let cursorX = 0
  for (const owner of orderedOwners) {
    const content = ownerContent.get(owner) ?? { height: OWNER_MIN_H, hasRightColumn: false }
    const width = Math.max(OWNER_MIN_W, OWNER_PAD_X * 2 + NODE_W + (content.hasRightColumn ? CHILD_X : 0))
    groupPositions.set(owner, { x: cursorX, y: 0, width, height: content.height })
    cursorX += width + OWNER_GAP_X
  }

  const ownerNodes: OwnerFlowNode[] = orderedOwners.map((slug) => {
    const group = groupPositions.get(slug) ?? { x: 0, y: 0, width: OWNER_MIN_W, height: OWNER_MIN_H }
    return {
      id: `owner-boundary:${slug}`,
      type: 'ownerGroup',
      data: { owner: ownerSummary(slug, ownerBySlug, effectiveOwners, nodes) },
      position: { x: group.x, y: group.y },
      style: { width: group.width, height: group.height },
      selectable: true,
      draggable: false,
      selected: slug === selectedOwner,
      zIndex: 0,
    }
  })

  const graphNodes: BrainFlowNode[] = nodes.map((node) => {
    const position = placed.get(node.id) ?? { owner: effectiveOwners.get(node.id) || 'unowned', x: OWNER_PAD_X, y: OWNER_PAD_TOP }
    return {
      id: node.id,
      type: 'brain',
      data: node,
      parentId: `owner-boundary:${position.owner}`,
      extent: 'parent',
      position: { x: position.x, y: position.y },
      width: NODE_W,
      height: NODE_H,
      selected: node.id === selectedId,
      zIndex: 2,
    }
  })

  return [...ownerNodes, ...graphNodes]
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
  if (node.type === 'ownerGroup') return 'color-mix(in srgb, var(--accent) 26%, var(--bg-3))'
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
  selectedOwner,
  onSelectNode,
  onSelectOwner,
  onClearSelection,
}: {
  nodes: BrainGraphNodeView[]
  edges: BrainGraphEdge[]
  owners: BrainGraphOwnerView[]
  selectedId?: string | null
  selectedOwner?: string | null
  onSelectNode: (node: BrainGraphNodeView) => void
  onSelectOwner: (ownerSlug: string) => void
  onClearSelection: () => void
}) {
  const [instance, setInstance] = useState<ReactFlowInstance<FlowNode, FlowEdge> | null>(null)
  const flowNodes = useMemo(() => layoutNodes(nodes, owners, selectedId, selectedOwner), [nodes, owners, selectedId, selectedOwner])
  const flowEdgeList = useMemo(() => flowEdges(edges), [edges])

  useEffect(() => {
    if (!instance || flowNodes.length === 0) return
    const timer = window.setTimeout(() => {
      instance.fitView({ padding: 0.18, duration: 240 })
    }, 40)
    return () => window.clearTimeout(timer)
  }, [instance, flowNodes.length, edges.length])

  const onNodeClick: NodeMouseHandler<FlowNode> = (_event, node) => {
    if (node.type === 'ownerGroup') {
      onSelectOwner((node.data as OwnerGroupData).owner.slug)
      return
    }
    onSelectNode(node.data as BrainGraphNodeView)
  }
  const onSelectionChange: OnSelectionChangeFunc<FlowNode, FlowEdge> = ({ nodes: selectedNodes }) => {
    const ownerGroup = selectedNodes.find((node) => node.id.startsWith('owner-boundary:') && node.type === 'ownerGroup')
    if (!ownerGroup) return
    const ownerSlug = (ownerGroup.data as OwnerGroupData).owner.slug
    if (ownerSlug !== selectedOwner || selectedId) {
      onSelectOwner(ownerSlug)
    }
  }

  return (
    <div className="brain-canvas">
      <ReactFlow<FlowNode, FlowEdge>
        nodes={flowNodes}
        edges={flowEdgeList}
        nodeTypes={nodeTypes}
        onInit={setInstance}
        onNodeClick={onNodeClick}
        onSelectionChange={onSelectionChange}
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
