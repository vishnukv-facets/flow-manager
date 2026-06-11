package flowdb

import (
	"path/filepath"
	"testing"

	"flow/internal/brain"
)

func TestBrainPlanCRUD(t *testing.T) {
	db := openTempDB(t)
	insertProject(t, db, "flow", "Flow project", t.TempDir(), "high")

	plan := &brain.Plan{
		ID:           "plan-1",
		Status:       brain.StatusDraft,
		Title:        "Brain plan",
		Query:        "Split the work into two tasks",
		Summary:      "Draft a task family",
		Source:       "ask-flow",
		Project:      "flow",
		WorkDir:      "/tmp/flow",
		BranchPolicy: "feature/flow-brain-orchestrator",
		SourceRefs: []brain.SourceRef{{
			Kind:  "search",
			Title: "Flow search hit",
		}},
		Items: []brain.Item{{
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
				AcceptanceCriteria: []string{"Map existing task APIs", "Write the plan bundle"},
			},
		}},
	}
	plan.CreatedAt = NowISO()
	plan.UpdatedAt = plan.CreatedAt

	if err := CreateBrainPlan(db, plan); err != nil {
		t.Fatalf("CreateBrainPlan: %v", err)
	}

	got, err := GetBrainPlan(db, plan.ID)
	if err != nil {
		t.Fatalf("GetBrainPlan: %v", err)
	}
	if got.ID != plan.ID || got.Status != brain.StatusDraft || got.Title != plan.Title {
		t.Fatalf("unexpected plan: %+v", got)
	}
	if len(got.Items) != 1 || got.Items[0].ID != "discover" {
		t.Fatalf("items = %+v", got.Items)
	}

	list, err := ListBrainPlans(db, BrainPlanFilter{})
	if err != nil {
		t.Fatalf("ListBrainPlans: %v", err)
	}
	if len(list) != 1 || list[0].ID != plan.ID {
		t.Fatalf("list = %+v", list)
	}

	now := NowISO()
	got.Status = brain.StatusApproved
	got.ApprovedAt = &now
	got.UpdatedAt = now
	if err := SaveBrainPlan(db, got); err != nil {
		t.Fatalf("SaveBrainPlan: %v", err)
	}

	updated, err := GetBrainPlan(db, plan.ID)
	if err != nil {
		t.Fatalf("GetBrainPlan updated: %v", err)
	}
	if updated.Status != brain.StatusApproved || updated.ApprovedAt == nil || *updated.ApprovedAt != now {
		t.Fatalf("updated plan = %+v", updated)
	}
}

func TestBrainPlanSchemaExists(t *testing.T) {
	db := openTempDB(t)

	for _, name := range []string{"brain_plans", "idx_brain_plans_status_updated"} {
		var got string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type IN ('table','index') AND name = ?`, name).Scan(&got); err != nil {
			t.Fatalf("sqlite_master lookup for %s: %v", name, err)
		}
	}
}

func TestBrainPlanReadWriteSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flow.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	plan := &brain.Plan{
		ID:           "plan-reopen",
		Status:       brain.StatusDraft,
		Title:        "Reopen plan",
		Query:        "Keep persisted JSON intact",
		Source:       "ask-flow",
		BranchPolicy: "feature/flow-brain-orchestrator",
		CreatedAt:    NowISO(),
		UpdatedAt:    NowISO(),
	}
	if err := CreateBrainPlan(db, plan); err != nil {
		db.Close()
		t.Fatalf("CreateBrainPlan: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	db, err = OpenDB(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	got, err := GetBrainPlan(db, "plan-reopen")
	if err != nil {
		t.Fatalf("GetBrainPlan after reopen: %v", err)
	}
	if got.Title != "Reopen plan" || got.Status != brain.StatusDraft {
		t.Fatalf("got %+v", got)
	}
}

func TestBrainPlanFilterByStatus(t *testing.T) {
	db := openTempDB(t)

	now := NowISO()
	plans := []*brain.Plan{
		{ID: "draft", Status: brain.StatusDraft, Title: "Draft", CreatedAt: now, UpdatedAt: now},
		{ID: "approved", Status: brain.StatusApproved, Title: "Approved", CreatedAt: now, UpdatedAt: now},
	}
	for _, plan := range plans {
		if err := CreateBrainPlan(db, plan); err != nil {
			t.Fatalf("CreateBrainPlan %s: %v", plan.ID, err)
		}
	}

	got, err := ListBrainPlans(db, BrainPlanFilter{Status: string(brain.StatusApproved)})
	if err != nil {
		t.Fatalf("ListBrainPlans filter: %v", err)
	}
	if len(got) != 1 || got[0].ID != "approved" {
		t.Fatalf("filtered plans = %+v", got)
	}
}
