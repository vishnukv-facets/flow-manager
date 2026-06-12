package app

import (
	"flow/internal/flowdb"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCmdAddOwnerAndList(t *testing.T) {
	root := setupFlowRoot(t)
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if rc := cmdAdd([]string{"owner", "Release Watcher", "--work-dir", repo, "--every", "1h", "--agent", "claude"}); rc != 0 {
			t.Fatalf("add owner rc=%d", rc)
		}
	})
	if !strings.Contains(out, `Created owner "release-watcher"`) {
		t.Fatalf("add output = %q", out)
	}
	if _, err := os.Stat(filepath.Join(root, "owners", "release-watcher", "charter.md")); err != nil {
		t.Fatalf("charter not created: %v", err)
	}

	out = captureStdout(t, func() {
		if rc := cmdOwner([]string{"list"}); rc != 0 {
			t.Fatalf("owner list rc=%d", rc)
		}
	})
	for _, want := range []string{"release-watcher", "active", "1h", "claude"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list output missing %q: %q", want, out)
		}
	}
}

func TestCmdOwnerLifecycle(t *testing.T) {
	root := setupFlowRoot(t)
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if rc := cmdAdd([]string{"owner", "Deploy Owner", "--work-dir", repo, "--every", "30m"}); rc != 0 {
		t.Fatalf("add owner rc=%d", rc)
	}

	if rc := cmdOwner([]string{"pause", "deploy-owner"}); rc != 0 {
		t.Fatalf("pause rc=%d", rc)
	}
	dbPath, err := flowDBPath()
	if err != nil {
		t.Fatal(err)
	}
	db, err := flowdb.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	o, err := flowdb.GetOwner(db, "deploy-owner")
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != "paused" {
		t.Fatalf("status after pause = %q", o.Status)
	}

	if rc := cmdOwner([]string{"start", "deploy-owner"}); rc != 0 {
		t.Fatalf("start rc=%d", rc)
	}
	o, err = flowdb.GetOwner(db, "deploy-owner")
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != "active" || !o.NextWakeAt.Valid {
		t.Fatalf("after start = %+v", o)
	}

	next := time.Now().Add(15 * time.Minute)
	if rc := cmdOwner([]string{"next", "deploy-owner", "--at", next.Format(time.RFC3339)}); rc != 0 {
		t.Fatalf("next rc=%d", rc)
	}
	o, err = flowdb.GetOwner(db, "deploy-owner")
	if err != nil {
		t.Fatal(err)
	}
	if !o.NextWakeAt.Valid || o.NextWakeAt.String != next.Format(time.RFC3339) {
		t.Fatalf("next_wake_at = %#v, want %s", o.NextWakeAt, next.Format(time.RFC3339))
	}

	if rc := cmdOwner([]string{"retire", "deploy-owner"}); rc != 0 {
		t.Fatalf("retire rc=%d", rc)
	}
	o, err = flowdb.GetOwner(db, "deploy-owner")
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != "retired" || !o.ArchivedAt.Valid {
		t.Fatalf("after retire = %+v", o)
	}
}

func TestCmdAddTaskTags(t *testing.T) {
	root := setupFlowRoot(t)
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if rc := cmdAdd([]string{"task", "Ask Human", "--work-dir", repo, "--agent", "claude", "--tag", "owner:ops", "--tag", "question"}); rc != 0 {
		t.Fatalf("add task rc=%d", rc)
	}
	db := openFlowDB(t)
	tags, err := flowdb.GetTaskTags(db, "ask-human")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tags, ",") != "owner:ops,question" {
		t.Fatalf("tags = %#v", tags)
	}
}

func TestCmdOwnerTickDueDispatchesAndAdvancesSchedule(t *testing.T) {
	root := setupFlowRoot(t)
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if rc := cmdAdd([]string{"owner", "Ops Owner", "--work-dir", repo, "--every", "1h"}); rc != 0 {
		t.Fatalf("add owner rc=%d", rc)
	}
	db := openFlowDB(t)
	if err := flowdb.SetOwnerNextWake(db, "ops-owner", time.Now().Add(-time.Minute).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	oldLauncher := ownerTickLauncher
	var gotSlug, gotWorkDir, gotLog string
	ownerTickLauncher = func(slug, workDir, logPath string, env []string) (int, error) {
		gotSlug, gotWorkDir, gotLog = slug, workDir, logPath
		return 4242, nil
	}
	t.Cleanup(func() { ownerTickLauncher = oldLauncher })
	oldAlive := processAlive
	processAlive = func(pid int) bool { return false }
	t.Cleanup(func() { processAlive = oldAlive })

	if rc := cmdOwner([]string{"tick-due"}); rc != 0 {
		t.Fatalf("tick-due rc=%d", rc)
	}
	o, err := flowdb.GetOwner(db, "ops-owner")
	if err != nil {
		t.Fatal(err)
	}
	if gotSlug != "ops-owner" || gotWorkDir != repo || !strings.Contains(gotLog, filepath.Join(root, "owners", "ops-owner", "ticks")) {
		t.Fatalf("launcher got slug=%q workDir=%q log=%q", gotSlug, gotWorkDir, gotLog)
	}
	if !o.TickPID.Valid || o.TickPID.Int64 != 4242 || !o.TickStarted.Valid {
		t.Fatalf("tick bookkeeping = %+v", o)
	}
	if !o.NextWakeAt.Valid {
		t.Fatalf("next wake missing after dispatch: %+v", o)
	}
	next, err := time.Parse(time.RFC3339, o.NextWakeAt.String)
	if err != nil {
		t.Fatal(err)
	}
	if !next.After(time.Now()) {
		t.Fatalf("next wake = %s, want future", o.NextWakeAt.String)
	}
}

func TestCmdOwnerTickRecordsCompletion(t *testing.T) {
	root := setupFlowRoot(t)
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if rc := cmdAdd([]string{"owner", "Deploy Owner", "--work-dir", repo, "--every", "30m"}); rc != 0 {
		t.Fatalf("add owner rc=%d", rc)
	}
	db := openFlowDB(t)
	if err := recordOwnerTickStarted(db, "deploy-owner", 1234, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	oldRunner := ownerTickRunner
	var gotProvider, gotWorkDir, gotRoot, gotPrompt string
	ownerTickRunner = func(provider, workDir, flowRootPath, prompt string) error {
		gotProvider, gotWorkDir, gotRoot, gotPrompt = provider, workDir, flowRootPath, prompt
		return nil
	}
	t.Cleanup(func() { ownerTickRunner = oldRunner })

	if rc := cmdOwnerTick([]string{"deploy-owner"}); rc != 0 {
		t.Fatalf("__owner-tick rc=%d", rc)
	}
	if gotProvider != "claude" || gotWorkDir != repo || gotRoot != root || !strings.Contains(gotPrompt, "deploy-owner") {
		t.Fatalf("runner got provider=%q workDir=%q root=%q prompt=%q", gotProvider, gotWorkDir, gotRoot, gotPrompt)
	}
	o, err := flowdb.GetOwner(db, "deploy-owner")
	if err != nil {
		t.Fatal(err)
	}
	if o.TickPID.Valid || o.TickStarted.Valid || !o.LastTickStatus.Valid || o.LastTickStatus.String != "ok" {
		t.Fatalf("owner after tick = %+v", o)
	}
}
