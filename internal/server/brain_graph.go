package server

import (
	"database/sql"
	"net/http"
	"sort"
	"strings"
	"time"

	"flow/internal/flowdb"
)

func parseBrainGraphFilters(r *http.Request) BrainGraphFilters {
	q := r.URL.Query()
	expand := map[string]bool{}
	for _, raw := range strings.Split(q.Get("expand"), ",") {
		raw = strings.TrimSpace(raw)
		if raw != "" {
			expand[raw] = true
		}
	}
	return BrainGraphFilters{
		Project:     strings.TrimSpace(q.Get("project")),
		Owner:       strings.TrimSpace(q.Get("owner")),
		Status:      strings.TrimSpace(q.Get("status")),
		IncludeDone: q.Get("include_done") == "1" || q.Get("include_done") == "true",
		Expand:      expand,
		Query:       strings.TrimSpace(q.Get("q")),
	}
}

func BuildBrainGraph(db *sql.DB, root string, filters BrainGraphFilters, now time.Time) (BrainGraphView, error) {
	view := BrainGraphView{
		GeneratedAt: now.Format(time.RFC3339),
		Freshness:   "fresh",
		Controller: BrainGraphController{
			Mode:        "global_brain",
			DisplayName: "Global Brain",
			Status:      "ready",
		},
		Policy: BrainGraphPolicyView{
			FullAuto:         true,
			RiskyWhitelist:   []string{},
			ApprovalRequired: []string{"merge", "deploy", "force_push", "destructive_shell", "delete_branch", "outbound_reply"},
		},
		Owners: []BrainGraphOwnerView{{
			ID:     "owner:unowned",
			Slug:   "unowned",
			Name:   "Unowned",
			Status: "active",
		}},
		Nodes:           []BrainGraphNode{},
		Edges:           []BrainGraphEdge{},
		SelectedActions: defaultBrainGraphActions(),
		Warnings:        []BrainGraphWarning{},
	}
	tasks, err := flowdb.ListTasks(db, flowdb.TaskFilter{
		Project: filters.Project,
		Status:  filters.Status,
		Kind:    "",
	})
	if err != nil {
		return view, err
	}
	tasks = filterBrainGraphTasks(tasks, filters)
	slugs := taskSlugs(tasks)
	tagsByTask, err := flowdb.GetTaskTagsBatch(db, slugs)
	if err != nil {
		return view, err
	}
	owners, err := flowdb.ListOwners(db, flowdb.OwnerFilter{})
	if err != nil {
		return view, err
	}
	ownerBySlug, taskOwners, warnings := resolveBrainGraphOwners(tasks, owners, tagsByTask)
	appendOwnerBoundaries(&view, owners)

	visible := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		ownerSlug := taskOwners[task.Slug]
		if ownerSlug == "" {
			ownerSlug = "unowned"
		}
		if filters.Owner != "" && filters.Owner != ownerSlug {
			continue
		}
		node := brainGraphTaskNode(task, ownerSlug, tagsByTask[task.Slug], filters)
		view.Nodes = append(view.Nodes, node)
		visible[task.Slug] = true
		view.Counts.TotalTasks++
		if task.Status == "done" {
			view.Counts.Done++
		}
		if task.Status == "in-progress" || nullStringValue(task.AutoRunStatus) == "running" {
			view.Counts.Running++
		}
	}
	view.Warnings = append(view.Warnings, visibleBrainGraphWarnings(warnings, visible)...)
	for _, node := range view.Nodes {
		ownerSlug := node.OwnerSlug
		if ownerSlug == "" {
			ownerSlug = "unowned"
		}
		for i := range view.Owners {
			if view.Owners[i].Slug != ownerSlug {
				continue
			}
			view.Owners[i].TaskCount++
			if node.Status == "in-progress" {
				view.Owners[i].RunningCount++
			}
			break
		}
		if _, ok := ownerBySlug[ownerSlug]; !ok && ownerSlug != "unowned" {
			view.Warnings = append(view.Warnings, BrainGraphWarning{
				Code:    "missing_owner_boundary",
				Message: "task is assigned to an owner boundary that is not present: " + ownerSlug,
				NodeID:  node.ID,
			})
		}
	}
	for _, task := range tasks {
		if !visible[task.Slug] || !task.ParentSlug.Valid || strings.TrimSpace(task.ParentSlug.String) == "" {
			continue
		}
		parentSlug := strings.TrimSpace(task.ParentSlug.String)
		if !visible[parentSlug] {
			continue
		}
		view.Edges = append(view.Edges, BrainGraphEdge{
			ID:     "parent:" + parentSlug + ":" + task.Slug,
			Type:   "parent",
			Source: "task:" + parentSlug,
			Target: "task:" + task.Slug,
		})
	}
	deps, err := listBrainGraphDependencies(db)
	if err != nil {
		return view, err
	}
	for _, dep := range deps {
		if !visible[dep.parentSlug] || !visible[dep.childSlug] {
			continue
		}
		view.Edges = append(view.Edges, BrainGraphEdge{
			ID:     "depends_on:" + dep.parentSlug + ":" + dep.childSlug,
			Type:   "depends_on",
			Source: "task:" + dep.parentSlug,
			Target: "task:" + dep.childSlug,
		})
	}
	view.Counts.Owners = len(view.Owners)
	view.Counts.Warnings = len(view.Warnings)
	return view, nil
}

func visibleBrainGraphWarnings(warnings []BrainGraphWarning, visible map[string]bool) []BrainGraphWarning {
	out := make([]BrainGraphWarning, 0, len(warnings))
	for _, warning := range warnings {
		if warning.NodeID == "" {
			out = append(out, warning)
			continue
		}
		taskSlug, ok := strings.CutPrefix(warning.NodeID, "task:")
		if ok && visible[taskSlug] {
			out = append(out, warning)
		}
	}
	return out
}

func filterBrainGraphTasks(tasks []*flowdb.Task, filters BrainGraphFilters) []*flowdb.Task {
	query := strings.ToLower(strings.TrimSpace(filters.Query))
	out := make([]*flowdb.Task, 0, len(tasks))
	for _, task := range tasks {
		if !filters.IncludeDone && task.Status == "done" {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(task.Slug), query) && !strings.Contains(strings.ToLower(task.Name), query) {
			continue
		}
		out = append(out, task)
	}
	return out
}

func taskSlugs(tasks []*flowdb.Task) []string {
	slugs := make([]string, 0, len(tasks))
	for _, task := range tasks {
		slugs = append(slugs, task.Slug)
	}
	return slugs
}

func resolveBrainGraphOwners(tasks []*flowdb.Task, owners []*flowdb.Owner, tagsByTask map[string][]string) (map[string]*flowdb.Owner, map[string]string, []BrainGraphWarning) {
	ownerBySlug := make(map[string]*flowdb.Owner, len(owners))
	for _, owner := range owners {
		ownerBySlug[owner.Slug] = owner
	}
	taskBySlug := make(map[string]*flowdb.Task, len(tasks))
	for _, task := range tasks {
		taskBySlug[task.Slug] = task
	}
	resolved := make(map[string]string, len(tasks))
	resolving := map[string]bool{}
	warnedUnknown := map[string]bool{}
	var warnings []BrainGraphWarning

	var resolve func(*flowdb.Task) string
	resolve = func(task *flowdb.Task) string {
		if owner, ok := resolved[task.Slug]; ok {
			return owner
		}
		if resolving[task.Slug] {
			resolved[task.Slug] = "unowned"
			return "unowned"
		}
		resolving[task.Slug] = true
		defer delete(resolving, task.Slug)

		ownerTags := brainGraphOwnerTags(tagsByTask[task.Slug])
		if len(ownerTags) > 0 {
			for _, ownerSlug := range ownerTags {
				if _, ok := ownerBySlug[ownerSlug]; ok {
					resolved[task.Slug] = ownerSlug
					return ownerSlug
				}
			}
			if !warnedUnknown[task.Slug] {
				warnings = append(warnings, BrainGraphWarning{
					Code:    "unknown_owner",
					Message: "task has owner tag with no matching owner: owner:" + ownerTags[0],
					NodeID:  "task:" + task.Slug,
				})
				warnedUnknown[task.Slug] = true
			}
			resolved[task.Slug] = "unowned"
			return "unowned"
		}
		if task.ParentSlug.Valid {
			parentSlug := strings.TrimSpace(task.ParentSlug.String)
			if parentSlug != "" {
				if parent, ok := taskBySlug[parentSlug]; ok {
					owner := resolve(parent)
					resolved[task.Slug] = owner
					return owner
				}
			}
		}
		resolved[task.Slug] = "unowned"
		return "unowned"
	}
	for _, task := range tasks {
		resolve(task)
	}
	return ownerBySlug, resolved, warnings
}

func brainGraphOwnerTags(tags []string) []string {
	var owners []string
	for _, tag := range tags {
		if owner, ok := strings.CutPrefix(tag, "owner:"); ok {
			owner = strings.TrimSpace(owner)
			if owner != "" {
				owners = append(owners, owner)
			}
		}
	}
	sort.Strings(owners)
	return owners
}

func appendOwnerBoundaries(view *BrainGraphView, owners []*flowdb.Owner) {
	for _, owner := range owners {
		view.Owners = append(view.Owners, BrainGraphOwnerView{
			ID:     "owner:" + owner.Slug,
			Slug:   owner.Slug,
			Name:   owner.Name,
			Status: owner.Status,
		})
	}
}

func brainGraphTaskNode(task *flowdb.Task, ownerSlug string, tags []string, filters BrainGraphFilters) BrainGraphNode {
	nodeID := "task:" + task.Slug
	return BrainGraphNode{
		ID:             nodeID,
		Type:           "task",
		OwnerSlug:      ownerSlug,
		TaskSlug:       task.Slug,
		ParentTaskSlug: nullStringValue(task.ParentSlug),
		Label:          task.Name,
		Status:         task.Status,
		Priority:       task.Priority,
		Provider:       task.SessionProvider,
		Harness:        task.Harness,
		PermissionMode: task.PermissionMode,
		Model:          nullStringValue(task.Model),
		Summary:        brainGraphTaskSummary(task),
		Expanded:       filters.Expand[nodeID] || filters.Expand[task.Slug],
		Ref: &BrainGraphRef{
			Kind: "task",
			ID:   task.Slug,
		},
		Badges:  append([]string(nil), tags...),
		Actions: []string{"open_session", "send_event", "seed"},
		Metadata: map[string]string{
			"kind": task.Kind,
		},
	}
}

func brainGraphTaskSummary(task *flowdb.Task) string {
	var parts []string
	if task.ProjectSlug.Valid && strings.TrimSpace(task.ProjectSlug.String) != "" {
		parts = append(parts, "project:"+strings.TrimSpace(task.ProjectSlug.String))
	}
	if task.WaitingOn.Valid && strings.TrimSpace(task.WaitingOn.String) != "" {
		parts = append(parts, "waiting:"+strings.TrimSpace(task.WaitingOn.String))
	}
	if task.AutoRunStatus.Valid && strings.TrimSpace(task.AutoRunStatus.String) != "" {
		parts = append(parts, "auto:"+strings.TrimSpace(task.AutoRunStatus.String))
	}
	return strings.Join(parts, " ")
}

type brainGraphDependency struct {
	childSlug  string
	parentSlug string
}

func listBrainGraphDependencies(db *sql.DB) ([]brainGraphDependency, error) {
	rows, err := db.Query(`
		SELECT child_slug, parent_slug
		FROM task_dependencies
		ORDER BY parent_slug, child_slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []brainGraphDependency
	for rows.Next() {
		var dep brainGraphDependency
		if err := rows.Scan(&dep.childSlug, &dep.parentSlug); err != nil {
			return nil, err
		}
		out = append(out, dep)
	}
	return out, rows.Err()
}

func defaultBrainGraphActions() []BrainGraphActionSpec {
	return []BrainGraphActionSpec{
		{Key: "open_session", Label: "Open session", Enabled: true},
		{Key: "send_event", Label: "Send event", Enabled: true},
		{Key: "seed", Label: "Seed input", Enabled: true},
		{Key: "retry", Label: "Retry", Enabled: true},
		{Key: "pause", Label: "Pause", Enabled: true},
		{Key: "approve", Label: "Approve", Risky: true, Enabled: true},
	}
}

func (s *Server) handleBrainGraph(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	view, err := BuildBrainGraph(s.cfg.DB, s.cfg.FlowRoot, parseBrainGraphFilters(r), time.Now())
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, view)
}
