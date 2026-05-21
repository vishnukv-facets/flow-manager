package server

import (
	"path/filepath"
	"testing"
	"time"

	"flow/internal/flowdb"
)

// TestDBWatcherFiresOnExternalWrite simulates a CLI process (a second
// independent connection to the same SQLite file) writing to the DB,
// and asserts the watcher publishes a ui_change event so an SSE
// subscriber would refresh.
func TestDBWatcherFiresOnExternalWrite(t *testing.T) {
	root, db := testRootDB(t)
	srv := New(Config{DB: db, FlowRoot: root, Version: "test"})

	// Subscribe BEFORE starting the watcher so we don't miss the
	// initial publish window.
	sub := srv.events.subscribe(eventFilter{Types: []string{"ui_change"}})
	defer srv.events.unsubscribe(sub)

	srv.dbWatcher.interval = 100 * time.Millisecond
	srv.dbWatcher.start()
	defer srv.dbWatcher.stopWatching()

	// Open a *separate* connection to the same DB file — this is what
	// data_version is designed to detect.
	cliDB, err := flowdb.OpenDB(filepath.Join(root, "flow.db"))
	if err != nil {
		t.Fatalf("open cli db: %v", err)
	}
	defer cliDB.Close()

	now := "2026-05-21T01:00:00+05:30"
	if _, err := cliDB.Exec(
		`INSERT INTO projects (slug, name, status, priority, work_dir, created_at, updated_at)
		 VALUES ('cli-project', 'CLI project', 'active', 'medium', ?, ?, ?)`,
		root, now, now,
	); err != nil {
		t.Fatalf("cli insert: %v", err)
	}

	select {
	case env := <-sub.send:
		if env.Type != "ui_change" {
			t.Fatalf("got event type %q, want ui_change", env.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ui_change event after external write")
	}
}

// TestDBWatcherIdleNoEvents confirms the watcher stays quiet when no
// external writes happen — otherwise it would defeat the whole point
// of switching SSE from polling to event-driven.
func TestDBWatcherIdleNoEvents(t *testing.T) {
	_, db := testRootDB(t)
	srv := New(Config{DB: db, FlowRoot: "", Version: "test"})

	sub := srv.events.subscribe(eventFilter{Types: []string{"ui_change"}})
	defer srv.events.unsubscribe(sub)

	srv.dbWatcher.interval = 50 * time.Millisecond
	srv.dbWatcher.start()
	defer srv.dbWatcher.stopWatching()

	select {
	case env := <-sub.send:
		t.Fatalf("unexpected event while idle: %+v", env)
	case <-time.After(400 * time.Millisecond):
		// Good — multiple poll intervals elapsed with no spurious event.
	}
}
