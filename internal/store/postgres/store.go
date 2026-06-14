// Package postgres provides pgx/v5-backed implementations of the store
// interfaces. Each constructor takes a *pgxpool.Pool; queries are plain SQL
// with fmt.Errorf("%w") error wrapping throughout.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/store"
)

// --- EngagementStore ---

// EngagementStore is the Postgres implementation of store.EngagementStore.
type EngagementStore struct {
	pool *pgxpool.Pool
}

// NewEngagementStore returns a new EngagementStore backed by pool.
func NewEngagementStore(pool *pgxpool.Pool) *EngagementStore {
	return &EngagementStore{pool: pool}
}

var _ store.EngagementStore = (*EngagementStore)(nil)

// Create inserts a new engagement and returns its generated UUID.
func (s *EngagementStore) Create(ctx context.Context, e domain.Engagement) (string, error) {
	scopeTargets, err := json.Marshal(e.Scope.AllowedTargets)
	if err != nil {
		return "", fmt.Errorf("marshal scope_targets: %w", err)
	}
	scopeExclusions, err := json.Marshal(e.Scope.Exclusions)
	if err != nil {
		return "", fmt.Errorf("marshal scope_exclusions: %w", err)
	}
	roeConstraints, err := json.Marshal(e.RoE.Constraints)
	if err != nil {
		return "", fmt.Errorf("marshal roe_constraints: %w", err)
	}

	var id string
	err = s.pool.QueryRow(ctx, `
		INSERT INTO engagements (
			client, codename, lead_operator, engagement_type, status,
			scope_targets, scope_notes, scope_exclusions,
			roe_document, roe_window_start, roe_window_end, roe_constraints,
			auth_by, auth_document, auth_granted_at, auth_expires_at,
			project_id
		) VALUES (
			$1,$2,$3,$4,$5,
			$6,$7,$8,
			$9,$10,$11,$12,
			$13,$14,$15,$16,
			$17
		) RETURNING id`,
		e.Client, e.Codename, e.LeadOperator, string(e.EngagementType), string(e.Status),
		scopeTargets, e.Scope.Notes, scopeExclusions,
		e.RoE.DocumentRef, nullTime(e.RoE.WindowStart), nullTime(e.RoE.WindowEnd), roeConstraints,
		e.Authorization.AuthorizedBy, e.Authorization.DocumentRef,
		nullTime(e.Authorization.GrantedAt), nullTime(e.Authorization.ExpiresAt),
		nullString(e.ProjectID),
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create engagement: %w", err)
	}
	return id, nil
}

// Get fetches a single engagement by ID.
func (s *EngagementStore) Get(ctx context.Context, id string) (domain.Engagement, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, client, codename, lead_operator, engagement_type, status,
		       scope_targets, scope_notes, scope_exclusions,
		       roe_document, roe_window_start, roe_window_end, roe_constraints,
		       auth_by, auth_document, auth_granted_at, auth_expires_at,
		       created_at, updated_at, project_id
		FROM engagements WHERE id = $1`, id)
	e, err := scanEngagement(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Engagement{}, fmt.Errorf("engagement %s: %w", id, store.ErrNotFound)
		}
		return domain.Engagement{}, fmt.Errorf("get engagement %s: %w", id, err)
	}
	return e, nil
}

// List returns all engagements ordered by created_at descending.
func (s *EngagementStore) List(ctx context.Context) ([]domain.Engagement, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, client, codename, lead_operator, engagement_type, status,
		       scope_targets, scope_notes, scope_exclusions,
		       roe_document, roe_window_start, roe_window_end, roe_constraints,
		       auth_by, auth_document, auth_granted_at, auth_expires_at,
		       created_at, updated_at, project_id
		FROM engagements ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list engagements: %w", err)
	}
	defer rows.Close()

	var out []domain.Engagement
	for rows.Next() {
		e, err := scanEngagement(rows)
		if err != nil {
			return nil, fmt.Errorf("scan engagement: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Update replaces the full engagement record (used when authorization or
// metadata changes). Only the mutable fields are updated; id/created_at are
// preserved.
func (s *EngagementStore) Update(ctx context.Context, e domain.Engagement) error {
	scopeTargets, err := json.Marshal(e.Scope.AllowedTargets)
	if err != nil {
		return fmt.Errorf("marshal scope_targets: %w", err)
	}
	scopeExclusions, err := json.Marshal(e.Scope.Exclusions)
	if err != nil {
		return fmt.Errorf("marshal scope_exclusions: %w", err)
	}
	roeConstraints, err := json.Marshal(e.RoE.Constraints)
	if err != nil {
		return fmt.Errorf("marshal roe_constraints: %w", err)
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE engagements SET
			client=$1, codename=$2, lead_operator=$3, engagement_type=$4, status=$5,
			scope_targets=$6, scope_notes=$7, scope_exclusions=$8,
			roe_document=$9, roe_window_start=$10, roe_window_end=$11, roe_constraints=$12,
			auth_by=$13, auth_document=$14, auth_granted_at=$15, auth_expires_at=$16,
			project_id=$17,
			updated_at=now()
		WHERE id=$18`,
		e.Client, e.Codename, e.LeadOperator, string(e.EngagementType), string(e.Status),
		scopeTargets, e.Scope.Notes, scopeExclusions,
		e.RoE.DocumentRef, nullTime(e.RoE.WindowStart), nullTime(e.RoE.WindowEnd), roeConstraints,
		e.Authorization.AuthorizedBy, e.Authorization.DocumentRef,
		nullTime(e.Authorization.GrantedAt), nullTime(e.Authorization.ExpiresAt),
		nullString(e.ProjectID),
		e.ID,
	)
	if err != nil {
		return fmt.Errorf("update engagement %s: %w", e.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("engagement %s: %w", e.ID, store.ErrNotFound)
	}
	return nil
}

// UpdateStatus updates only the status field and refreshes updated_at.
func (s *EngagementStore) UpdateStatus(ctx context.Context, id string, status domain.EngagementStatus) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE engagements SET status=$1, updated_at=now() WHERE id=$2`,
		string(status), id)
	if err != nil {
		return fmt.Errorf("update engagement status %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("engagement %s: %w", id, store.ErrNotFound)
	}
	return nil
}

// rowScanner is the shared interface for pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanEngagement(row rowScanner) (domain.Engagement, error) {
	var (
		e                domain.Engagement
		engagementType   string
		status           string
		scopeTargetsJSON []byte
		scopeExclJSON    []byte
		roeConstraJSON   []byte
		roeWinStart      *time.Time
		roeWinEnd        *time.Time
		authGrantedAt    *time.Time
		authExpiresAt    *time.Time
		projectID        *string
	)
	err := row.Scan(
		&e.ID, &e.Client, &e.Codename, &e.LeadOperator, &engagementType, &status,
		&scopeTargetsJSON, &e.Scope.Notes, &scopeExclJSON,
		&e.RoE.DocumentRef, &roeWinStart, &roeWinEnd, &roeConstraJSON,
		&e.Authorization.AuthorizedBy, &e.Authorization.DocumentRef,
		&authGrantedAt, &authExpiresAt,
		&e.CreatedAt, &e.UpdatedAt,
		&projectID,
	)
	if err != nil {
		return domain.Engagement{}, err
	}
	e.EngagementType = domain.EngagementType(engagementType)
	e.Status = domain.EngagementStatus(status)
	if err = json.Unmarshal(scopeTargetsJSON, &e.Scope.AllowedTargets); err != nil {
		return domain.Engagement{}, fmt.Errorf("unmarshal scope_targets: %w", err)
	}
	if err = json.Unmarshal(scopeExclJSON, &e.Scope.Exclusions); err != nil {
		return domain.Engagement{}, fmt.Errorf("unmarshal scope_exclusions: %w", err)
	}
	if err = json.Unmarshal(roeConstraJSON, &e.RoE.Constraints); err != nil {
		return domain.Engagement{}, fmt.Errorf("unmarshal roe_constraints: %w", err)
	}
	if roeWinStart != nil {
		e.RoE.WindowStart = *roeWinStart
	}
	if roeWinEnd != nil {
		e.RoE.WindowEnd = *roeWinEnd
	}
	if authGrantedAt != nil {
		e.Authorization.GrantedAt = *authGrantedAt
	}
	if authExpiresAt != nil {
		e.Authorization.ExpiresAt = *authExpiresAt
	}
	if projectID != nil {
		e.ProjectID = *projectID
	}
	return e, nil
}

// ListForProject returns all engagements belonging to the given project.
func (s *EngagementStore) ListForProject(ctx context.Context, projectID string) ([]domain.Engagement, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, client, codename, lead_operator, engagement_type, status,
		       scope_targets, scope_notes, scope_exclusions,
		       roe_document, roe_window_start, roe_window_end, roe_constraints,
		       auth_by, auth_document, auth_granted_at, auth_expires_at,
		       created_at, updated_at, project_id
		FROM engagements WHERE project_id = $1 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list engagements for project %s: %w", projectID, err)
	}
	defer rows.Close()

	var out []domain.Engagement
	for rows.Next() {
		e, err := scanEngagement(rows)
		if err != nil {
			return nil, fmt.Errorf("scan engagement: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- InfraStore ---

// InfraStore is the Postgres implementation of store.InfraStore.
type InfraStore struct {
	pool *pgxpool.Pool
}

// NewInfraStore returns a new InfraStore backed by pool.
func NewInfraStore(pool *pgxpool.Pool) *InfraStore {
	return &InfraStore{pool: pool}
}

var _ store.InfraStore = (*InfraStore)(nil)

// SaveTopology upserts all nodes and edges for an engagement in a single
// transaction. Nodes not present in t.Nodes are deleted; edges are fully
// replaced.
func (s *InfraStore) SaveTopology(ctx context.Context, t domain.Topology) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin SaveTopology tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Upsert nodes.
	for _, n := range t.Nodes {
		_, err = tx.Exec(ctx, `
			INSERT INTO nodes (
				id, engagement_id, node_type, cloud, region, size,
				c2_framework, profile_name, subtype,
				name, listener, front_domain, cost_estimate, x, y,
				status, health, public_ip, provider_ref
			) VALUES (
				$1,$2,$3,$4,$5,$6,
				$7,$8,$9,
				$10,$11,$12,$13,$14,$15,
				$16,$17,$18,$19
			)
			ON CONFLICT (id) DO UPDATE SET
				node_type=EXCLUDED.node_type,
				cloud=EXCLUDED.cloud,
				region=EXCLUDED.region,
				size=EXCLUDED.size,
				c2_framework=EXCLUDED.c2_framework,
				profile_name=EXCLUDED.profile_name,
				subtype=EXCLUDED.subtype,
				name=EXCLUDED.name,
				listener=EXCLUDED.listener,
				front_domain=EXCLUDED.front_domain,
				cost_estimate=EXCLUDED.cost_estimate,
				x=EXCLUDED.x,
				y=EXCLUDED.y,
				updated_at=now()`,
			n.ID, t.EngagementID,
			string(n.Spec.Type), string(n.Spec.Cloud), n.Spec.Region, n.Spec.Size,
			n.Spec.C2Framework, n.Spec.ProfileName, n.Spec.Subtype,
			n.Canvas.Name, n.Canvas.Listener, n.Canvas.FrontDomain,
			n.Canvas.CostEstimate, n.Canvas.X, n.Canvas.Y,
			string(n.Status), string(n.Health), n.PublicIP, n.ProviderRef,
		)
		if err != nil {
			return fmt.Errorf("upsert node %s: %w", n.ID, err)
		}
	}

	// Remove nodes no longer in topology.
	keepIDs := make([]string, len(t.Nodes))
	for i, n := range t.Nodes {
		keepIDs[i] = n.ID
	}
	_, err = tx.Exec(ctx,
		`DELETE FROM nodes WHERE engagement_id=$1 AND id != ALL($2::uuid[])`,
		t.EngagementID, keepIDs)
	if err != nil {
		return fmt.Errorf("prune nodes: %w", err)
	}

	// Replace edges.
	_, err = tx.Exec(ctx, `DELETE FROM edges WHERE engagement_id=$1`, t.EngagementID)
	if err != nil {
		return fmt.Errorf("delete edges: %w", err)
	}
	for _, edge := range t.Edges {
		_, err = tx.Exec(ctx,
			`INSERT INTO edges (engagement_id, from_node_id, to_node_id) VALUES ($1,$2,$3)`,
			t.EngagementID, edge.FromNodeID, edge.ToNodeID)
		if err != nil {
			return fmt.Errorf("insert edge: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// GetTopology returns the full node+edge graph for an engagement.
func (s *InfraStore) GetTopology(ctx context.Context, engagementID string) (domain.Topology, error) {
	nodes, err := s.NodesForEngagement(ctx, engagementID)
	if err != nil {
		return domain.Topology{}, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT from_node_id, to_node_id FROM edges WHERE engagement_id=$1`,
		engagementID)
	if err != nil {
		return domain.Topology{}, fmt.Errorf("query edges: %w", err)
	}
	defer rows.Close()

	var edges []domain.Edge
	for rows.Next() {
		var e domain.Edge
		if err = rows.Scan(&e.FromNodeID, &e.ToNodeID); err != nil {
			return domain.Topology{}, fmt.Errorf("scan edge: %w", err)
		}
		edges = append(edges, e)
	}
	if err = rows.Err(); err != nil {
		return domain.Topology{}, fmt.Errorf("edges rows: %w", err)
	}

	return domain.Topology{EngagementID: engagementID, Nodes: nodes, Edges: edges}, nil
}

// UpdateNodeStatus updates the status and health of a node.
func (s *InfraStore) UpdateNodeStatus(ctx context.Context, nodeID string, status domain.NodeStatus, health domain.Health) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE nodes SET status=$1, health=$2, updated_at=now() WHERE id=$3`,
		string(status), string(health), nodeID)
	if err != nil {
		return fmt.Errorf("update node status %s: %w", nodeID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("node %s: %w", nodeID, store.ErrNotFound)
	}
	return nil
}

// NodesForEngagement returns all nodes for the given engagement.
func (s *InfraStore) NodesForEngagement(ctx context.Context, engagementID string) ([]domain.Node, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, engagement_id,
		       node_type, cloud, region, size, c2_framework, profile_name, subtype,
		       name, listener, front_domain, cost_estimate, x, y,
		       status, health, public_ip, provider_ref, created_at, updated_at
		FROM nodes WHERE engagement_id=$1 ORDER BY created_at`, engagementID)
	if err != nil {
		return nil, fmt.Errorf("query nodes: %w", err)
	}
	defer rows.Close()

	var out []domain.Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func scanNode(row rowScanner) (domain.Node, error) {
	var n domain.Node
	var nodeType, cloud, status, health string
	err := row.Scan(
		&n.ID, &n.EngagementID,
		&nodeType, &cloud, &n.Spec.Region, &n.Spec.Size,
		&n.Spec.C2Framework, &n.Spec.ProfileName, &n.Spec.Subtype,
		&n.Canvas.Name, &n.Canvas.Listener, &n.Canvas.FrontDomain,
		&n.Canvas.CostEstimate, &n.Canvas.X, &n.Canvas.Y,
		&status, &health, &n.PublicIP, &n.ProviderRef,
		&n.CreatedAt, &n.UpdatedAt,
	)
	if err != nil {
		return domain.Node{}, err
	}
	n.Spec.Type = domain.NodeType(nodeType)
	n.Spec.Cloud = domain.CloudProviderType(cloud)
	n.Status = domain.NodeStatus(status)
	n.Health = domain.Health(health)
	return n, nil
}

// --- ScenarioStore ---

// ScenarioStore is the Postgres implementation of store.ScenarioStore.
type ScenarioStore struct {
	pool *pgxpool.Pool
}

// NewScenarioStore returns a new ScenarioStore backed by pool.
func NewScenarioStore(pool *pgxpool.Pool) *ScenarioStore {
	return &ScenarioStore{pool: pool}
}

var _ store.ScenarioStore = (*ScenarioStore)(nil)

// SaveRun inserts or updates a ScenarioRun. If run.ID is empty a new row is
// inserted and the generated UUID is returned. If run.ID is non-empty the row
// is updated (status, finished_at); Results in the run are inserted only when
// the run is new (incremental results use SaveResult instead).
func (s *ScenarioStore) SaveRun(ctx context.Context, run domain.ScenarioRun) (string, error) {
	// Update path: when ID is already set, update the run status only.
	if run.ID != "" {
		tag, err := s.pool.Exec(ctx, `
			UPDATE scenario_runs
			SET status=$2, finished_at=$3
			WHERE id=$1`,
			run.ID, string(run.Status), nullTime(run.FinishedAt),
		)
		if err != nil {
			return "", fmt.Errorf("update scenario_run %s: %w", run.ID, err)
		}
		if tag.RowsAffected() == 0 {
			return "", fmt.Errorf("update scenario_run %s: %w", run.ID, store.ErrNotFound)
		}
		return run.ID, nil
	}

	// Insert path: create the run row and any results already present.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin SaveRun tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO scenario_runs (engagement_id, scenario_id, status, started_at, finished_at)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id`,
		run.EngagementID, run.ScenarioID, string(run.Status),
		nullTime(run.StartedAt), nullTime(run.FinishedAt),
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert scenario_run: %w", err)
	}

	for _, r := range run.Results {
		_, err = tx.Exec(ctx, `
			INSERT INTO technique_results
			  (run_id, attack_id, status, output, started_at, finished_at, err)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			id, r.TechniqueAttackID, string(r.Status), r.Output,
			nullTime(r.StartedAt), nullTime(r.FinishedAt), r.Err,
		)
		if err != nil {
			return "", fmt.Errorf("insert technique_result: %w", err)
		}
	}

	return id, tx.Commit(ctx)
}

// GetRun retrieves a ScenarioRun and its Results by ID.
func (s *ScenarioStore) GetRun(ctx context.Context, id string) (domain.ScenarioRun, error) {
	var run domain.ScenarioRun
	var status string
	var startedAt, finishedAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id, engagement_id, scenario_id, status, started_at, finished_at
		FROM scenario_runs WHERE id=$1`, id).
		Scan(&run.ID, &run.EngagementID, &run.ScenarioID, &status, &startedAt, &finishedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ScenarioRun{}, fmt.Errorf("scenario_run %s: %w", id, store.ErrNotFound)
		}
		return domain.ScenarioRun{}, fmt.Errorf("get scenario_run %s: %w", id, err)
	}
	run.Status = domain.ExecutionStatus(status)
	if startedAt != nil {
		run.StartedAt = *startedAt
	}
	if finishedAt != nil {
		run.FinishedAt = *finishedAt
	}

	rows, err := s.pool.Query(ctx, `
		SELECT attack_id, status, output, started_at, finished_at, err
		FROM technique_results WHERE run_id=$1 ORDER BY started_at NULLS FIRST`, id)
	if err != nil {
		return domain.ScenarioRun{}, fmt.Errorf("query technique_results: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r domain.Result
		var rStatus string
		var rStart, rEnd *time.Time
		if err = rows.Scan(&r.TechniqueAttackID, &rStatus, &r.Output, &rStart, &rEnd, &r.Err); err != nil {
			return domain.ScenarioRun{}, fmt.Errorf("scan result: %w", err)
		}
		r.Status = domain.ExecutionStatus(rStatus)
		if rStart != nil {
			r.StartedAt = *rStart
		}
		if rEnd != nil {
			r.FinishedAt = *rEnd
		}
		run.Results = append(run.Results, r)
	}
	return run, rows.Err()
}

// SaveResult appends a single per-technique Result to an existing run.
func (s *ScenarioStore) SaveResult(ctx context.Context, runID string, result domain.Result) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO technique_results
		  (run_id, attack_id, status, output, started_at, finished_at, err)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		runID, result.TechniqueAttackID, string(result.Status), result.Output,
		nullTime(result.StartedAt), nullTime(result.FinishedAt), result.Err,
	)
	if err != nil {
		return fmt.Errorf("insert technique_result for run %s: %w", runID, err)
	}
	return nil
}

// RunsForEngagement returns all runs for an engagement ordered by started_at.
func (s *ScenarioStore) RunsForEngagement(ctx context.Context, engagementID string) ([]domain.ScenarioRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, engagement_id, scenario_id, status, started_at, finished_at
		FROM scenario_runs WHERE engagement_id=$1 ORDER BY started_at NULLS FIRST`,
		engagementID)
	if err != nil {
		return nil, fmt.Errorf("query scenario_runs: %w", err)
	}
	defer rows.Close()

	var out []domain.ScenarioRun
	for rows.Next() {
		var run domain.ScenarioRun
		var status string
		var startedAt, finishedAt *time.Time
		if err = rows.Scan(&run.ID, &run.EngagementID, &run.ScenarioID,
			&status, &startedAt, &finishedAt); err != nil {
			return nil, fmt.Errorf("scan scenario_run: %w", err)
		}
		run.Status = domain.ExecutionStatus(status)
		if startedAt != nil {
			run.StartedAt = *startedAt
		}
		if finishedAt != nil {
			run.FinishedAt = *finishedAt
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// --- UserScenarioStore ---

// UserScenarioStore is the Postgres implementation of store.UserScenarioStore.
type UserScenarioStore struct {
	pool *pgxpool.Pool
}

// NewUserScenarioStore returns a new UserScenarioStore backed by pool.
func NewUserScenarioStore(pool *pgxpool.Pool) *UserScenarioStore {
	return &UserScenarioStore{pool: pool}
}

var _ store.UserScenarioStore = (*UserScenarioStore)(nil)

// Create inserts an operator-authored scenario, storing techniques as JSONB.
func (s *UserScenarioStore) Create(ctx context.Context, sc domain.Scenario) (string, error) {
	techniques, err := json.Marshal(sc.Techniques)
	if err != nil {
		return "", fmt.Errorf("marshal techniques: %w", err)
	}
	var id string
	err = s.pool.QueryRow(ctx, `
		INSERT INTO user_scenarios (name, adversary_profile, description, techniques)
		VALUES ($1,$2,$3,$4)
		RETURNING id`,
		sc.Name, sc.AdversaryProfile, sc.Description, techniques,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert user_scenario: %w", err)
	}
	return id, nil
}

func (s *UserScenarioStore) scanScenario(row pgx.Row) (domain.Scenario, error) {
	var sc domain.Scenario
	var techniques []byte
	if err := row.Scan(&sc.ID, &sc.Name, &sc.AdversaryProfile, &sc.Description, &techniques, &sc.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Scenario{}, fmt.Errorf("user_scenario: %w", store.ErrNotFound)
		}
		return domain.Scenario{}, fmt.Errorf("scan user_scenario: %w", err)
	}
	if len(techniques) > 0 {
		if err := json.Unmarshal(techniques, &sc.Techniques); err != nil {
			return domain.Scenario{}, fmt.Errorf("unmarshal techniques: %w", err)
		}
	}
	return sc, nil
}

// Get returns a stored scenario by ID.
func (s *UserScenarioStore) Get(ctx context.Context, id string) (domain.Scenario, error) {
	return s.scanScenario(s.pool.QueryRow(ctx, `
		SELECT id, name, adversary_profile, description, techniques, created_at
		FROM user_scenarios WHERE id=$1`, id))
}

// List returns all stored scenarios, newest first.
func (s *UserScenarioStore) List(ctx context.Context) ([]domain.Scenario, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, adversary_profile, description, techniques, created_at
		FROM user_scenarios ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query user_scenarios: %w", err)
	}
	defer rows.Close()
	var out []domain.Scenario
	for rows.Next() {
		sc, err := s.scanScenario(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// --- CredentialStore ---

// CredentialStore is the Postgres implementation of store.CredentialStore.
type CredentialStore struct {
	pool *pgxpool.Pool
}

// NewCredentialStore returns a new CredentialStore backed by pool.
func NewCredentialStore(pool *pgxpool.Pool) *CredentialStore {
	return &CredentialStore{pool: pool}
}

var _ store.CredentialStore = (*CredentialStore)(nil)

// Upsert stores or replaces encrypted credentials. Uses INSERT ... ON CONFLICT
// so it never emits an UPDATE that could trigger application-level detections.
func (s *CredentialStore) Upsert(ctx context.Context, engagementID, provider string, ciphertext, nonce []byte, keyID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO engagement_credentials (engagement_id, provider, ciphertext, nonce, key_id)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (engagement_id, provider) DO UPDATE SET
			ciphertext=EXCLUDED.ciphertext,
			nonce=EXCLUDED.nonce,
			key_id=EXCLUDED.key_id`,
		engagementID, provider, ciphertext, nonce, keyID)
	if err != nil {
		return fmt.Errorf("upsert credential: %w", err)
	}
	return nil
}

// GetCiphertext returns the raw encrypted bytes for decryption.
func (s *CredentialStore) GetCiphertext(ctx context.Context, engagementID, provider string) ([]byte, []byte, string, error) {
	var ct, nonce []byte
	var keyID string
	err := s.pool.QueryRow(ctx, `
		SELECT ciphertext, nonce, key_id
		FROM engagement_credentials
		WHERE engagement_id=$1 AND provider=$2`,
		engagementID, provider).Scan(&ct, &nonce, &keyID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, "", fmt.Errorf("credential %s/%s: %w", engagementID, provider, store.ErrNotFound)
		}
		return nil, nil, "", fmt.Errorf("get ciphertext: %w", err)
	}
	return ct, nonce, keyID, nil
}

// TouchLastUsed records the current time as the last access time.
func (s *CredentialStore) TouchLastUsed(ctx context.Context, engagementID, provider string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE engagement_credentials SET last_used_at=now()
		WHERE engagement_id=$1 AND provider=$2`,
		engagementID, provider)
	if err != nil {
		return fmt.Errorf("touch last_used_at: %w", err)
	}
	return nil
}

// GetMeta returns non-sensitive credential metadata.
func (s *CredentialStore) GetMeta(ctx context.Context, engagementID, provider string) (domain.CredentialMeta, error) {
	var m domain.CredentialMeta
	err := s.pool.QueryRow(ctx, `
		SELECT id, engagement_id, provider, key_id, created_at, last_used_at
		FROM engagement_credentials
		WHERE engagement_id=$1 AND provider=$2`,
		engagementID, provider).
		Scan(&m.ID, &m.EngagementID, &m.Provider, &m.KeyID, &m.CreatedAt, &m.LastUsedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.CredentialMeta{}, fmt.Errorf("credential %s/%s: %w", engagementID, provider, store.ErrNotFound)
		}
		return domain.CredentialMeta{}, fmt.Errorf("get credential meta: %w", err)
	}
	return m, nil
}

// ListForEngagement returns all credential metadata for an engagement.
func (s *CredentialStore) ListForEngagement(ctx context.Context, engagementID string) ([]domain.CredentialMeta, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, engagement_id, provider, key_id, created_at, last_used_at
		FROM engagement_credentials
		WHERE engagement_id=$1 ORDER BY provider`,
		engagementID)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	defer rows.Close()

	var out []domain.CredentialMeta
	for rows.Next() {
		var m domain.CredentialMeta
		if err = rows.Scan(&m.ID, &m.EngagementID, &m.Provider, &m.KeyID,
			&m.CreatedAt, &m.LastUsedAt); err != nil {
			return nil, fmt.Errorf("scan credential meta: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// --- JobStore ---

// JobStore is the Postgres implementation of store.JobStore.
type JobStore struct {
	pool *pgxpool.Pool
}

// NewJobStore returns a new JobStore backed by pool.
func NewJobStore(pool *pgxpool.Pool) *JobStore {
	return &JobStore{pool: pool}
}

var _ store.JobStore = (*JobStore)(nil)

// Create inserts a new job record and returns its generated UUID.
func (s *JobStore) Create(ctx context.Context, j domain.Job) (string, error) {
	detail, err := json.Marshal(j.Detail)
	if err != nil {
		return "", fmt.Errorf("marshal job detail: %w", err)
	}
	var id string
	err = s.pool.QueryRow(ctx, `
		INSERT INTO jobs (engagement_id, kind, status, detail)
		VALUES ($1,$2,$3,$4)
		RETURNING id`,
		j.EngagementID, string(j.Kind), string(j.Status), detail).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create job: %w", err)
	}
	return id, nil
}

// Get retrieves a job by ID.
func (s *JobStore) Get(ctx context.Context, id string) (domain.Job, error) {
	var j domain.Job
	var kind, status string
	var detailJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, engagement_id, kind, status, detail,
		       created_at, started_at, finished_at, err
		FROM jobs WHERE id=$1`, id).
		Scan(&j.ID, &j.EngagementID, &kind, &status, &detailJSON,
			&j.CreatedAt, &j.StartedAt, &j.FinishedAt, &j.Err)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Job{}, fmt.Errorf("job %s: %w", id, store.ErrNotFound)
		}
		return domain.Job{}, fmt.Errorf("get job %s: %w", id, err)
	}
	j.Kind = domain.JobKind(kind)
	j.Status = domain.JobStatus(status)
	if err = json.Unmarshal(detailJSON, &j.Detail); err != nil {
		return domain.Job{}, fmt.Errorf("unmarshal job detail: %w", err)
	}
	return j, nil
}

// UpdateStatus changes a job's status and sets the appropriate timestamp.
func (s *JobStore) UpdateStatus(ctx context.Context, id string, status domain.JobStatus, errText string) error {
	var q string
	switch status {
	case domain.JobRunning:
		q = `UPDATE jobs SET status=$1, err=$2, started_at=now() WHERE id=$3`
	case domain.JobDone, domain.JobFailed:
		q = `UPDATE jobs SET status=$1, err=$2, finished_at=now() WHERE id=$3`
	default:
		q = `UPDATE jobs SET status=$1, err=$2 WHERE id=$3`
	}
	tag, err := s.pool.Exec(ctx, q, string(status), errText, id)
	if err != nil {
		return fmt.Errorf("update job status %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("job %s: %w", id, store.ErrNotFound)
	}
	return nil
}

// ListRunning returns all jobs in the running state for boot-time reconciliation.
func (s *JobStore) ListRunning(ctx context.Context) ([]domain.Job, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, engagement_id, kind, status, detail,
		       created_at, started_at, finished_at, err
		FROM jobs WHERE status='running' ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list running jobs: %w", err)
	}
	defer rows.Close()

	var out []domain.Job
	for rows.Next() {
		var j domain.Job
		var kind, status string
		var detailJSON []byte
		if err = rows.Scan(&j.ID, &j.EngagementID, &kind, &status, &detailJSON,
			&j.CreatedAt, &j.StartedAt, &j.FinishedAt, &j.Err); err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		j.Kind = domain.JobKind(kind)
		j.Status = domain.JobStatus(status)
		if err = json.Unmarshal(detailJSON, &j.Detail); err != nil {
			return nil, fmt.Errorf("unmarshal job detail: %w", err)
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// nullTime converts a zero time.Time to nil for nullable TIMESTAMPTZ columns.
func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
