//go:build integration

// Package postgres integration tests run against a live Postgres instance.
// Set DATABASE_URL (postgres://...) before running; tests skip with a clear
// message if the variable is unset.
package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rinfra/rinfra/internal/audit"
	audpg "github.com/rinfra/rinfra/internal/audit/postgres"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/store"
	pgstore "github.com/rinfra/rinfra/internal/store/postgres"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// --- EngagementStore ---

func TestPG_EngagementStore_CreateAndGet(t *testing.T) {
	pool := testPool(t)
	s := pgstore.NewEngagementStore(pool)
	ctx := context.Background()

	e := domain.Engagement{
		Client:         "Integration Client",
		Codename:       "OP TEST",
		LeadOperator:   "tester@example.com",
		EngagementType: domain.EngagementTypeRedTeam,
		Status:         domain.EngagementDraft,
		Scope: domain.Scope{
			AllowedTargets: []string{"192.168.0.0/16"},
			Exclusions:     []string{"192.168.1.0/24"},
			Notes:          "test scope",
		},
		RoE: domain.RulesOfEngagement{
			DocumentRef: "roe-doc-1",
			Constraints: []string{"no DoS"},
		},
		Authorization: domain.Authorization{
			AuthorizedBy: "client-cto",
			DocumentRef:  "auth-doc-1",
			GrantedAt:    time.Now().Add(-time.Hour),
			ExpiresAt:    time.Now().Add(24 * time.Hour),
		},
	}

	id, err := s.Create(ctx, e)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Client != e.Client {
		t.Errorf("Client = %q, want %q", got.Client, e.Client)
	}
	if got.Codename != e.Codename {
		t.Errorf("Codename = %q, want %q", got.Codename, e.Codename)
	}
	if got.LeadOperator != e.LeadOperator {
		t.Errorf("LeadOperator = %q", got.LeadOperator)
	}
	if len(got.Scope.Exclusions) != 1 {
		t.Errorf("Scope.Exclusions = %v", got.Scope.Exclusions)
	}
	if len(got.RoE.Constraints) != 1 {
		t.Errorf("RoE.Constraints = %v", got.RoE.Constraints)
	}
}

func TestPG_EngagementStore_GetNotFound(t *testing.T) {
	pool := testPool(t)
	s := pgstore.NewEngagementStore(pool)
	ctx := context.Background()

	_, err := s.Get(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestPG_EngagementStore_UpdateStatus(t *testing.T) {
	pool := testPool(t)
	s := pgstore.NewEngagementStore(pool)
	ctx := context.Background()

	id, err := s.Create(ctx, domain.Engagement{
		Client: "status test", Status: domain.EngagementDraft,
		Scope: domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err = s.UpdateStatus(ctx, id, domain.EngagementAuthorized); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.EngagementAuthorized {
		t.Errorf("Status = %q, want authorized", got.Status)
	}
}

func TestPG_EngagementStore_List(t *testing.T) {
	pool := testPool(t)
	s := pgstore.NewEngagementStore(pool)
	ctx := context.Background()

	// Create 2 engagements; list should return at least 2.
	for range 2 {
		_, err := s.Create(ctx, domain.Engagement{
			Client: "list-test", Status: domain.EngagementDraft,
			Scope: domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) < 2 {
		t.Errorf("expected at least 2 engagements, got %d", len(list))
	}
}

// --- InfraStore ---

func TestPG_InfraStore_SaveAndGetTopology(t *testing.T) {
	pool := testPool(t)
	engStore := pgstore.NewEngagementStore(pool)
	infraStore := pgstore.NewInfraStore(pool)
	ctx := context.Background()

	engID, err := engStore.Create(ctx, domain.Engagement{
		Client: "topo test", Status: domain.EngagementDraft,
		Scope: domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
	})
	if err != nil {
		t.Fatalf("create engagement: %v", err)
	}

	topo := domain.Topology{
		EngagementID: engID,
		Nodes: []domain.Node{
			{
				ID:           "00000000-0000-0000-0000-000000000001",
				EngagementID: engID,
				Spec: domain.NodeSpec{
					Type: domain.NodeRedirector, Cloud: domain.CloudAWS,
					Region: "us-east-1", Size: "t3.small", Subtype: "https",
				},
				Canvas: domain.NodeCanvas{
					Name: "HTTPS Redirector", X: 100, Y: 200, CostEstimate: 10.50,
				},
				Status: domain.NodePending,
				Health: domain.HealthUnknown,
			},
		},
		Edges: []domain.Edge{},
	}

	if err = infraStore.SaveTopology(ctx, topo); err != nil {
		t.Fatalf("SaveTopology: %v", err)
	}

	got, err := infraStore.GetTopology(ctx, engID)
	if err != nil {
		t.Fatalf("GetTopology: %v", err)
	}
	if len(got.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(got.Nodes))
	}
	if got.Nodes[0].Canvas.Name != "HTTPS Redirector" {
		t.Errorf("Canvas.Name = %q", got.Nodes[0].Canvas.Name)
	}
	if got.Nodes[0].Canvas.CostEstimate != 10.50 {
		t.Errorf("Canvas.CostEstimate = %v", got.Nodes[0].Canvas.CostEstimate)
	}
}

func TestPG_InfraStore_TopologyUpsertRemovesOldNodes(t *testing.T) {
	pool := testPool(t)
	engStore := pgstore.NewEngagementStore(pool)
	infraStore := pgstore.NewInfraStore(pool)
	ctx := context.Background()

	engID, _ := engStore.Create(ctx, domain.Engagement{
		Client: "prune test", Status: domain.EngagementDraft,
		Scope: domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
	})

	node := func(id string) domain.Node {
		return domain.Node{
			ID: id, EngagementID: engID,
			Spec:   domain.NodeSpec{Type: domain.NodeRedirector, Cloud: domain.CloudAWS, Region: "us-east-1", Size: "t3.small"},
			Status: domain.NodePending, Health: domain.HealthUnknown,
		}
	}

	// Save 2 nodes.
	_ = infraStore.SaveTopology(ctx, domain.Topology{
		EngagementID: engID,
		Nodes:        []domain.Node{node("00000000-0000-0000-0001-000000000001"), node("00000000-0000-0000-0001-000000000002")},
	})

	// Save only 1 node — second should be pruned.
	_ = infraStore.SaveTopology(ctx, domain.Topology{
		EngagementID: engID,
		Nodes:        []domain.Node{node("00000000-0000-0000-0001-000000000001")},
	})

	got, err := infraStore.GetTopology(ctx, engID)
	if err != nil {
		t.Fatalf("GetTopology: %v", err)
	}
	if len(got.Nodes) != 1 {
		t.Errorf("expected 1 node after prune, got %d", len(got.Nodes))
	}
}

func TestPG_InfraStore_UpdateNodeStatus(t *testing.T) {
	pool := testPool(t)
	engStore := pgstore.NewEngagementStore(pool)
	infraStore := pgstore.NewInfraStore(pool)
	ctx := context.Background()

	engID, _ := engStore.Create(ctx, domain.Engagement{
		Client: "status test", Status: domain.EngagementDraft,
		Scope: domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
	})
	nodeID := "00000000-0000-0000-0002-000000000001"
	_ = infraStore.SaveTopology(ctx, domain.Topology{
		EngagementID: engID,
		Nodes: []domain.Node{{
			ID: nodeID, EngagementID: engID,
			Spec:   domain.NodeSpec{Type: domain.NodeRedirector, Cloud: domain.CloudAWS, Region: "us-east-1", Size: "t3.small"},
			Status: domain.NodePending, Health: domain.HealthUnknown,
		}},
	})

	if err := infraStore.UpdateNodeStatus(ctx, nodeID, domain.NodeLive, domain.HealthHealthy); err != nil {
		t.Fatalf("UpdateNodeStatus: %v", err)
	}

	nodes, err := infraStore.NodesForEngagement(ctx, engID)
	if err != nil {
		t.Fatalf("NodesForEngagement: %v", err)
	}
	if len(nodes) == 0 || nodes[0].Status != domain.NodeLive {
		t.Errorf("expected node to be live, got %v", nodes)
	}
}

// --- ScenarioStore ---

func TestPG_ScenarioStore_SaveAndGet(t *testing.T) {
	pool := testPool(t)
	engStore := pgstore.NewEngagementStore(pool)
	scenStore := pgstore.NewScenarioStore(pool)
	ctx := context.Background()

	engID, _ := engStore.Create(ctx, domain.Engagement{
		Client: "scenario test", Status: domain.EngagementDraft,
		Scope: domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
	})

	run := domain.ScenarioRun{
		EngagementID: engID,
		ScenarioID:   "apt29",
		Status:       domain.ExecRunning,
		StartedAt:    time.Now().UTC(),
		Results: []domain.Result{
			{
				TechniqueAttackID: "T1059.001",
				Status:            domain.ExecSuccess,
				Output:            "executed",
				StartedAt:         time.Now().UTC(),
				FinishedAt:        time.Now().UTC(),
			},
		},
	}

	id, err := scenStore.SaveRun(ctx, run)
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	got, err := scenStore.GetRun(ctx, id)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.ScenarioID != run.ScenarioID {
		t.Errorf("ScenarioID = %q", got.ScenarioID)
	}
	if len(got.Results) != 1 {
		t.Errorf("expected 1 result, got %d", len(got.Results))
	}
	if got.Results[0].TechniqueAttackID != "T1059.001" {
		t.Errorf("AttackID = %q", got.Results[0].TechniqueAttackID)
	}
}

// --- CredentialStore ---

func TestPG_CredentialStore_UpsertAndGet(t *testing.T) {
	pool := testPool(t)
	engStore := pgstore.NewEngagementStore(pool)
	credStore := pgstore.NewCredentialStore(pool)
	ctx := context.Background()

	engID, _ := engStore.Create(ctx, domain.Engagement{
		Client: "cred test", Status: domain.EngagementDraft,
		Scope: domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
	})

	ct := []byte("fake-ciphertext-bytes")
	nonce := []byte("fakencye12345678")

	if err := credStore.Upsert(ctx, engID, "aws", ct, nonce, "key-v1"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	gotCT, gotNonce, gotKeyID, err := credStore.GetCiphertext(ctx, engID, "aws")
	if err != nil {
		t.Fatalf("GetCiphertext: %v", err)
	}
	if string(gotCT) != string(ct) {
		t.Errorf("ciphertext mismatch")
	}
	if string(gotNonce) != string(nonce) {
		t.Errorf("nonce mismatch")
	}
	if gotKeyID != "key-v1" {
		t.Errorf("keyID = %q, want key-v1", gotKeyID)
	}

	// Upsert again (overwrite).
	if err = credStore.Upsert(ctx, engID, "aws", []byte("new-ct"), nonce, "key-v2"); err != nil {
		t.Fatalf("Upsert overwrite: %v", err)
	}
	gotCT, _, gotKeyID, _ = credStore.GetCiphertext(ctx, engID, "aws")
	if string(gotCT) != "new-ct" {
		t.Errorf("overwrite failed")
	}
	if gotKeyID != "key-v2" {
		t.Errorf("keyID after overwrite = %q", gotKeyID)
	}

	// TouchLastUsed.
	if err = credStore.TouchLastUsed(ctx, engID, "aws"); err != nil {
		t.Fatalf("TouchLastUsed: %v", err)
	}
	meta, err := credStore.GetMeta(ctx, engID, "aws")
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if meta.LastUsedAt == nil {
		t.Error("LastUsedAt should not be nil after touch")
	}

	// ListForEngagement.
	_ = credStore.Upsert(ctx, engID, "gcp", []byte("gcp-ct"), nonce, "k")
	list, err := credStore.ListForEngagement(ctx, engID)
	if err != nil {
		t.Fatalf("ListForEngagement: %v", err)
	}
	if len(list) < 2 {
		t.Errorf("expected >= 2 credentials, got %d", len(list))
	}
}

// --- JobStore ---

func TestPG_JobStore_CreateGetUpdateList(t *testing.T) {
	pool := testPool(t)
	engStore := pgstore.NewEngagementStore(pool)
	jobStore := pgstore.NewJobStore(pool)
	ctx := context.Background()

	engID, _ := engStore.Create(ctx, domain.Engagement{
		Client: "job test", Status: domain.EngagementDraft,
		Scope: domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
	})

	j := domain.Job{
		EngagementID: engID,
		Kind:         domain.JobDeploy,
		Status:       domain.JobPending,
		Detail:       map[string]any{"provider": "aws"},
	}

	id, err := jobStore.Create(ctx, j)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := jobStore.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Kind != domain.JobDeploy {
		t.Errorf("Kind = %q", got.Kind)
	}
	if got.Detail["provider"] != "aws" {
		t.Errorf("Detail = %v", got.Detail)
	}

	// Transition to running.
	if err = jobStore.UpdateStatus(ctx, id, domain.JobRunning, ""); err != nil {
		t.Fatalf("UpdateStatus running: %v", err)
	}
	running, err := jobStore.ListRunning(ctx)
	if err != nil {
		t.Fatalf("ListRunning: %v", err)
	}
	found := false
	for _, r := range running {
		if r.ID == id {
			found = true
		}
	}
	if !found {
		t.Errorf("job %s not in ListRunning", id)
	}

	// Transition to done.
	if err = jobStore.UpdateStatus(ctx, id, domain.JobDone, ""); err != nil {
		t.Fatalf("UpdateStatus done: %v", err)
	}
	got, _ = jobStore.Get(ctx, id)
	if got.FinishedAt == nil {
		t.Error("FinishedAt should be set for done jobs")
	}
}

// --- Audit Logger ---

func TestPG_AuditLogger_Record(t *testing.T) {
	pool := testPool(t)
	logger := audpg.New(pool)
	ctx := context.Background()

	if err := logger.Record(ctx, audit.Event{
		Actor:  "tester",
		Action: "integration.test",
		Target: "test-target",
		Detail: "integration test run",
		At:     time.Now(),
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
}

func TestPG_AuditLogger_ImmutabilityTrigger(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// Insert a row via the logger.
	logger := audpg.New(pool)
	if err := logger.Record(ctx, audit.Event{
		Actor:  "tester",
		Action: "trigger.test",
		At:     time.Now(),
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Attempt UPDATE — must fail with the immutability trigger.
	_, errUpdate := pool.Exec(ctx,
		`UPDATE audit_events SET actor='hacked' WHERE action='trigger.test'`)
	if errUpdate == nil {
		t.Fatal("expected UPDATE on audit_events to fail due to immutability trigger, but it succeeded")
	}
	t.Logf("UPDATE correctly rejected: %v", errUpdate)

	// Attempt DELETE — must also fail.
	_, errDelete := pool.Exec(ctx,
		`DELETE FROM audit_events WHERE action='trigger.test'`)
	if errDelete == nil {
		t.Fatal("expected DELETE on audit_events to fail due to immutability trigger, but it succeeded")
	}
	t.Logf("DELETE correctly rejected: %v", errDelete)
}
