// Package postgres provides a Postgres-backed append-only implementation of
// audit.Logger. This package contains ONLY INSERT statements — no UPDATE or
// DELETE paths exist, enforcing the audit-log immutability invariant at the
// application layer. The database trigger in migration 000003 enforces the same
// rule at the DB layer.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rinfra/rinfra/internal/audit"
)

// Logger is the Postgres audit.Logger implementation.
type Logger struct {
	pool *pgxpool.Pool
}

// New returns a Logger backed by pool.
func New(pool *pgxpool.Pool) *Logger {
	return &Logger{pool: pool}
}

var _ audit.Logger = (*Logger)(nil)

var _ audit.Reader = (*Logger)(nil)

// List returns audit events for an engagement ordered by time descending with
// pagination support. Implements audit.Reader.
func (l *Logger) List(ctx context.Context, engagementID string, limit, offset int) ([]audit.Event, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := l.pool.Query(ctx, `
		SELECT id, engagement_id, actor, action, target, detail, at
		FROM audit_events
		WHERE ($1::uuid IS NULL OR engagement_id = $1)
		ORDER BY at DESC
		LIMIT $2 OFFSET $3`,
		nullStr(engagementID), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()

	var out []audit.Event
	for rows.Next() {
		var e audit.Event
		var engID *string
		if err = rows.Scan(&e.ID, &engID, &e.Actor, &e.Action, &e.Target, &e.Detail, &e.At); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		if engID != nil {
			e.EngagementID = *engID
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Record inserts a single audit event. It is INSERT-only: this method never
// issues UPDATE or DELETE statements.
func (l *Logger) Record(ctx context.Context, e audit.Event) error {
	var engID *string
	if e.EngagementID != "" {
		engID = &e.EngagementID
	}
	_, err := l.pool.Exec(ctx, `
		INSERT INTO audit_events (engagement_id, actor, action, target, detail, at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		engID, e.Actor, e.Action, e.Target, e.Detail, e.At)
	if err != nil {
		return fmt.Errorf("record audit event: %w", err)
	}
	return nil
}
