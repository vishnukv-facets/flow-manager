package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"flow/internal/flowdb"
)

const askFlowMaxCitations = 12

type askFlowChangedRow struct {
	task TaskView
	file FileRef
}

func (s *Server) handleAskFlow(w http.ResponseWriter, r *http.Request) {
	var req AskFlowRequest
	switch r.Method {
	case http.MethodPost:
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
			writeError(w, err, http.StatusBadRequest)
			return
		}
	case http.MethodGet:
		req.Query = r.URL.Query().Get("q")
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		writeError(w, errors.New("query is required"), http.StatusBadRequest)
		return
	}
	resp, err := s.answerAskFlow(r.Context(), req.Query)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, resp)
}

func (s *Server) answerAskFlow(ctx context.Context, query string) (AskFlowResponse, error) {
	intent := classifyAskFlowIntent(query)
	var (
		answer    string
		citations []AskFlowCitation
		err       error
	)
	switch intent {
	case "look_now":
		answer, citations, err = s.askFlowLookNow(ctx)
	case "blockers":
		answer, citations, err = s.askFlowBlockers()
	case "stale":
		answer, citations, err = s.askFlowStale()
	case "changed":
		answer, citations, err = s.askFlowChanged(ctx)
	case "draft_replies":
		answer, citations, err = s.askFlowDraftReplies(ctx)
	case "related":
		answer, citations, err = s.askFlowSearch(query, "related")
	default:
		answer, citations, err = s.askFlowSearch(query, "search")
	}
	if err != nil {
		return AskFlowResponse{}, err
	}
	return AskFlowResponse{
		Query:     query,
		Intent:    intent,
		Answer:    answer,
		Citations: limitAskFlowCitations(dedupeAskFlowCitations(citations), askFlowMaxCitations),
	}, nil
}

func classifyAskFlowIntent(query string) string {
	q := strings.ToLower(query)
	switch {
	case strings.Contains(q, "draft") && strings.Contains(q, "repl"):
		return "draft_replies"
	case strings.Contains(q, "blocker") || strings.Contains(q, "blocked") || strings.Contains(q, "waiting on"):
		return "blockers"
	case strings.Contains(q, "stale"):
		return "stale"
	case strings.Contains(q, "changed") || strings.Contains(q, "while i was away") || strings.Contains(q, "away"):
		return "changed"
	case strings.Contains(q, "related") || strings.Contains(q, "slack thread") || strings.Contains(q, "github thread"):
		return "related"
	case strings.Contains(q, "look at now") || strings.Contains(q, "what should") || strings.Contains(q, "triage my day") || strings.Contains(q, "work on"):
		return "look_now"
	default:
		return "search"
	}
}

func (s *Server) askFlowLookNow(ctx context.Context) (string, []AskFlowCitation, error) {
	var citations []AskFlowCitation
	var lines []string
	lines = append(lines, "Start with the freshest operator-review items, then unblock active work, then pick from high-priority backlog.")

	items, err := flowdb.ListFeedItems(s.cfg.DB, "new")
	if err != nil {
		return "", nil, err
	}
	if len(items) > 0 {
		lines = append(lines, "", "Attention:")
		for _, it := range takeFeedItems(items, 3) {
			lines = append(lines, fmt.Sprintf("- %s — %s (%.0f%% confidence)", nonempty(it.Summary, it.ThreadKey), actionLabel(it.SuggestedAction), it.Confidence*100))
			citations = append(citations, attentionCitation(ctx, s, it))
		}
	}

	tasks, err := s.askFlowTaskViews()
	if err != nil {
		return "", nil, err
	}
	waiting := filterTaskViews(tasks, func(t TaskView) bool { return t.WaitingOn != nil && *t.WaitingOn != "" && t.Status != "done" })
	stale := filterTaskViews(tasks, func(t TaskView) bool { return t.StaleDays != nil && t.Status == "in-progress" })
	inFlight := filterTaskViews(tasks, func(t TaskView) bool { return t.Status == "in-progress" && t.WaitingOn == nil })
	backlog := filterTaskViews(tasks, func(t TaskView) bool { return t.Status == "backlog" && t.Priority == "high" })

	if len(waiting) > 0 {
		lines = append(lines, "", "Blocked active work:")
		for _, task := range takeTaskViews(waiting, 3) {
			lines = append(lines, fmt.Sprintf("- %s — waiting on %s", task.Name, *task.WaitingOn))
			citations = append(citations, taskCitation(task))
		}
	}
	if len(stale) > 0 {
		lines = append(lines, "", "Stale sessions:")
		for _, task := range takeTaskViews(stale, 3) {
			lines = append(lines, fmt.Sprintf("- %s — stale for %d day(s)", task.Name, *task.StaleDays))
			citations = append(citations, taskCitation(task))
		}
	}
	if len(inFlight) > 0 {
		lines = append(lines, "", "In flight:")
		for _, task := range takeTaskViews(inFlight, 3) {
			lines = append(lines, fmt.Sprintf("- %s — %s", task.Name, task.TemporalSummary))
			citations = append(citations, taskCitation(task))
		}
	}
	if len(backlog) > 0 {
		lines = append(lines, "", "High-priority backlog:")
		for _, task := range takeTaskViews(backlog, 3) {
			lines = append(lines, fmt.Sprintf("- %s — %s", task.Name, task.TemporalSummary))
			citations = append(citations, taskCitation(task))
		}
	}
	if len(citations) == 0 {
		lines = append(lines, "", "No new Attention cards, blockers, stale sessions, or high-priority backlog tasks are visible.")
	}
	return strings.Join(lines, "\n"), citations, nil
}

func (s *Server) askFlowBlockers() (string, []AskFlowCitation, error) {
	tasks, err := s.askFlowTaskViews()
	if err != nil {
		return "", nil, err
	}
	var citations []AskFlowCitation
	var lines []string
	for _, task := range tasks {
		if task.Status == "done" {
			continue
		}
		if task.WaitingOn != nil && strings.TrimSpace(*task.WaitingOn) != "" {
			lines = append(lines, fmt.Sprintf("- %s — waiting on %s", task.Name, *task.WaitingOn))
			citations = append(citations, taskCitation(task))
		}
		for _, parent := range task.Parents {
			if parent.Status == "done" || parent.Slug == task.ParentSlugValue() {
				continue
			}
			lines = append(lines, fmt.Sprintf("- %s — blocked by %s (%s)", task.Name, parent.Name, parent.Status))
			citations = append(citations, taskCitation(task), taskSummaryCitation(parent))
		}
	}
	if len(lines) == 0 {
		return "No open blockers found in active tasks. I checked waiting notes and task dependencies.", nil, nil
	}
	return "Open blockers:\n" + strings.Join(lines, "\n"), citations, nil
}

func (s *Server) askFlowStale() (string, []AskFlowCitation, error) {
	tasks, err := s.askFlowTaskViews()
	if err != nil {
		return "", nil, err
	}
	var citations []AskFlowCitation
	var lines []string
	for _, task := range tasks {
		if task.StaleDays == nil {
			continue
		}
		waiting := ""
		if task.WaitingOn != nil && *task.WaitingOn != "" {
			waiting = " and is waiting on " + *task.WaitingOn
		}
		lines = append(lines, fmt.Sprintf("- %s — stale for %d day(s)%s", task.Name, *task.StaleDays, waiting))
		citations = append(citations, taskCitation(task))
	}
	if len(lines) == 0 {
		return "No stale in-progress tasks found.", nil, nil
	}
	return "Stale work:\n" + strings.Join(lines, "\n"), citations, nil
}

func (s *Server) askFlowChanged(ctx context.Context) (string, []AskFlowCitation, error) {
	tasks, err := s.askFlowTaskViews()
	if err != nil {
		return "", nil, err
	}
	var rows []askFlowChangedRow
	for _, task := range tasks {
		for _, file := range task.Updates {
			rows = append(rows, askFlowChangedRow{task: task, file: file})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].file.MTime > rows[j].file.MTime })
	var citations []AskFlowCitation
	var lines []string
	for _, row := range takeChanged(rows, 6) {
		lines = append(lines, fmt.Sprintf("- %s — %s", row.task.Name, row.file.Filename))
		citations = append(citations, updateCitation(row.task, row.file))
	}
	items, err := flowdb.ListFeedItems(s.cfg.DB, "new")
	if err != nil {
		return "", nil, err
	}
	for _, it := range takeFeedItems(items, 3) {
		lines = append(lines, fmt.Sprintf("- Attention: %s — %s", nonempty(it.Summary, it.ThreadKey), actionLabel(it.SuggestedAction)))
		citations = append(citations, attentionCitation(ctx, s, it))
	}
	if len(lines) == 0 {
		return "No task updates or new Attention cards were found.", nil, nil
	}
	return "Recent changes I found:\n" + strings.Join(lines, "\n"), citations, nil
}

func (s *Server) askFlowDraftReplies(ctx context.Context) (string, []AskFlowCitation, error) {
	items, err := flowdb.ListFeedItems(s.cfg.DB, "new")
	if err != nil {
		return "", nil, err
	}
	var citations []AskFlowCitation
	var lines []string
	for _, it := range items {
		if strings.TrimSpace(it.Draft) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s\n  Draft: %s", nonempty(it.Summary, it.ThreadKey), strings.TrimSpace(it.Draft)))
		citations = append(citations, attentionCitation(ctx, s, it))
	}
	if len(lines) == 0 {
		return "No new Attention cards with draft replies found.", nil, nil
	}
	return "Draft replies from new Attention cards:\n" + strings.Join(lines, "\n"), citations, nil
}

func (s *Server) askFlowSearch(query, intent string) (string, []AskFlowCitation, error) {
	terms := askFlowSearchTerms(query)
	if terms == "" {
		terms = query
	}
	scopes := flowdb.DefaultSearchScopes()
	s.syncSearchThrottled(scopes)
	results, err := flowdb.SearchDocs(s.cfg.DB, terms, scopes, 8)
	if err != nil {
		return "", nil, err
	}
	results = append(results, s.askFlowNameMatches(terms, 8-len(results))...)
	if len(results) == 0 {
		return fmt.Sprintf("I could not find Flow records matching %q.", terms), nil, nil
	}
	var citations []AskFlowCitation
	var lines []string
	prefix := "Related Flow records"
	if intent == "search" {
		prefix = "Grounded matches"
	}
	for _, result := range results {
		c := searchCitation(result)
		lines = append(lines, fmt.Sprintf("- %s — %s", c.Title, nonempty(result.Snippet, result.Scope)))
		citations = append(citations, c)
	}
	return prefix + ":\n" + strings.Join(lines, "\n"), citations, nil
}

func (s *Server) askFlowNameMatches(q string, limit int) []flowdb.SearchResult {
	if limit <= 0 {
		return nil
	}
	like := "%" + q + "%"
	rows, err := s.cfg.DB.Query(
		`SELECT slug, name, status, updated_at FROM tasks
		 WHERE name LIKE ? AND archived_at IS NULL AND deleted_at IS NULL
		 ORDER BY updated_at DESC LIMIT ?`,
		like, limit,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []flowdb.SearchResult
	for rows.Next() {
		var slug, name, status, updated string
		if err := rows.Scan(&slug, &name, &status, &updated); err != nil {
			return out
		}
		out = append(out, flowdb.SearchResult{
			Type:       "task_name",
			Scope:      "name",
			EntityType: "task",
			Slug:       slug,
			Name:       name,
			Snippet:    status,
			UpdatedAt:  updated,
		})
	}
	return out
}

func (s *Server) askFlowTaskViews() ([]TaskView, error) {
	tasks, err := flowdb.ListTasks(s.cfg.DB, flowdb.TaskFilter{IncludeArchived: false, IncludeDeleted: false, Kind: ""})
	if err != nil {
		return nil, err
	}
	return buildTaskViewsWithLive(s.cfg.DB, s.cfg.FlowRoot, tasks, map[string]bool{})
}

func askFlowSearchTerms(query string) string {
	stop := map[string]bool{
		"a": true, "about": true, "am": true, "are": true, "flow": true, "for": true, "from": true,
		"i": true, "is": true, "me": true, "my": true, "of": true, "please": true, "show": true,
		"task": true, "tasks": true, "the": true, "this": true, "thread": true, "to": true,
		"what": true, "which": true, "who": true, "why": true, "with": true,
		"related": true, "slack": true, "github": true,
	}
	var terms []string
	for _, tok := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-')
	}) {
		tok = strings.Trim(tok, "-_")
		if tok == "" || stop[tok] {
			continue
		}
		terms = append(terms, tok)
	}
	return strings.Join(terms, " ")
}

func filterTaskViews(tasks []TaskView, keep func(TaskView) bool) []TaskView {
	var out []TaskView
	for _, task := range tasks {
		if keep(task) {
			out = append(out, task)
		}
	}
	return out
}

func takeTaskViews(in []TaskView, n int) []TaskView {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

func takeFeedItems(in []flowdb.FeedItem, n int) []flowdb.FeedItem {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

func takeChanged(in []askFlowChangedRow, n int) []askFlowChangedRow {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

func taskCitation(task TaskView) AskFlowCitation {
	return AskFlowCitation{
		Type:  "task",
		Slug:  task.Slug,
		Title: task.Name,
		URL:   "/task/" + task.Slug,
	}
}

func taskSummaryCitation(task TaskSummary) AskFlowCitation {
	return AskFlowCitation{
		Type:  "task",
		Slug:  task.Slug,
		Title: task.Name,
		URL:   "/task/" + task.Slug,
	}
}

func updateCitation(task TaskView, file FileRef) AskFlowCitation {
	return AskFlowCitation{
		Type:       "update",
		Slug:       task.Slug,
		Title:      task.Name + " update " + file.Filename,
		URL:        "/task/" + task.Slug,
		SourcePath: file.Path,
	}
}

func attentionCitation(ctx context.Context, s *Server, it flowdb.FeedItem) AskFlowCitation {
	title := nonempty(it.Summary, it.ThreadKey)
	if s != nil && s.nameResolver != nil {
		title = s.nameResolver.CleanText(ctx, title)
	}
	return AskFlowCitation{
		Type:    "attention",
		ID:      it.ID,
		Title:   title,
		URL:     "/attention",
		Snippet: nonempty(it.Reason, actionLabel(it.SuggestedAction)),
	}
}

func searchCitation(result flowdb.SearchResult) AskFlowCitation {
	typ := result.Scope
	if result.EntityType != "" && result.Scope == string(flowdb.SearchScopeBrief) {
		typ = result.EntityType
	}
	if result.EntityType == "memory" {
		typ = "memory"
	}
	if typ == "" {
		typ = result.EntityType
	}
	return AskFlowCitation{
		Type:       typ,
		Slug:       result.Slug,
		Title:      result.Name,
		URL:        searchResultURL(result.EntityType, result.Slug),
		SourcePath: result.SourcePath,
		Snippet:    result.Snippet,
	}
}

func dedupeAskFlowCitations(in []AskFlowCitation) []AskFlowCitation {
	seen := map[string]bool{}
	var out []AskFlowCitation
	for _, c := range in {
		key := c.Type + "\x00" + c.ID + "\x00" + c.Slug + "\x00" + c.SourcePath
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}

func limitAskFlowCitations(in []AskFlowCitation, n int) []AskFlowCitation {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

func actionLabel(action string) string {
	switch strings.TrimSpace(strings.ToLower(action)) {
	case "make_task", "make-task":
		return "make a task"
	case "make_task_start", "make-task-start":
		return "make and start a task"
	case "send_reply", "send-reply", "reply":
		return "draft/send a reply"
	case "forward":
		return "forward to an existing task"
	case "confirm_handoff", "confirm-handoff":
		return "confirm task handoff"
	case "dismiss":
		return "dismiss"
	default:
		return nonempty(action, "review")
	}
}

func nonempty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (t TaskView) ParentSlugValue() string {
	if t.ParentSlug == nil {
		return ""
	}
	return *t.ParentSlug
}
