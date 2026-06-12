package flowdb

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type Owner struct {
	Slug           string
	Name           string
	WorkDir        string
	ProjectSlug    sql.NullString
	Status         string
	Every          string
	NextWakeAt     sql.NullString
	LastTickAt     sql.NullString
	LastTickStatus sql.NullString
	TickPID        sql.NullInt64
	TickStarted    sql.NullString
	Harness        string
	CreatedAt      string
	UpdatedAt      string
	ArchivedAt     sql.NullString
}

type OwnerFilter struct {
	Status          string
	IncludeArchived bool
}

const OwnerCols = "slug, name, work_dir, project_slug, status, every, next_wake_at, last_tick_at, last_tick_status, tick_pid, tick_started, harness, created_at, updated_at, archived_at"

func ScanOwner(row interface{ Scan(dest ...any) error }) (*Owner, error) {
	var o Owner
	var harness sql.NullString
	err := row.Scan(
		&o.Slug, &o.Name, &o.WorkDir, &o.ProjectSlug, &o.Status, &o.Every,
		&o.NextWakeAt, &o.LastTickAt, &o.LastTickStatus, &o.TickPID, &o.TickStarted, &harness,
		&o.CreatedAt, &o.UpdatedAt, &o.ArchivedAt,
	)
	if err != nil {
		return nil, err
	}
	if harness.Valid && strings.TrimSpace(harness.String) != "" {
		o.Harness = harness.String
	} else {
		o.Harness = "claude"
	}
	return &o, nil
}

func CreateOwner(db *sql.DB, o *Owner) error {
	now := NowISO()
	if o.CreatedAt == "" {
		o.CreatedAt = now
	}
	o.UpdatedAt = now
	if o.Status == "" {
		o.Status = "active"
	}
	harnessName, err := NormalizeHarnessName(o.Harness)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
		INSERT INTO owners (slug, name, work_dir, project_slug, status, every,
			next_wake_at, last_tick_at, last_tick_status, tick_pid, tick_started, harness, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		o.Slug, o.Name, o.WorkDir, o.ProjectSlug, o.Status, o.Every,
		o.NextWakeAt, o.LastTickAt, o.LastTickStatus, o.TickPID, o.TickStarted, harnessName, o.CreatedAt, o.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create owner %s: %w", o.Slug, err)
	}
	o.Harness = harnessName
	return nil
}

func GetOwner(db *sql.DB, slug string) (*Owner, error) {
	row := db.QueryRow("SELECT "+OwnerCols+" FROM owners WHERE slug = ?", slug)
	return ScanOwner(row)
}

func UpdateOwner(db *sql.DB, o *Owner) error {
	o.UpdatedAt = NowISO()
	harnessName, err := NormalizeHarnessName(o.Harness)
	if err != nil {
		return err
	}
	res, err := db.Exec(`
		UPDATE owners SET
			name = ?, work_dir = ?, project_slug = ?, status = ?, every = ?,
			next_wake_at = ?, last_tick_at = ?, last_tick_status = ?,
			tick_pid = ?, tick_started = ?, harness = ?, updated_at = ?
		WHERE slug = ?`,
		o.Name, o.WorkDir, o.ProjectSlug, o.Status, o.Every,
		o.NextWakeAt, o.LastTickAt, o.LastTickStatus, o.TickPID, o.TickStarted, harnessName, o.UpdatedAt, o.Slug,
	)
	if err != nil {
		return fmt.Errorf("update owner %s: %w", o.Slug, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("update owner %s: no such owner", o.Slug)
	}
	o.Harness = harnessName
	return nil
}

func ListOwners(db *sql.DB, filter OwnerFilter) ([]*Owner, error) {
	var where []string
	var args []any
	if filter.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filter.Status)
	}
	if !filter.IncludeArchived {
		where = append(where, "archived_at IS NULL")
	}
	q := "SELECT " + OwnerCols + " FROM owners"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY slug"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list owners: %w", err)
	}
	defer rows.Close()
	var out []*Owner
	for rows.Next() {
		o, err := ScanOwner(rows)
		if err != nil {
			return nil, fmt.Errorf("scan owner: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func ActivateOwner(db *sql.DB, slug, nextWakeAt string) error {
	res, err := db.Exec(
		`UPDATE owners SET status='active', next_wake_at=?, archived_at=NULL, updated_at=? WHERE slug=?`,
		nextWakeAt, NowISO(), slug,
	)
	return affectedOwnerRow(res, err, "activate", slug)
}

func PauseOwner(db *sql.DB, slug string) error {
	res, err := db.Exec(
		`UPDATE owners SET status='paused', updated_at=? WHERE slug=?`,
		NowISO(), slug,
	)
	return affectedOwnerRow(res, err, "pause", slug)
}

func SetOwnerNextWake(db *sql.DB, slug, nextWakeAt string) error {
	res, err := db.Exec(
		`UPDATE owners SET next_wake_at=?, updated_at=? WHERE slug=?`,
		nextWakeAt, NowISO(), slug,
	)
	return affectedOwnerRow(res, err, "set next wake", slug)
}

func RetireOwner(db *sql.DB, slug string) error {
	now := NowISO()
	res, err := db.Exec(
		`UPDATE owners SET status='retired', archived_at=COALESCE(archived_at, ?), updated_at=? WHERE slug=?`,
		now, now, slug,
	)
	return affectedOwnerRow(res, err, "retire", slug)
}

func DeleteOwner(db *sql.DB, slug string) error {
	res, err := db.Exec(`DELETE FROM owners WHERE slug=?`, slug)
	return affectedOwnerRow(res, err, "delete", slug)
}

func DueOwners(db *sql.DB, nowISO string) ([]*Owner, error) {
	now, err := time.Parse(time.RFC3339, nowISO)
	if err != nil {
		return nil, fmt.Errorf("due owners: parse now %q: %w", nowISO, err)
	}
	owners, err := ListOwners(db, OwnerFilter{Status: "active"})
	if err != nil {
		return nil, err
	}
	var out []*Owner
	for _, o := range owners {
		if !o.NextWakeAt.Valid || o.NextWakeAt.String == "" {
			continue
		}
		wake, err := time.Parse(time.RFC3339, o.NextWakeAt.String)
		if err != nil {
			continue
		}
		if !wake.After(now) {
			out = append(out, o)
		}
	}
	return out, nil
}

func affectedOwnerRow(res sql.Result, err error, op, slug string) error {
	if err != nil {
		return fmt.Errorf("%s owner %s: %w", op, slug, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%s owner %s: no such owner", op, slug)
	}
	return nil
}
