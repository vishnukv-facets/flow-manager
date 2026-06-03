package monitor

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"flow/internal/flowdb"

	"github.com/slack-go/slack"
)

// SlackThreadReplies fetches a thread's replies for reconciliation. Only the
// fields the backfill needs are surfaced (see SlackMessage). `oldest` is a
// Slack ts lower bound (exclusive) so we fetch just the tail of the thread.
type SlackThreadReplies interface {
	Replies(ctx context.Context, channelID, threadTS, oldest string, limit int) ([]SlackMessage, error)
}

type slackRepliesAPIClient struct{ api *slack.Client }

// NewSlackRepliesClient returns a production replies client, or nil when no
// Slack bot/read token is configured — in which case the caller skips
// backfill entirely.
func NewSlackRepliesClient() SlackThreadReplies {
	if strings.TrimSpace(SlackBotToken()) == "" {
		return nil
	}
	return slackRepliesAPIClient{api: slack.New(SlackBotToken())}
}

func (c slackRepliesAPIClient) Replies(ctx context.Context, channelID, threadTS, oldest string, limit int) ([]SlackMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	msgs, _, _, err := c.api.GetConversationRepliesContext(ctx, &slack.GetConversationRepliesParameters{
		ChannelID: normalizeSlackChannelID(channelID),
		Timestamp: strings.TrimSpace(threadTS),
		Oldest:    strings.TrimSpace(oldest),
		Inclusive: false,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]SlackMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, SlackMessage{
			User:     firstNonEmpty(m.User, m.Username),
			Text:     m.Text,
			TS:       m.Timestamp,
			ThreadTS: m.ThreadTimestamp,
			SubType:  m.SubType,
		})
	}
	return out, nil
}

// SlackConversationHistory reads a DM/group-DM channel for backfill. DMs can be
// flat OR threaded ("also send as DM" + threaded replies), so it exposes both
// conversations.history (top-level messages) and conversations.replies (a
// thread's children) — history alone is blind to thread replies. Backed by the
// USER token: the bot can't read the operator's DMs, only the authorizing user.
type SlackConversationHistory interface {
	History(ctx context.Context, channelID, oldest string, limit int) ([]SlackMessage, error)
	Replies(ctx context.Context, channelID, threadTS, oldest string, limit int) ([]SlackMessage, error)
}

type slackHistoryAPIClient struct{ api *slack.Client }

// NewSlackDMHistoryClient returns a production history client backed by the
// user token, or nil when no user token is configured — in which case DM
// backfill is skipped (live socket events still flow; only the restart-gap
// safety net is unavailable).
func NewSlackDMHistoryClient() SlackConversationHistory {
	if strings.TrimSpace(SlackUserToken()) == "" {
		return nil
	}
	return slackHistoryAPIClient{api: slack.New(SlackUserToken())}
}

func (c slackHistoryAPIClient) History(ctx context.Context, channelID, oldest string, limit int) ([]SlackMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	resp, err := c.api.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{
		ChannelID: normalizeSlackChannelID(channelID),
		Oldest:    strings.TrimSpace(oldest),
		Inclusive: false,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	return toSlackMessages(resp.Messages), nil
}

// Replies fetches a DM thread's replies via conversations.replies on the user
// token, so threaded DM messages missed during a gap can be recovered.
func (c slackHistoryAPIClient) Replies(ctx context.Context, channelID, threadTS, oldest string, limit int) ([]SlackMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	msgs, _, _, err := c.api.GetConversationRepliesContext(ctx, &slack.GetConversationRepliesParameters{
		ChannelID: normalizeSlackChannelID(channelID),
		Timestamp: strings.TrimSpace(threadTS),
		Oldest:    strings.TrimSpace(oldest),
		Inclusive: false,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	return toSlackMessages(msgs), nil
}

// toSlackMessages projects slack-go messages into the backfill's compact shape.
func toSlackMessages(msgs []slack.Message) []SlackMessage {
	out := make([]SlackMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, SlackMessage{
			User:     firstNonEmpty(m.User, m.Username),
			Text:     m.Text,
			TS:       m.Timestamp,
			ThreadTS: m.ThreadTimestamp,
			SubType:  m.SubType,
		})
	}
	return out
}

type slackDMMembersClient struct{ api *slack.Client }

// NewSlackDMMembersResolver returns a conversations.members resolver backed by
// the user token (the bot can't enumerate the operator's DMs), or nil when no
// user token is configured — in which case DM auto-registration is disabled.
func NewSlackDMMembersResolver() DMMembersResolver {
	if strings.TrimSpace(SlackUserToken()) == "" {
		return nil
	}
	return slackDMMembersClient{api: slack.New(SlackUserToken())}
}

func (c slackDMMembersClient) DMMembers(ctx context.Context, channelID string) ([]string, error) {
	members, _, err := c.api.GetUsersInConversationContext(ctx, &slack.GetUsersInConversationParameters{
		ChannelID: normalizeSlackChannelID(channelID),
		Limit:     100,
	})
	if err != nil {
		return nil, err
	}
	return members, nil
}

// SlackBackfill is the durable safety net behind the live Socket Mode
// listener. The live listener only sees events delivered while its socket is
// connected; anything that arrives during a disconnect — every server restart,
// any network blip — is lost, because Socket Mode never replays missed events.
// SlackBackfill periodically pulls each monitored thread's recent replies from
// the Slack Web API and appends any that are missing from the task's
// inbox.jsonl, so the Inbox and the same-session monitor eventually see every
// message regardless of socket gaps. It runs independently of the socket, so
// it works even while Socket Mode is mid-reconnect.
type SlackBackfill struct {
	db       *sql.DB
	client   SlackThreadReplies
	dmClient SlackConversationHistory // optional; nil → DM backfill skipped
	interval time.Duration
	limit    int
	logFn    func(string, ...any)
}

// NewSlackBackfill builds a backfiller. A zero interval defaults to 45s — well
// inside Slack's conversations.replies rate budget even with a few dozen
// monitored threads.
func NewSlackBackfill(db *sql.DB, client SlackThreadReplies, interval time.Duration) *SlackBackfill {
	if interval <= 0 {
		interval = 45 * time.Second
	}
	return &SlackBackfill{db: db, client: client, interval: interval, limit: 200, logFn: func(string, ...any) {}}
}

// SetLogger installs a printf-style logger (e.g. the server's). Optional.
func (b *SlackBackfill) SetLogger(fn func(string, ...any)) {
	if fn != nil {
		b.logFn = fn
	}
}

// SetDMHistoryClient installs the user-token conversations.history client used
// to reconcile registered DM channels (slack-dm: tags). Optional — when unset,
// DM channels are not backfilled. Kept a setter (not a constructor arg) so
// existing callers and tests don't have to thread it through.
func (b *SlackBackfill) SetDMHistoryClient(c SlackConversationHistory) {
	b.dmClient = c
}

// Run does an immediate reconciliation pass — catching anything missed while
// the server was down — then repeats every interval until ctx is cancelled.
func (b *SlackBackfill) Run(ctx context.Context) {
	if b == nil || b.db == nil || b.client == nil {
		return
	}
	b.runOnce(ctx)
	t := time.NewTicker(b.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.runOnce(ctx)
		}
	}
}

func (b *SlackBackfill) runOnce(ctx context.Context) {
	// Only non-done Slack-reply tasks: finished threads don't need waking, and
	// the tag is the authoritative source of (channel, thread_ts).
	tasks, err := flowdb.ListTasks(b.db, flowdb.TaskFilter{Tag: "slack-reply", ExcludeDone: true})
	if err != nil {
		b.logFn("slack backfill: list tasks: %v", err)
		return
	}
	for _, task := range tasks {
		if task == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
		tags, err := flowdb.GetTaskTags(b.db, task.Slug)
		if err != nil {
			continue
		}
		// Thread reconcile (origin slack-thread tag).
		if decision, ok := decisionFromSlackThreadTags(tags); ok {
			n, err := b.reconcile(ctx, task.Slug, decision.Channel, decision.ThreadTS)
			if err != nil {
				b.logFn("slack backfill %s: %v", task.Slug, err)
			} else if n > 0 {
				b.logFn("slack backfill %s: recovered %d missed message(s)", task.Slug, n)
			}
		}
		// DM reconcile (any slack-dm tags the agent registered). Independent of
		// the thread reconcile so a DM is recovered even if the thread lookup
		// fails or the task somehow lacks a thread tag.
		if b.dmClient != nil {
			for _, ch := range dmChannelsFromTags(tags) {
				select {
				case <-ctx.Done():
					return
				default:
				}
				n, err := b.reconcileDM(ctx, task.Slug, ch)
				if err != nil {
					b.logFn("slack dm backfill %s (%s): %v", task.Slug, ch, err)
					continue
				}
				if n > 0 {
					b.logFn("slack dm backfill %s (%s): recovered %d missed message(s)", task.Slug, ch, n)
				}
			}
		}
	}
}

// dmChannelsFromTags extracts the channel IDs from a task's slack-dm:<channel>
// tags. Tags are stored normalized (lowercased); the History client uppercases
// them again before calling Slack.
func dmChannelsFromTags(tags []string) []string {
	var out []string
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if ch := strings.TrimPrefix(t, SlackDMTagPrefix); ch != t {
			if ch = strings.TrimSpace(ch); ch != "" {
				out = append(out, ch)
			}
		}
	}
	return out
}

// reconcile appends any thread replies newer than what's already in the task's
// inbox.jsonl. inbox.jsonl is treated as the durable cursor (its max message
// ts), so reconcile self-heals across restarts and never double-appends —
// every candidate is deduped by ts against what's already recorded.
func (b *SlackBackfill) reconcile(ctx context.Context, slug, channel, threadTS string) (int, error) {
	// Per-channel cursor: a task can mix its origin thread with DM channels, so
	// the resume point must be the newest ts in THIS channel, not a global max
	// (a newer DM message must not advance the thread cursor past unseen thread
	// replies, and vice-versa).
	maxTS, seen, err := inboxSlackTSIndexForChannel(slug, channel)
	if err != nil {
		return 0, err
	}
	// No message baseline yet → let the live listener establish the first
	// entry. Backfilling the whole thread here could flood the inbox with old
	// history the user never had and wake the session for ancient messages.
	if maxTS == "" {
		return 0, nil
	}
	msgs, err := b.client.Replies(ctx, channel, threadTS, maxTS, b.limit)
	if err != nil {
		return 0, err
	}
	appended := 0
	for _, m := range msgs {
		ts := strings.TrimSpace(m.TS)
		if ts == "" || ts == threadTS {
			continue // skip the thread root Slack always returns first
		}
		if seen[ts] || !slackTSLess(maxTS, ts) {
			continue // already recorded, or not newer than our cursor
		}
		if !backfillAcceptMessage(m) {
			continue
		}
		ev := InboundEvent{
			Kind:        "message",
			Channel:     channel,
			ChannelType: "channel",
			TS:          ts,
			ThreadTS:    threadTS,
			UserID:      strings.TrimSpace(m.User),
			Text:        strings.TrimSpace(m.Text),
		}
		if err := AppendInboxEvent(slug, ev); err != nil {
			return appended, err
		}
		seen[ts] = true
		appended++
	}
	return appended, nil
}

// reconcileDM appends any DM-channel messages newer than what's already in the
// task's inbox.jsonl. DMs can be flat OR threaded, so it pulls BOTH
// conversations.history (top-level) AND conversations.replies for every thread
// root already seen in this DM — history alone is blind to thread replies, and
// these DMs ("also send as DM" + threaded replies) live entirely under a thread.
// Matched by channel alone; self-heals across restarts and never double-appends
// (deduped by ts, with the (channel, ts) guard in AppendInboxEvent as backstop).
func (b *SlackBackfill) reconcileDM(ctx context.Context, slug, channel string) (int, error) {
	if b.dmClient == nil {
		return 0, nil
	}
	channel = normalizeSlackChannelID(channel)
	maxTS, seen, err := inboxSlackTSIndexForChannel(slug, channel)
	if err != nil {
		return 0, err
	}
	// No baseline in this DM yet → let the live listener establish the first
	// entry rather than flooding the inbox with old DM history.
	if maxTS == "" {
		return 0, nil
	}

	// Top-level messages.
	candidates, err := b.dmClient.History(ctx, channel, maxTS, b.limit)
	if err != nil {
		return 0, err
	}
	// Thread replies for every thread root this DM is known to use. Errors on a
	// single thread are logged but don't abort the others.
	for _, root := range inboxThreadRootsForChannel(slug, channel) {
		replies, err := b.dmClient.Replies(ctx, channel, root, maxTS, b.limit)
		if err != nil {
			b.logFn("slack dm backfill %s (%s thread %s): %v", slug, channel, root, err)
			continue
		}
		candidates = append(candidates, replies...)
	}

	chanType := "mpim"
	if strings.HasPrefix(channel, "D") {
		chanType = "im"
	}
	appended := 0
	for _, m := range candidates {
		ts := strings.TrimSpace(m.TS)
		if ts == "" {
			continue
		}
		if seen[ts] || !slackTSLess(maxTS, ts) {
			continue // already recorded, or not newer than our cursor
		}
		if !backfillAcceptMessage(m) {
			continue
		}
		threadTS := strings.TrimSpace(m.ThreadTS)
		if threadTS == "" {
			threadTS = ts // unthreaded DM message is its own root
		}
		ev := InboundEvent{
			Kind:        "message",
			Channel:     channel,
			ChannelType: chanType,
			TS:          ts,
			ThreadTS:    threadTS,
			UserID:      strings.TrimSpace(m.User),
			Text:        strings.TrimSpace(m.Text),
		}
		if err := AppendInboxEvent(slug, ev); err != nil {
			return appended, err
		}
		seen[ts] = true
		appended++
	}
	return appended, nil
}

// inboxSlackTSIndexForChannel reads a task's inbox.jsonl once and returns the
// newest Slack message ts in the given channel (the resume cursor) plus the set
// of that channel's message ts, for dedup. Scoping by channel keeps each
// monitored conversation's cursor independent — see reconcile's note.
func inboxSlackTSIndexForChannel(slug, channel string) (maxTS string, seen map[string]bool, err error) {
	entries, err := ReadInboxEntries(slug)
	if err != nil {
		return "", nil, err
	}
	want := normalizeSlackChannelID(channel)
	seen = make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.Event.Kind != "message" && e.Event.Kind != "app_mention" {
			continue
		}
		if normalizeSlackChannelID(e.Event.Channel) != want {
			continue
		}
		ts := strings.TrimSpace(e.Event.TS)
		if ts == "" {
			continue
		}
		seen[ts] = true
		if maxTS == "" || slackTSLess(maxTS, ts) {
			maxTS = ts
		}
	}
	return maxTS, seen, nil
}

// slackTSLess reports whether Slack ts a is older than b. Slack ts are
// "seconds.microseconds" strings; compare numerically, falling back to lexical
// order when either fails to parse.
func slackTSLess(a, b string) bool {
	fa, ea := strconv.ParseFloat(a, 64)
	fb, eb := strconv.ParseFloat(b, 64)
	if ea != nil || eb != nil {
		return a < b
	}
	return fa < fb
}

// backfillAcceptMessage keeps real human/bot/broadcast replies and drops
// system + edit/delete subtypes (joins, leaves, message_changed, …). It also
// accepts thread_broadcast — which the live parser drops — so a broadcast
// reply still reaches the inbox via the durable path.
func backfillAcceptMessage(m SlackMessage) bool {
	switch strings.TrimSpace(m.SubType) {
	case "", "bot_message", "thread_broadcast":
		return strings.TrimSpace(m.Text) != "" || strings.TrimSpace(m.User) != ""
	default:
		return false
	}
}
