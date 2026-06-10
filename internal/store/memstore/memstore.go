// Package memstore provides in-memory implementations of every store interface
// and of audit.Logger. All implementations are safe for concurrent use.
//
// Intended uses:
//   - Service unit tests (no database required).
//   - --dev mode: run the server locally without Postgres.
package memstore

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rinfra/rinfra/internal/audit"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/store"
)

// --- EngagementStore ---

// EngagementStore is the in-memory implementation of store.EngagementStore.
type EngagementStore struct {
	mu   sync.RWMutex
	rows map[string]domain.Engagement
}

// NewEngagementStore returns an empty EngagementStore.
func NewEngagementStore() *EngagementStore {
	return &EngagementStore{rows: make(map[string]domain.Engagement)}
}

var _ store.EngagementStore = (*EngagementStore)(nil)

// Create inserts e, generating a UUID if e.ID is empty, and returns the ID.
func (s *EngagementStore) Create(_ context.Context, e domain.Engagement) (string, error) {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	e.CreatedAt = now
	e.UpdatedAt = now

	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[e.ID] = e
	return e.ID, nil
}

// Get returns the engagement with the given ID or ErrNotFound.
func (s *EngagementStore) Get(_ context.Context, id string) (domain.Engagement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.rows[id]
	if !ok {
		return domain.Engagement{}, fmt.Errorf("engagement %s: %w", id, store.ErrNotFound)
	}
	return e, nil
}

// List returns all engagements.
func (s *EngagementStore) List(_ context.Context) ([]domain.Engagement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Engagement, 0, len(s.rows))
	for _, e := range s.rows {
		out = append(out, e)
	}
	return out, nil
}

// UpdateStatus changes an engagement's status.
func (s *EngagementStore) UpdateStatus(_ context.Context, id string, status domain.EngagementStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.rows[id]
	if !ok {
		return fmt.Errorf("engagement %s: %w", id, store.ErrNotFound)
	}
	e.Status = status
	e.UpdatedAt = time.Now().UTC()
	s.rows[id] = e
	return nil
}

// --- InfraStore ---

// InfraStore is the in-memory implementation of store.InfraStore.
type InfraStore struct {
	mu    sync.RWMutex
	nodes map[string]domain.Node   // keyed by node ID
	edges map[string][]domain.Edge // keyed by engagement ID
}

// NewInfraStore returns an empty InfraStore.
func NewInfraStore() *InfraStore {
	return &InfraStore{
		nodes: make(map[string]domain.Node),
		edges: make(map[string][]domain.Edge),
	}
}

var _ store.InfraStore = (*InfraStore)(nil)

// SaveTopology replaces all nodes and edges for t.EngagementID.
func (s *InfraStore) SaveTopology(_ context.Context, t domain.Topology) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove existing nodes for this engagement.
	for id, n := range s.nodes {
		if n.EngagementID == t.EngagementID {
			delete(s.nodes, id)
		}
	}
	now := time.Now().UTC()
	for _, n := range t.Nodes {
		if n.ID == "" {
			n.ID = uuid.NewString()
		}
		n.EngagementID = t.EngagementID
		if n.CreatedAt.IsZero() {
			n.CreatedAt = now
		}
		n.UpdatedAt = now
		s.nodes[n.ID] = n
	}
	s.edges[t.EngagementID] = t.Edges
	return nil
}

// GetTopology returns the current topology for an engagement.
func (s *InfraStore) GetTopology(_ context.Context, engagementID string) (domain.Topology, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var nodes []domain.Node
	for _, n := range s.nodes {
		if n.EngagementID == engagementID {
			nodes = append(nodes, n)
		}
	}
	return domain.Topology{
		EngagementID: engagementID,
		Nodes:        nodes,
		Edges:        s.edges[engagementID],
	}, nil
}

// UpdateNodeStatus updates a node's status and health.
func (s *InfraStore) UpdateNodeStatus(_ context.Context, nodeID string, status domain.NodeStatus, health domain.Health) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.nodes[nodeID]
	if !ok {
		return fmt.Errorf("node %s: %w", nodeID, store.ErrNotFound)
	}
	n.Status = status
	n.Health = health
	n.UpdatedAt = time.Now().UTC()
	s.nodes[nodeID] = n
	return nil
}

// NodesForEngagement returns all nodes for the given engagement.
func (s *InfraStore) NodesForEngagement(_ context.Context, engagementID string) ([]domain.Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.Node
	for _, n := range s.nodes {
		if n.EngagementID == engagementID {
			out = append(out, n)
		}
	}
	return out, nil
}

// --- ScenarioStore ---

// ScenarioStore is the in-memory implementation of store.ScenarioStore.
type ScenarioStore struct {
	mu   sync.RWMutex
	runs map[string]domain.ScenarioRun
}

// NewScenarioStore returns an empty ScenarioStore.
func NewScenarioStore() *ScenarioStore {
	return &ScenarioStore{runs: make(map[string]domain.ScenarioRun)}
}

var _ store.ScenarioStore = (*ScenarioStore)(nil)

// SaveRun stores a ScenarioRun, generating an ID if needed.
func (s *ScenarioStore) SaveRun(_ context.Context, run domain.ScenarioRun) (string, error) {
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[run.ID] = run
	return run.ID, nil
}

// GetRun returns a ScenarioRun by ID.
func (s *ScenarioStore) GetRun(_ context.Context, id string) (domain.ScenarioRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.runs[id]
	if !ok {
		return domain.ScenarioRun{}, fmt.Errorf("scenario_run %s: %w", id, store.ErrNotFound)
	}
	return r, nil
}

// RunsForEngagement returns all runs for an engagement.
func (s *ScenarioStore) RunsForEngagement(_ context.Context, engagementID string) ([]domain.ScenarioRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.ScenarioRun
	for _, r := range s.runs {
		if r.EngagementID == engagementID {
			out = append(out, r)
		}
	}
	return out, nil
}

// --- CredentialStore ---

type credKey struct{ engagementID, provider string }

type credRow struct {
	meta       domain.CredentialMeta
	ciphertext []byte
	nonce      []byte
}

// CredentialStore is the in-memory implementation of store.CredentialStore.
type CredentialStore struct {
	mu   sync.RWMutex
	rows map[credKey]credRow
}

// NewCredentialStore returns an empty CredentialStore.
func NewCredentialStore() *CredentialStore {
	return &CredentialStore{rows: make(map[credKey]credRow)}
}

var _ store.CredentialStore = (*CredentialStore)(nil)

// Upsert stores or replaces credentials.
func (s *CredentialStore) Upsert(_ context.Context, engagementID, provider string, ciphertext, nonce []byte, keyID string) error {
	k := credKey{engagementID, provider}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, exists := s.rows[k]
	id := existing.meta.ID
	if !exists {
		id = uuid.NewString()
	}
	ct := make([]byte, len(ciphertext))
	copy(ct, ciphertext)
	n := make([]byte, len(nonce))
	copy(n, nonce)
	s.rows[k] = credRow{
		meta: domain.CredentialMeta{
			ID:           id,
			EngagementID: engagementID,
			Provider:     provider,
			KeyID:        keyID,
			CreatedAt:    time.Now().UTC(),
		},
		ciphertext: ct,
		nonce:      n,
	}
	return nil
}

// GetCiphertext returns the encrypted bytes.
func (s *CredentialStore) GetCiphertext(_ context.Context, engagementID, provider string) ([]byte, []byte, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	row, ok := s.rows[credKey{engagementID, provider}]
	if !ok {
		return nil, nil, "", fmt.Errorf("credential %s/%s: %w", engagementID, provider, store.ErrNotFound)
	}
	ct := make([]byte, len(row.ciphertext))
	copy(ct, row.ciphertext)
	nonce := make([]byte, len(row.nonce))
	copy(nonce, row.nonce)
	return ct, nonce, row.meta.KeyID, nil
}

// TouchLastUsed records the current time as last access.
func (s *CredentialStore) TouchLastUsed(_ context.Context, engagementID, provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := credKey{engagementID, provider}
	row, ok := s.rows[k]
	if !ok {
		return fmt.Errorf("credential %s/%s: %w", engagementID, provider, store.ErrNotFound)
	}
	now := time.Now().UTC()
	row.meta.LastUsedAt = &now
	s.rows[k] = row
	return nil
}

// GetMeta returns non-sensitive credential metadata.
func (s *CredentialStore) GetMeta(_ context.Context, engagementID, provider string) (domain.CredentialMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	row, ok := s.rows[credKey{engagementID, provider}]
	if !ok {
		return domain.CredentialMeta{}, fmt.Errorf("credential %s/%s: %w", engagementID, provider, store.ErrNotFound)
	}
	return row.meta, nil
}

// ListForEngagement returns metadata for all credentials for an engagement.
func (s *CredentialStore) ListForEngagement(_ context.Context, engagementID string) ([]domain.CredentialMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.CredentialMeta
	for k, row := range s.rows {
		if k.engagementID == engagementID {
			out = append(out, row.meta)
		}
	}
	return out, nil
}

// --- JobStore ---

// JobStore is the in-memory implementation of store.JobStore.
type JobStore struct {
	mu   sync.RWMutex
	rows map[string]domain.Job
}

// NewJobStore returns an empty JobStore.
func NewJobStore() *JobStore {
	return &JobStore{rows: make(map[string]domain.Job)}
}

var _ store.JobStore = (*JobStore)(nil)

// Create stores j and returns its ID.
func (s *JobStore) Create(_ context.Context, j domain.Job) (string, error) {
	if j.ID == "" {
		j.ID = uuid.NewString()
	}
	j.CreatedAt = time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[j.ID] = j
	return j.ID, nil
}

// Get returns a job by ID.
func (s *JobStore) Get(_ context.Context, id string) (domain.Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.rows[id]
	if !ok {
		return domain.Job{}, fmt.Errorf("job %s: %w", id, store.ErrNotFound)
	}
	return j, nil
}

// UpdateStatus changes the job status and sets timestamps.
func (s *JobStore) UpdateStatus(_ context.Context, id string, status domain.JobStatus, errText string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.rows[id]
	if !ok {
		return fmt.Errorf("job %s: %w", id, store.ErrNotFound)
	}
	j.Status = status
	j.Err = errText
	now := time.Now().UTC()
	switch status {
	case domain.JobRunning:
		j.StartedAt = &now
	case domain.JobDone, domain.JobFailed:
		j.FinishedAt = &now
	}
	s.rows[id] = j
	return nil
}

// ListRunning returns all jobs in the running state.
func (s *JobStore) ListRunning(_ context.Context) ([]domain.Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.Job
	for _, j := range s.rows {
		if j.Status == domain.JobRunning {
			out = append(out, j)
		}
	}
	return out, nil
}

// --- Audit Logger ---

// AuditEvent is a stored audit event with its assigned ID.
type AuditEvent struct {
	audit.Event
	ID string
}

// AuditLogger is an in-memory implementation of audit.Logger.
type AuditLogger struct {
	mu     sync.RWMutex
	events []AuditEvent
}

// NewAuditLogger returns an empty AuditLogger.
func NewAuditLogger() *AuditLogger { return &AuditLogger{} }

var _ audit.Logger = (*AuditLogger)(nil)

// Record appends an event.
func (l *AuditLogger) Record(_ context.Context, e audit.Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, AuditEvent{Event: e, ID: uuid.NewString()})
	return nil
}

// Events returns a snapshot of all recorded events. Useful in tests.
func (l *AuditLogger) Events() []AuditEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]AuditEvent, len(l.events))
	copy(out, l.events)
	return out
}
