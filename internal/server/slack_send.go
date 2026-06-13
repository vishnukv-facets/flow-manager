package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"flow/internal/monitor"
)

type slackSendRequest struct {
	Channel string `json:"channel"`
	Text    string `json:"text"`
}

// handleSlackSend posts a Slack message as the flow bot using the SERVER's
// in-process token. The CLI (`flow slack send`) routes here so the message is
// sent with the freshly-validated token the running server holds in its
// environment, rather than a stale token captured by a tmux-spawned agent.
func (s *Server) handleSlackSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req slackSendRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid slack send payload", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Channel) == "" {
		http.Error(w, "channel is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}
	if err := monitor.SendAsBot(req.Channel, req.Text); err != nil {
		// 502: we reached the server but Slack (or the writes gate) rejected
		// the send. The CLI surfaces this and must NOT fall back to its own
		// (potentially stale) token.
		writeError(w, err, http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
