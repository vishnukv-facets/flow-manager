// internal/steering/triage_test.go
package steering

import (
	"context"
	"strings"
	"testing"
)

func stubDeepTriage(t *testing.T, fn func(prompt string) (string, error)) {
	t.Helper()
	old := deepTriageRunner
	deepTriageRunner = func(ctx context.Context, prompt string) (string, error) { return fn(prompt) }
	t.Cleanup(func() { deepTriageRunner = old })
}

func TestDeepTriage(t *testing.T) {
	stubDeepTriage(t, func(prompt string) (string, error) {
		if !strings.Contains(prompt, "MODE: stage3-deep") {
			t.Fatalf("deep prompt missing marker")
		}
		return "```json\n" + `{"suggested_action":"reply","confidence":0.93,
		  "summary":"customer wants ETA","draft":"Targeting Friday — will confirm.",
		  "urgency":"urgent","reason":"direct question to operator"}` + "\n```", nil
	})
	v, err := DeepTriage(context.Background(), ClassifyInput{ThreadKey: "C1:9.9", Source: "slack", Text: "ETA?"}, "Tasks:\n(none)")
	if err != nil {
		t.Fatalf("DeepTriage: %v", err)
	}
	if v.SuggestedAction != ActionReply || v.Draft == "" || v.ThreadKey != "C1:9.9" {
		t.Errorf("verdict = %+v", v)
	}
}
