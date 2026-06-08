package server

import "testing"

func TestParseGitHubAuthStatus(t *testing.T) {
	t.Run("multi-source dedupes by login and detects env-pinned active", func(t *testing.T) {
		// Real gh 2.93 output: the active login appears twice (GH_TOKEN +
		// keyring) plus a second keyring-only login.
		out := `github.com
  ✓ Logged in to github.com account vishnukv-facets (GH_TOKEN)
  - Active account: true
  - Git operations protocol: ssh
  - Token: gho_****
  - Token scopes: 'repo'

  ✓ Logged in to github.com account vishnukv-facets (keyring)
  - Active account: false

  ✓ Logged in to github.com account vishnukv64 (keyring)
  - Active account: false
`
		accounts, active, source, host := parseGitHubAuthStatus(out)
		if active != "vishnukv-facets" {
			t.Fatalf("active = %q, want vishnukv-facets", active)
		}
		if source != "GH_TOKEN" {
			t.Fatalf("active source = %q, want GH_TOKEN", source)
		}
		if !isEnvTokenSource(source) {
			t.Fatalf("GH_TOKEN should be env-pinned")
		}
		if host != "github.com" {
			t.Fatalf("host = %q", host)
		}
		if len(accounts) != 2 {
			t.Fatalf("want 2 deduped accounts, got %d: %+v", len(accounts), accounts)
		}
		if !accounts[0].Active || accounts[0].Login != "vishnukv-facets" {
			t.Fatalf("first account should be active vishnukv-facets, got %+v", accounts[0])
		}
		if accounts[1].Login != "vishnukv64" || accounts[1].Active {
			t.Fatalf("second account should be inactive vishnukv64, got %+v", accounts[1])
		}
	})

	t.Run("keyring-only multi-account is switchable (not env-pinned)", func(t *testing.T) {
		out := `github.com
  ✓ Logged in to github.com account alice (keyring)
  - Active account: true

  ✓ Logged in to github.com account bob (keyring)
  - Active account: false
`
		accounts, active, source, _ := parseGitHubAuthStatus(out)
		if active != "alice" || source != "keyring" {
			t.Fatalf("active=%q source=%q, want alice/keyring", active, source)
		}
		if isEnvTokenSource(source) {
			t.Fatalf("keyring source must not be env-pinned")
		}
		if len(accounts) != 2 {
			t.Fatalf("want 2 accounts, got %d", len(accounts))
		}
	})

	t.Run("legacy single-account 'as' format is active", func(t *testing.T) {
		out := `github.com
  ✓ Logged in to github.com as octocat (oauth_token)
  - Git operations protocol: https
`
		accounts, active, _, _ := parseGitHubAuthStatus(out)
		if active != "octocat" {
			t.Fatalf("active = %q, want octocat", active)
		}
		if len(accounts) != 1 || !accounts[0].Active {
			t.Fatalf("want one active account, got %+v", accounts)
		}
	})

	t.Run("logged-out output yields no active account", func(t *testing.T) {
		out := "You are not logged into any GitHub hosts. To log in, run: gh auth login\n"
		accounts, active, _, _ := parseGitHubAuthStatus(out)
		if active != "" || len(accounts) != 0 {
			t.Fatalf("want no accounts, got active=%q accounts=%+v", active, accounts)
		}
	})
}
