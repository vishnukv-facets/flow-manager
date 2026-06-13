package monitor

import (
	"context"
	"errors"
	"testing"
)

// fbHistory records calls and returns a scripted error/result.
type fbHistory struct {
	called int
	err    error
	msgs   []SlackMessage
}

func (f *fbHistory) History(_ context.Context, _, _ string, _ int) ([]SlackMessage, error) {
	f.called++
	return f.msgs, f.err
}

type fbReplies struct {
	called int
	err    error
	msgs   []SlackMessage
}

func (f *fbReplies) Replies(_ context.Context, _, _, _ string, _ int) ([]SlackMessage, error) {
	f.called++
	return f.msgs, f.err
}

func TestFallbackHistory(t *testing.T) {
	ctx := context.Background()
	want := []SlackMessage{{TS: "1.0", Text: "hi"}}

	t.Run("falls back to user token when bot is not in channel", func(t *testing.T) {
		bot := &fbHistory{err: errors.New("not_in_channel")}
		user := &fbHistory{msgs: want}
		got, err := fallbackHistory{primary: bot, secondary: user}.History(ctx, "C1", "", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].TS != "1.0" {
			t.Fatalf("expected user-token result, got %+v", got)
		}
		if user.called != 1 {
			t.Fatalf("expected user client to be called once, got %d", user.called)
		}
	})

	t.Run("channel_not_found and missing_scope also trigger fallback", func(t *testing.T) {
		for _, marker := range []string{"channel_not_found", "missing_scope"} {
			bot := &fbHistory{err: errors.New(marker)}
			user := &fbHistory{msgs: want}
			if _, err := (fallbackHistory{primary: bot, secondary: user}).History(ctx, "C1", "", 10); err != nil {
				t.Fatalf("%s: unexpected error: %v", marker, err)
			}
			if user.called != 1 {
				t.Fatalf("%s: expected fallback to user client", marker)
			}
		}
	})

	t.Run("does NOT fall back on unrelated errors", func(t *testing.T) {
		bot := &fbHistory{err: errors.New("ratelimited")}
		user := &fbHistory{}
		if _, err := (fallbackHistory{primary: bot, secondary: user}).History(ctx, "C1", "", 10); err == nil {
			t.Fatal("expected the bot error to propagate")
		}
		if user.called != 0 {
			t.Fatalf("expected NO fallback on unrelated error, user called %d", user.called)
		}
	})

	t.Run("success on primary never touches secondary", func(t *testing.T) {
		bot := &fbHistory{msgs: want}
		user := &fbHistory{}
		if _, err := (fallbackHistory{primary: bot, secondary: user}).History(ctx, "C1", "", 10); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.called != 0 {
			t.Fatalf("expected secondary untouched, user called %d", user.called)
		}
	})

	t.Run("nil secondary behaves like the bare primary", func(t *testing.T) {
		bot := &fbHistory{err: errors.New("not_in_channel")}
		if _, err := (fallbackHistory{primary: bot, secondary: nil}).History(ctx, "C1", "", 10); err == nil {
			t.Fatal("expected primary error to propagate with nil secondary")
		}
	})
}

func TestFallbackReplies(t *testing.T) {
	ctx := context.Background()
	want := []SlackMessage{{TS: "2.0", Text: "reply"}}

	t.Run("falls back to user token for inaccessible channel threads", func(t *testing.T) {
		bot := &fbReplies{err: errors.New("not_in_channel")}
		user := &fbReplies{msgs: want}
		got, err := fallbackReplies{primary: bot, secondary: user}.Replies(ctx, "C1", "1.0", "", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].TS != "2.0" {
			t.Fatalf("expected user-token replies, got %+v", got)
		}
		if user.called != 1 {
			t.Fatalf("expected user client called once, got %d", user.called)
		}
	})

	t.Run("does NOT fall back on unrelated errors", func(t *testing.T) {
		bot := &fbReplies{err: errors.New("thread_not_found")}
		user := &fbReplies{}
		if _, err := (fallbackReplies{primary: bot, secondary: user}).Replies(ctx, "C1", "1.0", "", 10); err == nil {
			t.Fatal("expected the bot error to propagate")
		}
		if user.called != 0 {
			t.Fatalf("expected no fallback, user called %d", user.called)
		}
	})
}
