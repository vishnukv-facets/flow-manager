package steering

// ActionPolicy is the operator's autonomy setting for one action: whether the
// steerer may perform it without asking, and the minimum confidence required.
type ActionPolicy struct {
	Enabled   bool    `json:"enabled"`
	Threshold float64 `json:"threshold"`
}

// AutonomyPolicy maps each action to its policy. A missing action is treated
// as disabled (deny). See spec §8.
type AutonomyPolicy map[Action]ActionPolicy

// DefaultAutonomy returns the P1 posture: every action surface-only (disabled).
// The thresholds are pre-seeded with the spec's defaults so the P2 settings UI
// has sensible starting values when an action is later enabled.
func DefaultAutonomy() AutonomyPolicy {
	return AutonomyPolicy{
		ActionForward:  {Enabled: false, Threshold: 0.85},
		ActionAFKReply: {Enabled: false, Threshold: 0.90},
		ActionMakeTask: {Enabled: false, Threshold: 0.80},
		ActionReply:    {Enabled: false, Threshold: 0.95},
	}
}

// Allow reports whether the steerer may perform action autonomously at the
// given confidence. This is the single chokepoint every outward effect must
// pass; an action that is absent or disabled is always denied, so triage code
// can never act on its own unless the operator opted in.
func (p AutonomyPolicy) Allow(action Action, confidence float64) bool {
	pol, ok := p[action]
	if !ok || !pol.Enabled {
		return false
	}
	return confidence >= pol.Threshold
}
