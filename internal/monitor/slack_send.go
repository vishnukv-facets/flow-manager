package monitor

import (
	"fmt"
	"strings"

	"github.com/slack-go/slack"
)

// resolveSendIdentity picks the token + as_user flag for an outbound message to
// `channel`, honoring FLOW_SLACK_SEND_AS. The operator↔bot command IM ALWAYS
// uses the bot identity regardless of the setting: posting flow's own replies
// there as the operator would loop (the reply re-enters as a command) and
// defeat self-echo detection (which keys on the bot's user id). Other
// conversations honor the setting — "user" posts as the operator (user token,
// as_user=true) when a user token exists; otherwise it falls back to the bot.
func resolveSendIdentity(channel string) (token string, asUser bool) {
	if SlackSendIdentity() == "user" && !botIsMemberOfIM(channel) {
		if ut := strings.TrimSpace(SlackUserToken()); ut != "" {
			return ut, true
		}
	}
	return SlackBotToken(), false
}

// sendAsBotFn performs the actual post; a package var so tests don't hit Slack.
var sendAsBotFn = func(channel, text string) error {
	token, asUser := resolveSendIdentity(channel)
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("no slack token configured (FLOW_SLACK_TOKEN / user token)")
	}
	api := slack.New(token)
	_, _, err := api.PostMessage(channel, slack.MsgOptionText(text, false), slack.MsgOptionAsUser(asUser))
	return err
}

// SendAsBot posts text to a Slack channel/DM under flow's configured send
// identity (FLOW_SLACK_SEND_AS — the flow bot by default, or the operator's user
// identity for replies into channels/colleague threads). The operator↔bot
// command DM is always posted as the bot regardless of the setting. Gated by
// FLOW_SLACK_WRITES_ENABLED.
func SendAsBot(channel, text string) error {
	if !slackWritesEnabled() {
		return fmt.Errorf("slack writes disabled (set FLOW_SLACK_WRITES_ENABLED=1)")
	}
	if strings.TrimSpace(channel) == "" {
		return fmt.Errorf("channel is required")
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("text is required")
	}
	return sendAsBotFn(channel, text)
}
