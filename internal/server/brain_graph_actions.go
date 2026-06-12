package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"flow/internal/flowdb"

	"github.com/google/uuid"
)

const maxBrainGraphActionBodyBytes = 64 * 1024

type brainGraphActionNode struct {
	ID             string
	Type           string
	TaskSlug       string
	RunID          string
	ApprovalAction string
}

func (s *Server) handleBrainGraphAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req BrainGraphActionRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBrainGraphActionBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	resp, status := s.runBrainGraphAction(req)
	writeJSONStatus(w, resp, status)
}

func (s *Server) runBrainGraphAction(req BrainGraphActionRequest) (BrainGraphActionResponse, int) {
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	req.NodeID = strings.TrimSpace(req.NodeID)
	req.Prompt = strings.TrimSpace(req.Prompt)
	req.Actor = strings.TrimSpace(req.Actor)
	if req.Actor == "" {
		req.Actor = "operator"
	}
	base := BrainGraphActionResponse{
		Action: req.Action,
		NodeID: req.NodeID,
	}
	if req.Action == "" {
		base.Message = "action is required"
		return base, http.StatusBadRequest
	}
	if req.NodeID == "" {
		base.Message = "node_id is required"
		return base, http.StatusBadRequest
	}

	node, err := resolveBrainGraphActionNode(s.cfg.DB, req.NodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			base.Message = "graph node not found: " + req.NodeID
			return base, http.StatusNotFound
		}
		base.Message = err.Error()
		return base, http.StatusInternalServerError
	}

	switch req.Action {
	case "open_session", "resume":
		if node.Type != "task" {
			base.Message = req.Action + " is available for task nodes only"
			return base, http.StatusBadRequest
		}
		resp, status := s.openBrowserTerminalBridge(node.TaskSlug, "")
		base.OK = resp.OK
		base.Message = resp.Message
		base.Output = resp.Output
		base.ActionResponse = &resp
		if resp.OK {
			audit, auditErr := s.insertBrainGraphActionAudit(req.Action, "task", node.TaskSlug, req.Actor, "operator", "opened", brainGraphOpenSessionEvidence(req, node, resp, status), "")
			base.Audit = audit
			if auditErr != nil {
				base.OK = false
				base.Message = auditErr.Error()
				return base, http.StatusInternalServerError
			}
			s.publishUIChange("brain-graph")
		}
		return base, status
	case "retry":
		if node.TaskSlug == "" || !strings.HasSuffix(node.Type, "_run") {
			base.Message = req.Action + " is available for run nodes only"
			return base, http.StatusBadRequest
		}
		return s.runBrainGraphRetryAction(base, req, node)
	case "approve":
		if node.Type != "approval" {
			base.Message = "approve is available for approval nodes only"
			return base, http.StatusBadRequest
		}
		return s.runBrainGraphApproveAction(base, req, node)
	case "pause":
		if !strings.HasSuffix(node.Type, "_run") {
			base.Message = "pause is available for run nodes only"
			return base, http.StatusBadRequest
		}
		base.Message = "pause is disabled: no safe persisted stop primitive is available for Brain Graph runs"
		return base, http.StatusConflict
	case "send_event", "seed":
		if node.Type != "task" {
			base.Message = req.Action + " is available for task nodes only"
			return base, http.StatusBadRequest
		}
		return s.runBrainGraphSessionEventAction(base, req, node)
	default:
		base.Message = "unknown graph action " + req.Action
		return base, http.StatusBadRequest
	}
}

func brainGraphOpenSessionEvidence(req BrainGraphActionRequest, node brainGraphActionNode, resp actionResponse, status int) map[string]any {
	bridge := map[string]any{
		"ok":      resp.OK,
		"status":  status,
		"message": resp.Message,
		"bridge":  resp.Bridge,
	}
	if resp.Output != "" {
		bridge["output"] = resp.Output
	}
	if resp.Agent != nil {
		bridge["agent_provider"] = resp.Agent.Provider
		bridge["agent_status"] = resp.Agent.Status
		bridge["agent_runtime_status"] = resp.Agent.RuntimeStatus
	}
	return map[string]any{
		"node_id":   node.ID,
		"task_slug": node.TaskSlug,
		"action":    req.Action,
		"bridge":    bridge,
	}
}

func resolveBrainGraphActionNode(db *sql.DB, nodeID string) (brainGraphActionNode, error) {
	nodeID = strings.TrimSpace(nodeID)
	switch {
	case strings.HasPrefix(nodeID, "task:"):
		slug := strings.TrimSpace(strings.TrimPrefix(nodeID, "task:"))
		task, err := flowdb.GetTask(db, slug)
		if err != nil {
			return brainGraphActionNode{}, err
		}
		if !brainGraphTaskIsActionable(task) {
			return brainGraphActionNode{}, sql.ErrNoRows
		}
		return brainGraphActionNode{
			ID:       "task:" + task.Slug,
			Type:     "task",
			TaskSlug: task.Slug,
		}, nil
	case strings.HasPrefix(nodeID, "run:auto:"):
		slug := strings.TrimSpace(strings.TrimPrefix(nodeID, "run:auto:"))
		run, err := flowdb.GetBrainRun(db, "legacy:auto-run:"+slug)
		if err != nil {
			return brainGraphActionNode{}, err
		}
		task, err := flowdb.GetTask(db, run.TaskSlug)
		if err != nil {
			return brainGraphActionNode{}, err
		}
		if !brainGraphTaskIsActionable(task) {
			return brainGraphActionNode{}, sql.ErrNoRows
		}
		return brainGraphActionNode{
			ID:       "run:auto:" + task.Slug,
			Type:     brainGraphRunNodeType(run.Role),
			TaskSlug: task.Slug,
			RunID:    run.RunID,
		}, nil
	case strings.HasPrefix(nodeID, "run:"):
		runID := strings.TrimSpace(strings.TrimPrefix(nodeID, "run:"))
		if runID == "" {
			return brainGraphActionNode{}, sql.ErrNoRows
		}
		run, err := flowdb.GetBrainRun(db, runID)
		if err != nil {
			return brainGraphActionNode{}, err
		}
		task, err := flowdb.GetTask(db, run.TaskSlug)
		if err != nil {
			return brainGraphActionNode{}, err
		}
		if !brainGraphTaskIsActionable(task) {
			return brainGraphActionNode{}, sql.ErrNoRows
		}
		return brainGraphActionNode{
			ID:       "run:" + run.RunID,
			Type:     brainGraphRunNodeType(run.Role),
			TaskSlug: task.Slug,
			RunID:    run.RunID,
		}, nil
	case strings.HasPrefix(nodeID, "approval:"):
		return resolveBrainGraphApprovalActionNode(db, strings.TrimPrefix(nodeID, "approval:"), nodeID)
	default:
		return brainGraphActionNode{}, sql.ErrNoRows
	}
}

func brainGraphTaskIsActionable(task *flowdb.Task) bool {
	return task != nil && !task.ArchivedAt.Valid && !task.DeletedAt.Valid
}

func resolveBrainGraphApprovalActionNode(db *sql.DB, rest, nodeID string) (brainGraphActionNode, error) {
	action, taskSlug, ok := strings.Cut(rest, ":")
	action = strings.TrimSpace(action)
	taskSlug = strings.TrimSpace(taskSlug)
	if !ok || action == "" || taskSlug == "" {
		return brainGraphActionNode{}, sql.ErrNoRows
	}
	if !brainGraphKnownRiskyAction(action) {
		return brainGraphActionNode{}, sql.ErrNoRows
	}
	task, err := flowdb.GetTask(db, taskSlug)
	if err != nil {
		return brainGraphActionNode{}, err
	}
	if !brainGraphTaskIsActionable(task) {
		return brainGraphActionNode{}, sql.ErrNoRows
	}
	tags, err := flowdb.GetTaskTags(db, task.Slug)
	if err != nil {
		return brainGraphActionNode{}, err
	}
	if !brainGraphRiskyActionsForTask(tags)[action] {
		return brainGraphActionNode{}, sql.ErrNoRows
	}
	policy, err := flowdb.GetBrainPolicy(db)
	if err != nil {
		return brainGraphActionNode{}, err
	}
	if policy.IsWhitelisted(action) {
		return brainGraphActionNode{}, sql.ErrNoRows
	}
	return brainGraphActionNode{
		ID:             nodeID,
		Type:           "approval",
		TaskSlug:       task.Slug,
		ApprovalAction: action,
	}, nil
}

func (s *Server) runBrainGraphRetryAction(base BrainGraphActionResponse, req BrainGraphActionRequest, node brainGraphActionNode) (BrainGraphActionResponse, int) {
	args := []string{"do", node.TaskSlug, "--auto"}
	if req.Prompt != "" {
		args = append(args, "--with", req.Prompt)
	}
	out, err := s.runFlowCommand(args...)
	base.Output = out
	evidence := map[string]any{
		"node_id":   node.ID,
		"task_slug": node.TaskSlug,
		"command":   append([]string{"flow"}, args...),
	}
	if node.RunID != "" {
		evidence["run_id"] = node.RunID
	}
	if req.Prompt != "" {
		evidence["prompt"] = req.Prompt
	}
	if err != nil {
		audit, auditErr := s.insertBrainGraphActionAudit(req.Action, "task", node.TaskSlug, req.Actor, "operator", "error", evidence, err.Error())
		base.OK = false
		base.Message = err.Error()
		base.Audit = audit
		if auditErr != nil {
			base.Message = base.Message + "; audit failed: " + auditErr.Error()
			return base, http.StatusInternalServerError
		}
		return base, http.StatusInternalServerError
	}
	audit, auditErr := s.insertBrainGraphActionAudit(req.Action, "task", node.TaskSlug, req.Actor, "operator", "launched", evidence, "")
	if auditErr != nil {
		base.OK = false
		base.Message = auditErr.Error()
		return base, http.StatusInternalServerError
	}
	base.OK = true
	base.Message = "launched autonomous run for " + node.TaskSlug
	base.Audit = audit
	s.publishUIChange("brain-graph")
	return base, http.StatusOK
}

func (s *Server) runBrainGraphSessionEventAction(base BrainGraphActionResponse, req BrainGraphActionRequest, node brainGraphActionNode) (BrainGraphActionResponse, int) {
	if req.Prompt == "" {
		base.Message = "prompt is required for " + req.Action
		return base, http.StatusBadRequest
	}
	deliveredPrompt := brainGraphSessionEventPrompt(req.Action, node.TaskSlug, req.Prompt)
	resp, status := s.nudgeSession(node.TaskSlug, deliveredPrompt)
	base.OK = resp.OK
	base.Message = resp.Message
	base.Output = resp.Output
	base.ActionResponse = &resp
	evidence := map[string]any{
		"node_id":   node.ID,
		"task_slug": node.TaskSlug,
		"action":    req.Action,
		"prompt":    req.Prompt,
		"nudge": map[string]any{
			"ok":      resp.OK,
			"status":  status,
			"message": resp.Message,
		},
	}
	result := "sent"
	errorText := ""
	if !resp.OK {
		result = "error"
		errorText = resp.Message
	}
	audit, auditErr := s.insertBrainGraphActionAudit(req.Action, "task", node.TaskSlug, req.Actor, "operator", result, evidence, errorText)
	base.Audit = audit
	if auditErr != nil {
		base.OK = false
		base.Message = auditErr.Error()
		return base, http.StatusInternalServerError
	}
	if resp.OK {
		s.publishUIChange("brain-graph")
	}
	return base, status
}

func brainGraphSessionEventPrompt(action, taskSlug, prompt string) string {
	label := "event"
	if action == "seed" {
		label = "seed input"
	}
	return fmt.Sprintf("Flow Brain Graph %s for %s:\n\n%s", label, taskSlug, prompt)
}

func (s *Server) runBrainGraphApproveAction(base BrainGraphActionResponse, req BrainGraphActionRequest, node brainGraphActionNode) (BrainGraphActionResponse, int) {
	evidence := map[string]any{
		"node_id":   node.ID,
		"task_slug": node.TaskSlug,
		"action":    node.ApprovalAction,
	}
	if !req.Confirm {
		audit, auditErr := s.insertBrainGraphActionAudit(node.ApprovalAction, "task", node.TaskSlug, req.Actor, flowdb.BrainPolicyModeApprovalRequired, "blocked", evidence, "confirmation required")
		base.OK = false
		base.Message = "approval requires confirmation"
		base.RequiresConfirmation = true
		base.Audit = audit
		if auditErr != nil {
			base.Message = base.Message + "; audit failed: " + auditErr.Error()
			return base, http.StatusInternalServerError
		}
		return base, http.StatusConflict
	}
	now := flowdb.NowISO()
	evidence["policy_updated_at"] = now
	audit, auditErr := s.newBrainGraphActionAudit(node.ApprovalAction, "task", node.TaskSlug, req.Actor, flowdb.BrainPolicyModeAuto, "allowed", evidence, "")
	if auditErr != nil {
		base.Message = auditErr.Error()
		return base, http.StatusInternalServerError
	}
	tx, err := s.cfg.DB.Begin()
	if err != nil {
		base.Message = err.Error()
		return base, http.StatusInternalServerError
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := flowdb.SetBrainPolicyModeTx(tx, node.ApprovalAction, flowdb.BrainPolicyModeAuto, now); err != nil {
		base.Message = err.Error()
		return base, http.StatusInternalServerError
	}
	if err := flowdb.InsertBrainActionAuditTx(tx, audit); err != nil {
		base.Message = err.Error()
		return base, http.StatusInternalServerError
	}
	if err := tx.Commit(); err != nil {
		base.Message = err.Error()
		return base, http.StatusInternalServerError
	}
	committed = true
	policy, err := flowdb.GetBrainPolicy(s.cfg.DB)
	if err != nil {
		base.Message = err.Error()
		return base, http.StatusInternalServerError
	}
	policyView := brainGraphPolicyView(policy)
	base.OK = true
	base.Message = "approved " + node.ApprovalAction + " for autonomous Brain actions"
	base.Policy = &policyView
	auditView := brainGraphAuditViews([]*flowdb.BrainActionAudit{audit})[0]
	base.Audit = &auditView
	s.publishUIChange("brain-graph")
	return base, http.StatusOK
}

func (s *Server) insertBrainGraphActionAudit(action, targetType, targetID, actor, policy, result string, evidence map[string]any, errorText string) (*BrainGraphAuditView, error) {
	audit, err := s.newBrainGraphActionAudit(action, targetType, targetID, actor, policy, result, evidence, errorText)
	if err != nil {
		return nil, err
	}
	if err := flowdb.InsertBrainActionAudit(s.cfg.DB, audit); err != nil {
		return nil, err
	}
	view := brainGraphAuditViews([]*flowdb.BrainActionAudit{audit})[0]
	return &view, nil
}

func (s *Server) newBrainGraphActionAudit(action, targetType, targetID, actor, policy, result string, evidence map[string]any, errorText string) (*flowdb.BrainActionAudit, error) {
	raw, err := json.Marshal(evidence)
	if err != nil {
		return nil, fmt.Errorf("encode audit evidence: %w", err)
	}
	audit := &flowdb.BrainActionAudit{
		ID:           "graph-action:" + uuid.NewString(),
		Action:       strings.TrimSpace(action),
		TargetType:   strings.TrimSpace(targetType),
		TargetID:     strings.TrimSpace(targetID),
		Actor:        strings.TrimSpace(actor),
		Policy:       strings.TrimSpace(policy),
		EvidenceJSON: string(raw),
		Result:       strings.TrimSpace(result),
		CreatedAt:    flowdb.NowISO(),
	}
	if audit.Actor == "" {
		audit.Actor = "operator"
	}
	if strings.TrimSpace(errorText) != "" {
		audit.ErrorText = sql.NullString{String: strings.TrimSpace(errorText), Valid: true}
	}
	return audit, nil
}
