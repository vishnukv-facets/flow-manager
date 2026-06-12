package server

import (
	"database/sql"
	"net/http"
	"strings"
	"time"
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
	view.Counts.Owners = len(view.Owners)
	return view, nil
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
