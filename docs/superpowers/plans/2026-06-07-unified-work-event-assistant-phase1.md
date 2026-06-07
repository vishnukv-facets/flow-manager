# Unified WorkEvent Assistant Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the shared WorkEvent read model and use it to fix the GitHub attention drops, Mission Control bucket confusion, and broken orphan FYI links.

**Architecture:** Add a focused `internal/workevents` package that derives normalized assistant work events from existing Flow sources: `attention_feed`, `steering_trace`, task metadata, tags, and `inbox.jsonl`. Server, briefing, Inbox, Attention, and Ask Flow consume the same event contract instead of each reclassifying work independently. Stage 0 keeps deterministic filtering but consults task-linked GitHub refs before dropping self-authored or authorless lifecycle events.

**Tech Stack:** Go, `database/sql`, `modernc.org/sqlite`, existing `flowdb`, `monitor`, `steering`, `briefing`, and React/TypeScript Mission Control UI.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/workevents/types.go` | Shared WorkEvent types, bucket constants, link helpers, bucket ranking, filters, response counts. |
| `internal/workevents/builder.go` | Pure builder that derives WorkEvents from DB rows, task tags, and task `inbox.jsonl`. |
| `internal/workevents/types_test.go` | Unit tests for bucket ranking, link validation, and filtering. |
| `internal/workevents/builder_test.go` | Builder tests for attention items, GitHub inbox events, waiting/next-up/closeout buckets, and orphan-safe links. |
| `internal/server/work_events.go` | `/api/work-events` handler adapter and query parsing. |
| `internal/server/routes.go` | Register `/api/work-events`. |
| `internal/server/work_events_test.go` | API response and filter tests. |
| `internal/steering/stage0.go` | GitHub Stage 0 routing change for task-linked/self-authored/authorless events. |
| `internal/steering/config.go` | Populate task-linked GitHub thread refs in `WatchConfigFnWithMutes`. |
| `internal/steering/stage0_test.go` | GitHub routing regression tests. |
| `internal/briefing/briefing.go` | Split briefing buckets and skip broken orphan task links. |
| `internal/briefing/briefing_test.go` | Mission Control bucket and orphan-link regression tests. |
| `internal/server/ui/src/lib/types.ts` | Add WorkEvent response types and briefing bucket additions. |
| `internal/server/ui/src/lib/query.ts` | Add `useWorkEvents` query hook. |
| `internal/server/ui/src/components/WorkEventRow.tsx` | Shared dense row for bucket/reason/source/task links. |
| `internal/server/ui/src/screens/Inbox.tsx` | Display shared bucket/reason metadata for inbox rows. |
| `internal/server/ui/src/screens/Attention.tsx` | Display shared bucket/reason metadata for attention rows and trace detail. |
| `internal/server/ui/src/screens/Overview.tsx` | Render split Mission Control sections. |
| `internal/server/ask_flow.go` | Answer "what needs me", "what changed", and "what can I close" from WorkEvents. |
| `internal/server/ask_flow_test.go` | Grounded Ask Flow WorkEvent tests. |

---

## Task 1: Core WorkEvent Types

**Files:**
- Create: `internal/workevents/types.go`
- Test: `internal/workevents/types_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/workevents/types_test.go
package workevents

import "testing"

func TestBucketOrdering(t *testing.T) {
	cases := []struct {
		a, b Bucket
		want Bucket
	}{
		{BucketFYI, BucketNeedsAction, BucketNeedsAction},
		{BucketWaiting, BucketCloseout, BucketCloseout},
		{BucketIgnored, BucketHandled, BucketHandled},
		{BucketNextUp, BucketFYI, BucketNextUp},
	}
	for _, c := range cases {
		if got := StrongerBucket(c.a, c.b); got != c.want {
			t.Errorf("StrongerBucket(%q, %q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}

func TestEventLinkValidation(t *testing.T) {
	valid := Link{Kind: "task", Target: "autonomy-trust-ladder"}
	if !valid.Valid() {
		t.Fatalf("task link should be valid: %+v", valid)
	}
	invalid := Link{Kind: "task", Target: ""}
	if invalid.Valid() {
		t.Fatalf("empty target link should be invalid: %+v", invalid)
	}
}

func TestFilterMatches(t *testing.T) {
	ev := Event{Source: "github", Bucket: BucketNeedsAction, TaskSlug: "autonomy-trust-ladder"}
	if !Filter{Source: "github", Bucket: BucketNeedsAction, TaskSlug: "autonomy-trust-ladder"}.Matches(ev) {
		t.Fatal("exact filter should match")
	}
	if Filter{Source: "slack"}.Matches(ev) {
		t.Fatal("different source should not match")
	}
}
```

- [ ] **Step 2: Run the tests and confirm failure**

Run:

```bash
go test ./internal/workevents -run 'TestBucketOrdering|TestEventLinkValidation|TestFilterMatches' -v
```

Expected: package does not exist.

- [ ] **Step 3: Implement the types**

```go
// internal/workevents/types.go
package workevents

import "strings"

type Bucket string

const (
	BucketNeedsAction Bucket = "needs_action"
	BucketCloseout    Bucket = "closeout"
	BucketWaiting     Bucket = "waiting"
	BucketNextUp      Bucket = "next_up"
	BucketFYI         Bucket = "fyi"
	BucketHandled     Bucket = "handled"
	BucketIgnored     Bucket = "ignored"
)

type Link struct {
	Kind   string `json:"kind"`
	Label  string `json:"label,omitempty"`
	Target string `json:"target"`
	URL    string `json:"url,omitempty"`
}

func (l Link) Valid() bool {
	return strings.TrimSpace(l.Kind) != "" && strings.TrimSpace(l.Target) != ""
}

type Event struct {
	ID             string  `json:"id"`
	Source         string  `json:"source"`
	Kind           string  `json:"kind"`
	EventKey       string  `json:"event_key,omitempty"`
	ThreadKey      string  `json:"thread_key,omitempty"`
	URL            string  `json:"url,omitempty"`
	Title          string  `json:"title"`
	Summary        string  `json:"summary,omitempty"`
	Actor          string  `json:"actor,omitempty"`
	AuthoredBySelf bool    `json:"authored_by_self,omitempty"`
	OccurredAt     string  `json:"occurred_at,omitempty"`
	ObservedAt     string  `json:"observed_at,omitempty"`
	TaskSlug       string  `json:"task_slug,omitempty"`
	ProjectSlug    string  `json:"project_slug,omitempty"`
	EntityKind     string  `json:"entity_kind,omitempty"`
	EntityRef      string  `json:"entity_ref,omitempty"`
	Bucket         Bucket  `json:"bucket"`
	Urgency        string  `json:"urgency,omitempty"`
	Confidence     float64 `json:"confidence,omitempty"`
	ReasonCode     string  `json:"reason_code,omitempty"`
	ReasonText     string  `json:"reason_text,omitempty"`
	Links          []Link  `json:"links,omitempty"`
}

type Counts struct {
	NeedsAction int `json:"needs_action"`
	Closeout    int `json:"closeout"`
	Waiting     int `json:"waiting"`
	NextUp      int `json:"next_up"`
	FYI         int `json:"fyi"`
	Handled     int `json:"handled"`
	Ignored     int `json:"ignored"`
}

type Result struct {
	Items  []Event `json:"items"`
	Counts Counts  `json:"counts"`
}

type Filter struct {
	Source   string
	Bucket   Bucket
	TaskSlug string
	Limit    int
}

func (f Filter) Matches(ev Event) bool {
	if f.Source != "" && ev.Source != f.Source {
		return false
	}
	if f.Bucket != "" && ev.Bucket != f.Bucket {
		return false
	}
	if f.TaskSlug != "" && ev.TaskSlug != f.TaskSlug {
		return false
	}
	return true
}

func StrongerBucket(a, b Bucket) Bucket {
	if bucketRank(a) <= bucketRank(b) {
		return a
	}
	return b
}

func bucketRank(b Bucket) int {
	switch b {
	case BucketNeedsAction:
		return 0
	case BucketCloseout:
		return 1
	case BucketWaiting:
		return 2
	case BucketNextUp:
		return 3
	case BucketFYI:
		return 4
	case BucketHandled:
		return 5
	case BucketIgnored:
		return 6
	default:
		return 7
	}
}

func Count(items []Event) Counts {
	var c Counts
	for _, it := range items {
		switch it.Bucket {
		case BucketNeedsAction:
			c.NeedsAction++
		case BucketCloseout:
			c.Closeout++
		case BucketWaiting:
			c.Waiting++
		case BucketNextUp:
			c.NextUp++
		case BucketFYI:
			c.FYI++
		case BucketHandled:
			c.Handled++
		case BucketIgnored:
			c.Ignored++
		}
	}
	return c
}
```

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./internal/workevents -run 'TestBucketOrdering|TestEventLinkValidation|TestFilterMatches' -v
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/workevents/types.go internal/workevents/types_test.go
git commit -m "Add WorkEvent core types"
```

---

## Task 2: WorkEvent Builder For Existing Sources

**Files:**
- Create: `internal/workevents/builder.go`
- Test: `internal/workevents/builder_test.go`

- [ ] **Step 1: Write failing builder tests**

```go
// internal/workevents/builder_test.go
package workevents

import (
	"database/sql"
	"path/filepath"
	"testing"

	"flow/internal/flowdb"
	"flow/internal/monitor"
)

func testDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("FLOW_ROOT", root)
	t.Setenv("HOME", root)
	db, err := flowdb.OpenDB(filepath.Join(root, "flow.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, root
}

func seedProject(t *testing.T, db *sql.DB) {
	t.Helper()
	now := "2026-06-07T08:00:00Z"
	if _, err := db.Exec(`INSERT INTO projects (slug,name,status,priority,work_dir,created_at,updated_at)
		VALUES ('flow-manager','Flow Manager','active','high',?,?,?)`, t.TempDir(), now, now); err != nil {
		t.Fatalf("seed project: %v", err)
	}
}

func seedTask(t *testing.T, db *sql.DB, slug, status, priority string) {
	t.Helper()
	now := "2026-06-07T08:00:00Z"
	if _, err := db.Exec(`INSERT INTO tasks (slug,name,project_slug,status,priority,work_dir,session_provider,created_at,updated_at)
		VALUES (?,?, 'flow-manager', ?, ?, ?, 'codex', ?, ?)`, slug, slug, status, priority, t.TempDir(), now, now); err != nil {
		t.Fatalf("seed task %s: %v", slug, err)
	}
}

func TestBuildIncludesAttentionAsNeedsAction(t *testing.T) {
	db, root := testDB(t)
	seedProject(t, db)
	seedTask(t, db, "deploy-followup", "in-progress", "high")
	if _, err := flowdb.UpsertFeedItem(db, flowdb.FeedItem{
		ID: "feed1", Source: "slack", ThreadKey: "C1:1", Summary: "Deploy needs reply",
		SuggestedAction: "reply", MatchedTask: "deploy-followup", SuggestedProject: "flow-manager",
		Urgency: "urgent", Confidence: 0.92, Reason: "operator was asked a direct question",
		URL: "https://example.slack.com/archives/C1/p1", Status: "new", CreatedAt: "2026-06-07T08:02:00Z",
	}); err != nil {
		t.Fatalf("seed feed: %v", err)
	}
	got, err := Build(db, root, Filter{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ev := requireEvent(t, got.Items, "attention:feed1")
	if ev.Bucket != BucketNeedsAction || ev.ReasonCode != "attention_unresolved" {
		t.Fatalf("attention event = %+v", ev)
	}
}

func TestBuildClassifiesGitHubTaskLinkedInboxEvents(t *testing.T) {
	db, root := testDB(t)
	seedProject(t, db)
	seedTask(t, db, "autonomy-trust-ladder", "in-progress", "high")
	if err := flowdb.AddTaskTag(db, "autonomy-trust-ladder", "gh-pr:vishnukv-facets/flow-manager#21"); err != nil {
		t.Fatalf("tag: %v", err)
	}
	if err := monitor.AppendInboxEvent("autonomy-trust-ladder", monitor.InboundEvent{
		Kind: "pr_head_updated", ChannelType: "github", Channel: "vishnukv-facets/flow-manager",
		ThreadTS: "gh-pr:vishnukv-facets/flow-manager#21", URL: "https://github.com/vishnukv-facets/flow-manager/pull/21",
		Text: "Pull request head changed. Review the PR again.", UserID: "",
	}); err != nil {
		t.Fatalf("append inbox: %v", err)
	}
	got, err := Build(db, root, Filter{Source: "github"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ev := requireKind(t, got.Items, "pr_head_updated")
	if ev.Bucket != BucketNeedsAction || ev.ReasonCode != "github_task_linked_pr_head_updated" {
		t.Fatalf("github head update event = %+v", ev)
	}
}

func TestBuildClassifiesMergedPRAsCloseout(t *testing.T) {
	db, root := testDB(t)
	seedProject(t, db)
	seedTask(t, db, "ask-flow-command-center", "in-progress", "high")
	if err := flowdb.AddTaskTag(db, "ask-flow-command-center", "gh-pr:vishnukv-facets/flow-manager#19"); err != nil {
		t.Fatalf("tag: %v", err)
	}
	if err := monitor.AppendInboxEvent("ask-flow-command-center", monitor.InboundEvent{
		Kind: "pr_merged", ChannelType: "github", Channel: "vishnukv-facets/flow-manager",
		ThreadTS: "gh-pr:vishnukv-facets/flow-manager#19", URL: "https://github.com/vishnukv-facets/flow-manager/pull/19",
		Text: "Pull request merged.",
	}); err != nil {
		t.Fatalf("append inbox: %v", err)
	}
	got, err := Build(db, root, Filter{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ev := requireKind(t, got.Items, "pr_merged")
	if ev.Bucket != BucketCloseout || ev.ReasonCode != "github_task_linked_pr_merged" {
		t.Fatalf("merged PR event = %+v", ev)
	}
}

func requireEvent(t *testing.T, items []Event, id string) Event {
	t.Helper()
	for _, it := range items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("event %s missing from %+v", id, items)
	return Event{}
}

func requireKind(t *testing.T, items []Event, kind string) Event {
	t.Helper()
	for _, it := range items {
		if it.Kind == kind {
			return it
		}
	}
	t.Fatalf("kind %s missing from %+v", kind, items)
	return Event{}
}
```

- [ ] **Step 2: Run tests and confirm failure**

Run:

```bash
go test ./internal/workevents -run 'TestBuildIncludesAttentionAsNeedsAction|TestBuildClassifiesGitHubTaskLinkedInboxEvents|TestBuildClassifiesMergedPRAsCloseout' -v
```

Expected: build failure for undefined `Build`.

- [ ] **Step 3: Implement the builder**

Add `Build(db *sql.DB, flowRoot string, filter Filter) (Result, error)` in
`internal/workevents/builder.go` with this structure:

```go
package workevents

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"flow/internal/flowdb"
	"flow/internal/monitor"
)

func Build(db *sql.DB, flowRoot string, filter Filter) (Result, error) {
	if db == nil {
		return Result{}, fmt.Errorf("workevents: db is required")
	}
	tasks, err := flowdb.ListTasks(db, flowdb.TaskFilter{IncludeArchived: false})
	if err != nil {
		return Result{}, err
	}
	bySlug := map[string]*flowdb.Task{}
	for _, task := range tasks {
		bySlug[task.Slug] = task
	}

	var items []Event
	attention, err := attentionEvents(db, bySlug)
	if err != nil {
		return Result{}, err
	}
	items = append(items, attention...)
	items = append(items, taskStateEvents(db, tasks)...)
	items = append(items, inboxEvents(tasks)...)

	items = filterAndSort(items, filter)
	return Result{Items: items, Counts: Count(items)}, nil
}
```

Implement helper behavior exactly:

- `attentionEvents` maps every `flowdb.ListFeedItems(db, "new")` row to
  `BucketNeedsAction`, `Kind: "attention"`, `ID: "attention:"+row.ID`,
  `ReasonCode: "attention_unresolved"`, and links for attention, valid task,
  source URL, and trace when `GetSteeringTraceByFeedItem` succeeds.
- `taskStateEvents` maps active tasks with `waiting_on` to `BucketWaiting` and
  high-priority startable backlog tasks to `BucketNextUp`.
- `inboxEvents` uses `monitor.ReadInboxEntries(task.Slug)`, derives source with
  `monitor.ClassifyInboxEvent`, and maps GitHub kinds with `githubBucket`.
- `githubBucket` returns:
  - `pr_head_updated` + task slug: `BucketNeedsAction`,
    `github_task_linked_pr_head_updated`
  - `pr_merged` + task slug: `BucketCloseout`,
    `github_task_linked_pr_merged`
  - `pr_involved`: `BucketFYI`, `github_involved_fyi`
  - comments/reviews on task-linked events: `BucketNeedsAction`,
    `github_task_linked_comment`
  - all other GitHub entries: `BucketFYI`, `github_activity_fyi`
- `filterAndSort` applies `Filter.Matches`, sorts by `ObservedAt` descending,
  then applies `Limit` when `Limit > 0`.

- [ ] **Step 4: Run the builder tests**

Run:

```bash
go test ./internal/workevents -v
```

Expected: all workevents tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/workevents
git commit -m "Build WorkEvents from Flow activity"
```

---

## Task 3: `/api/work-events`

**Files:**
- Create: `internal/server/work_events.go`
- Modify: `internal/server/routes.go`
- Test: `internal/server/work_events_test.go`

- [ ] **Step 1: Write failing API test**

```go
// internal/server/work_events_test.go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"flow/internal/flowdb"
)

func TestHandleWorkEventsFiltersByBucketAndSource(t *testing.T) {
	s, db := attentionTestServer(t)
	if _, err := flowdb.UpsertFeedItem(db, flowdb.FeedItem{
		ID: "we-feed", Source: "github", ThreadKey: "gh:1", Summary: "PR needs review",
		SuggestedAction: "forward", MatchedTask: "", Urgency: "normal", Confidence: 0.9,
		Reason: "task-linked PR changed", URL: "https://github.com/o/r/pull/1",
		Status: "new", CreatedAt: "2026-06-07T08:00:00Z",
	}); err != nil {
		t.Fatalf("seed feed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/work-events?bucket=needs_action&source=github", nil)
	rec := httptest.NewRecorder()
	s.handleWorkEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			ID     string `json:"id"`
			Source string `json:"source"`
			Bucket string `json:"bucket"`
		} `json:"items"`
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].Source != "github" || body.Items[0].Bucket != "needs_action" {
		t.Fatalf("body = %+v", body)
	}
}
```

- [ ] **Step 2: Run test and confirm failure**

Run:

```bash
go test ./internal/server -run TestHandleWorkEventsFiltersByBucketAndSource -v
```

Expected: build failure for undefined `handleWorkEvents`.

- [ ] **Step 3: Implement handler**

Create `internal/server/work_events.go`:

```go
package server

import (
	"net/http"
	"strconv"
	"strings"

	"flow/internal/workevents"
)

func (s *Server) handleWorkEvents(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	filter := workevents.Filter{
		Source:   strings.TrimSpace(r.URL.Query().Get("source")),
		Bucket:   workevents.Bucket(strings.TrimSpace(r.URL.Query().Get("bucket"))),
		TaskSlug: strings.TrimSpace(r.URL.Query().Get("task")),
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			filter.Limit = n
		}
	}
	result, err := workevents.Build(s.cfg.DB, s.cfg.FlowRoot, filter)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}
```

Wire route in `internal/server/routes.go` alongside other `/api/*` routes:

```go
mux.HandleFunc("/api/work-events", s.handleWorkEvents)
```

- [ ] **Step 4: Run server tests**

Run:

```bash
go test ./internal/server -run 'TestHandleWorkEventsFiltersByBucketAndSource|TestHandleOverviewIncludesBriefing' -v
```

Expected: tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/server/work_events.go internal/server/routes.go internal/server/work_events_test.go
git commit -m "Expose WorkEvents API"
```

---

## Task 4: GitHub Stage 0 Task-Linked Routing

**Files:**
- Modify: `internal/steering/stage0.go`
- Modify: `internal/steering/config.go`
- Test: `internal/steering/stage0_test.go`

- [ ] **Step 1: Write failing Stage 0 tests**

```go
func TestStage0GitHubAllowsTaskLinkedSelfAuthoredHeadUpdate(t *testing.T) {
	cfg := githubCfg()
	cfg.TaskLinkedGitHubThreads = map[string]bool{"o/r:gh-pr:o/r#21": true}
	ev := ghEvent("o/r", "octocat-self", "head changed")
	ev.Kind = "pr_head_updated"
	ev.ThreadTS = "gh-pr:o/r#21"
	got := Stage0(ev, cfg)
	if !got.Pass {
		t.Fatalf("Stage0 = %+v, want task-linked self-authored head update to pass", got)
	}
}

func TestStage0GitHubAllowsTaskLinkedAuthorlessHeadUpdate(t *testing.T) {
	cfg := githubCfg()
	cfg.TaskLinkedGitHubThreads = map[string]bool{"o/r:gh-pr:o/r#21": true}
	ev := ghEvent("o/r", "", "head changed")
	ev.Kind = "pr_head_updated"
	ev.ThreadTS = "gh-pr:o/r#21"
	got := Stage0(ev, cfg)
	if !got.Pass {
		t.Fatalf("Stage0 = %+v, want task-linked authorless head update to pass", got)
	}
}

func TestStage0GitHubStillDropsUnlinkedSelfAuthoredInvolved(t *testing.T) {
	cfg := githubCfg()
	ev := ghEvent("o/r", "octocat-self", "self authored")
	ev.Kind = "pr_involved"
	ev.ThreadTS = "gh-pr:o/r#21"
	got := Stage0(ev, cfg)
	if got.Pass || got.DropReason != "self-authored" {
		t.Fatalf("Stage0 = %+v, want self-authored drop", got)
	}
}
```

- [ ] **Step 2: Run tests and confirm failure**

Run:

```bash
go test ./internal/steering -run 'TestStage0GitHubAllowsTaskLinked|TestStage0GitHubStillDropsUnlinkedSelfAuthoredInvolved' -v
```

Expected: build failure for missing `TaskLinkedGitHubThreads`, then failing
Stage 0 behavior.

- [ ] **Step 3: Implement task-linked GitHub pass-through**

Add to `WatchConfig`:

```go
TaskLinkedGitHubThreads map[string]bool
```

Add helper in `stage0.go`:

```go
func githubTaskLinked(ev monitor.InboundEvent, cfg WatchConfig) bool {
	key := monitor.ThreadKey(ev.Channel, ev.ThreadTS)
	return key != "" && cfg.TaskLinkedGitHubThreads[key]
}

func githubLifecycleNeedsTaskAttention(kind string) bool {
	switch kind {
	case "pr_head_updated", "pr_merged", "pr_review_changes_requested", "pr_review_comment", "pr_comment", "issue_comment":
		return true
	default:
		return false
	}
}
```

Change the self/no-author checks in `stage0GitHub`:

```go
taskLinked := githubTaskLinked(ev, cfg)
if containsFold(cfg.GitHubIdentity, ev.UserID) && !(taskLinked && githubLifecycleNeedsTaskAttention(ev.Kind)) {
	return Stage0Result{DropReason: "self-authored"}
}
if strings.TrimSpace(ev.UserID) == "" && !(taskLinked && githubLifecycleNeedsTaskAttention(ev.Kind)) {
	return Stage0Result{DropReason: "no author"}
}
```

Populate the map in `WatchConfigFnWithMutes` by listing non-archived tasks and
their tags. Add every `gh-pr:` and `gh-issue:` tag as
`monitor.ThreadKey(repo, tag)`, where `repo` is parsed from the tag body before
`#`.

- [ ] **Step 4: Run steering tests**

Run:

```bash
go test ./internal/steering -run 'TestStage0GitHub|TestWatchConfigFnWithMutes' -v
```

Expected: tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/steering/stage0.go internal/steering/config.go internal/steering/stage0_test.go internal/steering/config_test.go
git commit -m "Route task-linked GitHub events through Stage 0"
```

---

## Task 5: Mission Control Buckets And Orphan Links

**Files:**
- Modify: `internal/briefing/briefing.go`
- Test: `internal/briefing/briefing_test.go`
- Modify as needed: `internal/server/ui/src/lib/types.ts`
- Modify as needed: `internal/server/ui/src/screens/Overview.tsx`

- [ ] **Step 1: Write failing briefing tests**

```go
func TestBriefingSeparatesNeedsActionWaitingNextUpAndFYI(t *testing.T) {
	db, root := briefingTestDB(t)
	seedBriefingProject(t, db, root, "flow-manager")
	seedBriefingTask(t, db, root, taskSeed{Slug: "waiting-task", Name: "Waiting task", Project: "flow-manager", Status: "in-progress", Priority: "high", WaitingOn: "reviewer"})
	seedBriefingTask(t, db, root, taskSeed{Slug: "ready-task", Name: "Ready task", Project: "flow-manager", Status: "backlog", Priority: "high"})
	if _, err := flowdb.UpsertFeedItem(db, flowdb.FeedItem{
		ID: "feed-action", Source: "github", ThreadKey: "gh:1", Summary: "PR changed",
		SuggestedAction: "forward", Urgency: "normal", Confidence: 0.9, Status: "new", CreatedAt: "2026-06-07T08:00:00Z",
	}); err != nil {
		t.Fatalf("seed feed: %v", err)
	}
	got, err := Build(db, root, Options{Now: time.Date(2026, 6, 7, 9, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	requireItem(t, got.NeedsAction, "attention", "feed-action")
	requireItem(t, got.Waiting, "waiting", "waiting-task")
	requireItem(t, got.NextUp, "ready", "ready-task")
}

func TestBriefingSkipsTaskLinkForOrphanUpdateDirectory(t *testing.T) {
	db, root := briefingTestDB(t)
	seedBriefingProject(t, db, root, "flow-manager")
	writeBriefingUpdate(t, root, "ghost-task", "2026-06-07-note.md", "Ghost note\n")
	got, err := Build(db, root, Options{Now: time.Date(2026, 6, 7, 9, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	item := requireItem(t, got.FYI, "update", "ghost-task")
	for _, link := range item.Links {
		if link.Kind == "task" {
			t.Fatalf("orphan update should not contain task link: %+v", item.Links)
		}
	}
}
```

- [ ] **Step 2: Run tests and confirm failure**

Run:

```bash
go test ./internal/briefing -run 'TestBriefingSeparatesNeedsActionWaitingNextUpAndFYI|TestBriefingSkipsTaskLinkForOrphanUpdateDirectory' -v
```

Expected: missing `Waiting` and `NextUp` fields, orphan task link still present.

- [ ] **Step 3: Implement briefing split**

Change `Briefing`:

```go
type Briefing struct {
	GeneratedAt string `json:"generated_at"`
	WindowStart string `json:"window_start"`
	WindowEnd   string `json:"window_end"`
	NeedsAction []Item `json:"needs_action"`
	Closeout    []Item `json:"closeout"`
	Waiting     []Item `json:"waiting"`
	NextUp      []Item `json:"next_up"`
	FYI         []Item `json:"fyi"`
}
```

Change `Build` so:

- attention items remain in `NeedsAction`
- waiting task items go to `Waiting`
- stale in-progress items go to `FYI` with detail `stale session`
- high-priority startable backlog goes to `NextUp`
- shipped/done work remains `FYI`
- future WorkEvent closeout items can be appended to `Closeout`

Fix `taskUpdateItems`:

```go
links := []Link{{Kind: "update", Target: file.Path}}
if task != nil {
	links = append([]Link{{Kind: "task", Target: slug}}, links...)
}
```

- [ ] **Step 4: Update Overview UI types and render sections**

Update `BriefingView` in `internal/server/ui/src/lib/types.ts` with:

```ts
closeout: BriefingItem[]
waiting: BriefingItem[]
next_up: BriefingItem[]
```

Update `Overview.tsx` so Mission Control renders sections in this order:

1. Needs action
2. Closeout
3. Waiting
4. Next up
5. FYI

Keep the existing dense two-column layout; only move items into clearer
sections.

- [ ] **Step 5: Run tests and typecheck**

Run:

```bash
go test ./internal/briefing ./internal/server -run 'TestBriefing|TestHandleOverviewIncludesBriefing' -v
npm run typecheck --prefix internal/server/ui
```

Expected: tests and typecheck pass.

- [ ] **Step 6: Commit**

```bash
git add internal/briefing/briefing.go internal/briefing/briefing_test.go internal/server/ui/src/lib/types.ts internal/server/ui/src/screens/Overview.tsx
git commit -m "Split briefing work buckets"
```

---

## Task 6: Shared UI Metadata For Inbox And Attention

**Files:**
- Create: `internal/server/ui/src/components/WorkEventRow.tsx`
- Modify: `internal/server/ui/src/lib/types.ts`
- Modify: `internal/server/ui/src/lib/query.ts`
- Modify: `internal/server/ui/src/screens/Inbox.tsx`
- Modify: `internal/server/ui/src/screens/Attention.tsx`

- [ ] **Step 1: Add TypeScript types**

Add:

```ts
export type WorkEventBucket =
  | 'needs_action'
  | 'closeout'
  | 'waiting'
  | 'next_up'
  | 'fyi'
  | 'handled'
  | 'ignored'

export type WorkEventLink = {
  kind: string
  label?: string
  target: string
  url?: string
}

export type WorkEvent = {
  id: string
  source: string
  kind: string
  title: string
  summary?: string
  task_slug?: string
  bucket: WorkEventBucket
  urgency?: string
  reason_code?: string
  reason_text?: string
  links?: WorkEventLink[]
}

export type WorkEventResponse = {
  items: WorkEvent[]
  counts: Record<WorkEventBucket, number>
}
```

- [ ] **Step 2: Add query hook**

In `query.ts`, add:

```ts
export function useWorkEvents(params: { bucket?: string; source?: string; task?: string; limit?: number } = {}) {
  const qs = new URLSearchParams()
  if (params.bucket) qs.set('bucket', params.bucket)
  if (params.source) qs.set('source', params.source)
  if (params.task) qs.set('task', params.task)
  if (params.limit) qs.set('limit', String(params.limit))
  return useQuery<WorkEventResponse>({
    queryKey: ['work-events', params],
    queryFn: () => apiGet(`/api/work-events${qs.toString() ? `?${qs}` : ''}`),
  })
}
```

- [ ] **Step 3: Add shared row component**

Create `WorkEventRow.tsx` with a compact row that renders source icon, bucket
badge, title, reason, and task/source links. Use existing button/link classes
and no nested cards.

- [ ] **Step 4: Wire Inbox and Attention**

- Inbox: call `useWorkEvents({ limit: 100 })` and decorate rows by matching
  `task_slug + kind + source` where possible. If no match, keep existing row.
- Attention: call `useWorkEvents({ bucket: 'needs_action', limit: 100 })` and
  display matching `reason_text` in card detail. Keep existing feed actions.

- [ ] **Step 5: Run typecheck**

Run:

```bash
npm run typecheck --prefix internal/server/ui
```

Expected: typecheck passes.

- [ ] **Step 6: Commit**

```bash
git add internal/server/ui/src/components/WorkEventRow.tsx internal/server/ui/src/lib/types.ts internal/server/ui/src/lib/query.ts internal/server/ui/src/screens/Inbox.tsx internal/server/ui/src/screens/Attention.tsx
git commit -m "Show shared WorkEvent metadata in Inbox and Attention"
```

---

## Task 7: Ask Flow WorkEvent Answers

**Files:**
- Modify: `internal/server/ask_flow.go`
- Test: `internal/server/ask_flow_test.go`

- [ ] **Step 1: Write failing Ask Flow tests**

Add tests for:

- `"what needs me right now"` returns WorkEvents in `needs_action`
- `"what changed"` includes recent GitHub/Slack WorkEvents
- `"what can i close"` returns `closeout` WorkEvents

Each test should seed the same DB patterns used in `workevents` tests and
assert citations include `task`, `source`, or `trace` links.

- [ ] **Step 2: Run tests and confirm failure**

Run:

```bash
go test ./internal/server -run 'TestAskFlow.*WorkEvent|TestAskFlow.*NeedsMe|TestAskFlow.*Close' -v
```

Expected: existing Ask Flow intent routing does not answer from WorkEvents.

- [ ] **Step 3: Implement WorkEvent-backed intents**

In `ask_flow.go`, add deterministic intent matching before generic search:

```go
case strings.Contains(q, "needs me") || strings.Contains(q, "needs my attention"):
	return s.askFlowWorkEvents(workevents.Filter{Bucket: workevents.BucketNeedsAction, Limit: 8})
case strings.Contains(q, "can i close") || strings.Contains(q, "what can i close"):
	return s.askFlowWorkEvents(workevents.Filter{Bucket: workevents.BucketCloseout, Limit: 8})
case strings.Contains(q, "what changed"):
	return s.askFlowWorkEvents(workevents.Filter{Limit: 8})
```

Implement `askFlowWorkEvents` to:

- call `workevents.Build`
- render concise bullets from `Title`, `ReasonText`, and `TaskSlug`
- convert WorkEvent links into existing Ask Flow citations
- return an empty-state answer when no matching events exist

- [ ] **Step 4: Run server tests**

Run:

```bash
go test ./internal/server -run 'TestAskFlow|TestHandleWorkEvents' -v
```

Expected: tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/server/ask_flow.go internal/server/ask_flow_test.go
git commit -m "Answer Ask Flow attention questions from WorkEvents"
```

---

## Task 8: Final Verification

**Files:**
- Verify all files changed in prior tasks.

- [ ] **Step 1: Run focused Go tests**

```bash
go test ./internal/workevents ./internal/steering ./internal/briefing ./internal/server ./internal/flowdb -count=1
```

Expected: all pass.

- [ ] **Step 2: Run UI typecheck**

```bash
npm run typecheck --prefix internal/server/ui
```

Expected: typecheck passes.

- [ ] **Step 3: Build UI if static assets changed**

```bash
npm run build --prefix internal/server/ui
```

Expected: Vite build succeeds and updates `internal/server/static` if the repo tracks generated assets.

- [ ] **Step 4: Run full Go test sweep**

```bash
go test ./... -count=1
```

Expected: all pass.

- [ ] **Step 5: Local smoke**

Start the local server:

```bash
go run . ui serve --addr 127.0.0.1:8787
```

Open:

```text
http://127.0.0.1:8787
```

Smoke checks:

- `/api/work-events` returns JSON with `items` and `counts`
- Mission Control shows separate Needs action, Closeout, Waiting, Next up, FYI sections
- Inbox rows still load
- Attention feed/trace still load
- Clicking briefing FYI/update rows does not produce `sql: no rows in result set`

- [ ] **Step 6: Commit verification fixes if needed**

```bash
git status --short
git add internal/workevents internal/steering internal/briefing internal/server internal/server/ui
git commit -m "Polish WorkEvent assistant integration"
```

Expected: skip this step when verification produced no code changes.
