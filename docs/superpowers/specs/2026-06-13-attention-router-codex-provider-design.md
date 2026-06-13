# Attention Router Codex Provider Design

**Date:** 2026-06-13
**Status:** Draft for operator review
**Repo:** flow-manager (`flow` Go CLI + Mission Control UI)

---

## 1. Summary

The Attention Router currently assumes Claude for its model-backed triage and
for tasks created from Attention cards. Flow already supports both Claude and
Codex for normal task sessions, browser terminals, and autonomous runs, so the
Attention Router should expose the same provider choice.

Add a single durable Attention setting, `FLOW_STEERING_PROVIDER`, with
`claude|codex`. The setting controls the provider used for new Attention triage
work and newly-created Attention tasks. Existing matched tasks keep their stored
task provider, so forwarding or sending through a task wakes the agent that
already owns that task.

Slack no-task send-reply remains Claude-only in this first version because the
current implementation depends on a Claude interactive session with the Slack
MCP tool. Codex support for that path should be added only after the Codex
runtime has an equivalent proven Slack write capability.

---

## 2. Goals

- Let the operator choose Claude or Codex for Attention Router triage.
- Let Attention-created tasks use the selected provider instead of always
  `--agent claude`.
- Keep existing task ownership stable: a matched Claude task stays Claude; a
  matched Codex task stays Codex.
- Reuse the existing settings pipeline and Mission Control Attention config
  surface.
- Keep connector-specific send behavior honest, especially Slack MCP posting.
- Preserve traceability: traces and UI should show which provider/model powered
  a decision when available.

---

## 3. Non-goals

- Switching an already-started task from Claude to Codex.
- Per-card provider selection in the feed.
- Replacing `tasks.session_provider` or the normal task provider picker.
- Adding a new database table for Attention provider configuration.
- Claiming Slack no-task send-reply works under Codex before the tool path is
  verified.
- Reworking the Attention autonomy policy.

---

## 4. Current Behavior

Attention Stage 1 and Stage 2 run through `classifierRunner`, which shells out
to `claude -p` with the configured classifier model. Stage 3 deep triage shells
out to `claude -p` as well, with optional Claude stream-json output for the live
triage view.

Attention feed actions are mixed:

- `make-task` and `make-task-start` call `flow spawn ... --agent claude`.
- `forward`, handoff, and matched-task send-reply target an existing task, so
  the target task's stored provider already determines the resumed session.
- GitHub no-task send-reply uses a headless Claude agent that posts through
  `gh`.
- Slack no-task send-reply opens an ephemeral interactive Claude floating
  session because it needs the Claude Slack MCP tool.
- `capture-kb` uses a headless Claude agent to edit local KB markdown files.

Mission Control already has an Attention config surface backed by
`/api/settings`, and the server already persists settings to config.json and
exports them into the process environment.

---

## 5. Design Decisions

### One Attention Provider Setting

Add this settings entry:

```text
FLOW_STEERING_PROVIDER = claude | codex
```

Default: `claude`, preserving current behavior.

This setting applies to new Attention work only:

- Stage 1/2 classifier calls.
- Stage 3 deep triage calls.
- `capture-kb`.
- GitHub no-task send-reply.
- New tasks created by `make-task` / `make-task-start`.

It does not override an existing task's `tasks.session_provider`.

### Existing Tasks Keep Their Provider

When an Attention card has `matched_task`, the router should treat that task as
the owner. `forward`, confirmed handoff, matched-task send-reply, and session
wakeups continue through the matched task. If the task is Codex, Codex resumes;
if it is Claude, Claude resumes.

This avoids accidental midstream provider changes and keeps the provider lock
rules consistent with the task detail UI.

### Slack No-Task Send Stays Claude First

The Slack floating send path is an intentional exception. It currently relies on
a real interactive Claude PTY with the Slack MCP available. The global Attention
provider should not silently move that path to Codex until Codex can perform the
same tool call in this environment.

When `FLOW_STEERING_PROVIDER=codex`, Slack no-task send-reply should still show
that it is using a Claude send session, because that is the only verified sender
for this connector path.

---

## 6. Backend Architecture

### Provider Resolution

Add a small provider resolver in `internal/steering`:

```go
func SteeringProvider() string
```

It reads `FLOW_STEERING_PROVIDER`, normalizes via the same provider vocabulary
as task sessions, and falls back to `claude` on empty or invalid values.

### Headless Runner Abstraction

Replace direct `claude -p` calls in steering with a provider-aware runner:

```go
type HeadlessRequest struct {
    Provider       string
    Prompt         string
    Model          string
    PermissionMode string
    WorkDir        string
    StreamSink     streamSink
}
```

Implementation rules:

- Claude uses the existing `claude -p` invocation and keeps stream-json support
  for Stage 3.
- Codex uses `codex exec` with Flow's existing permission-mode mapping and
  prompt input. The implementation should reuse patterns from the existing
  Codex auto-run code rather than inventing a separate command grammar. Because
  `internal/steering` should not import CLI command handlers from
  `internal/app`, move any reusable Codex argument construction into a small
  shared helper package or duplicate only the minimal tested mapping.
- Codex Stage 3 streaming can start as unsupported. If a stream sink is present,
  run one-shot Codex and publish normal stage completion; do not block the
  feature on parity with Claude stream-json.
- Errors must include provider-specific stderr/stdout context, matching the
  current `commandError` behavior.

### Classifier Session Reuse

The existing classifier session pool is Claude-specific because it uses
preallocated Claude session IDs and `--resume`. For the first Codex version:

- Keep session pooling for Claude.
- Disable pooling for Codex and run one-shot `codex exec` classifier calls.
- Keep the budget guard and failure cooldown provider-neutral.

Codex pooling can be added later if the runtime provides a stable equivalent.

### Model Defaults

Add provider-aware defaults:

- Claude classifier default: existing `claude-haiku-4-5`.
- Codex classifier default: `gpt-5.4-mini`.
- Claude deep triage default: current Claude default behavior unless an
  explicit env override is provided.
- Codex deep triage default: `gpt-5.4` unless an explicit env override is
  provided.
- Claude send/capture defaults stay as they are.
- Codex send/capture defaults should prefer `gpt-5.4` for judgment-heavy work
  and `gpt-5.4-mini` only for cheap classifier stages.

Use explicit model env vars only where already useful:

- Preserve `FLOW_STEERING_CLASSIFIER_MODEL`.
- Preserve `FLOW_STEERING_SEND_MODEL` for Claude send sessions.
- Add Codex-aware interpretation only when the selected provider is Codex. A raw
  model value should pass through to the underlying provider CLI.

---

## 7. Attention Actions

### Make Task

Change the Attention task spawner to accept a provider:

```text
flow spawn ... --agent <FLOW_STEERING_PROVIDER> --no-open
```

`make-task-start` then starts that newly-created task through the normal server
session path, which already reads the task's stored provider.

### Forward and Handoff

No provider override. These target existing tasks and therefore use the task's
stored `session_provider`.

### Matched-Task Send Reply

No provider override. The reply is injected into the matched task inbox and that
task's own session is resumed.

### GitHub No-Task Send Reply

Use the selected Attention provider. Both Claude and Codex can invoke local CLI
commands, so GitHub posting via `gh` is a valid provider-neutral path as long as
the prompt explicitly says to use `gh`.

### Slack No-Task Send Reply

Keep using the Claude floating send session. Add UI/server copy that indicates
the send runtime is Claude because Slack MCP posting is currently Claude-only.

### Capture KB

Use the selected Attention provider. This is local filesystem work and does not
depend on connector MCP tools.

---

## 8. UI Design

Add an "Attention agent" provider selector to the Attention config page.

Placement:

- In the Performance section, above reply/classifier settings, or in a new
  compact "Agent" card before Autonomy.
- Use the existing `AgentPicker` component and the existing provider
  capabilities data so unavailable providers render disabled.

Display behavior:

- The saved value is `FLOW_STEERING_PROVIDER`.
- Saving uses the existing `update-settings` action.
- If Codex is unavailable, the Codex segment is disabled with the capability
  reason.
- The config help should explain that matched tasks keep their own provider.
- Slack send-reply should keep showing a Claude floating terminal chip when it
  uses the Claude-only send path.

Trace/detail behavior:

- `attention_trace.model` should include provider/model when possible, for
  example `codex:gpt-5.4-mini` or `claude:claude-haiku-4-5`.
- The feed card does not need a per-card provider badge in the first version.

---

## 9. Tests

Backend:

- Settings expose and validate `FLOW_STEERING_PROVIDER`.
- Claude remains the default when the setting is absent.
- Invalid provider values are rejected by settings validation.
- Classifier runner builds Claude commands when provider is Claude.
- Classifier runner builds Codex commands when provider is Codex.
- Stage 3 uses Codex when configured and falls back cleanly on command errors.
- Attention make-task spawns `--agent codex` when the setting is Codex.
- Matched-task forward/send does not override the matched task provider.
- Slack no-task send remains Claude even when the setting is Codex.
- GitHub no-task send uses the configured provider.
- Capture KB uses the configured provider.

Frontend:

- Attention config renders the provider picker.
- Saving the picker posts `FLOW_STEERING_PROVIDER`.
- Unavailable provider options render disabled.

Verification:

- `make ui` after UI changes.
- Focused Go tests for steering, server settings, and attention actions.
- `go test ./...`.
- `git diff --check`.

---

## 10. Rollout

1. Add the setting and UI picker with default `claude`.
2. Introduce provider-aware headless runners while preserving existing Claude
   behavior.
3. Route Attention-created tasks through the selected provider.
4. Route provider-safe headless actions through the selected provider.
5. Leave Slack no-task send on Claude with explicit copy and tests.
6. Update the embedded Flow skill so future sessions understand the Attention
   provider setting and the Slack exception.

This can ship without a database migration. The setting lives in config.json
and the selected provider is persisted on each newly-created task through the
existing `tasks.session_provider` column.

---

## 11. Risks

- Codex `exec` output formatting may differ from Claude. JSON extraction must
  remain tolerant of prose and fences.
- Codex does not have the Claude classifier session-pool semantics. Initial
  Codex support should prefer correctness over session reuse.
- Provider/model env vars can become confusing if the same model key is used for
  both Claude and Codex. Keep default helpers provider-aware and pass raw
  overrides through only when explicitly set.
- Slack no-task send could be misread as respecting the global provider. The UI
  and server response should make the Claude-only send runtime visible.

---

## 12. Acceptance Criteria

- The operator can choose Claude or Codex for Attention from Mission Control.
- New Attention triage runs use the selected provider.
- New Attention-created tasks are stored with the selected `session_provider`.
- Existing matched tasks are resumed with their stored provider.
- Slack no-task send-reply remains a verified Claude send session.
- Tests cover settings, runner selection, task spawning, matched-task behavior,
  Slack exception behavior, and UI save behavior.
- The embedded Flow skill documents the new switch and the Slack exception.
