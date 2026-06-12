package app

import (
	"testing"

	"flow/internal/flowdb"
	"flow/internal/harness"
)

func TestHarnessForTaskFallsBackToSessionProvider(t *testing.T) {
	task := &flowdb.Task{SessionProvider: sessionProviderCodex}
	got, err := harnessForTask(task)
	if err != nil {
		t.Fatalf("harnessForTask: %v", err)
	}
	if got.Name() != harness.NameCodex {
		t.Fatalf("harness name = %q, want codex", got.Name())
	}
}

func TestHarnessForTaskStoredHarnessWins(t *testing.T) {
	task := &flowdb.Task{SessionProvider: sessionProviderClaude, Harness: "codex"}
	got, err := harnessForTask(task)
	if err != nil {
		t.Fatalf("harnessForTask: %v", err)
	}
	if got.Name() != harness.NameCodex {
		t.Fatalf("harness name = %q, want codex", got.Name())
	}
}

func TestHarnessNameForProvider(t *testing.T) {
	for _, tc := range []struct {
		provider string
		want     string
	}{
		{provider: sessionProviderClaude, want: "claude"},
		{provider: sessionProviderCodex, want: "codex"},
		{provider: "", want: "claude"},
	} {
		got, err := harnessNameForProvider(tc.provider)
		if err != nil {
			t.Fatalf("harnessNameForProvider(%q): %v", tc.provider, err)
		}
		if got != tc.want {
			t.Fatalf("harnessNameForProvider(%q) = %q, want %q", tc.provider, got, tc.want)
		}
	}
}
