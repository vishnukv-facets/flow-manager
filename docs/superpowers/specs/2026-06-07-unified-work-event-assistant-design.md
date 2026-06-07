# Unified WorkEvent assistant surface - design

**Date:** 2026-06-07
**Status:** Draft for operator review
**Repo:** flow-manager (`flow` Go CLI + Mission Control UI)

---

## 1. Summary

Flow already has the right primitives for a smart work assistant:

- `internal/steering` observes Slack/GitHub events, classifies them, writes
  `attention_feed`, and records `steering_trace`.
- `internal/server/inbox_md.go` reads task-scoped `inbox.jsonl` activity.
- `internal/briefing` builds Mission Control and `flow standup` summaries.
- `/api/ask-flow` answers grounded questions from Flow data with citations.

The current problem is that these surfaces do not share one operator-attention
contract. Inbox can know about a GitHub PR event while Attention drops it.
Mission Control can label backlog or stale work as "Needs action" without
explaining whether the operator must act now. FYI links can point at orphan task
directories that are not real DB tasks. These break trust before any richer AI
features can help.

This phase introduces a normalized `WorkEvent` read model and uses it to make
Inbox, Attention, and Mission Control different views of the same work-state
logic. It also fixes the earlier GitHub and briefing bugs as part of that
contract.

---

## 2. Goals

- Treat Slack, GitHub, task updates, attention cards, and closeout signals as
  normalized work events with one classification vocabulary.
- Make GitHub PR lifecycle events actionable when they are linked to active
  Flow tasks, even if they are self-authored or authorless poll events.
- Separate operator-required decisions from FYI, waiting, startable backlog,
  and closeout work.
- Preserve explainability: every surfaced item says why it is in that bucket
  and links to source data, task, attention card, or trace when available.
- Keep the first implementation low-risk by deriving the read model from
  existing data instead of immediately replacing `inbox.jsonl` or
  `attention_feed`.

---

## 3. Non-goals

- Replacing the existing Slack/GitHub monitor pipelines.
- Rewriting historical inbox files into a new storage format.
- Adding autonomous outbound replies.
- Building a generic chatbot. Ask Flow stays grounded and citation-backed.
- Solving every assistant feature in one pass. This phase establishes the
  contract that later automation can trust.

---

## 4. WorkEvent Model

`WorkEvent` is a normalized read model built in a new pure-Go
`internal/workevents` package, not initially a new mandatory source-of-truth
table. The package should not depend on HTTP/UI code; server handlers,
briefing, and Ask Flow consume it.

```go
type WorkEvent struct {
    ID            string
    Source        string // slack | github | flow | attention
    Kind          string // pr_head_updated | reply | task_update | ...
    EventKey      string
    ThreadKey     string
    URL           string

    Title         string
    Summary       string
    Actor         string
    AuthoredBySelf bool
    OccurredAt    string
    ObservedAt    string

    TaskSlug      string
    ProjectSlug   string
    EntityKind    string // task | pr | issue | thread | update
    EntityRef     string // gh-pr:owner/repo#n, task slug, etc.

    Bucket        string // needs_action | fyi | next_up | waiting | closeout | handled | ignored
    Urgency       string // urgent | normal | low
    Confidence    float64
    ReasonCode    string
    ReasonText    string

    Links         []WorkEventLink
}
```

The read model is built from these existing sources:

| Source | Existing data | WorkEvent use |
|---|---|---|
| Attention | `attention_feed`, `attention_feedback` | unresolved operator decisions and acted history |
| Steering trace | `steering_trace` | explain why an event surfaced or dropped |
| Inbox | `~/.flow/tasks/<slug>/inbox.jsonl` | task-scoped GitHub/Slack wakeups |
| Tasks | `tasks`, tags, dependencies, `waiting_on` | ownership, startability, waiting state |
| GitHub monitor | `gh-pr:` / `gh-issue:` tags and event kinds | PR/issue lifecycle routing |
| Updates | task update directories | FYI history, but only link to DB-backed tasks |

---

## 5. Classification Buckets

The assistant should use a small, stable vocabulary across all surfaces.

| Bucket | Meaning | Examples |
|---|---|---|
| `needs_action` | The operator must decide, reply, review, unblock, or resume something. | PR head changed for active task; reply draft needs approval; comment asks operator a question. |
| `fyi` | Useful context, no immediate action required. | Self-authored PR opened; task update written; low-urgency digest event. |
| `next_up` | Startable work worth doing soon, but not triggered by a new external event. | High-priority backlog with dependencies satisfied. |
| `waiting` | Flow is blocked on someone/something else. | Task has `waiting_on`; linked PR waiting for review. |
| `closeout` | Work appears done and needs verification/bookkeeping. | Linked PR merged; reply sent; agent task can be marked done. |
| `handled` | Already acted or resolved. | Attention card acted; operator reply closed the thread. |
| `ignored` | Intentionally suppressed. | Muted channel, bot noise, duplicate event. |

`needs_action` must be reserved for actual operator-required decisions. This
fixes the current Mission Control confusion where stale/startable backlog work
can appear beside items that really need the operator now.

---

## 6. GitHub Routing Rules

Stage 0 must stop treating all self-authored or authorless GitHub activity as
noise. GitHub events should be classified by task linkage and event kind first.

| GitHub event | If linked to active task/PR | If unlinked |
|---|---|---|
| `pr_head_updated` | `needs_action`: re-review/rebase/resume task | `fyi` or ignored depending watch scope |
| `pr_review_changes_requested` | `needs_action` | `needs_action` if operator is mentioned/requested |
| `pr_review_comment` / `pr_comment` | `needs_action` unless authored by self and not a direct instruction | `needs_action` if mentions/requested, else `fyi` |
| `pr_merged` | `closeout`: verify merge and close Flow task | `fyi` |
| `pr_involved` self-authored | `fyi` unless active task says it is waiting on that PR | `fyi` or ignored |
| authorless poll update | classify by linked entity and event kind, not by missing author | classify by watch scope |

Implementation implication:

- Stage 0 can still drop obvious GitHub noise, but it must consult task tags or
  PR linkage before dropping self-authored/authorless events.
- `pr_head_updated` for a task-linked PR is never a pure drop.
- Trace rows should record a reason like
  `github task-linked pr_head_updated requires re-review`.

---

## 7. Mission Control Rules

Mission Control should be a prioritized briefing built from `WorkEvent`
buckets.

Primary sections:

1. **Needs action:** unresolved `needs_action` WorkEvents.
2. **Closeout:** merged/sent/done-looking work needing verification.
3. **Waiting:** active tasks with `waiting_on` or linked external blockers.
4. **Next up:** startable high-priority backlog or unblocked tasks.
5. **FYI:** recent useful activity with no action required.

Required behavior:

- Every item displays a compact reason.
- Links are validated before rendering.
- DB-backed task links go to task/session routes.
- Orphan update directories render as non-clickable FYI or link to raw update
  files only if the server can serve them safely.
- The old `NeedsAction = attention + waiting + stale + ready backlog` logic is
  split into the sections above.

---

## 8. Inbox And Attention UI Contract

Inbox and Attention should not be merged into one identical page. They should
share logic and diverge by view purpose.

| Surface | Purpose | WorkEvent filter |
|---|---|---|
| Inbox | Chronological activity and raw task wakeups | all buckets, grouped by source/task |
| Attention | Unresolved decisions | `needs_action`, selected `closeout`, manual reply approvals |
| Mission Control | Prioritized daily operating view | sectioned buckets |
| Task detail | Scoped history for one task | events where `task_slug = current task` |

Shared UI elements:

- bucket badge
- source icon
- linked task/PR
- reason text
- trace/source links
- action buttons

Attention-specific actions:

- approve/send reply
- forward to task
- create task
- mark FYI/read
- dismiss/mute
- open/resume linked session

Inbox-specific actions:

- mark read
- open source
- open task/session
- promote to Attention if the user wants to act on it

---

## 9. Assistant Behavior After This Phase

Once `WorkEvent` exists, the assistant can reliably answer and act on:

- "What needs me right now?"
- "What changed since I last looked?"
- "Which PR updates affect active tasks?"
- "What can I close?"
- "What should I start next?"

The first safe automations should be:

1. forward high-confidence context to a matched task
2. create a task for high-confidence unowned work
3. propose closeout for merged PRs
4. mark FYI/read on low-risk events

Substantive outbound replies remain manual-only.

---

## 10. API Shape

Add a new endpoint:

```http
GET /api/work-events?bucket=needs_action&source=github&task=<slug>&limit=50
```

Response:

```json
{
  "items": [
    {
      "id": "github:gh-pr:vishnukv-facets/flow-manager#21:pr_head_updated",
      "source": "github",
      "kind": "pr_head_updated",
      "bucket": "needs_action",
      "urgency": "normal",
      "title": "PR #21 head changed",
      "summary": "Review the PR again before closing autonomy-trust-ladder.",
      "task_slug": "autonomy-trust-ladder",
      "entity_kind": "pr",
      "entity_ref": "gh-pr:vishnukv-facets/flow-manager#21",
      "reason_code": "github_task_linked_pr_head_updated",
      "reason_text": "Task-linked PR changed after prior review.",
      "links": [
        {"kind": "task", "target": "autonomy-trust-ladder"},
        {"kind": "source", "target": "https://github.com/.../pull/21"},
        {"kind": "trace", "target": "trace-id"}
      ]
    }
  ],
  "counts": {
    "needs_action": 3,
    "closeout": 1,
    "waiting": 4,
    "next_up": 5,
    "fyi": 20
  }
}
```

Existing `/api/inbox`, `/api/attention`, and `/api/overview` can keep their
current routes initially, but their payloads should be derived from the same
builder where possible.

---

## 11. Implementation Slices

### Slice 1: Backend read model

- Add `internal/workevents` with the `WorkEvent` model, link helpers,
  bucket-ordering helpers, and builders over existing DB/filesystem sources.
- Add a small `internal/server/work_events.go` handler adapter for
  `/api/work-events`.
- Normalize attention cards, steering trace rows, task inbox events, and task
  state into `WorkEvent`.
- Add tests with seeded DB rows and temp `inbox.jsonl` files.

### Slice 2: GitHub routing fixes

- Update Stage 0 GitHub drop logic to consult task linkage before dropping
  self-authored or authorless events.
- Add tests for `pr_head_updated`, `pr_merged`, self-authored `pr_involved`,
  and authorless task-linked GitHub events.

### Slice 3: Mission Control buckets and orphan links

- Change `internal/briefing` to consume or mirror WorkEvent buckets.
- Split `NeedsAction`, `Closeout`, `Waiting`, `NextUp`, and `FYI` sections.
- Skip or safely render orphan update-only task directories.
- Add tests for orphan updates and bucket placement.

### Slice 4: UI shared rendering

- Add shared WorkEvent row/card component.
- Update Inbox, Attention trace/detail, and Overview sections to display the
  shared bucket/reason/link contract.
- Preserve dense operational UI. No marketing-style assistant page.

### Slice 5: Ask Flow integration

- Teach `/api/ask-flow` to answer "what needs me", "what changed", "what can I
  close", and "what should I start" from `WorkEvent`.
- Return citations to task, source, trace, and attention links.

---

## 12. Error Handling

- If an inbox file cannot be read, omit that source and return a degraded
  reason in API diagnostics rather than failing the whole page.
- If a task link cannot be resolved to a DB task, do not emit a clickable task
  link.
- If a GitHub event lacks author metadata, classify from event kind and linked
  entity instead of dropping it as "no author".
- If an event maps to multiple buckets, choose the strongest bucket in this
  order: `needs_action`, `closeout`, `waiting`, `next_up`, `fyi`, `handled`,
  `ignored`.

---

## 13. Testing

Backend:

- WorkEvent builder normalizes attention, trace, inbox, task, and update data.
- GitHub task-linked `pr_head_updated` becomes `needs_action`.
- GitHub task-linked `pr_merged` becomes `closeout`.
- Self-authored unlinked `pr_involved` becomes `fyi` or ignored, not
  `needs_action`.
- Authorless task-linked GitHub event is not dropped.
- Orphan task update directory does not produce a broken task link.
- Mission Control section counts match WorkEvent buckets.

Frontend:

- Inbox and Attention render the same bucket/reason/link metadata.
- Mission Control shows separate sections and no broken FYI task links.
- Long reason text and PR titles truncate cleanly on desktop and mobile.

Smoke:

- Seed local DB with GitHub PR events and verify `/api/work-events`,
  `/api/inbox`, `/api/attention`, and `/api/overview`.
- Run focused Go tests for `internal/steering`, `internal/briefing`,
  `internal/server`, and `internal/flowdb`.
- Run UI typecheck and a local browser smoke if UI changes are included.

---

## 14. Rollout

1. Land backend read model and tests without changing UI behavior.
2. Switch Mission Control briefing to the new buckets.
3. Switch Attention and Inbox to display shared bucket/reason fields.
4. Tighten Stage 0 GitHub behavior.
5. Add Ask Flow answers backed by WorkEvent.

This order keeps the behavior observable at each step and avoids a broad
front-end rewrite before the data contract is stable.

---

## 15. Acceptance Criteria

- A task-linked GitHub `pr_head_updated` event appears as `needs_action` with a
  reason and task/PR links.
- A task-linked GitHub `pr_merged` event appears as `closeout`.
- Mission Control no longer labels startable backlog and waiting tasks as the
  same kind of "Needs action".
- Clicking FYI/task links in Mission Control never produces
  `sql: no rows in result set`.
- Inbox, Attention, and Mission Control explain the same event consistently.
- Ask Flow can answer "what needs me right now?" from the same WorkEvent source.
