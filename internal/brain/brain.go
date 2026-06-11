package brain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// Status is used by Brain plans and plan items. The plan lifecycle uses a
// subset of these values; item states reuse the same vocabulary for progress.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusApproved  Status = "approved"
	StatusExecuting Status = "executing"
	StatusBlocked   Status = "blocked"
	StatusCompleted Status = "completed"
	StatusCancelled Status = "cancelled"
	StatusRejected  Status = "rejected"
	StatusDeferred  Status = "deferred"
	StatusFailed    Status = "failed"
)

// ItemKind identifies one action bundle entry.
type ItemKind string

const (
	ItemKindTask             ItemKind = "task"
	ItemKindLaunchWorker     ItemKind = "launch_worker"
	ItemKindMerge            ItemKind = "merge"
	ItemKindDestructiveShell ItemKind = "destructive_shell"
	ItemKindDeploy           ItemKind = "deploy"
	ItemKindOutboundReply    ItemKind = "outbound_reply"
	ItemKindNote             ItemKind = "note"
)

var (
	slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
	shortIDRe    = regexp.MustCompile(`[^a-z0-9]+`)
)

// SourceRef points back to Flow data/search rows or other external evidence.
type SourceRef struct {
	Kind    string `json:"kind,omitempty"`
	Slug    string `json:"slug,omitempty"`
	Title   string `json:"title,omitempty"`
	Path    string `json:"path,omitempty"`
	URL     string `json:"url,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

// TaskSpec is the task-creation payload inside a plan item.
type TaskSpec struct {
	Slug               string   `json:"slug,omitempty"`
	Name               string   `json:"name,omitempty"`
	Project            string   `json:"project,omitempty"`
	WorkDir            string   `json:"work_dir,omitempty"`
	SubtaskOf          string   `json:"subtask_of,omitempty"`
	DependsOn          []string `json:"depends_on,omitempty"`
	BranchPolicy       string   `json:"branch_policy,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
}

// Item is one action bundle entry.
type Item struct {
	ID             string      `json:"id,omitempty"`
	Kind           ItemKind    `json:"kind,omitempty"`
	Title          string      `json:"title,omitempty"`
	Provider       string      `json:"provider,omitempty"`
	Model          string      `json:"model,omitempty"`
	Tier           string      `json:"tier,omitempty"`
	PermissionMode string      `json:"permission_mode,omitempty"`
	Risk           string      `json:"risk,omitempty"`
	Task           *TaskSpec   `json:"task,omitempty"`
	SourceRefs     []SourceRef `json:"source_refs,omitempty"`
	Status         Status      `json:"status,omitempty"`
	TaskSlug       string      `json:"task_slug,omitempty"`
	Error          string      `json:"error,omitempty"`
}

// Plan is the persisted Brain plan/action bundle.
type Plan struct {
	ID           string      `json:"id"`
	Status       Status      `json:"status"`
	Title        string      `json:"title"`
	Query        string      `json:"query,omitempty"`
	Summary      string      `json:"summary,omitempty"`
	Source       string      `json:"source,omitempty"`
	Project      string      `json:"project,omitempty"`
	WorkDir      string      `json:"work_dir,omitempty"`
	BranchPolicy string      `json:"branch_policy,omitempty"`
	Items        []Item      `json:"items,omitempty"`
	SourceRefs   []SourceRef `json:"source_refs,omitempty"`
	CreatedAt    string      `json:"created_at"`
	UpdatedAt    string      `json:"updated_at"`
	ApprovedAt   *string     `json:"approved_at,omitempty"`
	ExecutedAt   *string     `json:"executed_at,omitempty"`
	CompletedAt  *string     `json:"completed_at,omitempty"`
	CancelledAt  *string     `json:"cancelled_at,omitempty"`
	RejectedAt   *string     `json:"rejected_at,omitempty"`
	BlockedAt    *string     `json:"blocked_at,omitempty"`
	Error        string      `json:"error,omitempty"`
}

// NormalizePlan fills in defaults and canonicalizes strings so the persisted
// JSON stays stable.
func NormalizePlan(p *Plan) error {
	if p == nil {
		return errors.New("plan is nil")
	}
	p.ID = strings.TrimSpace(p.ID)
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	p.Status = Status(strings.ToLower(strings.TrimSpace(string(p.Status))))
	if p.Status == "" {
		p.Status = StatusDraft
	}
	if !isPlanStatus(string(p.Status)) {
		return fmt.Errorf("invalid plan status %q", p.Status)
	}
	p.Title = strings.TrimSpace(p.Title)
	p.Query = strings.TrimSpace(p.Query)
	p.Summary = strings.TrimSpace(p.Summary)
	p.Source = strings.ToLower(strings.TrimSpace(p.Source))
	if p.Source == "" {
		p.Source = "ask-flow"
	}
	p.Project = strings.TrimSpace(p.Project)
	p.WorkDir = strings.TrimSpace(p.WorkDir)
	p.BranchPolicy = strings.TrimSpace(p.BranchPolicy)
	p.Error = strings.TrimSpace(p.Error)
	if p.Title == "" {
		switch {
		case p.Query != "":
			p.Title = p.Query
		case len(p.Items) > 0 && strings.TrimSpace(p.Items[0].Title) != "":
			p.Title = strings.TrimSpace(p.Items[0].Title)
		default:
			p.Title = "Brain plan"
		}
	}

	seenItemIDs := make(map[string]struct{}, len(p.Items))
	for i := range p.Items {
		item := &p.Items[i]
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" {
			item.ID = uuid.NewString()
		}
		if _, ok := seenItemIDs[item.ID]; ok {
			return fmt.Errorf("duplicate item id %q", item.ID)
		}
		seenItemIDs[item.ID] = struct{}{}

		item.Kind = ItemKind(strings.ToLower(strings.TrimSpace(string(item.Kind))))
		if item.Kind == "" {
			if item.Task != nil {
				item.Kind = ItemKindTask
			} else {
				item.Kind = ItemKindNote
			}
		}
		if !isItemKind(string(item.Kind)) {
			return fmt.Errorf("invalid item kind %q", item.Kind)
		}

		item.Title = strings.TrimSpace(item.Title)
		item.Provider = strings.ToLower(strings.TrimSpace(item.Provider))
		item.Model = strings.TrimSpace(item.Model)
		item.Tier = strings.ToLower(strings.TrimSpace(item.Tier))
		item.PermissionMode = normalizePermissionMode(strings.ToLower(strings.TrimSpace(item.PermissionMode)))
		item.Risk = normalizeRisk(strings.ToLower(strings.TrimSpace(item.Risk)))
		item.Error = strings.TrimSpace(item.Error)
		item.TaskSlug = strings.TrimSpace(item.TaskSlug)
		item.Status = Status(strings.ToLower(strings.TrimSpace(string(item.Status))))
		if item.Status == "" {
			item.Status = StatusDraft
		}
		if !isItemStatus(string(item.Status)) {
			return fmt.Errorf("invalid item status %q", item.Status)
		}

		item.SourceRefs = normalizeSourceRefs(item.SourceRefs)
		if item.Kind != ItemKindNote && item.Provider == "" {
			return fmt.Errorf("item %q requires provider", item.ID)
		}
		if item.Kind == ItemKindTask {
			if item.Task == nil {
				item.Task = &TaskSpec{}
			}
			item.Task.Slug = strings.TrimSpace(item.Task.Slug)
			item.Task.Name = strings.TrimSpace(item.Task.Name)
			if item.Task.Name == "" {
				item.Task.Name = item.Title
			}
			if item.Task.Name == "" {
				return fmt.Errorf("task item %q requires a name", item.ID)
			}
			item.Task.Project = strings.TrimSpace(item.Task.Project)
			if item.Task.Project == "" {
				item.Task.Project = p.Project
			}
			item.Task.WorkDir = strings.TrimSpace(item.Task.WorkDir)
			if item.Task.WorkDir == "" {
				item.Task.WorkDir = p.WorkDir
			}
			item.Task.SubtaskOf = strings.TrimSpace(item.Task.SubtaskOf)
			item.Task.BranchPolicy = strings.TrimSpace(item.Task.BranchPolicy)
			if item.Task.BranchPolicy == "" {
				item.Task.BranchPolicy = p.BranchPolicy
			}
			item.Task.DependsOn = normalizeStrings(item.Task.DependsOn)
			item.Task.Tags = normalizeStrings(item.Task.Tags)
			item.Task.AcceptanceCriteria = normalizeStrings(item.Task.AcceptanceCriteria)
		}
	}
	return nil
}

// ValidatePlan checks the normalized plan for structural correctness.
func ValidatePlan(p *Plan) error {
	if p == nil {
		return errors.New("plan is nil")
	}
	if strings.TrimSpace(p.ID) == "" {
		return errors.New("plan id is required")
	}
	if strings.TrimSpace(p.Title) == "" {
		return errors.New("plan title is required")
	}
	if !isPlanStatus(string(p.Status)) {
		return fmt.Errorf("invalid plan status %q", p.Status)
	}

	seen := make(map[string]struct{}, len(p.Items))
	taskSlugSeen := make(map[string]string, len(p.Items))
	for _, item := range p.Items {
		if item.ID == "" {
			return errors.New("item id is required")
		}
		if _, ok := seen[item.ID]; ok {
			return fmt.Errorf("duplicate item id %q", item.ID)
		}
		seen[item.ID] = struct{}{}
		if !isItemKind(string(item.Kind)) {
			return fmt.Errorf("invalid item kind %q", item.Kind)
		}
		if !isItemStatus(string(item.Status)) {
			return fmt.Errorf("invalid item status %q", item.Status)
		}
		if item.Kind != ItemKindNote && item.Provider == "" {
			return fmt.Errorf("item %q requires provider", item.ID)
		}
		if !isRisk(item.Risk) {
			return fmt.Errorf("item %q has invalid risk %q", item.ID, item.Risk)
		}
		if item.PermissionMode != "" && !isPermissionMode(item.PermissionMode) {
			return fmt.Errorf("item %q has invalid permission mode %q", item.ID, item.PermissionMode)
		}
		if item.Kind == ItemKindTask {
			if item.Task == nil {
				return fmt.Errorf("task item %q is missing task payload", item.ID)
			}
			if strings.TrimSpace(item.Task.Name) == "" {
				return fmt.Errorf("task item %q requires a task name", item.ID)
			}
			if item.Task.Slug != "" {
				if prev, ok := taskSlugSeen[item.Task.Slug]; ok && prev != item.ID {
					return fmt.Errorf("duplicate task slug %q", item.Task.Slug)
				}
				taskSlugSeen[item.Task.Slug] = item.ID
			}
		}
	}
	return nil
}

// TaskSlug returns a deterministic, human-readable task slug for a plan item.
func TaskSlug(planID, itemID, title string) string {
	base, err := Slugify(title)
	if err != nil || base == "" {
		base = "task"
	}
	prefix := shortID(planID)
	suffix := shortID(itemID)
	parts := []string{"brain"}
	if prefix != "" {
		parts = append(parts, prefix)
	}
	if base != "" {
		parts = append(parts, base)
	}
	if suffix != "" {
		parts = append(parts, suffix)
	}
	slug := strings.Join(parts, "-")
	if len(slug) > 96 {
		slug = slug[:96]
		slug = strings.Trim(slug, "-")
	}
	return slug
}

// Slugify converts a free-form string into a Flow-safe slug.
func Slugify(name string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugNonAlnum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "", fmt.Errorf("cannot slugify %q: result is empty", name)
	}
	parts := strings.SplitN(s, "-", 7)
	if len(parts) > 6 {
		s = strings.Join(parts[:6], "-")
	}
	return s, nil
}

func normalizeSourceRefs(in []SourceRef) []SourceRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]SourceRef, 0, len(in))
	for _, ref := range in {
		ref.Kind = strings.TrimSpace(ref.Kind)
		ref.Slug = strings.TrimSpace(ref.Slug)
		ref.Title = strings.TrimSpace(ref.Title)
		ref.Path = strings.TrimSpace(ref.Path)
		ref.URL = strings.TrimSpace(ref.URL)
		ref.Snippet = strings.TrimSpace(ref.Snippet)
		if ref.Kind == "" && ref.Title == "" && ref.Slug == "" && ref.Path == "" && ref.URL == "" && ref.Snippet == "" {
			continue
		}
		out = append(out, ref)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func shortID(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	raw = shortIDRe.ReplaceAllString(raw, "")
	if len(raw) > 8 {
		raw = raw[:8]
	}
	return raw
}

func normalizeRisk(raw string) string {
	switch raw {
	case "", "low", "medium", "high":
		if raw == "" {
			return "medium"
		}
		return raw
	default:
		return raw
	}
}

func isRisk(raw string) bool {
	switch raw {
	case "low", "medium", "high":
		return true
	default:
		return false
	}
}

func normalizePermissionMode(raw string) string {
	switch raw {
	case "", "auto":
		return "auto"
	case "default", "bypass":
		return raw
	default:
		return raw
	}
}

func isPermissionMode(raw string) bool {
	switch raw {
	case "default", "auto", "bypass":
		return true
	default:
		return false
	}
}

func isPlanStatus(raw string) bool {
	switch Status(raw) {
	case StatusDraft, StatusApproved, StatusExecuting, StatusBlocked, StatusCompleted, StatusCancelled, StatusRejected, StatusDeferred, StatusFailed:
		return true
	default:
		return false
	}
}

func isItemStatus(raw string) bool {
	return isPlanStatus(raw)
}

func isItemKind(raw string) bool {
	switch ItemKind(raw) {
	case ItemKindTask, ItemKindLaunchWorker, ItemKindMerge, ItemKindDestructiveShell, ItemKindDeploy, ItemKindOutboundReply, ItemKindNote:
		return true
	default:
		return false
	}
}
