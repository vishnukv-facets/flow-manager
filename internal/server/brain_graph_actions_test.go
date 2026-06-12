package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
