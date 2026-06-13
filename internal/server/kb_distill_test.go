package server

import (
	"strings"
	"testing"
	"time"
)

// TestKBShouldWake covers the gate that decides whether an idle, off-cooldown
// live session with a genuine new user turn should be woken for a KB checkpoint.
// genuineOffset is the byte offset of the last NON-checkpoint user turn (see
// latestGenuineUserOffset); the cursor is where we last checkpointed.
func TestKBShouldWake(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	const (
		idle     = 8 * time.Minute
		cooldown = 30 * time.Minute
	)
	zero := time.Time{}

	cases := []struct {
		name          string
		mtime         time.Time // last transcript activity
		capturedAt    time.Time
		cursor        int64
		genuineOffset int64
		want          bool
	}{
		{"never swept, idle, new user turn", now.Add(-10 * time.Minute), zero, 0, 5000, true},
		{"still active (fresh mtime)", now.Add(-1 * time.Minute), zero, 0, 5000, false},
		{"idle but within cooldown", now.Add(-10 * time.Minute), now.Add(-5 * time.Minute), 0, 5000, false},
		{"idle, past cooldown, new user turn", now.Add(-10 * time.Minute), now.Add(-40 * time.Minute), 1000, 5000, true},
		// The loop case: cooldown expired and the transcript grew (the checkpoint
		// prompt+reply), but NO new genuine user turn — genuineOffset == cursor.
		// Must NOT re-fire. This is the bug the genuine-offset gate fixes.
		{"idle, past cooldown, no new user turn", now.Add(-10 * time.Minute), now.Add(-40 * time.Minute), 5000, 5000, false},
		{"user hasn't spoken yet", now.Add(-10 * time.Minute), zero, 0, 0, false},
		{"idle exactly at threshold", now.Add(-idle), zero, 0, 5000, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := kbShouldWake(now, tc.mtime, tc.capturedAt, tc.cursor, tc.genuineOffset, idle, cooldown)
			if got != tc.want {
				t.Errorf("kbShouldWake = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestLatestGenuineUserOffset verifies the distiller's own injected checkpoint
// prompt (a user turn carrying kbCheckpointMarker) and the agent's reply
// (assistant turn) are BOTH excluded — only real user turns advance the offset.
// This is what structurally prevents the checkpoint from re-triggering itself.
func TestLatestGenuineUserOffset(t *testing.T) {
	entries := []TranscriptEntry{
		{Type: "user", Text: "real question from the user", ByteOffset: 100},
		{Type: "assistant", Text: "answer", ByteOffset: 300},
		{Type: "user", Text: kbCheckpointMarker + " automated, run this silently...]", ByteOffset: 900},
		{Type: "assistant", Text: "Checkpoint complete.", ByteOffset: 1200},
	}
	if got := latestGenuineUserOffset(entries); got != 100 {
		t.Errorf("latestGenuineUserOffset = %d, want 100 (checkpoint turns excluded)", got)
	}
	// A subsequent genuine user turn DOES advance the offset.
	entries = append(entries, TranscriptEntry{Type: "user", Text: "follow-up", ByteOffset: 1500})
	if got := latestGenuineUserOffset(entries); got != 1500 {
		t.Errorf("latestGenuineUserOffset = %d, want 1500", got)
	}
	if got := latestGenuineUserOffset(nil); got != 0 {
		t.Errorf("latestGenuineUserOffset(nil) = %d, want 0", got)
	}
}

func TestKBDistillEnabledDefault(t *testing.T) {
	t.Setenv("FLOW_KB_DISTILL_ENABLED", "")
	if !kbDistillEnabled() {
		t.Errorf("default should be enabled")
	}
	t.Setenv("FLOW_KB_DISTILL_ENABLED", "0")
	if kbDistillEnabled() {
		t.Errorf("=0 should disable")
	}
}

func TestKBCheckpointPromptReusesSkillRules(t *testing.T) {
	got := kbCheckpointPrompt("/custom/flowroot")
	for _, want := range []string{"KB checkpoint", "§4.10", "/custom/flowroot/kb/*.md", "silently", "DURABLE"} {
		if !strings.Contains(got, want) {
			t.Errorf("kbCheckpointPrompt missing %q", want)
		}
	}
	// Empty root falls back to ~/.flow (never an empty path in the instruction).
	if !strings.Contains(kbCheckpointPrompt(""), "~/.flow/kb/*.md") {
		t.Errorf("empty root should fall back to ~/.flow/kb/*.md")
	}
}
