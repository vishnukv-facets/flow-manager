package app

import (
	"testing"
)

func TestPreferredUIFlowBinaryUsesPathFlow(t *testing.T) {
	if got := preferredUIFlowBinary("/tmp/worktree/bin/flow"); got != "flow" {
		t.Fatalf("preferredUIFlowBinary() = %q, want flow", got)
	}
}

func TestPreferredUIFlowBinaryIgnoresOverride(t *testing.T) {
	t.Setenv("FLOW_UI_FLOW_BIN", "/tmp/custom-flow")
	if got := preferredUIFlowBinary("/tmp/fallback-flow"); got != "flow" {
		t.Fatalf("preferredUIFlowBinary() = %q, want flow", got)
	}
}
