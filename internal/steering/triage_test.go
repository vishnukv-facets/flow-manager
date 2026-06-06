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

func TestDeepTriagePromptUsesContextPackAsPrimaryInput(t *testing.T) {
	pack := ThreadContext{
		Source:      "github",
		ThreadKey:   "o/r:gh-pr:o/r#5",
		Permalink:   "https://github.com/o/r/pull/5",
		FetchStatus: "ok",
		Parent: &ContextMessage{
			Kind:   "parent",
			Author: "maintainer",
			Text:   "Please review the deploy change",
			TS:     "2026-06-05T09:00:00Z",
		},
		Messages: []ContextMessage{{
			Kind:   "comment",
			Author: "reviewer",
			Text:   "Can we add a rollback note?",
			TS:     "2026-06-05T10:00:00Z",
		}},
		Participants: []string{"maintainer", "reviewer"},
		Timestamps:   []string{"2026-06-05T09:00:00Z", "2026-06-05T10:00:00Z"},
		Summary:      "2 GitHub messages from maintainer, reviewer",
	}
	prompt := deepTriagePromptWithContext(
		ClassifyInput{ThreadKey: "o/r:gh-pr:o/r#5", Source: "github", Text: "review?"},
		"Tasks:\n(none)",
		pack,
	)
	if !strings.Contains(prompt, "Context pack (JSON):") {
		t.Fatalf("deep prompt missing context-pack section:\n%s", prompt)
	}
	if !strings.Contains(prompt, `"permalink":"https://github.com/o/r/pull/5"`) ||
		!strings.Contains(prompt, "rollback note") ||
		!strings.Contains(prompt, `"fetch_status":"ok"`) {
		t.Errorf("deep prompt did not include the structured context pack:\n%s", prompt)
	}
	if strings.Contains(prompt, "Slack MCP") || strings.Contains(prompt, "use the `gh` CLI") {
		t.Errorf("deep prompt must not primarily ask the model to fetch context itself:\n%s", prompt)
	}
}

func TestPromptsInstructUseNamesNotIDs(t *testing.T) {
	const want = "never output raw platform IDs"
	if !strings.Contains(stage1Prime(), want) {
		t.Errorf("stage1Prime must instruct the model to use names not IDs")
	}
	if !strings.Contains(stage2Prime("Tasks:\n(none)"), want) {
		t.Errorf("stage2Prime must instruct the model to use names not IDs")
	}
	if !strings.Contains(deepTriagePrompt(ClassifyInput{Source: "slack"}, "Tasks:\n(none)"), want) {
		t.Errorf("deepTriagePrompt must instruct the model to use names not IDs")
	}
}

func TestDeepTriage(t *testing.T) {
	stubDeepTriage(t, func(prompt string) (string, error) {
		if !strings.Contains(prompt, "MODE: stage3-deep") {
			t.Fatalf("deep prompt missing marker")
		}
		if !strings.Contains(prompt, "Context pack (JSON):") {
			t.Fatalf("deep prompt missing context pack:\n%s", prompt)
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
