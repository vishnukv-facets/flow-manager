package server

import (
	"errors"
	"os"

	"github.com/zalando/go-keyring"
)

// GitHub App credentials are stored at rest in the OS keyring rather than
// config.json. The PEM private key, OAuth client secret, and webhook signing
// secret never touch disk in plaintext; only non-secret App metadata (app id,
// slug, client id, installation ids) lives in config.json as Hidden settings.
const githubKeyringService = "flow.github"

const (
	keyringAcctWebhookSecret = "webhook_secret"
	keyringAcctAppPEM        = "app_pem"
	keyringAcctClientSecret  = "client_secret"
)

// githubSecretAccounts maps each keyring account to the process env var it
// hydrates. The rest of the code reads os.Getenv at call time (e.g.
// githubWebhookSecret), so loading the keyring into the env once at boot means
// no keychain access on the request hot path.
var githubSecretAccounts = map[string]string{
	keyringAcctWebhookSecret: "FLOW_GH_WEBHOOK_SECRET",
	keyringAcctAppPEM:        "FLOW_GH_APP_PEM",
	keyringAcctClientSecret:  "FLOW_GH_CLIENT_SECRET",
}

// getGitHubSecret reads a secret from the keyring, treating a missing entry as
// an empty value rather than an error.
func getGitHubSecret(account string) (string, error) {
	v, err := keyring.Get(githubKeyringService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	return v, err
}

// setGitHubSecret writes a secret to the keyring only.
func setGitHubSecret(account, value string) error {
	return keyring.Set(githubKeyringService, account, value)
}

// deleteGitHubSecret removes a secret from the keyring, treating an absent
// entry as success.
func deleteGitHubSecret(account string) error {
	err := keyring.Delete(githubKeyringService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// storeGitHubSecret persists a secret to the keyring AND exports it to the
// process env so it takes effect live (the webhook handler and SDK read env).
// An empty value clears the secret from both.
func storeGitHubSecret(account, value string) error {
	envKey := githubSecretAccounts[account]
	if value == "" {
		if err := deleteGitHubSecret(account); err != nil {
			return err
		}
		if envKey != "" {
			os.Unsetenv(envKey)
		}
		return nil
	}
	if err := setGitHubSecret(account, value); err != nil {
		return err
	}
	if envKey != "" {
		os.Setenv(envKey, value)
	}
	return nil
}

// loadGitHubSecretsFromKeyring hydrates the process env from the keyring at
// boot. It is called after applyConfigToEnv, so a keyring-stored secret takes
// precedence over any config.json / shell value (the keyring is the
// authoritative at-rest store for App credentials). An absent keyring entry
// leaves the env untouched, preserving the back-compat env/config fallback.
func loadGitHubSecretsFromKeyring() {
	for account, envKey := range githubSecretAccounts {
		v, err := getGitHubSecret(account)
		if err != nil || v == "" {
			continue
		}
		os.Setenv(envKey, v)
	}
}
