package flowdb

import (
	"database/sql"
	"fmt"
)

// SteeringTrace is one row of the attention-router decision log: the full
// journey of a single observed event through the triage cascade.
type SteeringTrace struct {
	ID, CreatedAt, Origin, Source         string
	Channel, ChannelType, Author          string
	ThreadKey, TextPreview                string
	Disposition, StageReached, DropReason string // disposition: dropped|surfaced|error
	Stage1Relevant                        *bool   // nil = stage 1 not reached
	Stage2Action                          string
	Stage2Confidence                      float64
	Stage3Action                          string
	Stage3Confidence                      float64
	FinalAction                           string
	FinalConfidence                       float64
	FeedItemID, Error                     string
	LatencyMS                             int64
	Model                                 string
}

// InsertSteeringTrace writes one decision-log row.
func InsertSteeringTrace(db *sql.DB, t SteeringTrace) error {
	var s1 any
	if t.Stage1Relevant != nil {
		if *t.Stage1Relevant {
			s1 = 1
		} else {
			s1 = 0
		}
	}
	_, err := db.Exec(`
		INSERT INTO steering_trace (
			id, created_at, origin, source,
			channel, channel_type, author, thread_key, text_preview,
			disposition, stage_reached, drop_reason,
			stage1_relevant,
			stage2_action, stage2_confidence,
			stage3_action, stage3_confidence,
			final_action, final_confidence,
			feed_item_id, error,
			latency_ms, model
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.CreatedAt, t.Origin, t.Source,
		NullIfEmpty(t.Channel), NullIfEmpty(t.ChannelType), NullIfEmpty(t.Author), NullIfEmpty(t.ThreadKey), NullIfEmpty(t.TextPreview),
		t.Disposition, t.StageReached, NullIfEmpty(t.DropReason),
		s1,
		NullIfEmpty(t.Stage2Action), t.Stage2Confidence,
		NullIfEmpty(t.Stage3Action), t.Stage3Confidence,
		NullIfEmpty(t.FinalAction), t.FinalConfidence,
		NullIfEmpty(t.FeedItemID), NullIfEmpty(t.Error),
		t.LatencyMS, NullIfEmpty(t.Model),
	)
	if err != nil {
		return fmt.Errorf("flowdb: insert steering trace: %w", err)
	}
	return nil
}

// TraceFilter narrows ListSteeringTrace.
type TraceFilter struct {
	Disposition string // "" = all
	Since       string // RFC3339 lower bound on created_at; "" = no bound
	Limit       int    // <=0 → 200
}

// ListSteeringTrace returns rows newest-first, filtered.
func ListSteeringTrace(db *sql.DB, f TraceFilter) ([]SteeringTrace, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 200
	}

	q := `SELECT
		id, created_at, origin, source,
		channel, channel_type, author, thread_key, text_preview,
		disposition, stage_reached, drop_reason,
		stage1_relevant,
		stage2_action, stage2_confidence,
		stage3_action, stage3_confidence,
		final_action, final_confidence,
		feed_item_id, error,
		latency_ms, model
	FROM steering_trace`

	args := []any{}
	conditions := []string{}

	if f.Disposition != "" {
		conditions = append(conditions, "disposition = ?")
		args = append(args, f.Disposition)
	}
	if f.Since != "" {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, f.Since)
	}
	if len(conditions) > 0 {
		q += " WHERE "
		for i, c := range conditions {
			if i > 0 {
				q += " AND "
			}
			q += c
		}
	}
	q += " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("flowdb: list steering traces: %w", err)
	}
	defer rows.Close()

	var out []SteeringTrace
	for rows.Next() {
		var tr SteeringTrace
		var channel, channelType, author, threadKey, textPreview, dropReason sql.NullString
		var stage2Action, stage3Action, finalAction, feedItemID, errStr, model sql.NullString
		var stage1Rel sql.NullInt64
		var stage2Conf, stage3Conf, finalConf sql.NullFloat64

		if err := rows.Scan(
			&tr.ID, &tr.CreatedAt, &tr.Origin, &tr.Source,
			&channel, &channelType, &author, &threadKey, &textPreview,
			&tr.Disposition, &tr.StageReached, &dropReason,
			&stage1Rel,
			&stage2Action, &stage2Conf,
			&stage3Action, &stage3Conf,
			&finalAction, &finalConf,
			&feedItemID, &errStr,
			&tr.LatencyMS, &model,
		); err != nil {
			return nil, fmt.Errorf("flowdb: scan steering trace: %w", err)
		}

		tr.Channel = channel.String
		tr.ChannelType = channelType.String
		tr.Author = author.String
		tr.ThreadKey = threadKey.String
		tr.TextPreview = textPreview.String
		tr.DropReason = dropReason.String
		tr.Stage2Action = stage2Action.String
		tr.Stage2Confidence = stage2Conf.Float64
		tr.Stage3Action = stage3Action.String
		tr.Stage3Confidence = stage3Conf.Float64
		tr.FinalAction = finalAction.String
		tr.FinalConfidence = finalConf.Float64
		tr.FeedItemID = feedItemID.String
		tr.Error = errStr.String
		tr.Model = model.String

		if stage1Rel.Valid {
			v := stage1Rel.Int64 == 1
			tr.Stage1Relevant = &v
		}

		out = append(out, tr)
	}
	return out, rows.Err()
}

// SteeringFunnel is the aggregate funnel over a time window.
type SteeringFunnel struct {
	Observed      int
	DroppedStage0 int
	DroppedCache  int
	DroppedStage1 int
	DroppedStage2 int
	Surfaced      int
	Errors        int
}

// SteeringFunnelSince returns funnel counts for rows with created_at >= since
// (since == "" → all rows).
func SteeringFunnelSince(db *sql.DB, since string) (SteeringFunnel, error) {
	q := `SELECT disposition, stage_reached, COUNT(*) FROM steering_trace`
	args := []any{}
	if since != "" {
		q += " WHERE created_at >= ?"
		args = append(args, since)
	}
	q += " GROUP BY disposition, stage_reached"

	rows, err := db.Query(q, args...)
	if err != nil {
		return SteeringFunnel{}, fmt.Errorf("flowdb: steering funnel: %w", err)
	}
	defer rows.Close()

	var f SteeringFunnel
	for rows.Next() {
		var disposition, stageReached string
		var count int
		if err := rows.Scan(&disposition, &stageReached, &count); err != nil {
			return SteeringFunnel{}, fmt.Errorf("flowdb: scan steering funnel: %w", err)
		}
		f.Observed += count
		switch disposition {
		case "surfaced":
			f.Surfaced += count
		case "error":
			f.Errors += count
		case "dropped":
			switch stageReached {
			case "stage0":
				f.DroppedStage0 += count
			case "cache":
				f.DroppedCache += count
			case "stage1":
				f.DroppedStage1 += count
			case "stage2":
				f.DroppedStage2 += count
			}
		}
	}
	return f, rows.Err()
}
