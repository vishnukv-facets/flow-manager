package monitor

import (
	"os"
	"strings"
)

// GitHubTransportMode selects how GitHub events reach Flow.
type GitHubTransportMode string

const (
	// GitHubTransportOff: no GitHub event ingress.
	GitHubTransportOff GitHubTransportMode = "off"
	// GitHubTransportWebhook: signed webhook deliveries are the live path; the
	// scheduled poller does not run (it stays available for manual backfill).
	GitHubTransportWebhook GitHubTransportMode = "webhook"
	// GitHubTransportPolling: legacy scheduled gh-api search polling (no webhook).
	GitHubTransportPolling GitHubTransportMode = "polling"
	// GitHubTransportHybrid: webhook receiver active AND the legacy search-poller
	// also runs — useful for discovering @-mentions / involvement in repos where
	// no webhook is installed (GitHub has no user-level webhook for those). NOTE:
	// the search-poller is discovery, NOT gap recovery; true backfill of missed
	// webhook deliveries (replay via GitHub's deliveries API) is part of the
	// Connect-GitHub App work, not this transport.
	GitHubTransportHybrid GitHubTransportMode = "hybrid"
)

// GitHubTransport resolves the configured transport mode. FLOW_GH_TRANSPORT is
// authoritative when set to a known value; otherwise the mode is derived from
// the legacy FLOW_GH_ENABLED flag so existing installs keep their current
// behavior until they explicitly opt into webhook mode.
func GitHubTransport() GitHubTransportMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("FLOW_GH_TRANSPORT"))) {
	case "webhook":
		return GitHubTransportWebhook
	case "polling":
		return GitHubTransportPolling
	case "hybrid":
		return GitHubTransportHybrid
	case "off":
		return GitHubTransportOff
	}
	// Unset or unknown: preserve legacy behavior.
	if GitHubPollingEnabled() {
		return GitHubTransportPolling
	}
	return GitHubTransportOff
}

// SchedulesPolling always reports false: the legacy gh-api search-poller has
// been retired in favor of App-based webhook ingress. The mode constants are
// retained for config back-compat (and the off/on summary), but no mode starts
// a scheduled poll loop anymore.
func (m GitHubTransportMode) SchedulesPolling() bool {
	return false
}
