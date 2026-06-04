package app

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"flow/internal/flowdb"
)

// attentionTestDB points FLOW_ROOT/HOME at a temp dir and returns an open DB at
// the same path the command will use (flowDBPath()).
func attentionTestDB(t *testing.T) *sql.DB {
	t.Helper()
	root := t.TempDir()
	t.Setenv("FLOW_ROOT", root)
	t.Setenv("HOME", root)
	db, err := flowdb.OpenDB(filepath.Join(root, "flow.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestRenderAttentionFeed(t *testing.T) {
	items := []flowdb.FeedItem{
		{ID: "abc123", Source: "slack", ThreadKey: "C1:1.1", SuggestedAction: "make_task", Confidence: 0.88, Urgency: "urgent", MatchedTask: "", Summary: "Customer wants rollout date"},
	}
	out := renderAttentionFeed(items)
	if !strings.Contains(out, "abc123") || !strings.Contains(out, "make_task") || !strings.Contains(out, "Customer wants rollout date") {
		t.Errorf("rendered feed missing fields:\n%s", out)
	}

	empty := renderAttentionFeed(nil)
	if !strings.Contains(strings.ToLower(empty), "no ") && strings.TrimSpace(empty) == "" {
		t.Errorf("empty feed should render a friendly message, got %q", empty)
	}
}

func TestCmdAttentionActDismiss(t *testing.T) {
	db := attentionTestDB(t)
	if _, err := flowdb.UpsertFeedItem(db, flowdb.FeedItem{ID: "d1", Source: "slack", ThreadKey: "C1:1.1", SuggestedAction: "reply", Status: "new", CreatedAt: "2026-06-05T10:00:00Z"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if rc := cmdAttentionAct([]string{"d1", "dismiss"}); rc != 0 {
		t.Fatalf("act dismiss rc = %d, want 0", rc)
	}
	if items, _ := flowdb.ListFeedItems(db, "dismissed"); len(items) != 1 {
		t.Errorf("item should be dismissed, got %d dismissed rows", len(items))
	}
}

func TestCmdAttentionActErrors(t *testing.T) {
	db := attentionTestDB(t)
	if _, err := flowdb.UpsertFeedItem(db, flowdb.FeedItem{ID: "e1", Source: "slack", ThreadKey: "C1:1.1", SuggestedAction: "reply", Status: "new", CreatedAt: "2026-06-05T10:00:00Z"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if rc := cmdAttentionAct([]string{"e1"}); rc != 2 {
		t.Errorf("missing action arg should rc=2, got %d", rc)
	}
	if rc := cmdAttentionAct([]string{"e1", "frobnicate"}); rc != 2 {
		t.Errorf("unknown action should rc=2, got %d", rc)
	}
	if rc := cmdAttentionAct([]string{"missing-id", "dismiss"}); rc != 1 {
		t.Errorf("missing feed item should rc=1, got %d", rc)
	}
}

func TestCmdAttentionListRuns(t *testing.T) {
	attentionTestDB(t)
	if rc := cmdAttentionList(nil); rc != 0 {
		t.Errorf("list on empty feed should rc=0, got %d", rc)
	}
}
