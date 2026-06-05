package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"flow/internal/flowdb"
	"flow/internal/monitor"
	"flow/internal/steering"
)

func attentionTestServer(t *testing.T) (*Server, *sql.DB) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("FLOW_ROOT", root)
	t.Setenv("HOME", root)
	db, err := flowdb.OpenDB(filepath.Join(root, "flow.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &Server{cfg: Config{DB: db, FlowRoot: root}}, db
}

func seedFeedItem(t *testing.T, db *sql.DB, id, status string) {
	t.Helper()
	if _, err := flowdb.UpsertFeedItem(db, flowdb.FeedItem{
		ID: id, Source: "slack", ThreadKey: "C1:" + id, Summary: "s-" + id,
		SuggestedAction: "make_task", Confidence: 0.8, Status: status, CreatedAt: "2026-06-05T10:00:00Z",
	}); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func TestHandleAttention(t *testing.T) {
	s, db := attentionTestServer(t)
	seedFeedItem(t, db, "a1", "new")
	seedFeedItem(t, db, "a2", "dismissed")

	req := httptest.NewRequest(http.MethodGet, "/api/attention?status=new", nil)
	rec := httptest.NewRecorder()
	s.handleAttention(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var views []AttentionItemView
	if err := json.Unmarshal(rec.Body.Bytes(), &views); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(views) != 1 || views[0].ID != "a1" || views[0].SuggestedAction != "make_task" {
		t.Errorf("views = %+v, want only a1", views)
	}
}

func TestAttentionActDismiss(t *testing.T) {
	s, db := attentionTestServer(t)
	seedFeedItem(t, db, "d1", "new")

	resp, status := s.runAction(actionRequest{Kind: "attention-act", Target: "d1", AttentionAction: "dismiss"})
	if status != 200 || !resp.OK {
		t.Fatalf("runAction = (%+v, %d), want OK 200", resp, status)
	}
	if items, _ := flowdb.ListFeedItems(db, "dismissed"); len(items) != 1 {
		t.Errorf("item should be dismissed, got %d dismissed", len(items))
	}
}

func TestAttentionActMakeTaskStart(t *testing.T) {
	s, db := attentionTestServer(t)
	seedFeedItem(t, db, "ms1", "new")

	// Stub the spawn + session-open seams so the test stays hermetic (no real
	// `flow spawn`, no PTY). The make-task seam marks the row acted+linked.
	oldMake, oldStart := attentionMakeTask, attentionStartSession
	var startedSlug string
	attentionMakeTask = func(srv *Server, item flowdb.FeedItem) error {
		return flowdb.SetFeedItemActed(srv.cfg.DB, item.ID, steering.FeedTaskSlug(item), "2026-06-05T11:00:00Z")
	}
	attentionStartSession = func(_ *Server, slug string) error { startedSlug = slug; return nil }
	t.Cleanup(func() { attentionMakeTask, attentionStartSession = oldMake, oldStart })

	resp, status := s.runAction(actionRequest{Kind: "attention-act", Target: "ms1", AttentionAction: "make-task-start"})
	if status != 200 || !resp.OK {
		t.Fatalf("runAction = (%+v, %d), want OK 200", resp, status)
	}
	item, _ := flowdb.GetFeedItem(db, "ms1")
	wantSlug := steering.FeedTaskSlug(item)
	if startedSlug != wantSlug {
		t.Errorf("started session for %q, want %q", startedSlug, wantSlug)
	}
	if item.Status != "acted" || item.LinkedTask != wantSlug {
		t.Errorf("feed row = status %q linked %q, want acted/%s", item.Status, item.LinkedTask, wantSlug)
	}
	// Underscore alias is also recognized.
	seedFeedItem(t, db, "ms2", "new")
	if _, status := s.runAction(actionRequest{Kind: "attention-act", Target: "ms2", AttentionAction: "make_task_start"}); status != 200 {
		t.Errorf("make_task_start alias → %d, want 200", status)
	}
}

func TestAttentionActMakeTaskStartOpenBestEffort(t *testing.T) {
	s, db := attentionTestServer(t)
	seedFeedItem(t, db, "be1", "new")

	oldMake, oldStart := attentionMakeTask, attentionStartSession
	attentionMakeTask = func(srv *Server, item flowdb.FeedItem) error {
		return flowdb.SetFeedItemActed(srv.cfg.DB, item.ID, steering.FeedTaskSlug(item), "2026-06-05T11:00:00Z")
	}
	attentionStartSession = func(_ *Server, _ string) error { return errors.New("pty down") }
	t.Cleanup(func() { attentionMakeTask, attentionStartSession = oldMake, oldStart })

	// Task creation succeeded; the open is best-effort, so the action still
	// reports OK (200) even though the session couldn't be auto-opened.
	resp, status := s.runAction(actionRequest{Kind: "attention-act", Target: "be1", AttentionAction: "make-task-start"})
	if status != 200 || !resp.OK {
		t.Fatalf("best-effort open: runAction = (%+v, %d), want OK 200", resp, status)
	}
	if item, _ := flowdb.GetFeedItem(db, "be1"); item.Status != "acted" {
		t.Errorf("feed row should still be acted, got %q", item.Status)
	}
}

func TestAttentionActErrors(t *testing.T) {
	s, db := attentionTestServer(t)
	seedFeedItem(t, db, "e1", "new")

	if _, status := s.runAction(actionRequest{Kind: "attention-act", AttentionAction: "dismiss"}); status != 400 {
		t.Errorf("missing target → %d, want 400", status)
	}
	if _, status := s.runAction(actionRequest{Kind: "attention-act", Target: "missing", AttentionAction: "dismiss"}); status != 404 {
		t.Errorf("missing item → %d, want 404", status)
	}
	if _, status := s.runAction(actionRequest{Kind: "attention-act", Target: "e1", AttentionAction: "frobnicate"}); status != 400 {
		t.Errorf("unknown action → %d, want 400", status)
	}
}

func TestAttentionItemView(t *testing.T) {
	s, _ := attentionTestServer(t) // nil nameResolver
	v := s.attentionItemView(context.Background(), flowdb.FeedItem{ID: "x", Source: "slack", ThreadKey: "C1:1.1", Summary: "hi <@U1>", SuggestedAction: "reply", Confidence: 0.5, Status: "new", CreatedAt: "2026-06-05T10:00:00Z"})
	if v.ID != "x" || v.Source != "slack" || v.SuggestedAction != "reply" || v.Confidence != 0.5 {
		t.Errorf("view = %+v", v)
	}
	// With a nil resolver the text passes through unchanged (nil-safe).
	if v.Summary != "hi <@U1>" {
		t.Errorf("Summary with nil resolver = %q, want unchanged", v.Summary)
	}
}

func TestHandleSlackChannels(t *testing.T) {
	s, _ := attentionTestServer(t)
	old := listSlackChannelsFn
	listSlackChannelsFn = func(_ context.Context) ([]monitor.SlackChannelInfo, error) {
		return []monitor.SlackChannelInfo{{ID: "C1", Name: "general", IsMember: true}}, nil
	}
	t.Cleanup(func() { listSlackChannelsFn = old })

	req := httptest.NewRequest(http.MethodGet, "/api/slack/channels", nil)
	rec := httptest.NewRecorder()
	s.handleSlackChannels(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var chans []monitor.SlackChannelInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &chans); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(chans) != 1 || chans[0].ID != "C1" {
		t.Errorf("chans = %+v", chans)
	}
}

func TestHandleSlackChannelsError(t *testing.T) {
	s, _ := attentionTestServer(t)
	old := listSlackChannelsFn
	listSlackChannelsFn = func(_ context.Context) ([]monitor.SlackChannelInfo, error) {
		return nil, errors.New("slack down")
	}
	t.Cleanup(func() { listSlackChannelsFn = old })

	req := httptest.NewRequest(http.MethodGet, "/api/slack/channels", nil)
	rec := httptest.NewRecorder()
	s.handleSlackChannels(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func seedTrace(t *testing.T, db *sql.DB, id, disposition, stage, createdAt string) {
	t.Helper()
	if err := flowdb.InsertSteeringTrace(db, flowdb.SteeringTrace{
		ID: id, CreatedAt: createdAt, Origin: "slack", Source: "slack",
		Channel: "C1", ChannelType: "channel", Author: "u1", ThreadKey: "C1:" + id,
		TextPreview: "preview-" + id, Disposition: disposition, StageReached: stage,
		LatencyMS: 42,
	}); err != nil {
		t.Fatalf("seedTrace %s: %v", id, err)
	}
}

func TestHandleAttentionTrace(t *testing.T) {
	s, db := attentionTestServer(t)

	// Seed 4 rows inside the since window and 1 older row.
	seedTrace(t, db, "tr1", "surfaced", "stage3", "2026-06-05T10:00:00Z")
	seedTrace(t, db, "tr2", "dropped", "stage0", "2026-06-05T10:01:00Z")
	seedTrace(t, db, "tr3", "dropped", "stage1", "2026-06-05T10:02:00Z")
	seedTrace(t, db, "tr4", "error", "stage2", "2026-06-05T10:03:00Z")
	seedTrace(t, db, "tr5", "dropped", "cache", "2026-06-01T10:00:00Z") // older — excluded

	since := "2026-06-05T00:00:00Z"

	// --- baseline: all rows in window ----------------------------------------
	req := httptest.NewRequest(http.MethodGet, "/api/attention/trace?since="+since, nil)
	rec := httptest.NewRecorder()
	s.handleAttentionTrace(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var resp AttentionTraceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Funnel should count 4 rows (tr1–tr4), not tr5.
	if resp.Funnel.Observed != 4 {
		t.Errorf("Funnel.Observed = %d, want 4", resp.Funnel.Observed)
	}
	if resp.Funnel.Surfaced != 1 {
		t.Errorf("Funnel.Surfaced = %d, want 1", resp.Funnel.Surfaced)
	}
	if resp.Funnel.DroppedStage0 != 1 {
		t.Errorf("Funnel.DroppedStage0 = %d, want 1", resp.Funnel.DroppedStage0)
	}
	if resp.Funnel.DroppedStage1 != 1 {
		t.Errorf("Funnel.DroppedStage1 = %d, want 1", resp.Funnel.DroppedStage1)
	}
	if resp.Funnel.Errors != 1 {
		t.Errorf("Funnel.Errors = %d, want 1", resp.Funnel.Errors)
	}
	if len(resp.Items) != 4 {
		t.Errorf("len(Items) = %d, want 4", len(resp.Items))
	}

	// --- disposition filter ---------------------------------------------------
	req2 := httptest.NewRequest(http.MethodGet, "/api/attention/trace?since="+since+"&disposition=dropped", nil)
	rec2 := httptest.NewRecorder()
	s.handleAttentionTrace(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("disposition filter status = %d, want 200", rec2.Code)
	}
	var resp2 AttentionTraceResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("decode resp2: %v", err)
	}
	for _, item := range resp2.Items {
		if item.Disposition != "dropped" {
			t.Errorf("disposition filter: got item with disposition=%q, want dropped", item.Disposition)
		}
	}
	if len(resp2.Items) != 2 {
		t.Errorf("disposition=dropped: len(Items) = %d, want 2", len(resp2.Items))
	}

	// --- limit filter ---------------------------------------------------------
	req3 := httptest.NewRequest(http.MethodGet, "/api/attention/trace?since="+since+"&limit=1", nil)
	rec3 := httptest.NewRecorder()
	s.handleAttentionTrace(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("limit filter status = %d, want 200", rec3.Code)
	}
	var resp3 AttentionTraceResponse
	if err := json.Unmarshal(rec3.Body.Bytes(), &resp3); err != nil {
		t.Fatalf("decode resp3: %v", err)
	}
	if len(resp3.Items) != 1 {
		t.Errorf("limit=1: len(Items) = %d, want 1", len(resp3.Items))
	}

	// --- POST should be rejected ----------------------------------------------
	req4 := httptest.NewRequest(http.MethodPost, "/api/attention/trace", nil)
	rec4 := httptest.NewRecorder()
	s.handleAttentionTrace(rec4, req4)
	if rec4.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST → %d, want 405", rec4.Code)
	}
}

func TestSteeringPermalink(t *testing.T) {
	got := steeringPermalink(flowdb.SteeringTrace{Source: "slack", TeamID: "T1", Channel: "C1", TS: "123.45"})
	want := "slack://channel?team=T1&id=C1&message=123.45"
	if got != want {
		t.Errorf("permalink = %q, want %q", got, want)
	}
	// Missing team → empty.
	if got := steeringPermalink(flowdb.SteeringTrace{Source: "slack", Channel: "C1", TS: "123.45"}); got != "" {
		t.Errorf("missing team: permalink = %q, want empty", got)
	}
	// Non-slack source → empty.
	if got := steeringPermalink(flowdb.SteeringTrace{Source: "github", TeamID: "T1", Channel: "C1", TS: "123.45"}); got != "" {
		t.Errorf("non-slack source: permalink = %q, want empty", got)
	}
}

func TestSteeringTraceViewGitHub(t *testing.T) {
	s, _ := attentionTestServer(t) // nil nameResolver
	tr := flowdb.SteeringTrace{
		Source: "github", Channel: "o/r", Author: "octocat",
		TextPreview: "please review", URL: "https://github.com/o/r/pull/5",
	}
	v := s.steeringTraceView(context.Background(), tr)
	if v.ChannelName != "o/r" {
		t.Errorf("ChannelName = %q, want %q (github repo is already human)", v.ChannelName, "o/r")
	}
	if v.AuthorName != "octocat" {
		t.Errorf("AuthorName = %q, want %q (github login is already human)", v.AuthorName, "octocat")
	}
	if v.Text != "please review" {
		t.Errorf("Text = %q, want %q", v.Text, "please review")
	}
	if v.Permalink != "https://github.com/o/r/pull/5" {
		t.Errorf("Permalink = %q, want the GitHub url", v.Permalink)
	}
}

func TestAttentionTraceResolvesNames(t *testing.T) {
	s, db := attentionTestServer(t)
	// The test server leaves nameResolver nil — exercise the graceful path.
	if s.nameResolver != nil {
		t.Fatal("expected nil nameResolver on the test server")
	}
	seedTrace(t, db, "rn1", "surfaced", "stage3", "2026-06-05T10:00:00Z")

	since := "2026-06-05T00:00:00Z"
	req := httptest.NewRequest(http.MethodGet, "/api/attention/trace?since="+since, nil)
	rec := httptest.NewRecorder()
	s.handleAttentionTrace(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var resp AttentionTraceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(resp.Items))
	}
	it := resp.Items[0]
	// With a nil resolver, names stay empty and Text falls back to the preview.
	if it.ChannelName != "" {
		t.Errorf("ChannelName = %q, want empty (nil resolver)", it.ChannelName)
	}
	if it.AuthorName != "" {
		t.Errorf("AuthorName = %q, want empty (nil resolver)", it.AuthorName)
	}
	if it.Text != "preview-rn1" {
		t.Errorf("Text = %q, want fallback to preview %q", it.Text, "preview-rn1")
	}
}
