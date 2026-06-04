// internal/steering/taskindex.go
package steering

import (
	"database/sql"
	"fmt"
	"strings"

	"flow/internal/flowdb"
)

// BuildTaskIndex renders a compact text index of the operator's ACTIVE tasks
// and projects, for the Stage 2/3 prompts to suggest a matched_task /
// suggested_project. Done, archived, and deleted rows are excluded (defensive
// filtering in Go, independent of filter defaults). Format:
//
//	Projects:
//	- goniyo: Goniyo
//	Tasks:
//	- kong-split [goniyo] (in-progress): Kong split
func BuildTaskIndex(db *sql.DB) (string, error) {
	projects, err := flowdb.ListProjects(db, flowdb.ProjectFilter{IncludeArchived: false})
	if err != nil {
		return "", fmt.Errorf("steering: list projects: %w", err)
	}
	tasks, err := flowdb.ListTasks(db, flowdb.TaskFilter{})
	if err != nil {
		return "", fmt.Errorf("steering: list tasks: %w", err)
	}

	var b strings.Builder
	b.WriteString("Projects:\n")
	pCount := 0
	for _, p := range projects {
		if p.DeletedAt.Valid || p.ArchivedAt.Valid || p.Status == "done" {
			continue
		}
		fmt.Fprintf(&b, "- %s: %s\n", p.Slug, p.Name)
		pCount++
	}
	if pCount == 0 {
		b.WriteString("(none)\n")
	}

	b.WriteString("Tasks:\n")
	tCount := 0
	for _, tk := range tasks {
		if tk.DeletedAt.Valid || tk.ArchivedAt.Valid || tk.Status == "done" {
			continue
		}
		project := ""
		if tk.ProjectSlug.Valid && tk.ProjectSlug.String != "" {
			project = " [" + tk.ProjectSlug.String + "]"
		}
		fmt.Fprintf(&b, "- %s%s (%s): %s\n", tk.Slug, project, tk.Status, tk.Name)
		tCount++
	}
	if tCount == 0 {
		b.WriteString("(none)\n")
	}
	return b.String(), nil
}
