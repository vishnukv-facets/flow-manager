# Same-Session Event Monitor

## Purpose

Flow should support generic monitoring that wakes the same task-bound agent session when external work arrives. The source can be GitHub PR review, Slack thread activity, CI results, deployment events, or a future integration. Flow is the detection and delivery layer. The original Claude or Codex task session remains responsible for understanding the event, deciding what to do, changing code or replying, testing, committing, pushing, and closing the loop.

This is not a headless worker that handles events outside the task. It is a same-session wake-up workflow.

## Non-Negotiable Behavior

- Every monitored source maps incoming activity to a flow task.
- Source events are appended to that task's `inbox.jsonl`.
- A task-local monitor watches the inbox and wakes the already-running task session.
- For Codex, the primary path must signal the same Flow-owned Codex terminal/session. `codex resume` is not the primary wake-up mechanism because it can create a separate process and split execution ownership.
- If the task session is not running, flow may start or reopen the task through the normal Flow-owned terminal path, then let the same-session monitor take over.
- The monitor never applies code changes, posts replies, merges PRs, or resolves events itself.

## Current State

The repo already has:

- Slack monitor ingestion through `monitor.SlackListener`.
- GitHub monitor ingestion through `monitor.GitHubListener`.
- Source-specific task tags such as `slack-thread:<channel>:<thread_ts>`, `gh-pr:<owner>/<repo>#<n>`, and `gh-issue:<owner>/<repo>#<n>`.
- SQLite idempotency for GitHub through `github_event_log`.
- `inbox.jsonl` append/read helpers in `internal/monitor/inbox.go`.
- Flow-owned Claude and Codex terminal sessions in Mission Control.
- Codex repo-local hooks through `.codex/hooks.json`, gated by `FLOW_HOOK_OWNED=1`.

The missing pieces are:

- A generic task inbox monitor that wakes the same session for any actionable inbox event.
- A generic source-event contract so Slack, GitHub, and future producers can mark which events should wake the agent.
- Terminal/session injection that wakes the existing Flow-owned session instead of spawning a competing agent process.
- GitHub-specific completion of PR linkage and top-level review state events.

## Architecture

The feature splits into four roles.

### 1. Event Producers

Producers detect source activity and normalize it into task inbox entries. Existing examples:

- Slack listener: reactions create tasks; thread messages append to tracked Slack tasks.
- GitHub listener: assignments, review requests, PR review comments, PR head changes, and merge state append to tracked GitHub tasks.

Future producers should follow the same contract:

- Resolve or create the target task.
- Add durable source tags.
- Record source-level idempotency where needed.
- Append a normalized inbox event.
- Avoid executing the requested work directly.

### 2. Inbox Event Contract

The existing `InboxEntry` remains the durable delivery envelope. The event payload needs enough normalized metadata for generic wake-up decisions:

- `source`: slack, github, ci, deploy, or another source name.
- `kind`: source-specific event kind, such as `message`, `pr_review_comment`, or `ci_failed`.
- `actionable`: whether this event should wake the task agent.
- `summary`: concise human/model-readable prompt material.
- `url` or source locator when available.
- `raw_json`: original payload for source-specific recovery.

This can be added compatibly by extending the existing event payload or adding a thin wrapper around it. Existing inbox lines must remain readable.

### 3. Task Inbox Monitor

Each Flow-owned task session can have an inbox monitor process bound to its task slug and inbox path. The monitor stores its own cursor so it reacts only to new lines after it is armed.

The monitor's job is intentionally small:

- Watch `~/.flow/tasks/<slug>/inbox.jsonl`.
- Detect new actionable events from any source.
- Coalesce bursts so one Slack thread burst or PR review batch does not spam the agent.
- Signal the live task terminal with a concise wake-up prompt.
- Continue or exit according to the terminal lifecycle.

It does not call source APIs except where a future source explicitly provides a read-only enrichment hook. It never performs the requested work itself.

### 4. Same-Session Wake-Up

Flow already owns browser-terminal PTYs for Mission Control sessions. The reliable implementation path is to inject a prompt into that PTY for the same task session:

```text
New monitored event arrived for this flow task.
Read ~/.flow/tasks/<slug>/inbox.jsonl from the last unhandled event, inspect the source context if needed, and continue the task in this same session.
```

For Claude and Codex this should use the same terminal transport abstraction. Provider-specific differences stay in session bootstrap and hooks, not in Slack/GitHub handling.

## Source Examples

### Slack

1. A Slack reaction creates or links a task with `slack-thread:<channel>:<thread_ts>`.
2. Later thread messages append to the task inbox.
3. The inbox monitor wakes the same session.
4. The agent reads the inbox, uses Slack MCP if needed, and decides whether to reply.

### GitHub PR Review

1. A task creates or discovers PR `owner/repo#123`.
2. Flow tags the task with `gh-pr:owner/repo#123`.
3. GitHub listener appends PR review comments, `CHANGES_REQUESTED`, head changes, approvals, or merge state events.
4. The inbox monitor wakes the same session.
5. The agent reads the inbox, fetches PR context if needed, refixes the PR, tests, commits, pushes, and keeps monitoring.

### CI Or Deployment

1. A future producer links a build/deploy source to a task.
2. Failure or recovery events append to the inbox.
3. The same monitor wakes the session.
4. The agent diagnoses or records status according to the task brief.

## Agent Bootstrap

Task bootstrap text and the embedded flow skill should tell agents:

- Read existing `inbox.jsonl` on startup.
- Arm the same-session event monitor for the task.
- Treat actionable inbox events as wake-up triggers, regardless of source.
- Use source-specific tools only after reading the durable inbox event.
- Keep source side effects under the same normal agent standards: verify before claiming, do not auto-post or auto-merge blindly, and preserve the task's scope.

For Codex, bootstrap must say that the monitor is part of the Flow-owned task session and must not use a separate `codex resume` process as the normal wake-up path.

## Error Handling

- Duplicate source events are ignored through source-specific event keys.
- Malformed inbox lines are skipped, matching current inbox reader behavior.
- If no live terminal session exists, mark the task as needing attention and optionally open it through the Flow-owned terminal bridge.
- If prompt injection fails, leave the inbox event durable and surface a warning in Mission Control.
- If the task has multiple live terminals, prefer the Flow-owned terminal session recorded for the task and warn rather than fan out.
- If a source listener fails, keep other listeners and inbox monitors alive.

## Out Of Scope

- A headless solver process that edits code, posts messages, or resolves source events independently of the task agent.
- A second `codex resume` session as the primary wake-up path.
- Webhook delivery. Polling/listening sources can be added later behind the same inbox contract.
- Source-specific UI polish beyond what is needed to show wake-up status and errors.

## Testing Plan

- Unit test the generic inbox monitor cursoring ignores old lines and signals only new actionable events.
- Unit test burst coalescing sends one wake-up for a batch of related inbox lines.
- Unit test same-session wake-up calls a terminal injection interface, not `codex resume`.
- Unit test Slack message events can be classified as generic actionable inbox events.
- Unit test GitHub PR review events can be classified as generic actionable inbox events.
- Unit test PR creation/linkage records `gh-pr:<owner>/<repo>#<n>` on the original task.
- Unit test GitHub review polling emits top-level review events with stable event keys.
- Integration-style server test verifies a Codex task receives a terminal wake-up through the Flow-owned bridge.
- Regression test the embedded skill/bootstrap text documents generic same-session monitoring for both Claude and Codex.

## Acceptance Criteria

- The wake-up mechanism is generic across Slack, GitHub, and future inbox producers.
- New actionable inbox events wake the same Flow-owned task session.
- Codex support uses same-session terminal signalling as the primary path.
- No implementation path automatically handles source events outside the task agent session.
- Existing Slack task creation and GitHub assignment/review-request task creation continue to work.
- GitHub PRs created from flow tasks become discoverable by the generic monitor through durable task tags.
