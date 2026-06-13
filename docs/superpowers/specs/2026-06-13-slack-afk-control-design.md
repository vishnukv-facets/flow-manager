# Chats — persistent multi-front-end agent conversations (incl. Slack AFK control) — design

**Date:** 2026-06-13 (rev 2 — scope expanded from "Slack command channel" to "Chats")
**Task:** `slack-afk-control` (project: flow-manager)
**Status:** design / in implementation
**Supersedes:** rev 1 of this file (Slack-only command channel). Rev-1 Slack mechanics are retained below as the "Slack front-end."

## 1. Problem & motive

The operator wants to converse with flow's command-center agent — an agent that can run work on their laptop — from multiple places and revisit those conversations later:

- **From their phone while AFK**, via a private chat (the original ask). Privacy constraint: flow must not see/contact their contacts.
- **From Mission Control**, with multiple chats open, choosing Claude or Codex per chat.
- **Revisit/manage** past chats: a list with titles, a badge for Slack-originated ones, archive/delete, and reopen.

## 2. Key realization: most of this already exists — reuse it

flow already ships the chat substrate:

- **The chat = an `overview-chat` command-center agent session.** Ask Flow ("Open session", `AskFlow.tsx`) calls `action {kind:'overview-chat', prompt, provider}` → `overviewChat()` (`actions.go:2178`) → a provider-picked agent session whose brief (`overviewBrief`, `actions.go:2209`) is *"You are the Flow overview command-center agent… inspect Flow/GitHub/Slack context… route work into tasks or sessions."* That is exactly "chat with flow, and it does things," with a Claude/Codex picker (`AgentPicker`).
- **Multiple at once.** Floating-terminal sessions (`terminal_bridge.go` `registerFloatingLaunch`, the tray, `floating_persist.go`) already support many concurrent sessions and survive a server restart while their tmux is alive.
- **Send into a live session.** `nudgeSession` (`actions.go:367`) injects text into a live/resumable session and wakes it.

So this feature is **not** a new chat console. It is: (a) make those chats **durable & manageable** (a Chats page), and (b) add **Slack as a remote front-end** to the same session model.

## 3. Architecture

```
        Ask Flow (UI)        Chats page (UI)         Slack DM (phone)
             │                    │                       │
        overview-chat        list/reopen/            command channel
        action               archive/delete          (operator↔bot IM)
             │                    │                       │
             └──────────┬─────────┴───────────┬───────────┘
                        ▼                      ▼
              chat-backed flow task    (origin tag: ui | slack)
                        │
                        ▼
            overview-chat agent session (provider claude|codex)
              ├─ live: floating-terminal / tmux PTY  (reopen/attach)
              └─ history: transcript + inbox
```

### 3.1 The chat model (durable registry — NOT task-backed)

**Finding (verified):** `prepareOverviewFloatingLaunch` (`terminal_bridge.go:1529`) builds a `terminalLaunch{FreeAgent: true, Slug: "overview-<uuid>", SessionID, Provider, …}` — overview-chat sessions are **deliberately task-less** ("carries no task row (FreeAgent), so nothing lands in the Tasks list"). They persist in `floating-sessions.json` only while their tmux is alive; `loadFloatingFromDisk` drops dead ones on boot.

**Decision — a dedicated, durable `chats` registry, additive to the existing FreeAgent launch.** Do NOT retrofit task-backing: it regresses the intentional task-less design, pollutes the Tasks board, and drags in status/project/brief baggage a chat doesn't want. Instead add a small **SQLite `chats` table** (flow's data idiom), written when a floating chat launches (UI or Slack):

| column | meaning |
|---|---|
| `slug` (PK) | the floating launch slug (`overview-<uuid>`) |
| `title` | derived from the first prompt (truncated), later editable |
| `provider` | claude \| codex |
| `origin` | `ui` \| `slack` |
| `session_id` | the agent session (for reopen/reattach) |
| `created_at`, `last_activity_at` | recency ordering |
| `archived_at`, `deleted_at` | nullable; archive/delete flags |

A chat is durable/revisitable independent of the live PTY. **Reopen** = `registerFloatingLaunch` from the stored launch/session (reattach if tmux alive, else resume the session id in a fresh floating terminal). **Archive/Delete** = set the timestamp. This is additive — the working Ask Flow / overview-chat launch path is unchanged except for one "record a chat row" call. (`origin` is a column here, not a `chat-origin:` task tag — §3.2/§3.3 references to that tag mean this column.)

### 3.2 The Chats page (new UI screen)

- New top-level nav item **"Chats"** (Workspace group; `Shell.tsx` nav + `app.tsx` route). Distinct from "Mission Control" (the Overview screen).
- **List** of chats from `GET /api/chats` (recent first by `updated_at`): title, provider badge, **Slack badge** when `chat-origin:slack`, a live/idle status dot (joined from the floating-terminal registry), last-activity time.
- **Actions per chat:** Reopen (→ `registerFloatingLaunch` reattach, like the tray), Archive, Delete (existing task ops via `/api/actions`).
- **Open chat view:** reuse the floating terminal for the live session; the message history reuses the `Conversation`/`TranscriptTab` render patterns. (v1 may simply reopen the floating terminal; a chat-bubble panel is a v1.1 nicety.)
- Live updates: `publishUIChange("chats")` on chat create/activity + a focused key in `liveInvalidation.ts` so only the chats query refetches (avoid broad invalidation — `buildUIData` hot-path discipline).
- Styling: existing `.page`/`.card`/`.btn`/`.badge` conventions; no glass/gradient/generic-SaaS.

### 3.3 Slack front-end (rev-1 mechanics, re-pointed)

The Slack command channel routes an operator DM into a chat session instead of a bespoke `slack-remote` task:

- **Bot DM-able** (done — Tasks 1–3): manifest App Home messages tab, `message.im` bot event, `im:history`/`im:write` scopes, reinstall signal, branding.
- **Command detection** (done — Tasks 4–5): `CommandChannelEnabled` gate (opt-in), `AuthorizedOperator` allowlist (`FLOW_SLACK_SELF_USER_IDS`), `IsCommandChannel` (resolved operator↔bot IM id).
- **Dispatch → chat** (Task 6, re-pointed): an authorized operator DM short-circuits in `dispatchMessage` (before the steerer, bypassing the self-authored drop) and **opens or continues a chat session** tagged `chat-origin:slack`. First DM creates the chat (via the same `overview-chat`/launch path, provider = the configured Slack default, e.g. `FLOW_SLACK_COMMAND_PROVIDER`); subsequent DMs `nudge` the existing chat session. The chat appears on the Chats page with a Slack badge.
- **Reply** (Tasks 9–11): the agent replies to the DM as the **bot** via `flow slack send` → `monitor.SendAsBot` (bot token, write-gated). Per the codebase invariant, the agent does the sending from its own session; the helper is the bot-identity transport.

### 3.4 Cross-surface unity

One chat session, reachable three ways: Ask Flow (open in UI), the Chats page (revisit/reopen), and Slack (continue from phone if `chat-origin:slack` or explicitly bound). A Slack-started chat is a normal chat in the UI; a UI chat can be the Slack-bound one.

## 4. Reuse vs new

| Concern | Reuse | New |
|---|---|---|
| Chat session + provider | `overview-chat` action, `overviewBrief`, `AgentPicker` | `kind=chat` + origin tag on the backing task |
| Multiple / reopen / live | floating terminals, tray, `floating_persist.go`, `registerFloatingLaunch` | join live state into the chats list |
| Send into session | `nudgeSession` | UI composer / Slack command both call it |
| History | transcript + inbox + `Conversation`/`TranscriptTab` | (optional) chat-bubble panel |
| List / manage | task `archive`/`delete`, `updated_at` | `GET /api/chats`, Chats screen + nav |
| Slack front-end | command channel (Tasks 1–6), `SendAsBot` | re-point Task 6 to open/continue a chat |

## 5. Security & the operator-proxy model

The Slack command channel is a remote-execution surface: opt-in `FLOW_SLACK_COMMAND_ENABLED` (default off), sender allowlist (`FLOW_SLACK_SELF_USER_IDS`), bot-write gate (`FLOW_SLACK_WRITES_ENABLED`). The UI Chats surface is local-trust (same as Ask Flow today). Deleting a chat soft-deletes its `chats` row and stops the live session.

**The Slack chat agent is a full operator-proxy.** It is a real Claude/Codex session on the laptop with full tool access, so by design it can do anything the operator could at the keyboard — run shell/code, and orchestrate the operator's *other* flow work: `flow list/show task`, `flow transcript` (check progress), `flow tell` (message a running session), `flow spawn` / `flow do <task> --auto`, `flow list playbooks` / `flow run playbook <slug> --auto`, `flow wait`. The brief (`slackReplyInstructions`, `chat_sink.go`) makes these explicit so the agent reaches for them.

**Two-layer permission model (resolves rev-1 open question 4):**
- *Native layer:* the Slack chat runs DETACHED for an AFK operator, so it launches in **`bypass`** (`slackChatPermissionMode`, `chat_sink.go`) — native tool-approval prompts have no one to answer and would hang the session. (Matches the detached send-reply floating session precedent.)
- *Semantic layer (human-in-the-loop):* the brief instructs the agent to **self-gate** — before any destructive/irreversible/out-of-scope action (delete data, force-push, prod deploy, spend money, mass changes) it must ask the operator over the Slack DM (`flow slack send`) and WAIT for the reply (which routes back into the same session as the next command) before proceeding. Routine safe work proceeds without asking. This is the AFK-appropriate substitute for native prompts, which can't reach a phone.

## 6. Open questions

1. **Does `overviewChat` already create a durable task?** Confirm in `prepareOverviewFloatingLaunch` (`terminal_bridge.go:1506`). If yes → tag it `kind=chat`. If no → add backing at launch. (Resolved during Task R1 below.)
2. **Chat view in the Chats page: floating-terminal reattach (v1) vs an embedded chat-bubble panel (v1.1).** Recommend v1 = reopen floating terminal (zero new render work); bubble panel deferred.
3. **Slack default provider** for a Slack-initiated chat: `FLOW_SLACK_COMMAND_PROVIDER` (default claude) vs always prompt. Recommend env default.
4. ~~Destructive-command guardrails~~ — **RESOLVED**: two-layer model in §5 (bypass native + brief-driven Slack-ask self-gate).

## 7. Out of scope (v1)

- WhatsApp/Telegram.
- Automated Slack icon upload (guided manual only).
- A bespoke chat-bubble renderer (reuse floating terminal / transcript first).
- Per-Slack-thread multi-chat mapping (Slack drives one chat at a time).

## 8. Build order (revised)

**Phase A — Slack bot plumbing (DONE):** Tasks 1–5 (manifest DM-ability, reinstall signal, command flag, allowlist, IM detection). Unchanged and complete.

**Phase B — Chat backing + Chats page:**
- R1: Confirm/establish `kind=chat` backing for `overview-chat` sessions + `chat-origin` tag; add `chat-origin:ui` on Ask Flow opens.
- R2: `GET /api/chats` — list chat-kind tasks (recent), joined with live floating state; register route.
- R3: Chats screen + nav + react-query hook + focused live-invalidation key.
- R4: Reopen / archive / delete wired to existing floating-launch + task actions.

**Phase C — Slack front-end:**
- Task 6 (re-pointed): operator DM opens/continues a `chat-origin:slack` chat session (not a `slack-remote` task).
- Task 7 (re-pointed): the chat agent brief covers command semantics + bot reply.
- Tasks 8–11: cache invalidation; `SendAsBot`; `flow slack send`; register subcommand.
- Task 12: inert-when-disabled + suites green.

**Phase D — Branding:** Tasks 13–14 (icon asset + guided wizard step).

## 9. Testing

- Backend: `kind=chat` filtering + `/api/chats` join (unit, real SQLite temp DB); command-channel dispatch → chat open/continue (the `routeCommand` tests, re-pointed); `SendAsBot` gating; inert-when-disabled. Mock Slack via function vars.
- UI: Chats list renders, Slack badge shows for `chat-origin:slack`, reopen/archive/delete fire the right actions (component tests where the repo has them; otherwise manual via `make ui`).
- Manual: Ask Flow open → appears on Chats page; Slack DM → chat appears with Slack badge → reopen in UI; archive/delete.
