// internal/steering/cascade.go
package steering

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"flow/internal/flowdb"
	"flow/internal/monitor"
)

// Cascade is the triage brain: Stage 0 (free) -> Stage 1 (cheap relevance) ->
// Stage 2 (cheap score) -> Stage 3 (deep), gated by a verdict cache and an
// hourly deep-triage budget, surfacing survivors to the Attention feed.
//
// P1.2a is SURFACE-ONLY: it never acts on a verdict, only writes a feed row.
type Cascade struct {
	DB     *sql.DB
	Config WatchConfig

	now    func() time.Time
	newID  func() string
	cache  *verdictCache
	budget *budgetGuard
	log    func(string, ...any)
}

// NewCascade builds a Cascade with production defaults (real clock, random IDs,
// a 10-minute verdict TTL, and an env-configurable hourly deep-triage budget).
func NewCascade(db *sql.DB, cfg WatchConfig) *Cascade {
	return &Cascade{
		DB:     db,
		Config: cfg,
		now:    time.Now,
		newID:  randomID,
		cache:  newVerdictCache(10 * time.Minute),
		budget: newBudgetGuard(deepBudgetPerHour()),
		log:    func(f string, a ...any) { fmt.Fprintf(os.Stderr, "[steering] "+f+"\n", a...) },
	}
}

// Observe runs the cascade for one inbound event. Errors from a stage abort
// this event's processing but are returned for logging; a dropped event (by
// any stage) returns nil.
func (c *Cascade) Observe(ctx context.Context, ev monitor.InboundEvent) error {
	s0 := Stage0(ev, c.Config)
	if !s0.Pass {
		return nil
	}
	if c.cache.seenFn(s0.ThreadKey, c.now()) {
		return nil
	}

	in := ClassifyInput{ThreadKey: s0.ThreadKey, Source: "slack", Author: ev.UserID, Text: ev.Text}

	rel, err := Stage1Relevance(ctx, []ClassifyInput{in})
	if err != nil {
		return fmt.Errorf("steering: stage1: %w", err)
	}
	if len(rel) == 0 || !rel[0].Relevant {
		c.cache.mark(s0.ThreadKey, c.now())
		return nil
	}

	taskIndex, err := BuildTaskIndex(c.DB)
	if err != nil {
		return fmt.Errorf("steering: task index: %w", err)
	}

	v2, err := Stage2Score(ctx, in, taskIndex)
	if err != nil {
		return fmt.Errorf("steering: stage2: %w", err)
	}
	if v2.SuggestedAction == ActionDrop {
		c.cache.mark(s0.ThreadKey, c.now())
		return nil
	}

	// Backpressure: when the deep-triage budget is exhausted, surface the cheap
	// Stage-2 verdict rather than silently deferring. Nothing is lost.
	if !c.budget.allow(c.now()) {
		c.log("deep-triage budget exhausted; surfacing stage2 verdict for %s", s0.ThreadKey)
		c.cache.mark(s0.ThreadKey, c.now())
		return c.writeFeed(v2)
	}

	v3, err := DeepTriage(ctx, in, taskIndex)
	if err != nil {
		c.log("deep triage failed for %s: %v; falling back to stage2 verdict", s0.ThreadKey, err)
		v3 = v2
	}
	c.cache.mark(s0.ThreadKey, c.now())
	return c.writeFeed(v3)
}

// writeFeed maps a Verdict to a surface-only ('new') Attention feed row.
func (c *Cascade) writeFeed(v Verdict) error {
	item := flowdb.FeedItem{
		ID:                c.newID(),
		Source:            v.Source,
		ThreadKey:         v.ThreadKey,
		Summary:           v.Summary,
		SuggestedAction:   string(v.SuggestedAction),
		MatchedTask:       v.MatchedTask,
		SuggestedProject:  v.SuggestedProject,
		SuggestedPriority: v.SuggestedPriority,
		Urgency:           string(v.Urgency),
		IsVIP:             v.IsVIP,
		Confidence:        v.Confidence,
		Draft:             v.Draft,
		Reason:            v.Reason,
		Status:            "new",
		CreatedAt:         c.now().UTC().Format(time.RFC3339),
	}
	if item.SuggestedAction == "" {
		item.SuggestedAction = string(ActionDrop)
	}
	if _, err := flowdb.UpsertFeedItem(c.DB, item); err != nil {
		return fmt.Errorf("steering: write feed item: %w", err)
	}
	return nil
}

// ---------- verdict cache ----------

// verdictCache suppresses re-triaging the same thread within a TTL window
// (handles Slack re-deliveries, backfill replays, and bursty threads).
type verdictCache struct {
	ttl  time.Duration
	mu   sync.Mutex
	seen map[string]time.Time
}

func newVerdictCache(ttl time.Duration) *verdictCache {
	return &verdictCache{ttl: ttl, seen: map[string]time.Time{}}
}

// seenFn reports whether key was marked within the TTL of now.
func (vc *verdictCache) seenFn(key string, now time.Time) bool {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	at, ok := vc.seen[key]
	return ok && now.Sub(at) < vc.ttl
}

func (vc *verdictCache) mark(key string, now time.Time) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.seen[key] = now
}

// ---------- budget guard ----------

// budgetGuard caps deep-triage calls per rolling hour (cost backpressure).
type budgetGuard struct {
	max   int
	mu    sync.Mutex
	calls []time.Time
}

func newBudgetGuard(maxPerHour int) *budgetGuard {
	return &budgetGuard{max: maxPerHour}
}

// allow records and permits a deep-triage call if fewer than max occurred in
// the last hour; otherwise returns false without recording.
func (b *budgetGuard) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	cutoff := now.Add(-time.Hour)
	kept := b.calls[:0]
	for _, t := range b.calls {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	b.calls = kept
	if len(b.calls) >= b.max {
		return false
	}
	b.calls = append(b.calls, now)
	return true
}

func deepBudgetPerHour() int {
	if v := strings.TrimSpace(os.Getenv("FLOW_STEERING_DEEP_BUDGET_PER_HOUR")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return 40
}

func randomID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
