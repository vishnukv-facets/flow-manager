package server

type BrainGraphFilters struct {
	Project     string
	Owner       string
	Status      string
	IncludeDone bool
	Expand      map[string]bool
	Query       string
}

type BrainGraphView struct {
	GeneratedAt     string                 `json:"generated_at"`
	Freshness       string                 `json:"freshness"`
	Controller      BrainGraphController   `json:"controller"`
	Owners          []BrainGraphOwnerView  `json:"owners"`
	Nodes           []BrainGraphNode       `json:"nodes"`
	Edges           []BrainGraphEdge       `json:"edges"`
	Counts          BrainGraphCounts       `json:"counts"`
	SelectedActions []BrainGraphActionSpec `json:"selected_actions"`
	Warnings        []BrainGraphWarning    `json:"warnings"`
}

type BrainGraphController struct {
	Mode        string `json:"mode"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
}

type BrainGraphOwnerView struct {
	ID           string `json:"id"`
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	TaskCount    int    `json:"task_count"`
	RunningCount int    `json:"running_count"`
}

type BrainGraphNode struct {
	ID             string            `json:"id"`
	Type           string            `json:"type"`
	OwnerSlug      string            `json:"owner_slug,omitempty"`
	TaskSlug       string            `json:"task_slug,omitempty"`
	ParentTaskSlug string            `json:"parent_task_slug,omitempty"`
	Label          string            `json:"label"`
	Status         string            `json:"status"`
	Priority       string            `json:"priority,omitempty"`
	Provider       string            `json:"provider,omitempty"`
	Harness        string            `json:"harness,omitempty"`
	PermissionMode string            `json:"permission_mode,omitempty"`
	Model          string            `json:"model,omitempty"`
	Summary        string            `json:"summary,omitempty"`
	Expanded       bool              `json:"expanded"`
	Ref            *BrainGraphRef    `json:"ref,omitempty"`
	Badges         []string          `json:"badges,omitempty"`
	Actions        []string          `json:"actions,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type BrainGraphRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	URL  string `json:"url,omitempty"`
}

type BrainGraphEdge struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Source string `json:"source"`
	Target string `json:"target"`
	Label  string `json:"label,omitempty"`
	Status string `json:"status,omitempty"`
}

type BrainGraphCounts struct {
	TotalTasks int `json:"total_tasks"`
	Running    int `json:"running"`
	Done       int `json:"done"`
	Owners     int `json:"owners"`
	Warnings   int `json:"warnings"`
}

type BrainGraphActionSpec struct {
	Key            string `json:"key"`
	Label          string `json:"label"`
	Risky          bool   `json:"risky"`
	Enabled        bool   `json:"enabled"`
	DisabledReason string `json:"disabled_reason,omitempty"`
}

type BrainGraphWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	NodeID  string `json:"node_id,omitempty"`
}

type BrainGraphNodeDetail struct {
	ID       string                    `json:"id"`
	Type     string                    `json:"type"`
	Task     *BrainGraphTaskDetail     `json:"task,omitempty"`
	Evidence *BrainGraphEvidenceDetail `json:"evidence,omitempty"`
}

type BrainGraphTaskDetail struct {
	Slug            string                    `json:"slug"`
	Name            string                    `json:"name"`
	Status          string                    `json:"status"`
	Priority        string                    `json:"priority"`
	ProjectSlug     *string                   `json:"project_slug,omitempty"`
	ParentSlug      *string                   `json:"parent_slug,omitempty"`
	WorkDir         string                    `json:"work_dir"`
	WorktreePath    *string                   `json:"worktree_path,omitempty"`
	SessionProvider string                    `json:"session_provider"`
	Harness         string                    `json:"harness"`
	PermissionMode  string                    `json:"permission_mode"`
	Model           *string                   `json:"model,omitempty"`
	SessionID       *string                   `json:"session_id,omitempty"`
	SessionPath     *string                   `json:"session_path,omitempty"`
	Transcript      *BrainGraphEvidenceDetail `json:"transcript,omitempty"`
	BriefPath       string                    `json:"brief_path"`
	Updates         []FileRef                 `json:"updates"`
}

type BrainGraphEvidenceDetail struct {
	Kind      string  `json:"kind"`
	TaskSlug  string  `json:"task_slug,omitempty"`
	RefID     string  `json:"ref_id,omitempty"`
	Path      *string `json:"path,omitempty"`
	URL       *string `json:"url,omitempty"`
	Available bool    `json:"available"`
	Message   string  `json:"message,omitempty"`
}

type BrainGraphActionRequest struct {
	Action string `json:"action"`
	NodeID string `json:"node_id"`
	Prompt string `json:"prompt,omitempty"`
	Actor  string `json:"actor,omitempty"`
}

type BrainGraphActionResponse struct {
	OK             bool            `json:"ok"`
	Message        string          `json:"message"`
	Action         string          `json:"action,omitempty"`
	NodeID         string          `json:"node_id,omitempty"`
	Output         string          `json:"output,omitempty"`
	ActionResponse *actionResponse `json:"action_response,omitempty"`
}
