package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"flow/internal/flowdb"
)

func TestBrainGraphApproveActionRequiresConfirmAndAudits(t *testing.T) {
	root, db := testRootDB(t)
	s := New(Config{DB: db, FlowRoot: root})
	insertBrainGraphTask(t, db, "ship", "Ship Feature", "backlog", nil)
	if err := flowdb.AddTaskTag(db, "ship", "gh-pr:Facets-cloud/flow-manager#44"); err != nil {
		t.Fatalf("AddTaskTag: %v", err)
	}

	got, rec := postBrainGraphAction(t, s, BrainGraphActionRequest{
		Action: "approve",
		NodeID: "approval:merge:ship",
		Actor:  "operator",
	})

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got.OK || !got.RequiresConfirmation || !strings.Contains(got.Message, "requires confirmation") {
		t.Fatalf("response = %#v, want confirmation-required failure", got)
	}
	if got.Audit == nil || got.Audit.Action != "merge" || got.Audit.TargetID != "ship" || got.Audit.Result != "blocked" {
		t.Fatalf("audit = %#v, want blocked merge audit", got.Audit)
	}
	policy, err := flowdb.GetBrainPolicy(db)
	if err != nil {
		t.Fatalf("GetBrainPolicy: %v", err)
	}
	if policy.IsWhitelisted("merge") {
		t.Fatalf("merge should not be whitelisted without confirm: %#v", policy)
	}
	audits, err := flowdb.ListBrainActionAudit(db, "task", "ship", 10)
	if err != nil {
		t.Fatalf("ListBrainActionAudit: %v", err)
	}
	if len(audits) != 1 || audits[0].Result != "blocked" || audits[0].Policy != flowdb.BrainPolicyModeApprovalRequired {
		t.Fatalf("audits = %#v, want one blocked approval audit", audits)
	}
	view, err := BuildBrainGraph(db, root, BrainGraphFilters{}, time.Date(2026, 6, 12, 10, 0, 0, 0, time.FixedZone("IST", 19800)))
	if err != nil {
		t.Fatalf("BuildBrainGraph: %v", err)
	}
	if _, ok := graphNodeByID(view, "approval:merge:ship"); !ok {
		t.Fatalf("approval gate should remain without confirm: %#v", view.Nodes)
	}
}

func TestBrainGraphApproveActionWithConfirmSwitchesPolicyAndSuppressesGate(t *testing.T) {
	root, db := testRootDB(t)
	s := New(Config{DB: db, FlowRoot: root})
	insertBrainGraphTask(t, db, "ship", "Ship Feature", "backlog", nil)
	if err := flowdb.AddTaskTag(db, "ship", "gh-pr:Facets-cloud/flow-manager#44"); err != nil {
		t.Fatalf("AddTaskTag: %v", err)
	}

	got, rec := postBrainGraphAction(t, s, BrainGraphActionRequest{
		Action:  "approve",
		NodeID:  "approval:merge:ship",
		Confirm: true,
		Actor:   "operator",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !got.OK || got.RequiresConfirmation || got.Policy == nil || !stringSliceContains(got.Policy.RiskyWhitelist, "merge") {
		t.Fatalf("response = %#v, want approved policy response", got)
	}
	if got.Audit == nil || got.Audit.Action != "merge" || got.Audit.TargetID != "ship" || got.Audit.Result != "allowed" || got.Audit.Policy != flowdb.BrainPolicyModeAuto {
		t.Fatalf("audit = %#v, want allowed merge audit", got.Audit)
	}
	policy, err := flowdb.GetBrainPolicy(db)
	if err != nil {
		t.Fatalf("GetBrainPolicy: %v", err)
	}
	if !policy.IsWhitelisted("merge") {
		t.Fatalf("merge should be whitelisted after confirm: %#v", policy)
	}
	audits, err := flowdb.ListBrainActionAudit(db, "task", "ship", 10)
	if err != nil {
		t.Fatalf("ListBrainActionAudit: %v", err)
	}
	if len(audits) != 1 || audits[0].Result != "allowed" {
		t.Fatalf("audits = %#v, want one allowed approval audit", audits)
	}
	view, err := BuildBrainGraph(db, root, BrainGraphFilters{}, time.Date(2026, 6, 12, 10, 0, 0, 0, time.FixedZone("IST", 19800)))
	if err != nil {
		t.Fatalf("BuildBrainGraph: %v", err)
	}
	if _, ok := graphNodeByID(view, "approval:merge:ship"); ok {
		t.Fatalf("approval gate should be suppressed after confirm: %#v", view.Nodes)
	}
}

func TestBrainGraphApproveActionRollsBackPolicyWhenAuditInsertFails(t *testing.T) {
	root, db := testRootDB(t)
	s := New(Config{DB: db, FlowRoot: root})
	insertBrainGraphTask(t, db, "ship", "Ship Feature", "backlog", nil)
	if err := flowdb.AddTaskTag(db, "ship", "gh-pr:Facets-cloud/flow-manager#44"); err != nil {
		t.Fatalf("AddTaskTag: %v", err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_brain_action_audit_insert BEFORE INSERT ON brain_action_audit BEGIN SELECT RAISE(FAIL, 'audit disabled'); END`); err != nil {
		t.Fatalf("create failing audit trigger: %v", err)
	}

	got, rec := postBrainGraphAction(t, s, BrainGraphActionRequest{
		Action:  "approve",
		NodeID:  "approval:merge:ship",
		Confirm: true,
		Actor:   "operator",
	})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got.OK || !strings.Contains(got.Message, "audit disabled") {
		t.Fatalf("response = %#v, want audit failure", got)
	}
	policy, err := flowdb.GetBrainPolicy(db)
	if err != nil {
		t.Fatalf("GetBrainPolicy: %v", err)
	}
	if policy.IsWhitelisted("merge") {
		t.Fatalf("merge policy changed even though audit insert failed: %#v", policy)
	}
}

func TestBrainGraphOpenSessionActionRejectsNonTaskNodes(t *testing.T) {
	root, db := testRootDB(t)
	s := New(Config{DB: db, FlowRoot: root})
	insertBrainGraphTask(t, db, "ship", "Ship Feature", "backlog", nil)
	if err := flowdb.AddTaskTag(db, "ship", "gh-pr:Facets-cloud/flow-manager#44"); err != nil {
		t.Fatalf("AddTaskTag: %v", err)
	}

	got, rec := postBrainGraphAction(t, s, BrainGraphActionRequest{
		Action: "open_session",
		NodeID: "approval:merge:ship",
		Actor:  "operator",
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got.OK || !strings.Contains(got.Message, "task nodes only") {
		t.Fatalf("response = %#v, want task-node validation error", got)
	}
}

func TestBrainGraphOpenSessionActionAuditsSuccessfulBridge(t *testing.T) {
	fakeProviderOnPath(t, "claude")
	root, db := testRootDB(t)
	s := New(Config{DB: db, FlowRoot: root})
	insertBrainGraphTask(t, db, "ship", "Ship Feature", "backlog", nil)

	got, rec := postBrainGraphAction(t, s, BrainGraphActionRequest{
		Action: "open_session",
		NodeID: "task:ship",
		Actor:  "operator",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !got.OK || got.Audit == nil {
		t.Fatalf("response = %#v, want successful audited open", got)
	}
	if got.Audit.Action != "open_session" || got.Audit.TargetID != "ship" || got.Audit.Policy != "operator" || got.Audit.Result != "opened" {
		t.Fatalf("audit = %#v, want opened open_session audit", got.Audit)
	}
	var evidence struct {
		NodeID   string `json:"node_id"`
		TaskSlug string `json:"task_slug"`
		Action   string `json:"action"`
		Bridge   struct {
			OK            bool   `json:"ok"`
			Status        int    `json:"status"`
			Message       string `json:"message"`
			Bridge        bool   `json:"bridge"`
			AgentProvider string `json:"agent_provider"`
		} `json:"bridge"`
	}
	if err := json.Unmarshal(got.Audit.EvidenceJSON, &evidence); err != nil {
		t.Fatalf("decode audit evidence %s: %v", got.Audit.EvidenceJSON, err)
	}
	if evidence.NodeID != "task:ship" || evidence.TaskSlug != "ship" || evidence.Action != "open_session" {
		t.Fatalf("evidence = %#v, want node/task/action", evidence)
	}
	if !evidence.Bridge.OK || evidence.Bridge.Status != http.StatusOK || !evidence.Bridge.Bridge || evidence.Bridge.AgentProvider != "claude" {
		t.Fatalf("bridge evidence = %#v, want successful claude bridge", evidence.Bridge)
	}
	audits, err := flowdb.ListBrainActionAudit(db, "task", "ship", 10)
	if err != nil {
		t.Fatalf("ListBrainActionAudit: %v", err)
	}
	if len(audits) != 1 || audits[0].Action != "open_session" || audits[0].Result != "opened" {
		t.Fatalf("audits = %#v, want one opened audit", audits)
	}
	detail, err := BuildBrainGraphNodeDetail(db, root, "task:ship")
	if err != nil {
		t.Fatalf("BuildBrainGraphNodeDetail: %v", err)
	}
	if len(detail.Audit) != 1 || detail.Audit[0].Action != "open_session" || detail.Audit[0].Result != "opened" {
		t.Fatalf("detail audit = %#v, want opened open_session audit", detail.Audit)
	}
}

func TestBrainGraphSeedAndSendEventActionsReturnUnsupported(t *testing.T) {
	root, db := testRootDB(t)
	s := New(Config{DB: db, FlowRoot: root})
	insertBrainGraphTask(t, db, "ship", "Ship Feature", "backlog", nil)

	for _, action := range []string{"seed", "send_event"} {
		t.Run(action, func(t *testing.T) {
			got, rec := postBrainGraphAction(t, s, BrainGraphActionRequest{
				Action: action,
				NodeID: "task:ship",
				Actor:  "operator",
			})
			if rec.Code != http.StatusNotImplemented {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if got.OK || got.Message != "graph action "+action+" is not supported yet" {
				t.Fatalf("response = %#v, want stable unsupported response", got)
			}
		})
	}
}

func TestBrainGraphRejectsHiddenAutoLaunchActionsForTaskNodes(t *testing.T) {
	root, db := testRootDB(t)
	s := New(Config{DB: db, FlowRoot: root, CommandPath: "/bin/echo"})
	insertBrainGraphTask(t, db, "ship", "Ship Feature", "backlog", nil)

	for _, action := range []string{"retry", "trigger_auto"} {
		t.Run(action, func(t *testing.T) {
			got, rec := postBrainGraphAction(t, s, BrainGraphActionRequest{
				Action: action,
				NodeID: "task:ship",
				Actor:  "operator",
			})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if got.OK || got.Output != "" {
				t.Fatalf("response = %#v, want rejected hidden launch", got)
			}
			audits, err := flowdb.ListBrainActionAudit(db, "task", "ship", 10)
			if err != nil {
				t.Fatalf("ListBrainActionAudit: %v", err)
			}
			if len(audits) != 0 {
				t.Fatalf("audits = %#v, want no hidden-launch audit", audits)
			}
		})
	}
}

func TestBrainGraphRetryActionLaunchesAutoRunForRunTaskSlug(t *testing.T) {
	root, db := testRootDB(t)
	s := New(Config{DB: db, FlowRoot: root, CommandPath: "/bin/echo"})
	insertBrainGraphTask(t, db, "worker", "Worker", "backlog", nil)
	run := &flowdb.BrainRun{
		RunID:          "run-1",
		FamilySlug:     "worker",
		TaskSlug:       "worker",
		Role:           "worker",
		Provider:       "codex",
		PermissionMode: "auto",
		Status:         "error",
		CreatedAt:      "2026-06-12T10:00:00+05:30",
		UpdatedAt:      "2026-06-12T10:00:00+05:30",
	}
	if err := flowdb.UpsertBrainRun(db, run); err != nil {
		t.Fatalf("UpsertBrainRun: %v", err)
	}

	got, rec := postBrainGraphAction(t, s, BrainGraphActionRequest{
		Action: "retry",
		NodeID: "run:run-1",
		Prompt: "focus the rerun",
		Actor:  "operator",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !got.OK || !strings.Contains(got.Output, "do worker --auto --with focus the rerun") {
		t.Fatalf("response = %#v, want flow do --auto output", got)
	}
	if got.Audit == nil || got.Audit.Action != "retry" || got.Audit.TargetID != "worker" || got.Audit.Result != "launched" {
		t.Fatalf("audit = %#v, want launched retry audit", got.Audit)
	}
	audits, err := flowdb.ListBrainActionAudit(db, "task", "worker", 10)
	if err != nil {
		t.Fatalf("ListBrainActionAudit: %v", err)
	}
	if len(audits) != 1 || audits[0].Action != "retry" || audits[0].Result != "launched" {
		t.Fatalf("audits = %#v, want retry audit", audits)
	}
	detail, err := BuildBrainGraphNodeDetail(db, root, "run:run-1")
	if err != nil {
		t.Fatalf("BuildBrainGraphNodeDetail: %v", err)
	}
	if len(detail.Audit) != 1 || detail.Audit[0].Action != "retry" || detail.Audit[0].Result != "launched" {
		t.Fatalf("run detail audit = %#v, want retry audit", detail.Audit)
	}
}

func TestBrainGraphActionsRejectArchivedAndDeletedTargets(t *testing.T) {
	root, db := testRootDB(t)
	s := New(Config{DB: db, FlowRoot: root, CommandPath: "/bin/echo"})
	insertBrainGraphTask(t, db, "archived", "Archived", "backlog", nil)
	insertBrainGraphTask(t, db, "deleted", "Deleted", "backlog", nil)
	insertBrainGraphTask(t, db, "run-deleted", "Run Deleted", "backlog", nil)
	if err := flowdb.AddTaskTag(db, "archived", "gh-pr:Facets-cloud/flow-manager#44"); err != nil {
		t.Fatalf("AddTaskTag: %v", err)
	}
	if _, err := db.Exec(`UPDATE tasks SET archived_at = '2026-06-12T10:00:00+05:30' WHERE slug = 'archived'`); err != nil {
		t.Fatalf("archive task: %v", err)
	}
	if _, err := db.Exec(`UPDATE tasks SET deleted_at = '2026-06-12T10:00:00+05:30' WHERE slug IN ('deleted', 'run-deleted')`); err != nil {
		t.Fatalf("delete task: %v", err)
	}
	run := &flowdb.BrainRun{
		RunID:          "run-deleted-1",
		FamilySlug:     "run-deleted",
		TaskSlug:       "run-deleted",
		Role:           "worker",
		Provider:       "codex",
		PermissionMode: "auto",
		Status:         "error",
		CreatedAt:      "2026-06-12T10:00:00+05:30",
		UpdatedAt:      "2026-06-12T10:00:00+05:30",
	}
	if err := flowdb.UpsertBrainRun(db, run); err != nil {
		t.Fatalf("UpsertBrainRun: %v", err)
	}

	for _, tc := range []BrainGraphActionRequest{
		{Action: "open_session", NodeID: "task:archived", Actor: "operator"},
		{Action: "approve", NodeID: "approval:merge:archived", Confirm: true, Actor: "operator"},
		{Action: "open_session", NodeID: "task:deleted", Actor: "operator"},
		{Action: "retry", NodeID: "run:run-deleted-1", Actor: "operator"},
	} {
		t.Run(tc.NodeID+"/"+tc.Action, func(t *testing.T) {
			got, rec := postBrainGraphAction(t, s, tc)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if got.OK || !strings.Contains(got.Message, "graph node not found") {
				t.Fatalf("response = %#v, want stale node rejection", got)
			}
		})
	}
}

func TestBrainGraphTaskNodesOnlyAdvertiseSupportedActions(t *testing.T) {
	root, db := testRootDB(t)
	insertBrainGraphTask(t, db, "ship", "Ship Feature", "backlog", nil)

	view, err := BuildBrainGraph(db, root, BrainGraphFilters{}, time.Date(2026, 6, 12, 10, 0, 0, 0, time.FixedZone("IST", 19800)))
	if err != nil {
		t.Fatalf("BuildBrainGraph: %v", err)
	}
	node, ok := graphNodeByID(view, "task:ship")
	if !ok {
		t.Fatalf("missing task node: %#v", view.Nodes)
	}
	if strings.Join(node.Actions, ",") != "open_session" {
		t.Fatalf("task actions = %#v, want only open_session", node.Actions)
	}
	actionSpecs := map[string]BrainGraphActionSpec{}
	for _, action := range view.SelectedActions {
		actionSpecs[action.Key] = action
	}
	for _, key := range []string{"send_event", "seed", "pause"} {
		spec := actionSpecs[key]
		if spec.Enabled || spec.DisabledReason == "" {
			t.Fatalf("action spec %s = %#v, want disabled reason", key, spec)
		}
	}
}

func postBrainGraphAction(t *testing.T, s *Server, req BrainGraphActionRequest) (BrainGraphActionResponse, *httptest.ResponseRecorder) {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/api/brain/graph/actions", bytes.NewReader(body))
	s.Handler().ServeHTTP(rec, httpReq)
	var got BrainGraphActionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response status=%d body=%s: %v", rec.Code, rec.Body.String(), err)
	}
	return got, rec
}

func fakeProviderOnPath(t *testing.T, name string) {
	t.Helper()
	dir := t.TempDir()
	exe := name
	content := "#!/bin/sh\nexit 0\n"
	if runtime.GOOS == "windows" {
		exe += ".bat"
		content = "@echo off\r\nexit /b 0\r\n"
	}
	path := filepath.Join(dir, exe)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake provider: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
