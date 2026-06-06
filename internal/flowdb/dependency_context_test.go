package flowdb

import (
	"strings"
	"testing"
)

func TestDependencyBootstrapNote(t *testing.T) {
	db := openTempDB(t)
	insertTask(t, db, "child", "Child task", "backlog", "high", "/tmp/wd", nil)
	insertTask(t, db, "merged-dep", "Merged dep", "done", "high", "/tmp/wd", nil)
	insertTask(t, db, "stranded-dep", "Stranded dep", "done", "high", "/tmp/wd", nil)
	insertTask(t, db, "open-dep", "Open dep", "backlog", "high", "/tmp/wd", nil)

	for _, parent := range []string{"merged-dep", "stranded-dep", "open-dep"} {
		if err := AddTaskDependency(db, "child", parent); err != nil {
			t.Fatalf("AddTaskDependency(%s): %v", parent, err)
		}
	}
	// Only the merged dep has a linked PR.
	if err := AddTaskTag(db, "merged-dep", "gh-pr:acme/app#42"); err != nil {
		t.Fatalf("AddTaskTag: %v", err)
	}

	note := DependencyBootstrapNote(db, "child")
	if note == "" {
		t.Fatal("expected a dependency note, got empty")
	}
	// PR-linked dependency surfaces its PR ref.
	if !strings.Contains(note, "acme/app#42") {
		t.Errorf("note missing PR ref for merged dep:\n%s", note)
	}
	// Done dependency with no PR is flagged as un-propagated work.
	if !strings.Contains(note, "Stranded dep") || !strings.Contains(note, "NO PR") {
		t.Errorf("note should flag the stranded done dep as NO PR:\n%s", note)
	}
	// Unfinished dependency is described as not done, not as a NO-PR strand.
	if !strings.Contains(note, "Open dep") || !strings.Contains(note, "not finished") {
		t.Errorf("note should mark the open dep as not finished:\n%s", note)
	}

	// A task with no dependencies gets no note.
	if got := DependencyBootstrapNote(db, "merged-dep"); got != "" {
		t.Errorf("task with no deps should yield empty note, got:\n%s", got)
	}
}
