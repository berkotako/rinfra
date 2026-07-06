package memstore_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rinfra/rinfra/internal/audit"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/store"
	"github.com/rinfra/rinfra/internal/store/memstore"
)

var ctx = context.Background()

// --- EngagementStore ---

func TestEngagementStore_CreateAndGet(t *testing.T) {
	s := memstore.NewEngagementStore()
	e := domain.Engagement{
		Client: "Acme Corp",
		Status: domain.EngagementDraft,
		Scope:  domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
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
}

func TestEngagementStore_GetNotFound(t *testing.T) {
	s := memstore.NewEngagementStore()
	_, err := s.Get(ctx, "nonexistent")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestEngagementStore_List(t *testing.T) {
	s := memstore.NewEngagementStore()
	for i := range 3 {
		_, err := s.Create(ctx, domain.Engagement{Client: fmt.Sprintf("client-%d", i), Status: domain.EngagementDraft})
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}
	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 engagements, got %d", len(list))
	}
}

func TestEngagementStore_UpdateStatus(t *testing.T) {
	s := memstore.NewEngagementStore()
	id, _ := s.Create(ctx, domain.Engagement{Client: "x", Status: domain.EngagementDraft})

	if err := s.UpdateStatus(ctx, id, domain.EngagementAuthorized); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, _ := s.Get(ctx, id)
	if got.Status != domain.EngagementAuthorized {
		t.Errorf("Status = %q, want authorized", got.Status)
	}
}

func TestEngagementStore_UpdateStatusNotFound(t *testing.T) {
	s := memstore.NewEngagementStore()
	err := s.UpdateStatus(ctx, "missing", domain.EngagementAuthorized)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

// --- InfraStore ---

func TestInfraStore_SaveAndGetTopology(t *testing.T) {
	s := memstore.NewInfraStore()
	topo := domain.Topology{
		EngagementID: "eng-1",
		Nodes: []domain.Node{
			{ID: "n1", Spec: domain.NodeSpec{Type: domain.NodeRedirector, Cloud: domain.CloudAWS, Region: "us-east-1", Size: "t3.small"}},
			{ID: "n2", Spec: domain.NodeSpec{Type: domain.NodeC2Server, Cloud: domain.CloudGCP, Region: "us-central1", Size: "n1-standard-1"}},
		},
		Edges: []domain.Edge{{FromNodeID: "n1", ToNodeID: "n2"}},
	}
	if err := s.SaveTopology(ctx, topo); err != nil {
		t.Fatalf("SaveTopology: %v", err)
	}

	got, err := s.GetTopology(ctx, "eng-1")
	if err != nil {
		t.Fatalf("GetTopology: %v", err)
	}
	if len(got.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(got.Nodes))
	}
	if len(got.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(got.Edges))
	}
}

func TestInfraStore_UpdateNodeStatus(t *testing.T) {
	s := memstore.NewInfraStore()
	_ = s.SaveTopology(ctx, domain.Topology{
		EngagementID: "eng-1",
		Nodes:        []domain.Node{{ID: "n1", Spec: domain.NodeSpec{Type: domain.NodeRedirector, Cloud: domain.CloudAWS, Region: "us-east-1", Size: "t3.small"}}},
	})

	if err := s.UpdateNodeStatus(ctx, "n1", domain.NodeLive, domain.HealthHealthy); err != nil {
		t.Fatalf("UpdateNodeStatus: %v", err)
	}
	nodes, _ := s.NodesForEngagement(ctx, "eng-1")
	if len(nodes) == 0 {
		t.Fatal("no nodes returned")
	}
	if nodes[0].Status != domain.NodeLive {
		t.Errorf("Status = %q, want live", nodes[0].Status)
	}
	if nodes[0].Health != domain.HealthHealthy {
		t.Errorf("Health = %q, want healthy", nodes[0].Health)
	}
}

func TestInfraStore_UpdateNodeStatusNotFound(t *testing.T) {
	s := memstore.NewInfraStore()
	err := s.UpdateNodeStatus(ctx, "missing", domain.NodeLive, domain.HealthHealthy)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestInfraStore_UpdateNodeProvisioning(t *testing.T) {
	s := memstore.NewInfraStore()
	_ = s.SaveTopology(ctx, domain.Topology{
		EngagementID: "eng-1",
		Nodes:        []domain.Node{{ID: "n1", Spec: domain.NodeSpec{Type: domain.NodeRedirector, Cloud: domain.CloudAWS, Region: "us-east-1", Size: "t3.small"}}},
	})
	if err := s.UpdateNodeProvisioning(ctx, "n1", "i-abc", "203.0.113.5", domain.NodeLive, domain.HealthHealthy); err != nil {
		t.Fatalf("UpdateNodeProvisioning: %v", err)
	}
	nodes, _ := s.NodesForEngagement(ctx, "eng-1")
	if len(nodes) == 0 {
		t.Fatal("no nodes returned")
	}
	n := nodes[0]
	if n.ProviderRef != "i-abc" || n.PublicIP != "203.0.113.5" || n.Status != domain.NodeLive || n.Health != domain.HealthHealthy {
		t.Errorf("node not fully updated: %+v", n)
	}
}

// TestInfraStore_UpdateNodeProvisioningNoResurrect verifies the targeted update
// never re-inserts a node a concurrent SaveTopology removed — it returns
// ErrNotFound and leaves the topology without the deleted node (the whole-topology
// read-modify-write it replaced could resurrect it).
func TestInfraStore_UpdateNodeProvisioningNoResurrect(t *testing.T) {
	s := memstore.NewInfraStore()
	_ = s.SaveTopology(ctx, domain.Topology{
		EngagementID: "eng-1",
		Nodes:        []domain.Node{{ID: "n1", Spec: domain.NodeSpec{Type: domain.NodeRedirector, Cloud: domain.CloudAWS, Region: "us-east-1", Size: "t3.small"}}},
	})
	// User deletes the node mid-deploy (SaveTopology without n1).
	_ = s.SaveTopology(ctx, domain.Topology{EngagementID: "eng-1", Nodes: nil})

	err := s.UpdateNodeProvisioning(ctx, "n1", "i-abc", "203.0.113.5", domain.NodeLive, domain.HealthHealthy)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound for deleted node, got: %v", err)
	}
	nodes, _ := s.NodesForEngagement(ctx, "eng-1")
	if len(nodes) != 0 {
		t.Errorf("deleted node must not be resurrected, got %d nodes", len(nodes))
	}
}

// --- ScenarioStore ---

func TestScenarioStore_SaveAndGet(t *testing.T) {
	s := memstore.NewScenarioStore()
	run := domain.ScenarioRun{
		EngagementID: "eng-1",
		ScenarioID:   "apt29",
		Status:       domain.ExecPending,
	}
	id, err := s.SaveRun(ctx, run)
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	got, err := s.GetRun(ctx, id)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.ScenarioID != run.ScenarioID {
		t.Errorf("ScenarioID = %q, want %q", got.ScenarioID, run.ScenarioID)
	}
}

func TestScenarioStore_GetNotFound(t *testing.T) {
	s := memstore.NewScenarioStore()
	_, err := s.GetRun(ctx, "missing")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

// --- CredentialStore ---

func TestCredentialStore_UpsertAndGet(t *testing.T) {
	s := memstore.NewCredentialStore()
	ct := []byte("fake-ciphertext")
	nonce := []byte("fake-nonce-12345")

	if err := s.Upsert(ctx, "eng-1", "aws", ct, nonce, "key-v1"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	gotCT, gotNonce, gotKeyID, err := s.GetCiphertext(ctx, "eng-1", "aws")
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
}

func TestCredentialStore_Upsert_Overwrites(t *testing.T) {
	s := memstore.NewCredentialStore()
	_ = s.Upsert(ctx, "eng-1", "aws", []byte("old"), []byte("old-n"), "key-v1")
	_ = s.Upsert(ctx, "eng-1", "aws", []byte("new"), []byte("new-n"), "key-v2")

	ct, _, keyID, _ := s.GetCiphertext(ctx, "eng-1", "aws")
	if string(ct) != "new" {
		t.Errorf("expected overwritten ciphertext, got %q", ct)
	}
	if keyID != "key-v2" {
		t.Errorf("expected key-v2, got %q", keyID)
	}
}

func TestCredentialStore_TouchLastUsed(t *testing.T) {
	s := memstore.NewCredentialStore()
	_ = s.Upsert(ctx, "eng-1", "aws", []byte("ct"), []byte("n"), "k")

	before := time.Now()
	if err := s.TouchLastUsed(ctx, "eng-1", "aws"); err != nil {
		t.Fatalf("TouchLastUsed: %v", err)
	}

	meta, err := s.GetMeta(ctx, "eng-1", "aws")
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if meta.LastUsedAt == nil {
		t.Fatal("LastUsedAt should not be nil after touch")
	}
	if meta.LastUsedAt.Before(before) {
		t.Errorf("LastUsedAt %v is before touch time %v", meta.LastUsedAt, before)
	}
}

func TestCredentialStore_GetNotFound(t *testing.T) {
	s := memstore.NewCredentialStore()
	_, _, _, err := s.GetCiphertext(ctx, "eng-1", "aws")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestCredentialStore_ListForEngagement(t *testing.T) {
	s := memstore.NewCredentialStore()
	_ = s.Upsert(ctx, "eng-1", "aws", []byte("ct"), []byte("n"), "k")
	_ = s.Upsert(ctx, "eng-1", "gcp", []byte("ct"), []byte("n"), "k")
	_ = s.Upsert(ctx, "eng-2", "aws", []byte("ct"), []byte("n"), "k")

	list, err := s.ListForEngagement(ctx, "eng-1")
	if err != nil {
		t.Fatalf("ListForEngagement: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 credentials for eng-1, got %d", len(list))
	}
}

// --- JobStore ---

func TestJobStore_CreateAndGet(t *testing.T) {
	s := memstore.NewJobStore()
	j := domain.Job{
		EngagementID: "eng-1",
		Kind:         domain.JobDeploy,
		Status:       domain.JobPending,
	}
	id, err := s.Create(ctx, j)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Kind != domain.JobDeploy {
		t.Errorf("Kind = %q, want deploy", got.Kind)
	}
}

// TestJobStore_SingleActiveInfraJob verifies the atomic guard: at most one active
// (pending/running) deploy/teardown job per engagement, while scenario runs and
// other engagements are unaffected, and a terminal job frees the slot.
func TestJobStore_SingleActiveInfraJob(t *testing.T) {
	s := memstore.NewJobStore()

	id1, err := s.Create(ctx, domain.Job{EngagementID: "eng-1", Kind: domain.JobDeploy, Status: domain.JobPending})
	if err != nil {
		t.Fatalf("first deploy job: %v", err)
	}

	// A second active infra job for the same engagement is rejected — even a
	// teardown, and even while the first is only PENDING (not yet running).
	if _, err := s.Create(ctx, domain.Job{EngagementID: "eng-1", Kind: domain.JobTeardown, Status: domain.JobPending}); !errors.Is(err, store.ErrActiveJobExists) {
		t.Errorf("second active infra job: got %v, want ErrActiveJobExists", err)
	}

	// A scenario-run job is exempt from the guard.
	if _, err := s.Create(ctx, domain.Job{EngagementID: "eng-1", Kind: domain.JobScenarioRun, Status: domain.JobPending}); err != nil {
		t.Errorf("scenario-run job should be allowed alongside a deploy: %v", err)
	}

	// A different engagement is unaffected.
	if _, err := s.Create(ctx, domain.Job{EngagementID: "eng-2", Kind: domain.JobDeploy, Status: domain.JobPending}); err != nil {
		t.Errorf("other engagement's deploy should be allowed: %v", err)
	}

	// Once the first job reaches a terminal state, the slot is free again.
	if err := s.UpdateStatus(ctx, id1, domain.JobDone, ""); err != nil {
		t.Fatalf("finish first job: %v", err)
	}
	if _, err := s.Create(ctx, domain.Job{EngagementID: "eng-1", Kind: domain.JobDeploy, Status: domain.JobPending}); err != nil {
		t.Errorf("new deploy after previous finished should be allowed: %v", err)
	}
}

// TestJobStore_ListActive verifies ListActive returns pending+running jobs
// (what boot reconciliation must clear) while ListRunning stays running-only.
func TestJobStore_ListActive(t *testing.T) {
	s := memstore.NewJobStore()
	_, _ = s.Create(ctx, domain.Job{EngagementID: "e1", Kind: domain.JobDeploy, Status: domain.JobPending})
	r, _ := s.Create(ctx, domain.Job{EngagementID: "e2", Kind: domain.JobTeardown, Status: domain.JobPending})
	_ = s.UpdateStatus(ctx, r, domain.JobRunning, "")
	d, _ := s.Create(ctx, domain.Job{EngagementID: "e3", Kind: domain.JobDeploy, Status: domain.JobPending})
	_ = s.UpdateStatus(ctx, d, domain.JobDone, "")

	active, err := s.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 2 { // pending e1 + running e2, not done e3
		t.Errorf("ListActive len = %d, want 2 (pending + running)", len(active))
	}
	running, _ := s.ListRunning(ctx)
	if len(running) != 1 {
		t.Errorf("ListRunning len = %d, want 1 (running only)", len(running))
	}
}

func TestJobStore_UpdateStatus(t *testing.T) {
	s := memstore.NewJobStore()
	id, _ := s.Create(ctx, domain.Job{EngagementID: "eng-1", Kind: domain.JobDeploy, Status: domain.JobPending})

	if err := s.UpdateStatus(ctx, id, domain.JobRunning, ""); err != nil {
		t.Fatalf("UpdateStatus running: %v", err)
	}
	j, _ := s.Get(ctx, id)
	if j.Status != domain.JobRunning {
		t.Errorf("Status = %q, want running", j.Status)
	}
	if j.StartedAt == nil {
		t.Error("StartedAt should be set for running jobs")
	}

	if err := s.UpdateStatus(ctx, id, domain.JobDone, ""); err != nil {
		t.Fatalf("UpdateStatus done: %v", err)
	}
	j, _ = s.Get(ctx, id)
	if j.FinishedAt == nil {
		t.Error("FinishedAt should be set for done jobs")
	}
}

func TestJobStore_ListRunning(t *testing.T) {
	s := memstore.NewJobStore()
	id1, _ := s.Create(ctx, domain.Job{EngagementID: "eng-1", Kind: domain.JobDeploy, Status: domain.JobPending})
	id2, _ := s.Create(ctx, domain.Job{EngagementID: "eng-1", Kind: domain.JobTeardown, Status: domain.JobPending})
	_ = s.UpdateStatus(ctx, id1, domain.JobRunning, "")
	_ = s.UpdateStatus(ctx, id2, domain.JobDone, "")

	running, err := s.ListRunning(ctx)
	if err != nil {
		t.Fatalf("ListRunning: %v", err)
	}
	if len(running) != 1 {
		t.Errorf("expected 1 running job, got %d", len(running))
	}
	if running[0].ID != id1 {
		t.Errorf("unexpected running job id %s", running[0].ID)
	}
}

// --- AuditLogger ---

func TestAuditLogger_Record(t *testing.T) {
	l := memstore.NewAuditLogger()
	e := audit.Event{
		EngagementID: "eng-1",
		Actor:        "operator@example.com",
		Action:       "infra.deploy",
		Target:       "node-xyz",
		Detail:       "deploy started",
		At:           time.Now(),
	}
	if err := l.Record(ctx, e); err != nil {
		t.Fatalf("Record: %v", err)
	}
	events := l.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Action != e.Action {
		t.Errorf("Action = %q, want %q", events[0].Action, e.Action)
	}
}

func TestAuditLogger_Concurrent(t *testing.T) {
	l := memstore.NewAuditLogger()
	const n = 100
	done := make(chan struct{})
	for i := range n {
		go func(i int) {
			_ = l.Record(ctx, audit.Event{Action: fmt.Sprintf("action-%d", i), At: time.Now()})
			done <- struct{}{}
		}(i)
	}
	for range n {
		<-done
	}
	if len(l.Events()) != n {
		t.Errorf("expected %d events, got %d", n, len(l.Events()))
	}
}
