// Package store defines persistence interfaces. Implementations live behind
// these (Postgres via pgx/sqlc). Keeping them as interfaces lets services be
// tested against in-memory fakes.
package store

import (
	"context"

	"github.com/rinfra/rinfra/internal/domain"
)

// EngagementStore persists engagements and their authorization state.
type EngagementStore interface {
	Create(ctx context.Context, e domain.Engagement) (string, error)
	Get(ctx context.Context, id string) (domain.Engagement, error)
	List(ctx context.Context) ([]domain.Engagement, error)
	UpdateStatus(ctx context.Context, id string, status domain.EngagementStatus) error
}

// InfraStore persists topology (nodes + edges) and their live status. The
// stored status is the source of truth reconciled against actual cloud state
// during teardown.
type InfraStore interface {
	SaveTopology(ctx context.Context, t domain.Topology) error
	GetTopology(ctx context.Context, engagementID string) (domain.Topology, error)
	UpdateNodeStatus(ctx context.Context, nodeID string, status domain.NodeStatus, health domain.Health) error
	NodesForEngagement(ctx context.Context, engagementID string) ([]domain.Node, error)
}

// ScenarioStore persists emulation runs and their per-technique results.
type ScenarioStore interface {
	SaveRun(ctx context.Context, run domain.ScenarioRun) (string, error)
	GetRun(ctx context.Context, id string) (domain.ScenarioRun, error)
	RunsForEngagement(ctx context.Context, engagementID string) ([]domain.ScenarioRun, error)
}
