package app

import (
	"testing"
	"time"

	"flow/internal/flowdb"
)

// armSchedule sets a playbook's next_fire_at to a known instant (bypassing the
// future-by-default that SetPlaybookSchedule computes) so tests control dueness.
func armSchedule(t *testing.T, slug, spec, input string, nextFireAt time.Time) {
	t.Helper()
	db := openFlowDB(t)
	defer db.Close()
	if err := flowdb.SetPlaybookSchedule(db, slug, spec, input, nextFireAt.Format(time.RFC3339)); err != nil {
		t.Fatalf("arm schedule: %v", err)
	}
}

func TestPlaybookTickDueFiresDuePlaybook(t *testing.T) {
	setupFlowRoot(t)
	wd := t.TempDir()
	if rc := cmdAdd([]string{"playbook", "Digest", "--slug", "digest", "--work-dir", wd}); rc != 0 {
		t.Fatal("add playbook failed")
	}
	armSchedule(t, "digest", "@every 6h", "every 6 hours", time.Now().Add(-time.Minute))

	var firedFor []string
	orig := playbookScheduleFire
	playbookScheduleFire = func(slug string) (string, error) {
		firedFor = append(firedFor, slug)
		return slug + "--run", nil
	}
	defer func() { playbookScheduleFire = orig }()

	if rc := playbookTickDue(nil); rc != 0 {
		t.Fatalf("tick-due rc=%d", rc)
	}
	if len(firedFor) != 1 || firedFor[0] != "digest" {
		t.Fatalf("expected one fire for digest, got %v", firedFor)
	}

	db := openFlowDB(t)
	defer db.Close()
	pb, _ := flowdb.GetPlaybook(db, "digest")
	if pb.LastFireRunSlug.String != "digest--run" {
		t.Errorf("last_fire_run_slug = %q, want digest--run", pb.LastFireRunSlug.String)
	}
	// next_fire_at must have advanced into the future.
	next, err := time.Parse(time.RFC3339, pb.NextFireAt.String)
	if err != nil || !next.After(time.Now()) {
		t.Errorf("next_fire_at not advanced: %q (err %v)", pb.NextFireAt.String, err)
	}
}

func TestPlaybookTickDueSkipsNotYetDue(t *testing.T) {
	setupFlowRoot(t)
	wd := t.TempDir()
	if rc := cmdAdd([]string{"playbook", "Digest", "--slug", "digest", "--work-dir", wd}); rc != 0 {
		t.Fatal("add playbook failed")
	}
	armSchedule(t, "digest", "@every 6h", "every 6 hours", time.Now().Add(time.Hour))

	fired := false
	orig := playbookScheduleFire
	playbookScheduleFire = func(slug string) (string, error) { fired = true; return "", nil }
	defer func() { playbookScheduleFire = orig }()

	if rc := playbookTickDue(nil); rc != 0 {
		t.Fatalf("tick-due rc=%d", rc)
	}
	if fired {
		t.Error("fired a playbook whose next fire is in the future")
	}
}

func TestFirePlaybookScheduleOverlapSkips(t *testing.T) {
	setupFlowRoot(t)
	wd := t.TempDir()
	if rc := cmdAdd([]string{"playbook", "Digest", "--slug", "digest", "--work-dir", wd}); rc != 0 {
		t.Fatal("add playbook failed")
	}
	armSchedule(t, "digest", "@every 6h", "every 6 hours", time.Now().Add(-time.Minute))

	db := openFlowDB(t)
	defer db.Close()

	// Simulate a prior run still in flight: a playbook_run task with a live
	// auto-run supervisor.
	now := flowdb.NowISO()
	// status stays 'backlog' to satisfy the session-invariant CHECK (an
	// in-progress row would need a session_id); the overlap guard keys off
	// auto_run_status + a live pid, not task status.
	if _, err := db.Exec(
		`INSERT INTO tasks (slug, name, status, kind, playbook_slug, priority, work_dir, auto_run_status, auto_run_pid, created_at, updated_at)
		 VALUES ('digest--prev', 'digest--prev', 'backlog', 'playbook_run', 'digest', 'medium', ?, 'running', 424242, ?, ?)`,
		wd, now, now,
	); err != nil {
		t.Fatal(err)
	}

	origAlive := processAlive
	processAlive = func(pid int) bool { return pid == 424242 }
	defer func() { processAlive = origAlive }()

	fired := false
	origFire := playbookScheduleFire
	playbookScheduleFire = func(slug string) (string, error) { fired = true; return "", nil }
	defer func() { playbookScheduleFire = origFire }()

	pb, _ := flowdb.GetPlaybook(db, "digest")
	if firePlaybookSchedule(db, pb) {
		t.Error("firePlaybookSchedule returned true despite an in-flight run")
	}
	if fired {
		t.Error("fired despite overlap guard")
	}
	// next_fire_at should still advance so we don't hot-loop.
	pb2, _ := flowdb.GetPlaybook(db, "digest")
	next, err := time.Parse(time.RFC3339, pb2.NextFireAt.String)
	if err != nil || !next.After(time.Now()) {
		t.Errorf("overlap skip did not advance next fire: %q (err %v)", pb2.NextFireAt.String, err)
	}
}

func TestFirePlaybookScheduleAdvancesOnFireError(t *testing.T) {
	setupFlowRoot(t)
	wd := t.TempDir()
	if rc := cmdAdd([]string{"playbook", "Digest", "--slug", "digest", "--work-dir", wd}); rc != 0 {
		t.Fatal("add playbook failed")
	}
	armSchedule(t, "digest", "@every 6h", "every 6 hours", time.Now().Add(-time.Minute))

	db := openFlowDB(t)
	defer db.Close()

	origFire := playbookScheduleFire
	playbookScheduleFire = func(slug string) (string, error) { return "", errFireBoom }
	defer func() { playbookScheduleFire = origFire }()

	pb, _ := flowdb.GetPlaybook(db, "digest")
	if firePlaybookSchedule(db, pb) {
		t.Error("expected false when fire errors")
	}
	pb2, _ := flowdb.GetPlaybook(db, "digest")
	next, err := time.Parse(time.RFC3339, pb2.NextFireAt.String)
	if err != nil || !next.After(time.Now()) {
		t.Errorf("fire error did not advance next fire: %q (err %v)", pb2.NextFireAt.String, err)
	}
	if pb2.LastFireRunSlug.Valid {
		t.Error("errored fire should not stamp last_fire_run_slug")
	}
}

var errFireBoom = errBoom("boom")

type errBoom string

func (e errBoom) Error() string { return string(e) }
