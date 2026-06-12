package app

import (
	"database/sql"
	"encoding/json"
	"flow/internal/flowdb"
	"fmt"
	"os"
	"strings"
	"time"
)

// ValidatorCheckResult represents the outcome of a single validation check.
type ValidatorCheckResult struct {
	Check          string `json:"check"`
	Status         string `json:"status"`           // "pass", "fail", "unknown"
	Evidence       string `json:"evidence"`          // summary of what was found
	MissingContext string `json:"missing_context"`  // if unknown, what would help
}

// ValidatorRunFindings is the structured output from a validator run.
type ValidatorRunFindings struct {
	RunID              string                   `json:"run_id"`
	TaskSlug           string                   `json:"task_slug"`
	FamilySlug         string                   `json:"family_slug"`
	WorkerRunID        string                   `json:"worker_run_id,omitempty"`
	OverallStatus      string                   `json:"overall_status"`        // "pass", "fail", "unknown"
	PassCount          int                      `json:"pass_count"`
	FailCount          int                      `json:"fail_count"`
	UnknownCount       int                      `json:"unknown_count"`
	Checks             []ValidatorCheckResult   `json:"checks"`
	RecommendedAction  string                   `json:"recommended_action"`     // e.g., "proceed to merge", "reopen for rework", "needs manual review"
	ValidatedAt        string                   `json:"validated_at"`
}

// newValidatorRunFindings creates an empty findings structure.
func newValidatorRunFindings(runID, taskSlug, familySlug, workerRunID string) *ValidatorRunFindings {
	return &ValidatorRunFindings{
		RunID:       runID,
		TaskSlug:    taskSlug,
		FamilySlug:  familySlug,
		WorkerRunID: workerRunID,
		Checks:      []ValidatorCheckResult{},
		ValidatedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// addCheck appends a validation check result.
func (f *ValidatorRunFindings) addCheck(check, status, evidence, missing string) {
	if f == nil {
		return
	}
	f.Checks = append(f.Checks, ValidatorCheckResult{
		Check:          check,
		Status:         status,
		Evidence:       evidence,
		MissingContext: missing,
	})
	switch status {
	case "pass":
		f.PassCount++
	case "fail":
		f.FailCount++
	case "unknown":
		f.UnknownCount++
	}
}

// summarize determines overall status and recommended action.
func (f *ValidatorRunFindings) summarize() {
	if f == nil {
		return
	}
	if f.FailCount > 0 {
		f.OverallStatus = "fail"
		f.RecommendedAction = "reopen for rework: one or more critical checks failed"
	} else if f.UnknownCount > 0 {
		f.OverallStatus = "unknown"
		f.RecommendedAction = "needs manual review: some evidence could not be verified automatically"
	} else if f.PassCount > 0 {
		f.OverallStatus = "pass"
		f.RecommendedAction = "proceed to steward review"
	} else {
		f.OverallStatus = "unknown"
		f.RecommendedAction = "no checks performed"
	}
}

// BriefCheck validates that the task brief exists and has required sections.
func BriefCheck(task *flowdb.Task) *ValidatorCheckResult {
	if task == nil {
		return &ValidatorCheckResult{
			Check:    "brief_exists",
			Status:   "fail",
			Evidence: "task is nil",
		}
	}
	briefPath := task.WorkDir
	if strings.TrimSpace(briefPath) != "" {
		briefPath = strings.TrimRight(briefPath, "/") + "/.flow/tasks/" + task.Slug + "/brief.md"
	}
	if briefPath == "" {
		return &ValidatorCheckResult{
			Check:    "brief_exists",
			Status:   "fail",
			Evidence: "could not determine brief path",
		}
	}
	_, err := os.Stat(briefPath)
	if err != nil {
		return &ValidatorCheckResult{
			Check:    "brief_exists",
			Status:   "fail",
			Evidence: fmt.Sprintf("brief not found at %s: %v", briefPath, err),
		}
	}
	return &ValidatorCheckResult{
		Check:    "brief_exists",
		Status:   "pass",
		Evidence: "brief file exists and is readable",
	}
}

// TaskStatusCheck validates that the task was marked done.
func TaskStatusCheck(task *flowdb.Task) *ValidatorCheckResult {
	if task == nil {
		return &ValidatorCheckResult{
			Check:    "task_marked_done",
			Status:   "fail",
			Evidence: "task is nil",
		}
	}
	if task.Status == "done" {
		return &ValidatorCheckResult{
			Check:    "task_marked_done",
			Status:   "pass",
			Evidence: "task status is 'done'",
		}
	}
	return &ValidatorCheckResult{
		Check:    "task_marked_done",
		Status:   "fail",
		Evidence: fmt.Sprintf("task status is '%s', expected 'done'", task.Status),
	}
}

// TranscriptCheck validates that the task has a session transcript.
func TranscriptCheck(task *flowdb.Task) *ValidatorCheckResult {
	check := &ValidatorCheckResult{
		Check: "has_transcript",
	}
	if task == nil {
		check.Status = "fail"
		check.Evidence = "task is nil"
		return check
	}
	if !task.SessionID.Valid || strings.TrimSpace(task.SessionID.String) == "" {
		check.Status = "unknown"
		check.Evidence = "no session_id recorded"
		check.MissingContext = "task must have been opened with 'flow do' to have a transcript"
		return check
	}
	check.Status = "pass"
	check.Evidence = fmt.Sprintf("session_id: %s", task.SessionID.String)
	return check
}

// SessionProviderCheck validates that the task has a valid session provider.
func SessionProviderCheck(task *flowdb.Task) *ValidatorCheckResult {
	check := &ValidatorCheckResult{
		Check: "valid_session_provider",
	}
	if task == nil {
		check.Status = "fail"
		check.Evidence = "task is nil"
		return check
	}
	provider := strings.TrimSpace(task.SessionProvider)
	if provider == "" {
		check.Status = "unknown"
		check.Evidence = "session provider not set"
		check.MissingContext = "task should have session_provider='claude' or 'codex'"
		return check
	}
	if provider != "claude" && provider != "codex" {
		check.Status = "fail"
		check.Evidence = fmt.Sprintf("invalid provider: %q", provider)
		return check
	}
	check.Status = "pass"
	check.Evidence = fmt.Sprintf("provider is '%s'", provider)
	return check
}

// GitDiffCheck validates that the task modified code (has a git diff).
func GitDiffCheck(task *flowdb.Task) *ValidatorCheckResult {
	check := &ValidatorCheckResult{
		Check: "git_diff_exists",
	}
	if task == nil {
		check.Status = "unknown"
		check.Evidence = "cannot check git diff without task"
		return check
	}
	// This check would need git access and the task's work_dir
	// For now, return unknown since we need external process execution
	check.Status = "unknown"
	check.Evidence = "git diff check requires external git process"
	check.MissingContext = "would need to run: cd <work_dir> && git diff HEAD"
	return check
}

// UpdatesFileCheck validates that task updates exist (work was logged).
func UpdatesFileCheck(updatesPath string) *ValidatorCheckResult {
	check := &ValidatorCheckResult{
		Check: "updates_logged",
	}
	if strings.TrimSpace(updatesPath) == "" {
		check.Status = "unknown"
		check.Evidence = "updates path not provided"
		check.MissingContext = "task should have updates/*.md files in work_dir"
		return check
	}
	_, err := os.Stat(updatesPath)
	if err != nil {
		check.Status = "unknown"
		check.Evidence = "could not access updates directory"
		check.MissingContext = fmt.Sprintf("expected updates at: %s", updatesPath)
		return check
	}
	// Check if directory is empty
	entries, err := os.ReadDir(updatesPath)
	if err != nil || len(entries) == 0 {
		check.Status = "unknown"
		check.Evidence = "no updates logged in updates directory"
		check.MissingContext = "task should have at least one update file documenting progress"
		return check
	}
	check.Status = "pass"
	check.Evidence = fmt.Sprintf("%d update file(s) found", len(entries))
	return check
}

// TestResultsCheck validates that tests pass (if present).
func TestResultsCheck(task *flowdb.Task) *ValidatorCheckResult {
	check := &ValidatorCheckResult{
		Check: "tests_pass",
	}
	if task == nil {
		check.Status = "unknown"
		check.Evidence = "cannot check tests without task"
		check.MissingContext = "task should have run tests before marking done"
		return check
	}
	// This would require running tests which is external
	check.Status = "unknown"
	check.Evidence = "test execution requires external process"
	check.MissingContext = "would run: make test or go test ./..."
	return check
}

// PRMetadataCheck validates PR metadata if present.
func PRMetadataCheck(task *flowdb.Task) *ValidatorCheckResult {
	check := &ValidatorCheckResult{
		Check: "pr_metadata",
	}
	if task == nil {
		check.Status = "unknown"
		check.Evidence = "cannot check PR metadata without task"
		check.MissingContext = "task must be loaded from database"
		return check
	}
	// For now, this check returns unknown since we can't determine PR state
	// from the task model alone. Future implementation should check
	// via GitHub API or task tags.
	check.Status = "unknown"
	check.Evidence = "PR metadata check requires external GitHub API"
	check.MissingContext = "would check linked PR status via GitHub API or gh-pr tag"
	return check
}

// RunValidationChecks performs a standard set of validation checks on a task.
func RunValidationChecks(task *flowdb.Task, db *sql.DB) *ValidatorRunFindings {
	if task == nil {
		findings := newValidatorRunFindings("", "", "", "")
		findings.OverallStatus = "fail"
		findings.RecommendedAction = "task is nil"
		return findings
	}
	familySlug := task.Slug
	if db != nil {
		if root, err := flowdb.TaskFamilyRoot(db, task.Slug); err == nil {
			familySlug = root
		}
	}
	findings := newValidatorRunFindings("", task.Slug, familySlug, "")
	checks := []struct {
		name string
		fn   func(*flowdb.Task) *ValidatorCheckResult
	}{
		{"brief exists", BriefCheck},
		{"task status", TaskStatusCheck},
		{"transcript", TranscriptCheck},
		{"session provider", SessionProviderCheck},
		{"git diff", GitDiffCheck},
		{"test results", TestResultsCheck},
		{"pr metadata", PRMetadataCheck},
	}
	for _, c := range checks {
		result := c.fn(task)
		if result != nil {
			findings.addCheck(result.Check, result.Status, result.Evidence, result.MissingContext)
		}
	}
	// Check for updates directory if work_dir is available
	if task.WorkDir != "" {
		updatesPath := strings.TrimRight(task.WorkDir, "/") + "/.flow/tasks/" + task.Slug + "/updates"
		result := UpdatesFileCheck(updatesPath)
		if result != nil {
			findings.addCheck(result.Check, result.Status, result.Evidence, result.MissingContext)
		}
	}
	findings.summarize()
	return findings
}

// ValidatorRunOutput records the structured findings from a validator run.
func ValidatorRunOutput(run *flowdb.BrainRun, findings *ValidatorRunFindings) (sql.NullString, sql.NullString, sql.NullString) {
	if run == nil || findings == nil {
		return sql.NullString{}, sql.NullString{}, sql.NullString{}
	}
	findings.RunID = run.RunID
	output := map[string]any{
		"run_id":       run.RunID,
		"task_slug":    findings.TaskSlug,
		"family_slug":  findings.FamilySlug,
		"role":         "validator",
		"status":       findings.OverallStatus,
		"check_count":  len(findings.Checks),
		"pass_count":   findings.PassCount,
		"fail_count":   findings.FailCount,
		"unknown_count": findings.UnknownCount,
	}
	outputJSON, _ := json.Marshal(output)
	evidenceJSON, _ := json.Marshal(findings)
	errText := sql.NullString{}
	if findings.FailCount > 0 || findings.OverallStatus == "fail" {
		errText = sql.NullString{String: "validation failed: " + findings.RecommendedAction, Valid: true}
	}
	return sql.NullString{String: string(outputJSON), Valid: true}, sql.NullString{String: string(evidenceJSON), Valid: true}, errText
}

// BuildValidatorBootstrapPrompt constructs the prompt for a validator run.
// The validator should read the task, check completion against the brief, and record findings.
func BuildValidatorBootstrapPrompt(taskSlug, workerRunID string) string {
	return fmt.Sprintf(
		"You are a validator agent for flow task %q. A worker has completed this task and marked it done.\n\n"+
			"Your job: validate whether the worker actually satisfied the task's brief and acceptance criteria.\n\n"+
			"Steps:\n"+
			"1. Load the flow skill via the Skill tool.\n"+
			"2. Run: flow show task %s\n"+
			"   Read the brief.md file path from the output.\n"+
			"3. Read brief.md carefully. Look for:\n"+
			"   - What: the stated problem\n"+
			"   - Why: motivation\n"+
			"   - Done when: acceptance criteria (bullets)\n"+
			"   - Out of scope: constraints\n\n"+
			"4. Read the task updates in flow's output to see what progress was made.\n"+
			"5. If a session_id is available, run: flow transcript %s --compact\n"+
			"   Skim the conversation for evidence of:\n"+
			"   - Whether each 'Done when' criterion was addressed\n"+
			"   - Any rework or issues encountered\n"+
			"   - Clarity on whether acceptance criteria are met\n\n"+
			"6. If linked PR metadata is available in flow show task, note it.\n\n"+
			"7. Based on your review, render a structured JSON validation result:\n"+
			"{\n"+
			"  \"brief_coverage\": \"pass|fail|unknown\",\n"+
			"  \"acceptance_criteria\": \"pass|fail|unknown\",\n"+
			"  \"implementation_quality\": \"pass|fail|unknown\",\n"+
			"  \"evidence\": \"<brief summary of what you found>\",\n"+
			"  \"issues\": [\"<issue 1 if any>\"],\n"+
			"  \"recommendation\": \"proceed_to_merge|needs_rework|needs_manual_review\"\n"+
			"}\n\n"+
			"8. Do not output chat. Output ONLY the JSON above, nothing else.\n",
		taskSlug, taskSlug, taskSlug,
	)
}

// cmdValidatorExec runs a validator for a completed task.
// Hidden CLI subcommand: flow __validator-exec <task-slug> [options]
func cmdValidatorExec(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: __validator-exec requires a task slug")
		return 2
	}
	slug := args[0]
	fs := flagSet("__validator-exec")
	runIDFlag := fs.String("run-id", "", "brain validator run id")
	workerRunIDFlag := fs.String("worker-run-id", "", "brain worker run id to validate")
	providerFlag := fs.String("provider", "", "session provider: claude or codex")
	permissionModeFlag := fs.String("permission-mode", "", "agent permission mode: default|auto|bypass")
	modelFlag := fs.String("model", "", "resolved session model")
	brainPlanFlag := fs.String("brain-plan", "", "brain plan id for scheduler attribution")
	_ = fs.String("brain-initiated-by", "", "brain scheduler initiator")
	if err := fs.Parse(args[1:]); err != nil {
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

	task, err := flowdb.GetTask(db, slug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load task %q: %v\n", slug, err)
		return 1
	}

	// Generate or use provided validator run id
	runID := strings.TrimSpace(*runIDFlag)
	if runID == "" {
		id, idErr := newUUID()
		if idErr != nil {
			fmt.Fprintf(os.Stderr, "error: generate run id: %v\n", idErr)
			return 1
		}
		runID = id
	}

	// Determine provider
	provider := task.SessionProvider
	if provider == "" {
		provider = sessionProviderClaude
	}
	if *providerFlag != "" {
		var perr error
		provider, perr = flowdb.NormalizeSessionProvider(*providerFlag)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", perr)
			return 2
		}
	}

	// Determine permission mode
	permissionMode := task.PermissionMode
	if permissionMode == "" {
		permissionMode = flowdb.DefaultPermissionMode
	}
	if *permissionModeFlag != "" {
		var perr error
		permissionMode, perr = flowdb.NormalizePermissionMode(*permissionModeFlag)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", perr)
			return 2
		}
	}

	// Determine model
	model := flowdb.NormalizeModel(*modelFlag)

	// Record the validator run
	now := flowdb.NowISO()
	familySlug := task.Slug
	if rootSlug, ferr := flowdb.TaskFamilyRoot(db, task.Slug); ferr == nil {
		familySlug = rootSlug
	}

	run := &flowdb.BrainRun{
		RunID:          runID,
		FamilySlug:     familySlug,
		TaskSlug:       task.Slug,
		Role:           "validator",
		Provider:       provider,
		PermissionMode: permissionMode,
		Status:         "queued",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if *brainPlanFlag != "" {
		run.PlanID = sql.NullString{String: *brainPlanFlag, Valid: true}
	}
	if *workerRunIDFlag != "" {
		run.InputSummary = sql.NullString{String: fmt.Sprintf("validating worker run %s", *workerRunIDFlag), Valid: true}
	} else {
		run.InputSummary = sql.NullString{String: "validating completed task", Valid: true}
	}
	if model != "" {
		run.ResolvedModel = sql.NullString{String: model, Valid: true}
	}

	if err := flowdb.UpsertBrainRun(db, run); err != nil {
		fmt.Fprintf(os.Stderr, "warning: record validator run: %v\n", err)
	}

	// Run the validator checks
	findings := RunValidationChecks(task, db)
	findings.RunID = runID
	findings.FamilySlug = familySlug
	if *workerRunIDFlag != "" {
		findings.WorkerRunID = *workerRunIDFlag
	}
	findings.summarize()

	// Record the findings
	run.Status = "completed"
	run.FinishedAt = sql.NullString{String: now, Valid: true}
	outJSON, evidenceJSON, errText := ValidatorRunOutput(run, findings)
	run.OutputJSON = outJSON
	run.EvidenceJSON = evidenceJSON
	run.ErrorText = errText
	run.UpdatedAt = now

	if err := flowdb.UpsertBrainRun(db, run); err != nil {
		fmt.Fprintf(os.Stderr, "error: update validator run with findings: %v\n", err)
		return 1
	}

	// Output findings
	if evidenceJSON.Valid {
		fmt.Println(evidenceJSON.String)
	}

	// Return appropriate exit code based on validation result
	if findings.FailCount > 0 || findings.OverallStatus == "fail" {
		return 1
	}
	return 0
}
