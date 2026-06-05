package steering

import (
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"strings"

	"flow/internal/flowdb"
)

// sendReplyRunner runs a hidden Haiku `claude -p` session that POSTS an
// operator-approved reply via the agent's own connector MCP (Slack/GitHub) — the
// same headless, bypass-permission agent layer the triage stages already use to
// READ threads. Bypass is correct here: the operator approved the exact text via
// the attention feed, so there's nothing left to gate. Mockable in tests.
var sendReplyRunner = func(ctx context.Context, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, "claude", "-p", prompt,
		"--model", classifierModel(), "--dangerously-skip-permissions")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("steering: send-reply claude -p: %w", err)
	}
	return string(out), nil
}

// SendReplyViaAgent posts an operator-approved reply WITHOUT spawning a visible
// flow task. A hidden Haiku agent session (bypass + MCP) posts the reply to the
// source thread, then the feed item is marked acted. This is the operator's
// intended flow: the hidden triage-layer session that surfaced the item sends
// it, rather than creating a new task that then trips the auto-mode permission
// gate. Returns an error (leaving the card unresolved so it can be retried) when
// the agent reports it could not post.
func SendReplyViaAgent(ctx context.Context, db *sql.DB, item flowdb.FeedItem, text, instructions string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("steering: send-reply requires non-empty text")
	}
	out, err := sendReplyRunner(ctx, sendReplyPrompt(item, text, instructions))
	if err != nil {
		return err
	}
	if strings.HasPrefix(strings.TrimSpace(out), "ERROR:") {
		return fmt.Errorf("steering: send-reply agent did not post: %s", strings.TrimSpace(out))
	}
	// No linked task — the hidden agent posted directly.
	return flowdb.SetFeedItemActed(db, item.ID, "", nowRFC3339())
}

func sendReplyPrompt(item flowdb.FeedItem, text, instructions string) string {
	// With no extra instructions: post the approved draft as-is (minor wording
	// only). With instructions: the draft is a starting point — revise it per the
	// operator's instructions, then post.
	draftClause := "The operator has ALREADY APPROVED the reply below and asked you to POST it NOW. Do not ask for confirmation and do not redraft beyond minor wording — just post it."
	if ins := strings.TrimSpace(instructions); ins != "" {
		draftClause = "The operator approved sending a reply on this thread and gave you specific instructions. Start from the draft below, APPLY the operator's instructions to revise it, then POST the result. Do not ask for confirmation.\n\nOperator instructions:\n" + ins
	}
	return `MODE: send-reply

You are the send step of an operator's attention router. ` + draftClause + `

1. Post the reply to the source thread using your MCP tools. ` + contextHintFor(item.Source) + `
   Post it THREADED to the source message (Slack: reply in-thread on thread_ts; GitHub: a comment on the PR/issue).
2. Refer to people and channels by name; never paste raw platform IDs.

Source: ` + item.Source + ` thread ` + item.ThreadKey + `

Draft reply:
` + strings.TrimSpace(text) + `

After posting, reply with a single line: "posted". If you cannot post (the
connector MCP tool isn't available, or the post failed), reply with a single line
starting "ERROR: " and the reason — do not retry endlessly.`
}
