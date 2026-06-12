package app

import (
	"database/sql"
	"flow/internal/flowdb"
	"testing"
)

func TestBriefCheck(t *testing.T) {
	tests := []struct {
		name        string
		task        *flowdb.Task
		expectPass  bool
		description string
	}{
		{
			name:        "nil task",
			task:        nil,
			expectPass:  false,
			description: "should fail when task is nil",
		},
		{
			name: "task without brief",
			task: &flowdb.Task{
				Slug:    "test-task",
				WorkDir: "/tmp",
				Status:  "done",
			},
			expectPass:  false,
			description: "should fail when brief does not exist",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BriefCheck(tt.task)
			if result == nil {
				t.Fatal("expected result, got nil")
			}
			hasPassed := result.Status == "pass"
			if hasPassed != tt.expectPass {
				t.Errorf("BriefCheck status = %q, expected pass=%v; evidence=%s", result.Status, tt.expectPass, result.Evidence)
			}
		})
	}
}

func TestTaskStatusCheck(t *testing.T) {
	tests := []struct {
		name       string
		task       *flowdb.Task
		expectPass bool
	}{
		{
			name:       "nil task",
			task:       nil,
			expectPass: false,
		},
		{
			name: "task marked done",
			task: &flowdb.Task{
				Slug:   "test-task",
				Status: "done",
			},
			expectPass: true,
		},
		{
			name: "task in progress",
			task: &flowdb.Task{
				Slug:   "test-task",
				Status: "in-progress",
			},
			expectPass: false,
		},
		{
			name: "task in backlog",
			task: &flowdb.Task{
				Slug:   "test-task",
				Status: "backlog",
			},
			expectPass: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TaskStatusCheck(tt.task)
			if result == nil {
				t.Fatal("expected result, got nil")
			}
			hasPassed := result.Status == "pass"
			if hasPassed != tt.expectPass {
				t.Errorf("TaskStatusCheck status = %q, expected pass=%v", result.Status, tt.expectPass)
			}
		})
	}
}

func TestTranscriptCheck(t *testing.T) {
	tests := []struct {
		name         string
		task         *flowdb.Task
		expectStatus string
		description  string
	}{
		{
			name:         "nil task",
			task:         nil,
			expectStatus: "fail",
			description:  "should fail when task is nil",
		},
		{
			name: "task without session",
			task: &flowdb.Task{
				Slug:   "test-task",
				Status: "done",
			},
			expectStatus: "unknown",
			description:  "should return unknown when no session_id",
		},
		{
			name: "task with session",
			task: &flowdb.Task{
				Slug:      "test-task",
				Status:    "done",
				SessionID: sql.NullString{String: "uuid-123", Valid: true},
			},
			expectStatus: "pass",
			description:  "should pass when session_id is set",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TranscriptCheck(tt.task)
			if result == nil {
				t.Fatal("expected result, got nil")
			}
			if result.Status != tt.expectStatus {
				t.Errorf("TranscriptCheck status = %q, expected %q; %s", result.Status, tt.expectStatus, tt.description)
			}
		})
	}
}

func TestSessionProviderCheck(t *testing.T) {
	tests := []struct {
		name         string
		task         *flowdb.Task
		expectStatus string
	}{
		{
			name:         "nil task",
			task:         nil,
			expectStatus: "fail",
		},
		{
			name: "no provider",
			task: &flowdb.Task{
				Slug:            "test-task",
				SessionProvider: "",
			},
			expectStatus: "unknown",
		},
		{
			name: "claude provider",
			task: &flowdb.Task{
				Slug:            "test-task",
				SessionProvider: "claude",
			},
			expectStatus: "pass",
		},
		{
			name: "codex provider",
			task: &flowdb.Task{
				Slug:            "test-task",
				SessionProvider: "codex",
			},
			expectStatus: "pass",
		},
		{
			name: "invalid provider",
			task: &flowdb.Task{
				Slug:            "test-task",
				SessionProvider: "invalid",
			},
			expectStatus: "fail",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SessionProviderCheck(tt.task)
			if result == nil {
				t.Fatal("expected result, got nil")
			}
			if result.Status != tt.expectStatus {
				t.Errorf("SessionProviderCheck status = %q, expected %q", result.Status, tt.expectStatus)
			}
		})
	}
}

func TestValidatorRunFindings(t *testing.T) {
	t.Run("new findings", func(t *testing.T) {
		findings := newValidatorRunFindings("run-1", "task-1", "family-1", "worker-1")
		if findings.RunID != "run-1" || findings.TaskSlug != "task-1" || findings.FamilySlug != "family-1" {
			t.Fatal("unexpected findings state")
		}
		if len(findings.Checks) != 0 {
			t.Errorf("expected empty checks, got %d", len(findings.Checks))
		}
	})

	t.Run("add checks and summarize", func(t *testing.T) {
		findings := newValidatorRunFindings("run-1", "task-1", "family-1", "")
		findings.addCheck("check1", "pass", "evidence1", "")
		findings.addCheck("check2", "fail", "evidence2", "")
		findings.addCheck("check3", "unknown", "evidence3", "more context needed")
		findings.summarize()

		if findings.PassCount != 1 {
			t.Errorf("PassCount = %d, expected 1", findings.PassCount)
		}
		if findings.FailCount != 1 {
			t.Errorf("FailCount = %d, expected 1", findings.FailCount)
		}
		if findings.UnknownCount != 1 {
			t.Errorf("UnknownCount = %d, expected 1", findings.UnknownCount)
		}
		if findings.OverallStatus != "fail" {
			t.Errorf("OverallStatus = %q, expected 'fail'", findings.OverallStatus)
		}
	})

	t.Run("summarize with no failures", func(t *testing.T) {
		findings := newValidatorRunFindings("run-1", "task-1", "family-1", "")
		findings.addCheck("check1", "pass", "evidence1", "")
		findings.addCheck("check2", "pass", "evidence2", "")
		findings.summarize()

		if findings.OverallStatus != "pass" {
			t.Errorf("OverallStatus = %q, expected 'pass'", findings.OverallStatus)
		}
	})

	t.Run("summarize with unknown", func(t *testing.T) {
		findings := newValidatorRunFindings("run-1", "task-1", "family-1", "")
		findings.addCheck("check1", "pass", "evidence1", "")
		findings.addCheck("check2", "unknown", "evidence2", "context needed")
		findings.summarize()

		if findings.OverallStatus != "unknown" {
			t.Errorf("OverallStatus = %q, expected 'unknown'", findings.OverallStatus)
		}
	})
}

func TestValidatorRunOutput(t *testing.T) {
	t.Run("nil inputs", func(t *testing.T) {
		o, e, err := ValidatorRunOutput(nil, nil)
		if o.Valid || e.Valid || err.Valid {
			t.Fatal("expected all nil, got non-nil output")
		}
	})

	t.Run("pass findings", func(t *testing.T) {
		run := &flowdb.BrainRun{
			RunID:      "run-1",
			TaskSlug:   "task-1",
			FamilySlug: "family-1",
		}
		findings := newValidatorRunFindings("run-1", "task-1", "family-1", "")
		findings.addCheck("check1", "pass", "evidence1", "")
		findings.summarize()

		o, e, errText := ValidatorRunOutput(run, findings)
		if !o.Valid || !e.Valid {
			t.Fatal("expected valid output and evidence")
		}
		if errText.Valid {
			t.Errorf("expected no error text for passing validation, got %q", errText.String)
		}
	})

	t.Run("fail findings", func(t *testing.T) {
		run := &flowdb.BrainRun{
			RunID:      "run-1",
			TaskSlug:   "task-1",
			FamilySlug: "family-1",
		}
		findings := newValidatorRunFindings("run-1", "task-1", "family-1", "")
		findings.addCheck("check1", "fail", "evidence1", "")
		findings.summarize()

		o, e, errText := ValidatorRunOutput(run, findings)
		if !o.Valid || !e.Valid {
			t.Fatal("expected valid output and evidence")
		}
		if !errText.Valid {
			t.Fatal("expected error text for failing validation")
		}
	})
}
