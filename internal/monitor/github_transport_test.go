package monitor

import "testing"

// TestGitHubTransport pins how the transport mode resolves, including the
// back-compat derivation: with FLOW_GH_TRANSPORT unset, an installation keeps
// its current behavior (polling iff legacy FLOW_GH_ENABLED is on).
func TestGitHubTransport(t *testing.T) {
	cases := []struct {
		name      string
		transport string
		enabled   string
		logins    string
		want      GitHubTransportMode
	}{
		{"explicit webhook", "webhook", "0", "", GitHubTransportWebhook},
		{"explicit polling", "polling", "0", "", GitHubTransportPolling},
		{"explicit off", "off", "1", "me", GitHubTransportOff},
		{"explicit hybrid", "hybrid", "0", "", GitHubTransportHybrid},
		{"derive polling from legacy enabled", "", "1", "me", GitHubTransportPolling},
		{"derive off when legacy disabled", "", "0", "", GitHubTransportOff},
		{"unknown falls back to derived", "bogus", "0", "", GitHubTransportOff},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("FLOW_GH_TRANSPORT", c.transport)
			t.Setenv("FLOW_GH_ENABLED", c.enabled)
			t.Setenv("FLOW_GH_SELF_LOGINS", c.logins)
			if got := GitHubTransport(); got != c.want {
				t.Errorf("GitHubTransport() = %q, want %q", got, c.want)
			}
		})
	}
}

// The search-poller has been retired in favor of App-based webhook ingress, so
// NO transport mode schedules a poll loop anymore — even the legacy
// polling/hybrid values (kept only for config back-compat) resolve to no
// scheduled polling.
func TestGitHubTransportSchedulesPolling(t *testing.T) {
	for _, m := range []GitHubTransportMode{
		GitHubTransportPolling, GitHubTransportHybrid, GitHubTransportWebhook, GitHubTransportOff,
	} {
		if m.SchedulesPolling() {
			t.Errorf("%s must not schedule the poll loop — the search-poller is retired", m)
		}
	}
}

// TestGitHubListener_StartNoOpInWebhookMode: even with the legacy enable flags
// set, webhook transport must not start the scheduled poller — the webhook
// receiver is the live path; the poller stays available only for manual backfill.
func TestGitHubListener_StartNoOpInWebhookMode(t *testing.T) {
	t.Setenv("FLOW_GH_TRANSPORT", "webhook")
	t.Setenv("FLOW_GH_ENABLED", "1")
	t.Setenv("FLOW_GH_SELF_LOGINS", "me")

	l := NewGitHubListener(NewGitHubDispatcher(nil, nil))
	if err := l.Start(); err != nil {
		t.Fatalf("Start err = %v", err)
	}
	l.mu.Lock()
	running := l.running
	l.mu.Unlock()
	if running {
		t.Fatal("scheduled poll loop must not run in webhook transport mode")
	}
	l.Stop()
}
