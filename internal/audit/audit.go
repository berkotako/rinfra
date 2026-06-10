// Package audit defines the append-only audit trail. Every privileged action
// (deploy, teardown, scenario run, credential use, scope change) must emit an
// Event. The log is IMMUTABLE: there are intentionally no update or delete
// operations in this interface.
package audit

import (
	"context"
	"time"
)

// Event is a single audit record. Never store raw secrets (credentials,
// license keys, connection strings) in any field — redact before recording.
type Event struct {
	ID           string
	EngagementID string
	Actor        string // who performed the action (operator identity)
	Action       string // e.g. "infra.deploy", "infra.teardown", "scenario.start"
	Target       string // what was acted on (node id, scenario id, ...)
	Detail       string // short, non-sensitive context
	At           time.Time
}

// Logger is the append-only audit sink. Implementations write to durable,
// tamper-evident storage. Record must not block the caller indefinitely.
type Logger interface {
	Record(ctx context.Context, e Event) error
}

// Reader provides read access to the audit log. It is separate from Logger to
// keep the write path minimal and to allow the HTTP audit endpoint to be wired
// to any backing implementation.
type Reader interface {
	// List returns audit events for an engagement, ordered by time descending.
	// limit and offset provide pagination.
	List(ctx context.Context, engagementID string, limit, offset int) ([]Event, error)
}
