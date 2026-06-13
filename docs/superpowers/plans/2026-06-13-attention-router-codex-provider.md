# Attention Router Codex Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a durable Attention Router provider switch so new Attention triage and Attention-created tasks can use Claude or Codex, while matched tasks keep their stored provider and Slack no-task send remains Claude-only.

**Architecture:** Add a small provider resolver plus a provider-aware headless runner inside `internal/steering`. Route classifier, deep triage, capture, GitHub send, and Attention task spawning through that resolver; keep Claude session pooling and Slack floating send behavior provider-specific. Surface the setting through the existing `/api/settings` pipeline and Attention config UI.

**Tech Stack:** Go, SQLite via `modernc.org/sqlite`, `os/exec`, React/Vite TypeScript, Node source-inspection tests.

---

## Scope Check

The spec covers one subsystem with three integration points: steering runners,
Attention feed actions, and the existing settings UI. These are coupled by the
single `FLOW_STEERING_PROVIDER` setting and can be implemented as one plan with
small commits.

This plan intentionally does not add Codex support for Slack no-task send-reply.
That path remains a verified Claude interactive floating session because the
current Slack send prompt depends on the Claude Slack MCP tool.

## File Structure

- Create `internal/steering/provider.go`
  - Owns `SteeringProvider`, provider-aware model defaults, and trace model
    labels.
- Create `internal/steering/provider_test.go`
  - Tests env/provider/model resolution.
- Create `internal/steering/headless.go`
  - Owns provider-aware one-shot headless execution for Claude and Codex.
- Create `internal/steering/headless_test.go`
  - Tests command construction and stderr/stdout error detail for both
    providers.
- Modify `internal/server/settings.go`
  - Registers `FLOW_STEERING_PROVIDER` as a Steering enum setting.
- Modify `internal/server/settings_test.go`
  - Verifies settings exposure and validation.
- Modify `internal/steering/classifier.go`
  - Routes Stage 1/2 through the provider-aware headless runner.
- Modify `internal/steering/session_dispatch.go`
  - Uses Claude classifier session pooling only when the selected provider is
    Claude.
- Modify `internal/steering/session.go`
  - Pins pooled classifier sessions to Claude model defaults explicitly.
- Modify `internal/steering/classifier_test.go`
  - Adds Codex classifier dispatch coverage.
- Modify `internal/steering/session_dispatch_test.go`
  - Adds Codex bypasses-pool coverage.
- Modify `internal/steering/triage.go`
  - Routes Stage 3 through the provider-aware headless runner and keeps
    streaming Claude-only.
- Modify `internal/steering/triage_test.go`
  - Adds Codex deep-triage dispatch coverage.
- Modify `internal/steering/cascade.go`
  - Labels trace rows with provider/model and updates the label when Stage 3
    produces the final decision.
- Modify `internal/steering/actions.go`
  - Passes the selected provider to `flow spawn` for Attention-created tasks.
- Modify `internal/steering/actions_test.go`
  - Captures provider in the task spawner seam and adds Codex make-task
    coverage.
- Modify `internal/steering/send_reply.go`
  - Routes provider-safe headless send through the selected provider.
- Modify `internal/steering/send_reply_test.go`
  - Verifies Codex dispatch for headless GitHub send.
- Modify `internal/steering/capture_kb.go`
  - Routes KB capture through the selected provider.
- Modify `internal/steering/capture_kb_test.go`
  - Verifies Codex dispatch for KB capture.
- Modify `internal/server/attention.go`
  - Makes the Slack no-task send response explicit when it launches Claude.
- Modify `internal/server/server_test.go`
  - Verifies Slack floating send remains Claude under Codex steering provider.
- Modify `internal/server/ui/src/components/SteeringConfig.tsx`
  - Adds an Attention agent provider picker.
- Modify `internal/server/ui/src/screens/Attention.steering.test.mjs`
  - Verifies the picker and setting key are present.
- Modify `internal/app/skill/SKILL.md`
  - Documents the Attention provider switch and Slack exception.
- Modify `internal/app/skill_test.go`
  - Verifies the embedded skill documents the new behavior.

---

### Task 1: Add Steering Provider Setting And Resolver

**Files:**
- Create: `internal/steering/provider.go`
- Create: `internal/steering/provider_test.go`
- Modify: `internal/server/settings.go`
- Modify: `internal/server/settings_test.go`

- [ ] **Step 1: Write settings tests**

Add this test to `internal/server/settings_test.go`:

```go
func TestSettingsExposeAndValidateSteeringProvider(t *testing.T) {
	root, db := testRootDB(t)
	srv := New(Config{DB: db, FlowRoot: root, CommandPath: "/bin/false"})

	t.Setenv("FLOW_STEERING_PROVIDER", "codex")

	sp, ok := settingSpecFor("FLOW_STEERING_PROVIDER")
	if !ok {
		t.Fatal("FLOW_STEERING_PROVIDER not registered")
	}
	if sp.Group != "Steering" || sp.Type != settingEnum {
		t.Fatalf("provider spec = group %q type %q, want Steering enum", sp.Group, sp.Type)
	}
	if got := strings.Join(sp.Options, ","); got != "claude,codex" {
		t.Fatalf("provider options = %q, want claude,codex", got)
	}

	rec := httptest.NewRecorder()
	srv.handleSettings(rec, httptest.NewRequest("GET", "/api/settings", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "FLOW_STEERING_PROVIDER") || !strings.Contains(body, `"value":"codex"`) {
		t.Fatalf("steering provider missing from settings response: %s", body)
	}

	if _, st := srv.updateSettings(actionRequest{Settings: map[string]string{"FLOW_STEERING_PROVIDER": "gpt"}}); st == http.StatusOK {
		t.Fatal("expected invalid provider to be rejected")
	}
	resp, st := srv.updateSettings(actionRequest{Settings: map[string]string{"FLOW_STEERING_PROVIDER": "claude"}})
	if st != http.StatusOK || !resp.OK {
		t.Fatalf("valid provider update = (%+v, %d), want OK", resp, st)
	}
	if got := os.Getenv("FLOW_STEERING_PROVIDER"); got != "claude" {
		t.Fatalf("FLOW_STEERING_PROVIDER env = %q, want claude", got)
	}
}
```

- [ ] **Step 2: Write resolver tests**

Create `internal/steering/provider_test.go`:

```go
package steering

import "testing"

func TestSteeringProviderFromEnv(t *testing.T) {
	t.Setenv("FLOW_STEERING_PROVIDER", "")
	if got := SteeringProvider(); got != "claude" {
		t.Fatalf("empty provider = %q, want claude", got)
	}

	t.Setenv("FLOW_STEERING_PROVIDER", "codex")
	if got := SteeringProvider(); got != "codex" {
		t.Fatalf("codex provider = %q, want codex", got)
	}

	t.Setenv("FLOW_STEERING_PROVIDER", "bad")
	if got := SteeringProvider(); got != "claude" {
		t.Fatalf("bad provider = %q, want claude fallback", got)
	}
}

func TestSteeringModelDefaults(t *testing.T) {
	t.Setenv("FLOW_STEERING_CLASSIFIER_MODEL", "")
	if got := ClassifierModelForProvider("claude"); got != "claude-haiku-4-5" {
		t.Fatalf("claude classifier model = %q", got)
	}
	if got := ClassifierModelForProvider("codex"); got != "gpt-5.4-mini" {
		t.Fatalf("codex classifier model = %q", got)
	}
	if got := DeepTriageModelForProvider("claude"); got != "" {
		t.Fatalf("claude deep model = %q, want provider default", got)
	}
	if got := DeepTriageModelForProvider("codex"); got != "gpt-5.4" {
		t.Fatalf("codex deep model = %q", got)
	}
	if got := modelLabel("codex", "gpt-5.4-mini"); got != "codex:gpt-5.4-mini" {
		t.Fatalf("model label = %q", got)
	}
}

func TestSteeringClassifierModelOverride(t *testing.T) {
	t.Setenv("FLOW_STEERING_CLASSIFIER_MODEL", "custom-model")
	if got := ClassifierModelForProvider("codex"); got != "custom-model" {
		t.Fatalf("classifier override = %q", got)
	}
}
```

- [ ] **Step 3: Run tests and verify they fail**

Run:

```bash
go test ./internal/server ./internal/steering -run 'TestSettingsExposeAndValidateSteeringProvider|TestSteeringProviderFromEnv|TestSteeringModelDefaults|TestSteeringClassifierModelOverride' -count=1
```

Expected: FAIL with missing `FLOW_STEERING_PROVIDER`, missing
`SteeringProvider`, or missing model helper symbols.

- [ ] **Step 4: Register the setting**

In `internal/server/settings.go`, add this entry in the Steering section before
`FLOW_STEERING_WATCH_CHANNELS`:

```go
{Key: "FLOW_STEERING_PROVIDER", Label: "Attention agent", Group: "Steering", Type: settingEnum, Options: []string{"claude", "codex"}, Default: "claude", Help: "Agent provider used for new Attention Router triage and Attention-created tasks. Existing matched tasks keep their own provider."},
```

- [ ] **Step 5: Add provider resolver**

Create `internal/steering/provider.go`:

```go
package steering

import (
	"os"
	"strings"

	"flow/internal/flowdb"
)

func SteeringProvider() string {
	provider, err := flowdb.NormalizeSessionProvider(os.Getenv("FLOW_STEERING_PROVIDER"))
	if err != nil {
		return "claude"
	}
	return provider
}

func ClassifierModelForProvider(provider string) string {
	if m := strings.TrimSpace(os.Getenv("FLOW_STEERING_CLASSIFIER_MODEL")); m != "" {
		return m
	}
	if provider == "codex" {
		return "gpt-5.4-mini"
	}
	return "claude-haiku-4-5"
}

func DeepTriageModelForProvider(provider string) string {
	if m := strings.TrimSpace(os.Getenv("FLOW_STEERING_DEEP_MODEL")); m != "" {
		return m
	}
	if provider == "codex" {
		return "gpt-5.4"
	}
	return ""
}

func SendReplyModelForProvider(provider string) string {
	if m := strings.TrimSpace(os.Getenv("FLOW_STEERING_SEND_MODEL")); m != "" {
		return m
	}
	if provider == "codex" {
		return "gpt-5.4"
	}
	return "claude-sonnet-4-6"
}

func CaptureKBModelForProvider(provider string) string {
	if m := strings.TrimSpace(os.Getenv("FLOW_STEERING_CAPTURE_MODEL")); m != "" {
		return m
	}
	if provider == "codex" {
		return "gpt-5.4"
	}
	return "claude-sonnet-4-6"
}

func modelLabel(provider, model string) string {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" {
		provider = "claude"
	}
	if model == "" {
		return provider
	}
	return provider + ":" + model
}
```

- [ ] **Step 6: Run tests and verify they pass**

Run:

```bash
go test ./internal/server ./internal/steering -run 'TestSettingsExposeAndValidateSteeringProvider|TestSteeringProviderFromEnv|TestSteeringModelDefaults|TestSteeringClassifierModelOverride' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/server/settings.go internal/server/settings_test.go internal/steering/provider.go internal/steering/provider_test.go
git commit -m "feat: add attention provider setting"
```

---

### Task 2: Add Provider-Aware Headless Runner

**Files:**
- Create: `internal/steering/headless.go`
- Create: `internal/steering/headless_test.go`

- [ ] **Step 1: Write headless runner tests**

Create `internal/steering/headless_test.go`:

```go
package steering

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeHeadlessBinary(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	body := `#!/bin/sh
printf 'argv:%s\n' "$*" >&2
cat > "$FLOW_HEADLESS_STDIN"
printf '{"suggested_action":"drop","confidence":0.7}'
`
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
}

func TestDefaultHeadlessRunnerCodexExec(t *testing.T) {
	dir := t.TempDir()
	stdinPath := filepath.Join(dir, "stdin.txt")
	writeFakeHeadlessBinary(t, dir, "codex")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FLOW_HEADLESS_STDIN", stdinPath)

	out, err := defaultHeadlessRunner(context.Background(), HeadlessRequest{
		Provider:       "codex",
		Prompt:         "MODE: stage1",
		Model:          "gpt-5.4-mini",
		PermissionMode: "auto",
		WorkDir:        "/tmp/work",
		FlowRoot:       "/tmp/flow-root",
	})
	if err != nil {
		t.Fatalf("defaultHeadlessRunner: %v", err)
	}
	if !strings.Contains(out, `"suggested_action":"drop"`) {
		t.Fatalf("stdout = %q", out)
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read stdin: %v", err)
	}
	if string(stdin) != "MODE: stage1" {
		t.Fatalf("stdin = %q, want prompt", string(stdin))
	}
}

func TestCodexHeadlessArgs(t *testing.T) {
	args := codexHeadlessArgs("/tmp/work", "/tmp/flow-root", "auto", "gpt-5.4-mini")
	want := []string{
		"--ask-for-approval", "never",
		"--sandbox", "workspace-write",
		"-c", "sandbox_workspace_write.network_access=true",
		"exec",
		"--color", "never",
		"--cd", "/tmp/work",
		"--add-dir", "/tmp/flow-root",
		"--model", "gpt-5.4-mini",
		"-",
	}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("codex args = %#v, want %#v", args, want)
	}
}

func TestDefaultHeadlessRunnerIncludesStderrOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'codex auth expired' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := defaultHeadlessRunner(context.Background(), HeadlessRequest{Provider: "codex", Prompt: "prompt"})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "codex auth expired") || !strings.Contains(got, "steering: headless codex exec") {
		t.Fatalf("error missing stderr/provider context: %s", got)
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/steering -run 'TestDefaultHeadlessRunnerCodexExec|TestCodexHeadlessArgs|TestDefaultHeadlessRunnerIncludesStderrOnFailure' -count=1
```

Expected: FAIL with missing `HeadlessRequest`, `defaultHeadlessRunner`, or
`codexHeadlessArgs`.

- [ ] **Step 3: Add headless runner**

Create `internal/steering/headless.go`:

```go
package steering

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
)

type HeadlessRequest struct {
	Provider       string
	Prompt         string
	Model          string
	PermissionMode string
	WorkDir        string
	FlowRoot       string
}

var headlessRunner = defaultHeadlessRunner

func runHeadless(ctx context.Context, req HeadlessRequest) (string, error) {
	return headlessRunner(ctx, req)
}

func defaultHeadlessRunner(ctx context.Context, req HeadlessRequest) (string, error) {
	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		provider = "claude"
	}
	switch provider {
	case "codex":
		cmd := exec.CommandContext(ctx, "codex", codexHeadlessArgs(req.WorkDir, req.FlowRoot, req.PermissionMode, req.Model)...)
		cmd.Stdin = strings.NewReader(req.Prompt)
		out, err := cmd.Output()
		if err != nil {
			return "", commandError("steering: headless codex exec", err, out)
		}
		return string(out), nil
	default:
		args := []string{"-p", req.Prompt}
		if model := strings.TrimSpace(req.Model); model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, claudeHeadlessPermissionArgs(req.PermissionMode)...)
		cmd := exec.CommandContext(ctx, "claude", args...)
		out, err := cmd.Output()
		if err != nil {
			return "", commandError("steering: headless claude -p", err, out)
		}
		return string(out), nil
	}
}

func claudeHeadlessPermissionArgs(mode string) []string {
	switch strings.TrimSpace(mode) {
	case "auto":
		return []string{"--permission-mode", "auto"}
	case "default":
		return []string{"--permission-mode", "acceptEdits"}
	default:
		return []string{"--dangerously-skip-permissions"}
	}
}

func codexHeadlessArgs(cwd, flowRootPath, permissionMode, model string) []string {
	args := append([]string{}, codexHeadlessPermissionArgs(permissionMode)...)
	args = append(args, "exec", "--color", "never")
	if strings.TrimSpace(cwd) != "" {
		args = append(args, "--cd", cwd)
	}
	args = appendCodexHeadlessWritableRoot(args, cwd, flowRootPath)
	if strings.TrimSpace(model) != "" {
		args = append(args, "--model", strings.TrimSpace(model))
	}
	return append(args, "-")
}

func codexHeadlessPermissionArgs(mode string) []string {
	const allowNetwork = "sandbox_workspace_write.network_access=true"
	switch strings.TrimSpace(mode) {
	case "bypass":
		return []string{"--dangerously-bypass-approvals-and-sandbox"}
	default:
		return []string{"--ask-for-approval", "never", "--sandbox", "workspace-write", "-c", allowNetwork}
	}
}

func appendCodexHeadlessWritableRoot(args []string, workDir, flowRootPath string) []string {
	flowRootPath = strings.TrimSpace(flowRootPath)
	if flowRootPath == "" {
		return args
	}
	cleanWorkDir := strings.TrimSpace(workDir)
	if cleanWorkDir != "" {
		if abs, err := filepath.Abs(cleanWorkDir); err == nil {
			cleanWorkDir = abs
		}
	}
	if abs, err := filepath.Abs(flowRootPath); err == nil {
		flowRootPath = abs
	}
	if cleanWorkDir == flowRootPath {
		return args
	}
	return append(args, "--add-dir", flowRootPath)
}
```

- [ ] **Step 4: Run tests and verify they pass**

Run:

```bash
go test ./internal/steering -run 'TestDefaultHeadlessRunnerCodexExec|TestCodexHeadlessArgs|TestDefaultHeadlessRunnerIncludesStderrOnFailure' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/steering/headless.go internal/steering/headless_test.go
git commit -m "feat: add steering headless provider runner"
```

---

### Task 3: Route Classifier Through Selected Provider

**Files:**
- Modify: `internal/steering/classifier.go`
- Modify: `internal/steering/classifier_test.go`
- Modify: `internal/steering/session_dispatch.go`
- Modify: `internal/steering/session_dispatch_test.go`
- Modify: `internal/steering/session.go`

- [ ] **Step 1: Write classifier dispatch tests**

Add to `internal/steering/classifier_test.go`:

```go
func TestClassifierRunnerUsesCodexProvider(t *testing.T) {
	t.Setenv("FLOW_STEERING_PROVIDER", "codex")
	t.Setenv("FLOW_STEERING_CLASSIFIER_MODEL", "")
	old := headlessRunner
	var seen HeadlessRequest
	headlessRunner = func(_ context.Context, req HeadlessRequest) (string, error) {
		seen = req
		return `[{"thread_key":"k1","relevant":true}]`, nil
	}
	t.Cleanup(func() { headlessRunner = old })

	out, err := classifierRunner(context.Background(), "MODE: stage1-relevance")
	if err != nil {
		t.Fatalf("classifierRunner: %v", err)
	}
	if !strings.Contains(out, `"relevant":true`) {
		t.Fatalf("output = %q", out)
	}
	if seen.Provider != "codex" || seen.Model != "gpt-5.4-mini" || seen.PermissionMode != "auto" {
		t.Fatalf("headless request = %+v, want codex gpt-5.4-mini auto", seen)
	}
}
```

Create or extend `internal/steering/session_dispatch_test.go`:

```go
func TestRunClassifierBypassesClaudePoolForCodex(t *testing.T) {
	t.Setenv("FLOW_STEERING_PROVIDER", "codex")
	oldPool := activeClassifierPool
	activeClassifierPool = newClassifierPool(40, time.Minute)
	activeClassifierPool.exec = func(context.Context, []string) (string, error) {
		t.Fatal("codex classifier must not use claude session pool")
		return "", nil
	}
	oldRunner := classifierRunner
	classifierRunner = func(context.Context, string) (string, error) {
		return `{"suggested_action":"drop","confidence":0.1}`, nil
	}
	t.Cleanup(func() {
		activeClassifierPool = oldPool
		classifierRunner = oldRunner
	})

	out, err := runClassifier(context.Background(), "stage2", "prime", "payload", "key")
	if err != nil {
		t.Fatalf("runClassifier: %v", err)
	}
	if !strings.Contains(out, `"suggested_action":"drop"`) {
		t.Fatalf("output = %q", out)
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/steering -run 'TestClassifierRunnerUsesCodexProvider|TestRunClassifierBypassesClaudePoolForCodex' -count=1
```

Expected: FAIL because `classifierRunner` still shells out to Claude and
`runClassifier` still uses the active pool when configured.

- [ ] **Step 3: Update classifier model and runner**

In `internal/steering/classifier.go`, replace `classifierRunner` and
`classifierModel` with:

```go
var classifierRunner = func(ctx context.Context, prompt string) (string, error) {
	provider := SteeringProvider()
	return runHeadless(ctx, HeadlessRequest{
		Provider:       provider,
		Prompt:         prompt,
		Model:          ClassifierModelForProvider(provider),
		PermissionMode: steeringHeadlessPermission(provider),
	})
}

func classifierModel() string {
	return ClassifierModelForProvider(SteeringProvider())
}

func steeringHeadlessPermission(provider string) string {
	if provider == "codex" {
		return "auto"
	}
	return "bypass"
}
```

- [ ] **Step 4: Update session dispatch**

In `internal/steering/session_dispatch.go`, change `runClassifier` to:

```go
func runClassifier(ctx context.Context, mode, prime, payload, primeKey string) (string, error) {
	if SteeringProvider() == "claude" && activeClassifierPool != nil {
		return activeClassifierPool.run(ctx, mode, prime, payload, primeKey)
	}
	return classifierRunner(ctx, prime+"\n\n"+payload)
}
```

In `internal/steering/session.go`, change the pooled session args to use the
Claude default explicitly:

```go
args = []string{"-p", prime + "\n\n" + payload, "--model", ClassifierModelForProvider("claude"), "--dangerously-skip-permissions", "--session-id", slot.id}
```

and:

```go
args = []string{"-p", payload, "--model", ClassifierModelForProvider("claude"), "--dangerously-skip-permissions", "--resume", slot.id}
```

- [ ] **Step 5: Run focused classifier tests**

Run:

```bash
go test ./internal/steering -run 'TestClassifierRunnerIncludesClaudeStderrOnFailure|TestClassifierRunnerUsesCodexProvider|TestRunClassifierBypassesClaudePoolForCodex|TestStage1Relevance|TestStage2Score' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/steering/classifier.go internal/steering/classifier_test.go internal/steering/session_dispatch.go internal/steering/session_dispatch_test.go internal/steering/session.go
git commit -m "feat: route attention classifiers by provider"
```

---

### Task 4: Route Deep Triage And Trace Model Labels

**Files:**
- Modify: `internal/steering/triage.go`
- Modify: `internal/steering/triage_test.go`
- Modify: `internal/steering/cascade.go`

- [ ] **Step 1: Write deep triage provider test**

Add to `internal/steering/triage_test.go`:

```go
func TestDeepTriageRunnerUsesCodexProvider(t *testing.T) {
	t.Setenv("FLOW_STEERING_PROVIDER", "codex")
	old := headlessRunner
	var seen HeadlessRequest
	headlessRunner = func(_ context.Context, req HeadlessRequest) (string, error) {
		seen = req
		return `{"suggested_action":"digest_only","confidence":0.7}`, nil
	}
	t.Cleanup(func() { headlessRunner = old })

	out, err := deepTriageRunner(context.Background(), "MODE: stage3-deep")
	if err != nil {
		t.Fatalf("deepTriageRunner: %v", err)
	}
	if !strings.Contains(out, `"digest_only"`) {
		t.Fatalf("output = %q", out)
	}
	if seen.Provider != "codex" || seen.Model != "gpt-5.4" || seen.PermissionMode != "auto" {
		t.Fatalf("headless request = %+v, want codex gpt-5.4 auto", seen)
	}
}
```

- [ ] **Step 2: Run test and verify it fails**

Run:

```bash
go test ./internal/steering -run TestDeepTriageRunnerUsesCodexProvider -count=1
```

Expected: FAIL because Stage 3 still uses Claude directly.

- [ ] **Step 3: Update deep triage runner**

In `internal/steering/triage.go`, replace `deepTriageRunner` with:

```go
var deepTriageRunner = func(ctx context.Context, prompt string) (string, error) {
	provider := SteeringProvider()
	if provider == "claude" {
		if sink := streamSinkFrom(ctx); sink != nil && streamingEnabled() {
			if out, err := runClaudeStreaming(ctx, []string{"--dangerously-skip-permissions"}, prompt, sink); err == nil && strings.ContainsAny(out, "{[") {
				return out, nil
			}
		}
	}
	return runHeadless(ctx, HeadlessRequest{
		Provider:       provider,
		Prompt:         prompt,
		Model:          DeepTriageModelForProvider(provider),
		PermissionMode: steeringHeadlessPermission(provider),
	})
}
```

- [ ] **Step 4: Label traces with provider/model**

In `internal/steering/cascade.go`, change the `Model` field in `newTrace` to:

```go
Model:       modelLabel(SteeringProvider(), ClassifierModelForProvider(SteeringProvider())),
```

In `finishItem`, immediately before the `c.stage(tr, start, "stage3", "running", "deep triage")` call, add:

```go
provider := SteeringProvider()
tr.Model = modelLabel(provider, DeepTriageModelForProvider(provider))
```

Do not change Stage 2 budget-exhausted behavior: those traces should keep the
classifier model label because Stage 3 did not produce the final decision.

- [ ] **Step 5: Run focused triage tests**

Run:

```bash
go test ./internal/steering -run 'TestDeepTriageRunnerIncludesClaudeStderrOnFailure|TestDeepTriageRunnerUsesCodexProvider|TestDeepTriagePromptUsesContextPackAsPrimaryInput|TestDeepTriagePromptIncludesTaskImpactHints' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/steering/triage.go internal/steering/triage_test.go internal/steering/cascade.go
git commit -m "feat: route attention deep triage by provider"
```

---

### Task 5: Spawn Attention-Created Tasks With Selected Provider

**Files:**
- Modify: `internal/steering/actions.go`
- Modify: `internal/steering/actions_test.go`

- [ ] **Step 1: Update test seam and add provider assertion**

In `internal/steering/actions_test.go`, change `spawnRec` to:

```go
type spawnRec struct{ name, slug, brief, project, provider string }
```

Change the `taskSpawner` stub in `stubActionIO` to:

```go
taskSpawner = func(_ context.Context, name, slug, brief, project, provider string) error {
	spawns = append(spawns, spawnRec{name, slug, brief, project, provider})
	return nil
}
```

Add this test:

```go
func TestMakeTaskFromFeedUsesConfiguredProvider(t *testing.T) {
	t.Setenv("FLOW_STEERING_PROVIDER", "codex")
	db, err := flowdb.OpenDB(filepath.Join(t.TempDir(), "flow.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	spawns, _ := stubActionIO(t)

	item := flowdb.FeedItem{
		ID: "provider-make", Source: "github", ThreadKey: "o/r:gh-pr:o/r#5",
		Summary: "review needs follow-up", SuggestedAction: "make_task",
		Status: "new", CreatedAt: "2026-06-05T10:00:00Z",
	}
	if _, err := flowdb.UpsertFeedItem(db, item); err != nil {
		t.Fatalf("seed feed: %v", err)
	}
	if err := MakeTaskFromFeed(context.Background(), db, item); err != nil {
		t.Fatalf("MakeTaskFromFeed: %v", err)
	}
	if len(*spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(*spawns))
	}
	if got := (*spawns)[0].provider; got != "codex" {
		t.Fatalf("spawn provider = %q, want codex", got)
	}
}
```

- [ ] **Step 2: Run test and verify it fails**

Run:

```bash
go test ./internal/steering -run 'TestMakeTaskFromFeedUsesConfiguredProvider|TestMakeTaskFromFeed' -count=1
```

Expected: FAIL because `taskSpawner` does not accept or record provider yet.

- [ ] **Step 3: Update task spawner**

In `internal/steering/actions.go`, change `taskSpawner` to:

```go
var taskSpawner = func(ctx context.Context, name, slug, brief, project, provider string) error {
	if strings.TrimSpace(provider) == "" {
		provider = "claude"
	}
	args := []string{"spawn", name, "--slug", slug, "--priority", "high", "--prompt", brief, "--no-open", "--agent", provider}
	if p := strings.TrimSpace(project); p != "" {
		args = append(args, "--project", p)
	}
	cmd := exec.CommandContext(ctx, "flow", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("steering: flow spawn: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
```

In `MakeTaskFromFeed`, change the call to:

```go
if err := taskSpawner(ctx, feedTaskName(item), slug, feedTaskBrief(item), item.SuggestedProject, SteeringProvider()); err != nil {
	return err
}
```

In `MakeReplyTaskFromFeed`, change the call to:

```go
if err := taskSpawner(ctx, feedTaskName(item), slug, feedReplyTaskBrief(item, text), item.SuggestedProject, SteeringProvider()); err != nil {
	return "", err
}
```

- [ ] **Step 4: Run focused action tests**

Run:

```bash
go test ./internal/steering -run 'TestMakeTaskFromFeed|TestMakeTaskFromFeedUsesConfiguredProvider|TestMakeTaskFromFeedTagsSourceThread|TestMakeReplyTaskFromFeed' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/steering/actions.go internal/steering/actions_test.go
git commit -m "feat: create attention tasks with selected provider"
```

---

### Task 6: Route Provider-Safe Headless Actions

**Files:**
- Modify: `internal/steering/send_reply.go`
- Modify: `internal/steering/send_reply_test.go`
- Modify: `internal/steering/capture_kb.go`
- Modify: `internal/steering/capture_kb_test.go`

- [ ] **Step 1: Write send and capture provider tests**

Add to `internal/steering/send_reply_test.go`:

```go
func TestSendReplyRunnerUsesCodexProvider(t *testing.T) {
	t.Setenv("FLOW_STEERING_PROVIDER", "codex")
	old := headlessRunner
	var seen HeadlessRequest
	headlessRunner = func(_ context.Context, req HeadlessRequest) (string, error) {
		seen = req
		return "POSTED", nil
	}
	t.Cleanup(func() { headlessRunner = old })

	out, err := sendReplyRunner(context.Background(), "MODE: send-reply")
	if err != nil {
		t.Fatalf("sendReplyRunner: %v", err)
	}
	if out != "POSTED" {
		t.Fatalf("out = %q", out)
	}
	if seen.Provider != "codex" || seen.Model != "gpt-5.4" || seen.PermissionMode != "auto" {
		t.Fatalf("headless request = %+v, want codex gpt-5.4 auto", seen)
	}
}
```

Add to `internal/steering/capture_kb_test.go`:

```go
func TestCaptureKBRunnerUsesCodexProvider(t *testing.T) {
	t.Setenv("FLOW_STEERING_PROVIDER", "codex")
	old := headlessRunner
	var seen HeadlessRequest
	headlessRunner = func(_ context.Context, req HeadlessRequest) (string, error) {
		seen = req
		return "CAPTURED kb/org.md", nil
	}
	t.Cleanup(func() { headlessRunner = old })

	out, err := captureKBRunner(context.Background(), "MODE: capture-kb")
	if err != nil {
		t.Fatalf("captureKBRunner: %v", err)
	}
	if out != "CAPTURED kb/org.md" {
		t.Fatalf("out = %q", out)
	}
	if seen.Provider != "codex" || seen.Model != "gpt-5.4" || seen.PermissionMode != "auto" {
		t.Fatalf("headless request = %+v, want codex gpt-5.4 auto", seen)
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/steering -run 'TestSendReplyRunnerUsesCodexProvider|TestCaptureKBRunnerUsesCodexProvider' -count=1
```

Expected: FAIL because both runners still use Claude directly.

- [ ] **Step 3: Update send reply runner**

In `internal/steering/send_reply.go`, replace `sendReplyRunner` with:

```go
var sendReplyRunner = func(ctx context.Context, prompt string) (string, error) {
	provider := SteeringProvider()
	return runHeadless(ctx, HeadlessRequest{
		Provider:       provider,
		Prompt:         prompt,
		Model:          SendReplyModelForProvider(provider),
		PermissionMode: steeringHeadlessPermission(provider),
	})
}
```

Replace `SendReplyModel` with:

```go
func SendReplyModel() string {
	return SendReplyModelForProvider("claude")
}
```

The Slack floating send path calls `SendReplyModel()` and therefore remains
Claude-model based.

- [ ] **Step 4: Update capture runner**

In `internal/steering/capture_kb.go`, replace `captureKBRunner` with:

```go
var captureKBRunner = func(ctx context.Context, prompt string) (string, error) {
	provider := SteeringProvider()
	return runHeadless(ctx, HeadlessRequest{
		Provider:       provider,
		Prompt:         prompt,
		Model:          CaptureKBModelForProvider(provider),
		PermissionMode: steeringHeadlessPermission(provider),
	})
}
```

Replace `CaptureKBModel` with:

```go
func CaptureKBModel() string {
	return CaptureKBModelForProvider("claude")
}
```

- [ ] **Step 5: Run focused tests**

Run:

```bash
go test ./internal/steering -run 'TestSendReplyViaAgent|TestSendReplyRunnerUsesCodexProvider|TestCaptureKBViaAgentMarksActedOnConfirm|TestCaptureKBRunnerUsesCodexProvider' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/steering/send_reply.go internal/steering/send_reply_test.go internal/steering/capture_kb.go internal/steering/capture_kb_test.go
git commit -m "feat: route provider-safe attention agents"
```

---

### Task 7: Keep Slack No-Task Send Claude-Only

**Files:**
- Modify: `internal/server/attention.go`
- Modify: `internal/server/server_test.go`

- [ ] **Step 1: Write Slack floating send test**

Add to `internal/server/server_test.go`:

```go
func TestSendReplyFloatingLaunchStaysClaudeWhenSteeringProviderIsCodex(t *testing.T) {
	t.Setenv("FLOW_STEERING_PROVIDER", "codex")
	root, db := testRootDB(t)
	srv := New(Config{DB: db, FlowRoot: root, CommandPath: "/bin/false"})

	launch, err := srv.prepareSendReplyFloatingLaunch(flowdb.FeedItem{
		ID: "send1", Source: "slack", ThreadKey: "C1:1780000000.000100", Channel: "C1",
	}, "Approved reply", "")
	if err != nil {
		t.Fatalf("prepareSendReplyFloatingLaunch: %v", err)
	}
	if launch.Provider != "claude" {
		t.Fatalf("send-reply floating provider = %q, want claude", launch.Provider)
	}
	args := strings.Join(launch.Args, " ")
	if !strings.Contains(args, "--model "+steering.SendReplyModel()) {
		t.Fatalf("send launch args = %q, want Claude send model", args)
	}
	if strings.Contains(args, "gpt-5.4") {
		t.Fatalf("Slack send launch must not use Codex model args: %q", args)
	}
}
```

- [ ] **Step 2: Run test and verify current behavior**

Run:

```bash
go test ./internal/server -run TestSendReplyFloatingLaunchStaysClaudeWhenSteeringProviderIsCodex -count=1
```

Expected: PASS if the existing Claude pin is intact. If it fails, restore the
Claude pin before continuing.

- [ ] **Step 3: Update server response copy**

In `internal/server/attention.go`, change the Slack no-task send response to:

```go
return actionResponse{OK: true, Message: "posting your reply in a Claude send session because Slack MCP posting is Claude-only right now; open the Send reply terminal from the tray to watch", FloatingTerminal: &ft}, http.StatusOK
```

- [ ] **Step 4: Run focused server tests**

Run:

```bash
go test ./internal/server -run 'TestSendReplyFloatingLaunchStaysClaudeWhenSteeringProviderIsCodex|TestAttentionSendReplyEmpty|TestAttentionSendReplyRecognized|TestAttentionSendReplyEditedTextOverridesEmptyDraft' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/attention.go internal/server/server_test.go
git commit -m "fix: keep slack attention send on claude"
```

---

### Task 8: Add Attention Provider Picker To UI

**Files:**
- Modify: `internal/server/ui/src/components/SteeringConfig.tsx`
- Modify: `internal/server/ui/src/screens/Attention.steering.test.mjs`

- [ ] **Step 1: Write source-inspection UI test**

In `internal/server/ui/src/screens/Attention.steering.test.mjs`, extend
`steering config consolidates every steering key` by adding
`FLOW_STEERING_PROVIDER` to the checked keys, and add:

```js
test('steering config renders an attention agent provider picker', () => {
  assert.match(steeringSource, /import \{ AgentPicker \}/)
  assert.match(steeringSource, /function AttentionAgentPanel/)
  assert.match(steeringSource, /FLOW_STEERING_PROVIDER/)
  assert.match(steeringSource, /Attention agent/)
  assert.match(steeringSource, /matched tasks keep their own provider/)
})
```

- [ ] **Step 2: Run UI source test and verify it fails**

Run:

```bash
cd internal/server/ui && node --test src/screens/Attention.steering.test.mjs
```

Expected: FAIL because `AttentionAgentPanel` and `AgentPicker` wiring do not
exist in `SteeringConfig.tsx`.

- [ ] **Step 3: Update SteeringConfig imports**

In `internal/server/ui/src/components/SteeringConfig.tsx`, change imports to:

```tsx
import { useMemo, useState, type ReactNode } from 'react'
import { BellOff, Bot, Clock, Filter, Gauge, Hash, Loader2, Save } from 'lucide-react'
import { useAction, useSettings, useUiData } from '../lib/query'
import { ConfigField, SettingsPanel, SettingsSection, useConfigDraft } from './SettingsPanels'
import { ChannelPicker } from './ChannelPicker'
import { AutonomyPanel } from './AutonomyPanel'
import { AgentPicker } from './pickers'
```

- [ ] **Step 4: Add AttentionAgentPanel**

Add this component above `export function SteeringConfig()`:

```tsx
function AttentionAgentPanel() {
  const { data: settings } = useSettings()
  const { data: ui } = useUiData()
  const action = useAction()

  const saved = useMemo(
    () => settings?.fields?.find((f) => f.key === 'FLOW_STEERING_PROVIDER')?.value || 'claude',
    [settings],
  )
  const [draft, setDraft] = useState<string | null>(null)
  const value = draft ?? saved
  const providers = ui?.CAPABILITIES?.providers ?? []
  const dirty = value !== saved

  const save = () => {
    action.mutate(
      { kind: 'update-settings', settings: { FLOW_STEERING_PROVIDER: value } },
      { onSuccess: () => setDraft(null) },
    )
  }

  return (
    <SettingsPanel title="Attention agent" icon={<Bot size={17} />}>
      <div className="config-form">
        <p className="config-help">
          New Attention triage and new Attention-created tasks use this agent; matched tasks keep their own provider.
          Slack send-reply still uses a Claude send session for Slack MCP posting.
        </p>
        <AgentPicker value={value} onChange={setDraft} providers={providers} />
        <div className="config-actions">
          <button type="button" className="btn primary" disabled={!dirty || action.isPending} onClick={save}>
            {action.isPending ? <Loader2 size={14} className="spin" /> : <Save size={14} />}
            Save agent
          </button>
        </div>
      </div>
    </SettingsPanel>
  )
}
```

- [ ] **Step 5: Render the panel**

In the Performance section of `SteeringConfig`, render the panel before
`Reply & classifier`:

```tsx
<SettingsSection title="Performance" hint="Attention agent, reply send model, and classifier subprocess budget.">
  <div className="settings-grid">
    <AttentionAgentPanel />
    <ConfigGroupPanel title="Reply & classifier" icon={<Gauge size={17} />} fieldKeys={PERFORMANCE_KEYS} />
  </div>
</SettingsSection>
```

- [ ] **Step 6: Run frontend checks**

Run:

```bash
cd internal/server/ui && node --test src/screens/Attention.steering.test.mjs && pnpm typecheck
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/server/ui/src/components/SteeringConfig.tsx internal/server/ui/src/screens/Attention.steering.test.mjs
git commit -m "feat(ui): add attention provider picker"
```

---

### Task 9: Update Embedded Flow Skill Docs

**Files:**
- Modify: `internal/app/skill/SKILL.md`
- Modify: `internal/app/skill_test.go`

- [ ] **Step 1: Write skill documentation test**

In `internal/app/skill_test.go`, extend `TestSkillDocumentsAttentionWorkflow`
with these expected strings:

```go
"FLOW_STEERING_PROVIDER",
"Attention Router provider switch",
"New Attention-created tasks use the selected provider",
"Existing matched tasks keep their stored provider",
"Slack no-task send-reply remains Claude-only",
```

- [ ] **Step 2: Run test and verify it fails**

Run:

```bash
go test ./internal/app -run TestSkillDocumentsAttentionWorkflow -count=1
```

Expected: FAIL because the embedded skill does not document the provider switch.

- [ ] **Step 3: Update skill docs**

In `internal/app/skill/SKILL.md`, under `## 10d. Attention Router feed`, add:

```markdown
### Attention Router provider switch

Mission Control's Attention config can set `FLOW_STEERING_PROVIDER=claude|codex`.
This switch controls new Attention triage runs and new Attention-created tasks:
`make-task` / `make-task-start` create tasks with the selected provider.

Existing matched tasks keep their stored provider. If an Attention card forwards
to a Codex task, wake that Codex task; if it forwards to a Claude task, wake that
Claude task. Do not change a started task's provider just because the Attention
provider setting changed.

Slack no-task send-reply remains Claude-only because that path posts through the
Claude Slack MCP in an ephemeral floating send session. GitHub no-task send and
KB capture may use the selected Attention provider because they rely on local
CLI/filesystem work rather than the Claude Slack MCP.
```

- [ ] **Step 4: Run skill test**

Run:

```bash
go test ./internal/app -run TestSkillDocumentsAttentionWorkflow -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/skill/SKILL.md internal/app/skill_test.go
git commit -m "docs: document attention provider switch"
```

---

### Task 10: Full Verification And UI Asset Build

**Files:**
- Modify generated assets only if `make ui` changes `internal/server/static/`.

- [ ] **Step 1: Run frontend build**

Run:

```bash
make ui
```

Expected: PASS. If this updates embedded static assets, include those generated
files in the final verification commit.

- [ ] **Step 2: Run focused Go suites**

Run:

```bash
go test ./internal/steering ./internal/server ./internal/app -count=1
```

Expected: PASS.

- [ ] **Step 3: Run full Go suite**

Run:

```bash
go test ./...
```

Expected: PASS. If an unrelated pre-existing failure appears, reproduce it with
the narrow package command and record the exact failing test and error in the
final report before continuing.

- [ ] **Step 4: Run whitespace check**

Run:

```bash
git diff --check
```

Expected: no output.

- [ ] **Step 5: Inspect final diff**

Run:

```bash
git status --short
git diff --stat
```

Expected: only files from this plan plus any generated UI assets changed. The
unrelated dirty files that existed before this plan should remain separate and
must not be included in commits for this feature.

- [ ] **Step 6: Commit generated assets if needed**

If `make ui` changed embedded static assets, commit them separately:

```bash
git add internal/server/static
git commit -m "chore: rebuild ui assets for attention provider switch"
```

If no generated assets changed, do not create an empty commit.

---

## Self-Review Checklist

- Spec coverage:
  - `FLOW_STEERING_PROVIDER` setting: Task 1.
  - Provider-aware classifier/deep triage: Tasks 2, 3, and 4.
  - Attention-created task provider: Task 5.
  - Matched tasks keep stored provider: Tasks 5 and 7 preserve existing
    matched-task paths; Task 9 documents the rule.
  - GitHub send and KB capture use selected provider: Task 6.
  - Slack no-task send remains Claude-only: Task 7.
  - UI switch: Task 8.
  - Embedded skill docs: Task 9.
  - Verification: Task 10.
- Marker scan:
  - No unresolved markers or open-ended test-writing steps.
  - Every code-changing step includes concrete code snippets.
- Type consistency:
  - `HeadlessRequest`, `SteeringProvider`, model helper names, and
    `taskSpawner` signature are consistent across tasks.
  - Frontend action uses existing `update-settings` shape and existing
    `AgentPicker` provider data.
