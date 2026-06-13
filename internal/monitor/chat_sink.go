package monitor

import "context"

// ChatCommandSink opens or continues a chat session for an authorized operator
// command arriving on the Slack command channel. Implemented by the server
// (*server.Server). It lives in this package — not server — so Dispatcher can
// hold one without an import cycle (server already imports monitor; monitor must
// NOT import server). This is the same direction as MessageObserver/TaskOpener.
type ChatCommandSink interface {
	// OpenOrContinueChat routes an operator's Slack DM into a durable chat agent
	// session keyed by the IM channel. The first call for a channel opens the
	// session (with text as the launch prompt); later calls deliver text into the
	// reused (or resumed) session.
	OpenOrContinueChat(ctx context.Context, channel, text string) error
}
