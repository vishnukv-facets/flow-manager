package app

import (
	"strings"
	"testing"
)

func TestUnpropagatedWorkWarnings(t *testing.T) {
	commits := taskGitCloseoutSnapshot{
		Branch:            "flow/feature-x",
		CommitsSinceStart: []string{"abc123 do a thing", "def456 do another"},
	}

	// Commits with no linked PR → warn that downstream tasks won't see the work.
	got := unpropagatedWorkWarnings(commits, "")
	if len(got) != 1 || !strings.Contains(got[0], "not in any PR") || !strings.Contains(got[0], "flow/feature-x") {
		t.Fatalf("expected an un-PR'd commits warning, got %#v", got)
	}
	if !strings.Contains(got[0], "2 commit") {
		t.Errorf("warning should count the commits: %q", got[0])
	}

	// Same commits, but a PR is linked → no warning (work is tracked).
	if got := unpropagatedWorkWarnings(commits, "gh-pr:acme/app#7"); len(got) != 0 {
		t.Fatalf("PR-tracked work should not warn, got %#v", got)
	}

	// No git changes at all → no warning even without a PR.
	if got := unpropagatedWorkWarnings(taskGitCloseoutSnapshot{Branch: "main"}, ""); len(got) != 0 {
		t.Fatalf("no commits/uncommitted → no warning, got %#v", got)
	}

	// Uncommitted changes warn regardless of PR linkage.
	dirty := taskGitCloseoutSnapshot{Branch: "main", WorkingTreeStatus: []string{" M file.go"}}
	if got := unpropagatedWorkWarnings(dirty, "gh-pr:acme/app#7"); len(got) != 1 || !strings.Contains(got[0], "uncommitted") {
		t.Fatalf("expected an uncommitted-changes warning, got %#v", got)
	}
}
