// Client-side analytics derivations for the Mission Control trends.
//
// Throughput and time-to-done are computed from the task list (done tasks use
// updated_at as the completion proxy — there is no done_at column). Token cost
// is bucketed from the server's TOKEN_SERIES (a Sunday-aligned 84-day daily
// grid; see buildTokenSeries in ui_data.go). All weekly grids are Sunday-
// aligned to match the server's heatmap window so the dashboards line up.
import type { TaskView, TokenDay } from './types'

export interface WeekPoint {
  weekStart: string // YYYY-MM-DD, the Sunday that starts the week
  value: number
  cost?: number // estimated USD; only set by tokensByWeek
}

/** Local YYYY-MM-DD for a Date. */
function fmt(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

/**
 * The `weeks` most-recent Sunday week-start dates, oldest first, ending with
 * the Sunday of the week containing `now`. Mirrors the server's grid alignment.
 */
export function sundayWeekStarts(now: Date, weeks = 12): string[] {
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const thisSunday = new Date(today)
  thisSunday.setDate(today.getDate() - today.getDay()) // getDay(): Sun=0
  const out: string[] = []
  for (let i = weeks - 1; i >= 0; i--) {
    const d = new Date(thisSunday)
    d.setDate(thisSunday.getDate() - i * 7)
    out.push(fmt(d))
  }
  return out
}

/** Index of the week bucket a YYYY-MM-DD date falls into, or -1 if before the grid. */
function weekIndex(grid: string[], date: string): number {
  let idx = -1
  for (let i = 0; i < grid.length; i++) {
    if (date >= grid[i]) idx = i
    else break
  }
  return idx
}

/**
 * Tasks completed per week over the last `weeks` weeks. Completion is proxied
 * by updated_at (no done_at column exists); tasks updated after a done-flip are
 * a minor source of drift. Only status==='done' tasks count.
 */
export function throughputByWeek(tasks: TaskView[], now: Date, weeks = 12): WeekPoint[] {
  const grid = sundayWeekStarts(now, weeks)
  const out = grid.map((weekStart) => ({ weekStart, value: 0 }))
  for (const t of tasks) {
    if (t.status !== 'done' || !t.updated_at) continue
    const idx = weekIndex(grid, t.updated_at.slice(0, 10))
    if (idx >= 0) out[idx].value++
  }
  return out
}

export interface TimeToDone {
  medianDays: number
  avgDays: number
  count: number
}

/**
 * Median and average days from created_at → done for completed tasks (updated_at
 * as the done timestamp). Tasks with bad/negative spans are skipped.
 */
export function timeToDone(tasks: TaskView[]): TimeToDone {
  const days: number[] = []
  for (const t of tasks) {
    if (t.status !== 'done') continue
    const c = Date.parse(t.created_at)
    const u = Date.parse(t.updated_at)
    if (!Number.isFinite(c) || !Number.isFinite(u) || u < c) continue
    days.push((u - c) / 86_400_000)
  }
  if (days.length === 0) return { medianDays: 0, avgDays: 0, count: 0 }
  days.sort((a, b) => a - b)
  const mid = Math.floor(days.length / 2)
  const median = days.length % 2 ? days[mid] : (days[mid - 1] + days[mid]) / 2
  const avg = days.reduce((s, d) => s + d, 0) / days.length
  return { medianDays: median, avgDays: avg, count: days.length }
}

export interface FlowWeek {
  weekStart: string // YYYY-MM-DD, the Sunday that starts the week
  created: number
  done: number
}

/**
 * Tasks created vs completed per week over the last `weeks` weeks. `created`
 * counts every task by created_at (regardless of current status); `done`
 * counts status==='done' tasks by updated_at (the same completion proxy
 * throughputByWeek uses). The window-level net (sum done − sum created) tells
 * you whether the backlog is shrinking (positive) or growing (negative) — the
 * "am I keeping up?" signal that throughput alone can't give.
 */
export function flowBalanceByWeek(tasks: TaskView[], now: Date, weeks = 12): FlowWeek[] {
  const grid = sundayWeekStarts(now, weeks)
  const out = grid.map((weekStart) => ({ weekStart, created: 0, done: 0 }))
  for (const t of tasks) {
    if (t.created_at) {
      const ci = weekIndex(grid, t.created_at.slice(0, 10))
      if (ci >= 0) out[ci].created++
    }
    if (t.status === 'done' && t.updated_at) {
      const di = weekIndex(grid, t.updated_at.slice(0, 10))
      if (di >= 0) out[di].done++
    }
  }
  return out
}

// A slice of a 100%-stacked composition bar: a labelled count plus the CSS
// color token to paint it with.
export interface Segment {
  key: string
  label: string
  value: number
  tone: string // CSS color (e.g. 'var(--accent)')
}

/**
 * Done tasks split by the engine that ran them. Claude and Codex always
 * appear (so the bar's two-colour identity is stable even at 0); a "No session"
 * slice is only added when some completed task never got a session bound.
 */
export function doneByProvider(tasks: TaskView[]): Segment[] {
  let claude = 0
  let codex = 0
  let other = 0
  for (const t of tasks) {
    if (t.status !== 'done') continue
    const p = (t.session_provider || '').toLowerCase()
    if (p === 'claude') claude++
    else if (p === 'codex') codex++
    else other++
  }
  const segs: Segment[] = [
    { key: 'claude', label: 'Claude', value: claude, tone: 'var(--accent)' },
    { key: 'codex', label: 'Codex', value: codex, tone: 'var(--accent-2)' },
  ]
  if (other > 0) segs.push({ key: 'other', label: 'No session', value: other, tone: 'var(--text-3)' })
  return segs
}

/**
 * Done tasks split by priority. Colours match the .prio convention in base.css
 * (high→danger, medium→warn, low→info) so the chart reads the same as the task
 * rows. A null/unknown priority falls into medium.
 */
export function doneByPriority(tasks: TaskView[]): Segment[] {
  let high = 0
  let medium = 0
  let low = 0
  for (const t of tasks) {
    if (t.status !== 'done') continue
    if (t.priority === 'high') high++
    else if (t.priority === 'low') low++
    else medium++
  }
  return [
    { key: 'high', label: 'High', value: high, tone: 'var(--danger)' },
    { key: 'medium', label: 'Medium', value: medium, tone: 'var(--warn)' },
    { key: 'low', label: 'Low', value: low, tone: 'var(--info)' },
  ]
}

/**
 * Share of done tasks that involved a headless (`--auto`) run, proxied by a
 * non-empty auto_run_status. Honest caveat: tasks completed before the --auto
 * feature existed carry no status, so this reads as "share of completions that
 * were headless", not a clean rate.
 */
export function autonomyRate(tasks: TaskView[]): { auto: number; total: number; pct: number } {
  let auto = 0
  let total = 0
  for (const t of tasks) {
    if (t.status !== 'done') continue
    total++
    if (t.auto_run_status) auto++
  }
  return { auto, total, pct: total ? Math.round((auto / total) * 100) : 0 }
}

/**
 * Done tasks split by what triggered them, inferred from tags: Slack-reply,
 * GitHub, or manual/self-initiated. A task tagged from both Slack and GitHub
 * (a Slack thread later linked to a PR) counts as Slack — the trigger source.
 */
export function doneBySource(tasks: TaskView[]): Segment[] {
  let slack = 0
  let github = 0
  let manual = 0
  for (const t of tasks) {
    if (t.status !== 'done') continue
    const tags = t.tags || []
    const isSlack = tags.some((g) => g === 'slack-reply' || g.startsWith('slack-thread:'))
    const isGitHub = tags.some((g) => g === 'github' || g.startsWith('gh-pr:') || g.startsWith('gh-issue:'))
    if (isSlack) slack++
    else if (isGitHub) github++
    else manual++
  }
  return [
    { key: 'slack', label: 'Slack', value: slack, tone: 'var(--info)' },
    { key: 'github', label: 'GitHub', value: github, tone: 'var(--accent-2)' },
    { key: 'manual', label: 'Manual', value: manual, tone: 'var(--accent)' },
  ]
}

// Normalize a raw model id/alias to a short tier label for grouping.
function modelTier(m: string): string {
  const s = (m || '').trim().toLowerCase()
  if (!s) return 'Auto'
  if (s.includes('opus')) return 'Opus'
  if (s.includes('sonnet')) return 'Sonnet'
  if (s.includes('haiku')) return 'Haiku'
  if (s.includes('mini')) return 'GPT mini'
  if (s.includes('gpt-5.5') || s.includes('gpt5.5')) return 'GPT-5.5'
  if (s.includes('gpt')) return 'GPT'
  return m.trim()
}

const MODEL_TONES: Record<string, string> = {
  Opus: 'var(--accent)',
  Sonnet: 'var(--accent-2)',
  Haiku: 'var(--info)',
  'GPT-5.5': 'var(--warn)',
  GPT: 'var(--ok)',
  'GPT mini': 'var(--ok)',
  Auto: 'var(--text-3)',
}

/**
 * Group the server's real run-model counts (MODEL_MIX, read from transcripts)
 * into tier segments for the Composition bar. Raw model ids are normalized to a
 * tier label and summed; sorted by count. Unlike the old explicit-pin approach,
 * this reflects what sessions actually ran on, so it isn't swamped by "Auto".
 */
export function modelSegments(counts: { model: string; count: number }[]): Segment[] {
  const grouped = new Map<string, number>()
  for (const c of counts) {
    const tier = modelTier(c.model)
    grouped.set(tier, (grouped.get(tier) || 0) + c.count)
  }
  return [...grouped.entries()]
    .map(([label, value]) => ({ key: label, label, value, tone: MODEL_TONES[label] ?? 'var(--text-3)' }))
    .sort((a, b) => b.value - a.value)
}

export interface CycleWeek {
  weekStart: string
  medianDays: number
  count: number
}

/**
 * Median time-to-done per week, bucketing done tasks by their completion week
 * (updated_at proxy). Each week's value is the median of created→done spans for
 * tasks completed that week — a cycle-time trend, not one all-time median.
 */
export function cycleTimeByWeek(tasks: TaskView[], now: Date, weeks = 12): CycleWeek[] {
  const grid = sundayWeekStarts(now, weeks)
  const spans: number[][] = grid.map(() => [])
  for (const t of tasks) {
    if (t.status !== 'done' || !t.updated_at) continue
    const idx = weekIndex(grid, t.updated_at.slice(0, 10))
    if (idx < 0) continue
    const c = Date.parse(t.created_at)
    const u = Date.parse(t.updated_at)
    if (!Number.isFinite(c) || !Number.isFinite(u) || u < c) continue
    spans[idx].push((u - c) / 86_400_000)
  }
  return grid.map((weekStart, i) => {
    const arr = spans[i].sort((a, b) => a - b)
    if (arr.length === 0) return { weekStart, medianDays: 0, count: 0 }
    const mid = Math.floor(arr.length / 2)
    const median = arr.length % 2 ? arr[mid] : (arr[mid - 1] + arr[mid]) / 2
    return { weekStart, medianDays: median, count: arr.length }
  })
}

export interface WipWeek {
  weekStart: string
  open: number
}

// Local YYYY-MM-DD of the Saturday that ends a Sunday-started week.
function weekEndISO(weekStart: string): string {
  const [y, m, d] = weekStart.split('-').map(Number)
  return fmt(new Date(y, m - 1, d + 6))
}

/**
 * Approximate open-task count at the END of each week: tasks created on/before
 * that week's Saturday and not yet done by then (done tasks use updated_at as
 * the completion proxy). Reconstructs a WIP trend from create/done timestamps;
 * archived/deleted tasks aren't in the set, so long-closed work is excluded.
 */
export function wipByWeek(tasks: TaskView[], now: Date, weeks = 12): WipWeek[] {
  const grid = sundayWeekStarts(now, weeks)
  return grid.map((weekStart) => {
    const weekEnd = weekEndISO(weekStart)
    let open = 0
    for (const t of tasks) {
      const created = t.created_at ? t.created_at.slice(0, 10) : ''
      if (!created || created > weekEnd) continue
      const doneDate = t.status === 'done' && t.updated_at ? t.updated_at.slice(0, 10) : null
      if (doneDate && doneDate <= weekEnd) continue
      open++
    }
    return { weekStart, open }
  })
}

/**
 * Bucket the server's daily TOKEN_SERIES into weekly sums. The series is
 * Sunday-aligned and a multiple of 7 long, so each 7-day chunk is one week.
 * `cost` carries the matching estimated-USD sum for the bar's tooltip.
 */
export function tokensByWeek(series: TokenDay[]): WeekPoint[] {
  const out: WeekPoint[] = []
  for (let w = 0; w * 7 < series.length; w++) {
    const chunk = series.slice(w * 7, w * 7 + 7)
    if (chunk.length === 0) break
    out.push({
      weekStart: chunk[0].date,
      value: chunk.reduce((s, d) => s + d.tokens, 0),
      cost: chunk.reduce((s, d) => s + (d.cost_usd ?? 0), 0),
    })
  }
  return out
}
