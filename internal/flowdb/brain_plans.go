package flowdb

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"flow/internal/brain"
)

// BrainPlanFilter narrows ListBrainPlans.
type BrainPlanFilter struct {
	Status  string
	Project string
	Source  string
}

type brainPlanRow struct {
	ID           string
	Title        string
	Query        string
	Summary      string
	Source       string
	Status       string
	ProjectSlug  sql.NullString
	WorkDir      sql.NullString
	BranchPolicy sql.NullString
	Error        sql.NullString
	ApprovedAt   sql.NullString
	ExecutedAt   sql.NullString
	CompletedAt  sql.NullString
	CancelledAt  sql.NullString
	RejectedAt   sql.NullString
	BlockedAt    sql.NullString
	CreatedAt    string
	UpdatedAt    string
	PlanJSON     string
}

// CreateBrainPlan persists a new draft plan.
func CreateBrainPlan(db *sql.DB, plan *brain.Plan) error {
	if plan == nil {
		return errors.New("brain plan is nil")
	}
	payload, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshal brain plan %s: %w", plan.ID, err)
	}
	_, err = db.Exec(`
		INSERT INTO brain_plans (
			id, title, query, summary, source, status,
			project_slug, work_dir, branch_policy, error,
			approved_at, executed_at, completed_at, cancelled_at, rejected_at, blocked_at,
			created_at, updated_at, plan_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		plan.ID, plan.Title, plan.Query, plan.Summary, plan.Source, string(plan.Status),
		NullIfEmpty(plan.Project), NullIfEmpty(plan.WorkDir), NullIfEmpty(plan.BranchPolicy), NullIfEmpty(plan.Error),
		nullStringPtr(plan.ApprovedAt), nullStringPtr(plan.ExecutedAt), nullStringPtr(plan.CompletedAt), nullStringPtr(plan.CancelledAt), nullStringPtr(plan.RejectedAt), nullStringPtr(plan.BlockedAt),
		plan.CreatedAt, plan.UpdatedAt, string(payload),
	)
	if err != nil {
		return fmt.Errorf("create brain plan %s: %w", plan.ID, err)
	}
	return nil
}

// SaveBrainPlan updates an existing plan row.
func SaveBrainPlan(db *sql.DB, plan *brain.Plan) error {
	if plan == nil {
		return errors.New("brain plan is nil")
	}
	payload, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshal brain plan %s: %w", plan.ID, err)
	}
	res, err := db.Exec(`
		UPDATE brain_plans SET
			title = ?,
			query = ?,
			summary = ?,
			source = ?,
			status = ?,
			project_slug = ?,
			work_dir = ?,
			branch_policy = ?,
			error = ?,
			approved_at = ?,
			executed_at = ?,
			completed_at = ?,
			cancelled_at = ?,
			rejected_at = ?,
			blocked_at = ?,
			updated_at = ?,
			plan_json = ?
		WHERE id = ?
	`,
		plan.Title, plan.Query, plan.Summary, plan.Source, string(plan.Status),
		NullIfEmpty(plan.Project), NullIfEmpty(plan.WorkDir), NullIfEmpty(plan.BranchPolicy), NullIfEmpty(plan.Error),
		nullStringPtr(plan.ApprovedAt), nullStringPtr(plan.ExecutedAt), nullStringPtr(plan.CompletedAt), nullStringPtr(plan.CancelledAt), nullStringPtr(plan.RejectedAt), nullStringPtr(plan.BlockedAt),
		plan.UpdatedAt, string(payload), plan.ID,
	)
	if err != nil {
		return fmt.Errorf("save brain plan %s: %w", plan.ID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetBrainPlan fetches a single persisted plan by ID.
func GetBrainPlan(db *sql.DB, id string) (*brain.Plan, error) {
	row := db.QueryRow(`
		SELECT id, title, query, summary, source, status,
		       project_slug, work_dir, branch_policy, error,
		       approved_at, executed_at, completed_at, cancelled_at, rejected_at, blocked_at,
		       created_at, updated_at, plan_json
		FROM brain_plans WHERE id = ?
	`, id)
	return scanBrainPlan(row)
}

// ListBrainPlans returns persisted plans, newest updated first.
func ListBrainPlans(db *sql.DB, filter BrainPlanFilter) ([]*brain.Plan, error) {
	var where []string
	var args []any
	if filter.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.Project != "" {
		where = append(where, "project_slug = ?")
		args = append(args, filter.Project)
	}
	if filter.Source != "" {
		where = append(where, "source = ?")
		args = append(args, filter.Source)
	}
	q := `
		SELECT id, title, query, summary, source, status,
		       project_slug, work_dir, branch_policy, error,
		       approved_at, executed_at, completed_at, cancelled_at, rejected_at, blocked_at,
		       created_at, updated_at, plan_json
		FROM brain_plans
	`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY updated_at DESC, created_at DESC, id DESC"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list brain plans: %w", err)
	}
	defer rows.Close()

	var out []*brain.Plan
	for rows.Next() {
		plan, err := scanBrainPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, plan)
	}
	return out, rows.Err()
}

func scanBrainPlan(row interface{ Scan(dest ...any) error }) (*brain.Plan, error) {
	var r brainPlanRow
	if err := row.Scan(
		&r.ID, &r.Title, &r.Query, &r.Summary, &r.Source, &r.Status,
		&r.ProjectSlug, &r.WorkDir, &r.BranchPolicy, &r.Error,
		&r.ApprovedAt, &r.ExecutedAt, &r.CompletedAt, &r.CancelledAt, &r.RejectedAt, &r.BlockedAt,
		&r.CreatedAt, &r.UpdatedAt, &r.PlanJSON,
	); err != nil {
		return nil, err
	}
	var plan brain.Plan
	if err := json.Unmarshal([]byte(r.PlanJSON), &plan); err != nil {
		return nil, fmt.Errorf("decode brain plan %s: %w", r.ID, err)
	}
	if plan.ID == "" {
		plan.ID = r.ID
	}
	if plan.Status == "" {
		plan.Status = brain.Status(r.Status)
	}
	if plan.Title == "" {
		plan.Title = r.Title
	}
	if plan.Query == "" {
		plan.Query = r.Query
	}
	if plan.Summary == "" {
		plan.Summary = r.Summary
	}
	if plan.Source == "" {
		plan.Source = r.Source
	}
	if plan.Project == "" && r.ProjectSlug.Valid {
		plan.Project = r.ProjectSlug.String
	}
	if plan.WorkDir == "" && r.WorkDir.Valid {
		plan.WorkDir = r.WorkDir.String
	}
	if plan.BranchPolicy == "" && r.BranchPolicy.Valid {
		plan.BranchPolicy = r.BranchPolicy.String
	}
	if plan.Error == "" && r.Error.Valid {
		plan.Error = r.Error.String
	}
	if plan.CreatedAt == "" {
		plan.CreatedAt = r.CreatedAt
	}
	if plan.UpdatedAt == "" {
		plan.UpdatedAt = r.UpdatedAt
	}
	if plan.ApprovedAt == nil && r.ApprovedAt.Valid {
		plan.ApprovedAt = ptrString(r.ApprovedAt.String)
	}
	if plan.ExecutedAt == nil && r.ExecutedAt.Valid {
		plan.ExecutedAt = ptrString(r.ExecutedAt.String)
	}
	if plan.CompletedAt == nil && r.CompletedAt.Valid {
		plan.CompletedAt = ptrString(r.CompletedAt.String)
	}
	if plan.CancelledAt == nil && r.CancelledAt.Valid {
		plan.CancelledAt = ptrString(r.CancelledAt.String)
	}
	if plan.RejectedAt == nil && r.RejectedAt.Valid {
		plan.RejectedAt = ptrString(r.RejectedAt.String)
	}
	if plan.BlockedAt == nil && r.BlockedAt.Valid {
		plan.BlockedAt = ptrString(r.BlockedAt.String)
	}
	return &plan, nil
}

func nullStringPtr(v *string) any {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil
	}
	return s
}

func ptrString(s string) *string {
	v := s
	return &v
}
