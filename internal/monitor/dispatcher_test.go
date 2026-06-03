package monitor

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"

	"flow/internal/flowdb"

	_ "modernc.org/sqlite"
)

// spawnCall and tagCall record what the dispatcher requested. Used by
// tests to assert on the orchestration without actually running flow CLI
// subprocesses.
type spawnCall struct {
	Name     string
	Slug     string
	Brief    string
	Provider string
	Project  string
}

type tagCall struct {
	Slug string
	Tag  string
}

// stubDispatcherIO swaps the package-level spawn/tag/open hooks for fakes
// and returns a teardown to restore the originals. Tests use the returned
// trackers to assert call patterns. Concurrency-safe so dispatch ordering
// doesn't matter for assertions.
func stubDispatcherIO(t *testing.T) (*[]spawnCall, *[]tagCall, *[]string, func()) {
	t.Helper()
	mu := &sync.Mutex{}
	var spawns []spawnCall
	var tags []tagCall
	var opens []string

	origSpawn := spawnFlowTask
	origTag := tagFlowTask
	origOpen := openSlackReplyTask

	spawnFlowTask = func(_ context.Context, name, slug, brief, provider, project string) error {
		mu.Lock()
		defer mu.Unlock()
		spawns = append(spawns, spawnCall{Name: name, Slug: slug, Brief: brief, Provider: provider, Project: project})
		return nil
	}
	tagFlowTask = func(_ context.Context, slug, tag string) error {
		mu.Lock()
		defer mu.Unlock()
		tags = append(tags, tagCall{Slug: slug, Tag: tag})
		return nil
	}
	openSlackReplyTask = func(slug string) error {
		mu.Lock()
		defer mu.Unlock()
		opens = append(opens, slug)
		return nil
	}
	return &spawns, &tags, &opens, func() {
		spawnFlowTask = origSpawn
		tagFlowTask = origTag
		openSlackReplyTask = origOpen
	}
}

// dispatcherTestDB opens a real on-disk SQLite using flowdb.OpenDB so all
// migrations run, and returns the DB. Cleanup closes it. The temp dir is
// also wired as FLOW_ROOT so any inbox file ops in dispatch land inside
// it instead of the user's real ~/.flow.
func dispatcherTestDB(t *testing.T) *sql.DB {
	t.Helper()
	root := t.TempDir()
	t.Setenv("FLOW_ROOT", root)
	t.Setenv("HOME", root)
	db, err := flowdb.OpenDB(root + "/flow.db")
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seedSlackTask inserts a task with the given slug and tags it with the
// slack-thread linkage so dispatcher lookups can find it. There's no
// public flowdb.AddTask helper today (CLI handles the INSERT), so we
// bypass with a direct INSERT — keeps the test focused on dispatch logic
// rather than CLI plumbing.
func seedSlackTask(t *testing.T, db *sql.DB, slug, threadKey string) {
	t.Helper()
	// status='backlog' satisfies the tasks invariant that non-backlog rows
	// must carry a session_id (or be a codex in-progress task). Seeded rows
	// don't have one yet; findTaskByThreadKey treats backlog as a valid
	// lookup result so this is enough.
	now := flowdb.NowISO()
	_, err := db.Exec(
		`INSERT INTO tasks (slug, name, status, priority, work_dir, permission_mode, session_provider, status_changed_at, created_at, updated_at)
		 VALUES (?, ?, 'backlog', 'high', ?, 'default', 'claude', ?, ?, ?)`,
		slug, "seeded slack task", t.TempDir(), now, now, now,
	)
	if err != nil {
		t.Fatalf("seed task %s: %v", slug, err)
	}
	if err := flowdb.AddTaskTag(db, slug, "slack-reply"); err != nil {
		t.Fatalf("tag slack-reply: %v", err)
	}
	if err := flowdb.AddTaskTag(db, slug, SlackThreadTagPrefix+threadKey); err != nil {
		t.Fatalf("tag thread: %v", err)
	}
}

func TestDispatcher_NewThreadReactionSpawnsAndAppends(t *testing.T) {
	t.Setenv("FLOW_SLACK_SELF_USER_IDS", "U_me")
	t.Setenv("FLOW_SLACK_TRIGGER_EMOJI", "claude")
	t.Setenv("FLOW_SLACK_AUTOOPEN", "0") // tests assert on opens separately
	db := dispatcherTestDB(t)
	spawns, tags, opens, restore := stubDispatcherIO(t)
	defer restore()

	d := NewDispatcher(db, nil)
	reaction := mustParseReaction(t, "U_me", "claude", "C123", "1234.0010", "1234.0001")
	if err := d.Dispatch(context.Background(), reaction); err != nil {
		t.Fatalf("Dispatch err = %v", err)
	}

	if len(*spawns) != 1 {
		t.Fatalf("spawn count = %d, want 1", len(*spawns))
	}
	if !strings.HasPrefix((*spawns)[0].Slug, "slack-c123-1234") {
		t.Errorf("derived slug looks wrong: %q", (*spawns)[0].Slug)
	}
	if !strings.Contains((*spawns)[0].Brief, "thread_ts=1234.0001") {
		t.Errorf("brief missing thread_ts hint: %s", (*spawns)[0].Brief)
	}

	// Must apply both the marker tag and the thread-linkage tag.
	gotTags := map[string]bool{}
	for _, c := range *tags {
		gotTags[c.Tag] = true
	}
	if !gotTags["slack-reply"] || !gotTags["slack-thread:C123:1234.0001"] {
		t.Errorf("tags missing expected entries: %v", gotTags)
	}

	if len(*opens) != 0 {
		t.Errorf("AUTOOPEN=0 should suppress opens; got %v", *opens)
	}
}

func TestDispatcher_BriefIncludesProjectPicker(t *testing.T) {
	t.Setenv("FLOW_SLACK_SELF_USER_IDS", "U_me")
	t.Setenv("FLOW_SLACK_TRIGGER_EMOJI", "claude")
	t.Setenv("FLOW_SLACK_AUTOOPEN", "0")
	db := dispatcherTestDB(t)
	spawns, _, _, restore := stubDispatcherIO(t)
	defer restore()

	// Stub the project lookup so the test doesn't depend on flowdb having
	// any projects inserted via the CLI. Two active projects + the
	// existence of the picker section is what the agent's first turn
	// keys off of.
	origProjects := listProjectChoices
	listProjectChoices = func(_ *sql.DB) ([]projectChoice, error) {
		return []projectChoice{
			{Slug: "budgeting", Name: "Budgeting app", UpdatedAt: "2026-05-21T00:00:00Z", Priority: "high"},
			{Slug: "devops", Name: "DevOps", UpdatedAt: "2026-05-20T12:00:00Z", Priority: "medium"},
		}, nil
	}
	defer func() { listProjectChoices = origProjects }()

	d := NewDispatcher(db, nil)
	if err := d.Dispatch(context.Background(), mustParseReaction(t, "U_me", "claude", "C123", "1.10", "1.01")); err != nil {
		t.Fatalf("Dispatch err = %v", err)
	}
	if len(*spawns) != 1 {
		t.Fatalf("spawn count = %d, want 1", len(*spawns))
	}
	brief := (*spawns)[0].Brief

	for _, want := range []string{
		"## First step — pick a project",
		"Ask the operator **in this Claude Code session**",
		"flow update task " + (*spawns)[0].Slug + " --project <chosen-slug>",
		"--clear-project",
		"`budgeting`",
		"`devops`",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief missing %q\n--- brief ---\n%s", want, brief)
		}
	}
}

func TestRenderProjectPicker_EmptyCatalogFallsBack(t *testing.T) {
	body := renderProjectPicker("slack-c123-1-01", nil)
	if !strings.Contains(body, "No active projects found") {
		t.Errorf("empty catalog should explain how to recover; got:\n%s", body)
	}
	if !strings.Contains(body, "flow update task slack-c123-1-01") {
		t.Errorf("empty catalog should still show the update command so the agent can wire up a project the operator creates next; got:\n%s", body)
	}
}

func TestDispatcher_BriefIncludesOperatorIdentity(t *testing.T) {
	// Multi-workspace operator: both IDs must land in the brief so the
	// downstream agent can match either against incoming inbox events. The
	// reactor (the user adding the :claude: reaction) is also the operator
	// in this scenario, so the "reactor:" line must carry the (operator)
	// annotation — that's the eyeball cue when the brief is read top-down.
	t.Setenv("FLOW_SLACK_SELF_USER_IDS", "U_me, U_alt")
	t.Setenv("FLOW_SLACK_TRIGGER_EMOJI", "claude")
	t.Setenv("FLOW_SLACK_AUTOOPEN", "0")
	db := dispatcherTestDB(t)
	spawns, _, _, restore := stubDispatcherIO(t)
	defer restore()

	d := NewDispatcher(db, nil)
	if err := d.Dispatch(context.Background(), mustParseReaction(t, "U_me", "claude", "C123", "1.10", "1.01")); err != nil {
		t.Fatalf("Dispatch err = %v", err)
	}
	if len(*spawns) != 1 {
		t.Fatalf("spawn count = %d, want 1", len(*spawns))
	}
	brief := (*spawns)[0].Brief

	for _, want := range []string{
		"## Operator identity",
		"`U_me`",
		"`U_alt`",
		"reactor: U_me (operator)",
		// The inbox classification copy must reference event.user_id so the
		// agent knows which field to compare against.
		"event.user_id",
		"coordination signal",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief missing %q\n--- brief ---\n%s", want, brief)
		}
	}
}

func TestSlackTaskBrief_AnnotatesItemAuthorWhenOperator(t *testing.T) {
	// When the operator reacted to their own earlier message (a common
	// escalation pattern — react :claude: to one of your own coordination
	// messages to spawn an agent on the thread), both reactor AND item_author
	// equal the operator's ID. The brief must annotate both lines so the
	// agent isn't tricked into thinking the item author is an external party
	// to reply to.
	decision := ReactionDecision{
		Trigger:   true,
		ThreadKey: "C123:1.01",
		Channel:   "C123",
		ThreadTS:  "1.01",
		ItemTS:    "1.01",
		Reactor:   "U_me",
		Reaction:  "claude",
		Event: InboundEvent{
			Kind:        "reaction_added",
			Channel:     "C123",
			ChannelType: "channel",
			TS:          "1.10",
			ThreadTS:    "1.01",
			UserID:      "U_me",
			Reaction:    "claude",
			ItemChannel: "C123",
			ItemTS:      "1.01",
			ItemAuthor:  "U_me",
		},
	}
	brief := slackTaskBrief(decision, "slack-c123-1-01", "Slack reply", nil, []string{"U_me"})

	for _, want := range []string{
		"item_author: U_me (operator)",
		"reactor: U_me (operator)",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief missing %q\n--- brief ---\n%s", want, brief)
		}
	}
	// And the inverse: a non-operator item_author must NOT get the suffix.
	decision.Event.ItemAuthor = "U_customer"
	brief = slackTaskBrief(decision, "slack-c123-1-01", "Slack reply", nil, []string{"U_me"})
	if strings.Contains(brief, "U_customer (operator)") {
		t.Errorf("non-operator item_author should not be annotated:\n%s", brief)
	}
	if !strings.Contains(brief, "item_author: U_customer") {
		t.Errorf("non-operator item_author still expected in brief:\n%s", brief)
	}
}

func TestSlackTaskBrief_IncludesDMRegistrationInstruction(t *testing.T) {
	// When the agent replies via DM instead of in-thread, the recipient's
	// replies land in a DM channel the task isn't watching yet. The brief must
	// tell the agent to register that channel so DM replies stream into the
	// inbox like thread replies — the only deterministic attribution point.
	decision := ReactionDecision{
		Trigger:   true,
		ThreadKey: "C123:1.01",
		Channel:   "C123",
		ThreadTS:  "1.01",
		ItemTS:    "1.01",
		Reactor:   "U_me",
		Reaction:  "claude",
		Event:     InboundEvent{Kind: "reaction_added", Channel: "C123", ChannelType: "channel", ThreadTS: "1.01"},
	}
	brief := slackTaskBrief(decision, "slack-c123-1-01", "Slack reply", nil, []string{"U_me"})

	for _, want := range []string{
		"flow update task slack-c123-1-01 --tag slack-dm:",
		"replies in that DM",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief missing DM-registration cue %q\n--- brief ---\n%s", want, brief)
		}
	}
}

func TestRenderOperatorIdentity_GracefulWhenUnconfigured(t *testing.T) {
	body := renderOperatorIdentity(nil)
	// Recovery copy must (a) explain why the block is empty and (b) tell
	// the agent how to keep the classification rule from silently failing.
	// Without this, an empty block would tacitly invite "everyone is
	// external," which is the Goniyo failure mode.
	for _, want := range []string{
		"FLOW_SLACK_SELF_USER_IDS",
		"Ask the operator",
		"coordination signal",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("empty-id recovery copy missing %q\n--- body ---\n%s", want, body)
		}
	}
}

func TestDispatcher_CodexEmojiSpawnsCodexProvider(t *testing.T) {
	t.Setenv("FLOW_SLACK_SELF_USER_IDS", "U_me")
	t.Setenv("FLOW_SLACK_TRIGGER_EMOJI", "claude,codex")
	t.Setenv("FLOW_SLACK_AUTOOPEN", "0")
	db := dispatcherTestDB(t)
	spawns, _, _, restore := stubDispatcherIO(t)
	defer restore()

	d := NewDispatcher(db, nil)
	// A :codex: reaction must route to the codex agent. A :claude:
	// reaction in the same workspace continues to route to claude — the
	// emoji-to-provider mapping must be per-event, not per-installation.
	codexEv := mustParseReaction(t, "U_me", "codex", "C123", "1.10", "1.01")
	if err := d.Dispatch(context.Background(), codexEv); err != nil {
		t.Fatalf("dispatch codex: %v", err)
	}
	claudeEv := mustParseReaction(t, "U_me", "claude", "C456", "1.20", "1.02")
	if err := d.Dispatch(context.Background(), claudeEv); err != nil {
		t.Fatalf("dispatch claude: %v", err)
	}

	if len(*spawns) != 2 {
		t.Fatalf("spawn count = %d, want 2", len(*spawns))
	}
	if (*spawns)[0].Provider != "codex" {
		t.Errorf("codex spawn provider = %q, want codex", (*spawns)[0].Provider)
	}
	if (*spawns)[1].Provider != "claude" {
		t.Errorf("claude spawn provider = %q, want claude", (*spawns)[1].Provider)
	}
}

func TestDispatcher_AutoOpenWhenEnabled(t *testing.T) {
	t.Setenv("FLOW_SLACK_SELF_USER_IDS", "U_me")
	t.Setenv("FLOW_SLACK_TRIGGER_EMOJI", "claude")
	t.Setenv("FLOW_SLACK_AUTOOPEN", "1")
	db := dispatcherTestDB(t)
	_, _, opens, restore := stubDispatcherIO(t)
	defer restore()

	d := NewDispatcher(db, nil)
	reaction := mustParseReaction(t, "U_me", "claude", "C123", "1234.0010", "1234.0001")
	if err := d.Dispatch(context.Background(), reaction); err != nil {
		t.Fatalf("Dispatch err = %v", err)
	}
	if len(*opens) != 1 {
		t.Fatalf("expected 1 open call; got %v", *opens)
	}
}

func TestDispatcher_ExistingThreadReactionSkipsSpawnAndAppends(t *testing.T) {
	t.Setenv("FLOW_SLACK_SELF_USER_IDS", "U_me")
	t.Setenv("FLOW_SLACK_TRIGGER_EMOJI", "claude")
	db := dispatcherTestDB(t)
	spawns, _, opens, restore := stubDispatcherIO(t)
	defer restore()

	threadKey := "C123:1234.0001"
	seedSlackTask(t, db, "preexisting-task", threadKey)

	d := NewDispatcher(db, nil)
	reaction := mustParseReaction(t, "U_me", "claude", "C123", "1234.0020", "1234.0001")
	if err := d.Dispatch(context.Background(), reaction); err != nil {
		t.Fatalf("Dispatch err = %v", err)
	}

	if len(*spawns) != 0 {
		t.Fatalf("spawn should NOT fire for existing thread; got %v", *spawns)
	}
	if len(*opens) != 0 {
		t.Fatalf("open should NOT fire for existing thread; got %v", *opens)
	}

	entries, err := ReadInboxEntries("preexisting-task")
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("inbox len = %d, want 1", len(entries))
	}
	if entries[0].Event.Kind != "reaction_added" {
		t.Errorf("inbox event kind = %q", entries[0].Event.Kind)
	}
}

func TestDispatcher_NonTriggerReactionIgnored(t *testing.T) {
	t.Setenv("FLOW_SLACK_SELF_USER_IDS", "U_me")
	t.Setenv("FLOW_SLACK_TRIGGER_EMOJI", "claude")
	db := dispatcherTestDB(t)
	spawns, tags, opens, restore := stubDispatcherIO(t)
	defer restore()

	d := NewDispatcher(db, nil)
	// Coworker reacted — not consent.
	noConsent := mustParseReaction(t, "U_coworker", "claude", "C123", "1.5", "1.1")
	// Wrong emoji.
	wrongEmoji := mustParseReaction(t, "U_me", "thumbsup", "C123", "1.6", "1.1")

	for _, ev := range []InboundEvent{noConsent, wrongEmoji} {
		if err := d.Dispatch(context.Background(), ev); err != nil {
			t.Fatalf("Dispatch err = %v", err)
		}
	}
	if len(*spawns)+len(*tags)+len(*opens) != 0 {
		t.Errorf("non-trigger events should have no side effects; spawns=%v tags=%v opens=%v",
			*spawns, *tags, *opens)
	}
}

func TestDispatcher_MessageInTrackedThreadAppends(t *testing.T) {
	t.Setenv("FLOW_SLACK_SELF_USER_IDS", "U_me")
	db := dispatcherTestDB(t)
	_, _, _, restore := stubDispatcherIO(t)
	defer restore()

	threadKey := "C123:1700000000.000100"
	seedSlackTask(t, db, "live-thread", threadKey)

	// A follow-up message from a coworker in the tracked thread.
	msg := InboundEvent{
		Kind:        "message",
		Channel:     "C123",
		ChannelType: "channel",
		TS:          "1700000050.000001",
		ThreadTS:    "1700000000.000100",
		UserID:      "U_coworker",
		Text:        "another reply",
	}
	d := NewDispatcher(db, nil)
	if err := d.Dispatch(context.Background(), msg); err != nil {
		t.Fatalf("Dispatch err = %v", err)
	}

	entries, err := ReadInboxEntries("live-thread")
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("inbox len = %d, want 1", len(entries))
	}
	if entries[0].Event.Text != "another reply" || entries[0].Event.UserID != "U_coworker" {
		t.Errorf("inbox entry wrong: %+v", entries[0])
	}
}

func TestDispatcher_MessageInUntrackedThreadIgnored(t *testing.T) {
	// No matching task → message is dropped. This is the firehose-suppression
	// guarantee: only threads we've consented to track ever reach Claude.
	db := dispatcherTestDB(t)
	_, _, _, restore := stubDispatcherIO(t)
	defer restore()

	d := NewDispatcher(db, nil)
	msg := InboundEvent{
		Kind:    "message",
		Channel: "C_nothere",
		TS:      "1.5",
		Text:    "noise",
	}
	if err := d.Dispatch(context.Background(), msg); err != nil {
		t.Fatalf("Dispatch err = %v", err)
	}
	// No inbox file created anywhere
	root := strings.TrimSpace(getenv(t, "FLOW_ROOT"))
	if root == "" {
		t.Skip("FLOW_ROOT not set; skip filesystem check")
	}
	if entries, _ := ReadInboxEntries("slack-c-nothere-1-5"); len(entries) != 0 {
		t.Errorf("untracked thread should not produce inbox: %v", entries)
	}
}

// seedSlackDMTask inserts a task and tags it with one or more slack-dm:
// channel linkages so DM routing lookups can find it. Mirrors seedSlackTask
// but for the channel-only DM monitoring tags.
func seedSlackDMTask(t *testing.T, db *sql.DB, slug string, dmChannels ...string) {
	t.Helper()
	now := flowdb.NowISO()
	_, err := db.Exec(
		`INSERT INTO tasks (slug, name, status, priority, work_dir, permission_mode, session_provider, status_changed_at, created_at, updated_at)
		 VALUES (?, ?, 'backlog', 'high', ?, 'default', 'claude', ?, ?, ?)`,
		slug, "seeded slack dm task", t.TempDir(), now, now, now,
	)
	if err != nil {
		t.Fatalf("seed dm task %s: %v", slug, err)
	}
	if err := flowdb.AddTaskTag(db, slug, "slack-reply"); err != nil {
		t.Fatalf("tag slack-reply: %v", err)
	}
	for _, ch := range dmChannels {
		// Literal prefix (not the constant) so the test fails on routing
		// behavior rather than a missing symbol during the RED step.
		if err := flowdb.AddTaskTag(db, slug, "slack-dm:"+ch); err != nil {
			t.Fatalf("tag slack-dm:%s: %v", ch, err)
		}
	}
}

func TestDispatcher_MessageInTrackedDMAppends(t *testing.T) {
	// A DM the agent registered for this task. Unlike a thread, the DM is
	// matched by channel ID alone — each top-level DM message carries its own
	// thread_ts, so a thread-key match would never fire.
	db := dispatcherTestDB(t)
	_, _, _, restore := stubDispatcherIO(t)
	defer restore()

	seedSlackDMTask(t, db, "dm-task", "D_ALICE")

	msg := InboundEvent{
		Kind:        "message",
		Channel:     "D_ALICE",
		ChannelType: "im",
		TS:          "1700000100.000001",
		ThreadTS:    "1700000100.000001", // top-level DM message: thread_ts == ts
		UserID:      "U_alice",
		Text:        "thanks for the DM!",
	}
	d := NewDispatcher(db, nil)
	if err := d.Dispatch(context.Background(), msg); err != nil {
		t.Fatalf("Dispatch err = %v", err)
	}

	entries, err := ReadInboxEntries("dm-task")
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("inbox len = %d, want 1", len(entries))
	}
	if entries[0].Event.Text != "thanks for the DM!" || entries[0].Event.ChannelType != "im" {
		t.Errorf("inbox entry wrong: %+v", entries[0].Event)
	}
}

func TestDispatcher_DMMessageInUntrackedChannelIgnored(t *testing.T) {
	// A DM channel no task registered → dropped, same firehose-suppression
	// guarantee as untracked threads.
	db := dispatcherTestDB(t)
	_, _, _, restore := stubDispatcherIO(t)
	defer restore()

	d := NewDispatcher(db, nil)
	msg := InboundEvent{
		Kind:        "message",
		Channel:     "D_STRANGER",
		ChannelType: "im",
		TS:          "1700000200.000001",
		ThreadTS:    "1700000200.000001",
		UserID:      "U_stranger",
		Text:        "unsolicited DM",
	}
	if err := d.Dispatch(context.Background(), msg); err != nil {
		t.Fatalf("Dispatch err = %v", err)
	}
	if entries, _ := ReadInboxEntries("any-task"); len(entries) != 0 {
		t.Errorf("untracked DM should not produce inbox: %v", entries)
	}
}

func TestDispatcher_MultipleDMTagsRouteIntoOneInbox(t *testing.T) {
	// One task can monitor several DMs at once (e.g. the agent DM'd two
	// people for the same task). Messages from each registered channel land
	// in the single task inbox.
	db := dispatcherTestDB(t)
	_, _, _, restore := stubDispatcherIO(t)
	defer restore()

	seedSlackDMTask(t, db, "multi-dm-task", "D_ALICE", "D_BOB")

	d := NewDispatcher(db, nil)
	for _, m := range []InboundEvent{
		{Kind: "message", Channel: "D_ALICE", ChannelType: "im", TS: "1700000300.000001", ThreadTS: "1700000300.000001", UserID: "U_alice", Text: "from alice"},
		{Kind: "message", Channel: "D_BOB", ChannelType: "im", TS: "1700000400.000001", ThreadTS: "1700000400.000001", UserID: "U_bob", Text: "from bob"},
	} {
		if err := d.Dispatch(context.Background(), m); err != nil {
			t.Fatalf("Dispatch err = %v", err)
		}
	}

	entries, err := ReadInboxEntries("multi-dm-task")
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("inbox len = %d, want 2", len(entries))
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Event.Text] = true
	}
	if !got["from alice"] || !got["from bob"] {
		t.Errorf("expected messages from both DMs; got %v", got)
	}
}

// fakeDMMembers stubs the conversations.members resolver used by DM
// auto-registration. Keyed by channel ID.
type fakeDMMembers struct {
	byChannel map[string][]string
	calls     int
}

func (f *fakeDMMembers) DMMembers(_ context.Context, channelID string) ([]string, error) {
	f.calls++
	return f.byChannel[normalizeSlackChannelID(channelID)], nil
}

// seedThreadParticipant appends an inbox message authored by uid so the task's
// participant set (scanned from inbox user_ids) includes that user.
func seedThreadParticipant(t *testing.T, slug, channel, uid, text string) {
	t.Helper()
	if err := AppendInboxEvent(slug, InboundEvent{
		Kind: "message", Channel: channel, ChannelType: "channel",
		TS: "1700000001.000001", ThreadTS: "1700000000.000100", UserID: uid, Text: text,
	}); err != nil {
		t.Fatalf("seed participant: %v", err)
	}
}

func agentOutboundDM(channel, text string) InboundEvent {
	return InboundEvent{
		Kind: "message", Channel: channel, ChannelType: "im",
		TS: "1780489050.947509", ThreadTS: "1780489050.947509",
		UserID: "U_me", Text: text,
	}
}

func TestDispatcher_AutoRegistersAgentDMToMatchingThread(t *testing.T) {
	// The agent (running as the operator) DMs a thread participant. flow sees
	// the outbound DM over the socket (footer = "Sent using @Claude"), resolves
	// the recipient, matches them to the one active thread that has them as a
	// participant, and auto-registers the slack-dm tag — no agent cooperation.
	t.Setenv("FLOW_SLACK_SELF_USER_IDS", "U_me")
	db := dispatcherTestDB(t)
	_, tags, _, restore := stubDispatcherIO(t)
	defer restore()

	seedSlackTask(t, db, "coinswitch", "C123:1700000000.000100")
	seedThreadParticipant(t, "coinswitch", "C123", "U_ishaan", "ishaan in thread")

	d := NewDispatcher(db, nil)
	d.SetDMMembersResolver(&fakeDMMembers{byChannel: map[string][]string{
		"D_NEW": {"U_me", "U_ishaan"},
	}})

	out := agentOutboundDM("D_NEW", "PR ready to merge from my side.\n\nSent using @Claude")
	if err := d.Dispatch(context.Background(), out); err != nil {
		t.Fatalf("Dispatch err = %v", err)
	}

	if !dmTagRegistered(*tags, "coinswitch", "D_NEW") {
		t.Fatalf("expected slack-dm:D_NEW registered on coinswitch; tag calls = %v", *tags)
	}
	// The outbound DM is recorded so the DM channel has a backfill baseline.
	entries, _ := ReadInboxEntries("coinswitch")
	foundOut := false
	for _, e := range entries {
		if e.Event.Channel == "D_NEW" && e.Event.TS == "1780489050.947509" {
			foundOut = true
		}
	}
	if !foundOut {
		t.Errorf("outbound DM should be appended to establish a baseline; entries=%v", entries)
	}
}

func TestDispatcher_DoesNotAutoRegisterWithoutFooter(t *testing.T) {
	// No agent footer → indistinguishable from a personal DM you typed → never
	// auto-register (the privacy footgun guard).
	t.Setenv("FLOW_SLACK_SELF_USER_IDS", "U_me")
	db := dispatcherTestDB(t)
	_, tags, _, restore := stubDispatcherIO(t)
	defer restore()

	seedSlackTask(t, db, "coinswitch", "C123:1700000000.000100")
	seedThreadParticipant(t, "coinswitch", "C123", "U_ishaan", "ishaan in thread")

	d := NewDispatcher(db, nil)
	resolver := &fakeDMMembers{byChannel: map[string][]string{"D_NEW": {"U_me", "U_ishaan"}}}
	d.SetDMMembersResolver(resolver)

	out := agentOutboundDM("D_NEW", "hey, personal note, nothing to do with work")
	if err := d.Dispatch(context.Background(), out); err != nil {
		t.Fatalf("Dispatch err = %v", err)
	}
	if dmTagRegistered(*tags, "coinswitch", "D_NEW") {
		t.Fatalf("must NOT auto-register a footer-less DM; tag calls = %v", *tags)
	}
	if resolver.calls != 0 {
		t.Errorf("should not even resolve members for a footer-less DM; calls=%d", resolver.calls)
	}
}

func TestDispatcher_DoesNotAutoRegisterNonOperatorAuthor(t *testing.T) {
	// A footer-bearing message authored by someone who isn't the operator is
	// not an agent-as-you send; don't auto-register.
	t.Setenv("FLOW_SLACK_SELF_USER_IDS", "U_me")
	db := dispatcherTestDB(t)
	_, tags, _, restore := stubDispatcherIO(t)
	defer restore()

	seedSlackTask(t, db, "coinswitch", "C123:1700000000.000100")
	seedThreadParticipant(t, "coinswitch", "C123", "U_ishaan", "ishaan in thread")

	d := NewDispatcher(db, nil)
	d.SetDMMembersResolver(&fakeDMMembers{byChannel: map[string][]string{"D_NEW": {"U_x", "U_ishaan"}}})

	out := agentOutboundDM("D_NEW", "Sent using @Claude")
	out.UserID = "U_someone_else"
	if err := d.Dispatch(context.Background(), out); err != nil {
		t.Fatalf("Dispatch err = %v", err)
	}
	if dmTagRegistered(*tags, "coinswitch", "D_NEW") {
		t.Fatalf("non-operator author must not auto-register; tag calls = %v", *tags)
	}
}

func TestDispatcher_AutoRegisterAmbiguousSkips(t *testing.T) {
	// The recipient participates in TWO active threads → ambiguous → skip
	// (fall back to manual/agent registration) rather than guess wrong.
	t.Setenv("FLOW_SLACK_SELF_USER_IDS", "U_me")
	db := dispatcherTestDB(t)
	_, tags, _, restore := stubDispatcherIO(t)
	defer restore()

	seedSlackTask(t, db, "task-a", "C123:1700000000.000100")
	seedThreadParticipant(t, "task-a", "C123", "U_ishaan", "ishaan in A")
	seedSlackTask(t, db, "task-b", "C456:1700000000.000200")
	seedThreadParticipant(t, "task-b", "C456", "U_ishaan", "ishaan in B")

	d := NewDispatcher(db, nil)
	d.SetDMMembersResolver(&fakeDMMembers{byChannel: map[string][]string{"D_NEW": {"U_me", "U_ishaan"}}})

	out := agentOutboundDM("D_NEW", "Sent using @Claude")
	if err := d.Dispatch(context.Background(), out); err != nil {
		t.Fatalf("Dispatch err = %v", err)
	}
	for _, slug := range []string{"task-a", "task-b"} {
		if dmTagRegistered(*tags, slug, "D_NEW") {
			t.Fatalf("ambiguous recipient must not auto-register %s; tag calls = %v", slug, *tags)
		}
	}
}

// dmTagRegistered reports whether the dispatcher requested a slack-dm:<channel>
// tag on slug. tagFlowTask is stubbed in tests, so registration is observed via
// the recorded tag calls rather than the DB.
func dmTagRegistered(calls []tagCall, slug, channel string) bool {
	want := SlackDMTagPrefix + channel
	for _, c := range calls {
		if c.Slug == slug && c.Tag == want {
			return true
		}
	}
	return false
}

func TestDispatcher_BackfillSlackTaskTitlesOnlyLegacyNames(t *testing.T) {
	t.Setenv("FLOW_SLACK_SELF_USER_IDS", "U_me")
	db := dispatcherTestDB(t)
	_, _, _, restoreIO := stubDispatcherIO(t)
	defer restoreIO()

	legacyKey := "D123:1779345633.950689"
	manualKey := "D456:1779345999.123456"
	seedSlackTask(t, db, "legacy-slack", legacyKey)
	seedSlackTask(t, db, "manual-slack", manualKey)
	if _, err := db.Exec(`UPDATE tasks SET name = ? WHERE slug = ?`,
		"Slack reply in D123 (thread 1779345633.9506)", "legacy-slack"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE tasks SET name = ? WHERE slug = ?`,
		"Rohit - manually curated context", "manual-slack"); err != nil {
		t.Fatal(err)
	}

	origResolver := resolveSlackTaskTitle
	resolveSlackTaskTitle = func(_ context.Context, decision ReactionDecision) (string, error) {
		switch decision.ThreadKey {
		case legacyKey:
			return "Rohit - CoinSwitch CSX project kickoff", nil
		case manualKey:
			return "Should not overwrite manual names", nil
		default:
			return "", nil
		}
	}
	defer func() { resolveSlackTaskTitle = origResolver }()

	d := NewDispatcher(db, nil)
	updated, err := d.BackfillSlackTaskTitles(context.Background())
	if err != nil {
		t.Fatalf("BackfillSlackTaskTitles: %v", err)
	}
	if updated != 1 {
		t.Fatalf("BackfillSlackTaskTitles updated %d tasks, want 1", updated)
	}

	legacy, err := flowdb.GetTask(db, "legacy-slack")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Name != "Rohit - CoinSwitch CSX project kickoff" {
		t.Fatalf("legacy name = %q", legacy.Name)
	}
	manual, err := flowdb.GetTask(db, "manual-slack")
	if err != nil {
		t.Fatal(err)
	}
	if manual.Name != "Rohit - manually curated context" {
		t.Fatalf("manual name was overwritten: %q", manual.Name)
	}
}

func TestSlugForThread_Idempotent(t *testing.T) {
	// Same key in → same slug out. Required for re-fire safety.
	got1 := SlugForThread("C123:1234.0001")
	got2 := SlugForThread("C123:1234.0001")
	if got1 != got2 {
		t.Errorf("not deterministic: %q vs %q", got1, got2)
	}
	if !strings.HasPrefix(got1, "slack-") {
		t.Errorf("missing prefix: %q", got1)
	}
	if strings.ContainsAny(got1, ":._") {
		t.Errorf("slug should not contain colons/dots/underscores: %q", got1)
	}
}

func TestSlugForThread_CollapsesDashes(t *testing.T) {
	// Adjacent separators (colon + dot or two dots from edge cases) shouldn't
	// produce double dashes in the slug.
	got := SlugForThread("C1..2")
	if strings.Contains(got, "--") {
		t.Errorf("doubled dash: %q", got)
	}
}

func TestSlugForThread_Empty(t *testing.T) {
	if got := SlugForThread(""); got != "" {
		t.Errorf("empty in → empty out, got %q", got)
	}
}

func getenv(t *testing.T, name string) string {
	t.Helper()
	return strings.TrimSpace(os.Getenv(name))
}
