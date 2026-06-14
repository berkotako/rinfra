// Package store defines persistence interfaces. Implementations live behind
// these (Postgres via pgx/v5). Keeping them as interfaces lets services be
// tested against in-memory fakes.
package store

import (
	"context"
	"errors"

	"github.com/rinfra/rinfra/internal/domain"
)

// ErrNotFound is returned by Get methods when the requested record does not
// exist. Callers should use errors.Is(err, store.ErrNotFound) to detect this.
var ErrNotFound = errors.New("record not found")

// EngagementStore persists engagements and their authorization state.
type EngagementStore interface {
	Create(ctx context.Context, e domain.Engagement) (string, error)
	Get(ctx context.Context, id string) (domain.Engagement, error)
	List(ctx context.Context) ([]domain.Engagement, error)
	UpdateStatus(ctx context.Context, id string, status domain.EngagementStatus) error
	// Update replaces the full engagement record (used when authorization fields
	// or metadata change). The engagement must already exist.
	Update(ctx context.Context, e domain.Engagement) error
	// ListForProject returns all engagements belonging to a project.
	ListForProject(ctx context.Context, projectID string) ([]domain.Engagement, error)
}

// InfraStore persists topology (nodes + edges) and their live status. The
// stored status is the source of truth reconciled against actual cloud state
// during teardown.
type InfraStore interface {
	// SaveTopology upserts all nodes and edges for an engagement in a single
	// transaction. Nodes absent from t.Nodes but present in the DB are removed.
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
	// SaveResult appends a per-technique Result to a run. This enables
	// incremental persistence as each technique completes rather than requiring
	// a full SaveRun after the run finishes.
	SaveResult(ctx context.Context, runID string, result domain.Result) error
}

// CredentialStore persists envelope-encrypted credentials keyed by engagement
// and provider. The raw ciphertext is write-only from the caller's perspective;
// reads return only metadata. The Postgres implementation holds the actual bytes.
type CredentialStore interface {
	// Upsert stores or replaces encrypted credentials for the given engagement
	// and provider. ciphertext and nonce are the AES-256-GCM output from the
	// secrets package; keyID identifies the wrapping data-key.
	Upsert(ctx context.Context, engagementID, provider string, ciphertext, nonce []byte, keyID string) error

	// GetCiphertext returns the raw ciphertext and nonce for decryption.
	// Callers must not log or surface these bytes.
	GetCiphertext(ctx context.Context, engagementID, provider string) (ciphertext, nonce []byte, keyID string, err error)

	// TouchLastUsed records that credentials were accessed now.
	TouchLastUsed(ctx context.Context, engagementID, provider string) error

	// GetMeta returns non-sensitive metadata (no ciphertext) for the given
	// engagement and provider.
	GetMeta(ctx context.Context, engagementID, provider string) (domain.CredentialMeta, error)

	// ListForEngagement returns metadata for all credentials stored for an
	// engagement, ordered by provider.
	ListForEngagement(ctx context.Context, engagementID string) ([]domain.CredentialMeta, error)
}

// JobStore persists durable background-job records. Jobs survive server restarts;
// the boot-time reconciler calls ListRunning to re-adopt in-flight work.
type JobStore interface {
	Create(ctx context.Context, j domain.Job) (string, error)
	Get(ctx context.Context, id string) (domain.Job, error)
	UpdateStatus(ctx context.Context, id string, status domain.JobStatus, errText string) error
	// ListRunning returns all jobs whose status is JobRunning, used during
	// boot-time reconciliation to detect orphaned jobs from a prior server
	// instance.
	ListRunning(ctx context.Context) ([]domain.Job, error)
}

// UserStore persists operator user accounts.
type UserStore interface {
	Create(ctx context.Context, u domain.User) (id string, err error)
	GetByID(ctx context.Context, id string) (domain.User, error)
	GetByUsername(ctx context.Context, username string) (domain.User, error)
	List(ctx context.Context) ([]domain.User, error)
	ListByManager(ctx context.Context, managerID string) ([]domain.User, error)
	Update(ctx context.Context, u domain.User) error
	SetPassword(ctx context.Context, id, passwordHash string) error
	CountAll(ctx context.Context) (int, error)
}

// ProjectStore persists projects and their membership lists.
type ProjectStore interface {
	Create(ctx context.Context, p domain.Project) (id string, err error)
	Get(ctx context.Context, id string) (domain.Project, error)
	List(ctx context.Context) ([]domain.Project, error)
	ListForUser(ctx context.Context, userID string) ([]domain.Project, error)
	Update(ctx context.Context, p domain.Project) error
	Delete(ctx context.Context, id string) error
	AddMember(ctx context.Context, projectID, userID string) error
	RemoveMember(ctx context.Context, projectID, userID string) error
	ListMembers(ctx context.Context, projectID string) ([]domain.ProjectMember, error)
	IsMember(ctx context.Context, projectID, userID string) (bool, error)
}

// SessionStore persists authentication sessions. Sessions expire; the store
// does not automatically purge expired rows — callers must check ExpiresAt.
type SessionStore interface {
	Create(ctx context.Context, s domain.Session) error
	GetByTokenHash(ctx context.Context, tokenHash string) (domain.Session, error)
	Delete(ctx context.Context, tokenHash string) error
	DeleteForUser(ctx context.Context, userID string) error
}
