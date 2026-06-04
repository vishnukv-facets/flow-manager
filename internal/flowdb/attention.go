package flowdb

import (
	"database/sql"
	"fmt"
)

// FeedItem mirrors a row in the attention_feed table (spec §7). It is the
// durable record of one triage candidate surfaced to the operator.
type FeedItem struct {
	ID                string
	Source            string
	ThreadKey         string
	Summary           string
	SuggestedAction   string
	MatchedTask       string
	SuggestedProject  string
	SuggestedPriority string
	Urgency           string
	IsVIP             bool
	Confidence        float64
	Draft             string
	Reason            string
	ContextJSON       string
	Status            string // new|acted|dismissed|snoozed|deferred
	SnoozeUntil       string
	CreatedAt         string // RFC3339
	ActedAt           string // RFC3339, set when status leaves 'new'
}

// UpsertFeedItem inserts a feed item, coalescing by thread_key: if a row for
// the same thread_key already exists with status 'new', that row is updated
// in place (and its existing id is returned) instead of creating a duplicate
// card. Otherwise the item is inserted as given. Returns the id of the row
// written.
func UpsertFeedItem(db *sql.DB, item FeedItem) (string, error) {
	if item.ID == "" || item.ThreadKey == "" || item.SuggestedAction == "" {
		return "", fmt.Errorf("flowdb: feed item requires id, thread_key, suggested_action")
	}
	if item.Status == "" {
		item.Status = "new"
	}

	var existingID string
	err := db.QueryRow(
		`SELECT id FROM attention_feed WHERE thread_key = ? AND status = 'new' LIMIT 1`,
		item.ThreadKey,
	).Scan(&existingID)
	switch {
	case err == sql.ErrNoRows:
		// fall through to insert
	case err != nil:
		return "", fmt.Errorf("flowdb: lookup feed coalesce: %w", err)
	default:
		_, uerr := db.Exec(
			`UPDATE attention_feed SET
			   source=?, summary=?, suggested_action=?, matched_task=?,
			   suggested_project=?, suggested_priority=?, urgency=?, is_vip=?,
			   confidence=?, draft=?, reason=?, context_json=?
			 WHERE id=?`,
			item.Source, item.Summary, item.SuggestedAction, NullIfEmpty(item.MatchedTask),
			NullIfEmpty(item.SuggestedProject), NullIfEmpty(item.SuggestedPriority), NullIfEmpty(item.Urgency), boolToInt(item.IsVIP),
			item.Confidence, NullIfEmpty(item.Draft), NullIfEmpty(item.Reason), NullIfEmpty(item.ContextJSON),
			existingID,
		)
		if uerr != nil {
			return "", fmt.Errorf("flowdb: coalesce feed item: %w", uerr)
		}
		return existingID, nil
	}

	_, err = db.Exec(
		`INSERT INTO attention_feed (
		   id, source, thread_key, summary, suggested_action, matched_task,
		   suggested_project, suggested_priority, urgency, is_vip, confidence,
		   draft, reason, context_json, status, snooze_until, created_at, acted_at
		 ) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.Source, item.ThreadKey, item.Summary, item.SuggestedAction, NullIfEmpty(item.MatchedTask),
		NullIfEmpty(item.SuggestedProject), NullIfEmpty(item.SuggestedPriority), NullIfEmpty(item.Urgency), boolToInt(item.IsVIP), item.Confidence,
		NullIfEmpty(item.Draft), NullIfEmpty(item.Reason), NullIfEmpty(item.ContextJSON), item.Status, NullIfEmpty(item.SnoozeUntil), item.CreatedAt, NullIfEmpty(item.ActedAt),
	)
	if err != nil {
		return "", fmt.Errorf("flowdb: insert feed item: %w", err)
	}
	return item.ID, nil
}

// ListFeedItems returns feed rows, newest first. An empty status returns all
// rows; otherwise it filters to that status.
func ListFeedItems(db *sql.DB, status string) ([]FeedItem, error) {
	q := `SELECT id, source, thread_key, summary, suggested_action, matched_task,
	             suggested_project, suggested_priority, urgency, is_vip, confidence,
	             draft, reason, context_json, status, snooze_until, created_at, acted_at
	      FROM attention_feed`
	args := []any{}
	if status != "" {
		q += ` WHERE status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY created_at DESC, id DESC`

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("flowdb: list feed items: %w", err)
	}
	defer rows.Close()

	var out []FeedItem
	for rows.Next() {
		var it FeedItem
		var matched, project, priority, urgency, draft, reason, ctx, snooze, acted sql.NullString
		var isVIP int
		if err := rows.Scan(
			&it.ID, &it.Source, &it.ThreadKey, &it.Summary, &it.SuggestedAction, &matched,
			&project, &priority, &urgency, &isVIP, &it.Confidence,
			&draft, &reason, &ctx, &it.Status, &snooze, &it.CreatedAt, &acted,
		); err != nil {
			return nil, fmt.Errorf("flowdb: scan feed item: %w", err)
		}
		it.MatchedTask = matched.String
		it.SuggestedProject = project.String
		it.SuggestedPriority = priority.String
		it.Urgency = urgency.String
		it.IsVIP = isVIP != 0
		it.Draft = draft.String
		it.Reason = reason.String
		it.ContextJSON = ctx.String
		it.SnoozeUntil = snooze.String
		it.ActedAt = acted.String
		out = append(out, it)
	}
	return out, rows.Err()
}

// SetFeedItemStatus moves a feed item to a new lifecycle status and stamps
// acted_at. Used when the operator (or an autonomous action) resolves a card.
func SetFeedItemStatus(db *sql.DB, id, status, actedAt string) error {
	res, err := db.Exec(
		`UPDATE attention_feed SET status = ?, acted_at = ? WHERE id = ?`,
		status, NullIfEmpty(actedAt), id,
	)
	if err != nil {
		return fmt.Errorf("flowdb: set feed status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("flowdb: no feed item with id %q", id)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
