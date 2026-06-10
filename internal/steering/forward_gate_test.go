package steering

import "testing"

func TestForwardMatchFloorEnv(t *testing.T) {
	t.Setenv("FLOW_STEERING_FORWARD_MIN_CONFIDENCE", "") // unset → default
	if got := forwardMatchFloor(); got != 0.6 {
		t.Fatalf("default floor = %v, want 0.6", got)
	}
	t.Setenv("FLOW_STEERING_FORWARD_MIN_CONFIDENCE", "0.8")
	if got := forwardMatchFloor(); got != 0.8 {
		t.Fatalf("env floor = %v, want 0.8", got)
	}
	t.Setenv("FLOW_STEERING_FORWARD_MIN_CONFIDENCE", "bogus")
	if got := forwardMatchFloor(); got != 0.6 {
		t.Fatalf("invalid env floor = %v, want default 0.6", got)
	}
}

func TestGateWeakSemanticForward(t *testing.T) {
	t.Setenv("FLOW_STEERING_FORWARD_MIN_CONFIDENCE", "") // default 0.6

	// Weak semantic forward (no deterministic link, below floor) → digest_only.
	v := Verdict{SuggestedAction: ActionForward, MatchedTask: "coinswitch", Confidence: 0.40}
	if note := gateWeakSemanticForward(&v, false); note == "" {
		t.Fatalf("expected a downgrade note for a 0.40 semantic forward")
	}
	if v.SuggestedAction != ActionDigestOnly || v.MatchedTask != "" {
		t.Fatalf("weak semantic forward not downgraded: %+v", v)
	}

	// Deterministic thread match is trusted at any confidence → never gated.
	v = Verdict{SuggestedAction: ActionForward, MatchedTask: "coinswitch", Confidence: 0.40}
	if note := gateWeakSemanticForward(&v, true); note != "" || v.SuggestedAction != ActionForward || v.MatchedTask != "coinswitch" {
		t.Fatalf("deterministic forward must not be gated: note=%q v=%+v", note, v)
	}

	// Semantic forward at/above the floor → kept.
	v = Verdict{SuggestedAction: ActionForward, MatchedTask: "coinswitch", Confidence: 0.6}
	if note := gateWeakSemanticForward(&v, false); note != "" || v.SuggestedAction != ActionForward {
		t.Fatalf("forward at floor must not be gated: note=%q v=%+v", note, v)
	}

	// Non-forward verdicts are never gated, regardless of confidence.
	v = Verdict{SuggestedAction: ActionMakeTask, Confidence: 0.10}
	if note := gateWeakSemanticForward(&v, false); note != "" || v.SuggestedAction != ActionMakeTask {
		t.Fatalf("make_task must not be gated: note=%q v=%+v", note, v)
	}

	// Forward with no matched task (shouldn't happen, but guard) → not gated.
	v = Verdict{SuggestedAction: ActionForward, MatchedTask: "", Confidence: 0.10}
	if note := gateWeakSemanticForward(&v, false); note != "" {
		t.Fatalf("forward with empty match must not produce a note: %q", note)
	}
}
