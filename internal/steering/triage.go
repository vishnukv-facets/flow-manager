// internal/steering/triage.go
package steering

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// deepTriageRunner shells out to the capable (default) Claude model for the
// Stage 3 deep triage: it reads full context (e.g. via the Slack MCP), drafts
// a reply, and emits a final Verdict. Tests swap this var. Unlike the cheap
// classifier it does NOT pin --model, so the operator's default (capable)
// model is used.
var deepTriageRunner = func(ctx context.Context, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, "claude", "-p", prompt, "--dangerously-skip-permissions")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("steering: deep triage claude -p: %w", err)
	}
	return string(out), nil
}

// DeepTriage runs Stage 3 on a single survivor. Callers that already fetched a
// deterministic thread context should use DeepTriageWithContext; this wrapper is
// kept for narrow tests and older call sites and passes an event-only fallback
// pack rather than asking the model to fetch context itself.
func DeepTriage(ctx context.Context, in ClassifyInput, taskIndex string) (Verdict, error) {
	return DeepTriageWithContext(ctx, in, taskIndex, contextFromClassifyInput(in))
}

// DeepTriageWithContext runs Stage 3 with the explicit context pack assembled
// by Go. The draft is SURFACED only — P1 never auto-sends it.
func DeepTriageWithContext(ctx context.Context, in ClassifyInput, taskIndex string, pack ThreadContext) (Verdict, error) {
	raw, err := deepTriageRunner(ctx, deepTriagePromptWithContext(in, taskIndex, pack))
	if err != nil {
		return Verdict{}, err
	}
	return parseVerdict(raw, in.Source, in.ThreadKey)
}

func deepTriagePrompt(in ClassifyInput, taskIndex string) string {
	return deepTriagePromptWithContext(in, taskIndex, contextFromClassifyInput(in))
}

func deepTriagePromptWithContext(in ClassifyInput, taskIndex string, pack ThreadContext) string {
	payload, _ := json.Marshal(in)
	contextPayload, _ := json.Marshal(pack)
	return `MODE: stage3-deep

You are the deep-triage step of an operator's attention router. A cheap gate has already decided this message is worth a closer look. Go has already fetched the surrounding source context into the context pack below. Treat that context pack as the primary source of truth; do not rely on fetching Slack/GitHub context yourself. If fetch_status is "error" or "unavailable", proceed from the fallback event context and lower confidence when the missing context matters.

Do the following, then emit a single verdict:

1. Read the context pack's source permalink, parent message, replies/comments, participants, timestamps, and pre-summary.
2. Decide whether this message belongs to an EXISTING task (set matched_task) or warrants a new one. Do NOT decide from the task name alone — for any plausibly related task (especially ones in the project this message seems to belong to), use your file tools to READ that task's brief.md AND the progress notes in its updates/ directory (paths are given in the index below) before judging. A message belongs to an existing task when it continues, follows up on, or is the next step of the work that task covers — even if it arrives in a different Slack thread/DM. Prefer matched_task to an existing active task in such cases; only treat it as net-new when, after reading, no active task actually covers it.
3. If a reply from the operator is appropriate, draft it in the operator's voice. DO NOT SEND ANYTHING — the draft is surfaced for the operator's approval only.

Always refer to people and channels by name; never output raw platform IDs (e.g. Slack user IDs like U0123, channel IDs like C0123).

Respond with ONLY a minified JSON object (no prose, fences allowed but optional):
{"suggested_action":"make_task|forward|reply|afk_reply|digest_only|drop","matched_task":"<slug or empty>","suggested_project":"<slug or empty>","suggested_priority":"high|medium|low","urgency":"urgent|normal|low","confidence":0.0,"summary":"<= 140 chars","draft":"<reply text, if any>","reason":"<why>"}

Operator task/project index:
` + taskIndex + `

Context pack (JSON):
` + string(contextPayload) + `

Message (JSON):
` + string(payload)
}

func contextFromClassifyInput(in ClassifyInput) ThreadContext {
	pack := ThreadContext{
		Source:      in.Source,
		ThreadKey:   in.ThreadKey,
		FetchStatus: "unavailable",
		FetchError:  "deterministic context pack was not provided",
	}
	if in.Text != "" || in.Author != "" {
		pack.Parent = &ContextMessage{Kind: "event", Author: in.Author, Text: in.Text}
	}
	pack.Participants, pack.Timestamps = deriveContextMeta(pack.Parent, pack.Messages)
	pack.Summary = summarizeThreadContext(pack.Source, pack.Parent, pack.Messages)
	return pack
}
