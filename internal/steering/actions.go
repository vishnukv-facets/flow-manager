package steering

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"flow/internal/flowdb"
)

// taskSpawner shells out to `flow spawn` to create a task from a feed item
// (mirrors the monitor.spawnFlowTask seam). Mockable in tests.
var taskSpawner = func(ctx context.Context, name, slug, brief, project string) error {
	args := []string{"spawn", name, "--slug", slug, "--priority", "high", "--prompt", brief, "--no-open", "--agent", "claude"}
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

// taskTeller shells out to `flow tell` to forward a context block into an
// existing task's inbox. Mockable in tests.
var taskTeller = func(ctx context.Context, slug, message string) error {
	cmd := exec.CommandContext(ctx, "flow", "tell", slug, message)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("steering: flow tell %s: %w (output: %s)", slug, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// MakeTaskFromFeed spawns a flow task from a feed item's pre-assembled context
// pack and marks the feed row 'acted'.
func MakeTaskFromFeed(ctx context.Context, db *sql.DB, item flowdb.FeedItem) error {
	if err := taskSpawner(ctx, feedTaskName(item), feedTaskSlug(item), feedTaskBrief(item), item.SuggestedProject); err != nil {
		return err
	}
	return markActed(db, item.ID)
}

// ForwardFeed hands a summarized context block to the matched task's inbox via
// `flow tell` and marks the feed row 'acted'. Requires item.MatchedTask.
func ForwardFeed(ctx context.Context, db *sql.DB, item flowdb.FeedItem) error {
	target := strings.TrimSpace(item.MatchedTask)
	if target == "" {
		return fmt.Errorf("steering: forward requires a matched_task on feed item %q", item.ID)
	}
	if err := taskTeller(ctx, target, feedForwardMessage(item)); err != nil {
		return err
	}
	return markActed(db, item.ID)
}

// DismissFeed marks a feed row 'dismissed' (no external effect).
func DismissFeed(db *sql.DB, id string) error {
	return flowdb.SetFeedItemStatus(db, id, "dismissed", nowRFC3339())
}

func markActed(db *sql.DB, id string) error {
	return flowdb.SetFeedItemStatus(db, id, "acted", nowRFC3339())
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

// feedTaskName is a short task title derived from the summary (or the thread
// key when there's no summary).
func feedTaskName(item flowdb.FeedItem) string {
	if s := strings.TrimSpace(item.Summary); s != "" {
		if len(s) > 60 {
			s = strings.TrimSpace(s[:60])
		}
		return s
	}
	return "Attention: " + item.ThreadKey
}

// feedTaskSlug derives a stable, filesystem-safe slug from the thread key.
func feedTaskSlug(item flowdb.FeedItem) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(item.ThreadKey) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		s = "thread"
	}
	return "att-" + s
}

// feedTaskBrief assembles the context-pack brief for a new task (spec §8.2).
func feedTaskBrief(item flowdb.FeedItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", feedTaskName(item))
	summary := strings.TrimSpace(item.Summary)
	if summary == "" {
		summary = "Follow up on a message surfaced by the attention router."
	}
	fmt.Fprintf(&b, "## What\n%s\n\n", summary)
	fmt.Fprintf(&b, "## Why\nSurfaced by the attention router from %s.\n", item.Source)
	if r := strings.TrimSpace(item.Reason); r != "" {
		fmt.Fprintf(&b, "Reason flagged: %s\n", r)
	}
	fmt.Fprintf(&b, "\n## Source\nthread: %s (%s)\n", item.ThreadKey, item.Source)
	if d := strings.TrimSpace(item.Draft); d != "" {
		fmt.Fprintf(&b, "\n## Suggested reply (draft — review before sending)\n%s\n", d)
	}
	b.WriteString("\n---\n*Created from the attention feed. Read the linked thread before acting.*\n")
	return b.String()
}

// feedForwardMessage is the summarized context block forwarded to a matched
// task's inbox (spec §8.3).
func feedForwardMessage(item flowdb.FeedItem) string {
	var b strings.Builder
	b.WriteString("Forwarded by the attention router.\n")
	if s := strings.TrimSpace(item.Summary); s != "" {
		fmt.Fprintf(&b, "Summary: %s\n", s)
	}
	fmt.Fprintf(&b, "Source thread: %s (%s)\n", item.ThreadKey, item.Source)
	if r := strings.TrimSpace(item.Reason); r != "" {
		fmt.Fprintf(&b, "Why it may relate: %s\n", r)
	}
	return b.String()
}

// ErrAutonomyDenied is returned when an autonomous (non-manual) action is
// blocked by the autonomy policy.
var ErrAutonomyDenied = errors.New("steering: action denied by autonomy policy")

// ApplyAction performs action on a feed item. manual=true (operator-initiated)
// bypasses the autonomy gate — the operator IS the authorization. manual=false
// (autonomous) must pass autonomy.Allow(action, item.Confidence) or it returns
// ErrAutonomyDenied without side effects. Only make_task and forward are
// supported in P1.3; reply/afk_reply (outward sends) arrive in P2.
func ApplyAction(ctx context.Context, db *sql.DB, item flowdb.FeedItem, action Action, autonomy AutonomyPolicy, manual bool) error {
	if !manual && !autonomy.Allow(action, item.Confidence) {
		return ErrAutonomyDenied
	}
	switch action {
	case ActionMakeTask:
		return MakeTaskFromFeed(ctx, db, item)
	case ActionForward:
		return ForwardFeed(ctx, db, item)
	default:
		return fmt.Errorf("steering: action %q not supported in P1.3 (make_task/forward only)", action)
	}
}
