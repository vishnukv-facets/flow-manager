package steering

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"flow/internal/flowdb"
)

// stubActionIO swaps the shell-out vars and records calls.
type spawnRec struct{ name, slug, brief, project string }
type tellRec struct{ slug, msg string }

func stubActionIO(t *testing.T) (*[]spawnRec, *[]tellRec) {
	t.Helper()
	var spawns []spawnRec
	var tells []tellRec
	oldSpawn, oldTell := taskSpawner, taskTeller
	taskSpawner = func(_ context.Context, name, slug, brief, project string) error {
		spawns = append(spawns, spawnRec{name, slug, brief, project})
		return nil
	}
	taskTeller = func(_ context.Context, slug, msg string) error {
		tells = append(tells, tellRec{slug, msg})
		return nil
	}
	t.Cleanup(func() { taskSpawner, taskTeller = oldSpawn, oldTell })
	return &spawns, &tells
}

func TestMakeTaskFromFeed(t *testing.T) {
	db, err := flowdb.OpenDB(filepath.Join(t.TempDir(), "flow.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	spawns, _ := stubActionIO(t)

	item := flowdb.FeedItem{
		ID: "f1", Source: "slack", ThreadKey: "C1:100.1", Summary: "Customer wants rollout date",
		SuggestedAction: "make_task", SuggestedProject: "goniyo", Reason: "names operator",
		Draft: "Targeting Friday.", Status: "new", CreatedAt: "2026-06-05T10:00:00Z",
	}
	if _, err := flowdb.UpsertFeedItem(db, item); err != nil {
		t.Fatalf("seed feed: %v", err)
	}

	if err := MakeTaskFromFeed(context.Background(), db, item); err != nil {
		t.Fatalf("MakeTaskFromFeed: %v", err)
	}
	if len(*spawns) != 1 {
		t.Fatalf("taskSpawner calls = %d, want 1", len(*spawns))
	}
	got := (*spawns)[0]
	if got.project != "goniyo" {
		t.Errorf("project = %q, want goniyo", got.project)
	}
	if !strings.Contains(got.brief, "Customer wants rollout date") || !strings.Contains(got.brief, "C1:100.1") {
		t.Errorf("brief should embed summary + thread key:\n%s", got.brief)
	}
	if !strings.HasPrefix(got.slug, "att-") {
		t.Errorf("slug = %q, want att- prefix", got.slug)
	}
	// feed row marked acted
	if items, _ := flowdb.ListFeedItems(db, "acted"); len(items) != 1 {
		t.Errorf("feed item should be 'acted', got %d acted rows", len(items))
	}
}

func TestMakeTaskFromFeedLinksTask(t *testing.T) {
	db, err := flowdb.OpenDB(filepath.Join(t.TempDir(), "flow.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	stubActionIO(t)

	item := flowdb.FeedItem{
		ID: "lk1", Source: "slack", ThreadKey: "C1:700.1", Summary: "do a thing",
		SuggestedAction: "make_task", Status: "new", CreatedAt: "2026-06-05T10:00:00Z",
	}
	if _, err := flowdb.UpsertFeedItem(db, item); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := MakeTaskFromFeed(context.Background(), db, item); err != nil {
		t.Fatalf("MakeTaskFromFeed: %v", err)
	}
	got, err := flowdb.GetFeedItem(db, "lk1")
	if err != nil {
		t.Fatalf("GetFeedItem: %v", err)
	}
	if got.Status != "acted" {
		t.Errorf("Status = %q, want acted", got.Status)
	}
	if got.LinkedTask != FeedTaskSlug(item) {
		t.Errorf("LinkedTask = %q, want %q", got.LinkedTask, FeedTaskSlug(item))
	}
}

func TestForwardFeedLinksMatchedTask(t *testing.T) {
	db, err := flowdb.OpenDB(filepath.Join(t.TempDir(), "flow.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	stubActionIO(t)

	item := flowdb.FeedItem{ID: "fl1", Source: "slack", ThreadKey: "C1:800.1", MatchedTask: "kong-split", SuggestedAction: "forward", Status: "new", CreatedAt: "2026-06-05T10:00:00Z"}
	if _, err := flowdb.UpsertFeedItem(db, item); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := ForwardFeed(context.Background(), db, item); err != nil {
		t.Fatalf("ForwardFeed: %v", err)
	}
	got, err := flowdb.GetFeedItem(db, "fl1")
	if err != nil {
		t.Fatalf("GetFeedItem: %v", err)
	}
	if got.Status != "acted" || got.LinkedTask != "kong-split" {
		t.Errorf("forwarded item = status %q linked %q, want acted/kong-split", got.Status, got.LinkedTask)
	}
}

func TestForwardFeed(t *testing.T) {
	db, err := flowdb.OpenDB(filepath.Join(t.TempDir(), "flow.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	_, tells := stubActionIO(t)

	item := flowdb.FeedItem{ID: "f2", Source: "slack", ThreadKey: "C1:200.1", Summary: "rel q", MatchedTask: "kong-split", SuggestedAction: "forward", Status: "new", CreatedAt: "2026-06-05T10:00:00Z"}
	if _, err := flowdb.UpsertFeedItem(db, item); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := ForwardFeed(context.Background(), db, item); err != nil {
		t.Fatalf("ForwardFeed: %v", err)
	}
	if len(*tells) != 1 || (*tells)[0].slug != "kong-split" {
		t.Fatalf("taskTeller = %+v, want one call to kong-split", *tells)
	}
	if !strings.Contains((*tells)[0].msg, "C1:200.1") {
		t.Errorf("forward message should reference the source thread: %q", (*tells)[0].msg)
	}
	if items, _ := flowdb.ListFeedItems(db, "acted"); len(items) != 1 {
		t.Errorf("forwarded item should be 'acted'")
	}
}

func TestForwardRequiresMatchedTask(t *testing.T) {
	db, err := flowdb.OpenDB(filepath.Join(t.TempDir(), "flow.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	stubActionIO(t)
	item := flowdb.FeedItem{ID: "f3", Source: "slack", ThreadKey: "C1:300.1", SuggestedAction: "forward", Status: "new", CreatedAt: "2026-06-05T10:00:00Z"}
	if _, err := flowdb.UpsertFeedItem(db, item); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := ForwardFeed(context.Background(), db, item); err == nil {
		t.Error("forward without matched_task must error")
	}
}

func TestDismissFeed(t *testing.T) {
	db, err := flowdb.OpenDB(filepath.Join(t.TempDir(), "flow.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	item := flowdb.FeedItem{ID: "f4", Source: "slack", ThreadKey: "C1:400.1", SuggestedAction: "reply", Status: "new", CreatedAt: "2026-06-05T10:00:00Z"}
	if _, err := flowdb.UpsertFeedItem(db, item); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := DismissFeed(db, "f4"); err != nil {
		t.Fatalf("DismissFeed: %v", err)
	}
	if items, _ := flowdb.ListFeedItems(db, "dismissed"); len(items) != 1 {
		t.Errorf("item should be dismissed")
	}
}

func TestApplyActionManualBypassesGate(t *testing.T) {
	db, err := flowdb.OpenDB(filepath.Join(t.TempDir(), "flow.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	spawns, _ := stubActionIO(t)

	item := flowdb.FeedItem{ID: "g1", Source: "slack", ThreadKey: "C1:1.1", Summary: "s", SuggestedAction: "make_task", Confidence: 0.1, Status: "new", CreatedAt: "2026-06-05T10:00:00Z"}
	if _, err := flowdb.UpsertFeedItem(db, item); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// manual=true, even with all-off DefaultAutonomy and low confidence → executes.
	if err := ApplyAction(context.Background(), db, item, ActionMakeTask, DefaultAutonomy(), true); err != nil {
		t.Fatalf("manual ApplyAction: %v", err)
	}
	if len(*spawns) != 1 {
		t.Errorf("manual action should execute regardless of gate, spawns=%d", len(*spawns))
	}
}

func TestApplyActionAutonomousDenied(t *testing.T) {
	db, err := flowdb.OpenDB(filepath.Join(t.TempDir(), "flow.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	spawns, _ := stubActionIO(t)

	item := flowdb.FeedItem{ID: "g2", Source: "slack", ThreadKey: "C1:2.1", SuggestedAction: "make_task", Confidence: 0.99, Status: "new", CreatedAt: "2026-06-05T10:00:00Z"}
	if _, err := flowdb.UpsertFeedItem(db, item); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// manual=false + DefaultAutonomy (all disabled) → denied, no execution.
	err = ApplyAction(context.Background(), db, item, ActionMakeTask, DefaultAutonomy(), false)
	if err != ErrAutonomyDenied {
		t.Fatalf("autonomous make_task under default policy should be ErrAutonomyDenied, got %v", err)
	}
	if len(*spawns) != 0 {
		t.Errorf("denied action must NOT execute, spawns=%d", len(*spawns))
	}
	if items, _ := flowdb.ListFeedItems(db, "new"); len(items) != 1 {
		t.Errorf("denied action must leave the feed row untouched ('new')")
	}
}

func TestApplyActionAutonomousAllowed(t *testing.T) {
	db, err := flowdb.OpenDB(filepath.Join(t.TempDir(), "flow.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	spawns, _ := stubActionIO(t)

	item := flowdb.FeedItem{ID: "g3", Source: "slack", ThreadKey: "C1:3.1", Summary: "s", SuggestedAction: "make_task", Confidence: 0.95, Status: "new", CreatedAt: "2026-06-05T10:00:00Z"}
	if _, err := flowdb.UpsertFeedItem(db, item); err != nil {
		t.Fatalf("seed: %v", err)
	}
	policy := AutonomyPolicy{ActionMakeTask: {Enabled: true, Threshold: 0.80}}
	if err := ApplyAction(context.Background(), db, item, ActionMakeTask, policy, false); err != nil {
		t.Fatalf("autonomous allowed ApplyAction: %v", err)
	}
	if len(*spawns) != 1 {
		t.Errorf("allowed autonomous action should execute, spawns=%d", len(*spawns))
	}
}

func TestApplyActionUnsupported(t *testing.T) {
	db, err := flowdb.OpenDB(filepath.Join(t.TempDir(), "flow.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	stubActionIO(t)
	item := flowdb.FeedItem{ID: "g4", Source: "slack", ThreadKey: "C1:4.1", SuggestedAction: "reply", Confidence: 0.9, Status: "new", CreatedAt: "2026-06-05T10:00:00Z"}
	if _, err := flowdb.UpsertFeedItem(db, item); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// reply/afk_reply are outward sends — not implemented until P2. Manual or not, ApplyAction errors.
	if err := ApplyAction(context.Background(), db, item, ActionReply, DefaultAutonomy(), true); err == nil {
		t.Error("reply action is unsupported in P1.3 and must error")
	}
}
