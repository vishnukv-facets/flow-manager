package server

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"flow/internal/flowdb"
)

func (s *Server) handleBrainGraphNodeDetail(w http.ResponseWriter, r *http.Request) {
	parts, ok := routeParts(w, r, "/api/brain/graph/node/")
	if !ok {
		return
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	if !getOnly(w, r) {
		return
	}
	detail, err := BuildBrainGraphNodeDetail(s.cfg.DB, s.cfg.FlowRoot, parts[0])
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, detail)
}

func BuildBrainGraphNodeDetail(db *sql.DB, root, nodeID string) (BrainGraphNodeDetail, error) {
	nodeID = strings.TrimSpace(nodeID)
	switch {
	case strings.HasPrefix(nodeID, "task:"):
		slug := strings.TrimPrefix(nodeID, "task:")
		task, err := flowdb.GetTask(db, slug)
		if err != nil {
			return BrainGraphNodeDetail{}, err
		}
		return BrainGraphNodeDetail{
			ID:    "task:" + task.Slug,
			Type:  "task",
			Task:  brainGraphTaskDetail(root, task),
			Audit: []BrainGraphAuditView{},
		}, nil
	case strings.HasPrefix(nodeID, "run:auto:"):
		slug := strings.TrimPrefix(nodeID, "run:auto:")
		return brainGraphRunDetail(db, "legacy:auto-run:"+slug, nodeID)
	case strings.HasPrefix(nodeID, "run:"):
		runID := strings.TrimPrefix(nodeID, "run:")
		return brainGraphRunDetail(db, runID, nodeID)
	case strings.HasPrefix(nodeID, "approval:"):
		return brainGraphApprovalDetail(db, strings.TrimPrefix(nodeID, "approval:"), nodeID)
	case strings.HasPrefix(nodeID, "transcript:"):
		slug := strings.TrimPrefix(nodeID, "transcript:")
		task, err := flowdb.GetTask(db, slug)
		if err != nil {
			return BrainGraphNodeDetail{}, err
		}
		evidence := brainGraphTranscriptDetail(task)
		if evidence == nil {
			return BrainGraphNodeDetail{}, sql.ErrNoRows
		}
		return BrainGraphNodeDetail{
			ID:       "transcript:" + task.Slug,
			Type:     "transcript_ref",
			Evidence: evidence,
			Audit:    []BrainGraphAuditView{},
		}, nil
	case strings.HasPrefix(nodeID, "github:"):
		return brainGraphGitHubEvidenceDetail(db, strings.TrimPrefix(nodeID, "github:"), nodeID)
	default:
		return BrainGraphNodeDetail{}, sql.ErrNoRows
	}
}

func brainGraphTaskDetail(root string, task *flowdb.Task) *BrainGraphTaskDetail {
	briefPath := filepath.Join(root, "tasks", task.Slug, "brief.md")
	updates := markdownFiles(filepath.Join(root, "tasks", task.Slug, "updates"), true)
	if updates == nil {
		updates = []FileRef{}
	}
	if len(updates) > 5 {
		updates = updates[:5]
	}
	return &BrainGraphTaskDetail{
		Slug:            task.Slug,
		Name:            task.Name,
		Status:          task.Status,
		Priority:        task.Priority,
		ProjectSlug:     nullStringPtr(task.ProjectSlug),
		ParentSlug:      nullStringPtr(task.ParentSlug),
		WorkDir:         task.WorkDir,
		WorktreePath:    nullStringPtr(task.WorktreePath),
		SessionProvider: task.SessionProvider,
		Harness:         task.Harness,
		PermissionMode:  task.PermissionMode,
		Model:           nullStringPtr(task.Model),
		SessionID:       nullStringPtr(task.SessionID),
		SessionPath:     nullStringPtr(task.SessionPath),
		Transcript:      brainGraphTranscriptDetail(task),
		BriefPath:       briefPath,
		Updates:         updates,
	}
}

func brainGraphRunDetail(db *sql.DB, runID, nodeID string) (BrainGraphNodeDetail, error) {
	run, err := flowdb.GetBrainRun(db, runID)
	if err != nil {
		return BrainGraphNodeDetail{}, err
	}
	detail := brainGraphRunDetailFromDB(run)
	if task, err := flowdb.GetTask(db, run.TaskSlug); err == nil {
		detail.TaskName = &task.Name
		detail.TaskStatus = &task.Status
	}
	return BrainGraphNodeDetail{
		ID:    nodeID,
		Type:  brainGraphRunNodeType(run.Role),
		Run:   &detail,
		Audit: []BrainGraphAuditView{},
	}, nil
}

func brainGraphRunDetailFromDB(run *flowdb.BrainRun) BrainGraphRunDetail {
	if run == nil {
		return BrainGraphRunDetail{}
	}
	return BrainGraphRunDetail{
		RunID:          run.RunID,
		FamilySlug:     run.FamilySlug,
		TaskSlug:       run.TaskSlug,
		PlanID:         nullStringPtr(run.PlanID),
		Role:           run.Role,
		Provider:       run.Provider,
		RequestedModel: nullStringPtr(run.RequestedModel),
		RequestedTier:  nullStringPtr(run.RequestedTier),
		ResolvedModel:  nullStringPtr(run.ResolvedModel),
		PermissionMode: run.PermissionMode,
		Status:         run.Status,
		PID:            nullInt64Ptr(run.PID),
		SessionID:      nullStringPtr(run.SessionID),
		LogPath:        nullStringPtr(run.LogPath),
		InputSummary:   nullStringPtr(run.InputSummary),
		OutputJSON:     rawJSONFromNullString(run.OutputJSON),
		EvidenceJSON:   rawJSONFromNullString(run.EvidenceJSON),
		ErrorText:      nullStringPtr(run.ErrorText),
		StartedAt:      nullStringPtr(run.StartedAt),
		FinishedAt:     nullStringPtr(run.FinishedAt),
		CreatedAt:      run.CreatedAt,
		UpdatedAt:      run.UpdatedAt,
		Legacy:         run.Legacy,
	}
}

func brainGraphTranscriptDetail(task *flowdb.Task) *BrainGraphEvidenceDetail {
	if task == nil || !task.SessionID.Valid || strings.TrimSpace(task.SessionID.String) == "" {
		return nil
	}
	path := nullStringPtr(task.SessionPath)
	available := false
	message := "transcript path not captured"
	if path != nil {
		if _, err := os.Stat(*path); err == nil {
			available = true
			message = ""
		} else {
			message = "transcript file not found"
		}
	}
	return &BrainGraphEvidenceDetail{
		Kind:      "transcript",
		TaskSlug:  task.Slug,
		RefID:     task.SessionID.String,
		Path:      path,
		Available: available,
		Message:   message,
	}
}

func brainGraphGitHubEvidenceDetail(db *sql.DB, escapedTag, nodeID string) (BrainGraphNodeDetail, error) {
	tag, err := url.PathUnescape(escapedTag)
	if err != nil {
		return BrainGraphNodeDetail{}, err
	}
	tag = flowdb.NormalizeTag(tag)
	var taskSlug string
	err = db.QueryRow(`SELECT task_slug FROM task_tags WHERE tag = ? ORDER BY task_slug LIMIT 1`, tag).Scan(&taskSlug)
	if err != nil {
		return BrainGraphNodeDetail{}, err
	}
	urlValue := brainGraphGitHubRefURL(tag)
	evidence := &BrainGraphEvidenceDetail{
		Kind:      "github",
		TaskSlug:  taskSlug,
		RefID:     tag,
		URL:       stringPtrIfNotEmpty(urlValue),
		Available: true,
	}
	return BrainGraphNodeDetail{
		ID:       brainGraphGitHubRefNodeID(tag),
		Type:     "github_ref",
		Evidence: evidence,
		Audit:    []BrainGraphAuditView{},
	}, nil
}

func brainGraphApprovalDetail(db *sql.DB, rest, nodeID string) (BrainGraphNodeDetail, error) {
	action, taskSlug, ok := strings.Cut(rest, ":")
	if !ok || strings.TrimSpace(action) == "" || strings.TrimSpace(taskSlug) == "" {
		return BrainGraphNodeDetail{}, sql.ErrNoRows
	}
	task, err := flowdb.GetTask(db, taskSlug)
	if err != nil {
		return BrainGraphNodeDetail{}, err
	}
	policyMode := flowdb.BrainPolicyModeApprovalRequired
	if policy, err := flowdb.GetBrainPolicy(db); err != nil {
		return BrainGraphNodeDetail{}, err
	} else if policy.IsWhitelisted(action) {
		policyMode = flowdb.BrainPolicyModeAuto
	}
	audits, err := flowdb.ListBrainActionAudit(db, "task", taskSlug, 10)
	if err != nil {
		return BrainGraphNodeDetail{}, err
	}
	return BrainGraphNodeDetail{
		ID:   nodeID,
		Type: "approval",
		Approval: &BrainGraphApprovalDetail{
			Action:     action,
			TaskSlug:   taskSlug,
			TaskName:   &task.Name,
			PolicyMode: policyMode,
		},
		Audit: brainGraphAuditViews(audits),
	}, nil
}

func brainGraphAuditViews(audits []*flowdb.BrainActionAudit) []BrainGraphAuditView {
	out := make([]BrainGraphAuditView, 0, len(audits))
	for _, audit := range audits {
		if audit == nil {
			continue
		}
		out = append(out, BrainGraphAuditView{
			ID:           audit.ID,
			Action:       audit.Action,
			TargetType:   audit.TargetType,
			TargetID:     audit.TargetID,
			Actor:        audit.Actor,
			Policy:       audit.Policy,
			EvidenceJSON: rawJSONFromString(audit.EvidenceJSON),
			Result:       audit.Result,
			ErrorText:    nullStringPtr(audit.ErrorText),
			CreatedAt:    audit.CreatedAt,
		})
	}
	return out
}

func rawJSONFromString(raw string) []byte {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return []byte(raw)
}

func stringPtrIfNotEmpty(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}
