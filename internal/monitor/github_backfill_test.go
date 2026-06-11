package monitor

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// prAssignedPayload is a minimal but valid pull_request "assigned" webhook body
// the normalizer turns into one GitHubEventPRAssigned.
func prAssignedPayload(number int) string {
	return fmt.Sprintf(`{"action":"assigned","repository":{"full_name":"o/r"},`+
		`"pull_request":{"number":%d,"title":"T","html_url":"https://x/%d","user":{"login":"me"},`+
		`"head":{"ref":"h","sha":"deadbeef"},"base":{"ref":"main"}}}`, number, number)
}

// githubDeliveriesStub serves the App hook-deliveries API: a list of two
// deliveries and their full payloads.
func githubDeliveriesStub(t *testing.T) string {
	t.Helper()
	useMockHTTPTransport(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/app/hook/deliveries" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`[{"id":1,"guid":"g1","event":"pull_request","action":"assigned"},` +
				`{"id":2,"guid":"g2","event":"issues","action":"opened"}]`))
		case r.URL.Path == "/app/hook/deliveries/1":
			fmt.Fprintf(w, `{"id":1,"guid":"g1","event":"pull_request","action":"assigned","request":{"payload":%s}}`, prAssignedPayload(5))
		case r.URL.Path == "/app/hook/deliveries/2":
			// issues "opened" → the normalizer ignores it (0 events).
			_, _ = w.Write([]byte(`{"id":2,"guid":"g2","event":"issues","action":"opened","request":{"payload":{"action":"opened","repository":{"full_name":"o/r"},"issue":{"number":9}}}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	return "https://github.test"
}

func TestNewGitHubAppClient_NoAppReturnsNotOK(t *testing.T) {
	t.Setenv("FLOW_GH_APP_ID", "")
	t.Setenv("FLOW_GH_APP_PEM", "")
	if _, ok, err := newGitHubAppClient(); ok || err != nil {
		t.Fatalf("want (false,nil) without an App; got ok=%v err=%v", ok, err)
	}
}

func TestBackfillGitHubDeliveries_ReplaysAndDedupes(t *testing.T) {
	baseURL := githubDeliveriesStub(t)
	t.Setenv("FLOW_GH_APP_ID", "123")
	t.Setenv("FLOW_GH_APP_PEM", testRSAKeyPEM(t))
	t.Setenv("FLOW_GH_API_BASE_URL", baseURL)

	db := dispatcherTestDB(t)
	var dispatched []GitHubEvent
	dispatch := func(_ context.Context, ev GitHubEvent) error {
		dispatched = append(dispatched, ev)
		return nil
	}

	n, err := BackfillGitHubDeliveries(context.Background(), db, dispatch)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	// Delivery 1 (pull_request assigned) replays one event; delivery 2 (issues
	// opened) normalizes to nothing.
	if n != 1 {
		t.Errorf("replayed = %d, want 1", n)
	}
	if len(dispatched) != 1 || dispatched[0].Kind != GitHubEventPRAssigned || dispatched[0].Number != 5 {
		t.Fatalf("dispatched = %+v, want one pr_assigned #5", dispatched)
	}

	// Re-running is idempotent — both delivery GUIDs are already recorded.
	dispatched = nil
	n2, err := BackfillGitHubDeliveries(context.Background(), db, dispatch)
	if err != nil {
		t.Fatalf("backfill rerun: %v", err)
	}
	if n2 != 0 || len(dispatched) != 0 {
		t.Errorf("rerun replayed = %d, dispatched = %d, want 0/0 (dedupe)", n2, len(dispatched))
	}
}

func TestBackfillGitHubDeliveries_NoAppIsNoOp(t *testing.T) {
	t.Setenv("FLOW_GH_APP_ID", "")
	t.Setenv("FLOW_GH_APP_PEM", "")
	db := dispatcherTestDB(t)
	n, err := BackfillGitHubDeliveries(context.Background(), db, func(context.Context, GitHubEvent) error { return nil })
	if err != nil || n != 0 {
		t.Fatalf("want (0,nil) with no App; got n=%d err=%v", n, err)
	}
}
