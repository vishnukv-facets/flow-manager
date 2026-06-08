package server

import (
	"os"
	"regexp"
	"testing"
)

// zrokUniqueNameRe mirrors openziti/zrok util.IsValidUniqueName: lowercase
// alphanumeric, 4-32 chars. A generated share name that fails this would be
// rejected by zrok's CreateShare at runtime.
var zrokUniqueNameRe = regexp.MustCompile(`^[a-z0-9]{4,32}$`)

func TestGenerateZrokShareName_Valid(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		name, err := generateZrokShareName()
		if err != nil {
			t.Fatalf("generateZrokShareName: %v", err)
		}
		if !zrokUniqueNameRe.MatchString(name) {
			t.Fatalf("share name %q does not match zrok unique-name rule", name)
		}
		if seen[name] {
			t.Fatalf("share name %q repeated — not random", name)
		}
		seen[name] = true
	}
}

func TestGenerateWebhookSecret_HexAndUnique(t *testing.T) {
	a, err := generateWebhookSecret()
	if err != nil {
		t.Fatalf("generateWebhookSecret: %v", err)
	}
	b, err := generateWebhookSecret()
	if err != nil {
		t.Fatalf("generateWebhookSecret: %v", err)
	}
	if len(a) != 64 { // 32 bytes hex-encoded
		t.Errorf("secret length = %d, want 64", len(a))
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(a) {
		t.Errorf("secret %q is not 64 hex chars", a)
	}
	if a == b {
		t.Error("two generated secrets are identical — not random")
	}
}

// TestEnsureZrokIngressCredentials_Disabled verifies credentials are never
// minted when zrok auto-start ingress isn't actually enabled.
func TestEnsureZrokIngressCredentials_Disabled(t *testing.T) {
	cases := []struct{ provider, autoStart string }{
		{"", ""},        // no ingress
		{"manual", "1"}, // manual provider
		{"zrok", ""},    // zrok but auto-start off
		{"none", "1"},   // explicit none
	}
	for _, c := range cases {
		root, db := testRootDB(t)
		srv := New(Config{DB: db, FlowRoot: root, CommandPath: "/bin/false"})
		clearIngressEnv(t)
		t.Setenv("FLOW_INGRESS_PROVIDER", c.provider)
		t.Setenv("FLOW_ZROK_AUTO_START", c.autoStart)

		srv.ensureZrokIngressCredentials()

		if got := githubWebhookSecret(); got != "" {
			t.Errorf("provider=%q autoStart=%q: secret minted unexpectedly: %q", c.provider, c.autoStart, got)
		}
		if got := os.Getenv("FLOW_ZROK_SHARE_NAME"); got != "" {
			t.Errorf("provider=%q autoStart=%q: share name minted unexpectedly: %q", c.provider, c.autoStart, got)
		}
	}
}

// TestEnsureZrokIngressCredentials_GeneratesAndPersists is the core fix: when
// zrok ingress is enabled with no secret/share name, both are generated,
// exported to env, and written to config.json so they survive a restart.
func TestEnsureZrokIngressCredentials_GeneratesAndPersists(t *testing.T) {
	root, db := testRootDB(t)
	srv := New(Config{DB: db, FlowRoot: root, CommandPath: "/bin/false"})
	clearIngressEnv(t)
	t.Setenv("FLOW_INGRESS_PROVIDER", "zrok")
	t.Setenv("FLOW_ZROK_AUTO_START", "1")

	srv.ensureZrokIngressCredentials()

	secret := githubWebhookSecret()
	name := os.Getenv("FLOW_ZROK_SHARE_NAME")
	if secret == "" {
		t.Fatal("webhook secret was not generated")
	}
	if !zrokUniqueNameRe.MatchString(name) {
		t.Fatalf("share name %q invalid", name)
	}

	// Persisted to config.json (so a restart reloads them).
	cfg := loadConfigFile(srv.configPath())
	if cfg["FLOW_GH_WEBHOOK_SECRET"] != secret {
		t.Errorf("secret not persisted: cfg=%q env=%q", cfg["FLOW_GH_WEBHOOK_SECRET"], secret)
	}
	if cfg["FLOW_ZROK_SHARE_NAME"] != name {
		t.Errorf("share name not persisted: cfg=%q env=%q", cfg["FLOW_ZROK_SHARE_NAME"], name)
	}
}

// TestEnsureZrokIngressCredentials_StableAcrossRestart is the "don't rotate on
// restart" guarantee: once generated, the values never change on subsequent
// ensure calls — including a simulated reboot that reloads them from config.
func TestEnsureZrokIngressCredentials_StableAcrossRestart(t *testing.T) {
	root, db := testRootDB(t)
	srv := New(Config{DB: db, FlowRoot: root, CommandPath: "/bin/false"})
	clearIngressEnv(t)
	t.Setenv("FLOW_INGRESS_PROVIDER", "zrok")
	t.Setenv("FLOW_ZROK_AUTO_START", "1")

	srv.ensureZrokIngressCredentials()
	secret1 := githubWebhookSecret()
	name1 := os.Getenv("FLOW_ZROK_SHARE_NAME")

	// Same process, called again (e.g. a second settings save) — unchanged.
	srv.ensureZrokIngressCredentials()
	if githubWebhookSecret() != secret1 || os.Getenv("FLOW_ZROK_SHARE_NAME") != name1 {
		t.Fatal("ensure mutated already-set credentials")
	}

	// Simulate a restart: wipe env, reload config into env, ensure again.
	os.Unsetenv("FLOW_GH_WEBHOOK_SECRET")
	os.Unsetenv("FLOW_ZROK_SHARE_NAME")
	srv.applyConfigToEnv()
	srv.ensureZrokIngressCredentials()

	if got := githubWebhookSecret(); got != secret1 {
		t.Errorf("secret rotated across restart: was %q now %q", secret1, got)
	}
	if got := os.Getenv("FLOW_ZROK_SHARE_NAME"); got != name1 {
		t.Errorf("share name rotated across restart: was %q now %q", name1, got)
	}
}

// TestEnsureZrokIngressCredentials_KeepsOperatorValues verifies an
// operator-supplied secret or share name is never overwritten by generation.
func TestEnsureZrokIngressCredentials_KeepsOperatorValues(t *testing.T) {
	root, db := testRootDB(t)
	srv := New(Config{DB: db, FlowRoot: root, CommandPath: "/bin/false"})
	clearIngressEnv(t)
	t.Setenv("FLOW_INGRESS_PROVIDER", "zrok")
	t.Setenv("FLOW_ZROK_AUTO_START", "1")
	t.Setenv("FLOW_GH_WEBHOOK_SECRET", "operator-chosen-secret")
	t.Setenv("FLOW_ZROK_SHARE_NAME", "myshare01")

	srv.ensureZrokIngressCredentials()

	if got := githubWebhookSecret(); got != "operator-chosen-secret" {
		t.Errorf("operator secret overwritten: %q", got)
	}
	if got := os.Getenv("FLOW_ZROK_SHARE_NAME"); got != "myshare01" {
		t.Errorf("operator share name overwritten: %q", got)
	}
}

// TestRotateWebhookSecret rotates only on explicit request and persists.
func TestRotateWebhookSecret(t *testing.T) {
	root, db := testRootDB(t)
	srv := New(Config{DB: db, FlowRoot: root, CommandPath: "/bin/false"})
	clearIngressEnv(t)
	t.Setenv("FLOW_INGRESS_PROVIDER", "zrok") // auto-start off → restartIngress is a no-op
	t.Setenv("FLOW_GH_WEBHOOK_SECRET", "before")

	resp, code := srv.rotateWebhookSecret()
	if code != 200 || !resp.OK {
		t.Fatalf("rotate: code=%d resp=%+v", code, resp)
	}
	after := githubWebhookSecret()
	if after == "before" || after == "" {
		t.Fatalf("secret not rotated: %q", after)
	}
	if cfg := loadConfigFile(srv.configPath()); cfg["FLOW_GH_WEBHOOK_SECRET"] != after {
		t.Errorf("rotated secret not persisted: %q vs %q", cfg["FLOW_GH_WEBHOOK_SECRET"], after)
	}
}

// TestRotateZrokShareName rotates only for zrok and rejects other providers.
func TestRotateZrokShareName(t *testing.T) {
	t.Run("zrok rotates and persists", func(t *testing.T) {
		root, db := testRootDB(t)
		srv := New(Config{DB: db, FlowRoot: root, CommandPath: "/bin/false"})
		clearIngressEnv(t)
		t.Setenv("FLOW_INGRESS_PROVIDER", "zrok") // auto-start off → start() no-op
		t.Setenv("FLOW_ZROK_SHARE_NAME", "flowold0001")

		resp, code := srv.rotateZrokShareName()
		if code != 200 || !resp.OK {
			t.Fatalf("rotate: code=%d resp=%+v", code, resp)
		}
		after := os.Getenv("FLOW_ZROK_SHARE_NAME")
		if after == "flowold0001" || !zrokUniqueNameRe.MatchString(after) {
			t.Fatalf("share name not rotated to a valid new name: %q", after)
		}
		if cfg := loadConfigFile(srv.configPath()); cfg["FLOW_ZROK_SHARE_NAME"] != after {
			t.Errorf("rotated share name not persisted: %q vs %q", cfg["FLOW_ZROK_SHARE_NAME"], after)
		}
	})

	t.Run("non-zrok provider rejected", func(t *testing.T) {
		root, db := testRootDB(t)
		srv := New(Config{DB: db, FlowRoot: root, CommandPath: "/bin/false"})
		clearIngressEnv(t)
		t.Setenv("FLOW_INGRESS_PROVIDER", "manual")

		resp, code := srv.rotateZrokShareName()
		if code != 400 || resp.OK {
			t.Fatalf("manual provider should be rejected: code=%d resp=%+v", code, resp)
		}
	})
}
