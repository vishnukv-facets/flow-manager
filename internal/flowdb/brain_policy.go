package flowdb

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const (
	BrainPolicyModeAuto             = "auto"
	BrainPolicyModeApprovalRequired = "approval_required"
)

// BrainRiskyActions is the stable policy order used by storage and graph views.
var BrainRiskyActions = []string{
	"merge",
	"deploy",
	"force_push",
	"destructive_shell",
	"delete_branch",
	"outbound_reply",
}

type BrainPolicy struct {
	FullAuto       bool
	ActionModes    map[string]string
	RiskyWhitelist []string
	RequiresReview []string
}

func (p BrainPolicy) IsWhitelisted(action string) bool {
	action = normalizeBrainPolicyAction(action)
	if action == "" {
		return false
	}
	return p.ActionModes[action] == BrainPolicyModeAuto
}

func GetBrainPolicy(db *sql.DB) (BrainPolicy, error) {
	policy := BrainPolicy{
		FullAuto:    true,
		ActionModes: make(map[string]string, len(BrainRiskyActions)),
	}
	for _, action := range BrainRiskyActions {
		policy.ActionModes[action] = BrainPolicyModeApprovalRequired
	}

	rows, err := db.Query(`SELECT action, mode FROM brain_policy ORDER BY action`)
	if err != nil {
		return policy, fmt.Errorf("get brain policy: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var action, mode string
		if err := rows.Scan(&action, &mode); err != nil {
			return policy, err
		}
		action = normalizeBrainPolicyAction(action)
		if !isBrainRiskyAction(action) || !isBrainPolicyMode(mode) {
			continue
		}
		policy.ActionModes[action] = mode
	}
	if err := rows.Err(); err != nil {
		return policy, err
	}

	for _, action := range BrainRiskyActions {
		switch policy.ActionModes[action] {
		case BrainPolicyModeAuto:
			policy.RiskyWhitelist = append(policy.RiskyWhitelist, action)
		default:
			policy.ActionModes[action] = BrainPolicyModeApprovalRequired
			policy.RequiresReview = append(policy.RequiresReview, action)
		}
	}
	return policy, nil
}

func SetBrainPolicyMode(db *sql.DB, action, mode, now string) error {
	action = normalizeBrainPolicyAction(action)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if !isBrainRiskyAction(action) {
		return fmt.Errorf("unknown brain policy action %q", action)
	}
	if !isBrainPolicyMode(mode) {
		return fmt.Errorf("invalid brain policy mode %q", mode)
	}
	now = strings.TrimSpace(now)
	if now == "" {
		now = NowISO()
	}
	_, err := db.Exec(
		`INSERT INTO brain_policy (action, mode, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(action) DO UPDATE SET
		     mode = excluded.mode,
		     updated_at = excluded.updated_at`,
		action, mode, now,
	)
	if err != nil {
		return fmt.Errorf("set brain policy mode: %w", err)
	}
	return nil
}

type BrainActionAudit struct {
	ID           string
	Action       string
	TargetType   string
	TargetID     string
	Actor        string
	Policy       string
	EvidenceJSON string
	Result       string
	ErrorText    sql.NullString
	CreatedAt    string
}

const brainActionAuditCols = "id, action, target_type, target_id, actor, policy, evidence_json, result, error_text, created_at"

func InsertBrainActionAudit(db *sql.DB, audit *BrainActionAudit) error {
	if audit == nil {
		return errors.New("brain action audit is nil")
	}
	if strings.TrimSpace(audit.ID) == "" {
		return errors.New("brain action audit id is required")
	}
	if strings.TrimSpace(audit.Action) == "" {
		return errors.New("brain action audit action is required")
	}
	if strings.TrimSpace(audit.TargetType) == "" {
		return errors.New("brain action audit target type is required")
	}
	if strings.TrimSpace(audit.TargetID) == "" {
		return errors.New("brain action audit target id is required")
	}
	if strings.TrimSpace(audit.Actor) == "" {
		return errors.New("brain action audit actor is required")
	}
	if strings.TrimSpace(audit.Policy) == "" {
		return errors.New("brain action audit policy is required")
	}
	if strings.TrimSpace(audit.Result) == "" {
		return errors.New("brain action audit result is required")
	}
	if strings.TrimSpace(audit.EvidenceJSON) == "" {
		audit.EvidenceJSON = "{}"
	}
	if strings.TrimSpace(audit.CreatedAt) == "" {
		audit.CreatedAt = NowISO()
	}
	_, err := db.Exec(
		`INSERT INTO brain_action_audit (
			id, action, target_type, target_id, actor, policy, evidence_json, result, error_text, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		audit.ID, audit.Action, audit.TargetType, audit.TargetID, audit.Actor, audit.Policy,
		audit.EvidenceJSON, audit.Result, audit.ErrorText, audit.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert brain action audit: %w", err)
	}
	return nil
}

func ListBrainActionAudit(db *sql.DB, targetType, targetID string, limit int) ([]*BrainActionAudit, error) {
	targetType = strings.TrimSpace(targetType)
	targetID = strings.TrimSpace(targetID)
	if targetType == "" {
		return nil, errors.New("brain action audit target type is required")
	}
	if targetID == "" {
		return nil, errors.New("brain action audit target id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := db.Query(
		`SELECT `+brainActionAuditCols+` FROM brain_action_audit
		 WHERE target_type = ? AND target_id = ?
		 ORDER BY created_at DESC, id DESC
		 LIMIT ?`,
		targetType, targetID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list brain action audit: %w", err)
	}
	defer rows.Close()
	var out []*BrainActionAudit
	for rows.Next() {
		audit, err := scanBrainActionAudit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, audit)
	}
	return out, rows.Err()
}

func scanBrainActionAudit(row interface{ Scan(dest ...any) error }) (*BrainActionAudit, error) {
	var audit BrainActionAudit
	if err := row.Scan(
		&audit.ID,
		&audit.Action,
		&audit.TargetType,
		&audit.TargetID,
		&audit.Actor,
		&audit.Policy,
		&audit.EvidenceJSON,
		&audit.Result,
		&audit.ErrorText,
		&audit.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &audit, nil
}

func normalizeBrainPolicyAction(action string) string {
	return strings.ToLower(strings.TrimSpace(action))
}

func isBrainRiskyAction(action string) bool {
	for _, known := range BrainRiskyActions {
		if action == known {
			return true
		}
	}
	return false
}

func isBrainPolicyMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case BrainPolicyModeAuto, BrainPolicyModeApprovalRequired:
		return true
	default:
		return false
	}
}
