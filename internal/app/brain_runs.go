package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"flow/internal/flowdb"
)

type autoRunLaunchMetadata struct {
	PlanID       string
	ItemID       string
	TargetBranch string
	InitiatedBy  string
}

func (m autoRunLaunchMetadata) normalized() autoRunLaunchMetadata {
	return autoRunLaunchMetadata{
		PlanID:       strings.TrimSpace(m.PlanID),
		ItemID:       strings.TrimSpace(m.ItemID),
		TargetBranch: strings.TrimSpace(m.TargetBranch),
		InitiatedBy:  strings.TrimSpace(m.InitiatedBy),
	}
}

func (m autoRunLaunchMetadata) hasAny() bool {
	m = m.normalized()
	return m.PlanID != "" || m.ItemID != "" || m.TargetBranch != "" || m.InitiatedBy != ""
}

func autoBrainRunInputSummary(task *flowdb.Task, provider, requestedModel, resolvedModel, permissionMode, injection, playbookSlug string, meta autoRunLaunchMetadata) string {
	meta = meta.normalized()
	parts := []string{fmt.Sprintf("%s autonomous run", strings.TrimSpace(provider))}
	if task != nil {
		if task.Kind == "playbook_run" && strings.TrimSpace(playbookSlug) != "" {
			parts = append(parts, "playbook "+strings.TrimSpace(playbookSlug))
		} else {
			parts = append(parts, "task "+task.Slug)
		}
	}
	if requestedModel != "" {
		if resolvedModel != "" && requestedModel != resolvedModel {
			parts = append(parts, "requested model "+requestedModel)
		} else {
			parts = append(parts, "model "+requestedModel)
		}
	}
	if resolvedModel != "" && resolvedModel != requestedModel {
		parts = append(parts, "resolved model "+resolvedModel)
	}
	if permissionMode != "" {
		parts = append(parts, "permission "+permissionMode)
	}
	if meta.PlanID != "" {
		parts = append(parts, "brain plan "+meta.PlanID)
	}
	if meta.ItemID != "" {
		parts = append(parts, "plan item "+meta.ItemID)
	}
	if meta.TargetBranch != "" {
		parts = append(parts, "target branch "+meta.TargetBranch)
	}
	if meta.InitiatedBy != "" {
		parts = append(parts, "started by "+meta.InitiatedBy)
	}
	if strings.TrimSpace(injection) != "" {
		parts = append(parts, "operator instruction attached")
	}
	return strings.Join(parts, "; ")
}

func newAutoBrainRun(task *flowdb.Task, familySlug, runID, provider, permissionMode, requestedModel, resolvedModel, injection, sessionID, playbookSlug string, meta autoRunLaunchMetadata) *flowdb.BrainRun {
	now := flowdb.NowISO()
	run := &flowdb.BrainRun{
		RunID:          runID,
		FamilySlug:     familySlug,
		TaskSlug:       task.Slug,
		Role:           "worker",
		Provider:       provider,
		PermissionMode: permissionMode,
		Status:         "queued",
		CreatedAt:      now,
		UpdatedAt:      now,
		InputSummary:   sql.NullString{String: autoBrainRunInputSummary(task, provider, requestedModel, resolvedModel, permissionMode, injection, playbookSlug, meta), Valid: true},
	}
	if requestedModel != "" {
		run.RequestedModel = sql.NullString{String: requestedModel, Valid: true}
	}
	if resolvedModel != "" {
		run.ResolvedModel = sql.NullString{String: resolvedModel, Valid: true}
	}
	if strings.TrimSpace(sessionID) != "" {
		run.SessionID = sql.NullString{String: sessionID, Valid: true}
	}
	applyAutoBrainRunMetadata(run, meta)
	return run
}

func autoBrainRunResult(run *flowdb.BrainRun, finalTask *flowdb.Task, status string, runErr error) (sql.NullString, sql.NullString, sql.NullString) {
	if run == nil {
		return sql.NullString{}, sql.NullString{}, sql.NullString{}
	}
	taskStatus := ""
	if finalTask != nil {
		taskStatus = finalTask.Status
	}
	output := map[string]any{
		"run_id":          run.RunID,
		"family_slug":     run.FamilySlug,
		"task_slug":       run.TaskSlug,
		"role":            run.Role,
		"provider":        run.Provider,
		"status":          status,
		"task_status":     taskStatus,
		"permission_mode": run.PermissionMode,
	}
	if run.PlanID.Valid && strings.TrimSpace(run.PlanID.String) != "" {
		output["plan_id"] = run.PlanID.String
	}
	if run.RequestedModel.Valid && run.RequestedModel.String != "" {
		output["requested_model"] = run.RequestedModel.String
	}
	if run.ResolvedModel.Valid && run.ResolvedModel.String != "" {
		output["resolved_model"] = run.ResolvedModel.String
	}
	if runErr != nil {
		output["error"] = runErr.Error()
	}

	evidence := brainRunEvidenceMap(run)
	evidence["run_id"] = run.RunID
	evidence["task_slug"] = run.TaskSlug
	evidence["family_slug"] = run.FamilySlug
	if run.PlanID.Valid && strings.TrimSpace(run.PlanID.String) != "" {
		evidence["plan_id"] = run.PlanID.String
	}
	if run.Provider != "" {
		evidence["provider"] = run.Provider
	}
	if run.PermissionMode != "" {
		evidence["permission_mode"] = run.PermissionMode
	}
	if run.RequestedModel.Valid && strings.TrimSpace(run.RequestedModel.String) != "" {
		evidence["requested_model"] = run.RequestedModel.String
	}
	if run.ResolvedModel.Valid && strings.TrimSpace(run.ResolvedModel.String) != "" {
		evidence["resolved_model"] = run.ResolvedModel.String
	}
	if run.PID.Valid {
		evidence["pid"] = run.PID.Int64
	}
	if run.SessionID.Valid && strings.TrimSpace(run.SessionID.String) != "" {
		evidence["session_id"] = run.SessionID.String
	}
	if run.LogPath.Valid && strings.TrimSpace(run.LogPath.String) != "" {
		evidence["log_path"] = run.LogPath.String
	}
	if runErr != nil {
		evidence["error"] = runErr.Error()
	}

	outputJSON, _ := json.Marshal(output)
	evidenceJSON, _ := json.Marshal(evidence)
	errText := sql.NullString{}
	if runErr != nil {
		errText = sql.NullString{String: runErr.Error(), Valid: true}
	}
	return sql.NullString{String: string(outputJSON), Valid: true}, sql.NullString{String: string(evidenceJSON), Valid: true}, errText
}

func applyAutoBrainRunMetadata(run *flowdb.BrainRun, meta autoRunLaunchMetadata) {
	if run == nil {
		return
	}
	meta = meta.normalized()
	if !meta.hasAny() {
		return
	}
	if meta.PlanID != "" {
		run.PlanID = sql.NullString{String: meta.PlanID, Valid: true}
	}
	evidence := brainRunEvidenceMap(run)
	if meta.InitiatedBy != "" {
		evidence["initiated_by"] = meta.InitiatedBy
	}
	if meta.PlanID != "" {
		evidence["plan_id"] = meta.PlanID
	}
	if meta.ItemID != "" {
		evidence["plan_item_id"] = meta.ItemID
	}
	if meta.TargetBranch != "" {
		evidence["target_branch"] = meta.TargetBranch
	}
	b, err := json.Marshal(evidence)
	if err == nil {
		run.EvidenceJSON = sql.NullString{String: string(b), Valid: true}
	}
}

func brainRunEvidenceMap(run *flowdb.BrainRun) map[string]any {
	out := map[string]any{}
	if run == nil || !run.EvidenceJSON.Valid || strings.TrimSpace(run.EvidenceJSON.String) == "" {
		return out
	}
	if err := json.Unmarshal([]byte(run.EvidenceJSON.String), &out); err != nil {
		return map[string]any{}
	}
	return out
}
