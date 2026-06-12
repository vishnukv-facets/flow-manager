package flowdb

import (
	"database/sql"
	"testing"
)

func TestOwnerCRUDAndState(t *testing.T) {
	db := openTempDB(t)
	if err := CreateOwner(db, &Owner{
		Slug:    "release-owner",
		Name:    "Release owner",
		WorkDir: t.TempDir(),
		Every:   "24h",
		Harness: "claude",
	}); err != nil {
		t.Fatalf("CreateOwner: %v", err)
	}
	o, err := GetOwner(db, "release-owner")
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	if o.Status != "active" || o.Harness != "claude" {
		t.Fatalf("owner = %+v", o)
	}
	if err := PauseOwner(db, o.Slug); err != nil {
		t.Fatalf("PauseOwner: %v", err)
	}
	o, _ = GetOwner(db, o.Slug)
	if o.Status != "paused" {
		t.Fatalf("status after pause = %q, want paused", o.Status)
	}
	if err := ActivateOwner(db, o.Slug, "2026-06-11T10:00:00Z"); err != nil {
		t.Fatalf("ActivateOwner: %v", err)
	}
	owners, err := ListOwners(db, OwnerFilter{Status: "active"})
	if err != nil {
		t.Fatalf("ListOwners: %v", err)
	}
	if len(owners) != 1 || owners[0].Slug != o.Slug {
		t.Fatalf("owners = %+v", owners)
	}
}

func TestDueOwnersParsesTimes(t *testing.T) {
	db := openTempDB(t)
	must := func(o *Owner) {
		t.Helper()
		if err := CreateOwner(db, o); err != nil {
			t.Fatalf("CreateOwner(%s): %v", o.Slug, err)
		}
	}
	must(&Owner{Slug: "due", Name: "Due", WorkDir: t.TempDir(), Every: "1h", NextWakeAt: sql.NullString{String: "2026-06-11T09:00:00Z", Valid: true}})
	must(&Owner{Slug: "future", Name: "Future", WorkDir: t.TempDir(), Every: "1h", NextWakeAt: sql.NullString{String: "2026-06-11T11:00:00Z", Valid: true}})
	due, err := DueOwners(db, "2026-06-11T10:00:00Z")
	if err != nil {
		t.Fatalf("DueOwners: %v", err)
	}
	if len(due) != 1 || due[0].Slug != "due" {
		t.Fatalf("due owners = %+v", due)
	}
}
