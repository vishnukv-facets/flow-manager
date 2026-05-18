package server

import (
	"testing"
	"time"
)

func TestContextWindowForModel(t *testing.T) {
	if got := contextWindowForModel("claude", "claude-opus-4-7"); got != 1000000 {
		t.Fatalf("opus-4-7 = %d, want 1000000", got)
	}
	if got := contextWindowForModel("claude", "claude-opus-4-6"); got != 1000000 {
		t.Fatalf("opus-4-6 = %d, want 1000000", got)
	}
	if got := contextWindowForModel("claude", "claude-sonnet-4-5"); got != 200000 {
		t.Fatalf("sonnet-4-5 = %d, want 200000", got)
	}
	if got := contextWindowForModel("claude", "claude-haiku-4-5"); got != 200000 {
		t.Fatalf("haiku-4-5 = %d, want 200000", got)
	}
	if got := contextWindowForModel("claude", ""); got != 1000000 {
		t.Fatalf("empty model claude = %d, want 1000000 (provider default)", got)
	}
	if got := contextWindowForModel("codex", "gpt-5"); got != 200000 {
		t.Fatalf("codex = %d, want 200000", got)
	}
}

func TestRuntimeStateStaleForRunning(t *testing.T) {
	now := time.Now()
	fresh := now.Add(-10 * time.Second).Format(time.RFC3339)
	stillFresh := now.Add(-60 * time.Second).Format(time.RFC3339)
	stale := now.Add(-2 * time.Minute).Format(time.RFC3339)
	veryStale := now.Add(-10 * time.Minute).Format(time.RFC3339)

	if runtimeStateStaleForRunning(fresh, fresh) {
		t.Fatalf("fresh hook + fresh transcript should not be stale")
	}
	if runtimeStateStaleForRunning(stillFresh, stale) {
		t.Fatalf("hook still under 90s should not be stale even if transcript is")
	}
	if runtimeStateStaleForRunning(stale, fresh) {
		t.Fatalf("stale hook but fresh transcript (active tool call) should not be demoted")
	}
	if !runtimeStateStaleForRunning(stale, stale) {
		t.Fatalf("hook stale + transcript stale should be flagged stale")
	}
	if !runtimeStateStaleForRunning(veryStale, "") {
		t.Fatalf("very stale hook with no transcript info should be flagged stale")
	}
	if !runtimeStateStaleForRunning(veryStale, veryStale) {
		t.Fatalf("very stale both should be flagged stale")
	}
}
