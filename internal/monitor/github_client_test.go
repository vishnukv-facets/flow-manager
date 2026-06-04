package monitor

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

type fakeGitHubAPIClient struct {
	search           []githubIssueRecord
	mentionRows      []githubIssueRecord
	involvesRows     []githubIssueRecord
	prs              map[string]githubPullRequestRecord
	commentPRs       []int
	commentRows      []githubReviewCommentRecord
	reviewPRs        []int
	reviewRows       []githubReviewRecord
	issueCommentRefs []issueCommentCall
	issueCommentRows []githubIssueCommentRecord
}

type issueCommentCall struct {
	Owner  string
	Repo   string
	Number int
}

func (f fakeGitHubAPIClient) SearchIssues(_ context.Context, query string) ([]githubIssueRecord, error) {
	switch {
	case strings.Contains(query, "mentions:"):
		return f.mentionRows, nil
	case strings.Contains(query, "involves:"):
		return f.involvesRows, nil
	case strings.Contains(query, "assignee:"):
		return f.search, nil
	default:
		return nil, nil
	}
}

func (f fakeGitHubAPIClient) GetPullRequest(_ context.Context, owner, repo string, number int) (githubPullRequestRecord, error) {
	key := owner + "/" + repo + "#" + strconv.Itoa(number)
	if pr, ok := f.prs[key]; ok {
		return pr, nil
	}
	for k, pr := range f.prs {
		if strings.EqualFold(k, key) {
			return pr, nil
		}
	}
	return githubPullRequestRecord{}, nil
}

func (f *fakeGitHubAPIClient) ListReviewComments(_ context.Context, _ string, _ string, number int, _ string) ([]githubReviewCommentRecord, error) {
	f.commentPRs = append(f.commentPRs, number)
	return f.commentRows, nil
}

func (f *fakeGitHubAPIClient) ListReviews(_ context.Context, _ string, _ string, number int, _ string) ([]githubReviewRecord, error) {
	f.reviewPRs = append(f.reviewPRs, number)
	return f.reviewRows, nil
}

func (f *fakeGitHubAPIClient) ListIssueComments(_ context.Context, owner, repo string, number int, _ string) ([]githubIssueCommentRecord, error) {
	f.issueCommentRefs = append(f.issueCommentRefs, issueCommentCall{Owner: owner, Repo: repo, Number: number})
	return f.issueCommentRows, nil
}

func TestGithubIssueRecordKindByQuery(t *testing.T) {
	prRec := githubIssueRecord{Number: 9, RepositoryURL: "https://api.github.com/repos/o/r", PullRequest: []byte(`{"url":"x"}`), User: githubUser{Login: "u"}}
	issueRec := githubIssueRecord{Number: 9, RepositoryURL: "https://api.github.com/repos/o/r", User: githubUser{Login: "u"}}
	cases := []struct {
		rec   githubIssueRecord
		query string
		want  GitHubEventKind
	}{
		{prRec, "is:open mentions:me", GitHubEventPRMentioned},
		{issueRec, "is:open mentions:me", GitHubEventIssueMentioned},
		{prRec, "is:open involves:me", GitHubEventPRInvolved},
		{issueRec, "is:open involves:me", GitHubEventIssueInvolved},
		{prRec, "is:open assignee:me", GitHubEventPRAssigned},
		{issueRec, "is:open assignee:me", GitHubEventIssueAssigned},
		{prRec, "is:open is:pr review-requested:me", GitHubEventPRReviewRequested},
	}
	for _, c := range cases {
		ev, ok := c.rec.toGitHubEvent("me", c.query)
		if !ok || ev.Kind != c.want {
			t.Errorf("query %q -> kind %q (ok=%v), want %q", c.query, ev.Kind, ok, c.want)
		}
	}
}

func TestQueriesForLoginIncludesDiscoveryWithWatermark(t *testing.T) {
	p := GitHubPoller{SelfLogins: []string{"me"}}
	joined := strings.Join(p.queriesForLogin("me", "2026-06-04T00:00:00Z", true), "\n")
	for _, want := range []string{
		"assignee:me",
		"review-requested:me",
		"mentions:me updated:>=2026-06-04T00:00:00Z",
		"involves:me updated:>=2026-06-04T00:00:00Z",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("queries missing %q in:\n%s", want, joined)
		}
	}
	// Skip-on-error path: includeBroad=false drops the broad queries entirely.
	off := strings.Join(p.queriesForLogin("me", "", false), "\n")
	if strings.Contains(off, "mentions:") || strings.Contains(off, "involves:") {
		t.Errorf("broad queries must be omitted when includeBroad=false:\n%s", off)
	}
	if !strings.Contains(off, "assignee:me") {
		t.Errorf("direct queries must always run:\n%s", off)
	}
}

func TestGitHubPollerCollapsesDiscoveryByItem(t *testing.T) {
	db := dispatcherTestDB(t)
	mk := func(n int) githubIssueRecord {
		return githubIssueRecord{
			Number:        n,
			RepositoryURL: "https://api.github.com/repos/o/r",
			HTMLURL:       "https://github.com/o/r/issues/" + strconv.Itoa(n),
			User:          githubUser{Login: "x"},
		}
	}
	client := &fakeGitHubAPIClient{
		search:       []githubIssueRecord{mk(50)},         // assignee
		mentionRows:  []githubIssueRecord{mk(70)},          // mention only
		involvesRows: []githubIssueRecord{mk(50), mk(60)},  // 50 also involved, 60 involved only
	}
	p := GitHubPoller{DB: db, Client: client, SelfLogins: []string{"me"}}

	events, err := p.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	byTag := map[string]GitHubEventKind{}
	for _, e := range events {
		if _, dup := byTag[e.LinkTag()]; dup {
			t.Fatalf("item %s produced more than one event (collapse failed): %#v", e.LinkTag(), events)
		}
		byTag[e.LinkTag()] = e.Kind
	}
	if byTag["gh-issue:o/r#50"] != GitHubEventIssueAssigned {
		t.Errorf("#50 (assignee+involves) should collapse to assigned, got %q", byTag["gh-issue:o/r#50"])
	}
	if byTag["gh-issue:o/r#60"] != GitHubEventIssueInvolved {
		t.Errorf("#60 should be involved, got %q", byTag["gh-issue:o/r#60"])
	}
	if byTag["gh-issue:o/r#70"] != GitHubEventIssueMentioned {
		t.Errorf("#70 should be mentioned, got %q", byTag["gh-issue:o/r#70"])
	}
}

func TestGitHubPollerEnrichesPullRequestRefs(t *testing.T) {
	p := GitHubPoller{
		Client: &fakeGitHubAPIClient{
			search: []githubIssueRecord{
				{
					Number:        42,
					Title:         "Add GitHub integration",
					HTMLURL:       "https://github.com/Facets-cloud/flow-manager/pull/42",
					RepositoryURL: "https://api.github.com/repos/Facets-cloud/flow-manager",
					PullRequest:   []byte(`{"url":"https://api.github.com/repos/Facets-cloud/flow-manager/pulls/42"}`),
					User:          githubUser{Login: "octo"},
				},
			},
			prs: map[string]githubPullRequestRecord{
				"Facets-cloud/flow-manager#42": {
					Base: githubRef{Name: "main"},
					Head: githubRef{Name: "feature/github"},
				},
			},
		},
		SelfLogins: []string{"me"},
	}

	events, err := p.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].BaseRef != "main" || events[0].HeadRef != "feature/github" {
		t.Fatalf("refs = base %q head %q", events[0].BaseRef, events[0].HeadRef)
	}
}

func TestGitHubPollerFetchesReviewCommentsForTrackedPRNumber(t *testing.T) {
	db := dispatcherTestDB(t)
	seedGitHubTask(t, "tracked-pr", db, "gh-pr:Facets-cloud/flow-manager#42")
	client := &fakeGitHubAPIClient{
		commentRows: []githubReviewCommentRecord{
			{NodeID: "PRRC_1", Body: "Please fix docs.", User: githubUser{Login: "reviewer"}},
		},
	}
	p := GitHubPoller{
		DB:         db,
		Client:     client,
		SelfLogins: []string{"me"},
	}

	events, err := p.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(client.commentPRs) != 1 || client.commentPRs[0] != 42 {
		t.Fatalf("comment PR calls = %v, want [42]", client.commentPRs)
	}
	if len(events) != 1 || events[0].Kind != GitHubEventPRReviewComment {
		t.Fatalf("events = %#v", events)
	}
}

func TestGitHubPollerFetchesReviewsForTrackedPRNumber(t *testing.T) {
	db := dispatcherTestDB(t)
	seedGitHubTask(t, "tracked-pr", db, "gh-pr:Facets-cloud/flow-manager#42")
	client := &fakeGitHubAPIClient{
		prs: map[string]githubPullRequestRecord{
			"Facets-cloud/flow-manager#42": {
				State: "open",
			},
		},
		reviewRows: []githubReviewRecord{
			{
				NodeID:      "PRR_kwDOAAABBB",
				State:       "CHANGES_REQUESTED",
				Body:        "Please add a regression test.",
				HTMLURL:     "https://github.com/Facets-cloud/flow-manager/pull/42#pullrequestreview-44",
				User:        githubUser{Login: "reviewer"},
				SubmittedAt: "2026-05-23T10:00:00Z",
			},
		},
	}
	p := GitHubPoller{
		DB:         db,
		Client:     client,
		SelfLogins: []string{"me"},
	}

	events, err := p.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(client.reviewPRs) != 1 || client.reviewPRs[0] != 42 {
		t.Fatalf("review PR calls = %v, want [42]", client.reviewPRs)
	}
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Kind != GitHubEventPRReviewChangesRequested {
		t.Fatalf("kind = %q, want %q", events[0].Kind, GitHubEventPRReviewChangesRequested)
	}
	if events[0].EventKey != "review:PRR_kwDOAAABBB" {
		t.Fatalf("event key = %q", events[0].EventKey)
	}
	if events[0].URL != "https://github.com/Facets-cloud/flow-manager/pull/42#pullrequestreview-44" {
		t.Fatalf("url = %q", events[0].URL)
	}
}

func TestGitHubPollerEmitsHeadUpdatedForTrackedPR(t *testing.T) {
	db := dispatcherTestDB(t)
	seedGitHubTask(t, "tracked-pr", db, "gh-pr:Facets-cloud/flow-manager#42")
	client := &fakeGitHubAPIClient{
		prs: map[string]githubPullRequestRecord{
			"Facets-cloud/flow-manager#42": {
				State: "open",
				Head:  githubRef{Name: "feature/github", SHA: "abc123"},
			},
		},
	}
	p := GitHubPoller{DB: db, Client: client, SelfLogins: []string{"me"}}

	events, err := p.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Kind != GitHubEventPRHeadUpdated || events[0].HeadSHA != "abc123" {
		t.Fatalf("head update event = %#v", events[0])
	}
}

func TestGitHubPollerFetchesIssueCommentsForTrackedPR(t *testing.T) {
	db := dispatcherTestDB(t)
	seedGitHubTask(t, "tracked-pr", db, "gh-pr:Facets-cloud/flow-manager#42")
	client := &fakeGitHubAPIClient{
		prs: map[string]githubPullRequestRecord{
			"Facets-cloud/flow-manager#42": {State: "open"},
		},
		issueCommentRows: []githubIssueCommentRecord{
			{
				NodeID:  "IC_kwDOAAABBB",
				Body:    "Top-level reviewer reply.",
				HTMLURL: "https://github.com/Facets-cloud/flow-manager/pull/42#issuecomment-1",
				User:    githubUser{Login: "reviewer"},
			},
		},
	}
	p := GitHubPoller{DB: db, Client: client, SelfLogins: []string{"me"}}

	events, err := p.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	// Two refs: one from the PR-comments sweep, none for issues (no gh-issue tags).
	if len(client.issueCommentRefs) != 1 || client.issueCommentRefs[0].Number != 42 {
		t.Fatalf("issue-comment calls = %v, want [{Facets-cloud flow-manager 42}]", client.issueCommentRefs)
	}
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Kind != GitHubEventPRComment {
		t.Fatalf("kind = %q, want %q", events[0].Kind, GitHubEventPRComment)
	}
	if events[0].EventKey != "issue-comment:IC_kwDOAAABBB" {
		t.Fatalf("event key = %q", events[0].EventKey)
	}
	if events[0].URL != "https://github.com/Facets-cloud/flow-manager/pull/42#issuecomment-1" {
		t.Fatalf("url = %q", events[0].URL)
	}
	// task_tags rows are normalized to lowercase on write, so when the
	// poller walks them back the parsed owner/repo come out lowercased.
	// That's the production behavior, not a test artifact.
	if events[0].LinkTag() != "gh-pr:facets-cloud/flow-manager#42" {
		t.Fatalf("link tag = %q, want gh-pr:facets-cloud/flow-manager#42", events[0].LinkTag())
	}
}

func TestGitHubPollerFetchesIssueCommentsForTrackedIssue(t *testing.T) {
	db := dispatcherTestDB(t)
	seedGitHubTask(t, "tracked-issue", db, "gh-issue:Facets-cloud/flow-manager#7")
	client := &fakeGitHubAPIClient{
		issueCommentRows: []githubIssueCommentRecord{
			{
				NodeID:  "IC_kwDOAAACCC",
				Body:    "Issue follow-up question.",
				HTMLURL: "https://github.com/Facets-cloud/flow-manager/issues/7#issuecomment-2",
				User:    githubUser{Login: "reporter"},
			},
		},
	}
	p := GitHubPoller{DB: db, Client: client, SelfLogins: []string{"me"}}

	events, err := p.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(client.issueCommentRefs) != 1 || client.issueCommentRefs[0].Number != 7 {
		t.Fatalf("issue-comment calls = %v", client.issueCommentRefs)
	}
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Kind != GitHubEventIssueComment {
		t.Fatalf("kind = %q, want %q", events[0].Kind, GitHubEventIssueComment)
	}
	if events[0].LinkTag() != "gh-issue:facets-cloud/flow-manager#7" {
		t.Fatalf("link tag = %q", events[0].LinkTag())
	}
}

func TestGitHubPollerDeliversSelfAuthoredIssueComments(t *testing.T) {
	db := dispatcherTestDB(t)
	seedGitHubTask(t, "tracked-pr", db, "gh-pr:Facets-cloud/flow-manager#42")
	client := &fakeGitHubAPIClient{
		prs: map[string]githubPullRequestRecord{
			"Facets-cloud/flow-manager#42": {State: "open"},
		},
		issueCommentRows: []githubIssueCommentRecord{
			{NodeID: "IC_self", Body: "fix merge conflicts", User: githubUser{Login: "Me"}},
			{NodeID: "IC_other", Body: "their comment", User: githubUser{Login: "other"}},
		},
	}
	// Operator-authored (self-login) top-level comments MUST be delivered — they
	// are the operator's instruction channel on a monitored PR. SelfLogins still
	// drives the search queries, so it's set, but it no longer drops comments.
	p := GitHubPoller{DB: db, Client: client, SelfLogins: []string{"me"}}

	events, err := p.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	keys := map[string]bool{}
	for _, e := range events {
		keys[e.EventKey] = true
	}
	if !keys["issue-comment:IC_self"] {
		t.Errorf("self-authored top-level comment must be delivered; got %#v", events)
	}
	if !keys["issue-comment:IC_other"] {
		t.Errorf("external top-level comment must be delivered; got %#v", events)
	}
}

func TestGitHubPollerDropsSelfAuthoredAckComments(t *testing.T) {
	db := dispatcherTestDB(t)
	seedGitHubTask(t, "tracked-pr", db, "gh-pr:Facets-cloud/flow-manager#42")
	client := &fakeGitHubAPIClient{
		prs: map[string]githubPullRequestRecord{
			"Facets-cloud/flow-manager#42": {State: "open"},
		},
		issueCommentRows: []githubIssueCommentRecord{
			// Our own agent ack — carries the marker. Must NOT re-wake the session.
			{NodeID: "IC_ack", Body: "On it — reviewing now.\n\n" + agentAckMarker, User: githubUser{Login: "Me"}},
			// Our own real instruction — no marker. Must still be delivered.
			{NodeID: "IC_instr", Body: "fix merge conflicts", User: githubUser{Login: "Me"}},
			// Someone else quoting the marker — not self-authored, must be delivered.
			{NodeID: "IC_ext", Body: "you wrote " + agentAckMarker, User: githubUser{Login: "other"}},
		},
	}
	p := GitHubPoller{DB: db, Client: client, SelfLogins: []string{"me"}}

	events, err := p.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	keys := map[string]bool{}
	for _, e := range events {
		keys[e.EventKey] = true
	}
	if keys["issue-comment:IC_ack"] {
		t.Errorf("self-authored ack comment must be dropped (no echo loop); got %#v", events)
	}
	if !keys["issue-comment:IC_instr"] {
		t.Errorf("self-authored non-ack instruction must still be delivered")
	}
	if !keys["issue-comment:IC_ext"] {
		t.Errorf("external comment containing the marker must still be delivered")
	}
}

func TestGitHubPollerEmitsClosedAndSkipsCommentsForUnmergedClosedTrackedPR(t *testing.T) {
	db := dispatcherTestDB(t)
	seedGitHubTask(t, "tracked-pr", db, "gh-pr:Facets-cloud/flow-manager#42")
	client := &fakeGitHubAPIClient{
		prs: map[string]githubPullRequestRecord{
			"Facets-cloud/flow-manager#42": {
				State:  "closed",
				Merged: false,
				Head:   githubRef{Name: "feature/github", SHA: "abc123"},
			},
		},
		commentRows: []githubReviewCommentRecord{
			{NodeID: "PRRC_1", Body: "stale comment"},
		},
	}
	p := GitHubPoller{DB: db, Client: client, SelfLogins: []string{"me"}}

	events, err := p.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(client.commentPRs) != 0 {
		t.Fatalf("closed PR should not fetch comments, got calls %v", client.commentPRs)
	}
	if len(events) != 1 || events[0].Kind != GitHubEventPRClosed {
		t.Fatalf("events = %#v", events)
	}
}

func TestGitHubPollerEmitsMergedAndSkipsCommentsForMergedTrackedPR(t *testing.T) {
	db := dispatcherTestDB(t)
	seedGitHubTask(t, "tracked-pr", db, "gh-pr:Facets-cloud/flow-manager#42")
	client := &fakeGitHubAPIClient{
		prs: map[string]githubPullRequestRecord{
			"Facets-cloud/flow-manager#42": {
				State:  "closed",
				Merged: true,
				Head:   githubRef{Name: "feature/github", SHA: "abc123"},
			},
		},
		commentRows: []githubReviewCommentRecord{
			{NodeID: "PRRC_1", Body: "stale comment"},
		},
	}
	p := GitHubPoller{DB: db, Client: client, SelfLogins: []string{"me"}}

	events, err := p.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(client.commentPRs) != 0 {
		t.Fatalf("merged PR should not fetch comments, got calls %v", client.commentPRs)
	}
	if len(events) != 1 || events[0].Kind != GitHubEventPRMerged {
		t.Fatalf("events = %#v", events)
	}
}
