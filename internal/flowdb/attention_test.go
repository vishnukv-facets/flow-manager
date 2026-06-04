package flowdb

import "testing"

func TestAttentionFeedInsertAndList(t *testing.T) {
	db := openTempDB(t)
	defer db.Close()

	item := FeedItem{
		ID:              "f1",
		Source:          "slack",
		ThreadKey:       "C1:100.1",
		Summary:         "Customer asks for rollout date",
		SuggestedAction: "make_task",
		MatchedTask:     "kong-split",
		Urgency:         "urgent",
		IsVIP:           true,
		Confidence:      0.9,
		Draft:           "On it.",
		Reason:          "names operator",
		ContextJSON:     `{"k":"v"}`,
		Status:          "new",
		CreatedAt:       "2026-06-05T10:00:00Z",
	}
	id, err := UpsertFeedItem(db, item)
	if err != nil {
		t.Fatalf("UpsertFeedItem: %v", err)
	}
	if id != "f1" {
		t.Fatalf("id = %q, want f1", id)
	}

	got, err := ListFeedItems(db, "new")
	if err != nil {
		t.Fatalf("ListFeedItems: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].ID != "f1" || got[0].MatchedTask != "kong-split" || !got[0].IsVIP || got[0].Confidence != 0.9 {
		t.Errorf("round-trip mismatch: %+v", got[0])
	}
}

func TestAttentionFeedCoalescesByThreadKey(t *testing.T) {
	db := openTempDB(t)
	defer db.Close()

	first := FeedItem{ID: "a", Source: "slack", ThreadKey: "C1:200.1", SuggestedAction: "reply", Summary: "first", Status: "new", CreatedAt: "2026-06-05T10:00:00Z"}
	if _, err := UpsertFeedItem(db, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	second := FeedItem{ID: "b", Source: "slack", ThreadKey: "C1:200.1", SuggestedAction: "make_task", Summary: "updated", Status: "new", CreatedAt: "2026-06-05T10:05:00Z"}
	id, err := UpsertFeedItem(db, second)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if id != "a" {
		t.Errorf("coalesced id = %q, want existing id a", id)
	}

	got, err := ListFeedItems(db, "new")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (coalesced)", len(got))
	}
	if got[0].Summary != "updated" || got[0].SuggestedAction != "make_task" {
		t.Errorf("expected coalesced row to carry new fields, got %+v", got[0])
	}
}

func TestAttentionFeedSetStatus(t *testing.T) {
	db := openTempDB(t)
	defer db.Close()

	if _, err := UpsertFeedItem(db, FeedItem{ID: "x", Source: "slack", ThreadKey: "C1:300.1", SuggestedAction: "reply", Status: "new", CreatedAt: "2026-06-05T10:00:00Z"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := SetFeedItemStatus(db, "x", "dismissed", "2026-06-05T11:00:00Z"); err != nil {
		t.Fatalf("SetFeedItemStatus: %v", err)
	}
	if n, _ := ListFeedItems(db, "new"); len(n) != 0 {
		t.Errorf("new count = %d, want 0", len(n))
	}
	d, _ := ListFeedItems(db, "dismissed")
	if len(d) != 1 || d[0].ActedAt != "2026-06-05T11:00:00Z" {
		t.Errorf("dismissed = %+v", d)
	}
}

func TestGetFeedItem(t *testing.T) {
	db := openTempDB(t)
	defer db.Close()

	in := FeedItem{ID: "g1", Source: "slack", ThreadKey: "C1:1.1", Summary: "hi", SuggestedAction: "make_task", MatchedTask: "kong-split", Confidence: 0.7, Status: "new", CreatedAt: "2026-06-05T10:00:00Z"}
	if _, err := UpsertFeedItem(db, in); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := GetFeedItem(db, "g1")
	if err != nil {
		t.Fatalf("GetFeedItem: %v", err)
	}
	if got.ID != "g1" || got.MatchedTask != "kong-split" || got.SuggestedAction != "make_task" || got.Confidence != 0.7 {
		t.Errorf("round-trip mismatch: %+v", got)
	}

	if _, err := GetFeedItem(db, "nope"); err == nil {
		t.Error("GetFeedItem on a missing id must return an error")
	}
}
