package monitor

import "testing"

func TestGitHubEventKindClassification(t *testing.T) {
	cases := []struct {
		kind         GitHubEventKind
		isPR         bool
		isIssue      bool
		involvedOnly bool
	}{
		{GitHubEventPRMentioned, true, false, false},
		{GitHubEventIssueMentioned, false, true, false},
		{GitHubEventPRInvolved, true, false, true},
		{GitHubEventIssueInvolved, false, true, true},
		{GitHubEventIssueAssigned, false, true, false},
		{GitHubEventPRAssigned, true, false, false},
		{GitHubEventPRReviewRequested, true, false, false},
	}
	for _, c := range cases {
		ev := GitHubEvent{Kind: c.kind, Owner: "o", Repo: "r", Number: 5}
		if ev.IsPR() != c.isPR || ev.IsIssue() != c.isIssue || ev.IsInvolvedOnly() != c.involvedOnly {
			t.Errorf("%s: IsPR=%v(want %v) IsIssue=%v(want %v) InvolvedOnly=%v(want %v)",
				c.kind, ev.IsPR(), c.isPR, ev.IsIssue(), c.isIssue, ev.IsInvolvedOnly(), c.involvedOnly)
		}
	}
	// LinkTag prefix follows PR/issue-ness for the new kinds too.
	if got := (GitHubEvent{Kind: GitHubEventIssueMentioned, Owner: "o", Repo: "r", Number: 5}).LinkTag(); got != "gh-issue:o/r#5" {
		t.Errorf("issue_mentioned LinkTag = %q, want gh-issue:o/r#5", got)
	}
	if got := (GitHubEvent{Kind: GitHubEventPRInvolved, Owner: "o", Repo: "r", Number: 5}).LinkTag(); got != "gh-pr:o/r#5" {
		t.Errorf("pr_involved LinkTag = %q, want gh-pr:o/r#5", got)
	}
}
