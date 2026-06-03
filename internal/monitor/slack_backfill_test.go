package monitor

import (
	"context"
	"testing"
	"time"
)

type fakeReplies struct {
	msgs      []SlackMessage
	gotOldest string
	calls     int
}

func (f *fakeReplies) Replies(_ context.Context, _, _, oldest string, _ int) ([]SlackMessage, error) {
	f.calls++
	f.gotOldest = oldest
	return f.msgs, nil
}

// fakeHistory stubs the user-token DM reader: conversations.history (top-level)
// plus conversations.replies (thread replies), since DMs can be threaded.
type fakeHistory struct {
	msgs           []SlackMessage            // history (top-level) results
	repliesByRoot  map[string][]SlackMessage // replies keyed by thread root ts
	gotOldest      string
	gotChannel     string
	calls          int
	replyCalls     int
	gotThreadRoots map[string]bool
}

func (f *fakeHistory) History(_ context.Context, channel, oldest string, _ int) ([]SlackMessage, error) {
	f.calls++
	f.gotOldest = oldest
	f.gotChannel = channel
	return f.msgs, nil
}

// repliesByRoot lets a fakeHistory also answer conversations.replies, keyed by
// thread root ts. DMs here are threaded, so the recoverable messages live under
// replies, not history.
func (f *fakeHistory) Replies(_ context.Context, channel, threadTS, oldest string, _ int) ([]SlackMessage, error) {
	f.replyCalls++
	if f.gotThreadRoots == nil {
		f.gotThreadRoots = map[string]bool{}
	}
	f.gotThreadRoots[threadTS] = true
	return f.repliesByRoot[threadTS], nil
}

// seedInbox writes a Slack message entry so the backfill has a baseline ts.
func seedInbox(t *testing.T, slug, channel, threadTS, ts, text string) {
	t.Helper()
	if err := AppendInboxEvent(slug, InboundEvent{
		Kind: "message", Channel: channel, ChannelType: "channel",
		TS: ts, ThreadTS: threadTS, Text: text,
	}); err != nil {
		t.Fatalf("seed inbox: %v", err)
	}
}

func TestSlackBackfillReconcile_AppendsOnlyNewerDeduped(t *testing.T) {
	t.Setenv("FLOW_ROOT", t.TempDir())
	const slug, channel, root = "slack-c1-50-000000", "C1", "50.000000"
	seedInbox(t, slug, channel, root, "100.000000", "first reply")

	fake := &fakeReplies{msgs: []SlackMessage{
		{TS: root, User: "U1", Text: "thread root"},               // skipped: == threadTS
		{TS: "100.000000", User: "U1", Text: "first reply"},       // skipped: already seen
		{TS: "120.000000", User: "U2", Text: "newer reply A"},     // appended
		{TS: "150.000000", User: "U3", Text: "newer reply B"},     // appended
		{TS: "160.000000", User: "U4", Text: "edit", SubType: "message_changed"}, // skipped: subtype
		{TS: "170.000000", User: "", Text: ""},                    // skipped: empty
		{TS: "180.000000", User: "U5", Text: "broadcast", SubType: "thread_broadcast"}, // appended
	}}

	bf := &SlackBackfill{client: fake, limit: 200}
	n, err := bf.reconcile(context.Background(), slug, channel, root)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n != 3 {
		t.Fatalf("appended = %d, want 3 (120, 150, 180)", n)
	}
	if fake.gotOldest != "100.000000" {
		t.Fatalf("oldest passed = %q, want the inbox max ts 100.000000", fake.gotOldest)
	}

	// inbox.jsonl now holds the original + 3 recovered, no dupes.
	entries, err := ReadInboxEntries(slug)
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	got := map[string]int{}
	for _, e := range entries {
		got[e.Event.TS]++
	}
	for _, ts := range []string{"100.000000", "120.000000", "150.000000", "180.000000"} {
		if got[ts] != 1 {
			t.Errorf("ts %s appears %d times, want exactly 1", ts, got[ts])
		}
	}
	for _, ts := range []string{"50.000000", "160.000000", "170.000000"} {
		if got[ts] != 0 {
			t.Errorf("ts %s should not be in inbox, found %d", ts, got[ts])
		}
	}

	// Idempotent: a second pass over the same replies appends nothing.
	n2, err := bf.reconcile(context.Background(), slug, channel, root)
	if err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("second pass appended = %d, want 0 (dedup)", n2)
	}
}

func TestSlackDMBackfill_ReconcileAppendsNewerDeduped(t *testing.T) {
	t.Setenv("FLOW_ROOT", t.TempDir())
	const slug, dm = "slack-dm-task", "D_ALICE"
	// DMs aren't threaded — each message is its own root, so threadTS == ts.
	seedInbox(t, slug, dm, "100.000000", "100.000000", "first dm")

	fake := &fakeHistory{msgs: []SlackMessage{
		{TS: "100.000000", User: "U1", Text: "first dm"},   // skipped: already seen
		{TS: "120.000000", User: "U2", Text: "newer dm A"}, // appended
		{TS: "150.000000", User: "U3", Text: "newer dm B"}, // appended
		{TS: "160.000000", User: "U4", Text: "edit", SubType: "message_changed"}, // skipped subtype
		{TS: "90.000000", User: "U5", Text: "older"},       // skipped: older than cursor
	}}
	bf := &SlackBackfill{limit: 200}
	bf.SetDMHistoryClient(fake)

	n, err := bf.reconcileDM(context.Background(), slug, dm)
	if err != nil {
		t.Fatalf("reconcileDM: %v", err)
	}
	if n != 2 {
		t.Fatalf("appended = %d, want 2 (120, 150)", n)
	}
	if fake.gotOldest != "100.000000" {
		t.Fatalf("oldest passed = %q, want the DM's inbox max ts 100.000000", fake.gotOldest)
	}

	// Idempotent: second pass appends nothing.
	n2, err := bf.reconcileDM(context.Background(), slug, dm)
	if err != nil {
		t.Fatalf("reconcileDM 2: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("second pass appended = %d, want 0 (dedup)", n2)
	}
}

func TestSlackDMBackfill_PerChannelCursorIgnoresOtherChannels(t *testing.T) {
	// A task that monitors a thread AND a DM. The thread has a message newer
	// than the DM's last-seen. The DM cursor must be the DM's OWN max, not the
	// global max — otherwise a DM reply that arrived during a gap (older than
	// the latest thread message) would be wrongly skipped.
	t.Setenv("FLOW_ROOT", t.TempDir())
	const slug = "slack-mixed-task"
	seedInbox(t, slug, "C_THREAD", "10.000000", "200.000000", "latest thread msg")
	seedInbox(t, slug, "D_ALICE", "150.000000", "150.000000", "dm baseline")

	fake := &fakeHistory{msgs: []SlackMessage{
		{TS: "160.000000", User: "U2", Text: "dm reply missed during the gap"},
	}}
	bf := &SlackBackfill{limit: 200}
	bf.SetDMHistoryClient(fake)

	n, err := bf.reconcileDM(context.Background(), slug, "D_ALICE")
	if err != nil {
		t.Fatalf("reconcileDM: %v", err)
	}
	if fake.gotOldest != "150.000000" {
		t.Fatalf("DM cursor = %q, want 150.000000 (per-channel, not the global 200.000000)", fake.gotOldest)
	}
	if n != 1 {
		t.Fatalf("appended = %d, want 1 (the 160 reply)", n)
	}
}

func TestSlackDMBackfill_RecoversThreadReplies(t *testing.T) {
	// DMs can be threaded ("also send as DM" + threaded replies). A reply
	// missed during a socket gap is NOT in conversations.history (which returns
	// only top-level messages) — it's only reachable via conversations.replies
	// on the thread root. reconcileDM must consult replies for the thread roots
	// it already knows about from the inbox, or it can never recover DM replies.
	t.Setenv("FLOW_ROOT", t.TempDir())
	const slug, dm, root = "slack-dm-threaded", "D_ALICE", "1780480392.819809"
	// Baseline is itself a thread reply under `root`.
	if err := AppendInboxEvent(slug, InboundEvent{
		Kind: "message", Channel: dm, ChannelType: "im",
		TS: "1780489629.079919", ThreadTS: root, UserID: "U_me", Text: "stepping out",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	fake := &fakeHistory{
		msgs: nil, // history sees no new top-level messages
		repliesByRoot: map[string][]SlackMessage{
			root: {
				{TS: "1780489629.079919", ThreadTS: root, User: "U_me", Text: "stepping out"}, // already seen
				{TS: "1780491705.662279", ThreadTS: root, User: "U_ishaan", Text: "why is there a new file?"}, // missed reply
			},
		},
	}
	bf := &SlackBackfill{limit: 200}
	bf.SetDMHistoryClient(fake)

	n, err := bf.reconcileDM(context.Background(), slug, dm)
	if err != nil {
		t.Fatalf("reconcileDM: %v", err)
	}
	if n != 1 {
		t.Fatalf("appended = %d, want 1 (the missed thread reply)", n)
	}
	if !fake.gotThreadRoots[root] {
		t.Fatalf("reconcileDM must fetch replies for known thread root %s; roots queried = %v", root, fake.gotThreadRoots)
	}
	entries, _ := ReadInboxEntries(slug)
	found := false
	for _, e := range entries {
		if e.Event.TS == "1780491705.662279" {
			found = true
		}
	}
	if !found {
		t.Errorf("missed DM thread reply not recovered into inbox; entries=%v", entries)
	}
}

func TestSlackDMBackfill_NoBaselineSkips(t *testing.T) {
	t.Setenv("FLOW_ROOT", t.TempDir())
	fake := &fakeHistory{msgs: []SlackMessage{{TS: "20.000000", User: "U1", Text: "dm"}}}
	bf := &SlackBackfill{limit: 200}
	bf.SetDMHistoryClient(fake)
	n, err := bf.reconcileDM(context.Background(), "no-baseline-dm", "D_X")
	if err != nil {
		t.Fatalf("reconcileDM: %v", err)
	}
	if n != 0 || fake.calls != 0 {
		t.Fatalf("no baseline → want 0 appended / 0 Slack calls; got n=%d calls=%d", n, fake.calls)
	}
}

func TestSlackDMBackfill_RunOnceReconcilesRegisteredDMs(t *testing.T) {
	// End-to-end through runOnce: a slack-reply task carrying a slack-dm tag
	// has its DM reconciled against conversations.history.
	db := dispatcherTestDB(t)
	seedSlackDMTask(t, db, "dm-task", "D_ALICE")
	seedInbox(t, "dm-task", "D_ALICE", "100.000000", "100.000000", "dm baseline")

	fake := &fakeHistory{msgs: []SlackMessage{
		{TS: "120.000000", User: "U2", Text: "missed dm reply"},
	}}
	bf := NewSlackBackfill(db, &fakeReplies{}, time.Hour)
	bf.SetDMHistoryClient(fake)
	bf.runOnce(context.Background())

	entries, err := ReadInboxEntries("dm-task")
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Event.TS == "120.000000" && e.Event.Text == "missed dm reply" {
			found = true
		}
	}
	if !found {
		t.Fatalf("runOnce did not recover the missed DM reply; entries = %+v", entries)
	}
}

func TestDMChannelsFromTags(t *testing.T) {
	got := dmChannelsFromTags([]string{
		"slack-reply",
		"slack-thread:c123:1.01",
		"slack-dm:d_alice",
		"slack-dm:d_bob",
		"priority-high",
	})
	if len(got) != 2 {
		t.Fatalf("got %d dm channels, want 2: %v", len(got), got)
	}
	set := map[string]bool{got[0]: true, got[1]: true}
	if !set["d_alice"] || !set["d_bob"] {
		t.Errorf("expected d_alice and d_bob; got %v", got)
	}
}

func TestSlackBackfillReconcile_NoBaselineSkips(t *testing.T) {
	t.Setenv("FLOW_ROOT", t.TempDir())
	const slug, channel, root = "slack-c2-10-000000", "C2", "10.000000"
	// No inbox.jsonl at all → no baseline → backfill must not flood history,
	// and must not even call Slack.
	fake := &fakeReplies{msgs: []SlackMessage{{TS: "20.000000", User: "U1", Text: "reply"}}}
	bf := &SlackBackfill{client: fake, limit: 200}
	n, err := bf.reconcile(context.Background(), slug, channel, root)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n != 0 {
		t.Fatalf("appended = %d, want 0 with no baseline", n)
	}
	if fake.calls != 0 {
		t.Fatalf("Slack called %d times, want 0 with no baseline", fake.calls)
	}
}
