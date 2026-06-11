package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"flow/internal/brain"
	"flow/internal/flowdb"
)

const defaultBrainWorkerTimeout = 2 * time.Hour

type BrainScheduleRequest struct {
	Launch          bool `json:"launch,omitempty"`
	Force           bool `json:"force,omitempty"`
	ApproveHighRisk bool `json:"approve_high_risk,omitempty"`
	MaxLaunches     int  `json:"max_launches,omitempty"`
	TimeoutMinutes  int  `json:"timeout_minutes,omitempty"`
}

type BrainScheduleView struct {
	PlanID    string                    `json:"plan_id"`
	Status    brain.Status              `json:"status"`
	Items     []BrainScheduleItemView   `json:"items"`
	Summary   BrainScheduleSummary      `json:"summary"`
	Events    []BrainScheduleEvent      `json:"events,omitempty"`
	Proposals []BrainScheduleProposal   `json:"proposals,omitempty"`
	Launches  []BrainScheduleLaunchView `json:"launches,omitempty"`
}

type BrainScheduleSummary struct {
	Ready    int `json:"ready"`
	Running  int `json:"running"`
	Blocked  int `json:"blocked"`
	Done     int `json:"done"`
	Dead     int `json:"dead"`
	Timeout  int `json:"timeout"`
	Missing  int `json:"missing"`
	Deferred int `json:"deferred"`
}

type BrainScheduleItemView struct {
	ItemID         string        `json:"item_id"`
	Kind           string        `json:"kind"`
	Title          string        `json:"title"`
	TaskSlug       string        `json:"task_slug,omitempty"`
	TaskName       string        `json:"task_name,omitempty"`
	TaskStatus     string        `json:"task_status,omitempty"`
	State          string        `json:"state"`
	Reason         string        `json:"reason,omitempty"`
	Provider       string        `json:"provider,omitempty"`
	Model          string        `json:"model,omitempty"`
	Tier           string        `json:"tier,omitempty"`
	PermissionMode string        `json:"permission_mode,omitempty"`
	Risk           string        `json:"risk,omitempty"`
	BlockedBy      []TaskSummary `json:"blocked_by,omitempty"`
	Blocks         []TaskSummary `json:"blocks,omitempty"`
	LatestRun      *BrainRunView `json:"latest_run,omitempty"`
}

type BrainScheduleEvent struct {
	Kind     string `json:"kind"`
	ItemID   string `json:"item_id,omitempty"`
	TaskSlug string `json:"task_slug,omitempty"`
	RunID    string `json:"run_id,omitempty"`
	Message  string `json:"message"`
}

type BrainScheduleProposal struct {
	Kind     string `json:"kind"`
	ItemID   string `json:"item_id,omitempty"`
	TaskSlug string `json:"task_slug,omitempty"`
	Message  string `json:"message"`
}

type BrainScheduleLaunchView struct {
	RunID        string `json:"run_id,omitempty"`
	ItemID       string `json:"item_id"`
	TaskSlug     string `json:"task_slug"`
	Provider     string `json:"provider"`
	Model        string `json:"model,omitempty"`
	TargetBranch string `json:"target_branch,omitempty"`
	Output       string `json:"output,omitempty"`
}

type brainWorkerLaunchRequest struct {
	PlanID         string
	ItemID         string
	TaskSlug       string
	Provider       string
	Model          string
	Tier           string
	PermissionMode string
	Risk           string
	TargetBranch   string
	Force          bool
	InitiatedBy    string
}

type brainWorkerLaunchResult struct {
	RunID  string
	Output string
}

type brainWorkerLauncherFunc func(*Server, brainWorkerLaunchRequest) (brainWorkerLaunchResult, error)

var brainWorkerLauncher brainWorkerLauncherFunc = func(s *Server, req brainWorkerLaunchRequest) (brainWorkerLaunchResult, error) {
	args := []string{"do", req.TaskSlug, "--auto"}
	if req.Provider != "" {
		args = append(args, "--agent", req.Provider)
	}
	if req.PlanID != "" {
		args = append(args, "--brain-plan", req.PlanID)
	}
	if req.ItemID != "" {
		args = append(args, "--brain-item", req.ItemID)
	}
	if req.TargetBranch != "" {
		args = append(args, "--brain-target-branch", req.TargetBranch)
	}
	if req.InitiatedBy != "" {
		args = append(args, "--brain-initiated-by", req.InitiatedBy)
	}
	if req.Force {
		args = append(args, "--force")
	}
	out, err := s.runFlowCommand(args...)
	if err != nil {
		return brainWorkerLaunchResult{Output: out}, err
	}
	runID, runErr := latestBrainRunIDForTask(s.cfg.DB, req.TaskSlug)
	if runErr != nil {
		return brainWorkerLaunchResult{Output: out}, runErr
	}
	if runID == "" {
		return brainWorkerLaunchResult{Output: out}, errors.New("brain worker launch did not record a run id")
	}
	_ = s.attachBrainLaunchMetadata(req, runID)
	return brainWorkerLaunchResult{RunID: runID, Output: out}, nil
}

func (s *Server) handleBrainPlanSchedule(w http.ResponseWriter, r *http.Request, plan *brain.Plan) {
	req := brainScheduleRequestFromQuery(r)
	switch r.Method {
	case http.MethodGet:
		view, status := s.buildBrainSchedule(plan, req)
		writeJSONStatus(w, view, status)
	case http.MethodPost:
		if r.Body != nil && r.Body != http.NoBody {
			var body BrainScheduleRequest
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
				writeError(w, err, http.StatusBadRequest)
				return
			} else {
				if body.TimeoutMinutes != 0 {
					req.TimeoutMinutes = body.TimeoutMinutes
				}
				req.Launch = body.Launch
				req.Force = body.Force
				req.ApproveHighRisk = body.ApproveHighRisk
				req.MaxLaunches = body.MaxLaunches
			}
		}
		view, status := s.runBrainSchedule(plan, req)
		writeJSONStatus(w, view, status)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func brainScheduleRequestFromQuery(r *http.Request) BrainScheduleRequest {
	var req BrainScheduleRequest
	if r == nil {
		return req
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("timeout_minutes")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			req.TimeoutMinutes = n
		}
	}
	return req
}

func (s *Server) runBrainSchedule(plan *brain.Plan, req BrainScheduleRequest) (BrainScheduleView, int) {
	if plan == nil {
		return BrainScheduleView{}, http.StatusInternalServerError
	}
	if req.MaxLaunches <= 0 {
		req.MaxLaunches = 1
	}
	view, status := s.buildBrainSchedule(plan, req)
	if !req.Launch {
		return view, status
	}
	switch plan.Status {
	case brain.StatusCancelled, brain.StatusRejected:
		view.Events = append(view.Events, BrainScheduleEvent{
			Kind:    "plan_not_launchable",
			Message: fmt.Sprintf("plan is %s; no workers launched", plan.Status),
		})
		return view, http.StatusConflict
	case brain.StatusDraft:
		view.Proposals = append(view.Proposals, BrainScheduleProposal{
			Kind:    "approve_plan",
			Message: "approve and execute the plan before launching workers",
		})
		return view, http.StatusConflict
	}

	targetBranch := brainPlanTargetBranch(plan)
	launches := 0
	for _, item := range view.Items {
		if launches >= req.MaxLaunches {
			break
		}
		if item.Kind != string(brain.ItemKindTask) || item.State != "ready" {
			continue
		}
		if item.Risk == "high" && !req.ApproveHighRisk {
			continue
		}
		launchReq := brainWorkerLaunchRequest{
			PlanID:         plan.ID,
			ItemID:         item.ItemID,
			TaskSlug:       item.TaskSlug,
			Provider:       item.Provider,
			Model:          item.Model,
			Tier:           item.Tier,
			PermissionMode: item.PermissionMode,
			Risk:           item.Risk,
			TargetBranch:   targetBranch,
			Force:          req.Force,
			InitiatedBy:    "brain-scheduler",
		}
		result, err := brainWorkerLauncher(s, launchReq)
		if err != nil {
			view.Events = append(view.Events, BrainScheduleEvent{
				Kind:     "worker_launch_failed",
				ItemID:   item.ItemID,
				TaskSlug: item.TaskSlug,
				Message:  err.Error(),
			})
			view.Proposals = append(view.Proposals, BrainScheduleProposal{
				Kind:     "inspect_launch_failure",
				ItemID:   item.ItemID,
				TaskSlug: item.TaskSlug,
				Message:  "inspect the launch failure before retrying",
			})
			return view, http.StatusInternalServerError
		}
		view.Launches = append(view.Launches, BrainScheduleLaunchView{
			RunID:        result.RunID,
			ItemID:       item.ItemID,
			TaskSlug:     item.TaskSlug,
			Provider:     item.Provider,
			Model:        item.Model,
			TargetBranch: targetBranch,
			Output:       result.Output,
		})
		launches++
	}
	if launches == 0 {
		return view, status
	}
	next, nextStatus := s.buildBrainSchedule(plan, req)
	next.Launches = mergeBrainLaunches(next.Launches, view.Launches)
	return next, nextStatus
}

func (s *Server) buildBrainSchedule(plan *brain.Plan, req BrainScheduleRequest) (BrainScheduleView, int) {
	view := BrainScheduleView{}
	if plan == nil {
		return view, http.StatusInternalServerError
	}
	timeout := defaultBrainWorkerTimeout
	if req.TimeoutMinutes > 0 {
		timeout = time.Duration(req.TimeoutMinutes) * time.Minute
	}
	view.PlanID = plan.ID
	view.Status = plan.Status
	view.Launches = s.brainPlanLaunches(plan)
	for i := range plan.Items {
		item := plan.Items[i]
		itemView := s.brainScheduleItem(plan, &item, timeout)
		view.Items = append(view.Items, itemView)
		view.count(itemView.State)
		view.addDerivedSignals(itemView, req)
	}
	return view, http.StatusOK
}

func (s *Server) brainScheduleItem(plan *brain.Plan, item *brain.Item, timeout time.Duration) BrainScheduleItemView {
	out := BrainScheduleItemView{
		ItemID:         item.ID,
		Kind:           string(item.Kind),
		Title:          item.Title,
		TaskSlug:       strings.TrimSpace(firstNonEmpty(item.TaskSlug, taskSpecSlug(item))),
		State:          "deferred",
		Provider:       item.Provider,
		Model:          item.Model,
		Tier:           item.Tier,
		PermissionMode: item.PermissionMode,
		Risk:           item.Risk,
	}
	if item.Kind != brain.ItemKindTask {
		out.Reason = "non-worker action requires explicit operator approval"
		return out
	}
	if out.TaskSlug == "" {
		out.State = "missing"
		out.Reason = "task has not been materialized"
		return out
	}
	task, err := flowdb.GetTask(s.cfg.DB, out.TaskSlug)
	if err != nil {
		out.State = "missing"
		out.Reason = err.Error()
		return out
	}
	out.TaskName = task.Name
	out.TaskStatus = task.Status
	if out.Provider == "" {
		out.Provider = task.SessionProvider
	}
	if out.Model == "" && task.Model.Valid {
		out.Model = task.Model.String
	}
	if out.PermissionMode == "" {
		out.PermissionMode = task.PermissionMode
	}
	out.Blocks = s.brainBlockingDependents(task.Slug)
	if latest := s.latestBrainRunView(task); latest != nil {
		out.LatestRun = latest
	}
	out.State, out.Reason = s.brainTaskState(plan, task, timeout)
	if blocker, err := flowdb.TaskStartBlockerFor(s.cfg.DB, task); err == nil && blocker != nil {
		out.BlockedBy = taskSummariesFromBlocker(blocker)
	}
	return out
}

func (s *Server) brainTaskState(plan *brain.Plan, task *flowdb.Task, timeout time.Duration) (string, string) {
	switch plan.Status {
	case brain.StatusCancelled, brain.StatusRejected:
		return "cancelled", fmt.Sprintf("plan is %s", plan.Status)
	}
	if task.Status == "done" {
		return "done", "task is done"
	}
	if task.AutoRunStatus.Valid {
		switch task.AutoRunStatus.String {
		case "running":
			if autoRunTimedOut(task.AutoRunStarted.String, timeout) {
				return "timeout", "worker run exceeded timeout"
			}
			return "running", "worker run is already in progress"
		case "dead", "error":
			return "dead", "last worker run did not complete"
		case "completed":
			return "done", "worker completed"
		}
	}
	if blocker, err := flowdb.TaskStartBlockerFor(s.cfg.DB, task); err != nil {
		return "blocked", err.Error()
	} else if blocker != nil {
		return "blocked", blocker.Error()
	}
	return "ready", "all dependencies are satisfied"
}

func (view *BrainScheduleView) count(state string) {
	switch state {
	case "ready":
		view.Summary.Ready++
	case "running":
		view.Summary.Running++
	case "blocked":
		view.Summary.Blocked++
	case "done":
		view.Summary.Done++
	case "dead":
		view.Summary.Dead++
	case "timeout":
		view.Summary.Timeout++
	case "missing":
		view.Summary.Missing++
	default:
		view.Summary.Deferred++
	}
}

func (view *BrainScheduleView) addDerivedSignals(item BrainScheduleItemView, req BrainScheduleRequest) {
	switch item.State {
	case "ready":
		if item.Risk == "high" && !req.ApproveHighRisk {
			view.Proposals = append(view.Proposals, BrainScheduleProposal{
				Kind:     "approve_high_risk_worker",
				ItemID:   item.ItemID,
				TaskSlug: item.TaskSlug,
				Message:  "high-risk worker launch requires explicit approval",
			})
			return
		}
		view.Proposals = append(view.Proposals, BrainScheduleProposal{
			Kind:     "launch_worker",
			ItemID:   item.ItemID,
			TaskSlug: item.TaskSlug,
			Message:  "launch this startable worker",
		})
	case "running":
		runID := ""
		if item.LatestRun != nil {
			runID = item.LatestRun.RunID
		}
		view.Events = append(view.Events, BrainScheduleEvent{
			Kind:     "worker_running",
			ItemID:   item.ItemID,
			TaskSlug: item.TaskSlug,
			RunID:    runID,
			Message:  "worker is already running; duplicate launch skipped",
		})
	case "dead":
		view.Events = append(view.Events, BrainScheduleEvent{
			Kind:     "worker_dead",
			ItemID:   item.ItemID,
			TaskSlug: item.TaskSlug,
			Message:  "worker run ended without completing the task",
		})
		view.Proposals = append(view.Proposals, BrainScheduleProposal{
			Kind:     "retry_worker",
			ItemID:   item.ItemID,
			TaskSlug: item.TaskSlug,
			Message:  "review the run evidence and retry if the failure is understood",
		})
	case "timeout":
		view.Events = append(view.Events, BrainScheduleEvent{
			Kind:     "worker_timeout",
			ItemID:   item.ItemID,
			TaskSlug: item.TaskSlug,
			Message:  "worker has been running longer than the scheduler timeout",
		})
		view.Proposals = append(view.Proposals, BrainScheduleProposal{
			Kind:     "review_timed_out_worker",
			ItemID:   item.ItemID,
			TaskSlug: item.TaskSlug,
			Message:  "inspect the worker before deciding whether to cancel or retry",
		})
	case "done":
		runID := ""
		if item.LatestRun != nil {
			runID = item.LatestRun.RunID
		}
		view.Events = append(view.Events, BrainScheduleEvent{
			Kind:     "worker_completed",
			ItemID:   item.ItemID,
			TaskSlug: item.TaskSlug,
			RunID:    runID,
			Message:  "task is complete; dependents may now become startable",
		})
	}
}

func (s *Server) latestBrainRunView(task *flowdb.Task) *BrainRunView {
	if task == nil {
		return nil
	}
	run, err := latestBrainRunForTask(s.cfg.DB, task.Slug)
	if err == nil && run != nil {
		view := brainRunViewFromDB(run)
		attachTaskToBrainRunView(&view, task)
		return &view
	}
	if !task.AutoRunStatus.Valid || strings.TrimSpace(task.AutoRunStatus.String) == "" {
		return nil
	}
	view := BrainRunView{
		RunID:          "legacy:auto-run:" + task.Slug,
		FamilySlug:     task.Slug,
		TaskSlug:       task.Slug,
		TaskName:       task.Name,
		TaskStatus:     task.Status,
		Role:           "worker",
		Provider:       task.SessionProvider,
		PermissionMode: task.PermissionMode,
		Status:         task.AutoRunStatus.String,
		Legacy:         true,
		CreatedAt:      firstNonEmpty(task.AutoRunStarted.String, task.CreatedAt),
		UpdatedAt:      task.UpdatedAt,
	}
	if task.Model.Valid {
		view.RequestedModel = &task.Model.String
		view.ResolvedModel = &task.Model.String
	}
	if task.AutoRunPID.Valid {
		pid := task.AutoRunPID.Int64
		view.PID = &pid
	}
	if task.AutoRunStarted.Valid {
		view.StartedAt = &task.AutoRunStarted.String
	}
	if task.AutoRunFinished.Valid {
		view.FinishedAt = &task.AutoRunFinished.String
	}
	if task.AutoRunLog.Valid {
		view.LogPath = &task.AutoRunLog.String
	}
	if task.SessionID.Valid {
		view.SessionID = &task.SessionID.String
	}
	return &view
}

func latestBrainRunForTask(db *sql.DB, taskSlug string) (*flowdb.BrainRun, error) {
	row := db.QueryRow(
		`SELECT `+flowdb.BrainRunCols+` FROM brain_runs
		 WHERE task_slug = ?
		 ORDER BY COALESCE(started_at, created_at) DESC, created_at DESC, run_id DESC
		 LIMIT 1`,
		taskSlug,
	)
	return flowdb.ScanBrainRun(row)
}

func latestBrainRunIDForTask(db *sql.DB, taskSlug string) (string, error) {
	run, err := latestBrainRunForTask(db, taskSlug)
	if err != nil {
		return "", err
	}
	return run.RunID, nil
}

func (s *Server) brainBlockingDependents(parentSlug string) []TaskSummary {
	rows, err := s.cfg.DB.Query(`
		SELECT t.slug, t.name, t.status, t.priority, t.project_slug, t.updated_at
		FROM task_dependencies d
		JOIN tasks t ON t.slug = d.child_slug
		WHERE d.parent_slug = ? AND t.status != 'done' AND t.deleted_at IS NULL
		ORDER BY d.created_at ASC, t.slug ASC`,
		parentSlug,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []TaskSummary
	for rows.Next() {
		var s TaskSummary
		var project sql.NullString
		if err := rows.Scan(&s.Slug, &s.Name, &s.Status, &s.Priority, &project, &s.UpdatedAt); err != nil {
			return out
		}
		if project.Valid {
			s.ProjectSlug = &project.String
		}
		out = append(out, s)
	}
	return out
}

func taskSummariesFromBlocker(blocker *flowdb.TaskStartBlocker) []TaskSummary {
	if blocker == nil || blocker.Kind != "dependency" {
		return nil
	}
	out := make([]TaskSummary, 0, len(blocker.Parents))
	for _, p := range blocker.Parents {
		out = append(out, TaskSummary{
			Slug:   p.Slug,
			Name:   p.Name,
			Status: p.Status,
		})
	}
	return out
}

func autoRunTimedOut(started string, timeout time.Duration) bool {
	if timeout <= 0 || strings.TrimSpace(started) == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, started)
	if err != nil {
		return false
	}
	return time.Since(t) > timeout
}

func taskSpecSlug(item *brain.Item) string {
	if item == nil || item.Task == nil {
		return ""
	}
	return item.Task.Slug
}

var brainTargetAgainstRe = regexp.MustCompile(`(?i)\bagainst\s+` + "`?" + `([A-Za-z0-9._/\-]+)` + "`?")

func brainPlanTargetBranch(plan *brain.Plan) string {
	if plan == nil {
		return ""
	}
	if m := brainTargetAgainstRe.FindStringSubmatch(plan.BranchPolicy); len(m) == 2 {
		return m[1]
	}
	return ""
}

func (s *Server) attachBrainLaunchMetadata(req brainWorkerLaunchRequest, runID string) error {
	if s == nil || s.cfg.DB == nil {
		return nil
	}
	run, err := flowdb.GetBrainRun(s.cfg.DB, runID)
	if err != nil {
		return err
	}
	updated := false
	if req.PlanID != "" {
		if !run.PlanID.Valid || strings.TrimSpace(run.PlanID.String) != req.PlanID {
			run.PlanID = sql.NullString{String: req.PlanID, Valid: true}
			updated = true
		}
	}
	if req.Tier != "" {
		if !run.RequestedTier.Valid || strings.TrimSpace(run.RequestedTier.String) != req.Tier {
			run.RequestedTier = sql.NullString{String: req.Tier, Valid: true}
			updated = true
		}
	}
	summary := strings.TrimSpace(run.InputSummary.String)
	if req.InitiatedBy != "" {
		summary = appendBrainLaunchSummaryClause(summary, "started by "+req.InitiatedBy)
	}
	if req.ItemID != "" {
		summary = appendBrainLaunchSummaryClause(summary, "plan item "+req.ItemID)
	}
	if req.TargetBranch != "" {
		summary = appendBrainLaunchSummaryClause(summary, "target branch "+req.TargetBranch)
	}
	evidence := brainLaunchEvidence(run)
	if req.PlanID != "" {
		evidence["plan_id"] = req.PlanID
	}
	if req.ItemID != "" {
		evidence["plan_item_id"] = req.ItemID
	}
	if req.TargetBranch != "" {
		evidence["target_branch"] = req.TargetBranch
	}
	if req.InitiatedBy != "" {
		evidence["initiated_by"] = req.InitiatedBy
	}
	if req.Provider != "" {
		evidence["provider"] = req.Provider
	}
	if req.Model != "" {
		evidence["requested_model"] = req.Model
	}
	if req.Tier != "" {
		evidence["requested_tier"] = req.Tier
	}
	if req.PermissionMode != "" {
		evidence["permission_mode"] = req.PermissionMode
	}
	if len(evidence) > 0 {
		if b, err := json.Marshal(evidence); err == nil {
			next := string(b)
			if !run.EvidenceJSON.Valid || run.EvidenceJSON.String != next {
				run.EvidenceJSON = sql.NullString{String: next, Valid: true}
				updated = true
			}
		}
	}
	if summary != "" && (!run.InputSummary.Valid || run.InputSummary.String != summary) {
		run.InputSummary = sql.NullString{String: summary, Valid: true}
		updated = true
	}
	if !updated {
		return nil
	}
	run.UpdatedAt = flowdb.NowISO()
	return flowdb.UpsertBrainRun(s.cfg.DB, run)
}

func brainLaunchEvidence(run *flowdb.BrainRun) map[string]any {
	out := map[string]any{}
	if run == nil || !run.EvidenceJSON.Valid || strings.TrimSpace(run.EvidenceJSON.String) == "" {
		return out
	}
	if err := json.Unmarshal([]byte(run.EvidenceJSON.String), &out); err != nil {
		return map[string]any{}
	}
	return out
}

func appendBrainLaunchSummaryClause(base, clause string) string {
	base = strings.TrimSpace(base)
	clause = strings.TrimSpace(clause)
	if clause == "" {
		return base
	}
	if base == "" {
		return clause
	}
	lowerBase := strings.ToLower(base)
	if strings.Contains(lowerBase, strings.ToLower(clause)) {
		return base
	}
	return base + "; " + clause
}

func (s *Server) brainPlanLaunches(plan *brain.Plan) []BrainScheduleLaunchView {
	if s == nil || s.cfg.DB == nil || plan == nil || strings.TrimSpace(plan.ID) == "" {
		return nil
	}
	itemIDByTaskSlug := map[string]string{}
	for i := range plan.Items {
		item := &plan.Items[i]
		if item.Kind != brain.ItemKindTask {
			continue
		}
		taskSlug := strings.TrimSpace(firstNonEmpty(item.TaskSlug, taskSpecSlug(item)))
		if taskSlug == "" {
			continue
		}
		itemIDByTaskSlug[taskSlug] = item.ID
	}
	rows, err := s.cfg.DB.Query(
		`SELECT `+flowdb.BrainRunCols+` FROM brain_runs
		 WHERE plan_id = ? AND role = 'worker'
		 ORDER BY COALESCE(started_at, created_at) DESC, created_at DESC, run_id DESC`,
		plan.ID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	targetBranch := brainPlanTargetBranch(plan)
	var out []BrainScheduleLaunchView
	for rows.Next() {
		run, err := flowdb.ScanBrainRun(rows)
		if err != nil || run == nil {
			return out
		}
		itemID := itemIDByTaskSlug[run.TaskSlug]
		if itemID == "" {
			continue
		}
		out = append(out, BrainScheduleLaunchView{
			RunID:        run.RunID,
			ItemID:       itemID,
			TaskSlug:     run.TaskSlug,
			Provider:     run.Provider,
			Model:        brainRunLaunchModel(run),
			TargetBranch: targetBranch,
		})
	}
	return out
}

func brainRunLaunchModel(run *flowdb.BrainRun) string {
	if run == nil {
		return ""
	}
	if model := strings.TrimSpace(run.ResolvedModel.String); model != "" {
		return model
	}
	if model := strings.TrimSpace(run.RequestedModel.String); model != "" {
		return model
	}
	return ""
}

func mergeBrainLaunches(dst, src []BrainScheduleLaunchView) []BrainScheduleLaunchView {
	if len(src) == 0 {
		return dst
	}
	index := make(map[string]int, len(dst))
	for i := range dst {
		if runID := strings.TrimSpace(dst[i].RunID); runID != "" {
			index[runID] = i
		}
	}
	for _, launch := range src {
		runID := strings.TrimSpace(launch.RunID)
		if runID == "" {
			continue
		}
		if i, ok := index[runID]; ok {
			if launch.ItemID != "" {
				dst[i].ItemID = launch.ItemID
			}
			if launch.TaskSlug != "" {
				dst[i].TaskSlug = launch.TaskSlug
			}
			if launch.Provider != "" {
				dst[i].Provider = launch.Provider
			}
			if launch.Model != "" {
				dst[i].Model = launch.Model
			}
			if launch.TargetBranch != "" {
				dst[i].TargetBranch = launch.TargetBranch
			}
			if launch.Output != "" {
				dst[i].Output = launch.Output
			}
			continue
		}
		index[runID] = len(dst)
		dst = append(dst, launch)
	}
	return dst
}
