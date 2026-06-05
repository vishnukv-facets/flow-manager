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
	v := attentionItemView(flowdb.FeedItem{ID: "x", Source: "slack", ThreadKey: "C1:1.1", Summary: "hi", SuggestedAction: "reply", Confidence: 0.5, Status: "new", CreatedAt: "2026-06-05T10:00:00Z"})
	if v.ID != "x" || v.Source != "slack" || v.SuggestedAction != "reply" || v.Confidence != 0.5 {
		t.Errorf("view = %+v", v)
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
