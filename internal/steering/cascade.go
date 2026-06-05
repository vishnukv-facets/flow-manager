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
	// ConfigFn, when set, is called per Observe to read the watch-config live
	// (so Mission Control settings changes take effect without a restart). When
	// nil, the static Config is used. NewCascade leaves it nil; serve wiring
	// sets it to WatchConfigFromEnv.
	ConfigFn func() WatchConfig

	now    func() time.Time
	newID  func() string
	cache  *verdictCache
	budget *budgetGuard
	log    func(string, ...any)
	// trace records one decision-trace row per observed event. NewCascade
	// defaults it to a writer that inserts into the steering_trace table; tests
	// swap it to capture rows in memory.
	trace func(flowdb.SteeringTrace)
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
		trace:  func(t flowdb.SteeringTrace) { _ = flowdb.InsertSteeringTrace(db, t) },
	}
}

// Observe runs the cascade for one live inbound event. Errors from a stage
// abort this event's processing but are returned for logging; a dropped event
// (by any stage) returns nil. Every observed event emits exactly one
// decision-trace row.
func (c *Cascade) Observe(ctx context.Context, ev monitor.InboundEvent) error {
	return c.observe(ctx, ev, "live")
}

// ObserveBackfill is identical to Observe but tags traces with origin
// "backfill" (used by the steerer's catch-up replay).
func (c *Cascade) ObserveBackfill(ctx context.Context, ev monitor.InboundEvent) error {
	return c.observe(ctx, ev, "backfill")
}

// observe is the single-event triage path: Stage 0 → verdict cache →
// single-event Stage 1 relevance, then the shared finishItem tail. It emits a
// trace at every exit.
func (c *Cascade) observe(ctx context.Context, ev monitor.InboundEvent, origin string) error {
	start := c.now()
	tr := c.newTrace(ev, origin)
	cfg := c.Config
	if c.ConfigFn != nil {
		cfg = c.ConfigFn()
	}

	s0 := Stage0(ev, cfg)
	if !s0.Pass {
		tr.Disposition, tr.StageReached, tr.DropReason = "dropped", "stage0", s0.DropReason
		c.emitTrace(tr, start)
		return nil
	}
	tr.ThreadKey = s0.ThreadKey
	if c.cache.seenFn(s0.ThreadKey, c.now()) {
		tr.Disposition, tr.StageReached, tr.DropReason = "dropped", "cache", "duplicate within verdict TTL"
		c.emitTrace(tr, start)
		return nil
	}

	in := ClassifyInput{ThreadKey: s0.ThreadKey, Source: connectorOf(ev), Author: ev.UserID, Text: ev.Text}

	rel, err := Stage1Relevance(ctx, []ClassifyInput{in})
	if err != nil {
		tr.Disposition, tr.StageReached, tr.Error = "error", "stage1", err.Error()
		c.emitTrace(tr, start)
		return fmt.Errorf("steering: stage1: %w", err)
	}
	if len(rel) == 0 || !rel[0].Relevant {
		c.cache.mark(s0.ThreadKey, c.now())
		f := false
		tr.Stage1Relevant = &f
		tr.Disposition, tr.StageReached, tr.DropReason = "dropped", "stage1", "not relevant"
		c.emitTrace(tr, start)
		return nil
	}
	t := true
	tr.Stage1Relevant = &t
	return c.finishItem(ctx, in, tr, start)
}

// finishItem runs the per-item tail of the cascade — task index → Stage 2 →
// budget gate → Stage 3 deep triage → feed write — and emits a trace at every
// exit. It assumes Stage 0/cache/Stage 1 have already passed and tr.ThreadKey
// + tr.Stage1Relevant are set.
func (c *Cascade) finishItem(ctx context.Context, in ClassifyInput, tr *flowdb.SteeringTrace, start time.Time) error {
	taskIndex, err := BuildTaskIndex(c.DB)
	if err != nil {
		tr.Disposition, tr.StageReached, tr.Error = "error", "stage1", err.Error()
		c.emitTrace(tr, start)
		return fmt.Errorf("steering: task index: %w", err)
	}

	v2, err := Stage2Score(ctx, in, taskIndex)
	if err != nil {
		tr.Disposition, tr.StageReached, tr.Error = "error", "stage2", err.Error()
		c.emitTrace(tr, start)
		return fmt.Errorf("steering: stage2: %w", err)
	}
	tr.Stage2Action = string(v2.SuggestedAction)
	tr.Stage2Confidence = v2.Confidence
	if v2.SuggestedAction == ActionDrop {
		c.cache.mark(in.ThreadKey, c.now())
		tr.Disposition, tr.StageReached, tr.DropReason = "dropped", "stage2", "stage2 action=drop"
		c.emitTrace(tr, start)
		return nil
	}

	// Backpressure: when the deep-triage budget is exhausted, surface the cheap
	// Stage-2 verdict rather than silently deferring. Nothing is lost.
	if !c.budget.allow(c.now()) {
		c.log("deep-triage budget exhausted; surfacing stage2 verdict for %s", in.ThreadKey)
		c.cache.mark(in.ThreadKey, c.now())
		id, werr := c.writeFeed(v2)
		tr.Disposition, tr.StageReached = "surfaced", "stage2"
		tr.DropReason = "deep budget exhausted; surfaced stage2 verdict"
		tr.FinalAction, tr.FinalConfidence, tr.FeedItemID = string(v2.SuggestedAction), v2.Confidence, id
		c.emitTrace(tr, start)
		return werr
	}

	v3, err := DeepTriage(ctx, in, taskIndex)
	if err != nil {
		c.log("deep triage failed for %s: %v; falling back to stage2 verdict", in.ThreadKey, err)
		tr.Error = "deep triage failed: " + err.Error() + "; fell back to stage2"
		v3 = v2
		tr.StageReached = "stage2"
	} else {
		tr.Stage3Action = string(v3.SuggestedAction)
		tr.Stage3Confidence = v3.Confidence
		tr.StageReached = "stage3"
	}
	c.cache.mark(in.ThreadKey, c.now())
	id, werr := c.writeFeed(v3)
	tr.Disposition = "surfaced"
	tr.FinalAction, tr.FinalConfidence, tr.FeedItemID = string(v3.SuggestedAction), v3.Confidence, id
	c.emitTrace(tr, start)
	return werr
}

// ObserveBatch triages a batch of events with a SINGLE batched Stage 1
// relevance call (the rest is per-item). Used by the steerer backfill, where
// many events arrive at once. Each event still emits exactly one trace.
func (c *Cascade) ObserveBatch(ctx context.Context, evs []monitor.InboundEvent) error {
	cfg := c.Config
	if c.ConfigFn != nil {
		cfg = c.ConfigFn()
	}
	type pending struct {
		in    ClassifyInput
		tr    *flowdb.SteeringTrace
		start time.Time
	}
	var survivors []pending
	var inputs []ClassifyInput
	for _, ev := range evs {
		start := c.now()
		tr := c.newTrace(ev, "backfill")
		s0 := Stage0(ev, cfg)
		if !s0.Pass {
			tr.Disposition, tr.StageReached, tr.DropReason = "dropped", "stage0", s0.DropReason
			c.emitTrace(tr, start)
			continue
		}
		tr.ThreadKey = s0.ThreadKey
		if c.cache.seenFn(s0.ThreadKey, c.now()) {
			tr.Disposition, tr.StageReached, tr.DropReason = "dropped", "cache", "duplicate within verdict TTL"
			c.emitTrace(tr, start)
			continue
		}
		in := ClassifyInput{ThreadKey: s0.ThreadKey, Source: connectorOf(ev), Author: ev.UserID, Text: ev.Text}
		survivors = append(survivors, pending{in, tr, start})
		inputs = append(inputs, in)
	}
	if len(inputs) == 0 {
		return nil
	}
	rel, err := Stage1Relevance(ctx, inputs)
	if err != nil {
		for _, p := range survivors {
			p.tr.Disposition, p.tr.StageReached, p.tr.Error = "error", "stage1", err.Error()
			c.emitTrace(p.tr, p.start)
		}
		return fmt.Errorf("steering: stage1 batch: %w", err)
	}
	relByKey := make(map[string]bool, len(rel))
	for _, v := range rel {
		relByKey[v.ThreadKey] = v.Relevant
	}
	var firstErr error
	for _, p := range survivors {
		if !relByKey[p.in.ThreadKey] {
			c.cache.mark(p.in.ThreadKey, c.now())
			f := false
			p.tr.Stage1Relevant = &f
			p.tr.Disposition, p.tr.StageReached, p.tr.DropReason = "dropped", "stage1", "not relevant"
			c.emitTrace(p.tr, p.start)
			continue
		}
		t := true
		p.tr.Stage1Relevant = &t
		if e := c.finishItem(ctx, p.in, p.tr, p.start); e != nil && firstErr == nil {
			firstErr = e
		}
	}
	return firstErr
}

// newTrace seeds a decision-trace row from the inbound event with the fields
// known before any stage runs.
func (c *Cascade) newTrace(ev monitor.InboundEvent, origin string) *flowdb.SteeringTrace {
	return &flowdb.SteeringTrace{
		ID:          c.newID(),
		CreatedAt:   c.now().UTC().Format(time.RFC3339),
		Origin:      origin,
		Source:      connectorOf(ev),
		Channel:     ev.Channel,
		ChannelType: ev.ChannelType,
		Author:      ev.UserID,
		TextPreview: preview(ev.Text),
		Model:       classifierModel(),
		TS:          ev.TS,
		TeamID:      ev.TeamID,
		URL:         ev.URL,
	}
}

// emitTrace stamps the latency and hands the finished trace row to the sink.
func (c *Cascade) emitTrace(tr *flowdb.SteeringTrace, start time.Time) {
	tr.LatencyMS = c.now().Sub(start).Milliseconds()
	c.trace(*tr)
}

// preview trims and truncates message text for the trace (operator's own data —
// safe to store; just keep rows small).
func preview(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}

// writeFeed maps a Verdict to a surface-only ('new') Attention feed row and
// returns the upserted item's id.
func (c *Cascade) writeFeed(v Verdict) (string, error) {
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
	id, err := flowdb.UpsertFeedItem(c.DB, item)
	if err != nil {
		return "", fmt.Errorf("steering: write feed item: %w", err)
	}
	return id, nil
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
