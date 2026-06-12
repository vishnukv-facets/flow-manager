# Mission Control Brain Graph Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Flow-backed Mission Control Brain Graph where one global Brain controls owner-bounded task families, autonomous runs, dependencies, evidence, and policy-gated actions from a single graph surface.

**Architecture:** Reconcile the Brain feature branch with the harness/owners branch first, then expose a bounded server-side graph projection over Flow tasks, owners, Brain runs, transcripts, logs, inbox events, PR tags, and policy audit records. The React UI renders that projection with `@xyflow/react`, keeps Flow as the system of record, and routes all graph actions through explicit server endpoints that reuse existing Flow commands and server action helpers.

**Tech Stack:** Go, SQLite via `modernc.org/sqlite`, Flow server API, React 18, TypeScript, Vite, TanStack Query, `@xyflow/react` 12.11.0 MIT, lucide-react, existing SSE/data-version refresh, Go unit tests, UI typecheck/build, `make ui`, `go test ./...`.

---

## Constraints From The Approved Design

- The product has one global Brain. Owners are accountability and execution scopes, not mini-Brains.
- The graph is Flow-backed, not a separate freeform workflow engine.
- Task and subtask nodes are primary. Active, failed, and selected tasks expand into worker, validator, steward, transcript, event, approval, PR, log, and closeout subnodes.
- Full-auto is the target mode, but merge, deploy, force-push, destructive shell, branch deletion, and outbound reply require whitelist policy.
- Owner assignment starts with `owner:<slug>` task tags. Children inherit ownership from their parent unless they have an explicit owner tag.
- Brain/orchestrator work stays on `feature/flow-brain-orchestrator`. Child PRs target that branch. Only the final integration PR targets `main`.
- Work must stay in `flow-manager` and use `origin` (`git@github.com:vishnukv-facets/flow-manager.git`), not `Facets-cloud/flow`.
- The execution worktree must be created from `feature/flow-brain-orchestrator`, even if a Flow-created worktree defaults to another branch.

## File Map

Backend files:

- Create `internal/server/brain_graph_types.go`: API DTOs and filter/request structs for graph projection and actions.
- Create `internal/server/brain_graph.go`: graph builder, ownership resolution, task-family projection, run summaries, filters, counts, and warnings.
- Create `internal/server/brain_graph_actions.go`: graph action handlers for event, seed, open session, retry, pause, approve, and policy updates.
- Create `internal/server/brain_graph_test.go`: graph builder and route tests.
- Create `internal/server/brain_graph_actions_test.go`: action endpoint validation and audit tests.
- Modify `internal/server/server.go`: register `/api/brain/graph` and `/api/brain/graph/*`.
- Modify `internal/server/types.go`: add exported graph DTO references only if existing UI type grouping requires it.
- Modify `internal/server/views.go`: reuse `BuildTaskView` and `BuildOwnerView` fields instead of duplicating task/owner presentation logic.
- Modify `internal/flowdb/db.go`: add policy/audit migrations and small query helpers if no Brain policy table exists after branch reconciliation.
- Create `internal/flowdb/brain_policy.go`: policy and audit storage helpers.
- Create `internal/flowdb/brain_policy_test.go`: migration, defaults, whitelist, and audit coverage.
- Modify existing Brain run files on the reconciled branch so run rows expose provider, harness, requested model/tier, resolved model, permission mode, log path, session id, status, and role.

Frontend files:

- Modify `internal/server/ui/package.json`: add `@xyflow/react`.
- Modify `internal/server/ui/pnpm-lock.yaml`: pin dependency resolution through `pnpm install`.
- Modify `internal/server/ui/src/lib/types.ts`: add `BrainGraphView`, node, edge, policy, action, warning, and inspector types.
- Modify `internal/server/ui/src/lib/query.ts`: add graph query and action mutation hooks.
- Create `internal/server/ui/src/screens/BrainGraph.tsx`: page shell, filters, graph state, selected node state, inspector layout.
- Create `internal/server/ui/src/components/brainGraph/BrainGraphCanvas.tsx`: React Flow canvas, node/edge mapping, pan/zoom, selection, expansion state.
- Create `internal/server/ui/src/components/brainGraph/BrainGraphNode.tsx`: task/run/evidence node renderers.
- Create `internal/server/ui/src/components/brainGraph/OwnerBoundary.tsx`: owner group node renderer.
- Create `internal/server/ui/src/components/brainGraph/BrainGraphInspector.tsx`: selected owner/task/run/evidence detail panel.
- Create `internal/server/ui/src/components/brainGraph/BrainGraphToolbar.tsx`: filters, search, counts, full-auto indicator.
- Create `internal/server/ui/src/components/brainGraph/BrainGraphPolicyPanel.tsx`: whitelist and approval controls.
- Create `internal/server/ui/src/components/brainGraph/BrainGraphLegend.tsx`: graph status and edge legend.
- Create `internal/server/ui/src/screens/OwnerWorkspace.tsx`: owner deep-dive workspace.
- Modify `internal/server/ui/src/app.tsx`: add `/brain` and `/brain/owner/:slug` routes.
- Modify `internal/server/ui/src/components/Shell.tsx`: add Brain Graph nav item.
- Modify `internal/server/ui/src/components/CommandPalette.tsx`: add Brain Graph and owner workspace commands.
- Modify `internal/server/ui/src/styles/app.css`: graph layout, node, edge, inspector, and responsive rules.

Docs and skill files:

- Modify `internal/app/skill/SKILL.md`: document Brain Graph operator workflow and branch policy after implementation is verified.
- Modify `README.md`: add Mission Control Brain Graph section with startup and smoke-test notes.

## Task 0: Integration Branch Baseline

**Files:**
- Modify after merge: `internal/app/do.go`
- Modify after merge: `internal/flowdb/db.go`
- Modify after merge: `internal/server/server.go`
- Modify after merge: `internal/server/types.go`
- Modify after merge: `internal/server/views.go`
- Modify after merge: `internal/server/ui/src/lib/query.ts`
- Modify after merge: `internal/server/ui/src/lib/types.ts`
- Modify after merge: `internal/server/ui/src/app.tsx`
- Modify after merge: `internal/server/ui/src/components/Shell.tsx`
- Modify after merge: `internal/server/ui/src/components/CommandPalette.tsx`
- Verify after merge: existing Brain files from `feature/flow-brain-orchestrator`
- Verify after merge: existing harness/owner files from `flow/import-harness-integrated`

- [ ] **Step 1: Create the execution worktree from the Brain branch**

Run:

```bash
git fetch origin
git worktree add .codex/worktrees/mission-control-brain-graph feature/flow-brain-orchestrator
cd .codex/worktrees/mission-control-brain-graph
git status --short --branch
```

Expected:

```text
## feature/flow-brain-orchestrator
```

No modified or untracked files should appear in this execution worktree before integration begins.

- [ ] **Step 2: Verify the remotes and branch target**

Run:

```bash
git remote -v
git branch --show-current
git rev-parse --abbrev-ref --symbolic-full-name @{u}
```

Expected:

```text
origin	git@github.com:vishnukv-facets/flow-manager.git (fetch)
origin	git@github.com:vishnukv-facets/flow-manager.git (push)
feature/flow-brain-orchestrator
origin/feature/flow-brain-orchestrator
```

If the upstream is not `origin/feature/flow-brain-orchestrator`, run:

```bash
git branch --set-upstream-to=origin/feature/flow-brain-orchestrator feature/flow-brain-orchestrator
```

- [ ] **Step 3: Merge the harness/owners branch into the Brain branch**

Run:

```bash
git merge --no-ff flow/import-harness-integrated
```

Expected conflict set if both branches touched the known overlap:

```text
internal/app/do.go
internal/flowdb/db.go
internal/flowdb/db_test.go
internal/server/server.go
internal/server/types.go
internal/server/views.go
internal/server/ui/src/lib/query.ts
internal/server/ui/src/lib/types.ts
internal/server/ui/src/app.tsx
internal/server/ui/src/components/Shell.tsx
internal/server/ui/src/components/CommandPalette.tsx
```

- [ ] **Step 4: Resolve provider and harness contract conflicts**

Keep these field rules in the resolved code:

```go
// session_provider remains the user-facing provider contract.
// harness is the runtime pin used by pluggable launchers and imported harness work.
// Empty harness values read as the normalized session_provider for old rows.
```

The resolved `flowdb.Task` must retain these fields:

```go
SessionProvider string
Harness         string
PermissionMode  sql.NullString
Model           sql.NullString
SessionID       sql.NullString
AutoRunStatus   sql.NullString
AutoRunPID      sql.NullInt64
AutoRunStarted  sql.NullString
AutoRunFinished sql.NullString
AutoRunLog      sql.NullString
```

- [ ] **Step 5: Add the regression test that locks harness visibility**

Add this test to `internal/server/server_test.go` or the nearest existing task API test file:

```go
func TestTaskAPIExposesHarnessAndProvider(t *testing.T) {
	root, db := testRootDB(t)
	now := "2026-06-12T00:00:00Z"
	_, err := db.Exec(`
		INSERT INTO tasks (slug, name, status, kind, priority, work_dir, session_provider, harness, created_at, updated_at)
		VALUES ('harness-task', 'Harness Task', 'backlog', 'regular', 'medium', ?, 'codex', 'codex', ?, ?)
	`, root, now, now)
	if err != nil {
		t.Fatalf("insert harness task: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/harness-task", nil)
	New(Config{DB: db, FlowRoot: root}).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["session_provider"] != "codex" {
		t.Fatalf("session_provider = %#v, want codex", got["session_provider"])
	}
	if got["harness"] != "codex" {
		t.Fatalf("harness = %#v, want codex", got["harness"])
	}
}
```

- [ ] **Step 6: Run focused integration tests**

Run:

```bash
go test ./internal/flowdb ./internal/app ./internal/server -run 'Harness|Owner|Brain|AutoRun|TaskAPI' -count=1
```

Expected:

```text
ok  	flow/internal/flowdb
ok  	flow/internal/app
ok  	flow/internal/server
```

- [ ] **Step 7: Commit the branch reconciliation**

Run:

```bash
git status --short
git add internal/app internal/flowdb internal/server
git commit -m "chore: reconcile brain branch with harness owners"
```

Expected:

```text
[feature/flow-brain-orchestrator <sha>] chore: reconcile brain branch with harness owners
```

## Task 1: Backend Graph DTOs And Route Skeleton

**Files:**
- Create: `internal/server/brain_graph_types.go`
- Create: `internal/server/brain_graph.go`
- Create: `internal/server/brain_graph_test.go`
- Modify: `internal/server/server.go`

- [ ] **Step 1: Write failing tests for the empty graph route**

Create `internal/server/brain_graph_test.go` with:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrainGraphEmptyRoute(t *testing.T) {
	root, db := testRootDB(t)
	s := New(Config{DB: db, FlowRoot: root})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/brain/graph", nil)
	s.handleBrainGraph(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var got BrainGraphView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode graph: %v", err)
	}
	if got.Controller.Mode != "global_brain" {
		t.Fatalf("controller mode = %q, want global_brain", got.Controller.Mode)
	}
	if got.Counts.TotalTasks != 0 {
		t.Fatalf("total tasks = %d, want 0", got.Counts.TotalTasks)
	}
	if len(got.Owners) != 1 || got.Owners[0].Slug != "unowned" {
		t.Fatalf("owners = %#v, want only unowned boundary", got.Owners)
	}
}
```

- [ ] **Step 2: Run the test to verify the route is missing**

Run:

```bash
go test ./internal/server -run TestBrainGraphEmptyRoute -count=1 -v
```

Expected:

```text
undefined: BrainGraphView
```

- [ ] **Step 3: Add graph API DTOs**

Create `internal/server/brain_graph_types.go`:

```go
package server

type BrainGraphFilters struct {
	Project     string
	Owner       string
	Status      string
	IncludeDone bool
	Expand      map[string]bool
	Query       string
}

type BrainGraphView struct {
	GeneratedAt     string                 `json:"generated_at"`
	Freshness       string                 `json:"freshness"`
	Controller      BrainGraphController   `json:"controller"`
	Policy          BrainGraphPolicyView   `json:"policy"`
	Owners          []BrainGraphOwnerView  `json:"owners"`
	Nodes           []BrainGraphNode       `json:"nodes"`
	Edges           []BrainGraphEdge       `json:"edges"`
	Counts          BrainGraphCounts       `json:"counts"`
	SelectedActions []BrainGraphActionSpec `json:"selected_actions"`
	Warnings        []BrainGraphWarning    `json:"warnings"`
}

type BrainGraphController struct {
	Mode        string `json:"mode"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
}

type BrainGraphPolicyView struct {
	FullAuto          bool     `json:"full_auto"`
	RiskyWhitelist    []string `json:"risky_whitelist"`
	ApprovalRequired  []string `json:"approval_required"`
	LastDecisionAt    *string  `json:"last_decision_at,omitempty"`
	LastDecisionState *string  `json:"last_decision_state,omitempty"`
}

type BrainGraphOwnerView struct {
	ID            string `json:"id"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	TaskCount     int    `json:"task_count"`
	RunningCount  int    `json:"running_count"`
	BlockedCount  int    `json:"blocked_count"`
	ApprovalCount int    `json:"approval_count"`
}

type BrainGraphNode struct {
	ID              string            `json:"id"`
	Type            string            `json:"type"`
	OwnerSlug       string            `json:"owner_slug,omitempty"`
	TaskSlug        string            `json:"task_slug,omitempty"`
	ParentTaskSlug  string            `json:"parent_task_slug,omitempty"`
	Label           string            `json:"label"`
	Status          string            `json:"status"`
	Priority        string            `json:"priority,omitempty"`
	Provider        string            `json:"provider,omitempty"`
	Harness         string            `json:"harness,omitempty"`
	PermissionMode  string            `json:"permission_mode,omitempty"`
	Model           string            `json:"model,omitempty"`
	Summary         string            `json:"summary,omitempty"`
	Expanded        bool              `json:"expanded"`
	Ref             *BrainGraphRef    `json:"ref,omitempty"`
	Badges          []string          `json:"badges,omitempty"`
	Actions         []string          `json:"actions,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type BrainGraphRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	URL  string `json:"url,omitempty"`
}

type BrainGraphEdge struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Source string `json:"source"`
	Target string `json:"target"`
	Label  string `json:"label,omitempty"`
	Status string `json:"status,omitempty"`
}

type BrainGraphCounts struct {
	TotalTasks      int `json:"total_tasks"`
	Running         int `json:"running"`
	Blocked         int `json:"blocked"`
	Failed          int `json:"failed"`
	ApprovalNeeded  int `json:"approval_needed"`
	Done            int `json:"done"`
	Owners          int `json:"owners"`
	Warnings        int `json:"warnings"`
}

type BrainGraphActionSpec struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Risky     bool   `json:"risky"`
	Enabled   bool   `json:"enabled"`
	Disabled  string `json:"disabled_reason,omitempty"`
}

type BrainGraphWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	NodeID  string `json:"node_id,omitempty"`
}
```

- [ ] **Step 4: Add the minimal builder and handler**

Create `internal/server/brain_graph.go`:

```go
package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func parseBrainGraphFilters(r *http.Request) BrainGraphFilters {
	q := r.URL.Query()
	expand := map[string]bool{}
	for _, raw := range strings.Split(q.Get("expand"), ",") {
		raw = strings.TrimSpace(raw)
		if raw != "" {
			expand[raw] = true
		}
	}
	return BrainGraphFilters{
		Project:     strings.TrimSpace(q.Get("project")),
		Owner:       strings.TrimSpace(q.Get("owner")),
		Status:      strings.TrimSpace(q.Get("status")),
		IncludeDone: q.Get("include_done") == "1" || q.Get("include_done") == "true",
		Expand:      expand,
		Query:       strings.TrimSpace(q.Get("q")),
	}
}

func BuildBrainGraph(db *sql.DB, root string, filters BrainGraphFilters, now time.Time) (BrainGraphView, error) {
	view := BrainGraphView{
		GeneratedAt: now.Format(time.RFC3339),
		Freshness:   "fresh",
		Controller: BrainGraphController{
			Mode:        "global_brain",
			DisplayName: "Global Brain",
			Status:      "ready",
		},
		Policy: BrainGraphPolicyView{
			FullAuto:         true,
			RiskyWhitelist:   []string{},
			ApprovalRequired: []string{"merge", "deploy", "force_push", "destructive_shell", "delete_branch", "outbound_reply"},
		},
		Owners: []BrainGraphOwnerView{{
			ID:     "owner:unowned",
			Slug:   "unowned",
			Name:   "Unowned",
			Status: "active",
		}},
		Nodes:           []BrainGraphNode{},
		Edges:           []BrainGraphEdge{},
		SelectedActions: defaultBrainGraphActions(),
		Warnings:        []BrainGraphWarning{},
	}
	view.Counts.Owners = len(view.Owners)
	return view, nil
}

func defaultBrainGraphActions() []BrainGraphActionSpec {
	return []BrainGraphActionSpec{
		{Key: "open_session", Label: "Open session", Enabled: true},
		{Key: "send_event", Label: "Send event", Enabled: true},
		{Key: "seed", Label: "Seed input", Enabled: true},
		{Key: "retry", Label: "Retry", Enabled: true},
		{Key: "pause", Label: "Pause", Enabled: true},
		{Key: "approve", Label: "Approve", Risky: true, Enabled: true},
	}
}

func (s *Server) handleBrainGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	view, err := BuildBrainGraph(s.cfg.DB, s.cfg.FlowRoot, parseBrainGraphFilters(r), time.Now())
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, view)
}
```

- [ ] **Step 5: Register the route**

Modify `internal/server/server.go` in `registerAPIRoutes`:

```go
mux.HandleFunc("/api/brain/graph", s.handleBrainGraph)
```

- [ ] **Step 6: Run the route test**

Run:

```bash
go test ./internal/server -run TestBrainGraphEmptyRoute -count=1 -v
```

Expected:

```text
PASS
```

- [ ] **Step 7: Commit the route skeleton**

Run:

```bash
git add internal/server/brain_graph_types.go internal/server/brain_graph.go internal/server/brain_graph_test.go internal/server/server.go
git commit -m "feat: add brain graph api skeleton"
```

## Task 2: Owner Grouping And Task Dependency Graph

**Files:**
- Modify: `internal/server/brain_graph.go`
- Modify: `internal/server/brain_graph_test.go`
- Reuse: `internal/flowdb/db.go`
- Reuse: `internal/flowdb/owners.go`

- [ ] **Step 1: Write failing tests for owner tags, inheritance, and unowned tasks**

Update the import block in `internal/server/brain_graph_test.go` to include the new helpers:

```go
import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"flow/internal/flowdb"
)
```

Append to `internal/server/brain_graph_test.go`:

```go
func TestBrainGraphGroupsTasksByOwnerTagAndInheritance(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainGraphTask(t, db, "parent", "Parent", "in-progress", "high", root, nil)
	insertBrainGraphTask(t, db, "child", "Child", "backlog", "medium", root, strPtr("parent"))
	insertBrainGraphTask(t, db, "other", "Other", "backlog", "medium", root, nil)
	if _, err := db.Exec(`INSERT INTO owners (slug, name, work_dir, status, every, created_at, updated_at) VALUES ('brain-ui', 'Brain UI', ?, 'active', '24h', datetime('now'), datetime('now'))`, root); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	if err := flowdb.AddTaskTag(db, "parent", "owner:brain-ui"); err != nil {
		t.Fatalf("tag parent: %v", err)
	}

	view, err := BuildBrainGraph(db, root, BrainGraphFilters{}, time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("BuildBrainGraph: %v", err)
	}

	nodes := graphNodesByTask(view.Nodes)
	if nodes["parent"].OwnerSlug != "brain-ui" {
		t.Fatalf("parent owner = %q, want brain-ui", nodes["parent"].OwnerSlug)
	}
	if nodes["child"].OwnerSlug != "brain-ui" {
		t.Fatalf("child owner = %q, want inherited brain-ui", nodes["child"].OwnerSlug)
	}
	if nodes["other"].OwnerSlug != "unowned" {
		t.Fatalf("other owner = %q, want unowned", nodes["other"].OwnerSlug)
	}
}

func TestBrainGraphAddsParentAndDependencyEdges(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainGraphTask(t, db, "parent", "Parent", "in-progress", "high", root, nil)
	insertBrainGraphTask(t, db, "child", "Child", "backlog", "medium", root, strPtr("parent"))
	insertBrainGraphTask(t, db, "dep", "Dependency", "done", "medium", root, nil)
	if _, err := db.Exec(`INSERT INTO task_dependencies (child_slug, parent_slug, created_at) VALUES ('child', 'dep', datetime('now'))`); err != nil {
		t.Fatalf("insert dependency: %v", err)
	}

	view, err := BuildBrainGraph(db, root, BrainGraphFilters{IncludeDone: true}, time.Now())
	if err != nil {
		t.Fatalf("BuildBrainGraph: %v", err)
	}
	if !graphHasEdge(view.Edges, "parent", "task:parent", "task:child") {
		t.Fatalf("missing parent edge: %#v", view.Edges)
	}
	if !graphHasEdge(view.Edges, "depends_on", "task:dep", "task:child") {
		t.Fatalf("missing depends_on edge: %#v", view.Edges)
	}
}
```

Add helpers at the bottom of the file:

```go
func strPtr(s string) *string { return &s }

func graphNodesByTask(nodes []BrainGraphNode) map[string]BrainGraphNode {
	out := map[string]BrainGraphNode{}
	for _, n := range nodes {
		if n.TaskSlug != "" {
			out[n.TaskSlug] = n
		}
	}
	return out
}

func graphHasEdge(edges []BrainGraphEdge, typ, source, target string) bool {
	for _, e := range edges {
		if e.Type == typ && e.Source == source && e.Target == target {
			return true
		}
	}
	return false
}

func insertBrainGraphTask(t *testing.T, db *sql.DB, slug, name, status, priority, wd string, parent *string) {
	t.Helper()
	now := "2026-06-12T00:00:00Z"
	_, err := db.Exec(`
		INSERT INTO tasks (slug, name, status, kind, parent_slug, priority, work_dir, created_at, updated_at)
		VALUES (?, ?, ?, 'regular', ?, ?, ?, ?, ?)
	`, slug, name, status, parent, priority, wd, now, now)
	if err != nil {
		t.Fatalf("insert brain graph task %s: %v", slug, err)
	}
}
```

- [ ] **Step 2: Run tests to verify graph semantics are missing**

Run:

```bash
go test ./internal/server -run 'TestBrainGraphGroupsTasksByOwnerTagAndInheritance|TestBrainGraphAddsParentAndDependencyEdges' -count=1 -v
```

Expected:

```text
FAIL
```

- [ ] **Step 3: Implement owner and task projection**

In `internal/server/brain_graph.go`, update `BuildBrainGraph` to:

```go
tasks, err := flowdb.ListTasks(db, flowdb.TaskFilter{IncludeArchived: false, IncludeDeleted: false})
if err != nil {
	return BrainGraphView{}, err
}
owners, err := flowdb.ListOwners(db, flowdb.OwnerFilter{IncludeArchived: false})
if err != nil {
	return BrainGraphView{}, err
}
tagsByTask, err := flowdb.GetTaskTagsBatch(db, taskSlugs(tasks))
if err != nil {
	return BrainGraphView{}, err
}
ownerByTask := resolveBrainGraphOwners(tasks, owners, tagsByTask)
```

Add these helper functions:

```go
func taskSlugs(tasks []*flowdb.Task) []string {
	slugs := make([]string, 0, len(tasks))
	for _, t := range tasks {
		slugs = append(slugs, t.Slug)
	}
	return slugs
}

func resolveBrainGraphOwners(tasks []*flowdb.Task, owners []*flowdb.Owner, tagsByTask map[string][]string) map[string]string {
	knownOwners := map[string]bool{"unowned": true}
	for _, o := range owners {
		knownOwners[o.Slug] = true
	}
	explicit := map[string]string{}
	for slug, tags := range tagsByTask {
		for _, tag := range tags {
			if strings.HasPrefix(tag, "owner:") {
				owner := strings.TrimPrefix(tag, "owner:")
				if knownOwners[owner] {
					explicit[slug] = owner
				} else {
					explicit[slug] = "unowned"
				}
				break
			}
		}
	}
	bySlug := map[string]*flowdb.Task{}
	for _, t := range tasks {
		bySlug[t.Slug] = t
	}
	resolved := map[string]string{}
	var resolve func(string) string
	resolve = func(slug string) string {
		if owner, ok := resolved[slug]; ok {
			return owner
		}
		if owner, ok := explicit[slug]; ok {
			resolved[slug] = owner
			return owner
		}
		task, ok := bySlug[slug]
		if ok && task.ParentSlug.Valid && task.ParentSlug.String != slug {
			owner := resolve(task.ParentSlug.String)
			resolved[slug] = owner
			return owner
		}
		resolved[slug] = "unowned"
		return "unowned"
	}
	for _, t := range tasks {
		resolve(t.Slug)
	}
	return resolved
}
```

- [ ] **Step 4: Add task and owner node assembly**

Add helper functions:

```go
func appendOwnerBoundaries(view *BrainGraphView, owners []*flowdb.Owner) {
	view.Owners = []BrainGraphOwnerView{{
		ID:     "owner:unowned",
		Slug:   "unowned",
		Name:   "Unowned",
		Status: "active",
	}}
	for _, o := range owners {
		view.Owners = append(view.Owners, BrainGraphOwnerView{
			ID:     "owner:" + o.Slug,
			Slug:   o.Slug,
			Name:   o.Name,
			Status: o.Status,
		})
	}
}

func brainGraphTaskNode(t *flowdb.Task, owner string) BrainGraphNode {
	node := BrainGraphNode{
		ID:             "task:" + t.Slug,
		Type:           "task",
		OwnerSlug:      owner,
		TaskSlug:       t.Slug,
		Label:          t.Name,
		Status:         t.Status,
		Priority:       t.Priority,
		Provider:       t.SessionProvider,
		Harness:        t.Harness,
		PermissionMode: nullStringValue(t.PermissionMode),
		Model:          nullStringValue(t.Model),
		Summary:        brainGraphTaskSummary(t),
		Ref:            &BrainGraphRef{Kind: "task", ID: t.Slug, URL: "/tasks/" + t.Slug},
		Actions:        []string{"open_session", "send_event", "seed"},
	}
	if t.ParentSlug.Valid {
		node.ParentTaskSlug = t.ParentSlug.String
	}
	if t.AutoRunStatus.Valid && t.AutoRunStatus.String != "" {
		node.Badges = append(node.Badges, "auto:"+t.AutoRunStatus.String)
	}
	return node
}

func brainGraphTaskSummary(t *flowdb.Task) string {
	if t.WaitingOn.Valid && t.WaitingOn.String != "" {
		return "Waiting on " + t.WaitingOn.String
	}
	if t.AutoRunStatus.Valid && t.AutoRunStatus.String != "" {
		return "Auto run " + t.AutoRunStatus.String
	}
	return t.Status
}
```

- [ ] **Step 5: Add parent and dependency edges**

Use existing dependency query helpers if they exist after branch reconciliation. If they do not exist, add this local query in `brain_graph.go`:

```go
func listBrainGraphDependencies(db *sql.DB) ([]BrainGraphEdge, error) {
	rows, err := db.Query(`SELECT child_slug, parent_slug FROM task_dependencies ORDER BY parent_slug, child_slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []BrainGraphEdge
	for rows.Next() {
		var taskSlug, dependsOn string
		if err := rows.Scan(&taskSlug, &dependsOn); err != nil {
			return nil, err
		}
		edges = append(edges, BrainGraphEdge{
			ID:     "dep:" + dependsOn + "->" + taskSlug,
			Type:   "depends_on",
			Source: "task:" + dependsOn,
			Target: "task:" + taskSlug,
			Label:  "depends on",
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return edges, nil
}
```

Add parent edges during task assembly:

```go
if t.ParentSlug.Valid && t.ParentSlug.String != "" {
	view.Edges = append(view.Edges, BrainGraphEdge{
		ID:     "parent:" + t.ParentSlug.String + "->" + t.Slug,
		Type:   "parent",
		Source: "task:" + t.ParentSlug.String,
		Target: "task:" + t.Slug,
		Label:  "subtask",
	})
}
```

- [ ] **Step 6: Run owner and edge tests**

Run:

```bash
go test ./internal/server -run 'TestBrainGraphGroupsTasksByOwnerTagAndInheritance|TestBrainGraphAddsParentAndDependencyEdges|TestBrainGraphEmptyRoute' -count=1 -v
```

Expected:

```text
PASS
```

- [ ] **Step 7: Commit graph grouping and edges**

Run:

```bash
git add internal/server/brain_graph.go internal/server/brain_graph_test.go
git commit -m "feat: project owner task graph"
```

## Task 3: Run Expansion And Evidence Nodes

**Files:**
- Modify: `internal/server/brain_graph.go`
- Modify: `internal/server/brain_graph_types.go`
- Modify: `internal/server/brain_graph_test.go`
- Modify after branch reconciliation: Brain run ledger files under `internal/flowdb` or `internal/brain`

- [ ] **Step 1: Write failing tests for auto-run expansion**

Append to `internal/server/brain_graph_test.go`:

```go
func TestBrainGraphExpandsActiveAndFailedAutoRuns(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainGraphTask(t, db, "worker", "Worker", "in-progress", "high", root, nil)
	_, err := db.Exec(`UPDATE tasks SET session_provider='codex', harness='codex', auto_run_status='dead', auto_run_log='/tmp/worker.log' WHERE slug='worker'`)
	if err != nil {
		t.Fatalf("set auto run: %v", err)
	}

	view, err := BuildBrainGraph(db, root, BrainGraphFilters{Expand: map[string]bool{"task:worker": true}}, time.Now())
	if err != nil {
		t.Fatalf("BuildBrainGraph: %v", err)
	}
	if !graphHasNode(view.Nodes, "run:auto:worker") {
		t.Fatalf("missing auto run node: %#v", view.Nodes)
	}
	if !graphHasEdge(view.Edges, "run_of", "task:worker", "run:auto:worker") {
		t.Fatalf("missing run_of edge: %#v", view.Edges)
	}
	if view.Counts.Failed != 1 {
		t.Fatalf("failed count = %d, want 1", view.Counts.Failed)
	}
}

func graphHasNode(nodes []BrainGraphNode, id string) bool {
	for _, n := range nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run the test and confirm missing run nodes**

Run:

```bash
go test ./internal/server -run TestBrainGraphExpandsActiveAndFailedAutoRuns -count=1 -v
```

Expected:

```text
FAIL
```

- [ ] **Step 3: Add expansion helpers for legacy auto runs**

Add to `internal/server/brain_graph.go`:

```go
func appendAutoRunExpansion(view *BrainGraphView, task *flowdb.Task, expanded bool) {
	if !task.AutoRunStatus.Valid || task.AutoRunStatus.String == "" {
		return
	}
	status := task.AutoRunStatus.String
	shouldExpand := expanded || status == "running" || status == "dead"
	if !shouldExpand {
		return
	}
	runID := "run:auto:" + task.Slug
	node := BrainGraphNode{
		ID:             runID,
		Type:           "worker_run",
		OwnerSlug:      "",
		TaskSlug:       task.Slug,
		Label:          "Auto worker",
		Status:         status,
		Provider:       task.SessionProvider,
		Harness:        task.Harness,
		PermissionMode: nullStringValue(task.PermissionMode),
		Model:          nullStringValue(task.Model),
		Summary:        autoRunSummary(task),
		Ref:            &BrainGraphRef{Kind: "auto_run", ID: task.Slug},
		Actions:        []string{"retry", "pause"},
	}
	if task.AutoRunLog.Valid && task.AutoRunLog.String != "" {
		node.Metadata = map[string]string{"log_path": task.AutoRunLog.String}
	}
	view.Nodes = append(view.Nodes, node)
	view.Edges = append(view.Edges, BrainGraphEdge{
		ID:     "run-of:" + task.Slug,
		Type:   "run_of",
		Source: "task:" + task.Slug,
		Target: runID,
		Label:  "worker",
		Status: status,
	})
	if status == "dead" {
		view.Counts.Failed++
	}
}

func autoRunSummary(task *flowdb.Task) string {
	if task.AutoRunStatus.String == "running" && task.AutoRunStarted.Valid {
		return "Running since " + task.AutoRunStarted.String
	}
	if task.AutoRunStatus.String == "dead" && task.AutoRunFinished.Valid {
		return "Dead since " + task.AutoRunFinished.String
	}
	return "Auto run " + task.AutoRunStatus.String
}
```

Call it while iterating tasks:

```go
expanded := filters.Expand["task:"+t.Slug]
appendAutoRunExpansion(&view, t, expanded)
```

- [ ] **Step 4: Add Brain run expansion after ledger reconciliation**

After the merged Brain branch exposes run rows, add a small server adapter instead of importing UI code into the DB package:

```go
type brainGraphRunRow struct {
	ID             string
	TaskSlug       string
	Role           string
	Status         string
	Provider       string
	Harness        string
	RequestedModel string
	ResolvedModel  string
	PermissionMode string
	LogPath        string
	SessionID      string
	Summary        string
}
```

Add `listBrainGraphRunRows(db)` in `brain_graph.go` and map roles:

```go
func brainGraphRunNodeType(role string) string {
	switch role {
	case "validator":
		return "validator_run"
	case "steward":
		return "steward_run"
	default:
		return "worker_run"
	}
}
```

For every Brain run row, add:

```go
node := BrainGraphNode{
	ID:             "run:" + row.ID,
	Type:           brainGraphRunNodeType(row.Role),
	TaskSlug:       row.TaskSlug,
	Label:          strings.Title(row.Role),
	Status:         row.Status,
	Provider:       row.Provider,
	Harness:        row.Harness,
	PermissionMode: row.PermissionMode,
	Model:          row.ResolvedModel,
	Summary:        row.Summary,
	Ref:            &BrainGraphRef{Kind: "brain_run", ID: row.ID},
	Actions:        []string{"retry", "pause"},
}
```

- [ ] **Step 5: Add transcript, log, and PR reference nodes for expanded tasks**

Add these reference-node rules:

```go
func appendEvidenceRefs(view *BrainGraphView, task *flowdb.Task, tags []string, expanded bool) {
	if !expanded {
		return
	}
	if task.SessionID.Valid && task.SessionID.String != "" {
		id := "transcript:" + task.Slug
		view.Nodes = append(view.Nodes, BrainGraphNode{
			ID:       id,
			Type:     "transcript",
			TaskSlug: task.Slug,
			Label:    "Transcript",
			Status:   "available",
			Summary:  "Open conversation evidence",
			Ref:      &BrainGraphRef{Kind: "transcript", ID: task.Slug, URL: "/sessions/" + task.Slug},
		})
		view.Edges = append(view.Edges, BrainGraphEdge{ID: "evidence:" + task.Slug + ":transcript", Type: "external_ref", Source: "task:" + task.Slug, Target: id, Label: "transcript"})
	}
	for _, tag := range tags {
		if strings.HasPrefix(tag, "gh-pr:") || strings.HasPrefix(tag, "gh-issue:") {
			id := "external:" + task.Slug + ":" + tag
			view.Nodes = append(view.Nodes, BrainGraphNode{
				ID:       id,
				Type:     "pr",
				TaskSlug: task.Slug,
				Label:    tag,
				Status:   "linked",
				Summary:  "GitHub linkage",
				Ref:      &BrainGraphRef{Kind: "github", ID: tag},
			})
			view.Edges = append(view.Edges, BrainGraphEdge{ID: "external-ref:" + task.Slug + ":" + tag, Type: "external_ref", Source: "task:" + task.Slug, Target: id, Label: "github"})
		}
	}
}
```

- [ ] **Step 6: Run evidence-node tests**

Run:

```bash
go test ./internal/server -run 'TestBrainGraphExpandsActiveAndFailedAutoRuns|TestBrainGraph' -count=1 -v
```

Expected:

```text
PASS
```

- [ ] **Step 7: Commit run expansion**

Run:

```bash
git add internal/server/brain_graph.go internal/server/brain_graph_types.go internal/server/brain_graph_test.go
git commit -m "feat: expand brain graph evidence nodes"
```

## Task 4: Policy Storage And Approval Gates

**Files:**
- Create: `internal/flowdb/brain_policy.go`
- Create: `internal/flowdb/brain_policy_test.go`
- Modify: `internal/flowdb/db.go`
- Modify: `internal/server/brain_graph.go`
- Modify: `internal/server/brain_graph_test.go`

- [ ] **Step 1: Add policy migration test**

Create `internal/flowdb/brain_policy_test.go`:

```go
package flowdb

import "testing"

func TestBrainPolicyDefaultsRequireRiskyApproval(t *testing.T) {
	db := openTempDB(t)
	policy, err := GetBrainPolicy(db)
	if err != nil {
		t.Fatalf("GetBrainPolicy: %v", err)
	}
	if !policy.FullAuto {
		t.Fatal("FullAuto = false, want true")
	}
	for _, action := range []string{"merge", "deploy", "force_push", "destructive_shell", "delete_branch", "outbound_reply"} {
		if policy.IsWhitelisted(action) {
			t.Fatalf("%s unexpectedly whitelisted", action)
		}
	}
}

func TestBrainPolicyAuditRoundTrip(t *testing.T) {
	db := openTempDB(t)
	entry := BrainActionAudit{
		ID:           "audit-1",
		Action:       "merge",
		TargetType:   "task",
		TargetID:     "brain-feature-pr",
		Actor:        "operator",
		Policy:       "approval_required",
		EvidenceJSON: `{"checks":"passed"}`,
		Result:       "approved",
		CreatedAt:    "2026-06-12T00:00:00Z",
	}
	if err := InsertBrainActionAudit(db, entry); err != nil {
		t.Fatalf("InsertBrainActionAudit: %v", err)
	}
	rows, err := ListBrainActionAudit(db, "task", "brain-feature-pr", 10)
	if err != nil {
		t.Fatalf("ListBrainActionAudit: %v", err)
	}
	if len(rows) != 1 || rows[0].Action != "merge" || rows[0].Result != "approved" {
		t.Fatalf("audit rows = %#v", rows)
	}
}
```

- [ ] **Step 2: Run tests and confirm policy storage is absent**

Run:

```bash
go test ./internal/flowdb -run 'TestBrainPolicy' -count=1 -v
```

Expected:

```text
FAIL
```

- [ ] **Step 3: Add migrations**

Modify `internal/flowdb/db.go` migration setup with:

```go
CREATE TABLE IF NOT EXISTS brain_policy (
    action TEXT PRIMARY KEY,
    mode TEXT NOT NULL CHECK (mode IN ('auto', 'approval_required')),
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS brain_action_audit (
    id TEXT PRIMARY KEY,
    action TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    actor TEXT NOT NULL,
    policy TEXT NOT NULL,
    evidence_json TEXT NOT NULL DEFAULT '{}',
    result TEXT NOT NULL,
    error_text TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_brain_action_audit_target
ON brain_action_audit(target_type, target_id, created_at);
```

- [ ] **Step 4: Add policy helper code**

Create `internal/flowdb/brain_policy.go`:

```go
package flowdb

import (
	"database/sql"
	"time"
)

var BrainRiskyActions = []string{"merge", "deploy", "force_push", "destructive_shell", "delete_branch", "outbound_reply"}

type BrainPolicy struct {
	FullAuto       bool
	ActionModes    map[string]string
	RiskyWhitelist []string
	RequiresReview []string
}

func (p BrainPolicy) IsWhitelisted(action string) bool {
	return p.ActionModes[action] == "auto"
}

type BrainActionAudit struct {
	ID           string
	Action       string
	TargetType   string
	TargetID     string
	Actor        string
	Policy       string
	EvidenceJSON string
	Result       string
	ErrorText    sql.NullString
	CreatedAt    string
}

func GetBrainPolicy(db *sql.DB) (BrainPolicy, error) {
	rows, err := db.Query(`SELECT action, mode FROM brain_policy`)
	if err != nil {
		return BrainPolicy{}, err
	}
	defer rows.Close()

	modes := map[string]string{}
	for rows.Next() {
		var action, mode string
		if err := rows.Scan(&action, &mode); err != nil {
			return BrainPolicy{}, err
		}
		modes[action] = mode
	}
	if err := rows.Err(); err != nil {
		return BrainPolicy{}, err
	}

	p := BrainPolicy{FullAuto: true, ActionModes: modes}
	for _, action := range BrainRiskyActions {
		if modes[action] == "auto" {
			p.RiskyWhitelist = append(p.RiskyWhitelist, action)
		} else {
			p.ActionModes[action] = "approval_required"
			p.RequiresReview = append(p.RequiresReview, action)
		}
	}
	return p, nil
}

func SetBrainPolicyMode(db *sql.DB, action, mode string, now time.Time) error {
	_, err := db.Exec(`
		INSERT INTO brain_policy(action, mode, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(action) DO UPDATE SET mode=excluded.mode, updated_at=excluded.updated_at
	`, action, mode, now.Format(time.RFC3339))
	return err
}

func InsertBrainActionAudit(db *sql.DB, entry BrainActionAudit) error {
	_, err := db.Exec(`
		INSERT INTO brain_action_audit(id, action, target_type, target_id, actor, policy, evidence_json, result, error_text, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, entry.ID, entry.Action, entry.TargetType, entry.TargetID, entry.Actor, entry.Policy, entry.EvidenceJSON, entry.Result, entry.ErrorText, entry.CreatedAt)
	return err
}

func ListBrainActionAudit(db *sql.DB, targetType, targetID string, limit int) ([]BrainActionAudit, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := db.Query(`
		SELECT id, action, target_type, target_id, actor, policy, evidence_json, result, error_text, created_at
		FROM brain_action_audit
		WHERE target_type = ? AND target_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, targetType, targetID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BrainActionAudit
	for rows.Next() {
		var e BrainActionAudit
		if err := rows.Scan(&e.ID, &e.Action, &e.TargetType, &e.TargetID, &e.Actor, &e.Policy, &e.EvidenceJSON, &e.Result, &e.ErrorText, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
```

- [ ] **Step 5: Add approval nodes for denied risky actions**

In `BuildBrainGraph`, replace static policy creation with:

```go
policy, err := flowdb.GetBrainPolicy(db)
if err != nil {
	return BrainGraphView{}, err
}
view.Policy = BrainGraphPolicyView{
	FullAuto:         policy.FullAuto,
	RiskyWhitelist:   policy.RiskyWhitelist,
	ApprovalRequired: policy.RequiresReview,
}
```

Add approval node helper:

```go
func appendApprovalNode(view *BrainGraphView, taskSlug, action string) {
	id := "approval:" + action + ":" + taskSlug
	view.Nodes = append(view.Nodes, BrainGraphNode{
		ID:       id,
		Type:     "approval",
		TaskSlug: taskSlug,
		Label:    "Approve " + action,
		Status:   "approval_required",
		Summary:  action + " requires whitelist approval",
		Ref:      &BrainGraphRef{Kind: "approval", ID: action + ":" + taskSlug},
		Actions:  []string{"approve"},
	})
	view.Edges = append(view.Edges, BrainGraphEdge{
		ID:     "blocks:" + id,
		Type:   "blocks",
		Source: id,
		Target: "task:" + taskSlug,
		Label:  "policy gate",
		Status: "blocked",
	})
	view.Counts.ApprovalNeeded++
	view.Counts.Blocked++
}
```

- [ ] **Step 6: Run policy tests**

Run:

```bash
go test ./internal/flowdb -run 'TestBrainPolicy' -count=1 -v
go test ./internal/server -run 'TestBrainGraph' -count=1 -v
```

Expected:

```text
PASS
```

- [ ] **Step 7: Commit policy storage**

Run:

```bash
git add internal/flowdb/brain_policy.go internal/flowdb/brain_policy_test.go internal/flowdb/db.go internal/server/brain_graph.go internal/server/brain_graph_test.go
git commit -m "feat: add brain graph policy gates"
```

## Task 5: Read-Only Brain Graph UI

**Files:**
- Modify: `internal/server/ui/package.json`
- Modify: `internal/server/ui/pnpm-lock.yaml`
- Modify: `internal/server/ui/src/lib/types.ts`
- Modify: `internal/server/ui/src/lib/query.ts`
- Create: `internal/server/ui/src/screens/BrainGraph.tsx`
- Create: `internal/server/ui/src/components/brainGraph/BrainGraphCanvas.tsx`
- Create: `internal/server/ui/src/components/brainGraph/BrainGraphNode.tsx`
- Create: `internal/server/ui/src/components/brainGraph/OwnerBoundary.tsx`
- Create: `internal/server/ui/src/components/brainGraph/BrainGraphToolbar.tsx`
- Create: `internal/server/ui/src/components/brainGraph/BrainGraphLegend.tsx`
- Modify: `internal/server/ui/src/app.tsx`
- Modify: `internal/server/ui/src/components/Shell.tsx`
- Modify: `internal/server/ui/src/components/CommandPalette.tsx`
- Modify: `internal/server/ui/src/styles/app.css`

- [ ] **Step 1: Add the graph renderer dependency**

Run:

```bash
cd internal/server/ui
pnpm add @xyflow/react@12.11.0
cd ../../..
```

Expected:

```text
dependencies:
+ @xyflow/react 12.11.0
```

- [ ] **Step 2: Add TypeScript graph types**

Append to `internal/server/ui/src/lib/types.ts`:

```ts
export type BrainGraphView = {
  generated_at: string
  freshness: string
  controller: BrainGraphController
  policy: BrainGraphPolicy
  owners: BrainGraphOwner[]
  nodes: BrainGraphNode[]
  edges: BrainGraphEdge[]
  counts: BrainGraphCounts
  selected_actions: BrainGraphActionSpec[]
  warnings: BrainGraphWarning[]
}

export type BrainGraphController = {
  mode: 'global_brain'
  display_name: string
  status: string
}

export type BrainGraphPolicy = {
  full_auto: boolean
  risky_whitelist: string[]
  approval_required: string[]
  last_decision_at?: string
  last_decision_state?: string
}

export type BrainGraphOwner = {
  id: string
  slug: string
  name: string
  status: string
  task_count: number
  running_count: number
  blocked_count: number
  approval_count: number
}

export type BrainGraphNode = {
  id: string
  type: 'owner' | 'task' | 'worker_run' | 'validator_run' | 'steward_run' | 'approval' | 'event' | 'transcript' | 'log' | 'pr' | 'closeout'
  owner_slug?: string
  task_slug?: string
  parent_task_slug?: string
  label: string
  status: string
  priority?: string
  provider?: string
  harness?: string
  permission_mode?: string
  model?: string
  summary?: string
  expanded: boolean
  ref?: BrainGraphRef
  badges?: string[]
  actions?: string[]
  metadata?: Record<string, string>
}

export type BrainGraphRef = {
  kind: string
  id: string
  url?: string
}

export type BrainGraphEdge = {
  id: string
  type: 'contains' | 'parent' | 'depends_on' | 'run_of' | 'produced' | 'blocks' | 'external_ref'
  source: string
  target: string
  label?: string
  status?: string
}

export type BrainGraphCounts = {
  total_tasks: number
  running: number
  blocked: number
  failed: number
  approval_needed: number
  done: number
  owners: number
  warnings: number
}

export type BrainGraphActionSpec = {
  key: string
  label: string
  risky: boolean
  enabled: boolean
  disabled_reason?: string
}

export type BrainGraphWarning = {
  code: string
  message: string
  node_id?: string
}
```

- [ ] **Step 3: Add query hook**

Append to `internal/server/ui/src/lib/query.ts`:

```ts
export type BrainGraphFilters = {
  project?: string
  owner?: string
  status?: string
  includeDone?: boolean
  expand?: string[]
  q?: string
}

export function useBrainGraph(filters: BrainGraphFilters) {
  const params = new URLSearchParams()
  if (filters.project) params.set('project', filters.project)
  if (filters.owner) params.set('owner', filters.owner)
  if (filters.status) params.set('status', filters.status)
  if (filters.includeDone) params.set('include_done', 'true')
  if (filters.expand?.length) params.set('expand', filters.expand.join(','))
  if (filters.q) params.set('q', filters.q)
  const query = params.toString()
  return useQuery({
    queryKey: ['brain-graph', filters],
    queryFn: () => apiGet<BrainGraphView>(`/api/brain/graph${query ? `?${query}` : ''}`),
  })
}
```

Ensure the import list includes `BrainGraphView`.

- [ ] **Step 4: Create graph node renderers**

Create `internal/server/ui/src/components/brainGraph/BrainGraphNode.tsx`:

```tsx
import type { BrainGraphNode as BrainGraphNodeData } from '../../lib/types'

export function BrainGraphNode({ data }: { data: BrainGraphNodeData }) {
  return (
    <div className={`brain-node brain-node-${data.type} status-${data.status}`}>
      <div className="brain-node-top">
        <span className="brain-node-type">{data.type.replace('_', ' ')}</span>
        {data.priority ? <span className={`prio ${data.priority}`}>{data.priority}</span> : null}
      </div>
      <div className="brain-node-title">{data.label}</div>
      {data.summary ? <div className="brain-node-summary">{data.summary}</div> : null}
      <div className="brain-node-badges">
        {data.provider ? <span className="badge">{data.provider}</span> : null}
        {data.harness ? <span className="badge">{data.harness}</span> : null}
        {data.permission_mode ? <span className="badge">{data.permission_mode}</span> : null}
        {(data.badges ?? []).map((badge) => <span className="badge" key={badge}>{badge}</span>)}
      </div>
    </div>
  )
}
```

Create `internal/server/ui/src/components/brainGraph/OwnerBoundary.tsx`:

```tsx
import type { BrainGraphOwner } from '../../lib/types'

export function OwnerBoundary({ owner }: { owner: BrainGraphOwner }) {
  return (
    <div className="owner-boundary">
      <div className="owner-boundary-head">
        <div>
          <div className="owner-boundary-name">{owner.name}</div>
          <div className="owner-boundary-slug">owner:{owner.slug}</div>
        </div>
        <span className={`badge ${owner.status === 'active' ? 'ok' : ''}`}>{owner.status}</span>
      </div>
      <div className="owner-boundary-stats">
        <span>{owner.task_count} tasks</span>
        <span>{owner.running_count} running</span>
        <span>{owner.blocked_count} blocked</span>
      </div>
    </div>
  )
}
```

- [ ] **Step 5: Create the React Flow canvas**

Create `internal/server/ui/src/components/brainGraph/BrainGraphCanvas.tsx`:

```tsx
import '@xyflow/react/dist/style.css'
import {
  Background,
  Controls,
  MiniMap,
  ReactFlow,
  type Edge,
  type Node,
} from '@xyflow/react'
import type { BrainGraphEdge, BrainGraphNode as BrainGraphNodeData, BrainGraphView } from '../../lib/types'
import { BrainGraphNode } from './BrainGraphNode'

const nodeTypes = {
  brainNode: BrainGraphNode,
}

function toFlowNodes(graph: BrainGraphView): Node<BrainGraphNodeData>[] {
  return graph.nodes.map((node, index) => ({
    id: node.id,
    type: 'brainNode',
    position: {
      x: (index % 4) * 260,
      y: Math.floor(index / 4) * 170,
    },
    data: node,
  }))
}

function toFlowEdges(edges: BrainGraphEdge[]): Edge[] {
  return edges.map((edge) => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    label: edge.label,
    className: `brain-edge brain-edge-${edge.type} status-${edge.status ?? 'normal'}`,
    animated: edge.status === 'running',
  }))
}

export function BrainGraphCanvas({
  graph,
  selectedNodeId,
  onSelectNode,
}: {
  graph: BrainGraphView
  selectedNodeId?: string
  onSelectNode: (node?: BrainGraphNodeData) => void
}) {
  const nodes = toFlowNodes(graph).map((node) => ({
    ...node,
    selected: node.id === selectedNodeId,
  }))
  const edges = toFlowEdges(graph.edges)

  return (
    <div className="brain-graph-canvas">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        fitView
        minZoom={0.25}
        maxZoom={1.6}
        onNodeClick={(_, node) => onSelectNode(node.data)}
        onPaneClick={() => onSelectNode(undefined)}
      >
        <Background gap={24} size={1} />
        <Controls />
        <MiniMap pannable zoomable />
      </ReactFlow>
    </div>
  )
}
```

- [ ] **Step 6: Create toolbar and legend**

Create `internal/server/ui/src/components/brainGraph/BrainGraphToolbar.tsx`:

```tsx
import { Search } from 'lucide-react'
import type { BrainGraphView } from '../../lib/types'

export function BrainGraphToolbar({
  graph,
  query,
  onQuery,
  includeDone,
  onIncludeDone,
}: {
  graph: BrainGraphView
  query: string
  onQuery: (value: string) => void
  includeDone: boolean
  onIncludeDone: (value: boolean) => void
}) {
  return (
    <div className="brain-toolbar">
      <div>
        <div className="eyebrow">Global Brain</div>
        <h1 className="h-lg">Mission Control Graph</h1>
      </div>
      <div className="brain-counts">
        <span className="badge ok">{graph.counts.running} running</span>
        <span className="badge warn">{graph.counts.approval_needed} approvals</span>
        <span className="badge danger">{graph.counts.failed} failed</span>
        <span className="badge">{graph.counts.total_tasks} tasks</span>
      </div>
      <label className="brain-search">
        <Search size={15} />
        <input value={query} onChange={(event) => onQuery(event.target.value)} aria-label="Search graph" />
      </label>
      <label className="brain-toggle">
        <input type="checkbox" checked={includeDone} onChange={(event) => onIncludeDone(event.target.checked)} />
        <span>Done</span>
      </label>
    </div>
  )
}
```

Create `internal/server/ui/src/components/brainGraph/BrainGraphLegend.tsx`:

```tsx
export function BrainGraphLegend() {
  return (
    <div className="brain-legend">
      <span><i className="legend-dot running" /> Running</span>
      <span><i className="legend-dot blocked" /> Blocked</span>
      <span><i className="legend-dot failed" /> Failed</span>
      <span><i className="legend-line depends" /> Dependency</span>
      <span><i className="legend-line blocks" /> Policy gate</span>
    </div>
  )
}
```

- [ ] **Step 7: Create the Brain Graph screen**

Create `internal/server/ui/src/screens/BrainGraph.tsx`:

```tsx
import { useMemo, useState } from 'react'
import { BrainGraphCanvas } from '../components/brainGraph/BrainGraphCanvas'
import { BrainGraphLegend } from '../components/brainGraph/BrainGraphLegend'
import { BrainGraphToolbar } from '../components/brainGraph/BrainGraphToolbar'
import { FlowLoader } from '../components/Loaders'
import { useBrainGraph } from '../lib/query'
import type { BrainGraphNode } from '../lib/types'

export function BrainGraph() {
  const [query, setQuery] = useState('')
  const [includeDone, setIncludeDone] = useState(false)
  const [selected, setSelected] = useState<BrainGraphNode | undefined>()
  const graphQuery = useBrainGraph({ q: query, includeDone })
  const graph = graphQuery.data

  const selectedNodeId = useMemo(() => selected?.id, [selected])

  if (graphQuery.isLoading) {
    return <FlowLoader label="Loading Brain graph" />
  }
  if (graphQuery.isError || !graph) {
    return <div className="screen-error">Brain graph unavailable</div>
  }

  return (
    <div className="brain-screen">
      <BrainGraphToolbar graph={graph} query={query} onQuery={setQuery} includeDone={includeDone} onIncludeDone={setIncludeDone} />
      <div className="brain-main">
        <BrainGraphCanvas graph={graph} selectedNodeId={selectedNodeId} onSelectNode={setSelected} />
        <aside className="brain-inspector">
          <div className="eyebrow">Inspector</div>
          {selected ? (
            <>
              <h2>{selected.label}</h2>
              <p>{selected.summary ?? selected.status}</p>
              <div className="brain-inspector-grid">
                <span>Status</span><strong>{selected.status}</strong>
                <span>Type</span><strong>{selected.type}</strong>
                {selected.task_slug ? <><span>Task</span><strong>{selected.task_slug}</strong></> : null}
                {selected.owner_slug ? <><span>Owner</span><strong>{selected.owner_slug}</strong></> : null}
              </div>
            </>
          ) : (
            <p className="dim">Select a node to inspect task, run, evidence, and actions.</p>
          )}
          <BrainGraphLegend />
        </aside>
      </div>
    </div>
  )
}
```

- [ ] **Step 8: Wire route, nav, and command palette**

In `internal/server/ui/src/app.tsx`, import and route:

```tsx
import { BrainGraph } from './screens/BrainGraph'
```

Add route:

```tsx
{ path: '/brain', element: <BrainGraph /> },
```

In `internal/server/ui/src/components/Shell.tsx`, add a nav item using the existing nav array pattern:

```tsx
{ to: '/brain', label: 'Brain Graph', icon: <Network size={17} /> },
```

Add `Network` to the lucide import.

In `internal/server/ui/src/components/CommandPalette.tsx`, add:

```tsx
{ key: 'nav-brain', label: 'Brain Graph', to: '/brain', icon: <Network size={15} /> },
```

- [ ] **Step 9: Add graph CSS**

Append to `internal/server/ui/src/styles/app.css`:

```css
.brain-screen {
  display: flex;
  flex-direction: column;
  min-height: 100%;
  background: var(--bg);
}

.brain-toolbar {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) auto minmax(240px, 360px) auto;
  align-items: center;
  gap: 14px;
  padding: 18px 22px;
  border-bottom: 1px solid var(--border);
}

.brain-counts,
.brain-node-badges,
.brain-legend {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.brain-search {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 34px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
}

.brain-search input {
  min-width: 0;
  width: 100%;
  border: 0;
  outline: 0;
  background: transparent;
}

.brain-toggle {
  display: flex;
  align-items: center;
  gap: 7px;
  color: var(--text-2);
}

.brain-main {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 360px;
  min-height: 0;
  flex: 1;
}

.brain-graph-canvas {
  min-height: 640px;
  background:
    radial-gradient(circle at 1px 1px, var(--grid-dot) 1px, transparent 0) 0 0 / 24px 24px,
    var(--bg);
}

.brain-inspector {
  border-left: 1px solid var(--border);
  background: var(--bg-1);
  padding: 18px;
  overflow: auto;
}

.brain-node {
  width: 220px;
  min-height: 112px;
  padding: 11px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  box-shadow: var(--shadow-card);
}

.brain-node.status-running {
  border-color: color-mix(in srgb, var(--ok) 48%, var(--border));
}

.brain-node.status-dead,
.brain-node.status-failed {
  border-color: color-mix(in srgb, var(--danger) 56%, var(--border));
}

.brain-node.status-approval_required {
  border-color: color-mix(in srgb, var(--warn) 58%, var(--border));
}

.brain-node-top {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 7px;
}

.brain-node-type,
.owner-boundary-slug {
  font-family: var(--font-mono);
  font-size: 10.5px;
  color: var(--text-3);
  text-transform: uppercase;
}

.brain-node-title {
  font-weight: 600;
  line-height: 1.2;
  margin-bottom: 6px;
}

.brain-node-summary {
  color: var(--text-2);
  font-size: 12px;
  line-height: 1.35;
  margin-bottom: 8px;
}

.brain-inspector-grid {
  display: grid;
  grid-template-columns: 96px 1fr;
  gap: 8px 12px;
  margin: 16px 0;
  font-size: 13px;
}

.brain-inspector-grid span {
  color: var(--text-3);
}

.legend-dot,
.legend-line {
  display: inline-block;
  width: 10px;
  height: 10px;
  margin-right: 5px;
}

.legend-dot {
  border-radius: 50%;
}

.legend-dot.running { background: var(--ok); }
.legend-dot.blocked { background: var(--warn); }
.legend-dot.failed { background: var(--danger); }
.legend-line {
  width: 18px;
  height: 2px;
  vertical-align: middle;
  background: var(--border-strong);
}
.legend-line.depends { background: var(--info); }
.legend-line.blocks { background: var(--warn); }

@media (max-width: 980px) {
  .brain-toolbar {
    grid-template-columns: 1fr;
  }
  .brain-main {
    grid-template-columns: 1fr;
  }
  .brain-inspector {
    border-left: 0;
    border-top: 1px solid var(--border);
    max-height: 42vh;
  }
}
```

- [ ] **Step 10: Typecheck and build UI assets**

Run:

```bash
cd internal/server/ui
pnpm run typecheck
pnpm run build
cd ../../..
make ui
```

Expected:

```text
Done in
```

and `make ui` exits with code 0.

- [ ] **Step 11: Commit read-only graph UI**

Run:

```bash
git add internal/server/ui/package.json internal/server/ui/pnpm-lock.yaml internal/server/ui/src internal/server/static
git commit -m "feat: add read-only brain graph ui"
```

## Task 6: Inspector Evidence And Detail Loading

**Files:**
- Modify: `internal/server/brain_graph_types.go`
- Modify: `internal/server/brain_graph.go`
- Modify: `internal/server/brain_graph_test.go`
- Modify: `internal/server/server.go`
- Modify: `internal/server/ui/src/lib/types.ts`
- Modify: `internal/server/ui/src/lib/query.ts`
- Create: `internal/server/ui/src/components/brainGraph/BrainGraphInspector.tsx`
- Modify: `internal/server/ui/src/screens/BrainGraph.tsx`

- [ ] **Step 1: Add detail response types**

Add to `internal/server/brain_graph_types.go`:

```go
type BrainGraphNodeDetail struct {
	NodeID      string                    `json:"node_id"`
	Task       *TaskView                 `json:"task,omitempty"`
	Owner      *OwnerView                `json:"owner,omitempty"`
	Transcript *BrainGraphEvidenceDetail `json:"transcript,omitempty"`
	Logs       []BrainGraphEvidenceDetail `json:"logs,omitempty"`
	Events     []BrainGraphEvidenceDetail `json:"events,omitempty"`
	Audit      []BrainGraphAuditView      `json:"audit,omitempty"`
	Warnings   []BrainGraphWarning        `json:"warnings,omitempty"`
}

type BrainGraphEvidenceDetail struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Summary   string `json:"summary"`
	Path      string `json:"path,omitempty"`
	URL       string `json:"url,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

type BrainGraphAuditView struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Actor     string `json:"actor"`
	Policy    string `json:"policy"`
	Result    string `json:"result"`
	ErrorText string `json:"error_text,omitempty"`
	CreatedAt string `json:"created_at"`
}
```

- [ ] **Step 2: Write failing detail route test**

Append to `internal/server/brain_graph_test.go`:

```go
func TestBrainGraphTaskDetailIncludesTaskAndTranscriptRef(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainGraphTask(t, db, "detail-task", "Detail Task", "in-progress", "high", root, nil)
	_, err := db.Exec(`UPDATE tasks SET session_id='019eb65a-3e49-7d42-9f9f-2b68149e1a82', session_provider='codex' WHERE slug='detail-task'`)
	if err != nil {
		t.Fatalf("set session: %v", err)
	}

	s := New(Config{DB: db, FlowRoot: root})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/brain/graph/node/task:detail-task", nil)
	s.handleBrainGraphNodeDetail(rec, req, "task:detail-task")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got BrainGraphNodeDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if got.Task == nil || got.Task.Slug != "detail-task" {
		t.Fatalf("task detail = %#v", got.Task)
	}
	if got.Transcript == nil || got.Transcript.Kind != "transcript" {
		t.Fatalf("transcript detail = %#v", got.Transcript)
	}
}
```

- [ ] **Step 3: Implement detail handler**

In `internal/server/brain_graph.go`, add:

```go
func (s *Server) handleBrainGraphNodeDetail(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	detail, err := BuildBrainGraphNodeDetail(s.cfg.DB, s.cfg.FlowRoot, nodeID, time.Now())
	if err != nil {
		writeError(w, err, http.StatusNotFound)
		return
	}
	writeJSON(w, detail)
}

func BuildBrainGraphNodeDetail(db *sql.DB, root, nodeID string, now time.Time) (BrainGraphNodeDetail, error) {
	if strings.HasPrefix(nodeID, "task:") {
		slug := strings.TrimPrefix(nodeID, "task:")
		task, err := flowdb.GetTask(db, slug)
		if err != nil {
			return BrainGraphNodeDetail{}, err
		}
		live := LiveState{}
		taskView, err := BuildTaskView(db, root, task, live)
		if err != nil {
			return BrainGraphNodeDetail{}, err
		}
		detail := BrainGraphNodeDetail{NodeID: nodeID, Task: &taskView}
		if task.SessionID.Valid && task.SessionID.String != "" {
			detail.Transcript = &BrainGraphEvidenceDetail{
				ID:      slug,
				Kind:    "transcript",
				Title:   "Session transcript",
				Summary: "Transcript is available through the task session view.",
				URL:     "/sessions/" + slug,
			}
		}
		if task.AutoRunLog.Valid && task.AutoRunLog.String != "" {
			detail.Logs = append(detail.Logs, BrainGraphEvidenceDetail{
				ID:      slug + ":auto-log",
				Kind:    "log",
				Title:   "Auto run log",
				Summary: "Background run log",
				Path:    task.AutoRunLog.String,
			})
		}
		return detail, nil
	}
	return BrainGraphNodeDetail{}, fmt.Errorf("unsupported brain graph node %q", nodeID)
}
```

Register a prefix handler in `server.go`:

```go
mux.HandleFunc("/api/brain/graph/node/", func(w http.ResponseWriter, r *http.Request) {
	s.handleBrainGraphNodeDetail(w, r, strings.TrimPrefix(r.URL.Path, "/api/brain/graph/node/"))
})
```

Ensure `server.go` imports `strings` if it does not already.

- [ ] **Step 4: Add UI detail hook**

Append to `internal/server/ui/src/lib/query.ts`:

```ts
export function useBrainGraphNodeDetail(nodeId?: string) {
  return useQuery({
    queryKey: ['brain-graph-node', nodeId],
    enabled: Boolean(nodeId),
    queryFn: () => apiGet<BrainGraphNodeDetail>(`/api/brain/graph/node/${encodeURIComponent(nodeId ?? '')}`),
  })
}
```

Add matching TypeScript types to `types.ts`:

```ts
export type BrainGraphNodeDetail = {
  node_id: string
  task?: Task
  owner?: Owner
  transcript?: BrainGraphEvidenceDetail
  logs?: BrainGraphEvidenceDetail[]
  events?: BrainGraphEvidenceDetail[]
  audit?: BrainGraphAuditView[]
  warnings?: BrainGraphWarning[]
}

export type BrainGraphEvidenceDetail = {
  id: string
  kind: string
  title: string
  summary: string
  path?: string
  url?: string
  created_at?: string
}

export type BrainGraphAuditView = {
  id: string
  action: string
  actor: string
  policy: string
  result: string
  error_text?: string
  created_at: string
}
```

- [ ] **Step 5: Create evidence inspector**

Create `internal/server/ui/src/components/brainGraph/BrainGraphInspector.tsx`:

```tsx
import { ExternalLink } from 'lucide-react'
import { useBrainGraphNodeDetail } from '../../lib/query'
import type { BrainGraphNode } from '../../lib/types'

export function BrainGraphInspector({ selected }: { selected?: BrainGraphNode }) {
  const detail = useBrainGraphNodeDetail(selected?.id)

  if (!selected) {
    return (
      <aside className="brain-inspector">
        <div className="eyebrow">Inspector</div>
        <p className="dim">Select a node to inspect task, run, evidence, and actions.</p>
      </aside>
    )
  }

  const data = detail.data
  return (
    <aside className="brain-inspector">
      <div className="eyebrow">Inspector</div>
      <h2>{selected.label}</h2>
      <p>{selected.summary ?? selected.status}</p>
      <div className="brain-inspector-grid">
        <span>Status</span><strong>{selected.status}</strong>
        <span>Type</span><strong>{selected.type}</strong>
        {selected.task_slug ? <><span>Task</span><strong>{selected.task_slug}</strong></> : null}
        {selected.owner_slug ? <><span>Owner</span><strong>{selected.owner_slug}</strong></> : null}
      </div>
      {detail.isLoading ? <div className="spinner" /> : null}
      {data?.transcript ? (
        <a className="brain-evidence-link" href={data.transcript.url}>
          <span>{data.transcript.title}</span>
          <ExternalLink size={14} />
        </a>
      ) : null}
      {(data?.logs ?? []).map((log) => (
        <div className="brain-evidence-row" key={log.id}>
          <strong>{log.title}</strong>
          <span>{log.path ?? log.summary}</span>
        </div>
      ))}
      {(data?.warnings ?? []).map((warning) => (
        <div className="brain-warning" key={`${warning.code}:${warning.node_id ?? ''}`}>{warning.message}</div>
      ))}
    </aside>
  )
}
```

- [ ] **Step 6: Use the inspector in the screen**

Modify `internal/server/ui/src/screens/BrainGraph.tsx`:

```tsx
import { BrainGraphInspector } from '../components/brainGraph/BrainGraphInspector'
```

Replace the inline `<aside className="brain-inspector">...</aside>` with:

```tsx
<BrainGraphInspector selected={selected} />
```

- [ ] **Step 7: Run tests and UI typecheck**

Run:

```bash
go test ./internal/server -run 'TestBrainGraphTaskDetailIncludesTaskAndTranscriptRef|TestBrainGraph' -count=1 -v
cd internal/server/ui
pnpm run typecheck
cd ../../..
```

Expected:

```text
PASS
```

- [ ] **Step 8: Commit inspector evidence**

Run:

```bash
git add internal/server internal/server/ui/src
git commit -m "feat: add brain graph inspector evidence"
```

## Task 7: Graph Action Endpoints

**Files:**
- Create: `internal/server/brain_graph_actions.go`
- Create: `internal/server/brain_graph_actions_test.go`
- Modify: `internal/server/types.go`
- Modify: `internal/server/actions.go`
- Modify: `internal/server/server.go`
- Modify: `internal/server/ui/src/lib/types.ts`
- Modify: `internal/server/ui/src/lib/query.ts`
- Modify: `internal/server/ui/src/components/brainGraph/BrainGraphInspector.tsx`

- [ ] **Step 1: Add action request and response types**

Create `internal/server/brain_graph_actions.go`:

```go
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type BrainGraphActionRequest struct {
	NodeID      string            `json:"node_id"`
	TargetKind  string            `json:"target_kind"`
	TargetID    string            `json:"target_id"`
	Message     string            `json:"message"`
	RouteVia    string            `json:"route_via"`
	Action      string            `json:"action"`
	Metadata    map[string]string `json:"metadata"`
}

type BrainGraphActionResponse struct {
	OK        bool   `json:"ok"`
	Action    string `json:"action"`
	Output    string `json:"output,omitempty"`
	ErrorText string `json:"error_text,omitempty"`
	AuditID   string `json:"audit_id,omitempty"`
}
```

- [ ] **Step 2: Add an injectable Flow command runner for server tests**

Modify `internal/server/types.go` in `Server`:

```go
runFlowCommandFunc func(args ...string) (string, error)
```

Modify `internal/server/actions.go` at the top of `runFlowCommand`:

```go
func (s *Server) runFlowCommand(args ...string) (string, error) {
	if s.runFlowCommandFunc != nil {
		return s.runFlowCommandFunc(args...)
	}
	exe := s.cfg.CommandPath
```

- [ ] **Step 3: Write action endpoint tests**

Create `internal/server/brain_graph_actions_test.go`:

```go
package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrainGraphEventRequiresTargetAndMessage(t *testing.T) {
	root, db := testRootDB(t)
	s := New(Config{DB: db, FlowRoot: root})

	body := bytes.NewBufferString(`{"target_kind":"task","target_id":"worker"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/brain/graph/event", body)
	s.handleBrainGraphEvent(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
}

func TestBrainGraphOpenSessionUsesFlowCommand(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainGraphTask(t, db, "open-me", "Open Me", "backlog", "medium", root, nil)
	s := New(Config{DB: db, FlowRoot: root})
	var gotArgs []string
	s.runFlowCommandFunc = func(args ...string) (string, error) {
		gotArgs = append([]string{}, args...)
		return "opened tab: flow-open-me", nil
	}

	body := bytes.NewBufferString(`{"target_kind":"task","target_id":"open-me"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/brain/graph/open-session", body)
	s.handleBrainGraphOpenSession(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Join(gotArgs, " ") != "do open-me" {
		t.Fatalf("flow args = %#v, want do open-me", gotArgs)
	}

	var resp BrainGraphActionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.OK || resp.Action != "open_session" {
		t.Fatalf("response = %#v", resp)
	}
}
```

- [ ] **Step 4: Implement selected-node event sending through Flow**

Add to `brain_graph_actions.go`:

```go
func decodeBrainGraphAction(r *http.Request) (BrainGraphActionRequest, error) {
	var req BrainGraphActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, err
	}
	req.TargetKind = strings.TrimSpace(req.TargetKind)
	req.TargetID = strings.TrimSpace(req.TargetID)
	req.Message = strings.TrimSpace(req.Message)
	req.RouteVia = strings.TrimSpace(req.RouteVia)
	return req, nil
}

func (s *Server) handleBrainGraphEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	req, err := decodeBrainGraphAction(r)
	if err != nil {
		writeError(w, fmt.Errorf("invalid json"), http.StatusBadRequest)
		return
	}
	if req.TargetKind != "task" || req.TargetID == "" || req.Message == "" {
		writeError(w, fmt.Errorf("target_kind=task, target_id, and message are required"), http.StatusBadRequest)
		return
	}
	out, err := s.runFlowCommand("tell", req.TargetID, req.Message, "--from", "brain")
	resp := BrainGraphActionResponse{Action: "send_event", Output: out}
	if err != nil {
		resp.ErrorText = err.Error()
		writeJSONStatus(w, resp, http.StatusBadGateway)
		return
	}
	resp.OK = true
	writeJSON(w, resp)
}
```

- [ ] **Step 5: Implement open-session action**

Add:

```go
func (s *Server) handleBrainGraphOpenSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	req, err := decodeBrainGraphAction(r)
	if err != nil {
		writeError(w, fmt.Errorf("invalid json"), http.StatusBadRequest)
		return
	}
	if req.TargetKind != "task" || req.TargetID == "" {
		writeError(w, fmt.Errorf("target_kind=task and target_id are required"), http.StatusBadRequest)
		return
	}
	out, err := s.runFlowCommand("do", req.TargetID)
	resp := BrainGraphActionResponse{Action: "open_session", Output: out}
	if err != nil {
		resp.ErrorText = err.Error()
		writeJSONStatus(w, resp, http.StatusBadGateway)
		return
	}
	resp.OK = true
	writeJSON(w, resp)
}
```

- [ ] **Step 6: Implement seed, retry, pause, approve, and policy handlers**

Add explicit handlers with these behaviors:

```go
func (s *Server) handleBrainGraphSeed(w http.ResponseWriter, r *http.Request) {
	req, err := decodeBrainGraphAction(r)
	if err != nil || req.TargetID == "" || req.Message == "" {
		writeError(w, fmt.Errorf("target_id and message are required"), http.StatusBadRequest)
		return
	}
	out, runErr := s.runFlowCommand("tell", req.TargetID, "[seed] "+req.Message, "--from", "brain")
	writeBrainGraphActionResult(w, "seed", out, runErr)
}

func (s *Server) handleBrainGraphRetry(w http.ResponseWriter, r *http.Request) {
	req, err := decodeBrainGraphAction(r)
	if err != nil || req.TargetKind != "task" || req.TargetID == "" {
		writeError(w, fmt.Errorf("target_kind=task and target_id are required"), http.StatusBadRequest)
		return
	}
	out, runErr := s.runFlowCommand("do", req.TargetID, "--auto", "--force")
	writeBrainGraphActionResult(w, "retry", out, runErr)
}

func (s *Server) handleBrainGraphPause(w http.ResponseWriter, r *http.Request) {
	req, err := decodeBrainGraphAction(r)
	if err != nil || req.TargetID == "" {
		writeError(w, fmt.Errorf("target_id is required"), http.StatusBadRequest)
		return
	}
	if req.TargetKind == "owner" {
		out, runErr := s.runFlowCommand("owner", "pause", req.TargetID)
		writeBrainGraphActionResult(w, "pause", out, runErr)
		return
	}
	out, runErr := s.runFlowCommand("update", "task", req.TargetID, "--waiting", "Paused from Brain Graph")
	writeBrainGraphActionResult(w, "pause", out, runErr)
}
```

Add response helper:

```go
func writeBrainGraphActionResult(w http.ResponseWriter, action, out string, err error) {
	resp := BrainGraphActionResponse{Action: action, Output: out}
	if err != nil {
		resp.ErrorText = err.Error()
		writeJSONStatus(w, resp, http.StatusBadGateway)
		return
	}
	resp.OK = true
	writeJSON(w, resp)
}
```

- [ ] **Step 7: Register action routes**

Modify `internal/server/server.go`:

```go
mux.HandleFunc("/api/brain/graph/event", s.handleBrainGraphEvent)
mux.HandleFunc("/api/brain/graph/seed", s.handleBrainGraphSeed)
mux.HandleFunc("/api/brain/graph/open-session", s.handleBrainGraphOpenSession)
mux.HandleFunc("/api/brain/graph/retry", s.handleBrainGraphRetry)
mux.HandleFunc("/api/brain/graph/pause", s.handleBrainGraphPause)
mux.HandleFunc("/api/brain/graph/approve", s.handleBrainGraphApprove)
mux.HandleFunc("/api/brain/graph/policy", s.handleBrainGraphPolicy)
```

- [ ] **Step 8: Add UI mutation hooks**

Append to `internal/server/ui/src/lib/query.ts`:

```ts
export type BrainGraphActionRequest = {
  node_id?: string
  target_kind: string
  target_id: string
  message?: string
  route_via?: string
  action?: string
  metadata?: Record<string, string>
}

export type BrainGraphActionResponse = {
  ok: boolean
  action: string
  output?: string
  error_text?: string
  audit_id?: string
}

export function useBrainGraphAction(endpoint: 'event' | 'seed' | 'open-session' | 'retry' | 'pause' | 'approve' | 'policy') {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (body: BrainGraphActionRequest) =>
      apiPost<BrainGraphActionResponse>(`/api/brain/graph/${endpoint}`, body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['brain-graph'] })
    },
  })
}
```

- [ ] **Step 9: Add action controls to inspector**

In `BrainGraphInspector.tsx`, add an event composer for task nodes:

```tsx
const sendEvent = useBrainGraphAction('event')
const openSession = useBrainGraphAction('open-session')
const [message, setMessage] = useState('')

function targetBody() {
  return {
    node_id: selected.id,
    target_kind: selected.task_slug ? 'task' : selected.owner_slug ? 'owner' : selected.type,
    target_id: selected.task_slug ?? selected.owner_slug ?? selected.id,
  }
}
```

Render controls:

```tsx
<div className="brain-actions">
  {selected.task_slug ? (
    <button className="btn primary" onClick={() => openSession.mutate(targetBody())}>Open</button>
  ) : null}
  <textarea className="textarea" value={message} onChange={(event) => setMessage(event.target.value)} aria-label="Send input to selected node" />
  <button
    className="btn"
    disabled={!message.trim() || sendEvent.isPending}
    onClick={() => sendEvent.mutate({ ...targetBody(), message })}
  >
    Send event
  </button>
</div>
```

- [ ] **Step 10: Run action tests and UI typecheck**

Run:

```bash
go test ./internal/server -run 'TestBrainGraph.*Action|TestBrainGraphOpenSessionUsesFlowCommand|TestBrainGraphEventRequiresTargetAndMessage' -count=1 -v
cd internal/server/ui
pnpm run typecheck
cd ../../..
```

Expected:

```text
PASS
```

- [ ] **Step 11: Commit graph actions**

Run:

```bash
git add internal/server/brain_graph_actions.go internal/server/brain_graph_actions_test.go internal/server/types.go internal/server/actions.go internal/server/server.go internal/server/ui/src
git commit -m "feat: add brain graph node actions"
```

## Task 8: Full-Auto Policy Controls

**Files:**
- Modify: `internal/server/brain_graph_actions.go`
- Modify: `internal/server/brain_graph_actions_test.go`
- Modify: `internal/server/brain_graph.go`
- Modify: `internal/server/ui/src/components/brainGraph/BrainGraphPolicyPanel.tsx`
- Modify: `internal/server/ui/src/components/brainGraph/BrainGraphInspector.tsx`
- Modify: `internal/server/ui/src/screens/BrainGraph.tsx`
- Modify: `internal/server/ui/src/styles/app.css`

- [ ] **Step 1: Add server tests for policy mutation and approve action**

Update the import block in `internal/server/brain_graph_actions_test.go` to include policy storage:

```go
import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"flow/internal/flowdb"
)
```

Append to `internal/server/brain_graph_actions_test.go`:

```go
func TestBrainGraphPolicyWhitelistsRiskyAction(t *testing.T) {
	root, db := testRootDB(t)
	s := New(Config{DB: db, FlowRoot: root})

	body := bytes.NewBufferString(`{"action":"merge","metadata":{"mode":"auto"}}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/brain/graph/policy", body)
	s.handleBrainGraphPolicy(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	policy, err := flowdb.GetBrainPolicy(db)
	if err != nil {
		t.Fatalf("GetBrainPolicy: %v", err)
	}
	if !policy.IsWhitelisted("merge") {
		t.Fatal("merge should be whitelisted")
	}
}

func TestBrainGraphApproveWritesAudit(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainGraphTask(t, db, "merge-task", "Merge Task", "in-progress", "high", root, nil)
	s := New(Config{DB: db, FlowRoot: root})

	body := bytes.NewBufferString(`{"target_kind":"task","target_id":"merge-task","action":"merge","message":"checks passed"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/brain/graph/approve", body)
	s.handleBrainGraphApprove(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	audit, err := flowdb.ListBrainActionAudit(db, "task", "merge-task", 10)
	if err != nil {
		t.Fatalf("ListBrainActionAudit: %v", err)
	}
	if len(audit) != 1 || audit[0].Action != "merge" || audit[0].Result != "approved" {
		t.Fatalf("audit = %#v", audit)
	}
}
```

- [ ] **Step 2: Implement policy and approve handlers**

Add to `brain_graph_actions.go`:

```go
func (s *Server) handleBrainGraphPolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	req, err := decodeBrainGraphAction(r)
	if err != nil {
		writeError(w, fmt.Errorf("invalid json"), http.StatusBadRequest)
		return
	}
	mode := req.Metadata["mode"]
	if req.Action == "" || (mode != "auto" && mode != "approval_required") {
		writeError(w, fmt.Errorf("action and metadata.mode=auto|approval_required are required"), http.StatusBadRequest)
		return
	}
	if err := flowdb.SetBrainPolicyMode(s.cfg.DB, req.Action, mode, time.Now()); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, BrainGraphActionResponse{OK: true, Action: "policy"})
}

func (s *Server) handleBrainGraphApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	req, err := decodeBrainGraphAction(r)
	if err != nil {
		writeError(w, fmt.Errorf("invalid json"), http.StatusBadRequest)
		return
	}
	if req.TargetKind == "" || req.TargetID == "" || req.Action == "" {
		writeError(w, fmt.Errorf("target_kind, target_id, and action are required"), http.StatusBadRequest)
		return
	}
	auditID := newUUID()
	err = flowdb.InsertBrainActionAudit(s.cfg.DB, flowdb.BrainActionAudit{
		ID:           auditID,
		Action:       req.Action,
		TargetType:   req.TargetKind,
		TargetID:     req.TargetID,
		Actor:        "operator",
		Policy:       "operator_approved",
		EvidenceJSON: fmt.Sprintf(`{"message":%q}`, req.Message),
		Result:       "approved",
		CreatedAt:    time.Now().Format(time.RFC3339),
	})
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, BrainGraphActionResponse{OK: true, Action: "approve", AuditID: auditID})
}
```

Use the repo's existing UUID helper if `newUUID()` is not available in `internal/server`; otherwise add a small unexported helper in `brain_graph_actions.go`:

```go
func newUUID() string {
	return uuid.NewString()
}
```

and import the same UUID package already used by the app layer.

- [ ] **Step 3: Create policy panel**

Create `internal/server/ui/src/components/brainGraph/BrainGraphPolicyPanel.tsx`:

```tsx
import { ShieldCheck } from 'lucide-react'
import { useBrainGraphAction } from '../../lib/query'
import type { BrainGraphPolicy } from '../../lib/types'

const riskyActions = ['merge', 'deploy', 'force_push', 'destructive_shell', 'delete_branch', 'outbound_reply']

export function BrainGraphPolicyPanel({ policy }: { policy: BrainGraphPolicy }) {
  const mutatePolicy = useBrainGraphAction('policy')

  return (
    <div className="brain-policy">
      <div className="brain-policy-head">
        <ShieldCheck size={16} />
        <strong>Full-auto policy</strong>
        <span className={`badge ${policy.full_auto ? 'ok' : 'warn'}`}>{policy.full_auto ? 'enabled' : 'paused'}</span>
      </div>
      <div className="brain-policy-list">
        {riskyActions.map((action) => {
          const enabled = policy.risky_whitelist.includes(action)
          return (
            <label className="brain-policy-row" key={action}>
              <span>{action.replaceAll('_', ' ')}</span>
              <input
                type="checkbox"
                checked={enabled}
                onChange={(event) => mutatePolicy.mutate({
                  target_kind: 'policy',
                  target_id: action,
                  action,
                  metadata: { mode: event.target.checked ? 'auto' : 'approval_required' },
                })}
              />
            </label>
          )
        })}
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Render policy controls**

In `BrainGraph.tsx`, import:

```tsx
import { BrainGraphPolicyPanel } from '../components/brainGraph/BrainGraphPolicyPanel'
```

Render under the inspector or toolbar area:

```tsx
<BrainGraphPolicyPanel policy={graph.policy} />
```

- [ ] **Step 5: Add approve button for approval nodes**

In `BrainGraphInspector.tsx`, add:

```tsx
const approve = useBrainGraphAction('approve')
```

Render for approval nodes:

```tsx
{selected.type === 'approval' ? (
  <button
    className="btn primary"
    onClick={() => approve.mutate({
      node_id: selected.id,
      target_kind: 'task',
      target_id: selected.task_slug ?? selected.id,
      action: selected.ref?.id.split(':')[0] ?? 'approve',
      message: 'Approved from Brain Graph',
    })}
  >
    Approve
  </button>
) : null}
```

- [ ] **Step 6: Run policy tests and UI typecheck**

Run:

```bash
go test ./internal/flowdb -run 'TestBrainPolicy' -count=1 -v
go test ./internal/server -run 'TestBrainGraphPolicy|TestBrainGraphApprove|TestBrainGraph' -count=1 -v
cd internal/server/ui
pnpm run typecheck
cd ../../..
```

Expected:

```text
PASS
```

- [ ] **Step 7: Commit policy controls**

Run:

```bash
git add internal/flowdb internal/server internal/server/ui/src
git commit -m "feat: add brain graph policy controls"
```

## Task 9: Validator And Steward Progression

**Files:**
- Modify: `internal/server/brain_graph.go`
- Modify: `internal/server/brain_graph_test.go`
- Modify after branch reconciliation: Brain scheduler, validator, and steward files under `internal/brain` or `internal/app`

- [ ] **Step 1: Write tests for validator blocking steward**

Append to `internal/server/brain_graph_test.go`:

```go
func TestBrainGraphBlocksStewardWhenValidationMissing(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainGraphTask(t, db, "feature-task", "Feature Task", "in-progress", "high", root, nil)
	if err := flowdb.AddTaskTag(db, "feature-task", "gh-pr:Facets-cloud/flow-manager#1"); err != nil {
		t.Fatalf("tag pr: %v", err)
	}

	view, err := BuildBrainGraph(db, root, BrainGraphFilters{Expand: map[string]bool{"task:feature-task": true}}, time.Now())
	if err != nil {
		t.Fatalf("BuildBrainGraph: %v", err)
	}
	if !graphHasNode(view.Nodes, "approval:validation:feature-task") {
		t.Fatalf("missing validation approval/block node: %#v", view.Nodes)
	}
	if !graphHasEdge(view.Edges, "blocks", "approval:validation:feature-task", "task:feature-task") {
		t.Fatalf("missing validation block edge: %#v", view.Edges)
	}
}
```

- [ ] **Step 2: Add validation state projection**

Add a validation state helper:

```go
type brainGraphValidationState struct {
	TaskSlug string
	Status   string
	Summary  string
}

func validationStateForTask(task *flowdb.Task, runRows []brainGraphRunRow) brainGraphValidationState {
	for _, row := range runRows {
		if row.TaskSlug == task.Slug && row.Role == "validator" {
			return brainGraphValidationState{TaskSlug: task.Slug, Status: row.Status, Summary: row.Summary}
		}
	}
	return brainGraphValidationState{TaskSlug: task.Slug, Status: "missing", Summary: "Validation evidence required before steward progression"}
}
```

When an expanded task has GitHub PR tags and validation status is `missing`, add:

```go
appendValidationBlockNode(&view, t.Slug)
```

Implement:

```go
func appendValidationBlockNode(view *BrainGraphView, taskSlug string) {
	id := "approval:validation:" + taskSlug
	view.Nodes = append(view.Nodes, BrainGraphNode{
		ID:       id,
		Type:     "approval",
		TaskSlug: taskSlug,
		Label:    "Validation required",
		Status:   "blocked",
		Summary:  "Steward progression is blocked until validator evidence exists",
		Actions:  []string{"retry"},
	})
	view.Edges = append(view.Edges, BrainGraphEdge{
		ID:     "validation-block:" + taskSlug,
		Type:   "blocks",
		Source: id,
		Target: "task:" + taskSlug,
		Label:  "validation",
		Status: "blocked",
	})
	view.Counts.Blocked++
}
```

- [ ] **Step 3: Add branch target policy warning**

In the steward projection, compare the PR base branch from Brain/PR evidence with the required branch target:

```go
func appendWrongBranchWarning(view *BrainGraphView, taskSlug, actualBase, requiredBase string) {
	if actualBase == "" || requiredBase == "" || actualBase == requiredBase {
		return
	}
	view.Warnings = append(view.Warnings, BrainGraphWarning{
		Code:    "wrong_branch_target",
		Message: "Task " + taskSlug + " targets " + actualBase + " but Brain work must target " + requiredBase,
		NodeID:  "task:" + taskSlug,
	})
	view.Counts.Warnings = len(view.Warnings)
	appendApprovalNode(view, taskSlug, "merge")
}
```

For Brain family tasks, pass `requiredBase = "feature/flow-brain-orchestrator"`.

- [ ] **Step 4: Run progression tests**

Run:

```bash
go test ./internal/server -run 'TestBrainGraphBlocksStewardWhenValidationMissing|TestBrainGraph' -count=1 -v
```

Expected:

```text
PASS
```

- [ ] **Step 5: Commit validator and steward graph state**

Run:

```bash
git add internal/server/brain_graph.go internal/server/brain_graph_test.go
git commit -m "feat: show validator and steward graph gates"
```

## Task 10: Owner Workspace

**Files:**
- Create: `internal/server/ui/src/screens/OwnerWorkspace.tsx`
- Modify: `internal/server/ui/src/app.tsx`
- Modify: `internal/server/ui/src/screens/Owners.tsx`
- Modify: `internal/server/ui/src/components/brainGraph/OwnerBoundary.tsx`
- Modify: `internal/server/ui/src/styles/app.css`
- Verify: `internal/server/owners.go`
- Verify: `internal/server/owners_test.go`

- [ ] **Step 1: Add owner workspace route**

Create `internal/server/ui/src/screens/OwnerWorkspace.tsx`:

```tsx
import { useParams } from 'react-router-dom'
import { BrainGraphCanvas } from '../components/brainGraph/BrainGraphCanvas'
import { FlowLoader } from '../components/Loaders'
import { useBrainGraph, useOwner } from '../lib/query'

export function OwnerWorkspace() {
  const { slug = '' } = useParams()
  const owner = useOwner(slug)
  const graph = useBrainGraph({ owner: slug, includeDone: true })

  if (owner.isLoading || graph.isLoading) {
    return <FlowLoader label="Loading owner workspace" />
  }
  if (owner.isError || graph.isError || !graph.data) {
    return <div className="screen-error">Owner workspace unavailable</div>
  }

  return (
    <div className="owner-workspace">
      <header className="owner-workspace-head">
        <div>
          <div className="eyebrow">Owner</div>
          <h1 className="h-lg">{owner.data?.name ?? slug}</h1>
          <p className="dim">{slug}</p>
        </div>
      </header>
      <section className="owner-workspace-grid">
        <div className="owner-workspace-charter">
          <div className="eyebrow">Charter</div>
          <pre>{owner.data?.charter ?? 'No charter loaded'}</pre>
        </div>
        <BrainGraphCanvas graph={graph.data} onSelectNode={() => {}} />
      </section>
    </div>
  )
}
```

- [ ] **Step 2: Add route**

Modify `internal/server/ui/src/app.tsx`:

```tsx
import { OwnerWorkspace } from './screens/OwnerWorkspace'
```

Add route:

```tsx
{ path: '/brain/owner/:slug', element: <OwnerWorkspace /> },
```

- [ ] **Step 3: Link owner boundaries to workspace**

Modify `OwnerBoundary.tsx`:

```tsx
import { Link } from 'react-router-dom'
```

Wrap the boundary head action:

```tsx
<Link className="btn sm" to={`/brain/owner/${owner.slug}`}>Open</Link>
```

- [ ] **Step 4: Update Owners page to point at Brain workspace**

In `internal/server/ui/src/screens/Owners.tsx`, change each owner row action to link to:

```tsx
to={`/brain/owner/${owner.slug}`}
```

Keep existing start/pause/tick controls available from either Owners or the workspace.

- [ ] **Step 5: Add owner workspace CSS**

Append to `app.css`:

```css
.owner-workspace {
  display: flex;
  flex-direction: column;
  min-height: 100%;
}

.owner-workspace-head {
  padding: 18px 22px;
  border-bottom: 1px solid var(--border);
}

.owner-workspace-grid {
  display: grid;
  grid-template-columns: 360px minmax(0, 1fr);
  min-height: 0;
  flex: 1;
}

.owner-workspace-charter {
  border-right: 1px solid var(--border);
  padding: 18px;
  overflow: auto;
  background: var(--bg-1);
}

.owner-workspace-charter pre {
  white-space: pre-wrap;
  color: var(--text-2);
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.55;
}

@media (max-width: 980px) {
  .owner-workspace-grid {
    grid-template-columns: 1fr;
  }
  .owner-workspace-charter {
    border-right: 0;
    border-bottom: 1px solid var(--border);
    max-height: 38vh;
  }
}
```

- [ ] **Step 6: Run UI typecheck**

Run:

```bash
cd internal/server/ui
pnpm run typecheck
cd ../../..
```

Expected:

```text
Done in
```

- [ ] **Step 7: Commit owner workspace**

Run:

```bash
git add internal/server/ui/src
git commit -m "feat: add brain owner workspace"
```

## Task 11: End-To-End Verification, Docs, And Final PR

**Files:**
- Modify: `internal/app/skill/SKILL.md`
- Modify: `README.md`
- Verify generated assets under: `internal/server/static`

- [ ] **Step 1: Add operator docs to the embedded skill**

Append a concise Brain Graph section to `internal/app/skill/SKILL.md`:

```markdown
### Mission Control Brain Graph

Mission Control exposes `/brain` as the graph-first view of the global Brain.
The graph is a projection of Flow owners, tasks, dependencies, Brain runs,
autonomous run logs, transcripts, inbox events, GitHub tags, and policy audit
records. Owners appear as boundaries; task and subtask nodes are the primary
workflow; active, failed, and selected tasks expand into worker, validator,
steward, approval, transcript, log, PR, and closeout evidence.

Use the graph to inspect autonomy, send selected-node input, seed a future run,
open a task session, retry failed safe runs, pause owner/task scopes, and approve
policy-gated risky actions. Merge, deploy, force-push, destructive shell, branch
deletion, and outbound reply remain blocked until a whitelist rule or explicit
operator approval exists.
```

- [ ] **Step 2: Add README smoke-test docs**

Add to `README.md`:

```markdown
## Mission Control Brain Graph

Run Mission Control and open `/brain` to inspect the global Brain graph. The
graph groups tasks by `owner:<slug>` tags, inherits owner boundaries through
parent tasks, shows dependencies and autonomous run evidence, and routes graph
actions through Flow server APIs.

Smoke test:

```bash
make ui
go test ./internal/flowdb ./internal/server ./internal/app
flow ui serve --host 127.0.0.1 --port 8787
```

Then open `http://127.0.0.1:8787/brain`.
```

- [ ] **Step 3: Run full backend verification**

Run:

```bash
go test ./...
```

Expected:

```text
ok
```

If unrelated pre-existing tests fail, rerun the exact failing package and record the failing test name, command, and error in the PR body.

- [ ] **Step 4: Run UI build**

Run:

```bash
make ui
```

Expected:

```text
pnpm run build
```

and exit code 0.

- [ ] **Step 5: Start local server for browser smoke**

Run:

```bash
flow ui serve --host 127.0.0.1 --port 8787 --bg
curl -fsS http://127.0.0.1:8787/api/health
curl -fsS http://127.0.0.1:8787/api/brain/graph | head -c 200
```

Expected:

```text
ok
```

and the graph API response begins with:

```json
{"generated_at":
```

- [ ] **Step 6: Browser smoke check**

Open:

```text
http://127.0.0.1:8787/brain
```

Verify:

- Brain Graph nav item opens the page.
- Owner boundaries render.
- Task nodes render without overlap at desktop width.
- Search filters nodes through the server query.
- Selecting a task opens inspector detail.
- Open action triggers the existing session-open flow.
- Sending an event writes through the server and invalidates the graph query.
- Policy panel toggles risky whitelist state and graph approval nodes update.
- Mobile width stacks inspector under the graph.

- [ ] **Step 7: Rebuild embedded static assets and check diff**

Run:

```bash
make ui
git status --short
git diff --stat
```

Expected changed areas:

```text
internal/server/static
internal/server/ui
internal/server
internal/flowdb
internal/app/skill/SKILL.md
README.md
```

- [ ] **Step 8: Commit docs and final assets**

Run:

```bash
git add internal/app/skill/SKILL.md README.md internal/server/static
git commit -m "docs: document brain graph workflow"
```

- [ ] **Step 9: Push Brain feature branch to origin**

Run:

```bash
git push origin feature/flow-brain-orchestrator
```

Expected:

```text
feature/flow-brain-orchestrator -> feature/flow-brain-orchestrator
```

- [ ] **Step 10: Create final PR only after the feature branch is coherent**

Run:

```bash
gh pr create \
  --repo vishnukv-facets/flow-manager \
  --base main \
  --head feature/flow-brain-orchestrator \
  --title "feat: add Mission Control Brain Graph" \
  --body-file /tmp/brain-graph-pr.md
```

Use this PR body:

```markdown
## Summary

- adds a Flow-backed Mission Control Brain Graph at `/brain`
- groups task families inside owner boundaries using `owner:<slug>` tags and parent inheritance
- expands autonomous work into worker, validator, steward, evidence, approval, PR, transcript, log, and closeout nodes
- adds graph actions for selected-node events, seeding, open session, retry, pause, approval, and policy updates
- gates risky actions through full-auto whitelist policy and audit records

## Verification

- `go test ./...`
- `make ui`
- `curl -fsS http://127.0.0.1:8787/api/brain/graph`
- browser smoke at `/brain`
```

- [ ] **Step 11: Close Flow tasks only after merge is confirmed**

After GitHub shows the final PR merged, run the authoritative closeout for the relevant Brain tasks:

```bash
flow done brain-ui-command-center
flow done brain-validator-runs
flow done brain-steward-merge-queue
flow done brain-feature-pr
flow done flow-brain-feature
```

Expected for each:

```text
marked done
```

If any task refuses closeout because a branch is not merged into `feature/flow-brain-orchestrator`, merge or PR that branch into the feature branch first, then rerun the closeout.

## Final Verification Checklist

- [ ] `feature/flow-brain-orchestrator` contains the harness/owners reconciliation commit.
- [ ] No commits were pushed to `Facets-cloud/flow`.
- [ ] Child work targets `feature/flow-brain-orchestrator`.
- [ ] Final PR targets `main` from `feature/flow-brain-orchestrator`.
- [ ] `GET /api/brain/graph` returns owner boundaries, task nodes, edges, counts, warnings, and policy.
- [ ] Owner tags and parent inheritance drive graph boundaries.
- [ ] Active, dead, and selected tasks expand into evidence nodes.
- [ ] Risky actions create approval nodes unless whitelisted.
- [ ] Selected-node event sending uses `flow tell` through the server.
- [ ] Open-session action uses the existing Flow session path.
- [ ] UI builds through `make ui`.
- [ ] Backend passes `go test ./...` or documents any unrelated pre-existing failure with exact command output.
- [ ] Browser smoke confirms the graph renders and the inspector works on desktop and mobile widths.
