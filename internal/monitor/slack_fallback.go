package monitor

import (
	"context"
	"strings"
)

// Channel monitoring rides the operator's membership, not the bot's: the bot is
// not necessarily a member of every watched channel (it may have been removed,
// or never invited). When the bot token can't read a channel, the operator's
// user token can — the operator is a member of the channels they care to watch,
// and the user token carries channels:history / groups:history. These fallback
// wrappers try the bot token first (its prior behavior, and the only token that
// can read channels the operator left) and retry with the user token when Slack
// reports the channel is inaccessible to the bot. This is the backfill twin of
// the user-token message.channels/groups event subscription that covers the
// live path.

// slackChannelInaccessible reports whether err is Slack telling us the calling
// token can't see this channel — the signal to retry with the other token.
// Mirrors the steerer's backfillInaccessibleError marker set.
func slackChannelInaccessible(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{"not_in_channel", "channel_not_found", "missing_scope"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// fallbackHistory is a SlackHistory that tries primary, then secondary when
// primary reports the channel inaccessible. A nil secondary makes it behave
// exactly like primary (so callers without a user token are unaffected).
type fallbackHistory struct{ primary, secondary SlackHistory }

func (f fallbackHistory) History(ctx context.Context, channelID, oldest string, limit int) ([]SlackMessage, error) {
	msgs, err := f.primary.History(ctx, channelID, oldest, limit)
	if err != nil && f.secondary != nil && slackChannelInaccessible(err) {
		return f.secondary.History(ctx, channelID, oldest, limit)
	}
	return msgs, err
}

// NewSlackChannelHistoryClient returns the bot-token channel history client,
// transparently backed by the user token for channels the bot can't read.
// Returns nil only when no bot token is configured (no channel sweep at all);
// returns the bare bot client when no user token exists (prior behavior).
func NewSlackChannelHistoryClient() SlackHistory {
	bot := NewSlackHistoryClient()
	if bot == nil {
		return nil
	}
	user := NewSlackUserHistoryClient()
	if user == nil {
		return bot
	}
	return fallbackHistory{primary: bot, secondary: user}
}

// fallbackReplies is the SlackThreadReplies twin of fallbackHistory: bot first,
// user token when the channel is inaccessible to the bot.
type fallbackReplies struct{ primary, secondary SlackThreadReplies }

func (f fallbackReplies) Replies(ctx context.Context, channelID, threadTS, oldest string, limit int) ([]SlackMessage, error) {
	msgs, err := f.primary.Replies(ctx, channelID, threadTS, oldest, limit)
	if err != nil && f.secondary != nil && slackChannelInaccessible(err) {
		return f.secondary.Replies(ctx, channelID, threadTS, oldest, limit)
	}
	return msgs, err
}

// NewSlackChannelRepliesClient returns the bot-token channel replies client,
// backed by the user token for channel threads the bot can't read. Nil only
// when no bot token is configured; the bare bot client when no user token.
func NewSlackChannelRepliesClient() SlackThreadReplies {
	bot := NewSlackRepliesClient()
	if bot == nil {
		return nil
	}
	user := NewSlackUserRepliesClient()
	if user == nil {
		return bot
	}
	return fallbackReplies{primary: bot, secondary: user}
}
