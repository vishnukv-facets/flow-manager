package monitor

import "strings"

// agentAckMarker is the hidden HTML-comment marker an autonomous agent appends
// to every acknowledgement it posts on a GitHub PR/issue (see SKILL.md §10c).
// It renders invisibly on GitHub but lets the monitor recognize an ack as the
// agent's own output and drop it, so an ack never re-wakes the session that
// posted it. The skill text and this constant must stay in sync.
const agentAckMarker = "<!-- flow-agent-ack -->"

// isSelfAckComment reports whether a top-level comment is one of the operator's
// own autonomous-agent acknowledgements — authored by a self login AND carrying
// the marker. Such comments are dropped from the poll so they don't echo back
// and re-wake the session. A self-authored comment WITHOUT the marker is a real
// instruction and still flows through; an external comment that merely quotes
// the marker is not self-authored and also flows through.
func isSelfAckComment(author, body string, selfLogins []string) bool {
	if !strings.Contains(body, agentAckMarker) {
		return false
	}
	a := strings.ToLower(strings.TrimSpace(author))
	if a == "" {
		return false
	}
	for _, l := range selfLogins {
		if a == strings.ToLower(strings.TrimSpace(l)) {
			return true
		}
	}
	return false
}
