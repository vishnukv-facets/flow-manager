// internal/steering/cascade_test.go
package steering

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flow/internal/flowdb"
	"flow/internal/monitor"
)

// captureTraces wires c.trace to append into a slice and returns a pointer to
// it, so a test can assert on the decision-trace rows the cascade emits.
func captureTraces(c *Cascade) *[]flowdb.SteeringTrace {
	var traces []flowdb.SteeringTrace
	c.trace = func(t flowdb.SteeringTrace) { traces = append(traces, t) }
	return &traces
}

func cascadeFixture(t *testing.T) (*Cascade, *sql.DB) {
	t.Helper()
	db, err := flowdb.OpenDB(filepath.Join(t.TempDir(), "flow.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	c := NewCascade(db, WatchConfig{
		WatchedChannels: map[string]bool{"C1": true},
		Identity:        OperatorIdentity{UserIDs: []string{"U_ME"}},
		MentionUserIDs:  []string{"U_ME"},
	})
	// deterministic id + clock for assertions
	n := 0
	c.newID = func() string { n++; return "id" + string(rune('0'+n)) }
	c.now = func() time.Time { return time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC) }
	return c, db
}

func msg(channel, ts, user, text string) monitor.InboundEvent {
	return monitor.InboundEvent{Kind: "message", ChannelType: "channel", Channel: channel, TS: ts, ThreadTS: ts, UserID: user, Text: text}
}

func TestCascadeSurfacesSurvivor(t *testing.T) {
	c, db := cascadeFixture(t)
	stubClassifier(t, func(prompt string) (string, error) {
		if strings.Contains(prompt, "MODE: stage1-relevance") {
			return `[{"thread_key":"C1:1.1","relevant":true,"urgency_hint":"urgent"}]`, nil
		}
		return `{"suggested_action":"reply","confidence":0.8,"summary":"q"}`, nil // stage2
	})
	stubDeepTriage(t, func(prompt string) (string, error) {
		return `{"suggested_action":"reply","confidence":0.9,"summary":"customer q","draft":"On it."}`, nil
	})
	if err := c.Observe(context.Background(), msg("C1", "1.1", "U_OTHER", "need help")); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	items, _ := flowdb.ListFeedItems(db, "new")
	if len(items) != 1 {
		t.Fatalf("feed len = %d, want 1", len(items))
	}
	if items[0].Draft != "On it." || items[0].SuggestedAction != "reply" || items[0].ThreadKey != "C1:1.1" {
		t.Errorf("feed item = %+v", items[0])
	}
}

func TestCascadeStage0DropWritesNothing(t *testing.T) {
	c, db := cascadeFixture(t)
	// self-authored → Stage0 drops before any model call
	if err := c.Observe(context.Background(), msg("C1", "2.1", "U_ME", "note")); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if items, _ := flowdb.ListFeedItems(db, ""); len(items) != 0 {
		t.Errorf("expected no feed items, got %d", len(items))
	}
}

func TestCascadeStage1DropWritesNothing(t *testing.T) {
	c, db := cascadeFixture(t)
	stubClassifier(t, func(prompt string) (string, error) {
		return `[{"thread_key":"C1:3.1","relevant":false}]`, nil // stage1 says no
	})
	if err := c.Observe(context.Background(), msg("C1", "3.1", "U_OTHER", "lol")); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if items, _ := flowdb.ListFeedItems(db, ""); len(items) != 0 {
		t.Errorf("expected no feed items, got %d", len(items))
	}
}

func TestCascadeVerdictCacheSkipsRepeat(t *testing.T) {
	c, db := cascadeFixture(t)
	calls := 0
	stubClassifier(t, func(prompt string) (string, error) {
		calls++
		if strings.Contains(prompt, "MODE: stage1-relevance") {
			return `[{"thread_key":"C1:4.1","relevant":true}]`, nil
		}
		return `{"suggested_action":"reply","confidence":0.8}`, nil
	})
	stubDeepTriage(t, func(prompt string) (string, error) {
		return `{"suggested_action":"reply","confidence":0.9,"summary":"q"}`, nil
	})
	ev := msg("C1", "4.1", "U_OTHER", "help")
	_ = c.Observe(context.Background(), ev)
	callsAfterFirst := calls
	_ = c.Observe(context.Background(), ev) // same thread within TTL
	if calls != callsAfterFirst {
		t.Errorf("second Observe should hit verdict cache and make no model calls (calls %d -> %d)", callsAfterFirst, calls)
	}
	if items, _ := flowdb.ListFeedItems(db, "new"); len(items) != 1 {
		t.Errorf("cache must prevent a duplicate feed row, got %d", len(items))
	}
}

func TestCascadeBudgetExhaustionSurfacesStage2(t *testing.T) {
	c, db := cascadeFixture(t)
	c.budget = newBudgetGuard(0) // zero deep-triage budget
	deepCalled := false
	stubClassifier(t, func(prompt string) (string, error) {
		if strings.Contains(prompt, "MODE: stage1-relevance") {
			return `[{"thread_key":"C1:5.1","relevant":true}]`, nil
		}
		return `{"suggested_action":"make_task","confidence":0.7,"summary":"stage2 only"}`, nil
	})
	stubDeepTriage(t, func(prompt string) (string, error) { deepCalled = true; return "{}", nil })
	if err := c.Observe(context.Background(), msg("C1", "5.1", "U_OTHER", "please")); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if deepCalled {
		t.Error("deep triage must NOT run when budget is exhausted")
	}
	items, _ := flowdb.ListFeedItems(db, "new")
	if len(items) != 1 || items[0].Summary != "stage2 only" {
		t.Errorf("budget exhaustion must still surface the stage2 verdict, got %+v", items)
	}
}

func TestObserveTraceStage0Drop(t *testing.T) {
	c, _ := cascadeFixture(t)
	traces := captureTraces(c)
	// self-authored (U_ME is in cfg.Identity.UserIDs) → Stage0 drop, no model call
	if err := c.Observe(context.Background(), msg("C1", "10.1", "U_ME", "note")); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(*traces) != 1 {
		t.Fatalf("trace count = %d, want exactly 1", len(*traces))
	}
	tr := (*traces)[0]
	if tr.Disposition != "dropped" || tr.StageReached != "stage0" || tr.DropReason != "self-authored" {
		t.Errorf("trace = %+v; want dropped/stage0/self-authored", tr)
	}
}

func TestObserveTraceSurfaced(t *testing.T) {
	c, _ := cascadeFixture(t)
	traces := captureTraces(c)
	stubClassifier(t, func(prompt string) (string, error) {
		if strings.Contains(prompt, "MODE: stage1-relevance") {
			return `[{"thread_key":"C1:11.1","relevant":true}]`, nil
		}
		return `{"suggested_action":"make_task","confidence":0.72,"summary":"do it"}`, nil // stage2
	})
	stubDeepTriage(t, func(prompt string) (string, error) {
		return `{"suggested_action":"make_task","confidence":0.9,"summary":"deep","draft":""}`, nil
	})
	if err := c.Observe(context.Background(), msg("C1", "11.1", "U_OTHER", "please do this")); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(*traces) != 1 {
		t.Fatalf("trace count = %d, want exactly 1", len(*traces))
	}
	tr := (*traces)[0]
	if tr.Disposition != "surfaced" || tr.StageReached != "stage3" {
		t.Errorf("trace disposition/stage = %s/%s; want surfaced/stage3", tr.Disposition, tr.StageReached)
	}
	if tr.FeedItemID == "" {
		t.Error("surfaced trace must record a FeedItemID")
	}
	if tr.FinalAction != "make_task" {
		t.Errorf("FinalAction = %q, want make_task", tr.FinalAction)
	}
	if tr.Stage1Relevant == nil || !*tr.Stage1Relevant {
		t.Errorf("Stage1Relevant = %v, want non-nil true", tr.Stage1Relevant)
	}
}

func TestObserveTraceCacheDuplicate(t *testing.T) {
	c, _ := cascadeFixture(t)
	traces := captureTraces(c)
	stubClassifier(t, func(prompt string) (string, error) {
		if strings.Contains(prompt, "MODE: stage1-relevance") {
			return `[{"thread_key":"C1:12.1","relevant":true}]`, nil
		}
		return `{"suggested_action":"reply","confidence":0.8}`, nil
	})
	stubDeepTriage(t, func(prompt string) (string, error) {
		return `{"suggested_action":"reply","confidence":0.9,"summary":"q"}`, nil
	})
	ev := msg("C1", "12.1", "U_OTHER", "help")
	if err := c.Observe(context.Background(), ev); err != nil {
		t.Fatalf("first Observe: %v", err)
	}
	if err := c.Observe(context.Background(), ev); err != nil { // same thread within TTL
		t.Fatalf("second Observe: %v", err)
	}
	if len(*traces) != 2 {
		t.Fatalf("trace count = %d, want 2 (one per Observe)", len(*traces))
	}
	second := (*traces)[1]
	if second.Disposition != "dropped" || second.StageReached != "cache" {
		t.Errorf("second trace = %s/%s; want dropped/cache", second.Disposition, second.StageReached)
	}
}

// stage1RelevanceCalls counts how many classifier prompts contained the
// stage1-relevance mode marker (reset per test).
var stage1RelevanceCalls int

func TestObserveBatchSingleStage1Call(t *testing.T) {
	c, _ := cascadeFixture(t)
	traces := captureTraces(c)
	stage1RelevanceCalls = 0
	stubClassifier(t, func(prompt string) (string, error) {
		if strings.Contains(prompt, "MODE: stage1-relevance") {
			stage1RelevanceCalls++
			// Echo every input thread_key back as relevant so Stage1Relevance
			// (which matches by key and fails closed) blesses both survivors.
			i := strings.Index(prompt, "[")
			j := strings.LastIndex(prompt, "]")
			var inputs []ClassifyInput
			if i >= 0 && j > i {
				_ = json.Unmarshal([]byte(prompt[i:j+1]), &inputs)
			}
			out := make([]RelevanceVerdict, 0, len(inputs))
			for _, in := range inputs {
				out = append(out, RelevanceVerdict{ThreadKey: in.ThreadKey, Relevant: true})
			}
			b, _ := json.Marshal(out)
			return string(b), nil
		}
		return `{"suggested_action":"reply","confidence":0.8,"summary":"q"}`, nil // stage2
	})
	stubDeepTriage(t, func(prompt string) (string, error) {
		return `{"suggested_action":"reply","confidence":0.9,"summary":"q"}`, nil
	})
	evs := []monitor.InboundEvent{
		msg("C1", "20.1", "U_ME", "self note"),  // stage0 drop
		msg("C1", "21.1", "U_OTHER", "need a hand"),
		msg("C1", "22.1", "U_OTHER", "another one"),
	}
	if err := c.ObserveBatch(context.Background(), evs); err != nil {
		t.Fatalf("ObserveBatch: %v", err)
	}
	if stage1RelevanceCalls != 1 {
		t.Errorf("stage1-relevance call count = %d, want exactly 1 (one batched call)", stage1RelevanceCalls)
	}
	if len(*traces) != 3 {
		t.Errorf("trace count = %d, want 3 (one per event)", len(*traces))
	}
}

func TestCascadeConfigFnOverridesStatic(t *testing.T) {
	c, _ := cascadeFixture(t) // static Config watches C1 (see cascadeFixture)
	// ConfigFn watches a DIFFERENT channel — proves Observe consults ConfigFn,
	// not the static Config captured at construction.
	c.ConfigFn = func() WatchConfig {
		return WatchConfig{WatchedChannels: map[string]bool{"C_LIVE": true}}
	}
	called := false
	stubClassifier(t, func(prompt string) (string, error) {
		called = true
		return `[{"thread_key":"C_LIVE:1.1","relevant":false}]`, nil // stage1 drops, cheap
	})
	// Message in C_LIVE (only in ConfigFn's set, NOT the static C1 set).
	if err := c.Observe(context.Background(), msg("C_LIVE", "1.1", "U_OTHER", "hi")); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !called {
		t.Error("Stage 0 should have passed using ConfigFn's watched channels (classifier never ran)")
	}

	// And a message in the STATIC-only channel C1 must now drop (ConfigFn wins).
	called = false
	if err := c.Observe(context.Background(), msg("C1", "2.1", "U_OTHER", "hi")); err != nil {
		t.Fatalf("Observe C1: %v", err)
	}
	if called {
		t.Error("C1 is not in ConfigFn's set, so Stage 0 should drop it (classifier must not run)")
	}
}
