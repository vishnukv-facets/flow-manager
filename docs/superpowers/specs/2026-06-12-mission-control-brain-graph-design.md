# Mission Control Brain Graph Design

## Intent

Mission Control needs a graph-first Brain surface that gives the operator one
pane for autonomous work: what the global Brain is controlling, which owner is
responsible for each task family, what is running autonomously, what is
blocked, what needs a whitelist policy, and where to inspect or intervene.

The product should feel n8n-like in spatial clarity, but Flow remains the
system of record. The graph is a projection over real Flow owners, tasks,
dependencies, Brain plans, Brain runs, task inbox events, transcripts, logs,
PR state, and approval records. It is not a separate freeform workflow engine.

## Decisions

- One global Brain controls all owner boundaries.
- Owners are execution/context scopes, not independent mini-Brains.
- The graph is Flow-backed. UI edits mutate Flow entities through explicit APIs.
- Owner boundaries contain task/subtask family graphs.
- The graph uses a hybrid model: tasks are primary; active, failed, or selected
  tasks expand into worker, validator, steward, transcript, event, approval,
  PR, and closeout subnodes.
- Full-auto is the target operating mode, with risky actions gated by explicit
  whitelist policy.
- Risky actions include merge, deploy, force-push, destructive shell, branch
  deletion, and outbound reply.
- Sending input targets the selected node by default. The UI can offer "route
  via Brain" as an explicit alternate path.
- Inline graph nodes show status plus latest thought/activity summary. Full
  transcripts and logs live in the inspector or owner workspace.

## Existing Code Context

The current `feature/flow-brain-orchestrator` branch provides the Brain-side
foundation: plan/action bundles, the run ledger, scheduler APIs, and scheduler
tests. The current `flow/import-harness-integrated` branch provides runtime
foundation: `tasks.harness`, harness adapters, owner rows, owner ticks,
background launch plumbing, owner APIs, and an Owners UI.

These branches are complementary. The graph design should integrate both:

- Use Brain plans/runs as the orchestration and evidence layer.
- Use owners as graph boundaries and durable accountability scopes.
- Use task hierarchy and dependencies as the primary graph structure.
- Use harness/provider/model fields as runtime metadata, not a parallel
  scheduling model.

Before implementation, the branches should be reconciled so Brain code consumes
`tasks.harness` and owner tags instead of inventing another provider or owner
binding.

## Data Model

### Graph Ownership

Owner assignment starts with `owner:<slug>` task tags, because owner ticks in
the harness branch already create tasks that way. The graph API normalizes that
into an owner boundary.

Rules:

- A task tagged `owner:<slug>` appears inside that owner boundary.
- Descendants of an owned parent inherit the owner in the graph unless they
  carry a different explicit owner tag.
- Tasks without any owner mapping appear in an "Unowned" boundary.
- A future `tasks.owner_slug` column may replace tags if ownership needs
  stricter constraints; the graph API should hide that storage detail from the
  UI.

### Graph Node Types

Primary nodes:

- `owner`: boundary container backed by an owner row.
- `task`: Flow task, including regular tasks and playbook runs.

Expandable subnodes:

- `worker_run`: Brain worker or legacy autonomous run.
- `validator_run`: validation result and evidence.
- `steward_run`: PR/merge/closeout steward state.
- `approval`: policy gate for risky or blocked action.
- `event`: task/owner inbox event or Brain-routed event.
- `transcript`: session transcript pointer and compact preview.
- `log`: run log pointer and compact status.
- `pr`: GitHub PR/issue linkage and merge/check state.
- `closeout`: Flow done / post-merge closeout state.

### Graph Edge Types

- `contains`: owner boundary contains a task or task contains expanded subnode.
- `parent`: task/subtask hierarchy.
- `depends_on`: blocking dependency.
- `run_of`: run node belongs to a task.
- `produced`: worker produced validation/steward/PR evidence.
- `blocks`: approval or failed validation blocks the next action.
- `external_ref`: PR, issue, Slack thread, log, or transcript evidence.

## Backend API

Add a graph projection endpoint:

```text
GET /api/brain/graph
```

Query parameters:

- `project`
- `owner`
- `status`
- `include_done`
- `expand`
- `q`

Response shape:

```text
BrainGraphView
  generated_at
  freshness
  controller
  policy
  owners[]
  nodes[]
  edges[]
  counts
  selected_actions
  warnings[]
```

The endpoint should do bounded work. It may include compact run and transcript
summaries, but it must not scan large logs or full transcripts inline. Deep
content loads through detail endpoints when the inspector opens.

Action endpoints:

- `POST /api/brain/graph/event`
  - Send an event to selected owner/task/session/run.
  - Default target is selected node; optional route through Brain.
- `POST /api/brain/graph/seed`
  - Seed input into the next run, owner tick, or live session.
- `POST /api/brain/graph/open-session`
  - Open or resume the selected task/session.
- `POST /api/brain/graph/retry`
  - Retry a failed/dead worker, validator, or steward step.
- `POST /api/brain/graph/pause`
  - Pause an owner, task family, or run according to scope.
- `POST /api/brain/graph/approve`
  - Approve a specific risky action or policy whitelist decision.
- `POST /api/brain/graph/policy`
  - Update full-auto policy and risky-action whitelist.

Where possible, these should call existing server action paths rather than
duplicate session launch, owner tick, or task mutation logic.

## Autonomy Policy

The graph operates in full-auto mode by default: Brain can keep safe work
moving without asking on every step.

Safe actions:

- Start eligible autonomous workers.
- Retry safe failed runs under retry limits.
- Route events to selected tasks or owners.
- Run validators.
- Update graph state and run evidence.
- Propose steward work.
- Close non-risky internal state transitions.

Risky actions require an explicit whitelist policy before Brain can perform
them automatically:

- Merge PR.
- Deploy.
- Run destructive shell.
- Force-push.
- Delete branch.
- Send outbound reply.

Each autonomous decision records an audit entry with:

- actor (`brain`, owner slug, or operator),
- target node,
- policy rule,
- evidence checked,
- command/action attempted,
- result,
- timestamp,
- error text when applicable.

If a risky action is not whitelisted, the graph shows an approval node blocking
the downstream edge.

## UI Design

### Top-Level Surface

Add a Mission Control Brain/Graph page, likely a top-level nav item. The page
should eventually absorb the standalone Owners page by making owners visible as
boundaries in context.

Header:

- global Brain status,
- full-auto mode indicator,
- risky whitelist summary,
- search,
- project/owner/status filters,
- counts for running, blocked, approval needed, failed, done.

Canvas:

- pan/zoom graph view,
- owner boundaries with stable dimensions,
- task and subtask nodes,
- dependency edges,
- blocked and approval edges,
- active/failed/selected node expansion,
- compact provider/model/permission badges,
- latest thought/activity summary for live sessions.

Inspector:

- selected owner/task/run details,
- current state and next proposed action,
- transcript preview and link to full transcript,
- latest log preview and log history,
- inbox/event list,
- PR/check/review state,
- validation and steward evidence,
- action composer,
- controls for open/resume, seed input, send event, retry, pause, approve.

Owner workspace:

- secondary deep-dive page or drawer for a single owner,
- owner charter,
- owner journal,
- owner-local task family graph,
- owner sessions and transcripts,
- owner event history,
- controls for start/pause/tick/next wake.

### Graph Rendering

The current UI package does not include a graph/canvas library. Implementation
should add a proven React graph library after checking current package and
license constraints. A hand-rolled SVG graph is acceptable only for a temporary
read-only prototype, not for the production Mission Control surface.

The graph must support stable layout, pan/zoom, selection, keyboard focus,
node expansion, and edge labels. The UI should keep dense operational styling
consistent with existing Mission Control: no landing page, no decorative hero,
no card-in-card graph containers, and no hidden critical status.

## Data Flow

1. Brain creates or updates a plan.
2. Approved plan items create Flow tasks with owner tags, dependencies,
   provider/model/permission metadata, and branch policy.
3. The graph API projects owners, task families, dependencies, and Brain runs.
4. The scheduler launches startable autonomous workers.
5. Worker state updates tasks and Brain run ledger rows.
6. The graph updates through existing data-version/SSE refresh paths.
7. Completed/dead worker states expand into validator nodes.
8. Passing validation enables steward nodes.
9. Steward nodes can auto-merge only when risky-action policy permits it.
10. Closeout updates task state and evidence.
11. The inspector can send selected-node events, seed future runs, or open a
    live session at any point.

## Error Handling

Graph projection should degrade gracefully:

- Missing owner row: show task in "Unowned" with a warning.
- Missing task for a run: show orphaned run in a diagnostics section.
- Missing transcript/log: show unavailable evidence, not a blank node.
- Dead auto run: surface as failed node with retry action.
- Validation unknown: block steward progression until evidence improves.
- Wrong branch target: show policy violation and block merge edge.
- Policy denies risky action: show approval node, not a hidden error.
- Graph too large: collapse done branches and prompt filtering by owner/project.
- Backend partial failure: return warnings and the renderable subset.

## Testing

Backend:

- graph builder groups tasks by owner tag,
- inherited ownership from parent task,
- unowned boundary,
- dependency edges,
- parent/subtask edges,
- run expansion for worker/validator/steward,
- policy gate edges,
- stale/dead run handling,
- graph filters,
- action endpoint authorization and audit records.

Frontend:

- owner boundary rendering,
- hybrid node expansion,
- inspector selection,
- send event/seed/open actions,
- policy gate display,
- keyboard navigation,
- dense and mobile layout sanity,
- SSE refresh updates status without full page reload.

Integration:

- plan approval creates graph-visible task family,
- scheduler launches a startable node,
- completed worker creates validator-visible state,
- failed validation blocks steward,
- whitelisted merge path advances steward,
- non-whitelisted risky action creates approval node,
- transcript/log links open from inspector.

## Phased Plan

### Phase 0: Branch Integration Baseline

- Reconcile `feature/flow-brain-orchestrator` with
  `flow/import-harness-integrated`.
- Keep `session_provider` as UI/API provider contract.
- Use `tasks.harness` as runtime pin.
- Ensure Brain runs record provider, harness, requested model/tier, resolved
  model, permission mode, and log/session refs.
- Keep owner-created tasks tagged `owner:<slug>`.

### Phase 1: Graph Projection API

- Add server graph types and builder.
- Project owners, owned/unowned tasks, dependencies, parent/child edges, and
  compact run summaries.
- Add tests for graph grouping and edge semantics.
- Expose `GET /api/brain/graph`.

### Phase 2: Read-Only Graph UI

- Add Brain Graph route in Mission Control.
- Add graph renderer dependency or temporary prototype renderer.
- Render owner boundaries, task nodes, dependency edges, status badges, and
  filters.
- Add selection and inspector summary without mutation controls.

### Phase 3: Inspector Evidence

- Add transcript/log/event detail loading.
- Show latest thought/activity summary inline.
- Show full transcript/log/event tabs in inspector.
- Link PR/issue/source refs from graph nodes.

### Phase 4: Node Actions

- Add selected-node event sending.
- Add seed input for next run/session/owner tick.
- Add open/resume session action.
- Add retry and pause actions.
- Record action outcomes and errors in graph-visible evidence.

### Phase 5: Full-Auto Policy Controls

- Add global full-auto policy display.
- Add risky-action whitelist settings.
- Add approval nodes and approve action.
- Record audit entries for autonomous and operator-approved actions.

### Phase 6: Validator and Steward Expansion

- Expand task nodes into worker, validator, steward, approval, PR, and closeout
  subnodes.
- Block steward edges until validation passes.
- Block merge/closeout until branch target, PR state, validation, and policy
  are satisfied.

### Phase 7: Owner Workspace

- Add owner deep-dive workspace with charter, journal, owner-local graph,
  sessions, transcripts, events, and controls.
- Make the current Owners page a compact list or redirect into the graph
  experience.

### Phase 8: Hardening and Final Integration

- Performance pass for large graphs.
- Accessibility and keyboard support.
- Browser smoke tests.
- Embedded skill/operator docs for Brain graph workflows.
- Final feature branch verification and PR.

## Non-Goals

- A separate n8n-compatible workflow engine.
- Freeform arbitrary graph nodes that do not map to Flow entities.
- Per-owner mini-Brains.
- Replacing tasks, projects, playbooks, or Attention with a separate product.
- Making risky external/destructive actions invisible just because full-auto is
  enabled.
