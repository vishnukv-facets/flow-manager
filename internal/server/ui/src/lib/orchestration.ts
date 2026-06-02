// Client-side orchestration-tree assembly.
//
// flow's spawn/tell/wait model links tasks via `parent_slug` (a denormalized
// first-parent mirror) and the task_dependencies table (the full parent set,
// surfaced as `parents`). The server already ships parent_slug / parents /
// children on every row in /api/tasks (see BuildTaskView in views.go), and that
// list query is live-invalidated on every activity event plus a 5s poll. So we
// rebuild the whole family hierarchy in the browser from the flat list — no
// dedicated tree endpoint, and running/done state is current for free.
//
// Tree-first by design: we render the parent_slug spine as a tree and flag
// additional parents (DAG edges) as a hint, rather than drawing a full graph.
import type { TaskView } from './types'

export interface OrchNode {
  task: TaskView
  children: OrchNode[]
  /** Parents beyond the primary parent_slug — a DAG hint surfaced on the node. */
  extraParents: number
}

/**
 * Canonical per-task status derivation, mirroring the Tasks table row
 * (`task.live ? 'running' : task.waiting_on ? 'waiting' : task.status`). Kept
 * here so the tree's status dots match the rest of the UI exactly.
 */
export function nodeStatus(t: TaskView): string {
  return t.live ? 'running' : t.waiting_on ? 'waiting' : t.status
}

/** Stable spawn-order sort: oldest first, slug as tiebreaker. */
function byCreated(a: TaskView, b: TaskView): number {
  if (a.created_at !== b.created_at) return a.created_at < b.created_at ? -1 : 1
  return a.slug < b.slug ? -1 : a.slug > b.slug ? 1 : 0
}

/**
 * Build the orchestration tree for `focusSlug`'s family: walk up via
 * parent_slug to the topmost reachable ancestor, then assemble the full subtree
 * beneath it. Both walks are cycle-safe (a `visited` guard), so a malformed
 * parent chain degrades to a finite tree instead of hanging the render. A task
 * with no parent and no children yields a single-node tree. Returns null only
 * when `focusSlug` isn't present in `tasks`.
 */
export function buildFamilyTree(tasks: TaskView[], focusSlug: string): OrchNode | null {
  const bySlug = new Map<string, TaskView>()
  for (const t of tasks) bySlug.set(t.slug, t)
  const focus = bySlug.get(focusSlug)
  if (!focus) return null

  // children index keyed by each task's parent_slug
  const kids = new Map<string, TaskView[]>()
  for (const t of tasks) {
    const p = t.parent_slug
    if (!p) continue
    const arr = kids.get(p)
    if (arr) arr.push(t)
    else kids.set(p, [t])
  }

  // walk up to the root ancestor (cycle-safe)
  let root = focus
  const seenUp = new Set<string>([root.slug])
  while (root.parent_slug) {
    const parent = bySlug.get(root.parent_slug)
    if (!parent || seenUp.has(parent.slug)) break
    seenUp.add(parent.slug)
    root = parent
  }

  // build the subtree downward (cycle-safe via seenDown)
  const seenDown = new Set<string>()
  const build = (t: TaskView): OrchNode => {
    seenDown.add(t.slug)
    const childTasks = (kids.get(t.slug) ?? [])
      .filter((c) => !seenDown.has(c.slug))
      .sort(byCreated)
    const parentCount = t.parents?.length ?? (t.parent_slug ? 1 : 0)
    return {
      task: t,
      extraParents: Math.max(0, parentCount - 1),
      children: childTasks.map(build),
    }
  }
  return build(root)
}

/** Total node count in a tree — for tab labels and modal titles. */
export function countNodes(node: OrchNode | null): number {
  if (!node) return 0
  let n = 1
  for (const c of node.children) n += countNodes(c)
  return n
}
