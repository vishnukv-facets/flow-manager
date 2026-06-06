package steering

import (
	"path/filepath"
	"testing"

	"flow/internal/flowdb"
)

func TestAutonomyFromEnv(t *testing.T) {
	t.Run("empty → all off defaults", func(t *testing.T) {
		t.Setenv("FLOW_STEERING_AUTONOMY", "")
		p := AutonomyFromEnv()
		if p.Allow(ActionMakeTask, 1.0) {
			t.Error("empty env should leave make_task off")
		}
	})
	t.Run("bad JSON → safe defaults (never accidentally ON)", func(t *testing.T) {
		t.Setenv("FLOW_STEERING_AUTONOMY", "{not json")
		if AutonomyFromEnv().Allow(ActionForward, 1.0) {
			t.Error("malformed JSON must not enable any action")
		}
	})
	t.Run("override enables an action above threshold", func(t *testing.T) {
		t.Setenv("FLOW_STEERING_AUTONOMY", `{"make_task":{"enabled":true,"threshold":0.7}}`)
		p := AutonomyFromEnv()
		if !p.Allow(ActionMakeTask, 0.75) {
			t.Error("make_task should be allowed at 0.75 (>= 0.7)")
		}
		if p.Allow(ActionMakeTask, 0.6) {
			t.Error("make_task should be denied at 0.6 (< 0.7)")
		}
		// unspecified actions keep the safe default (off)
		if p.Allow(ActionReply, 1.0) {
			t.Error("reply not in override should stay off")
		}
	})
	t.Run("threshold clamped to [0,1]", func(t *testing.T) {
		t.Setenv("FLOW_STEERING_AUTONOMY", `{"forward":{"enabled":true,"threshold":5}}`)
		// threshold clamps to 1.0, so even 0.99 is denied
		if AutonomyFromEnv().Allow(ActionForward, 0.99) {
			t.Error("threshold 5 should clamp to 1.0 → deny 0.99")
		}
	})
	t.Run("unknown action key ignored", func(t *testing.T) {
		t.Setenv("FLOW_STEERING_AUTONOMY", `{"frobnicate":{"enabled":true,"threshold":0}}`)
		_ = AutonomyFromEnv() // must not panic; unknown key ignored
	})
}

func TestDefaultAutonomyIsSurfaceOnly(t *testing.T) {
	p := DefaultAutonomy()
	for _, a := range []Action{ActionMakeTask, ActionForward, ActionReply, ActionAFKReply} {
		if p.Allow(a, 1.0) {
			t.Errorf("DefaultAutonomy allowed %q at confidence 1.0; want surface-only (deny)", a)
		}
	}
}

func TestAutonomyAllow(t *testing.T) {
	p := AutonomyPolicy{
		ActionForward:  {Enabled: true, Threshold: 0.85},
		ActionAFKReply: {Enabled: false, Threshold: 0.90},
	}
	cases := []struct {
		action     Action
		confidence float64
		want       bool
	}{
		{ActionForward, 0.90, true},
		{ActionForward, 0.85, true},
		{ActionForward, 0.80, false},
		{ActionAFKReply, 0.99, false},
		{ActionReply, 1.0, false},
	}
	for _, c := range cases {
		if got := p.Allow(c.action, c.confidence); got != c.want {
			t.Errorf("Allow(%q, %.2f) = %v, want %v", c.action, c.confidence, got, c.want)
		}
	}
}

func TestAutonomyFnWithFeedbackAdjustsThresholdsWithoutEnablingActions(t *testing.T) {
	db, err := flowdb.OpenDB(filepath.Join(t.TempDir(), "flow.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	for i := 0; i < 3; i++ {
		if err := flowdb.RecordAttentionFeedback(db, flowdb.AttentionFeedback{
			ID: "approved-forward-" + string(rune('a'+i)), FeedItemID: "fa", Source: "slack",
			Channel: "C_SIGNAL", Author: "U_OK", ThreadType: "channel", ThreadKey: "C_SIGNAL:1",
			SuggestedAction: "forward", FinalAction: "forward", Outcome: "approved",
			Confidence: 0.76, ConfidenceBand: "0.70-0.79", CreatedAt: "2026-06-05T10:00:00Z",
		}); err != nil {
			t.Fatalf("record approval %d: %v", i, err)
		}
		if err := flowdb.RecordAttentionFeedback(db, flowdb.AttentionFeedback{
			ID: "dismiss-reply-" + string(rune('a'+i)), FeedItemID: "fd", Source: "slack",
			Channel: "C_NOISE", Author: "U_NO", ThreadType: "channel", ThreadKey: "C_NOISE:1",
			SuggestedAction: "reply", FinalAction: "dismiss", Outcome: "dismissed",
			Confidence: 0.86, ConfidenceBand: "0.80-0.89", CreatedAt: "2026-06-05T11:00:00Z",
		}); err != nil {
			t.Fatalf("record dismiss %d: %v", i, err)
		}
	}

	base := func() AutonomyPolicy {
		return AutonomyPolicy{
			ActionForward:  {Enabled: true, Threshold: 0.85},
			ActionReply:    {Enabled: true, Threshold: 0.90},
			ActionMakeTask: {Enabled: false, Threshold: 0.80},
		}
	}
	pol := AutonomyFnWithFeedback(db, base)()
	if got := pol[ActionForward].Threshold; got != 0.80 {
		t.Errorf("forward threshold = %.2f, want 0.80 after approvals", got)
	}
	if got := pol[ActionReply].Threshold; got != 0.95 {
		t.Errorf("reply threshold = %.2f, want 0.95 after dismissals", got)
	}
	if pol[ActionMakeTask].Enabled {
		t.Error("feedback overlay must not enable a disabled action")
	}
}
