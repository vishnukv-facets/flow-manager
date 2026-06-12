package flowdb

import (
	"database/sql"
	"reflect"
	"testing"
)

func TestBrainPolicyDefaultsRequireApprovalForAllRiskyActions(t *testing.T) {
	db := openTempDB(t)

	policy, err := GetBrainPolicy(db)
	if err != nil {
		t.Fatalf("GetBrainPolicy: %v", err)
	}

	if !policy.FullAuto {
		t.Fatal("FullAuto = false, want true")
	}
	if len(policy.RiskyWhitelist) != 0 {
		t.Fatalf("RiskyWhitelist = %#v, want empty", policy.RiskyWhitelist)
	}
	if !reflect.DeepEqual(policy.RequiresReview, BrainRiskyActions) {
		t.Fatalf("RequiresReview = %#v, want %#v", policy.RequiresReview, BrainRiskyActions)
	}
	for _, action := range BrainRiskyActions {
		if got := policy.ActionModes[action]; got != "approval_required" {
			t.Fatalf("ActionModes[%q] = %q, want approval_required", action, got)
		}
		if policy.IsWhitelisted(action) {
			t.Fatalf("IsWhitelisted(%q) = true, want false", action)
		}
	}
}

func TestBrainPolicySetMergeAutoWhitelistsAction(t *testing.T) {
	db := openTempDB(t)

	if err := SetBrainPolicyMode(db, "merge", "auto", "2026-06-12T10:00:00+05:30"); err != nil {
		t.Fatalf("SetBrainPolicyMode: %v", err)
	}
	policy, err := GetBrainPolicy(db)
	if err != nil {
		t.Fatalf("GetBrainPolicy: %v", err)
	}

	if got := policy.ActionModes["merge"]; got != "auto" {
		t.Fatalf("ActionModes[merge] = %q, want auto", got)
	}
	if !policy.IsWhitelisted("merge") {
		t.Fatal("IsWhitelisted(merge) = false, want true")
	}
	if !reflect.DeepEqual(policy.RiskyWhitelist, []string{"merge"}) {
		t.Fatalf("RiskyWhitelist = %#v, want merge only", policy.RiskyWhitelist)
	}
	for _, action := range policy.RequiresReview {
		if action == "merge" {
			t.Fatalf("RequiresReview contains merge after auto policy: %#v", policy.RequiresReview)
		}
	}
}

func TestBrainPolicyAuditInsertListNewestFirstAndFiltersTarget(t *testing.T) {
	db := openTempDB(t)
	audits := []*BrainActionAudit{
		{
			ID:           "audit-old",
			Action:       "merge",
			TargetType:   "task",
			TargetID:     "ship",
			Actor:        "brain",
			Policy:       "approval_required",
			EvidenceJSON: `{"pr":"1"}`,
			Result:       "blocked",
			CreatedAt:    "2026-06-12T10:00:00+05:30",
		},
		{
			ID:           "audit-other",
			Action:       "deploy",
			TargetType:   "task",
			TargetID:     "other",
			Actor:        "brain",
			Policy:       "approval_required",
			EvidenceJSON: `{}`,
			Result:       "blocked",
			CreatedAt:    "2026-06-12T10:01:00+05:30",
		},
		{
			ID:           "audit-new",
			Action:       "merge",
			TargetType:   "task",
			TargetID:     "ship",
			Actor:        "operator",
			Policy:       "auto",
			EvidenceJSON: `{"pr":"2"}`,
			Result:       "allowed",
			ErrorText:    sql.NullString{String: "ignored after approval", Valid: true},
			CreatedAt:    "2026-06-12T10:02:00+05:30",
		},
	}
	for _, audit := range audits {
		if err := InsertBrainActionAudit(db, audit); err != nil {
			t.Fatalf("InsertBrainActionAudit(%s): %v", audit.ID, err)
		}
	}

	got, err := ListBrainActionAudit(db, "task", "ship", 20)
	if err != nil {
		t.Fatalf("ListBrainActionAudit: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("len(audits) = %d, want 2: %#v", len(got), got)
	}
	if got[0].ID != "audit-new" || got[1].ID != "audit-old" {
		t.Fatalf("audit order = %s, %s; want newest first", got[0].ID, got[1].ID)
	}
	if got[0].ErrorText.String != "ignored after approval" || !got[0].ErrorText.Valid {
		t.Fatalf("ErrorText round trip = %#v", got[0].ErrorText)
	}
	if got[1].EvidenceJSON != `{"pr":"1"}` {
		t.Fatalf("EvidenceJSON = %q, want old evidence", got[1].EvidenceJSON)
	}
}

func TestBrainPolicyRejectsInvalidActionAndMode(t *testing.T) {
	db := openTempDB(t)

	if err := SetBrainPolicyMode(db, "merg", "auto", "2026-06-12T10:00:00+05:30"); err == nil {
		t.Fatal("SetBrainPolicyMode invalid action returned nil error")
	}
	if err := SetBrainPolicyMode(db, "merge", "automatic", "2026-06-12T10:00:00+05:30"); err == nil {
		t.Fatal("SetBrainPolicyMode invalid mode returned nil error")
	}
}
