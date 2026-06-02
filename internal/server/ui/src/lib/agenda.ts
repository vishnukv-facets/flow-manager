// Due-date agenda bucketing.
//
// Groups open tasks into the near-term agenda the user actually acts on:
// overdue, due today, and due within the next 7 days. Tasks with no due date —
// or due further out than a week — are intentionally excluded; the agenda is a
// "what needs attention soon" lens, not a full due-date listing.
//
// Dates are YYYY-MM-DD strings compared lexically (chronological for this
// format) against a locally-built `todayISO()`, matching the convention in
// format.ts so comparisons stay timezone-correct.
import type { TaskView } from './types'
import { todayISO } from './format'

export interface DueBuckets {
  overdue: TaskView[]
  today: TaskView[]
  week: TaskView[]
}

/** Add `n` days to a YYYY-MM-DD string via local date math (handles rollover). */
function addDays(iso: string, n: number): string {
  const [y, m, d] = iso.split('-').map(Number)
  const dt = new Date(y, m - 1, d + n)
  return `${dt.getFullYear()}-${String(dt.getMonth() + 1).padStart(2, '0')}-${String(dt.getDate()).padStart(2, '0')}`
}

function byDue(a: TaskView, b: TaskView): number {
  const da = a.due_date ?? ''
  const db = b.due_date ?? ''
  if (da !== db) return da < db ? -1 : 1
  return a.slug < b.slug ? -1 : a.slug > b.slug ? 1 : 0
}

/**
 * Bucket tasks by due date relative to `today` (defaults to local today):
 * overdue (before today), today (== today), and week (after today, through
 * today+7). Tasks without a due date, or due beyond the week window, are
 * dropped. Each bucket is sorted soonest-first, slug as tiebreaker.
 */
export function bucketByDue(tasks: TaskView[], today: string = todayISO()): DueBuckets {
  const weekEnd = addDays(today, 7)
  const overdue: TaskView[] = []
  const dueToday: TaskView[] = []
  const week: TaskView[] = []
  for (const t of tasks) {
    const d = t.due_date
    if (!d) continue
    if (d < today) overdue.push(t)
    else if (d === today) dueToday.push(t)
    else if (d <= weekEnd) week.push(t)
  }
  overdue.sort(byDue)
  dueToday.sort(byDue)
  week.sort(byDue)
  return { overdue, today: dueToday, week }
}

/** Total tasks across all agenda buckets — for the section count / empty check. */
export function agendaCount(b: DueBuckets): number {
  return b.overdue.length + b.today.length + b.week.length
}
