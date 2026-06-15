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
	"sort"
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

// Update replaces the full engagement record.
func (s *EngagementStore) Update(_ context.Context, e domain.Engagement) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[e.ID]; !ok {
		return fmt.Errorf("engagement %s: %w", e.ID, store.ErrNotFound)
	}
	e.UpdatedAt = time.Now().UTC()
	s.rows[e.ID] = e
	return nil
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

// ListForProject returns all engagements that belong to the given project.
func (s *EngagementStore) ListForProject(_ context.Context, projectID string) ([]domain.Engagement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.Engagement
	for _, e := range s.rows {
		if e.ProjectID == projectID {
			out = append(out, e)
		}
	}
	return out, nil
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
	// Copy the edges slice so callers cannot mutate the store's internal state.
	var edges []domain.Edge
	if e := s.edges[engagementID]; len(e) > 0 {
		edges = append(edges, e...)
	}
	return domain.Topology{
		EngagementID: engagementID,
		Nodes:        nodes,
		Edges:        edges,
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

// SaveRun stores a ScenarioRun, generating an ID if needed. If run.ID is
// already set and the run exists, it merges: status and FinishedAt are updated
// but existing Results are preserved (incremental results use SaveResult).
func (s *ScenarioStore) SaveRun(_ context.Context, run domain.ScenarioRun) (string, error) {
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, exists := s.runs[run.ID]
	if exists {
		// Update path: keep existing results; update status fields only.
		existing.Status = run.Status
		if !run.FinishedAt.IsZero() {
			existing.FinishedAt = run.FinishedAt
		}
		// If caller supplies new results (legacy full-save path), append them.
		if len(run.Results) > 0 {
			existing.Results = run.Results
		}
		s.runs[run.ID] = existing
		return run.ID, nil
	}
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

// SaveResult appends a per-technique Result to an existing run.
func (s *ScenarioStore) SaveResult(_ context.Context, runID string, result domain.Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[runID]
	if !ok {
		return fmt.Errorf("scenario_run %s: %w", runID, store.ErrNotFound)
	}
	run.Results = append(run.Results, result)
	s.runs[runID] = run
	return nil
}

// --- UserScenarioStore ---

// UserScenarioStore is the in-memory implementation of store.UserScenarioStore.
type UserScenarioStore struct {
	mu        sync.RWMutex
	scenarios map[string]domain.Scenario
}

// NewUserScenarioStore returns an empty UserScenarioStore.
func NewUserScenarioStore() *UserScenarioStore {
	return &UserScenarioStore{scenarios: make(map[string]domain.Scenario)}
}

var _ store.UserScenarioStore = (*UserScenarioStore)(nil)

// Create stores an operator-authored scenario, generating an ID if needed.
func (s *UserScenarioStore) Create(_ context.Context, sc domain.Scenario) (string, error) {
	if sc.ID == "" {
		sc.ID = uuid.NewString()
	}
	if sc.CreatedAt.IsZero() {
		sc.CreatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scenarios[sc.ID] = sc
	return sc.ID, nil
}

// Get returns a stored scenario by ID.
func (s *UserScenarioStore) Get(_ context.Context, id string) (domain.Scenario, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sc, ok := s.scenarios[id]
	if !ok {
		return domain.Scenario{}, fmt.Errorf("scenario %s: %w", id, store.ErrNotFound)
	}
	return sc, nil
}

// List returns all stored scenarios, newest first.
func (s *UserScenarioStore) List(_ context.Context) ([]domain.Scenario, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Scenario, 0, len(s.scenarios))
	for _, sc := range s.scenarios {
		out = append(out, sc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Update replaces an existing scenario, preserving its CreatedAt.
func (s *UserScenarioStore) Update(_ context.Context, sc domain.Scenario) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.scenarios[sc.ID]
	if !ok {
		return fmt.Errorf("scenario %s: %w", sc.ID, store.ErrNotFound)
	}
	sc.CreatedAt = existing.CreatedAt
	s.scenarios[sc.ID] = sc
	return nil
}

// Delete removes a scenario by ID.
func (s *UserScenarioStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.scenarios[id]; !ok {
		return fmt.Errorf("scenario %s: %w", id, store.ErrNotFound)
	}
	delete(s.scenarios, id)
	return nil
}

// --- UserTechniqueStore ---

// UserTechniqueStore is the in-memory implementation of store.UserTechniqueStore.
type UserTechniqueStore struct {
	mu         sync.RWMutex
	techniques map[string]domain.Technique // keyed by AttackID
}

// NewUserTechniqueStore returns an empty UserTechniqueStore.
func NewUserTechniqueStore() *UserTechniqueStore {
	return &UserTechniqueStore{techniques: make(map[string]domain.Technique)}
}

var _ store.UserTechniqueStore = (*UserTechniqueStore)(nil)

// Create stores a new technique; errors if the AttackID already exists.
func (s *UserTechniqueStore) Create(_ context.Context, t domain.Technique) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.techniques[t.AttackID]; ok {
		return fmt.Errorf("technique %s already exists", t.AttackID)
	}
	s.techniques[t.AttackID] = t
	return nil
}

// List returns all stored techniques ordered by AttackID.
func (s *UserTechniqueStore) List(_ context.Context) ([]domain.Technique, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Technique, 0, len(s.techniques))
	for _, t := range s.techniques {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AttackID < out[j].AttackID })
	return out, nil
}

// Update replaces an existing technique.
func (s *UserTechniqueStore) Update(_ context.Context, t domain.Technique) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.techniques[t.AttackID]; !ok {
		return fmt.Errorf("technique %s: %w", t.AttackID, store.ErrNotFound)
	}
	s.techniques[t.AttackID] = t
	return nil
}

// Delete removes a technique by AttackID.
func (s *UserTechniqueStore) Delete(_ context.Context, attackID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.techniques[attackID]; !ok {
		return fmt.Errorf("technique %s: %w", attackID, store.ErrNotFound)
	}
	delete(s.techniques, attackID)
	return nil
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
	createdAt := existing.meta.CreatedAt
	if !exists {
		id = uuid.NewString()
		createdAt = time.Now().UTC()
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
			CreatedAt:    createdAt,
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

// List returns audit events for an engagement, most recent first, with
// pagination. Implements audit.Reader.
func (l *AuditLogger) List(_ context.Context, engagementID string, limit, offset int) ([]audit.Event, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var filtered []audit.Event
	for i := len(l.events) - 1; i >= 0; i-- {
		e := l.events[i]
		if engagementID == "" || e.EngagementID == engagementID {
			filtered = append(filtered, e.Event)
		}
	}

	if offset >= len(filtered) {
		return nil, nil
	}
	filtered = filtered[offset:]
	if limit > 0 && limit < len(filtered) {
		filtered = filtered[:limit]
	}
	return filtered, nil
}
