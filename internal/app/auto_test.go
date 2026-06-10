package app

import (
	"database/sql"
	"flow/internal/flowdb"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// autoTestSetup initializes a temp FLOW_ROOT and returns the db path.
func autoTestSetup(t *testing.T) (flowRoot string, db *sql.DB) {
	t.Helper()
	tmp := t.TempDir()
	flowRoot = filepath.Join(tmp, "flow")
	t.Setenv("FLOW_ROOT", flowRoot)
	t.Setenv("HOME", tmp)

	if rc := cmdInit(nil); rc != 0 {
		t.Fatalf("flow init: rc=%d", rc)
	}

	var err error
	db, err = flowdb.OpenDB(filepath.Join(flowRoot, "flow.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return flowRoot, db
}

// seedAutoTask inserts a task with an in-progress status and a session_id.
func seedAutoTask(t *testing.T, db *sql.DB, slug, sessionID string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO tasks (slug, name, status, work_dir, session_provider, session_id, kind, created_at, updated_at)
		 VALUES (?, ?, 'in-progress', '/tmp', 'claude', ?, 'regular', datetime('now'), datetime('now'))`,
		slug, "test task "+slug, sessionID,
	)
	if err != nil {
		t.Fatalf("seed task %s: %v", slug, err)
	}
}

// stubAutoRunner overrides autoRunner to capture the prompt and return retErr.
func stubAutoRunner(t *testing.T, retErr error) *string {
	t.Helper()
	captured := new(string)
	old := autoRunner
	autoRunner = func(sessionID, prompt string) error {
		*captured = prompt
		return retErr
	}
	t.Cleanup(func() { autoRunner = old })
	return captured
}

// stubProcessAlive overrides processAlive to return a fixed value.
func stubProcessAlive(t *testing.T, alive bool) {
	t.Helper()
	old := processAlive
	processAlive = func(pid int) bool { return alive }
	t.Cleanup(func() { processAlive = old })
}

func TestAutoExecFinalizesCompleted(t *testing.T) {
	_, db := autoTestSetup(t)
	stubClaudeRunner(t, nil)
	seedAutoTask(t, db, "at-comp", "sess-comp-1")

	// autoRunner stub: mark task done (simulates headless run calling flow done).
	old := autoRunner
	autoRunner = func(sessionID, prompt string) error {
		if rc := cmdDone([]string{"at-comp"}); rc != 0 {
			return fmt.Errorf("cmdDone returned %d", rc)
		}
		return nil
	}
	t.Cleanup(func() { autoRunner = old })

	rc := cmdAutoExec([]string{"at-comp"})
	if rc != 0 {
		t.Fatalf("cmdAutoExec: rc=%d", rc)
	}

	task, err := flowdb.GetTask(db, "at-comp")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if !task.AutoRunStatus.Valid || task.AutoRunStatus.String != "completed" {
		t.Errorf("auto_run_status: got %q, want 'completed'", task.AutoRunStatus.String)
	}
	if task.AutoRunPID.Valid {
		t.Errorf("auto_run_pid should be NULL after finalize, got %d", task.AutoRunPID.Int64)
	}
	if !task.AutoRunFinished.Valid || task.AutoRunFinished.String == "" {
		t.Error("auto_run_finished should be set after finalize")
	}
}

func TestAutoExecFinalizesDead(t *testing.T) {
	_, db := autoTestSetup(t)
	seedAutoTask(t, db, "at-dead", "sess-dead-1")

	old := autoRunner
	autoRunner = func(sessionID, prompt string) error {
		return fmt.Errorf("claude exited with code 1")
	}
	t.Cleanup(func() { autoRunner = old })

	rc := cmdAutoExec([]string{"at-dead"})
	if rc != 1 {
		t.Fatalf("cmdAutoExec should return 1 on runner error, got %d", rc)
	}

	task, err := flowdb.GetTask(db, "at-dead")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if !task.AutoRunStatus.Valid || task.AutoRunStatus.String != "dead" {
		t.Errorf("auto_run_status: got %q, want 'dead'", task.AutoRunStatus.String)
	}
	if task.AutoRunPID.Valid {
		t.Errorf("auto_run_pid should be NULL after finalize")
	}
	if !task.AutoRunFinished.Valid || task.AutoRunFinished.String == "" {
		t.Error("auto_run_finished should be set after finalize")
	}
}

func TestAutoExecAppendsInjection(t *testing.T) {
	_, db := autoTestSetup(t)
	seedAutoTask(t, db, "at-inj", "sess-inj-1")
	stubClaudeRunner(t, nil)
	capturedPrompt := stubAutoRunner(t, nil)

	rc := cmdAutoExec([]string{"at-inj", "--with", "extra instruction here"})
	if rc != 0 {
		t.Fatalf("cmdAutoExec: rc=%d", rc)
	}

	if !strings.Contains(*capturedPrompt, withInjectionMarker) {
		t.Errorf("prompt missing withInjectionMarker; got:\n%s", *capturedPrompt)
	}
	if !strings.Contains(*capturedPrompt, "extra instruction here") {
		t.Errorf("prompt missing injected text; got:\n%s", *capturedPrompt)
	}
}

func TestReconcileAutoRunDeadPid(t *testing.T) {
	_, db := autoTestSetup(t)
	stubProcessAlive(t, false)

	_, err := db.Exec(
		`INSERT INTO tasks (slug, name, status, work_dir, session_provider, session_id, kind,
		 auto_run_status, auto_run_pid, created_at, updated_at)
		 VALUES ('at-recon', 'reconcile test', 'in-progress', '/tmp', 'claude', 'sess-recon-1', 'regular',
		 'running', 99999, datetime('now'), datetime('now'))`,
	)
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}

	task, err := flowdb.GetTask(db, "at-recon")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}

	reconcileAutoRun(db, task)

	if !task.AutoRunStatus.Valid || task.AutoRunStatus.String != "dead" {
		t.Errorf("in-memory auto_run_status: got %q, want 'dead'", task.AutoRunStatus.String)
	}
	if task.AutoRunPID.Valid {
		t.Errorf("in-memory auto_run_pid should be zero after reconcile")
	}

	reloaded, err := flowdb.GetTask(db, "at-recon")
	if err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if !reloaded.AutoRunStatus.Valid || reloaded.AutoRunStatus.String != "dead" {
		t.Errorf("db auto_run_status: got %q, want 'dead'", reloaded.AutoRunStatus.String)
	}
}

func TestReconcileAutoRunLivePidStaysRunning(t *testing.T) {
	_, db := autoTestSetup(t)

	pid := os.Getpid()
	_, err := db.Exec(
		`INSERT INTO tasks (slug, name, status, work_dir, session_provider, session_id, kind,
		 auto_run_status, auto_run_pid, created_at, updated_at)
		 VALUES ('at-live', 'live pid test', 'in-progress', '/tmp', 'claude', 'sess-live-1', 'regular',
		 'running', ?, datetime('now'), datetime('now'))`,
		pid,
	)
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}

	task, err := flowdb.GetTask(db, "at-live")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}

	reconcileAutoRun(db, task)

	if !task.AutoRunStatus.Valid || task.AutoRunStatus.String != "running" {
		t.Errorf("auto_run_status should remain 'running' for live pid, got %q", task.AutoRunStatus.String)
	}
}

func TestBuildAutoBootstrapPrompt(t *testing.T) {
	prompt := buildAutoBootstrapPrompt("my-task", "task", "")

	checks := []string{
		"my-task",
		"flow done my-task",
		"AskUserQuestion",
		"PERSIST",
		"LAST RESORT",
		"EXHAUST",
		"NO HUMAN IS WATCHING",
	}
	for _, s := range checks {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt missing %q", s)
		}
	}
}

func TestBuildAutoBootstrapPromptPlaybookRun(t *testing.T) {
	prompt := buildAutoBootstrapPrompt("run-001", "playbook_run", "my-playbook")

	if !strings.Contains(prompt, "my-playbook") {
		t.Error("playbook_run prompt should mention playbook slug")
	}
	if !strings.Contains(prompt, "frozen snapshot") {
		t.Error("playbook_run prompt should mention frozen snapshot")
	}
}

func TestAutoChildEnvStripsSessionID(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "should-be-stripped")
	t.Setenv("FLOW_ROOT", "/tmp/test-flow-root")

	env := autoChildEnv()
	for _, kv := range env {
		if strings.HasPrefix(kv, "CLAUDE_CODE_SESSION_ID=") {
			t.Errorf("CLAUDE_CODE_SESSION_ID should be stripped from child env, got %q", kv)
		}
	}
	found := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "FLOW_ROOT=") {
			found = true
			break
		}
	}
	if !found {
		t.Error("FLOW_ROOT should be present in child env via flowSessionEnv overlay")
	}
}

// ── Stage 3: cmdDo --auto integration ──────────────────────────────────────

// noTabStub pins the spawner to iTerm and stubs iterm.Runner to a no-op,
// then returns a func() that returns how many times SpawnTab was called.
func noTabStub(t *testing.T) func() int64 {
	t.Helper()
	count, _ := stubITerm(t)
	return func() int64 { return *count }
}

// stubLauncherRecord overrides autoLauncher to record its last call and return pid 4242.
type launcherCall struct {
	slug      string
	workDir   string
	logPath   string
	injection string
}

func stubLauncherRecord(t *testing.T, retErr error) *launcherCall {
	t.Helper()
	rec := &launcherCall{}
	old := autoLauncher
	autoLauncher = func(slug, workDir, logPath, injection string, env []string) (int, error) {
		rec.slug = slug
		rec.workDir = workDir
		rec.logPath = logPath
		rec.injection = injection
		if retErr != nil {
			return 0, retErr
		}
		return 4242, nil
	}
	t.Cleanup(func() { autoLauncher = old })
	return rec
}

func TestCmdDoAutoLaunchesDetached(t *testing.T) {
	setupFlowRoot(t)
	seedTask(t, "auto-det")

	const fixedSID = "auto-det-session-uuid"
	oldUUID := newUUID
	newUUID = func() (string, error) { return fixedSID, nil }
	t.Cleanup(func() { newUUID = oldUUID })

	tabCount := noTabStub(t)
	rec := stubLauncherRecord(t, nil)

	rc := cmdDo([]string{"auto-det", "--auto"})
	if rc != 0 {
		t.Fatalf("cmdDo --auto: rc=%d", rc)
	}

	// No iTerm tab should be spawned.
	if n := tabCount(); n != 0 {
		t.Errorf("SpawnTab called %d times, want 0", n)
	}

	// Launcher received the right slug.
	if rec.slug != "auto-det" {
		t.Errorf("launcher slug = %q, want 'auto-det'", rec.slug)
	}
	// Log path is under tasks/<slug>/auto-runs/.
	if !strings.Contains(rec.logPath, filepath.Join("tasks", "auto-det", "auto-runs")) {
		t.Errorf("log path %q missing expected dir", rec.logPath)
	}

	// DB: in-progress, session_id set, auto_run_status=running, pid=4242, started set.
	db := openFlowDB(t)
	task, err := flowdb.GetTask(db, "auto-det")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if task.Status != "in-progress" {
		t.Errorf("status = %q, want in-progress", task.Status)
	}
	if !task.SessionID.Valid || task.SessionID.String != fixedSID {
		t.Errorf("session_id = %q, want %q", task.SessionID.String, fixedSID)
	}
	if !task.AutoRunStatus.Valid || task.AutoRunStatus.String != "running" {
		t.Errorf("auto_run_status = %q, want running", task.AutoRunStatus.String)
	}
	if !task.AutoRunPID.Valid || task.AutoRunPID.Int64 != 4242 {
		t.Errorf("auto_run_pid = %d, want 4242", task.AutoRunPID.Int64)
	}
	if !task.AutoRunStarted.Valid || task.AutoRunStarted.String == "" {
		t.Error("auto_run_started should be set")
	}
	if task.AutoRunFinished.Valid {
		t.Error("auto_run_finished should be NULL")
	}
}

func TestCmdDoAutoRejectsHere(t *testing.T) {
	setupFlowRoot(t)
	seedTask(t, "here-conflict")
	rc := cmdDo([]string{"here-conflict", "--auto", "--here"})
	if rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
}

func TestCmdDoWithRequiresAuto(t *testing.T) {
	setupFlowRoot(t)
	seedTask(t, "with-no-auto")
	noTabStub(t)
	rc := cmdDo([]string{"with-no-auto", "--with", "some instruction"})
	if rc != 2 {
		t.Errorf("rc = %d, want 2 (--with without --auto)", rc)
	}
}

func TestCmdDoAutoCodexRefused(t *testing.T) {
	setupFlowRoot(t)
	if rc := cmdAdd([]string{"task", "codex-auto", "--agent", "codex"}); rc != 0 {
		t.Fatalf("add codex task rc=%d", rc)
	}
	noTabStub(t)
	rc := cmdDo([]string{"codex-auto", "--auto"})
	if rc != 1 {
		t.Errorf("rc = %d, want 1 (D1 codex gate)", rc)
	}
	// Task status must remain backlog, no session_id.
	db := openFlowDB(t)
	task, err := flowdb.GetTask(db, "codex-auto")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if task.Status != "backlog" {
		t.Errorf("status = %q after D1 rejection, want backlog", task.Status)
	}
	if task.SessionID.Valid && task.SessionID.String != "" {
		t.Errorf("session_id should be empty after D1 rejection, got %q", task.SessionID.String)
	}
}

func TestCmdDoAutoWithInjection(t *testing.T) {
	setupFlowRoot(t)
	seedTask(t, "with-inj")
	noTabStub(t)
	rec := stubLauncherRecord(t, nil)

	rc := cmdDo([]string{"with-inj", "--auto", "--with", "extra step: verify X"})
	if rc != 0 {
		t.Fatalf("cmdDo --auto --with: rc=%d", rc)
	}
	if rec.injection != "extra step: verify X" {
		t.Errorf("launcher injection = %q, want 'extra step: verify X'", rec.injection)
	}
}

func TestCmdDoAutoRefusesWhenAlreadyRunning(t *testing.T) {
	setupFlowRoot(t)
	seedTask(t, "already-running")
	noTabStub(t)
	stubProcessAlive(t, true)

	// Seed the task as already having a running auto run.
	db := openFlowDB(t)
	if _, err := db.Exec(
		`UPDATE tasks SET session_id='sess-ar', auto_run_status='running', auto_run_pid=9999, session_started=datetime('now'), status='in-progress' WHERE slug='already-running'`,
	); err != nil {
		t.Fatalf("seed running state: %v", err)
	}

	rc := cmdDo([]string{"already-running", "--auto"})
	if rc != 1 {
		t.Errorf("rc = %d, want 1 (already in flight)", rc)
	}

	// With --force: should succeed.
	rec := stubLauncherRecord(t, nil)
	rc = cmdDo([]string{"already-running", "--auto", "--force"})
	if rc != 0 {
		t.Errorf("--force rc = %d, want 0", rc)
	}
	if rec.slug != "already-running" {
		t.Errorf("launcher not called with right slug under --force")
	}
}

func TestCmdDoAutoLaunchFailureRollsBack(t *testing.T) {
	setupFlowRoot(t)
	seedTask(t, "launch-fail")
	noTabStub(t)
	stubLauncherRecord(t, fmt.Errorf("supervisor spawn error"))

	oldUUID := newUUID
	newUUID = func() (string, error) { return "fail-session-uuid", nil }
	t.Cleanup(func() { newUUID = oldUUID })

	rc := cmdDo([]string{"launch-fail", "--auto"})
	if rc != 1 {
		t.Errorf("rc = %d, want 1 on launch failure", rc)
	}

	// session_id should be rolled back to NULL, status back to backlog.
	db := openFlowDB(t)
	task, err := flowdb.GetTask(db, "launch-fail")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if task.Status != "backlog" {
		t.Errorf("status = %q after rollback, want backlog", task.Status)
	}
	if task.SessionID.Valid && task.SessionID.String != "" {
		t.Errorf("session_id = %q after rollback, want NULL", task.SessionID.String)
	}
}

// ---------- Stage 4: list/show surfacing ----------

func TestListTasksAutoColumn(t *testing.T) {
	root, db := showListEditDB(t)
	insertTask(t, db, "auto-list-task", "Auto List Task", "in-progress", "medium", filepath.Join(root, "repo"), nil)
	// Directly set auto_run fields as if recordAutoRunLaunched had been called.
	_, err := db.Exec(
		`UPDATE tasks SET auto_run_status='running', auto_run_pid=55555, auto_run_started=datetime('now') WHERE slug='auto-list-task'`,
	)
	if err != nil {
		t.Fatalf("set auto_run fields: %v", err)
	}
	// PID is alive — reconcile must not flip it to dead.
	stubProcessAlive(t, true)

	out := captureStdout(t, func() {
		if rc := cmdList([]string{"tasks"}); rc != 0 {
			t.Fatalf("cmdList: rc=%d", rc)
		}
	})

	if !strings.Contains(out, "[auto]") {
		t.Errorf("list output missing [auto] marker; got:\n%s", out)
	}
	if !strings.Contains(out, "AUTO") {
		t.Errorf("list output missing AUTO header; got:\n%s", out)
	}
}

func TestListTasksAutoColumnCompleted(t *testing.T) {
	root, db := showListEditDB(t)
	insertTask(t, db, "auto-done-task", "Auto Done Task", "done", "medium", filepath.Join(root, "repo"), nil)
	_, err := db.Exec(
		`UPDATE tasks SET auto_run_status='completed', auto_run_finished=datetime('now') WHERE slug='auto-done-task'`,
	)
	if err != nil {
		t.Fatalf("set auto_run fields: %v", err)
	}

	out := captureStdout(t, func() {
		cmdList([]string{"tasks", "--status", "done"})
	})

	if !strings.Contains(out, "[done]") {
		t.Errorf("list output missing [done] marker for completed auto run; got:\n%s", out)
	}
}

func TestShowTaskAutoRunLines(t *testing.T) {
	root, db := showListEditDB(t)
	insertTask(t, db, "auto-show-task", "Auto Show Task", "in-progress", "high", filepath.Join(root, "repo"), nil)
	_, err := db.Exec(
		`UPDATE tasks SET auto_run_status='running', auto_run_pid=7777, auto_run_started=datetime('now'),
		 auto_run_log='/tmp/auto-runs/2026-06-10-120000.log' WHERE slug='auto-show-task'`,
	)
	if err != nil {
		t.Fatalf("set auto_run fields: %v", err)
	}
	stubProcessAlive(t, true)

	out := captureStdout(t, func() {
		if rc := cmdShow([]string{"task", "auto-show-task"}); rc != 0 {
			t.Fatalf("cmdShow: rc=%d", rc)
		}
	})

	if !strings.Contains(out, "auto_run:") {
		t.Errorf("show output missing auto_run: line; got:\n%s", out)
	}
	if !strings.Contains(out, "7777") {
		t.Errorf("show output missing pid 7777; got:\n%s", out)
	}
	if !strings.Contains(out, "auto_run_log:") {
		t.Errorf("show output missing auto_run_log: line; got:\n%s", out)
	}
	if !strings.Contains(out, "/tmp/auto-runs/") {
		t.Errorf("show output missing log path; got:\n%s", out)
	}
}

func TestShowTaskAutoRunCompleted(t *testing.T) {
	root, db := showListEditDB(t)
	insertTask(t, db, "auto-show-done", "Auto Show Done", "done", "medium", filepath.Join(root, "repo"), nil)
	_, err := db.Exec(
		`UPDATE tasks SET auto_run_status='completed', auto_run_finished='2026-06-10T12:00:00Z' WHERE slug='auto-show-done'`,
	)
	if err != nil {
		t.Fatalf("set auto_run fields: %v", err)
	}

	out := captureStdout(t, func() {
		cmdShow([]string{"task", "auto-show-done"})
	})

	if !strings.Contains(out, "auto_run:      completed") {
		t.Errorf("show output missing 'auto_run: completed'; got:\n%s", out)
	}
	if !strings.Contains(out, "finished") {
		t.Errorf("show output missing finished timestamp; got:\n%s", out)
	}
}
