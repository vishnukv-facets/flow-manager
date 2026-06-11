package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"flow/internal/flowdb"
)

func autoBrainRunInputSummary(task *flowdb.Task, provider, requestedModel, resolvedModel, permissionMode, injection, playbookSlug string) string {
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
	if strings.TrimSpace(injection) != "" {
		parts = append(parts, "operator instruction attached")
	}
	return strings.Join(parts, "; ")
}

func newAutoBrainRun(task *flowdb.Task, familySlug, runID, provider, permissionMode, requestedModel, resolvedModel, injection, sessionID, playbookSlug string) *flowdb.BrainRun {
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
		InputSummary:   sql.NullString{String: autoBrainRunInputSummary(task, provider, requestedModel, resolvedModel, permissionMode, injection, playbookSlug), Valid: true},
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
	if run.RequestedModel.Valid && run.RequestedModel.String != "" {
		output["requested_model"] = run.RequestedModel.String
	}
	if run.ResolvedModel.Valid && run.ResolvedModel.String != "" {
		output["resolved_model"] = run.ResolvedModel.String
	}
	if runErr != nil {
		output["error"] = runErr.Error()
	}

	evidence := map[string]any{
		"run_id":      run.RunID,
		"task_slug":   run.TaskSlug,
		"family_slug": run.FamilySlug,
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
