package app

import (
	"database/sql"
	"errors"
	"flow/internal/flowdb"
	"flow/internal/workdirreg"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const ownerCharterStub = `# %s - charter

This owner's operating manual. Edit freely.

## Owns
- The outcome this owner is responsible for.

## Each tick
- Observe current state.
- Act when something is off target.
- Ask by creating or tagging a task when a human decision is needed.
`

func cmdOwner(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: owner requires a subcommand (list, show, start, pause, tick, tick-due, next, retire)")
		return 2
	}
	switch args[0] {
	case "list":
		return ownerList(args[1:])
	case "show":
		return ownerShow(args[1:])
	case "start":
		return ownerStart(args[1:])
	case "pause":
		return ownerPause(args[1:])
	case "tick":
		return ownerTickManual(args[1:])
	case "tick-due":
		return ownerTickDue(args[1:])
	case "next":
		return ownerNext(args[1:])
	case "retire":
		return ownerRetire(args[1:])
	}
	fmt.Fprintf(os.Stderr, "error: unknown owner subcommand %q\n", args[0])
	return 2
}

func addOwner(args []string) int {
	if len(args) == 0 || args[0] == "" {
		fmt.Fprintln(os.Stderr, "error: add owner requires a name")
		return 2
	}
	name := args[0]
	fs := flagSet("add owner")
	slugFlag := fs.String("slug", "", "short user-chosen slug")
	workDir := fs.String("work-dir", "", "absolute path to the owner's work directory")
	project := fs.String("project", "", "parent project slug")
	every := fs.String("every", "24h", "fallback tick interval, e.g. 1h or 24h")
	mkdir := fs.Bool("mkdir", false, "create --work-dir if it does not exist")
	agentFlag := fs.String("agent", "claude", "owner harness: claude or codex")
	if handled, rc := parseFlagSet(fs, args[1:]); handled {
		return rc
	}
	if *workDir == "" {
		fmt.Fprintln(os.Stderr, "error: --work-dir is required for owners")
		return 2
	}
	if _, err := time.ParseDuration(*every); err != nil {
		fmt.Fprintf(os.Stderr, "error: --every %q is not a valid duration: %v\n", *every, err)
		return 2
	}
	harnessName, err := harnessNameForProvider(*agentFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
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

	var slug string
	if *slugFlag != "" {
		slug = *slugFlag
		if _, err := flowdb.GetOwner(db, slug); err == nil {
			fmt.Fprintf(os.Stderr, "error: owner slug %q already exists\n", slug)
			return 1
		} else if !errors.Is(err, sql.ErrNoRows) {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
	} else {
		baseSlug, err := Slugify(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 2
		}
		slug, err = uniqueSlug(db, "owners", baseSlug)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
	}

	var projectSlug sql.NullString
	if *project != "" {
		p, err := flowdb.GetProject(db, *project)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: project %q: %v\n", *project, err)
			return 1
		}
		projectSlug = sql.NullString{String: p.Slug, Valid: true}
	}
	if err := flowdb.CreateOwner(db, &flowdb.Owner{
		Slug:        slug,
		Name:        name,
		WorkDir:     abs,
		ProjectSlug: projectSlug,
		Every:       *every,
		Harness:     harnessName,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "error: create owner: %v\n", err)
		return 1
	}
	root, err := flowRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	ownerDir := filepath.Join(root, "owners", slug)
	if err := os.MkdirAll(filepath.Join(ownerDir, "updates"), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	charterPath := filepath.Join(ownerDir, "charter.md")
	if _, err := os.Stat(charterPath); os.IsNotExist(err) {
		if err := os.WriteFile(charterPath, []byte(fmt.Sprintf(ownerCharterStub, name)), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
	}
	if err := workdirreg.Register(db, abs, "", ""); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("Created owner %q at %s\n", slug, ownerDir)
	return 0
}

func loadOwnerArg(args []string, name string) (*flowdb.Owner, *sql.DB, int) {
	if len(args) < 1 || args[0] == "" {
		fmt.Fprintf(os.Stderr, "error: %s requires an owner slug\n", name)
		return nil, nil, 2
	}
	dbPath, err := flowDBPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return nil, nil, 1
	}
	db, err := flowdb.OpenDB(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return nil, nil, 1
	}
	o, err := flowdb.GetOwner(db, args[0])
	if err != nil {
		db.Close()
		fmt.Fprintf(os.Stderr, "error: owner %q: %v\n", args[0], err)
		return nil, nil, 1
	}
	return o, db, 0
}

func ownerList(args []string) int {
	fs := flagSet("owner list")
	includeArchived := fs.Bool("include-archived", false, "include archived owners")
	statusFilter := fs.String("status", "", "active|paused|retired")
	if err := fs.Parse(args); err != nil {
		return 2
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
	owners, err := flowdb.ListOwners(db, flowdb.OwnerFilter{Status: *statusFilter, IncludeArchived: *includeArchived})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if len(owners) == 0 {
		fmt.Println(`No owners. Create one with: flow add owner "<name>" --work-dir <path>`)
		return 0
	}
	now := time.Now()
	fmt.Printf("%-24s %-8s %-7s %-8s %s\n", "SLUG", "STATUS", "EVERY", "HARNESS", "NEXT")
	for _, o := range owners {
		fmt.Printf("%-24s %-8s %-7s %-8s %s\n", o.Slug, o.Status, o.Every, o.Harness, ownerNextTickLabel(o, now))
	}
	return 0
}

func ownerShow(args []string) int {
	o, db, rc := loadOwnerArg(args, "owner show")
	if rc != 0 {
		return rc
	}
	defer db.Close()
	reconcileOwnerTick(db, o)
	root, _ := flowRoot()
	fmt.Printf("owner:     %s\n", o.Slug)
	fmt.Printf("name:      %s\n", o.Name)
	fmt.Printf("status:    %s\n", o.Status)
	fmt.Printf("every:     %s\n", o.Every)
	fmt.Printf("harness:   %s\n", o.Harness)
	fmt.Printf("work_dir:  %s\n", o.WorkDir)
	if o.ProjectSlug.Valid {
		fmt.Printf("project:   %s\n", o.ProjectSlug.String)
	}
	if o.TickPID.Valid {
		since := ""
		if o.TickStarted.Valid && o.TickStarted.String != "" {
			since = ", since " + o.TickStarted.String
		}
		fmt.Printf("tick:      running (pid %d%s)\n", o.TickPID.Int64, since)
	}
	fmt.Printf("next:      %s\n", ownerNextTickLabel(o, time.Now()))
	fmt.Printf("last tick: %s\n", nullStringOr(o.LastTickAt, "(never)"))
	if root != "" {
		fmt.Printf("charter:   %s\n", filepath.Join(root, "owners", o.Slug, "charter.md"))
	}
	printOwnerLedger(db, o.Slug)
	return 0
}

func ownerStart(args []string) int {
	o, db, rc := loadOwnerArg(args, "owner start")
	if rc != 0 {
		return rc
	}
	defer db.Close()
	if err := flowdb.ActivateOwner(db, o.Slug, flowdb.NowISO()); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("Started owner %q\n", o.Slug)
	return 0
}

func ownerPause(args []string) int {
	o, db, rc := loadOwnerArg(args, "owner pause")
	if rc != 0 {
		return rc
	}
	defer db.Close()
	if err := flowdb.PauseOwner(db, o.Slug); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("Paused owner %q\n", o.Slug)
	return 0
}

func ownerNext(args []string) int {
	o, db, rc := loadOwnerArg(args, "owner next")
	if rc != 0 {
		return rc
	}
	defer db.Close()
	fs := flagSet("owner next")
	in := fs.String("in", "", "duration from now, e.g. 15m")
	at := fs.String("at", "", "absolute RFC3339 time")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if (*in == "") == (*at == "") {
		fmt.Fprintln(os.Stderr, "error: give exactly one of --in or --at")
		return 2
	}
	var next time.Time
	var err error
	if *in != "" {
		var d time.Duration
		d, err = time.ParseDuration(*in)
		next = time.Now().Add(d)
	} else {
		next, err = time.Parse(time.RFC3339, *at)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}
	if !next.After(time.Now()) {
		fmt.Fprintln(os.Stderr, "error: next wake must be in the future")
		return 2
	}
	if err := flowdb.SetOwnerNextWake(db, o.Slug, next.Format(time.RFC3339)); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("owner %q next tick set to %s\n", o.Slug, next.Format(time.RFC3339))
	return 0
}

func ownerRetire(args []string) int {
	o, db, rc := loadOwnerArg(args, "owner retire")
	if rc != 0 {
		return rc
	}
	defer db.Close()
	if err := flowdb.RetireOwner(db, o.Slug); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("Retired owner %q\n", o.Slug)
	return 0
}

func ownerNextTickLabel(o *flowdb.Owner, now time.Time) string {
	switch o.Status {
	case "paused":
		return "(paused)"
	case "retired":
		return "(retired)"
	}
	if !o.NextWakeAt.Valid || o.NextWakeAt.String == "" {
		return "(not started)"
	}
	w, err := time.Parse(time.RFC3339, o.NextWakeAt.String)
	if err != nil {
		return o.NextWakeAt.String
	}
	if !w.After(now) {
		return o.NextWakeAt.String + " (due)"
	}
	return o.NextWakeAt.String
}

func nullStringOr(s sql.NullString, fallback string) string {
	if s.Valid && s.String != "" {
		return s.String
	}
	return fallback
}

func printOwnerLedger(db *sql.DB, slug string) {
	owned, err := flowdb.ListTasks(db, flowdb.TaskFilter{Tag: flowdb.NormalizeTag("owner:" + slug), Kind: ""})
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: owner ledger: %v\n", err)
		return
	}
	slugs := make([]string, 0, len(owned))
	for _, t := range owned {
		slugs = append(slugs, t.Slug)
	}
	tagsBySlug, err := flowdb.GetTaskTagsBatch(db, slugs)
	if err != nil {
		tagsBySlug = map[string][]string{}
	}
	var inflight, runs, questions []*flowdb.Task
	for _, t := range owned {
		if ownerTaskHasTag(tagsBySlug[t.Slug], "question") {
			questions = append(questions, t)
			continue
		}
		if t.Kind == "playbook_run" {
			runs = append(runs, t)
			continue
		}
		if t.Status != "done" {
			inflight = append(inflight, t)
		}
	}
	printOwnerTaskSection("in flight", inflight)
	printOwnerTaskSection("playbook runs", runs)
	printOwnerTaskSection("questions", questions)
}

func ownerTaskHasTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

func printOwnerTaskSection(label string, tasks []*flowdb.Task) {
	if len(tasks) == 0 {
		fmt.Printf("%s: (none)\n", label)
		return
	}
	fmt.Printf("%s:\n", label)
	for _, t := range tasks {
		fmt.Printf("  - %-30s [%s]\n", t.Slug, t.Status)
	}
}
