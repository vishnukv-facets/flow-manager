package server

import (
	"context"
	"strings"
	"testing"

	"flow/internal/flowdb"

	_ "modernc.org/sqlite"
)

// TestOpenOrContinueChatRejectsEmptyChannel verifies channel validation happens
// up front: a blank channel returns an error rather than attempting a launch.
// There is no longer a server-side ack — the agent acknowledges itself as its
// first action (see slackReplyInstructions and TestSlackReplyInstructions).
func TestOpenOrContinueChatRejectsEmptyChannel(t *testing.T) {
	db, err := flowdb.OpenDB(t.TempDir() + "/flow.db")
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	s := &Server{cfg: Config{DB: db}}
	s.terminals = newTerminalHub(s)

	if err := s.OpenOrContinueChat(context.Background(), "   ", "hi"); err == nil {
		t.Fatal("expected empty-channel error, got nil")
	}
}

// TestSlackChatSlug covers the deterministic slug builder + sanitizer. Every
// produced slug must pass validateSlug (the repo-wide slug grammar) so the
// floating-terminal hub and chats table accept it.
func TestSlackChatSlug(t *testing.T) {
	tests := []struct {
		name    string
		channel string
		want    string
	}{
		{"typical IM id", "D0123ABCDEF", "chat-slack-d0123abcdef"},
		{"already lowercase", "dabc123", "chat-slack-dabc123"},
		{"non-alnum collapses to dash", "D-01.23:45", "chat-slack-d-01-23-45"},
		{"runs of separators collapse", "D__01--23", "chat-slack-d-01-23"},
		{"leading/trailing junk trimmed", "  *D9*  ", "chat-slack-d9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slackChatSlug(tt.channel)
			if got != tt.want {
				t.Fatalf("slackChatSlug(%q) = %q, want %q", tt.channel, got, tt.want)
			}
			if err := validateSlug(got); err != nil {
				t.Fatalf("slackChatSlug(%q) -> %q failed validateSlug: %v", tt.channel, got, err)
			}
		})
	}

	// Determinism: same channel must always map to the same slug (one chat reused
	// across messages).
	if slackChatSlug("D0123ABCDEF") != slackChatSlug("d0123abcdef") {
		t.Fatal("slackChatSlug must be case-insensitively deterministic for one channel")
	}
}

func TestSlackCommandProvider(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want string
	}{
		{"default is claude", "", "claude"},
		{"explicit claude", "claude", "claude"},
		{"codex", "codex", "codex"},
		{"codex-cli alias", "codex-cli", "codex"},
		{"unrecognized falls back to claude", "gpt-9", "claude"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("FLOW_SLACK_COMMAND_PROVIDER", tt.env)
			if got := slackCommandProvider(); got != tt.want {
				t.Fatalf("slackCommandProvider() with %q = %q, want %q", tt.env, got, tt.want)
			}
		})
	}
}

// TestSlackReplyInstructions verifies the reply block interpolates the real
// channel and carries the long-lived-chat guidance (no flow done).
func TestSlackReplyInstructions(t *testing.T) {
	got := slackReplyInstructions("D0123ABCDEF")
	for _, want := range []string{
		"flow slack send --channel D0123ABCDEF",
		"Do not call flow done",
		"AFK",
		"Acknowledge FIRST",
		"first action",
		"act on it directly",
		"flow do <task> --auto",
		"flow tell <slug>",
		"flow run playbook <slug> --auto",
		"Ask before risky",
		"WAIT for their",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("slackReplyInstructions missing %q in:\n%s", want, got)
		}
	}
}
