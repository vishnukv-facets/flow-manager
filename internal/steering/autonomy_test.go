package steering

import "testing"

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
