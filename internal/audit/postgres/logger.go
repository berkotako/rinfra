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
