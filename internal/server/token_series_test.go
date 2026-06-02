package server

import (
	"fmt"
	"testing"
)

// Claude assistant transcript line with the given timestamp and token usage.
func usageLine(ts string, input, cacheRead, output int) []byte {
	return fmt.Appendf(nil,
		`{"type":"assistant","timestamp":%q,"message":{"model":"claude-opus-4-8",`+
			`"usage":{"input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d}}}`,
		ts, input, cacheRead, output)
}

func TestAccumulateTranscriptUsageBucketsTokensByDay(t *testing.T) {
	var stats transcriptUsageStats
	// Two turns at the SAME instant (fresh 150 + 30 = 180) — identical timestamps
	// land on the same local day in any timezone — and one a full week later
	// (fresh 90), which is always a distinct local day. cache_read_input_tokens
	// is large but must be EXCLUDED from the per-day work total (freshTotal).
	const sameDay = "2026-06-01T12:00:00Z"
	const weekLater = "2026-06-08T12:00:00Z"
	accumulateTranscriptUsage(&stats, usageLine(sameDay, 100, 50000, 50))
	accumulateTranscriptUsage(&stats, usageLine(sameDay, 20, 90000, 10))
	accumulateTranscriptUsage(&stats, usageLine(weekLater, 40, 70000, 50))

	dayA := localDay(sameDay)
	dayB := localDay(weekLater)
	if dayA == "" || dayB == "" {
		t.Fatal("localDay returned empty for valid timestamps")
	}
	if dayA == dayB {
		t.Fatalf("timestamps a week apart must be different local days, both %s", dayA)
	}
	if len(stats.TokensByDay) != 2 {
		t.Errorf("TokensByDay should have 2 days, got %d: %v", len(stats.TokensByDay), stats.TokensByDay)
	}
	if got := stats.TokensByDay[dayA]; got != 180 {
		t.Errorf("dayA (%s): got %d fresh tokens, want 180 (cache reads excluded)", dayA, got)
	}
	if got := stats.TokensByDay[dayB]; got != 90 {
		t.Errorf("dayB (%s): got %d fresh tokens, want 90", dayB, got)
	}
	// Sanity: the per-day total reconciles with the cumulative session total.
	if stats.TokensSession != 270 {
		t.Errorf("TokensSession: got %d, want 270 (180+90)", stats.TokensSession)
	}
}

func TestAccumulateTranscriptUsageSkipsZeroAndUnparseableTimestamps(t *testing.T) {
	var stats transcriptUsageStats
	// Zero fresh work (all input was cache reads, no output) → no day entry.
	accumulateTranscriptUsage(&stats, usageLine("2026-06-01T09:00:00Z", 0, 50000, 0))
	// Missing timestamp but real work → counted in session total, not bucketed.
	accumulateTranscriptUsage(&stats, []byte(`{"type":"assistant","message":{"usage":{"input_tokens":100,"output_tokens":50}}}`))

	if len(stats.TokensByDay) != 0 {
		t.Errorf("TokensByDay should be empty (zero-work + undated turns), got %v", stats.TokensByDay)
	}
	if stats.TokensSession != 150 {
		t.Errorf("TokensSession: got %d, want 150 (undated turn still counts to session)", stats.TokensSession)
	}
}

func TestLocalDayRejectsGarbage(t *testing.T) {
	if got := localDay(""); got != "" {
		t.Errorf("localDay(empty): got %q, want empty", got)
	}
	if got := localDay("not-a-timestamp"); got != "" {
		t.Errorf("localDay(garbage): got %q, want empty", got)
	}
}
