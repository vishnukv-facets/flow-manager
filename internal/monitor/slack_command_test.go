package monitor

import "testing"

func TestIsCommandChannel_MatchesBotIMOnly(t *testing.T) {
	orig := conversationIsBotIMFn
	// Bot is a member of D_bot only; D_colleague is a third-party DM (bot absent).
	conversationIsBotIMFn = func(channel string) bool { return channel == "D_bot" }
	defer func() { conversationIsBotIMFn = orig }()
	resetCommandChannelCache()

	yes := InboundEvent{Kind: "message", ChannelType: "im", Channel: "D_bot", UserID: "U_me"}
	if !IsCommandChannel(yes) {
		t.Errorf("DM where the bot is a member should be a command channel")
	}
	no := InboundEvent{Kind: "message", ChannelType: "im", Channel: "D_colleague", UserID: "U_me"}
	if IsCommandChannel(no) {
		t.Errorf("operator's DM with a third party (bot not a member) must NOT be the command channel")
	}
	chMsg := InboundEvent{Kind: "message", ChannelType: "channel", Channel: "D_bot"}
	if IsCommandChannel(chMsg) {
		t.Errorf("non-im event must not match")
	}
	resetCommandChannelCache()
}

// TestResetCommandChannelCache_ForcesReResolution verifies that the bot-IM
// membership cache memoizes per-channel and that ResetCommandChannelCache()
// forces conversationIsBotIMFn to be invoked again instead of returning the
// cached result.
func TestResetCommandChannelCache_ForcesReResolution(t *testing.T) {
	orig := conversationIsBotIMFn
	defer func() {
		conversationIsBotIMFn = orig
		resetCommandChannelCache()
	}()
	resetCommandChannelCache()

	callCount := 0
	conversationIsBotIMFn = func(channel string) bool {
		callCount++
		return true
	}

	// First lookup resolves (counter = 1).
	if !botIsMemberOfIM("D_test") {
		t.Fatalf("expected D_test to resolve as a bot IM")
	}
	if callCount != 1 {
		t.Fatalf("expected 1 resolve call after first botIsMemberOfIM(), got %d", callCount)
	}

	// Second lookup hits the cache — counter stays at 1.
	_ = botIsMemberOfIM("D_test")
	if callCount != 1 {
		t.Fatalf("expected cache hit on second botIsMemberOfIM(), resolver call count should still be 1, got %d", callCount)
	}

	// ResetCommandChannelCache clears the cache; next lookup must re-resolve.
	ResetCommandChannelCache()
	_ = botIsMemberOfIM("D_test")
	if callCount != 2 {
		t.Fatalf("expected 2 resolve calls after ResetCommandChannelCache()+botIsMemberOfIM(), got %d", callCount)
	}
}

func TestCommandChannelEnabled(t *testing.T) {
	t.Setenv("FLOW_SLACK_COMMAND_ENABLED", "")
	if CommandChannelEnabled() {
		t.Errorf("default should be disabled")
	}
	t.Setenv("FLOW_SLACK_COMMAND_ENABLED", "1")
	if !CommandChannelEnabled() {
		t.Errorf("=1 should enable")
	}
}

func TestAuthorizedOperator(t *testing.T) {
	t.Setenv("FLOW_SLACK_SELF_USER_IDS", "U_me, U_alt") // space between IDs tests whitespace tolerance
	if !AuthorizedOperator("U_me") || !AuthorizedOperator("U_alt") {
		t.Errorf("listed operator IDs must be authorized")
	}
	if AuthorizedOperator("U_other") {
		t.Errorf("non-operator must NOT be authorized")
	}
	if AuthorizedOperator("") {
		t.Errorf("empty author must NOT be authorized")
	}
}

// TestAuthorizedOperator_TokenOwnerFallback verifies the robustness fix: the
// operator's own id may not appear in FLOW_SLACK_SELF_USER_IDS (Enterprise-Grid
// alternate id / stale env), but AuthorizedOperator still accepts them when their
// id matches the USER-token owner resolved via auth.test.
func TestAuthorizedOperator_TokenOwnerFallback(t *testing.T) {
	orig := operatorUserIDFn
	defer func() {
		operatorUserIDFn = orig
		resetCommandChannelCache()
	}()
	operatorUserIDFn = func() string { return "U_token_owner" }

	cases := []struct {
		name    string
		selfIDs string
		userID  string
		want    bool
	}{
		{"in self IDs", "U_me", "U_me", true},
		{"not in self IDs but is token owner", "U_me", "U_token_owner", true},
		{"neither self ID nor token owner", "U_me", "U_other", false},
		{"empty author", "U_me", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetCommandChannelCache() // reset memoized token owner between cases
			t.Setenv("FLOW_SLACK_SELF_USER_IDS", tc.selfIDs)
			if got := AuthorizedOperator(tc.userID); got != tc.want {
				t.Errorf("AuthorizedOperator(%q) with selfIDs=%q = %v, want %v", tc.userID, tc.selfIDs, got, tc.want)
			}
		})
	}
}

// TestOperatorIdentityKnown verifies that identity is "known" when EITHER the
// self-IDs env is set OR the token owner resolves, and unknown only when both
// are empty.
func TestOperatorIdentityKnown(t *testing.T) {
	orig := operatorUserIDFn
	defer func() {
		operatorUserIDFn = orig
		resetCommandChannelCache()
	}()

	// Self IDs set, token unresolved → known.
	operatorUserIDFn = func() string { return "" }
	resetCommandChannelCache()
	t.Setenv("FLOW_SLACK_SELF_USER_IDS", "U_me")
	if !OperatorIdentityKnown() {
		t.Errorf("identity should be known when self IDs are set")
	}

	// Self IDs empty, token owner resolves → known.
	operatorUserIDFn = func() string { return "U_token_owner" }
	resetCommandChannelCache()
	t.Setenv("FLOW_SLACK_SELF_USER_IDS", "")
	if !OperatorIdentityKnown() {
		t.Errorf("identity should be known when only the token owner resolves")
	}

	// Both empty → unknown.
	operatorUserIDFn = func() string { return "" }
	resetCommandChannelCache()
	t.Setenv("FLOW_SLACK_SELF_USER_IDS", "")
	if OperatorIdentityKnown() {
		t.Errorf("identity should be unknown when both self IDs and token owner are empty")
	}
}

// TestIsSelfAuthoredSlack verifies flow recognizes its OWN bot's messages
// (echoed back through the listener) by the resolved bot user id, and never
// mistakes the operator or third parties for itself. Fail-safe: an unresolved
// bot id (empty) matches nothing, so real traffic is processed, not swallowed.
func TestIsSelfAuthoredSlack(t *testing.T) {
	orig := selfBotUserIDFn
	defer func() {
		selfBotUserIDFn = orig
		resetCommandChannelCache()
	}()

	// Bot id resolves to U_bot.
	selfBotUserIDFn = func() string { return "U_bot" }
	resetCommandChannelCache()
	cases := []struct {
		name string
		ev   InboundEvent
		want bool
	}{
		{"flow's own bot message", InboundEvent{Kind: "message", ChannelType: "im", Channel: "D_bot", UserID: "U_bot"}, true},
		{"operator message", InboundEvent{Kind: "message", ChannelType: "im", Channel: "D_bot", UserID: "U_me"}, false},
		{"third party", InboundEvent{Kind: "message", ChannelType: "channel", Channel: "C1", UserID: "U_other"}, false},
		{"empty author", InboundEvent{Kind: "message", ChannelType: "im", Channel: "D_bot", UserID: ""}, false},
		{"github login that isn't us", InboundEvent{Kind: "message", ChannelType: "github", UserID: "octocat"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSelfAuthoredSlack(tc.ev); got != tc.want {
				t.Errorf("IsSelfAuthoredSlack(%+v) = %v, want %v", tc.ev, got, tc.want)
			}
		})
	}

	// Fail-safe: when the bot id can't be resolved, nothing is self-authored.
	selfBotUserIDFn = func() string { return "" }
	resetCommandChannelCache()
	if IsSelfAuthoredSlack(InboundEvent{Kind: "message", ChannelType: "im", UserID: "U_bot"}) {
		t.Errorf("unresolved bot id must make IsSelfAuthoredSlack false (fail-safe)")
	}
}

// TestResetCommandChannelCache_ClearsSelfBotID verifies the bot user id is
// memoized once and that ResetCommandChannelCache() forces re-resolution
// (the bot token changes on reinstall).
func TestResetCommandChannelCache_ClearsSelfBotID(t *testing.T) {
	orig := selfBotUserIDFn
	defer func() {
		selfBotUserIDFn = orig
		resetCommandChannelCache()
	}()
	resetCommandChannelCache()

	callCount := 0
	selfBotUserIDFn = func() string {
		callCount++
		return "U_bot"
	}

	if selfBotUserID() != "U_bot" {
		t.Fatalf("expected bot id to resolve")
	}
	if callCount != 1 {
		t.Fatalf("expected 1 resolve call, got %d", callCount)
	}
	_ = selfBotUserID()
	if callCount != 1 {
		t.Fatalf("expected cache hit (count still 1), got %d", callCount)
	}
	ResetCommandChannelCache()
	_ = selfBotUserID()
	if callCount != 2 {
		t.Fatalf("expected 2 resolve calls after reset, got %d", callCount)
	}
}

// TestResetCommandChannelCache_ClearsOperatorID verifies the token-owner id is
// memoized once and that ResetCommandChannelCache() forces re-resolution.
func TestResetCommandChannelCache_ClearsOperatorID(t *testing.T) {
	orig := operatorUserIDFn
	defer func() {
		operatorUserIDFn = orig
		resetCommandChannelCache()
	}()
	resetCommandChannelCache()

	callCount := 0
	operatorUserIDFn = func() string {
		callCount++
		return "U_token_owner"
	}

	if operatorUserID() != "U_token_owner" {
		t.Fatalf("expected token owner to resolve")
	}
	if callCount != 1 {
		t.Fatalf("expected 1 resolve call, got %d", callCount)
	}
	// Cached — no re-resolution.
	_ = operatorUserID()
	if callCount != 1 {
		t.Fatalf("expected cache hit (count still 1), got %d", callCount)
	}
	// Reset forces re-resolution.
	ResetCommandChannelCache()
	_ = operatorUserID()
	if callCount != 2 {
		t.Fatalf("expected 2 resolve calls after reset, got %d", callCount)
	}
}
