package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flow/internal/brain"
	"flow/internal/flowdb"
)

func TestBrainSchedulerLaunchesFirstStartableWorker(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainProject(t, db, "brain", "Brain feature", root)
	insertBrainTask(t, db, "done-parent", "Done parent", "done", "medium", root, "brain")

	srv := New(Config{DB: db, FlowRoot: root, Version: "test", CommandPath: "/bin/false"}).Handler()
	plan := createExecutedBrainSchedulerPlan(t, srv, "brain", root, []brain.Item{
		schedulerTaskItem("ready", "Ready worker", nil, "low"),
		schedulerTaskItem("blocked", "Blocked worker", []string{"ready"}, "medium"),
	})

	var launched []brainWorkerLaunchRequest
	restore := stubBrainWorkerLauncher(func(s *Server, req brainWorkerLaunchRequest) (brainWorkerLaunchResult, error) {
		launched = append(launched, req)
		now := flowdb.NowISO()
		if err := flowdb.UpsertBrainRun(s.cfg.DB, &flowdb.BrainRun{
			RunID:          "run-ready-1",
			FamilySlug:     "ready-worker",
			TaskSlug:       req.TaskSlug,
			PlanID:         sql.NullString{String: req.PlanID, Valid: true},
			Role:           "worker",
			Provider:       req.Provider,
			RequestedModel: sql.NullString{String: req.Model, Valid: req.Model != ""},
			ResolvedModel:  sql.NullString{String: req.Model, Valid: req.Model != ""},
			PermissionMode: req.PermissionMode,
			Status:         "running",
			InputSummary:   sql.NullString{String: "brain scheduler launch", Valid: true},
			StartedAt:      sql.NullString{String: now, Valid: true},
			CreatedAt:      now,
			UpdatedAt:      now,
		}); err != nil {
			t.Fatalf("UpsertBrainRun: %v", err)
		}
		if _, err := s.cfg.DB.Exec(`UPDATE tasks SET auto_run_status='running', auto_run_pid=4242, auto_run_started=?, updated_at=? WHERE slug=?`, now, now, req.TaskSlug); err != nil {
			t.Fatalf("mark auto run running: %v", err)
		}
		return brainWorkerLaunchResult{RunID: "run-ready-1", Output: "launched"}, nil
	})
	defer restore()

	view := postBrainSchedule(t, srv, plan.ID, BrainScheduleRequest{Launch: true}, http.StatusOK)
	if len(launched) != 1 {
		t.Fatalf("launch count = %d, want 1", len(launched))
	}
	if launched[0].TaskSlug != "ready-worker" {
		t.Fatalf("launched task = %q, want ready-worker", launched[0].TaskSlug)
	}
	if launched[0].PlanID != plan.ID || launched[0].ItemID != "ready" {
		t.Fatalf("launch attribution = %+v, want plan/item", launched[0])
	}
	if launched[0].Provider != "codex" || launched[0].Model != "gpt-5.4-mini" || launched[0].Tier != "small" || launched[0].PermissionMode != "auto" {
		t.Fatalf("launch policy = %+v", launched[0])
	}
	if launched[0].TargetBranch != "feature/flow-brain-orchestrator" {
		t.Fatalf("target branch = %q", launched[0].TargetBranch)
	}
	if len(view.Launches) != 1 || view.Launches[0].RunID != "run-ready-1" {
		t.Fatalf("launches = %+v", view.Launches)
	}
	ready := schedulerItemByID(t, view, "ready")
	if ready.State != "running" || ready.LatestRun == nil || ready.LatestRun.RunID != "run-ready-1" {
		t.Fatalf("ready item = %+v", ready)
	}
	blocked := schedulerItemByID(t, view, "blocked")
	if blocked.State != "blocked" || len(blocked.BlockedBy) != 1 || blocked.BlockedBy[0].Slug != "ready-worker" {
		t.Fatalf("blocked item = %+v", blocked)
	}
}

func TestBrainWorkerLauncherPassesSchedulerMetadataToFlowDo(t *testing.T) {
	root, db := testRootDB(t)
	now := flowdb.NowISO()
	insertBrainProject(t, db, "brain", "Brain feature", root)
	insertBrainTask(t, db, "ready-worker", "Ready worker", "in-progress", "medium", root, "brain")
	if err := flowdb.UpsertBrainRun(db, &flowdb.BrainRun{
		RunID:          "run-ready-production",
		FamilySlug:     "ready-worker",
		TaskSlug:       "ready-worker",
		Role:           "worker",
		Provider:       "codex",
		PermissionMode: "auto",
		Status:         "running",
		CreatedAt:      now,
		UpdatedAt:      now,
	}); err != nil {
		t.Fatalf("seed brain run: %v", err)
	}

	argsPath := filepath.Join(t.TempDir(), "argv.txt")
	fakeFlow := filepath.Join(t.TempDir(), "flow")
	if err := os.WriteFile(fakeFlow, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$FLOW_ARGS_PATH\"\n"), 0o755); err != nil {
		t.Fatalf("write fake flow: %v", err)
	}
	t.Setenv("FLOW_ARGS_PATH", argsPath)
	srv := New(Config{DB: db, FlowRoot: root, Version: "test", CommandPath: fakeFlow})

	result, err := brainWorkerLauncher(srv, brainWorkerLaunchRequest{
		PlanID:         "plan-123",
		ItemID:         "ready",
		TaskSlug:       "ready-worker",
		Provider:       "codex",
		Model:          "gpt-5.4-mini",
		Tier:           "small",
		PermissionMode: "auto",
		TargetBranch:   "feature/flow-brain-orchestrator",
		Force:          true,
		InitiatedBy:    "brain-scheduler",
	})
	if err != nil {
		t.Fatalf("brainWorkerLauncher: %v", err)
	}
	if result.RunID != "run-ready-production" {
		t.Fatalf("run id = %q, want run-ready-production", result.RunID)
	}
	rawArgs, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read fake flow args: %v", err)
	}
	gotArgs := strings.Split(strings.TrimSpace(string(rawArgs)), "\n")
	wantArgs := []string{
		"do", "ready-worker", "--auto",
		"--agent", "codex",
		"--brain-plan", "plan-123",
		"--brain-item", "ready",
		"--brain-target-branch", "feature/flow-brain-orchestrator",
		"--brain-initiated-by", "brain-scheduler",
		"--force",
	}
	if strings.Join(gotArgs, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("flow args = %#v, want %#v", gotArgs, wantArgs)
	}
	run, err := flowdb.GetBrainRun(db, "run-ready-production")
	if err != nil {
		t.Fatalf("reload brain run: %v", err)
	}
	if !run.PlanID.Valid || run.PlanID.String != "plan-123" {
		t.Fatalf("run plan id = %+v, want plan-123", run.PlanID)
	}
	if !run.RequestedTier.Valid || run.RequestedTier.String != "small" {
		t.Fatalf("requested tier = %+v, want small", run.RequestedTier)
	}
	var evidence map[string]any
	if err := json.Unmarshal([]byte(run.EvidenceJSON.String), &evidence); err != nil {
		t.Fatalf("unmarshal evidence: %v", err)
	}
	if evidence["plan_item_id"] != "ready" || evidence["target_branch"] != "feature/flow-brain-orchestrator" || evidence["initiated_by"] != "brain-scheduler" {
		t.Fatalf("evidence = %+v", evidence)
	}
}

func TestBrainSchedulerUnblocksDependentsWhenRealDependenciesFinish(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainProject(t, db, "brain", "Brain feature", root)

	srv := New(Config{DB: db, FlowRoot: root, Version: "test"}).Handler()
	plan := createExecutedBrainSchedulerPlan(t, srv, "brain", root, []brain.Item{
		schedulerTaskItem("setup", "Setup worker", nil, "low"),
		schedulerTaskItem("deploy", "Deploy worker", []string{"setup"}, "low"),
	})

	view := getBrainSchedule(t, srv, plan.ID, http.StatusOK)
	deploy := schedulerItemByID(t, view, "deploy")
	if deploy.State != "blocked" || len(deploy.BlockedBy) != 1 || deploy.BlockedBy[0].Slug != "setup-worker" {
		t.Fatalf("deploy before setup done = %+v", deploy)
	}

	if _, err := db.Exec(`UPDATE tasks SET status='done', updated_at=? WHERE slug='setup-worker'`, flowdb.NowISO()); err != nil {
		t.Fatalf("mark setup done: %v", err)
	}
	view = getBrainSchedule(t, srv, plan.ID, http.StatusOK)
	deploy = schedulerItemByID(t, view, "deploy")
	if deploy.State != "ready" || len(deploy.BlockedBy) != 0 {
		t.Fatalf("deploy after setup done = %+v", deploy)
	}
}

func TestBrainSchedulerPreventsDuplicateLaunchAndSurfacesDeadWorker(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainProject(t, db, "brain", "Brain feature", root)

	srv := New(Config{DB: db, FlowRoot: root, Version: "test"}).Handler()
	plan := createExecutedBrainSchedulerPlan(t, srv, "brain", root, []brain.Item{
		schedulerTaskItem("ready", "Ready worker", nil, "low"),
	})

	now := flowdb.NowISO()
	if _, err := db.Exec(`UPDATE tasks SET auto_run_status='running', auto_run_pid=999, auto_run_started=?, updated_at=? WHERE slug='ready-worker'`, now, now); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	called := false
	restore := stubBrainWorkerLauncher(func(s *Server, req brainWorkerLaunchRequest) (brainWorkerLaunchResult, error) {
		called = true
		return brainWorkerLaunchResult{}, nil
	})
	defer restore()

	view := postBrainSchedule(t, srv, plan.ID, BrainScheduleRequest{Launch: true}, http.StatusOK)
	if called {
		t.Fatal("launcher should not be called for an already-running worker")
	}
	ready := schedulerItemByID(t, view, "ready")
	if ready.State != "running" {
		t.Fatalf("ready state = %q, want running", ready.State)
	}
	if len(view.Events) == 0 || view.Events[0].Kind != "worker_running" {
		t.Fatalf("events = %+v, want worker_running", view.Events)
	}

	if _, err := db.Exec(`UPDATE tasks SET auto_run_status='dead', auto_run_pid=NULL, auto_run_finished=?, updated_at=? WHERE slug='ready-worker'`, now, now); err != nil {
		t.Fatalf("mark dead: %v", err)
	}
	view = getBrainSchedule(t, srv, plan.ID, http.StatusOK)
	ready = schedulerItemByID(t, view, "ready")
	if ready.State != "dead" {
		t.Fatalf("ready state after death = %q, want dead", ready.State)
	}
	if !hasProposal(view, "retry_worker", "ready-worker") {
		t.Fatalf("proposals = %+v, want retry_worker for ready-worker", view.Proposals)
	}
}

func TestBrainSchedulerKeepsHighRiskLaunchAsProposalUntilApproved(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainProject(t, db, "brain", "Brain feature", root)

	srv := New(Config{DB: db, FlowRoot: root, Version: "test"}).Handler()
	plan := createExecutedBrainSchedulerPlan(t, srv, "brain", root, []brain.Item{
		schedulerTaskItem("risky", "Risky worker", nil, "high"),
	})

	called := false
	restore := stubBrainWorkerLauncher(func(s *Server, req brainWorkerLaunchRequest) (brainWorkerLaunchResult, error) {
		called = true
		return brainWorkerLaunchResult{RunID: "run-risky"}, nil
	})
	defer restore()

	view := postBrainSchedule(t, srv, plan.ID, BrainScheduleRequest{Launch: true}, http.StatusOK)
	if called {
		t.Fatal("launcher should not run high-risk worker without explicit approval")
	}
	risky := schedulerItemByID(t, view, "risky")
	if risky.State != "ready" {
		t.Fatalf("risky state = %q, want ready", risky.State)
	}
	if !hasProposal(view, "approve_high_risk_worker", "risky-worker") {
		t.Fatalf("proposals = %+v, want approve_high_risk_worker", view.Proposals)
	}
}

func TestBrainSchedulerRejectsCancelledPlanLaunch(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainProject(t, db, "brain", "Brain feature", root)

	srv := New(Config{DB: db, FlowRoot: root, Version: "test"}).Handler()
	plan := mustPostBrainPlan(t, srv, "/api/brain/plans", brain.Plan{
		Title:        "Scheduler plan",
		Query:        "Run a task family",
		Summary:      "Scheduler test fixture",
		Source:       "ask-flow",
		Project:      "brain",
		WorkDir:      root,
		BranchPolicy: "- Open the task PR against `feature/flow-brain-orchestrator`.",
		Items: []brain.Item{
			schedulerTaskItem("ready", "Ready worker", nil, "low"),
		},
	}, http.StatusCreated)
	mustPostBrainPlanAction(t, srv, plan.ID, "cancel", http.StatusOK)

	called := false
	restore := stubBrainWorkerLauncher(func(s *Server, req brainWorkerLaunchRequest) (brainWorkerLaunchResult, error) {
		called = true
		return brainWorkerLaunchResult{}, nil
	})
	defer restore()

	view := postBrainSchedule(t, srv, plan.ID, BrainScheduleRequest{Launch: true}, http.StatusConflict)
	if called {
		t.Fatal("launcher should not run cancelled plans")
	}
	if view.Status != brain.StatusCancelled {
		t.Fatalf("schedule status = %q, want cancelled", view.Status)
	}
	if len(view.Launches) != 0 {
		t.Fatalf("launches = %+v, want none", view.Launches)
	}
}

func TestBrainSchedulerFlagsTimedOutWorker(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainProject(t, db, "brain", "Brain feature", root)

	srv := New(Config{DB: db, FlowRoot: root, Version: "test"}).Handler()
	plan := createExecutedBrainSchedulerPlan(t, srv, "brain", root, []brain.Item{
		schedulerTaskItem("ready", "Ready worker", nil, "low"),
	})
	started := time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE tasks SET auto_run_status='running', auto_run_pid=111, auto_run_started=?, updated_at=? WHERE slug='ready-worker'`, started, started); err != nil {
		t.Fatalf("mark old running: %v", err)
	}

	view := getBrainSchedule(t, srv, plan.ID, http.StatusOK)
	ready := schedulerItemByID(t, view, "ready")
	if ready.State != "timeout" {
		t.Fatalf("ready state = %q, want timeout", ready.State)
	}
	if !hasProposal(view, "review_timed_out_worker", "ready-worker") {
		t.Fatalf("proposals = %+v, want timeout review", view.Proposals)
	}
}

func createExecutedBrainSchedulerPlan(t *testing.T, handler http.Handler, project, root string, items []brain.Item) brain.Plan {
	t.Helper()
	plan := mustPostBrainPlan(t, handler, "/api/brain/plans", brain.Plan{
		Title:        "Scheduler plan",
		Query:        "Run a task family",
		Summary:      "Scheduler test fixture",
		Source:       "ask-flow",
		Project:      project,
		WorkDir:      root,
		BranchPolicy: "- Open the task PR against `feature/flow-brain-orchestrator`.",
		Items:        items,
	}, http.StatusCreated)
	mustPostBrainPlanAction(t, handler, plan.ID, "approve", http.StatusOK)
	return mustPostBrainPlanAction(t, handler, plan.ID, "execute", http.StatusOK)
}

func schedulerTaskItem(id, title string, dependsOn []string, risk string) brain.Item {
	slug, err := brain.Slugify(title)
	if err != nil {
		panic(err)
	}
	return brain.Item{
		ID:             id,
		Kind:           brain.ItemKindTask,
		Title:          title,
		Provider:       "codex",
		Model:          "gpt-5.4-mini",
		Tier:           "small",
		PermissionMode: "auto",
		Risk:           risk,
		Task: &brain.TaskSpec{
			Slug:               slug,
			Name:               title,
			DependsOn:          dependsOn,
			Tags:               []string{"brain"},
			AcceptanceCriteria: []string{"Complete " + title},
		},
	}
}

func getBrainSchedule(t *testing.T, handler http.Handler, planID string, wantStatus int) BrainScheduleView {
	t.Helper()
	var view BrainScheduleView
	mustGetJSON(t, handler, "/api/brain/plans/"+planID+"/schedule", &view, wantStatus)
	return view
}

func postBrainSchedule(t *testing.T, handler http.Handler, planID string, req BrainScheduleRequest, wantStatus int) BrainScheduleView {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal schedule request: %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/brain/plans/"+planID+"/schedule", bytes.NewReader(body)))
	if rec.Code != wantStatus {
		t.Fatalf("POST schedule status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var view BrainScheduleView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal schedule response: %v", err)
	}
	return view
}

func schedulerItemByID(t *testing.T, view BrainScheduleView, id string) BrainScheduleItemView {
	t.Helper()
	for _, item := range view.Items {
		if item.ItemID == id {
			return item
		}
	}
	t.Fatalf("item %q not found in %+v", id, view.Items)
	return BrainScheduleItemView{}
}

func hasProposal(view BrainScheduleView, kind, taskSlug string) bool {
	for _, p := range view.Proposals {
		if p.Kind == kind && p.TaskSlug == taskSlug {
			return true
		}
	}
	return false
}

func stubBrainWorkerLauncher(fn brainWorkerLauncherFunc) func() {
	old := brainWorkerLauncher
	brainWorkerLauncher = fn
	return func() { brainWorkerLauncher = old }
}
