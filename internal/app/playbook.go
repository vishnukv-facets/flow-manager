package app

import (
	"database/sql"
	"errors"
	"flow/internal/flowdb"
	"flow/internal/schedule"
	"flow/internal/workdirreg"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const playbookBriefStub = `# %s

## What
*Fill in: one sentence describing what each run does.*

## Why
*Fill in: why this playbook exists.*

## Where
work_dir: %s

## Each run does
- *Fill in: steps that every invocation performs.*

## Out of scope
- *Fill in non-goals.*

## Signals to watch for
- *Fill in: signals that should change behavior or escalate.*

---
*Run with ` + "`flow run playbook %s`" + `. Each run gets its own session
and a snapshot of this brief at run time. Editing this file does not
retroactively change past runs.*
`

// addPlaybook implements `flow add playbook "<name>" [flags]`.
func addPlaybook(args []string) int {
	fs := flagSet("add playbook")
	slugFlag := fs.String("slug", "", "short user-chosen slug (default: auto-generated from name)")
	project := fs.String("project", "", "parent project slug (optional)")
	workDir := fs.String("work-dir", "", "absolute path to the playbook's work directory (required)")
	mkdir := fs.Bool("mkdir", false, "create --work-dir if it does not exist")
	scheduleSpec := fs.String("schedule", "", "recurring schedule: English (\"every 6 hours\", \"Wednesday at 1pm\") or cron; scheduled runs fire in --auto mode")
	if leadingHelpArg(args) {
		fs.Usage()
		return 0
	}
	if len(args) == 0 || args[0] == "" {
		fmt.Fprintln(os.Stderr, "error: add playbook requires a name")
		return 2
	}
	name := args[0]
	if handled, rc := parseFlagSet(fs, args[1:]); handled {
		return rc
	}

	if *workDir == "" {
		fmt.Fprintln(os.Stderr, "error: --work-dir is required for playbooks")
		return 2
	}

	// Parse the schedule up-front so a bad expression fails before we create
	// anything on disk or in the DB.
	var sched schedule.Spec
	if *scheduleSpec != "" {
		s, err := schedule.Parse(*scheduleSpec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 2
		}
		sched = s
	}
	abs, err := resolveWorkDir(*workDir, *mkdir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	dbPath, err := flowDBPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	db, err := flowdb.OpenDB(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer db.Close()

	// Resolve project if supplied.
	var projectSlug sql.NullString
	if *project != "" {
		p, err := flowdb.GetProject(db, *project)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				fmt.Fprintf(os.Stderr, "error: project %q not found\n", *project)
				return 1
			}
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		projectSlug = sql.NullString{String: p.Slug, Valid: true}
	}

	var slug string
	if *slugFlag != "" {
		slug = *slugFlag
	} else {
		baseSlug, err := Slugify(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 2
		}
		slug, err = uniqueSlug(db, "playbooks", baseSlug)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
	}

	pb := &flowdb.Playbook{
		Slug:        slug,
		Name:        name,
		ProjectSlug: projectSlug,
		WorkDir:     abs,
	}
	if err := flowdb.UpsertPlaybook(db, pb); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	if sched.Cron != "" {
		next, err := schedule.Next(sched.Cron, time.Now())
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: compute next fire: %v\n", err)
			return 1
		}
		if err := flowdb.SetPlaybookSchedule(db, slug, sched.Cron, sched.Input, next.Format(time.RFC3339)); err != nil {
			fmt.Fprintf(os.Stderr, "error: set schedule: %v\n", err)
			return 1
		}
	}

	root, err := flowRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	pbDir := filepath.Join(root, "playbooks", slug)
	if err := os.MkdirAll(filepath.Join(pbDir, "updates"), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	briefPath := filepath.Join(pbDir, "brief.md")
	if _, err := os.Stat(briefPath); os.IsNotExist(err) {
		stub := fmt.Sprintf(playbookBriefStub, name, abs, slug)
		if err := os.WriteFile(briefPath, []byte(stub), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
	}

	if err := workdirreg.Register(db, abs, "", ""); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	fmt.Printf("Created playbook %q at %s\n", slug, pbDir)
	fmt.Printf("Brief: %s\n", briefPath)
	if projectSlug.Valid {
		fmt.Printf("Project: %s\n", projectSlug.String)
	}
	if sched.Cron != "" {
		fmt.Printf("Schedule: %s (runs fire automatically in --auto mode)\n", sched.Input)
	}
	fmt.Printf("Next: flow run playbook %s\n", slug)
	return 0
}
