// Package app — playbook scheduler entry point.
//
// `flow playbook tick-due` is the scheduler's heartbeat: it finds playbooks
// whose recurring schedule has come due and fires each as an autonomous
// (--auto) run. It mirrors `flow owner tick-due` — a low-frequency entry
// point driven either by the in-process ticker in `flow ui serve` or by host
// cron/launchd. Firing is always headless/self-closing; the manual
// `flow run playbook` path stays interactive (visible tab).
package app

import (
	"database/sql"
	"flow/internal/flowdb"
	"flow/internal/schedule"
	"fmt"
	"os"
	"time"
)

// cmdPlaybook dispatches `flow playbook <subcommand>`.
func cmdPlaybook(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: playbook requires a subcommand (tick-due)")
		return 2
	}
	switch args[0] {
	case "tick-due":
		return playbookTickDue(args[1:])
	}
	fmt.Fprintf(os.Stderr, "error: unknown playbook subcommand %q\n", args[0])
	return 2
}

// playbookScheduleFire fires one scheduled playbook run in --auto mode and
// returns the created run slug. It is a package var so tests can stub it
// without spawning real detached supervisors.
var playbookScheduleFire = func(slug string) (string, error) {
	dbPath, err := flowDBPath()
	if err != nil {
		return "", err
	}
	db, err := openConcurrentDB(dbPath)
	if err != nil {
		return "", err
	}
	pb, err := ResolvePlaybook(db, slug, false)
	if err != nil {
		db.Close()
		return "", err
	}
	root, err := flowRoot()
	if err != nil {
		db.Close()
		return "", err
	}
	runSlug, err := createPlaybookRun(db, root, pb, "")
	db.Close() // cmdDo opens its own handle
	if err != nil {
		return "", err
	}
	if rc := cmdDo([]string{runSlug, "--auto"}); rc != 0 {
		return runSlug, fmt.Errorf("auto run for %s exited with code %d", runSlug, rc)
	}
	return runSlug, nil
}

func playbookTickDue(args []string) int {
	fs := flagSet("playbook tick-due")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dbPath, err := flowDBPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	db, err := openConcurrentDB(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer db.Close()

	due, err := flowdb.DuePlaybooks(db, flowdb.NowISO())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fired := 0
	for _, pb := range due {
		if firePlaybookSchedule(db, pb) {
			fired++
		}
	}
	fmt.Printf("fired %d scheduled playbook run(s)\n", fired)
	return 0
}

// firePlaybookSchedule fires one due playbook (or skips it on overlap),
// advancing next_fire_at either way. Returns true only when a run was
// actually launched.
func firePlaybookSchedule(db *sql.DB, pb *flowdb.Playbook) bool {
	now := time.Now()
	next, err := schedule.Next(pb.ScheduleSpec.String, now)
	if err != nil {
		// A stored spec that no longer parses can't be advanced; pause it so
		// the scheduler stops hot-looping on the broken playbook.
		fmt.Fprintf(os.Stderr, "warning: playbook %q has invalid schedule %q: %v; pausing it\n", pb.Slug, pb.ScheduleSpec.String, err)
		_ = flowdb.PausePlaybookSchedule(db, pb.Slug)
		return false
	}
	nextISO := next.Format(time.RFC3339)

	// Overlap policy: skip if the prior scheduled run is still in flight.
	if playbookRunInFlight(db, pb.Slug) {
		fmt.Printf("playbook %q: prior run still in flight; skipping this fire\n", pb.Slug)
		if err := flowdb.SetPlaybookNextFire(db, pb.Slug, nextISO); err != nil {
			fmt.Fprintf(os.Stderr, "warning: advance next fire for %q: %v\n", pb.Slug, err)
		}
		return false
	}

	runSlug, err := playbookScheduleFire(pb.Slug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: fire playbook %q: %v\n", pb.Slug, err)
		// Advance anyway so a transient failure doesn't peg the scheduler.
		if e := flowdb.SetPlaybookNextFire(db, pb.Slug, nextISO); e != nil {
			fmt.Fprintf(os.Stderr, "warning: advance next fire for %q: %v\n", pb.Slug, e)
		}
		return false
	}
	if err := flowdb.RecordPlaybookFired(db, pb.Slug, now.Format(time.RFC3339), nextISO, runSlug); err != nil {
		fmt.Fprintf(os.Stderr, "warning: record fire for %q: %v\n", pb.Slug, err)
	}
	fmt.Printf("playbook %q: fired run %s (next fire %s)\n", pb.Slug, runSlug, nextISO)
	return true
}

// playbookRunInFlight reports whether any run of the playbook still has a live
// autonomous supervisor — the signal the overlap policy keys off.
func playbookRunInFlight(db *sql.DB, playbookSlug string) bool {
	runs, err := flowdb.ListTasks(db, flowdb.TaskFilter{Kind: "playbook_run", PlaybookSlug: playbookSlug})
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: overlap check for %q: %v\n", playbookSlug, err)
		return false
	}
	for _, r := range runs {
		if r.AutoRunStatus.Valid && r.AutoRunStatus.String == "running" &&
			r.AutoRunPID.Valid && processAlive(int(r.AutoRunPID.Int64)) {
			return true
		}
	}
	return false
}
