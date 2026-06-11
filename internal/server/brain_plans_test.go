package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"flow/internal/brain"
	"flow/internal/flowdb"
)

func TestBrainPlanLifecycleAPI(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainProject(t, db, "brain", "Brain feature", root)
	insertBrainTask(t, db, "support-task", "Support task", "done", "medium", root, "brain")

	srv := New(Config{DB: db, FlowRoot: root, Version: "test"}).Handler()

	createBody := brain.Plan{
		Title:        "Brain plan draft",
		Query:        "Split a broad ask into a durable task family",
		Summary:      "Draft the first Brain action bundle",
		Source:       "ask-flow",
		Project:      "brain",
		WorkDir:      root,
		BranchPolicy: "feature/flow-brain-orchestrator",
		SourceRefs: []brain.SourceRef{{
			Kind:    "search",
			Title:   "Ask Flow result",
			Snippet: "brain plans should be approval-first",
		}},
		Items: []brain.Item{
			{
				ID:             "discover",
				Kind:           brain.ItemKindTask,
				Title:          "Discover dependencies",
				Provider:       "codex",
				Model:          "gpt-5.4-mini",
				Tier:           "mini",
				PermissionMode: "auto",
				Risk:           "low",
				Task: &brain.TaskSpec{
					Name:               "Discover dependencies",
					Tags:               []string{"brain", "planning"},
					AcceptanceCriteria: []string{"Map the existing Flow plan APIs", "Capture citations from Flow search"},
				},
			},
			{
				ID:             "implement",
				Kind:           brain.ItemKindTask,
				Title:          "Implement the plan API",
				Provider:       "codex",
				Model:          "gpt-5.4-mini",
				Tier:           "mini",
				PermissionMode: "auto",
				Risk:           "medium",
				Task: &brain.TaskSpec{
					Name:               "Implement the plan API",
					DependsOn:          []string{"discover", "support-task"},
					SubtaskOf:          "support-task",
					Tags:               []string{"brain"},
					AcceptanceCriteria: []string{"Persist drafts", "Approve and execute idempotently"},
				},
			},
			{
				ID:             "launch",
				Kind:           brain.ItemKindLaunchWorker,
				Title:          "Launch worker after approval",
				Provider:       "codex",
				Model:          "gpt-5.4-mini",
				Tier:           "mini",
				PermissionMode: "auto",
				Risk:           "high",
				SourceRefs: []brain.SourceRef{{
					Kind:  "task",
					Slug:  "discover",
					Title: "Discover dependencies",
				}},
			},
		},
	}

	created := mustPostBrainPlan(t, srv, "/api/brain/plans", createBody, http.StatusCreated)
	if created.ID == "" {
		t.Fatal("created plan missing id")
	}
	if created.Status != brain.StatusDraft {
		t.Fatalf("created status = %q, want draft", created.Status)
	}
	if len(created.Items) != 3 {
		t.Fatalf("created items = %d, want 3", len(created.Items))
	}
	if created.Items[0].Status != brain.StatusDraft {
		t.Fatalf("item status = %q, want draft", created.Items[0].Status)
	}

	approved := mustPostBrainPlanAction(t, srv, created.ID, "approve", http.StatusOK)
	if approved.Status != brain.StatusApproved {
		t.Fatalf("approved status = %q", approved.Status)
	}

	executed := mustPostBrainPlanAction(t, srv, created.ID, "execute", http.StatusOK)
	if executed.Status != brain.StatusCompleted {
		t.Fatalf("executed status = %q, want completed", executed.Status)
	}
	if len(executed.Items) != 3 {
		t.Fatalf("executed items = %d, want 3", len(executed.Items))
	}
	if executed.Items[0].Status != brain.StatusCompleted || executed.Items[1].Status != brain.StatusCompleted {
		t.Fatalf("task items not completed: %+v", executed.Items)
	}
	if executed.Items[2].Status != brain.StatusDeferred {
		t.Fatalf("follow-up item status = %q, want deferred", executed.Items[2].Status)
	}
	if executed.Items[0].TaskSlug == "" || executed.Items[1].TaskSlug == "" {
		t.Fatalf("task slugs missing after execution: %+v", executed.Items)
	}

	discoverTask, err := flowdb.GetTask(db, executed.Items[0].TaskSlug)
	if err != nil {
		t.Fatalf("GetTask(discover): %v", err)
	}
	if !discoverTask.ProjectSlug.Valid || discoverTask.ProjectSlug.String != "brain" {
		t.Fatalf("discover project slug = %+v", discoverTask.ProjectSlug)
	}
	if discoverTask.SessionProvider != "codex" {
		t.Fatalf("discover provider = %q", discoverTask.SessionProvider)
	}
	if discoverTask.PermissionMode != "auto" {
		t.Fatalf("discover permission mode = %q", discoverTask.PermissionMode)
	}
	if discoverTask.Model.String != "gpt-5.4-mini" {
		t.Fatalf("discover model = %q", discoverTask.Model.String)
	}
	tags, err := flowdb.GetTaskTags(db, executed.Items[0].TaskSlug)
	if err != nil {
		t.Fatalf("GetTaskTags: %v", err)
	}
	if len(tags) != 2 || tags[0] != "brain" || tags[1] != "planning" {
		t.Fatalf("discover tags = %#v", tags)
	}

	implementTask, err := flowdb.GetTask(db, executed.Items[1].TaskSlug)
	if err != nil {
		t.Fatalf("GetTask(implement): %v", err)
	}
	if !implementTask.ParentSlug.Valid || implementTask.ParentSlug.String != "support-task" {
		t.Fatalf("implement parent = %+v", implementTask.ParentSlug)
	}
	deps, err := flowdb.TaskStartBlockerFor(db, implementTask)
	if err != nil {
		t.Fatalf("TaskStartBlockerFor: %v", err)
	}
	if deps == nil {
		t.Fatal("implement task should be blocked by its dependencies")
	}

	briefPath := filepath.Join(root, "tasks", executed.Items[0].TaskSlug, "brief.md")
	briefBody, err := os.ReadFile(briefPath)
	if err != nil {
		t.Fatalf("brief read: %v", err)
	}
	for _, want := range []string{"Branch policy", "Acceptance criteria", "feature/flow-brain-orchestrator"} {
		if !strings.Contains(string(briefBody), want) {
			t.Fatalf("brief missing %q: %s", want, string(briefBody))
		}
	}

	reexecuted := mustPostBrainPlanAction(t, srv, created.ID, "execute", http.StatusOK)
	if reexecuted.Status != brain.StatusCompleted {
		t.Fatalf("reexecuted status = %q", reexecuted.Status)
	}
	tasks, err := flowdb.ListTasks(db, flowdb.TaskFilter{IncludeArchived: true, IncludeDeleted: true})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	var createdCount int
	for _, task := range tasks {
		if task.Slug == executed.Items[0].TaskSlug || task.Slug == executed.Items[1].TaskSlug {
			createdCount++
		}
	}
	if createdCount != 2 {
		t.Fatalf("created task count = %d, want 2", createdCount)
	}

	var listed []brain.Plan
	mustGetJSON(t, srv, "/api/brain/plans?status="+string(brain.StatusCompleted), &listed, http.StatusOK)
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed plans = %+v", listed)
	}
}

func TestBrainPlanRejectCancelAndInvalidDependency(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainProject(t, db, "brain", "Brain feature", root)

	srv := New(Config{DB: db, FlowRoot: root, Version: "test"}).Handler()

	rejectPlan := mustPostBrainPlan(t, srv, "/api/brain/plans", brain.Plan{
		Title:  "Reject me",
		Query:  "This plan should be rejected",
		Source: "ask-flow",
		Items:  []brain.Item{{ID: "t1", Kind: brain.ItemKindTask, Title: "Task one", Provider: "codex", Risk: "low", Task: &brain.TaskSpec{Name: "Task one"}}},
	}, http.StatusCreated)
	rejected := mustPostBrainPlanAction(t, srv, rejectPlan.ID, "reject", http.StatusOK)
	if rejected.Status != brain.StatusRejected {
		t.Fatalf("rejected status = %q", rejected.Status)
	}
	mustPostBrainPlanActionStatus(t, srv, rejectPlan.ID, "execute", http.StatusConflict)

	cancelPlan := mustPostBrainPlan(t, srv, "/api/brain/plans", brain.Plan{
		Title:  "Cancel me",
		Query:  "This plan should be cancelled",
		Source: "ask-flow",
		Items:  []brain.Item{{ID: "t2", Kind: brain.ItemKindTask, Title: "Task two", Provider: "codex", Risk: "low", Task: &brain.TaskSpec{Name: "Task two"}}},
	}, http.StatusCreated)
	cancelled := mustPostBrainPlanAction(t, srv, cancelPlan.ID, "cancel", http.StatusOK)
	if cancelled.Status != brain.StatusCancelled {
		t.Fatalf("cancelled status = %q", cancelled.Status)
	}
	mustPostBrainPlanActionStatus(t, srv, cancelPlan.ID, "approve", http.StatusConflict)

	invalid := mustPostBrainPlan(t, srv, "/api/brain/plans", brain.Plan{
		Title:  "Invalid dependency",
		Query:  "Depends on something missing",
		Source: "ask-flow",
		Items: []brain.Item{{
			ID:             "task-a",
			Kind:           brain.ItemKindTask,
			Title:          "Task A",
			Provider:       "codex",
			Risk:           "medium",
			PermissionMode: "auto",
			Task: &brain.TaskSpec{
				Name:      "Task A",
				DependsOn: []string{"missing-task"},
			},
		}},
	}, http.StatusCreated)
	mustPostBrainPlanAction(t, srv, invalid.ID, "approve", http.StatusOK)
	blocked := mustPostBrainPlanAction(t, srv, invalid.ID, "execute", http.StatusConflict)
	if blocked.Status != brain.StatusBlocked {
		t.Fatalf("blocked status = %q, want blocked", blocked.Status)
	}
	if blocked.Error == "" {
		t.Fatal("blocked plan should carry an error")
	}
	tasks, err := flowdb.ListTasks(db, flowdb.TaskFilter{IncludeArchived: true, IncludeDeleted: true})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("invalid dependency should not create tasks, got %d rows", len(tasks))
	}
}

func mustPostBrainPlan(t *testing.T, handler http.Handler, path string, plan brain.Plan, wantStatus int) brain.Plan {
	t.Helper()
	body, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)))
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got brain.Plan
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return got
}

func mustPostBrainPlanAction(t *testing.T, handler http.Handler, id, action string, wantStatus int) brain.Plan {
	t.Helper()
	return mustPostBrainPlanActionStatus(t, handler, id, action, wantStatus)
}

func mustPostBrainPlanActionStatus(t *testing.T, handler http.Handler, id, action string, wantStatus int) brain.Plan {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/brain/plans/%s/%s", id, action), nil))
	if rec.Code != wantStatus {
		t.Fatalf("%s status = %d, body = %s", action, rec.Code, rec.Body.String())
	}
	var got brain.Plan
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal %s response: %v", action, err)
	}
	return got
}

func mustGetJSON(t *testing.T, handler http.Handler, path string, out any, wantStatus int) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != wantStatus {
		t.Fatalf("GET %s status = %d, body = %s", path, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("unmarshal GET %s response: %v", path, err)
	}
}

func insertBrainProject(t *testing.T, db *sql.DB, slug, name, wd string) {
	t.Helper()
	now := "2026-06-11T10:00:00Z"
	if _, err := db.Exec(`INSERT INTO projects (slug, name, status, priority, work_dir, created_at, updated_at) VALUES (?, ?, 'active', 'high', ?, ?, ?)`, slug, name, wd, now, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
}

func insertBrainTask(t *testing.T, db *sql.DB, slug, name, status, priority, wd, project string) {
	t.Helper()
	now := "2026-06-11T10:00:00Z"
	if _, err := db.Exec(
		`INSERT INTO tasks (slug, name, project_slug, status, kind, priority, work_dir, session_provider, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'regular', ?, ?, 'codex', ?, ?)`,
		slug, name, project, status, priority, wd, now, now,
	); err != nil {
		t.Fatalf("insert task: %v", err)
	}
}
