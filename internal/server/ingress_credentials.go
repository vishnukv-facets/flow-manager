package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
)

// generateWebhookSecret returns a fresh 32-byte random secret, hex-encoded
// (64 chars). It is used as the GitHub webhook HMAC signing secret
// (X-Hub-Signature-256) and only needs to be a high-entropy opaque string.
func generateWebhookSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// generateZrokShareName returns a fresh reserved-share unique name accepted by
// zrok, which requires it to match ^[a-z0-9]{4,32}$ and be non-profane (see
// openziti/zrok util.IsValidUniqueName). "flow" + 12 hex chars (16 total) is
// always lowercase-alphanumeric, stays inside the length window, and — being
// hex — can't spell a word the zrok server's profanity filter would reject.
func generateZrokShareName() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "flow" + hex.EncodeToString(b), nil
}

// ensureZrokIngressCredentials lazily provisions and PERSISTS the two pieces of
// durable state the zrok GitHub-webhook ingress depends on, so neither one
// churns across restarts:
//
//   - FLOW_GH_WEBHOOK_SECRET — the HMAC secret GitHub signs deliveries with.
//     Without it the share refuses to start (see zrokManager.start), which is
//     the "GitHub webhook secret required before public ingress can start"
//     error operators hit when they enable zrok without setting a secret first.
//   - FLOW_ZROK_SHARE_NAME — the reserved-share unique name that pins a STABLE
//     public URL. An unnamed (ephemeral) share is handed a brand-new URL every
//     boot, forcing the GitHub webhook to be re-registered after each restart.
//
// Each value is generated exactly once — the first time zrok auto-start ingress
// is enabled while the value is still empty — then written to config.json. On
// every later boot applyConfigToEnv repopulates them from config, this function
// observes them already set, and does nothing. Rotation is therefore explicit
// only (see rotateWebhookSecret / rotateZrokShareName), never a side effect of a
// restart.
//
// Gated on the same condition as zrokManager.start, so credentials are never
// minted for an ingress that isn't actually turned on. An operator who supplied
// their own secret or share name keeps it — generation only fills genuine gaps.
func (s *Server) ensureZrokIngressCredentials() {
	if activeIngressProvider() != ingressProviderZrok || !zrokAutoStart() {
		return
	}
	path := s.configPath()
	cfg := loadConfigFile(path)
	changed := false

	if githubWebhookSecret() == "" {
		if secret, err := generateWebhookSecret(); err == nil {
			cfg["FLOW_GH_WEBHOOK_SECRET"] = secret
			os.Setenv("FLOW_GH_WEBHOOK_SECRET", secret)
			changed = true
		}
	}
	if strings.TrimSpace(os.Getenv("FLOW_ZROK_SHARE_NAME")) == "" {
		if name, err := generateZrokShareName(); err == nil {
			cfg["FLOW_ZROK_SHARE_NAME"] = name
			os.Setenv("FLOW_ZROK_SHARE_NAME", name)
			changed = true
		}
	}
	if changed && path != "" {
		_ = saveConfigFile(path, cfg)
	}
}

// persistIngressSetting writes a single ingress key to config.json and exports
// it to the process env so the running listeners pick it up immediately. When
// FLOW_ROOT is unset (tests, or no config dir) it updates the env only.
func (s *Server) persistIngressSetting(key, val string) error {
	os.Setenv(key, val)
	path := s.configPath()
	if path == "" {
		return nil
	}
	cfg := loadConfigFile(path)
	cfg[key] = val
	return saveConfigFile(path, cfg)
}

// restartIngress bounces the zrok manager so newly-persisted ingress config
// (secret, share name, provider) takes effect. No-op when the new config
// disables the share — start() self-gates on provider + auto-start.
func (s *Server) restartIngress() {
	if s.zrok != nil {
		s.zrok.stop()
		s.zrok.start()
	}
}

// rotateWebhookSecret mints a new GitHub webhook signing secret on explicit
// operator request, persists it, and bounces ingress. The operator must copy
// the new secret into their GitHub webhook settings afterward, or deliveries
// will start failing signature verification.
func (s *Server) rotateWebhookSecret() (actionResponse, int) {
	secret, err := generateWebhookSecret()
	if err != nil {
		return actionResponse{OK: false, Message: "generate webhook secret: " + err.Error()}, http.StatusInternalServerError
	}
	if err := s.persistIngressSetting("FLOW_GH_WEBHOOK_SECRET", secret); err != nil {
		return actionResponse{OK: false, Message: "save webhook secret: " + err.Error()}, http.StatusInternalServerError
	}
	s.restartIngress()
	s.publishUIChange("settings")
	// Output carries the new secret so the UI can copy it straight to the
	// clipboard for pasting into GitHub — it is never shown again afterward.
	return actionResponse{OK: true, Message: "GitHub webhook secret rotated — paste the copied secret into your GitHub webhook settings", Output: secret}, http.StatusOK
}

// revealWebhookSecret returns the current GitHub webhook signing secret so the
// operator can copy it into their GitHub webhook configuration. The value is
// generated by flow, so this is the only way to retrieve it; it is returned via
// an explicit POST action (never the polling status GET) and only over the
// localhost-only data plane.
func (s *Server) revealWebhookSecret() (actionResponse, int) {
	secret := githubWebhookSecret()
	if secret == "" {
		return actionResponse{OK: false, Message: "no GitHub webhook secret is configured yet"}, http.StatusNotFound
	}
	// Empty Message suppresses the generic success toast; the UI confirms the
	// clipboard copy itself.
	return actionResponse{OK: true, Output: secret}, http.StatusOK
}

// rotateZrokShareName mints a new reserved-share name (hence a new public URL)
// on explicit operator request, releases the previous reserved share so its URL
// stops resolving, and bounces ingress to establish the new one. Only valid for
// the zrok provider.
func (s *Server) rotateZrokShareName() (actionResponse, int) {
	if activeIngressProvider() != ingressProviderZrok {
		return actionResponse{OK: false, Message: "public URL rotation applies only to the zrok ingress provider"}, http.StatusBadRequest
	}
	name, err := generateZrokShareName()
	if err != nil {
		return actionResponse{OK: false, Message: "generate share name: " + err.Error()}, http.StatusInternalServerError
	}
	oldName := strings.TrimSpace(os.Getenv("FLOW_ZROK_SHARE_NAME"))
	if err := s.persistIngressSetting("FLOW_ZROK_SHARE_NAME", name); err != nil {
		return actionResponse{OK: false, Message: "save share name: " + err.Error()}, http.StatusInternalServerError
	}
	// Tear down the old listener first, release the now-orphaned reserved share
	// in the background (best-effort, network-bound), then bring up the new one.
	if s.zrok != nil {
		s.zrok.stop()
		if oldName != "" && oldName != name {
			go s.zrok.releaseReservedShare(oldName)
		}
		s.zrok.start()
	}
	s.publishUIChange("settings")
	return actionResponse{OK: true, Message: "public URL rotation started — the new URL will appear shortly; update your GitHub webhook to match"}, http.StatusOK
}
