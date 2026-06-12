package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"flow/internal/flowdb"
)

func TestBrainGraphEmptyRoute(t *testing.T) {
	root, db := testRootDB(t)
	s := New(Config{DB: db, FlowRoot: root})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/brain/graph", nil)
	s.handleBrainGraph(rec, req)

	assertBrainGraphEmptyResponse(t, rec)
}

func TestBrainGraphEmptyRouteRegistered(t *testing.T) {
	root, db := testRootDB(t)
	s := New(Config{DB: db, FlowRoot: root})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/brain/graph", nil)
	s.Handler().ServeHTTP(rec, req)

	assertBrainGraphEmptyResponse(t, rec)
}

func TestBrainGraphGroupsTasksByOwnerTagAndInheritance(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainGraphOwner(t, db, root, "brain-ui")
	insertBrainGraphTask(t, db, "parent", "Parent", "backlog", nil)
	insertBrainGraphTask(t, db, "child", "Child", "backlog", strPtr("parent"))
	insertBrainGraphTask(t, db, "other", "Other", "backlog", nil)
	if err := flowdb.AddTaskTag(db, "parent", "owner:brain-ui"); err != nil {
		t.Fatalf("AddTaskTag: %v", err)
	}

	got, err := BuildBrainGraph(db, root, BrainGraphFilters{}, time.Date(2026, 6, 12, 10, 0, 0, 0, time.FixedZone("IST", 19800)))
	if err != nil {
		t.Fatalf("BuildBrainGraph: %v", err)
	}

	nodes := graphNodesByTask(got)
	if nodes["parent"].OwnerSlug != "brain-ui" {
		t.Fatalf("parent owner_slug = %q, want brain-ui", nodes["parent"].OwnerSlug)
	}
	if nodes["child"].OwnerSlug != "brain-ui" {
		t.Fatalf("child owner_slug = %q, want inherited brain-ui", nodes["child"].OwnerSlug)
	}
	if nodes["other"].OwnerSlug != "unowned" {
		t.Fatalf("other owner_slug = %q, want unowned", nodes["other"].OwnerSlug)
	}
	ownerCounts := map[string]int{}
	for _, owner := range got.Owners {
		ownerCounts[owner.Slug] = owner.TaskCount
	}
	if ownerCounts["brain-ui"] != 2 {
		t.Fatalf("brain-ui task count = %d, want 2", ownerCounts["brain-ui"])
	}
	if ownerCounts["unowned"] != 1 {
		t.Fatalf("unowned task count = %d, want 1", ownerCounts["unowned"])
	}
}

func TestBrainGraphInheritsOwnerFromParentHiddenByVisibilityFilters(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainGraphOwner(t, db, root, "brain-ui")
	insertBrainGraphTask(t, db, "query-parent", "Query Parent", "backlog", nil)
	insertBrainGraphTask(t, db, "query-child", "Needle Child", "backlog", strPtr("query-parent"))
	insertBrainGraphTask(t, db, "status-parent", "Status Parent", "done", nil)
	insertBrainGraphTask(t, db, "status-child", "Status Child", "backlog", strPtr("status-parent"))
	insertBrainGraphTask(t, db, "done-parent", "Done Parent", "done", nil)
	insertBrainGraphTask(t, db, "done-child", "Done Child", "backlog", strPtr("done-parent"))
	for _, slug := range []string{"query-parent", "status-parent", "done-parent"} {
		if err := flowdb.AddTaskTag(db, slug, "owner:brain-ui"); err != nil {
			t.Fatalf("AddTaskTag(%s): %v", slug, err)
		}
	}

	tests := []struct {
		name      string
		filters   BrainGraphFilters
		childSlug string
	}{
		{
			name:      "query",
			filters:   BrainGraphFilters{Query: "needle"},
			childSlug: "query-child",
		},
		{
			name:      "status",
			filters:   BrainGraphFilters{Status: "backlog", IncludeDone: true},
			childSlug: "status-child",
		},
		{
			name:      "include_done",
			filters:   BrainGraphFilters{},
			childSlug: "done-child",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildBrainGraph(db, root, tt.filters, time.Date(2026, 6, 12, 10, 0, 0, 0, time.FixedZone("IST", 19800)))
			if err != nil {
				t.Fatalf("BuildBrainGraph: %v", err)
			}
			nodes := graphNodesByTask(got)
			node, ok := nodes[tt.childSlug]
			if !ok {
				t.Fatalf("child %s missing from graph nodes: %#v", tt.childSlug, got.Nodes)
			}
			if node.OwnerSlug != "brain-ui" {
				t.Fatalf("%s owner_slug = %q, want inherited brain-ui", tt.childSlug, node.OwnerSlug)
			}
		})
	}
}

func TestBrainGraphWarnsForUnknownOwnerTagsEvenWhenValidOwnerSelected(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainGraphOwner(t, db, root, "brain-ui")
	insertBrainGraphTask(t, db, "mixed-owner", "Mixed Owner", "backlog", nil)
	for _, tag := range []string{"owner:brain-ui", "owner:missing-b", "owner:missing-a"} {
		if err := flowdb.AddTaskTag(db, "mixed-owner", tag); err != nil {
			t.Fatalf("AddTaskTag(%s): %v", tag, err)
		}
	}

	got, err := BuildBrainGraph(db, root, BrainGraphFilters{}, time.Date(2026, 6, 12, 10, 0, 0, 0, time.FixedZone("IST", 19800)))
	if err != nil {
		t.Fatalf("BuildBrainGraph: %v", err)
	}

	nodes := graphNodesByTask(got)
	if nodes["mixed-owner"].OwnerSlug != "brain-ui" {
		t.Fatalf("mixed-owner owner_slug = %q, want brain-ui", nodes["mixed-owner"].OwnerSlug)
	}
	if !graphHasWarning(got, "unknown_owner", "task:mixed-owner", "owner:missing-a") {
		t.Fatalf("missing warning for owner:missing-a: %#v", got.Warnings)
	}
	if !graphHasWarning(got, "unknown_owner", "task:mixed-owner", "owner:missing-b") {
		t.Fatalf("missing warning for owner:missing-b: %#v", got.Warnings)
	}
}

func TestBrainGraphAddsParentAndDependencyEdges(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainGraphTask(t, db, "parent", "Parent", "backlog", nil)
	insertBrainGraphTask(t, db, "child", "Child", "backlog", strPtr("parent"))
	insertBrainGraphTask(t, db, "dep", "Dependency", "done", nil)
	if _, err := db.Exec(
		`INSERT INTO task_dependencies (child_slug, parent_slug, created_at)
		 VALUES ('child', 'dep', ?)`,
		"2026-06-12T10:00:00+05:30",
	); err != nil {
		t.Fatal(err)
	}

	withoutDone, err := BuildBrainGraph(db, root, BrainGraphFilters{}, time.Date(2026, 6, 12, 10, 0, 0, 0, time.FixedZone("IST", 19800)))
	if err != nil {
		t.Fatalf("BuildBrainGraph without done: %v", err)
	}
	if graphHasEdge(withoutDone, "depends_on", "task:dep", "task:child") {
		t.Fatalf("depends_on edge should be hidden when done dependency node is excluded: %#v", withoutDone.Edges)
	}

	got, err := BuildBrainGraph(db, root, BrainGraphFilters{IncludeDone: true}, time.Date(2026, 6, 12, 10, 0, 0, 0, time.FixedZone("IST", 19800)))
	if err != nil {
		t.Fatalf("BuildBrainGraph: %v", err)
	}

	if !graphHasEdge(got, "parent", "task:parent", "task:child") {
		t.Fatalf("missing parent edge task:parent -> task:child: %#v", got.Edges)
	}
	if !graphHasEdge(got, "depends_on", "task:dep", "task:child") {
		t.Fatalf("missing depends_on edge task:dep -> task:child: %#v", got.Edges)
	}
}

func assertBrainGraphEmptyResponse(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
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

func strPtr(s string) *string {
	return &s
}

func graphNodesByTask(view BrainGraphView) map[string]BrainGraphNode {
	out := map[string]BrainGraphNode{}
	for _, node := range view.Nodes {
		if node.TaskSlug != "" {
			out[node.TaskSlug] = node
		}
	}
	return out
}

func graphHasEdge(view BrainGraphView, edgeType, source, target string) bool {
	for _, edge := range view.Edges {
		if edge.Type == edgeType && edge.Source == source && edge.Target == target {
			return true
		}
	}
	return false
}

func graphHasWarning(view BrainGraphView, code, nodeID, messagePart string) bool {
	for _, warning := range view.Warnings {
		if warning.Code == code && warning.NodeID == nodeID && strings.Contains(warning.Message, messagePart) {
			return true
		}
	}
	return false
}

func insertBrainGraphOwner(t *testing.T, db *sql.DB, root, slug string) {
	t.Helper()
	now := "2026-06-12T10:00:00+05:30"
	if _, err := db.Exec(
		`INSERT INTO owners (slug, name, work_dir, status, every, harness, created_at, updated_at)
		 VALUES (?, ?, ?, 'active', '1h', 'claude', ?, ?)`,
		slug, slug, root, now, now,
	); err != nil {
		t.Fatal(err)
	}
}

func insertBrainGraphTask(t *testing.T, db *sql.DB, slug, name, status string, parentSlug *string) {
	t.Helper()
	now := "2026-06-12T10:00:00+05:30"
	if _, err := db.Exec(
		`INSERT INTO tasks (slug, name, status, kind, parent_slug, priority, work_dir, created_at, updated_at)
		 VALUES (?, ?, ?, 'regular', ?, 'medium', ?, ?, ?)`,
		slug, name, status, parentSlug, t.TempDir(), now, now,
	); err != nil {
		t.Fatal(err)
	}
}
