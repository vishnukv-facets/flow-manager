package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"flow/internal/brain"
	"flow/internal/flowdb"
	"flow/internal/workdirreg"
)

func (s *Server) handleBrainPlans(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !getOnly(w, r) {
			return
		}
		filter := flowdb.BrainPlanFilter{
			Status:  strings.TrimSpace(r.URL.Query().Get("status")),
			Project: strings.TrimSpace(r.URL.Query().Get("project")),
			Source:  strings.TrimSpace(r.URL.Query().Get("source")),
		}
		plans, err := flowdb.ListBrainPlans(s.cfg.DB, filter)
		if err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, plans)
	case http.MethodPost:
		s.createBrainPlan(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleBrainPlanRoute(w http.ResponseWriter, r *http.Request) {
	parts, ok := routeParts(w, r, "/api/brain/plans/")
	if !ok {
		return
	}
	if len(parts) == 0 {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimSpace(parts[0])
	plan, err := flowdb.GetBrainPlan(s.cfg.DB, id)
	if err != nil {
		writeNotFoundOrError(w, err)
		return
	}
	if len(parts) == 1 {
		if r.Method != http.MethodGet || !getOnly(w, r) {
			return
		}
		writeJSON(w, plan)
		return
	}
	if len(parts) == 2 && parts[1] == "schedule" {
		s.handleBrainPlanSchedule(w, r, plan)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	switch parts[1] {
	case "approve":
		s.approveBrainPlan(w, plan)
	case "reject":
		s.rejectBrainPlan(w, plan)
	case "cancel":
		s.cancelBrainPlan(w, plan)
	case "execute":
		s.executeBrainPlan(w, plan)
	case "schedule":
		s.handleBrainPlanSchedule(w, r, plan)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) createBrainPlan(w http.ResponseWriter, r *http.Request) {
	var plan brain.Plan
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&plan); err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	if err := brain.NormalizePlan(&plan); err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	plan.Status = brain.StatusDraft
	plan.Error = ""
	now := flowdb.NowISO()
	plan.CreatedAt = now
	plan.UpdatedAt = now
	plan.ApprovedAt = nil
	plan.ExecutedAt = nil
	plan.CompletedAt = nil
	plan.CancelledAt = nil
	plan.RejectedAt = nil
	plan.BlockedAt = nil
	if err := brain.ValidatePlan(&plan); err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	if err := flowdb.CreateBrainPlan(s.cfg.DB, &plan); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, plan, http.StatusCreated)
}

func (s *Server) approveBrainPlan(w http.ResponseWriter, plan *brain.Plan) {
	if plan == nil {
		writeError(w, errors.New("brain plan is nil"), http.StatusInternalServerError)
		return
	}
	switch plan.Status {
	case brain.StatusDraft, brain.StatusApproved, brain.StatusExecuting, brain.StatusBlocked:
		// allowed
	case brain.StatusCompleted, brain.StatusCancelled, brain.StatusRejected:
		writeJSONStatus(w, plan, http.StatusConflict)
		return
	default:
		writeJSONStatus(w, plan, http.StatusConflict)
		return
	}
	if plan.Status == brain.StatusApproved {
		writeJSON(w, plan)
		return
	}
	now := flowdb.NowISO()
	plan.Status = brain.StatusApproved
	plan.Error = ""
	plan.ApprovedAt = strPtrIfNil(plan.ApprovedAt, now)
	plan.UpdatedAt = now
	if err := flowdb.SaveBrainPlan(s.cfg.DB, plan); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, plan)
}

func (s *Server) rejectBrainPlan(w http.ResponseWriter, plan *brain.Plan) {
	if plan == nil {
		writeError(w, errors.New("brain plan is nil"), http.StatusInternalServerError)
		return
	}
	switch plan.Status {
	case brain.StatusCompleted:
		writeJSONStatus(w, plan, http.StatusConflict)
		return
	case brain.StatusRejected:
		writeJSON(w, plan)
		return
	}
	now := flowdb.NowISO()
	plan.Status = brain.StatusRejected
	plan.Error = ""
	plan.RejectedAt = strPtrIfNil(plan.RejectedAt, now)
	plan.UpdatedAt = now
	if err := flowdb.SaveBrainPlan(s.cfg.DB, plan); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, plan)
}

func (s *Server) cancelBrainPlan(w http.ResponseWriter, plan *brain.Plan) {
	if plan == nil {
		writeError(w, errors.New("brain plan is nil"), http.StatusInternalServerError)
		return
	}
	switch plan.Status {
	case brain.StatusCompleted:
		writeJSONStatus(w, plan, http.StatusConflict)
		return
	case brain.StatusCancelled:
		writeJSON(w, plan)
		return
	}
	now := flowdb.NowISO()
	plan.Status = brain.StatusCancelled
	plan.Error = ""
	plan.CancelledAt = strPtrIfNil(plan.CancelledAt, now)
	plan.UpdatedAt = now
	if err := flowdb.SaveBrainPlan(s.cfg.DB, plan); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, plan)
}

func (s *Server) executeBrainPlan(w http.ResponseWriter, plan *brain.Plan) {
	if plan == nil {
		writeError(w, errors.New("brain plan is nil"), http.StatusInternalServerError)
		return
	}
	switch plan.Status {
	case brain.StatusCompleted:
		writeJSON(w, plan)
		return
	case brain.StatusCancelled, brain.StatusRejected:
		writeJSONStatus(w, plan, http.StatusConflict)
		return
	case brain.StatusDraft, brain.StatusApproved, brain.StatusExecuting, brain.StatusBlocked:
		// allowed
	default:
		writeJSONStatus(w, plan, http.StatusConflict)
		return
	}

	now := flowdb.NowISO()
	if plan.Status != brain.StatusExecuting {
		plan.Status = brain.StatusExecuting
		plan.ExecutedAt = strPtrIfNil(plan.ExecutedAt, now)
		plan.UpdatedAt = now
		if err := flowdb.SaveBrainPlan(s.cfg.DB, plan); err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
	}

	taskSlugByItemID, internalDeps, internalParents, err := s.resolveBrainPlanExecution(plan)
	if err != nil {
		plan.Status = brain.StatusBlocked
		plan.BlockedAt = strPtrIfNil(plan.BlockedAt, now)
		plan.Error = err.Error()
		plan.UpdatedAt = now
		_ = flowdb.SaveBrainPlan(s.cfg.DB, plan)
		writeJSONStatus(w, plan, http.StatusConflict)
		return
	}

	if err := s.materializeBrainPlanTasks(plan, taskSlugByItemID, internalParents, now); err != nil {
		plan.Status = brain.StatusBlocked
		plan.BlockedAt = strPtrIfNil(plan.BlockedAt, now)
		plan.Error = err.Error()
		plan.UpdatedAt = now
		_ = flowdb.SaveBrainPlan(s.cfg.DB, plan)
		writeJSONStatus(w, plan, http.StatusConflict)
		return
	}
	if err := flowdb.SaveBrainPlan(s.cfg.DB, plan); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}

	if err := s.applyBrainPlanDependencies(plan, taskSlugByItemID, internalDeps); err != nil {
		plan.Status = brain.StatusBlocked
		plan.BlockedAt = strPtrIfNil(plan.BlockedAt, now)
		plan.Error = err.Error()
		plan.UpdatedAt = now
		_ = flowdb.SaveBrainPlan(s.cfg.DB, plan)
		writeJSONStatus(w, plan, http.StatusConflict)
		return
	}

	now = flowdb.NowISO()
	plan.Status = brain.StatusCompleted
	plan.Error = ""
	plan.CompletedAt = strPtrIfNil(plan.CompletedAt, now)
	plan.UpdatedAt = now
	for i := range plan.Items {
		if plan.Items[i].Kind == brain.ItemKindTask {
			plan.Items[i].Status = brain.StatusCompleted
		} else {
			plan.Items[i].Status = brain.StatusDeferred
		}
	}
	if err := flowdb.SaveBrainPlan(s.cfg.DB, plan); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, plan)
}

type brainDependency struct {
	ChildItemID  string
	ParentRef    string
	ParentItemID string
	ParentSlug   string
}

func (s *Server) resolveBrainPlanExecution(plan *brain.Plan) (map[string]string, []brainDependency, map[string][]string, error) {
	taskSlugByItemID := make(map[string]string)
	refToItemID := make(map[string]string)
	internalParents := make(map[string][]string)
	internalDeps := make([]brainDependency, 0)

	for i := range plan.Items {
		item := &plan.Items[i]
		if item.Kind != brain.ItemKindTask {
			continue
		}
		if item.Task == nil {
			return nil, nil, nil, fmt.Errorf("item %q is missing task payload", item.ID)
		}
		slug := strings.TrimSpace(item.Task.Slug)
		if slug != "" {
			if err := validateSlug(slug); err != nil {
				return nil, nil, nil, fmt.Errorf("item %q task slug: %w", item.ID, err)
			}
		} else {
			slug = brain.TaskSlug(plan.ID, item.ID, firstNonEmpty(item.Task.Name, item.Title))
		}
		if prev, ok := refToItemID[slug]; ok && prev != item.ID {
			return nil, nil, nil, fmt.Errorf("duplicate task slug %q", slug)
		}
		taskSlugByItemID[item.ID] = slug
		refToItemID[item.ID] = item.ID
		refToItemID[slug] = item.ID
		if item.Task.Slug != "" {
			refToItemID[item.Task.Slug] = item.ID
		}
		item.TaskSlug = slug
	}

	for i := range plan.Items {
		item := &plan.Items[i]
		if item.Kind != brain.ItemKindTask || item.Task == nil {
			continue
		}
		if ref := strings.TrimSpace(item.Task.SubtaskOf); ref != "" {
			parentSlug, parentItemID, internal, err := s.resolveBrainPlanRef(refToItemID, taskSlugByItemID, ref)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("item %q subtask_of %q: %w", item.ID, ref, err)
			}
			if internal {
				if parentItemID == item.ID {
					return nil, nil, nil, fmt.Errorf("item %q cannot be a subtask of itself", item.ID)
				}
				internalParents[item.ID] = append(internalParents[item.ID], parentItemID)
			}
			item.Task.SubtaskOf = parentSlug
		}
		for _, ref := range item.Task.DependsOn {
			parentSlug, parentItemID, internal, err := s.resolveBrainPlanRef(refToItemID, taskSlugByItemID, ref)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("item %q depends_on %q: %w", item.ID, ref, err)
			}
			if internal {
				if parentItemID == item.ID {
					return nil, nil, nil, fmt.Errorf("item %q cannot depend on itself", item.ID)
				}
				internalDeps = append(internalDeps, brainDependency{
					ChildItemID:  item.ID,
					ParentRef:    ref,
					ParentItemID: parentItemID,
					ParentSlug:   parentSlug,
				})
			}
		}
	}

	if err := validateAcyclicGraph(internalDeps); err != nil {
		return nil, nil, nil, err
	}
	if err := validateAcyclicGraphForParents(internalParents); err != nil {
		return nil, nil, nil, err
	}
	return taskSlugByItemID, internalDeps, internalParents, nil
}

func (s *Server) resolveBrainPlanRef(refToItemID, taskSlugByItemID map[string]string, ref string) (string, string, bool, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", false, errors.New("reference is empty")
	}
	if itemID, ok := refToItemID[ref]; ok {
		return taskSlugByItemID[itemID], itemID, true, nil
	}
	if err := validateSlug(ref); err != nil {
		return "", "", false, err
	}
	task, err := flowdb.GetTask(s.cfg.DB, ref)
	if err != nil {
		return "", "", false, fmt.Errorf("task %q not found", ref)
	}
	if task.DeletedAt.Valid {
		return "", "", false, fmt.Errorf("task %q is deleted", ref)
	}
	return ref, "", false, nil
}

func validateAcyclicGraph(deps []brainDependency) error {
	if len(deps) == 0 {
		return nil
	}
	kids := make(map[string][]string)
	for _, dep := range deps {
		kids[dep.ChildItemID] = append(kids[dep.ChildItemID], dep.ParentItemID)
	}
	return detectCycle(kids)
}

func validateAcyclicGraphForParents(edges map[string][]string) error {
	if len(edges) == 0 {
		return nil
	}
	return detectCycle(edges)
}

func detectCycle(graph map[string][]string) error {
	visiting := make(map[string]bool)
	visited := make(map[string]bool)
	var visit func(string) bool
	visit = func(node string) bool {
		if visited[node] {
			return false
		}
		if visiting[node] {
			return true
		}
		visiting[node] = true
		for _, next := range graph[node] {
			if visit(next) {
				return true
			}
		}
		visiting[node] = false
		visited[node] = true
		return false
	}
	for node := range graph {
		if visit(node) {
			return fmt.Errorf("plan contains a cycle involving %q", node)
		}
	}
	return nil
}

func (s *Server) materializeBrainPlanTasks(plan *brain.Plan, taskSlugByItemID map[string]string, internalParents map[string][]string, now string) error {
	for i := range plan.Items {
		item := &plan.Items[i]
		if item.Kind != brain.ItemKindTask {
			item.Status = brain.StatusDeferred
			continue
		}
		if item.Task == nil {
			return fmt.Errorf("item %q is missing task payload", item.ID)
		}
		slug := taskSlugByItemID[item.ID]
		if slug == "" {
			return fmt.Errorf("item %q task slug missing", item.ID)
		}
		if err := s.ensureBrainTask(plan, item, slug, now); err != nil {
			return err
		}
		item.Status = brain.StatusCompleted
		item.TaskSlug = slug
	}
	// Apply hierarchy edges after all task rows exist. The refs were resolved
	// during validation, so the stored subtask_of value now carries the final
	// task slug whether it points at another plan item or an existing task.
	for i := range plan.Items {
		item := &plan.Items[i]
		if item.Kind != brain.ItemKindTask || item.Task == nil {
			continue
		}
		ref := strings.TrimSpace(item.Task.SubtaskOf)
		if ref == "" {
			continue
		}
		if err := flowdb.SetTaskHierarchyParent(s.cfg.DB, item.TaskSlug, ref); err != nil {
			return fmt.Errorf("set parent for %s: %w", item.TaskSlug, err)
		}
	}
	return nil
}

func (s *Server) ensureBrainTask(plan *brain.Plan, item *brain.Item, slug, now string) error {
	if task, err := flowdb.GetTask(s.cfg.DB, slug); err == nil && task != nil {
		if err := s.writeBrainTaskBriefIfNeeded(plan, item, slug); err != nil {
			return err
		}
		if err := s.applyBrainTaskMetadata(item, slug, now, false); err != nil {
			return err
		}
		return nil
	}
	if err := s.applyBrainTaskMetadata(item, slug, now, true); err != nil {
		return err
	}
	if err := s.writeBrainTaskBriefIfNeeded(plan, item, slug); err != nil {
		return err
	}
	return nil
}

func (s *Server) applyBrainTaskMetadata(item *brain.Item, slug, now string, create bool) error {
	provider := strings.ToLower(strings.TrimSpace(item.Provider))
	if provider != "claude" && provider != "codex" {
		return fmt.Errorf("item %q has invalid provider %q", item.ID, item.Provider)
	}
	permissionMode, err := flowdb.NormalizePermissionMode(item.PermissionMode)
	if err != nil {
		return fmt.Errorf("item %q permission mode: %w", item.ID, err)
	}
	model := flowdb.NormalizeModel(item.Model)
	risk := strings.ToLower(strings.TrimSpace(item.Risk))
	if risk == "" {
		risk = "medium"
	}
	priority := risk
	if priority != "low" && priority != "medium" && priority != "high" {
		priority = "medium"
	}
	projectSlug := item.Task.Project
	var projectWorkDir string
	if projectSlug != "" {
		project, err := flowdb.GetProject(s.cfg.DB, projectSlug)
		if err != nil {
			return fmt.Errorf("project %q: %w", projectSlug, err)
		}
		projectWorkDir = project.WorkDir
	}
	workDir, err := s.resolveBrainTaskWorkDir(item, slug, projectWorkDir)
	if err != nil {
		return err
	}

	if create {
		_, err = s.cfg.DB.Exec(`
			INSERT INTO tasks (
				slug, name, project_slug, status, kind, priority, work_dir,
				permission_mode, model, session_provider, status_changed_at,
				created_at, updated_at
			) VALUES (?, ?, ?, 'backlog', 'regular', ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			slug, item.Task.Name, flowdb.NullIfEmpty(projectSlug), priority, workDir, permissionMode, flowdb.NullIfEmpty(model), provider, now, now, now,
		)
		if err != nil {
			return fmt.Errorf("insert task %s: %w", slug, err)
		}
		if err := workdirreg.Register(s.cfg.DB, workDir, "", ""); err != nil {
			return fmt.Errorf("register workdir %s: %w", workDir, err)
		}
	}

	for _, tag := range item.Task.Tags {
		if err := flowdb.AddTaskTag(s.cfg.DB, slug, tag); err != nil {
			return err
		}
	}
	// Keep the task row up to date if the plan is retried after a partial
	// materialization and the row already existed.
	_, err = s.cfg.DB.Exec(`
		UPDATE tasks SET
			project_slug = COALESCE(project_slug, ?),
			permission_mode = ?,
			model = COALESCE(?, model),
			session_provider = ?,
			work_dir = ?,
			updated_at = ?
		WHERE slug = ?
	`,
		flowdb.NullIfEmpty(projectSlug), permissionMode, flowdb.NullIfEmpty(model), provider, workDir, now, slug,
	)
	if err != nil {
		return fmt.Errorf("update task %s: %w", slug, err)
	}
	return nil
}

func (s *Server) applyBrainPlanDependencies(plan *brain.Plan, taskSlugByItemID map[string]string, deps []brainDependency) error {
	for _, dep := range deps {
		childSlug := taskSlugByItemID[dep.ChildItemID]
		if childSlug == "" {
			return fmt.Errorf("dependency child %q missing slug", dep.ChildItemID)
		}
		if err := flowdb.AddTaskDependency(s.cfg.DB, childSlug, dep.ParentSlug); err != nil {
			return fmt.Errorf("add dependency %s -> %s: %w", childSlug, dep.ParentSlug, err)
		}
	}
	_ = plan
	return nil
}

func (s *Server) resolveBrainTaskWorkDir(item *brain.Item, slug, projectWorkDir string) (string, error) {
	if item == nil || item.Task == nil {
		return "", errors.New("task payload is missing")
	}
	if path := strings.TrimSpace(item.Task.WorkDir); path != "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve work dir %q: %w", path, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return "", fmt.Errorf("work dir %s: %w", abs, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("work dir %s is not a directory", abs)
		}
		return abs, nil
	}
	if projectWorkDir != "" {
		return projectWorkDir, nil
	}
	root, err := filepath.Abs(filepath.Join(s.cfg.FlowRoot, "tasks", slug, "workspace"))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create workspace %s: %w", root, err)
	}
	return root, nil
}

func (s *Server) writeBrainTaskBriefIfNeeded(plan *brain.Plan, item *brain.Item, slug string) error {
	path := filepath.Join(s.cfg.FlowRoot, "tasks", slug, "brief.md")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := buildBrainTaskBrief(plan, item, slug)
	return os.WriteFile(path, []byte(content), 0o644)
}

func buildBrainTaskBrief(plan *brain.Plan, item *brain.Item, slug string) string {
	var b strings.Builder
	name := strings.TrimSpace(item.Task.Name)
	if name == "" {
		name = strings.TrimSpace(item.Title)
	}
	if name == "" {
		name = slug
	}
	fmt.Fprintf(&b, "# %s\n\n", name)
	fmt.Fprintf(&b, "This task was materialized from Brain plan `%s`.\n\n", plan.ID)
	if q := strings.TrimSpace(plan.Query); q != "" {
		fmt.Fprintf(&b, "## Query\n\n%s\n\n", q)
	}
	if s := strings.TrimSpace(plan.Summary); s != "" {
		fmt.Fprintf(&b, "## Summary\n\n%s\n\n", s)
	}
	fmt.Fprintf(&b, "## Metadata\n\n")
	fmt.Fprintf(&b, "- Provider: %s\n", item.Provider)
	if item.Model != "" {
		fmt.Fprintf(&b, "- Model: %s\n", item.Model)
	}
	if item.Tier != "" {
		fmt.Fprintf(&b, "- Tier: %s\n", item.Tier)
	}
	fmt.Fprintf(&b, "- Permission mode: %s\n", item.PermissionMode)
	fmt.Fprintf(&b, "- Risk: %s\n", item.Risk)
	if item.Task.Project != "" {
		fmt.Fprintf(&b, "- Project: %s\n", item.Task.Project)
	}
	if item.Task.WorkDir != "" {
		fmt.Fprintf(&b, "- Work dir: %s\n", item.Task.WorkDir)
	}
	if item.Task.SubtaskOf != "" {
		fmt.Fprintf(&b, "- Subtask of: %s\n", item.Task.SubtaskOf)
	}
	if len(item.Task.DependsOn) > 0 {
		fmt.Fprintf(&b, "- Depends on: %s\n", strings.Join(item.Task.DependsOn, ", "))
	}
	if bp := strings.TrimSpace(item.Task.BranchPolicy); bp != "" {
		fmt.Fprintf(&b, "\n## Branch policy\n\n%s\n", bp)
	}
	if len(item.Task.AcceptanceCriteria) > 0 {
		b.WriteString("\n## Acceptance criteria\n\n")
		for _, ac := range item.Task.AcceptanceCriteria {
			fmt.Fprintf(&b, "- %s\n", ac)
		}
	}
	refs := append([]brain.SourceRef{}, plan.SourceRefs...)
	refs = append(refs, item.SourceRefs...)
	if len(refs) > 0 {
		b.WriteString("\n## Sources\n\n")
		for _, ref := range refs {
			parts := []string{}
			if ref.Kind != "" {
				parts = append(parts, ref.Kind)
			}
			if ref.Title != "" {
				parts = append(parts, ref.Title)
			}
			if ref.Slug != "" {
				parts = append(parts, ref.Slug)
			}
			if ref.Path != "" {
				parts = append(parts, ref.Path)
			}
			if ref.URL != "" {
				parts = append(parts, ref.URL)
			}
			line := strings.Join(parts, " — ")
			if ref.Snippet != "" {
				line += "\n\n> " + ref.Snippet
			}
			fmt.Fprintf(&b, "- %s\n", line)
		}
	}
	return b.String()
}

func strPtrIfNil(p *string, value string) *string {
	if p != nil && strings.TrimSpace(*p) != "" {
		return p
	}
	v := value
	return &v
}
