package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"flow/internal/monitor"
)

// githubSetupCallbackPath is the redirect_url path GitHub sends the manifest
// conversion code to. It is served on the public ingress mux (the manifest
// requires a public redirect) as well as locally.
const githubSetupCallbackPath = "/api/github/setup/callback"

// githubManifestTTL bounds how long a started setup remains valid before the
// state nonce is rejected.
const githubManifestTTL = 30 * time.Minute

// githubManifestPending is the server-side state of one in-flight Connect
// GitHub attempt.
type githubManifestPending struct {
	state   string
	target  string
	org     string
	created time.Time
}

func randomGitHubState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// pendingGitHubSetupState returns the state nonce of the in-flight setup, or ""
// when none is active. Exposed for tests.
func (s *Server) pendingGitHubSetupState() string {
	s.githubSetupMu.Lock()
	defer s.githubSetupMu.Unlock()
	if s.githubSetup == nil {
		return ""
	}
	return s.githubSetup.state
}

// ---------------------------------------------------------------------------
// Status
// ---------------------------------------------------------------------------

// GitHubSetupStatus drives the resumable Connect-GitHub wizard card. Every
// field is read from the process env (hydrated from config.json + the keyring
// at boot), so polling it never touches the keychain.
type GitHubSetupStatus struct {
	IngressReady     bool   `json:"ingress_ready"`
	WebhookURL       string `json:"webhook_url,omitempty"`
	RedirectURL      string `json:"redirect_url,omitempty"`
	AppCreated       bool   `json:"app_created"`
	AppID            string `json:"app_id,omitempty"`
	AppSlug          string `json:"app_slug,omitempty"`
	HTMLURL          string `json:"html_url,omitempty"`
	PemSet           bool   `json:"pem_set"`
	WebhookSecretSet bool   `json:"webhook_secret_set"`
	InstallURL       string `json:"install_url,omitempty"`
	InstallationIDs  string `json:"installation_ids,omitempty"`
	Installed        bool   `json:"installed"`
	Transport        string `json:"transport"`
	Summary          string `json:"summary"`
}

func (s *Server) githubSetupStatus() GitHubSetupStatus {
	appID := strings.TrimSpace(os.Getenv("FLOW_GH_APP_ID"))
	slug := strings.TrimSpace(os.Getenv("FLOW_GH_APP_SLUG"))
	installs := strings.TrimSpace(os.Getenv("FLOW_GH_INSTALLATION_IDS"))
	st := GitHubSetupStatus{
		IngressReady:     s.publicBaseURL() != "",
		WebhookURL:       s.connectorCallbackURL("/api/github/webhook"),
		RedirectURL:      s.connectorCallbackURL(githubSetupCallbackPath),
		AppCreated:       appID != "" && os.Getenv("FLOW_GH_APP_PEM") != "",
		AppID:            appID,
		AppSlug:          slug,
		HTMLURL:          strings.TrimSpace(os.Getenv("FLOW_GH_HTML_URL")),
		PemSet:           strings.TrimSpace(os.Getenv("FLOW_GH_APP_PEM")) != "",
		WebhookSecretSet: githubWebhookSecret() != "",
		InstallationIDs:  installs,
		Installed:        installs != "",
		Transport:        string(monitor.GitHubTransport()),
	}
	if slug != "" {
		st.InstallURL = "https://github.com/apps/" + url.PathEscape(slug) + "/installations/new"
	}
	st.Summary = githubSetupSummary(st)
	return st
}

func githubSetupSummary(st GitHubSetupStatus) string {
	switch {
	case !st.IngressReady:
		return "Start public ingress first — the App's webhook needs a public URL"
	case !st.AppCreated:
		return "Create a GitHub App to connect"
	case !st.Installed:
		return "App created — install it on your account or org"
	default:
		return "Connected — receiving GitHub webhooks"
	}
}

func (s *Server) handleGitHubSetupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.githubSetupStatus())
}

// ---------------------------------------------------------------------------
// create-app
// ---------------------------------------------------------------------------

func (s *Server) handleGitHubSetupCreateApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name   string `json:"name"`
		Target string `json:"target"` // "user" | "org"
		Org    string `json:"org"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONStatus(w, map[string]any{"ok": false, "error": "invalid JSON body"}, http.StatusBadRequest)
		return
	}

	// The manifest's hook_attributes.url must be a public URL at App-creation
	// time, so a running public ingress is a hard prerequisite.
	webhookURL := s.connectorCallbackURL("/api/github/webhook")
	redirectURL := s.connectorCallbackURL(githubSetupCallbackPath)
	if webhookURL == "" || redirectURL == "" {
		writeJSONStatus(w, map[string]any{"ok": false, "error": "public ingress is not running — enable it first so GitHub can reach the webhook + setup callback"}, http.StatusServiceUnavailable)
		return
	}

	state := randomGitHubState()
	createURL, err := githubManifestCreateURL(req.Target, req.Org, state)
	if err != nil {
		writeJSONStatus(w, map[string]any{"ok": false, "error": err.Error()}, http.StatusBadRequest)
		return
	}

	s.githubSetupMu.Lock()
	s.githubSetup = &githubManifestPending{state: state, target: req.Target, org: strings.TrimSpace(req.Org), created: time.Now()}
	s.githubSetupMu.Unlock()

	writeJSON(w, map[string]any{
		"ok":         true,
		"state":      state,
		"create_url": createURL,
		"manifest":   githubAppManifest(req.Name, webhookURL, redirectURL),
	})
}

// ---------------------------------------------------------------------------
// callback — manifest conversion
// ---------------------------------------------------------------------------

func (s *Server) handleGitHubSetupCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))

	// Post-install redirect (App setup_url): carries installation_id, not a
	// manifest code or our state nonce. Capture the installation so the SDK can
	// mint tokens for it, then we're done.
	if code == "" {
		if instID := strings.TrimSpace(r.URL.Query().Get("installation_id")); instID != "" {
			s.captureInstallationID(instID)
			s.publishUIChange("github-setup")
			writeSetupResultHTML(w, "GitHub App installed ✅", "Flow is now connected and receiving webhooks. You can close this tab and return to Mission Control.")
			return
		}
	}

	s.githubSetupMu.Lock()
	pending := s.githubSetup
	s.githubSetupMu.Unlock()

	if pending == nil || state == "" || state != pending.state {
		writeSetupResultHTML(w, "Couldn't verify the setup request", "The state nonce did not match or the setup expired. Close this tab and start Connect GitHub again.")
		return
	}
	if time.Since(pending.created) > githubManifestTTL {
		s.clearGitHubSetup()
		writeSetupResultHTML(w, "Setup expired", "Too much time passed before GitHub returned. Close this tab and start Connect GitHub again.")
		return
	}
	if code == "" {
		writeSetupResultHTML(w, "No code returned", "GitHub didn't return a manifest code. Close this tab and try Connect GitHub again.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	conv, err := newGitHubSetupAPI().convertManifest(ctx, code)
	if err != nil {
		writeSetupResultHTML(w, "GitHub setup failed", html.EscapeString(err.Error()))
		return
	}
	if err := s.persistGitHubApp(conv); err != nil {
		writeSetupResultHTML(w, "Couldn't save the GitHub App", html.EscapeString(err.Error()))
		return
	}
	s.clearGitHubSetup()
	s.publishUIChange("github-setup")

	installURL := "https://github.com/apps/" + url.PathEscape(conv.Slug) + "/installations/new"
	writeSetupResultHTML(w, "GitHub App created 🎉",
		fmt.Sprintf("Flow now owns the App <b>%s</b>. Next, install it on your account or org: <a href=%q>Install the App</a>. You can close this tab and return to Mission Control.",
			html.EscapeString(conv.Slug), installURL))
}

// handleGitHubSetupBackfill replays GitHub App webhook deliveries missed while
// Flow / the public ingress was down — the correct gap-recovery path (redelivery
// replay, not re-polling). Idempotent: already-processed deliveries are skipped.
func (s *Server) handleGitHubSetupBackfill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.DB == nil || s.githubListener == nil {
		writeJSONStatus(w, map[string]any{"ok": false, "error": "GitHub ingress is not initialized"}, http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	n, err := monitor.BackfillGitHubDeliveries(ctx, s.cfg.DB, s.githubListener.Dispatch)
	if err != nil {
		writeJSONStatus(w, map[string]any{"ok": false, "error": err.Error()}, http.StatusBadGateway)
		return
	}
	if n > 0 {
		s.publishUIChange("github-setup")
	}
	writeJSON(w, map[string]any{"ok": true, "replayed": n})
}

func (s *Server) clearGitHubSetup() {
	s.githubSetupMu.Lock()
	s.githubSetup = nil
	s.githubSetupMu.Unlock()
}

// captureInstallationID appends an installation id to FLOW_GH_INSTALLATION_IDS
// (a comma-separated, order-preserving, deduped list) and persists it to
// config + env. One App can be installed on several accounts/orgs, so the list
// grows; the SDK mints tokens per installation.
func (s *Server) captureInstallationID(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	merged := mergeInstallationIDs(os.Getenv("FLOW_GH_INSTALLATION_IDS"), id)
	os.Setenv("FLOW_GH_INSTALLATION_IDS", merged)
	cfg := loadConfigFile(s.configPath())
	cfg["FLOW_GH_INSTALLATION_IDS"] = merged
	_ = saveConfigFile(s.configPath(), cfg)
}

// mergeInstallationIDs unions a new id into an existing comma-separated list,
// preserving order and dropping duplicates.
func mergeInstallationIDs(existing, add string) string {
	var ids []string
	seen := map[string]bool{}
	for _, v := range strings.Split(existing+","+add, ",") {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		ids = append(ids, v)
	}
	return strings.Join(ids, ",")
}

// persistGitHubApp stores the converted App credentials: secrets (PEM, client
// secret, webhook secret) go to the OS keyring; non-secret metadata to
// config.json. It flips the transport to webhook (the poller stays off) and
// bounces the listener + ingress so the new signing secret takes effect live.
func (s *Server) persistGitHubApp(conv githubManifestConversion) error {
	if err := storeGitHubSecret(keyringAcctAppPEM, conv.PEM); err != nil {
		return fmt.Errorf("store App private key: %w", err)
	}
	if err := storeGitHubSecret(keyringAcctWebhookSecret, conv.WebhookSecret); err != nil {
		return fmt.Errorf("store webhook secret: %w", err)
	}
	if err := storeGitHubSecret(keyringAcctClientSecret, conv.ClientSecret); err != nil {
		return fmt.Errorf("store client secret: %w", err)
	}

	cfg := loadConfigFile(s.configPath())
	cfg["FLOW_GH_APP_ID"] = strconv.FormatInt(conv.AppID, 10)
	cfg["FLOW_GH_APP_SLUG"] = conv.Slug
	cfg["FLOW_GH_CLIENT_ID"] = conv.ClientID
	cfg["FLOW_GH_HTML_URL"] = conv.HTMLURL
	// Webhook-first: the App's webhook delivers events, so stop the poller.
	cfg["FLOW_GH_TRANSPORT"] = "webhook"
	for k, v := range cfg {
		if strings.HasPrefix(k, "FLOW_GH_APP_") || k == "FLOW_GH_CLIENT_ID" || k == "FLOW_GH_HTML_URL" || k == "FLOW_GH_TRANSPORT" {
			os.Setenv(k, v)
		}
	}
	if err := saveConfigFile(s.configPath(), cfg); err != nil {
		return fmt.Errorf("save App metadata: %w", err)
	}

	// Bounce the GitHub listener + ingress so the new transport + webhook
	// secret take effect without a restart.
	if s.githubListener != nil {
		s.githubListener.Stop()
		_ = s.githubListener.Start()
	}
	s.restartIngress()
	return nil
}

// writeSetupResultHTML renders a minimal standalone result page for the OAuth /
// manifest callbacks, which open in the operator's browser on the public
// ingress (which serves no UI). Mirrors the Slack callback's success page.
func writeSetupResultHTML(w http.ResponseWriter, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><title>%s</title>
<style>body{font:16px -apple-system,system-ui,sans-serif;max-width:36rem;margin:4rem auto;padding:0 1.5rem;color:#1a1a1a}h1{font-size:1.4rem}a{color:#2563eb}</style>
</head><body><h1>%s</h1><p>%s</p></body></html>`, html.EscapeString(title), html.EscapeString(title), body)
}
