// Package memorysrc discovers markdown files that agents or flow itself use as
// durable memory.
package memorysrc

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Source is one potential memory-bearing file. The path may not exist; callers
// that surface missing sources can still show users where the agent would look.
type Source struct {
	ID       string
	Provider string
	Scope    string
	Kind     string
	Label    string
	Path     string
}

// AgentSources returns Codex and Claude memory/instruction sources for the
// supplied workdirs, using the same discovery rules as Mission Control.
func AgentSources(workdirs []string) []Source {
	var candidates []Source
	candidates = append(candidates, codexUserMemoryCandidates()...)
	candidates = append(candidates, codexMemoryFileCandidates(CodexHomeDir())...)

	for _, workdir := range normalizeWorkdirs(workdirs) {
		candidates = append(candidates, codexProjectMemoryCandidates(workdir)...)
		candidates = append(candidates, claudeAutoMemoryCandidates(workdir)...)
	}

	out := make([]Source, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate.ID == "" {
			continue
		}
		if IsClaudeMDPath(candidate.Path) {
			continue
		}
		if seen[candidate.ID] {
			candidate.ID = candidate.ID + "-" + MemorySourceSlug(candidate.Path)
			if candidate.ID == "" || seen[candidate.ID] {
				continue
			}
		}
		seen[candidate.ID] = true
		out = append(out, candidate)
	}
	return out
}

// FlowKBSources returns flow's own markdown knowledge-base files.
func FlowKBSources(flowRoot string) []Source {
	var out []Source
	for _, name := range []string{"user", "org", "products", "processes", "business"} {
		out = append(out, Source{
			ID:       "flow-kb-" + name,
			Provider: "flow",
			Scope:    "global",
			Kind:     "knowledge-base",
			Label:    "Flow KB " + name,
			Path:     filepath.Join(flowRoot, "kb", name+".md"),
		})
	}
	return out
}

// AllSources returns Flow KB plus agent memory sources.
func AllSources(flowRoot string, workdirs []string) []Source {
	out := FlowKBSources(flowRoot)
	out = append(out, AgentSources(workdirs)...)
	return out
}

func codexUserMemoryCandidates() []Source {
	home := CodexHomeDir()
	return []Source{
		{
			ID:       "codex-user-agents-override",
			Provider: "codex",
			Scope:    "user",
			Kind:     "instructions",
			Label:    "Codex global override instructions",
			Path:     filepath.Join(home, "AGENTS.override.md"),
		},
		{
			ID:       "codex-user-agents",
			Provider: "codex",
			Scope:    "user",
			Kind:     "instructions",
			Label:    "Codex global instructions",
			Path:     filepath.Join(home, "AGENTS.md"),
		},
	}
}

func codexMemoryFileCandidates(home string) []Source {
	root := filepath.Join(home, "memories")
	paths := markdownFilesUnder(root, 500)
	if len(paths) == 0 {
		return []Source{{
			ID:       "codex-user-memories",
			Provider: "codex",
			Scope:    "user",
			Kind:     "auto-memory",
			Label:    "Codex memories directory",
			Path:     filepath.Join(root, "MEMORY.md"),
		}}
	}
	out := make([]Source, 0, len(paths))
	for _, path := range paths {
		rel := relTo(root, path)
		out = append(out, Source{
			ID:       "codex-memory-" + MemorySourceSlug(rel),
			Provider: "codex",
			Scope:    "user",
			Kind:     "auto-memory",
			Label:    "Codex memory " + rel,
			Path:     path,
		})
	}
	return out
}

func codexProjectMemoryCandidates(workdir string) []Source {
	root := repositoryRoot(workdir)
	dirs := pathChain(root, workdir)
	fallbacks := codexProjectFallbackFilenames()
	var out []Source
	for _, dir := range dirs {
		if candidate, ok := firstExistingCodexProjectCandidate(root, dir, fallbacks); ok {
			out = append(out, candidate)
		}
	}
	if len(out) == 0 {
		out = append(out, Source{
			ID:       "codex-project-" + MemorySourceSlug(workdir) + "-agents",
			Provider: "codex",
			Scope:    "project",
			Kind:     "instructions",
			Label:    "Codex project instructions",
			Path:     filepath.Join(workdir, "AGENTS.md"),
		})
	}
	return out
}

func firstExistingCodexProjectCandidate(root, dir string, fallbacks []string) (Source, bool) {
	names := append([]string{"AGENTS.override.md", "AGENTS.md"}, fallbacks...)
	for _, name := range names {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() && info.Size() > 0 {
			rel := relTo(root, path)
			return Source{
				ID:       "codex-project-" + MemorySourceSlug(root) + "-" + MemorySourceSlug(rel),
				Provider: "codex",
				Scope:    "project",
				Kind:     "instructions",
				Label:    "Codex project instructions " + rel,
				Path:     path,
			}, true
		}
	}
	return Source{}, false
}

func claudeAutoMemoryCandidates(workdir string) []Source {
	memoryDir := ClaudeAutoMemoryDir(workdir)
	paths := markdownFilesUnder(memoryDir, 200)
	if len(paths) == 0 {
		return []Source{{
			ID:       "claude-auto-memory-" + MemorySourceSlug(workdir),
			Provider: "claude",
			Scope:    "project",
			Kind:     "auto-memory",
			Label:    "Claude auto memory",
			Path:     filepath.Join(memoryDir, "MEMORY.md"),
		}}
	}
	out := make([]Source, 0, len(paths))
	for _, path := range paths {
		rel := relTo(memoryDir, path)
		out = append(out, Source{
			ID:       "claude-auto-memory-" + MemorySourceSlug(workdir) + "-" + MemorySourceSlug(rel),
			Provider: "claude",
			Scope:    "project",
			Kind:     "auto-memory",
			Label:    "Claude auto memory " + rel,
			Path:     path,
		})
	}
	return out
}

// CodexHomeDir returns the active Codex home directory.
func CodexHomeDir() string {
	if dir := strings.TrimSpace(os.Getenv("CODEX_HOME")); dir != "" {
		return dir
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".codex")
	}
	return filepath.Join("~", ".codex")
}

func claudeConfigDir() string {
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		return dir
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".claude")
	}
	return filepath.Join("~", ".claude")
}

// ClaudeAutoMemoryDir returns Claude's auto-memory directory for a workdir.
func ClaudeAutoMemoryDir(workdir string) string {
	if dir := configuredClaudeAutoMemoryDir(); dir != "" {
		return dir
	}
	root := repositoryRoot(workdir)
	return filepath.Join(claudeConfigDir(), "projects", ClaudeProjectKey(root), "memory")
}

func configuredClaudeAutoMemoryDir() string {
	settingsPath := filepath.Join(claudeConfigDir(), "settings.json")
	body, err := os.ReadFile(settingsPath)
	if err != nil {
		return ""
	}
	var settings struct {
		AutoMemoryDirectory string `json:"autoMemoryDirectory"`
	}
	if err := json.Unmarshal(body, &settings); err != nil {
		return ""
	}
	dir := strings.TrimSpace(settings.AutoMemoryDirectory)
	if dir == "" {
		return ""
	}
	if strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(dir, "~/"))
		}
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	return ""
}

// ClaudeProjectKey returns Claude's filesystem key for a project path.
func ClaudeProjectKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	path = filepath.Clean(path)
	return strings.ReplaceAll(filepath.ToSlash(path), "/", "-")
}

func codexProjectFallbackFilenames() []string {
	configPath := filepath.Join(CodexHomeDir(), "config.toml")
	body, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "project_doc_fallback_filenames") {
			continue
		}
		start := strings.Index(line, "[")
		end := strings.LastIndex(line, "]")
		if start < 0 || end <= start {
			return nil
		}
		var out []string
		for _, raw := range strings.Split(line[start+1:end], ",") {
			name := strings.Trim(strings.TrimSpace(raw), `"'`)
			if name != "" && filepath.Base(name) == name {
				out = append(out, name)
			}
		}
		return out
	}
	return nil
}

func normalizeWorkdirs(workdirs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, path := range workdirs {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func repositoryRoot(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	path = filepath.Clean(path)
	for dir := path; ; dir = filepath.Dir(dir) {
		gitPath := filepath.Join(dir, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			if root := worktreeMainRoot(dir, gitPath); root != "" {
				return root
			}
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}
	}
}

func worktreeMainRoot(worktreeRoot, gitPath string) string {
	info, err := os.Stat(gitPath)
	if err != nil || info.IsDir() {
		return ""
	}
	body, err := os.ReadFile(gitPath)
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(body))
	if !strings.HasPrefix(text, "gitdir:") {
		return ""
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(text, "gitdir:"))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Clean(filepath.Join(worktreeRoot, gitDir))
	}
	parts := strings.Split(filepath.ToSlash(gitDir), "/.git/worktrees/")
	if len(parts) != 2 {
		return ""
	}
	return filepath.FromSlash(parts[0])
}

func pathChain(root, leaf string) []string {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if abs, err := filepath.Abs(leaf); err == nil {
		leaf = abs
	}
	root = filepath.Clean(root)
	leaf = filepath.Clean(leaf)
	rel, err := filepath.Rel(root, leaf)
	if err != nil || strings.HasPrefix(rel, "..") {
		return []string{leaf}
	}
	out := []string{root}
	if rel == "." {
		return out
	}
	dir := root
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "" || part == "." {
			continue
		}
		dir = filepath.Join(dir, part)
		out = append(out, dir)
	}
	return out
}

func markdownFilesUnder(root string, limit int) []string {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && shouldSkipMemoryDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			out = append(out, path)
		}
		if limit > 0 && len(out) >= limit {
			return filepath.SkipAll
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func shouldSkipMemoryDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".codex", ".agents", "node_modules", "vendor", "dist", "build":
		return true
	default:
		return false
	}
}

// IsClaudeMDPath reports whether a path points to CLAUDE.md.
func IsClaudeMDPath(path string) bool {
	return strings.EqualFold(filepath.Base(path), "CLAUDE.md")
}

func relTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.Base(path)
	}
	return filepath.ToSlash(rel)
}

// MemorySourceSlug converts a source path/label into a stable slug fragment.
func MemorySourceSlug(path string) string {
	path = strings.TrimSuffix(filepath.ToSlash(path), ".md")
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(path) {
		allowed := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if allowed {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
