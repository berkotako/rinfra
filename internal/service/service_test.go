package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/cloud/fake"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration"
	"github.com/rinfra/rinfra/internal/secrets"
	"github.com/rinfra/rinfra/internal/service"
	"github.com/rinfra/rinfra/internal/store"
	"github.com/rinfra/rinfra/internal/store/memstore"
)

// testLogger returns a logger that discards output, keeping test output clean.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testEnc returns an Encrypter with a fixed key suitable for testing.
func testEnc(t *testing.T) *secrets.Encrypter {
	t.Helper()
	// 32 bytes of 0xAB base64-encoded.
	enc, err := secrets.New("q6urq6urq6urq6urq6urq6urq6urq6urq6urq6urq6s=")
	if err != nil {
		t.Fatalf("testEnc: %v", err)
	}
	return enc
}

// testStores bundles all in-memory stores for a single test.
type testStores struct {
	eng      *memstore.EngagementStore
	infra    *memstore.InfraStore
	scenario *memstore.ScenarioStore
	cred     *memstore.CredentialStore
	job      *memstore.JobStore
	audit    *memstore.AuditLogger
}

func newTestStores() testStores {
	return testStores{
		eng:      memstore.NewEngagementStore(),
		infra:    memstore.NewInfraStore(),
		scenario: memstore.NewScenarioStore(),
		cred:     memstore.NewCredentialStore(),
		job:      memstore.NewJobStore(),
		audit:    memstore.NewAuditLogger(),
	}
}

// authorizedEngagement creates and authorizes an engagement ready for deploy.
func authorizedEngagement(t *testing.T, ctx context.Context, engSvc *service.EngagementService) domain.Engagement {
	t.Helper()
	eng := domain.Engagement{
		Client:         "Test Client",
		Codename:       "OPERATION TEST",
		Status:         domain.EngagementDraft,
		Scope:          domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
		EngagementType: domain.EngagementTypeRedTeam,
		RoE: domain.RulesOfEngagement{
			WindowStart: time.Now().Add(-time.Hour),
			WindowEnd:   time.Now().Add(time.Hour),
		},
	}
	created, err := engSvc.Create(ctx, eng, "test-operator")
	if err != nil {
		t.Fatalf("create engagement: %v", err)
	}
	auth := domain.Authorization{
		AuthorizedBy: "test-approver",
		DocumentRef:  "auth-doc-1",
		GrantedAt:    time.Now().Add(-30 * time.Minute),
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	authorized, err := engSvc.Authorize(ctx, created.ID, auth, "test-operator")
	if err != nil {
		t.Fatalf("authorize engagement: %v", err)
	}
	return authorized
}

// buildInfraService wires up an InfraService with the given stores and provider.
func buildInfraService(t *testing.T, s testStores, hub *service.Hub) *service.InfraService {
	t.Helper()
	return service.NewInfraService(
		s.eng, s.infra, s.cred, s.job, s.audit,
		testEnc(t), hub, testLogger(),
	)
}

// saveTestTopology stores a valid topology (redirector + c2_server with edge) for engID.
func saveTestTopology(t *testing.T, ctx context.Context, svc *service.InfraService, engID string) {
	t.Helper()
	provType := fake.CloudProviderTypeFake
	topology := domain.Topology{
		EngagementID: engID,
		Nodes: []domain.Node{
			{
				ID:     "node-redir-1",
				Spec:   domain.NodeSpec{Type: domain.NodeRedirector, Cloud: provType, Region: "nyc3", Size: "s-1vcpu-1gb", ProfileName: "https"},
				Canvas: domain.NodeCanvas{Name: "redir-01"},
			},
			{
				ID:     "node-c2-1",
				Spec:   domain.NodeSpec{Type: domain.NodeC2Server, Cloud: provType, Region: "nyc3", Size: "s-2vcpu-4gb", C2Framework: "sliver"},
				Canvas: domain.NodeCanvas{Name: "c2-01"},
			},
		},
		Edges: []domain.Edge{{FromNodeID: "node-redir-1", ToNodeID: "node-c2-1"}},
	}
	if err := svc.SaveTopology(ctx, engID, topology, "test-operator"); err != nil {
		t.Fatalf("save topology: %v", err)
	}
}

// waitForJob polls until a job reaches a terminal state (done or failed).
func waitForJob(t *testing.T, ctx context.Context, jobs store.JobStore, jobID string) domain.JobStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, err := jobs.Get(ctx, jobID)
		if err != nil {
			t.Fatalf("get job %s: %v", jobID, err)
		}
		if j.Status == domain.JobDone || j.Status == domain.JobFailed {
			return j.Status
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("job %s did not complete within timeout", jobID)
	return domain.JobFailed
}

// hasAuditAction returns true if the audit log contains at least one event with
// the given action for the given engagement.
func hasAuditAction(audit *memstore.AuditLogger, action, engagementID string) bool {
	for _, ev := range audit.Events() {
		if ev.Action == action && (engagementID == "" || ev.EngagementID == engagementID) {
			return true
		}
	}
	return false
}

// ---------- Tests ----------

func TestDeployGate_DraftEngagement(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()

	svcEng := service.NewEngagementService(s.eng, s.audit)
	svcInfra := buildInfraService(t, s, hub)

	// Draft engagement — not authorized.
	created, err := svcEng.Create(ctx, domain.Engagement{
		Client:         "ACME",
		Codename:       "DRAFT-OP",
		Status:         domain.EngagementDraft,
		Scope:          domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
		EngagementType: domain.EngagementTypeRedTeam,
	}, "op1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = svcInfra.Deploy(ctx, created.ID, "op1")
	if err == nil {
		t.Fatal("expected error deploying draft engagement, got nil")
	}
	if !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("want ErrNotAuthorized, got %v", err)
	}
}

func TestDeployFullCycle_FakeProvider(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()

	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)

	svcInfra := buildInfraService(t, s, hub)
	saveTestTopology(t, ctx, svcInfra, eng.ID)

	jobID, err := svcInfra.Deploy(ctx, eng.ID, "op1")
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if jobID == "" {
		t.Fatal("expected non-empty jobID")
	}

	status := waitForJob(t, ctx, s.job, jobID)
	if status != domain.JobDone {
		t.Errorf("deploy job: want done, got %s", status)
	}

	// All nodes should be live with an IP.
	topo, err := svcInfra.GetTopology(ctx, eng.ID)
	if err != nil {
		t.Fatalf("get topology: %v", err)
	}
	for _, n := range topo.Nodes {
		if n.Status != domain.NodeLive {
			t.Errorf("node %s: want live, got %s", n.ID, n.Status)
		}
		if n.PublicIP == "" {
			t.Errorf("node %s: want non-empty PublicIP", n.ID)
		}
	}

	if !hasAuditAction(s.audit, "infra.deploy", eng.ID) {
		t.Error("expected infra.deploy audit event")
	}
	if !hasAuditAction(s.audit, "infra.deploy.complete", eng.ID) {
		t.Error("expected infra.deploy.complete audit event")
	}
}

func TestTeardownCycle_FakeProvider(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()

	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)
	svcInfra := buildInfraService(t, s, hub)
	saveTestTopology(t, ctx, svcInfra, eng.ID)

	// Deploy first.
	deployJobID, _ := svcInfra.Deploy(ctx, eng.ID, "op1")
	waitForJob(t, ctx, s.job, deployJobID)

	// Now teardown.
	teardownJobID, err := svcInfra.Teardown(ctx, eng.ID, "op1")
	if err != nil {
		t.Fatalf("teardown: %v", err)
	}
	status := waitForJob(t, ctx, s.job, teardownJobID)
	if status != domain.JobDone {
		t.Errorf("teardown job: want done, got %s", status)
	}

	// All nodes should be destroyed.
	topo, err := svcInfra.GetTopology(ctx, eng.ID)
	if err != nil {
		t.Fatalf("get topology: %v", err)
	}
	for _, n := range topo.Nodes {
		if n.Status != domain.NodeDestroyed {
			t.Errorf("node %s: want destroyed, got %s", n.ID, n.Status)
		}
	}

	if !hasAuditAction(s.audit, "infra.teardown", eng.ID) {
		t.Error("expected infra.teardown audit event")
	}
}

func TestDuplicateDeployReturnsErrJobRunning(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()

	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)

	// Use a slow fake provider so the first job stays running.
	// We need to register it — but the global registry already has "fake".
	// The InfraService uses cloud.Get(cloud) which looks up the registry.
	// Our topology uses fake.CloudProviderTypeFake so the global fake is used.
	svcInfra := buildInfraService(t, s, hub)
	saveTestTopology(t, ctx, svcInfra, eng.ID)

	// First deploy succeeds.
	_, err := svcInfra.Deploy(ctx, eng.ID, "op1")
	if err != nil {
		t.Fatalf("first deploy: %v", err)
	}

	// Second deploy immediately after — the job row should still be running or
	// pending. Since the fake provider has 0 delay, we need to create a separate
	// job manually to simulate the running state.
	// Instead, test with the job-store directly: mark the first job as running.
	running, err := s.job.ListRunning(ctx)
	if err != nil {
		t.Fatalf("list running: %v", err)
	}
	if len(running) == 0 {
		// Fast path: zero-delay fake completed already. Mark artificially.
		jobs, _ := s.job.ListRunning(ctx)
		t.Logf("running jobs: %d (may have completed)", len(jobs))
		// Create a fake running job.
		fakeJobID, _ := s.job.Create(ctx, domain.Job{
			EngagementID: eng.ID,
			Kind:         domain.JobDeploy,
			Status:       domain.JobPending,
		})
		_ = s.job.UpdateStatus(ctx, fakeJobID, domain.JobRunning, "")
	}

	_, err = svcInfra.Deploy(ctx, eng.ID, "op1")
	if err == nil {
		t.Fatal("expected ErrJobRunning on concurrent deploy, got nil")
	}
	if !errors.Is(err, service.ErrJobRunning) {
		t.Errorf("want ErrJobRunning, got %v", err)
	}
}

func TestCredentialsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()

	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng, err := svcEng.Create(ctx, domain.Engagement{
		Client:         "Cred Test",
		Codename:       "CRED-OP",
		Status:         domain.EngagementDraft,
		Scope:          domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
		EngagementType: domain.EngagementTypeRedTeam,
	}, "op1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	enc := testEnc(t)
	svcInfra := service.NewInfraService(s.eng, s.infra, s.cred, s.job, s.audit, enc, hub, testLogger())

	plaintext := []byte(`{"token":"super-secret-do-token"}`)
	if err := svcInfra.PutCredentials(ctx, eng.ID, "digitalocean", plaintext, "op1"); err != nil {
		t.Fatalf("put credentials: %v", err)
	}

	// Metadata (no plaintext).
	meta, err := svcInfra.GetCredentialMeta(ctx, eng.ID, "digitalocean")
	if err != nil {
		t.Fatalf("get meta: %v", err)
	}
	if meta.Provider != "digitalocean" {
		t.Errorf("meta provider: want digitalocean, got %s", meta.Provider)
	}
	if meta.KeyID == "" {
		t.Error("meta key_id must not be empty")
	}

	if !hasAuditAction(s.audit, "credential.store", eng.ID) {
		t.Error("expected credential.store audit event")
	}

	// Decrypt and verify the round-trip.
	ct, nonce, keyID, err := s.cred.GetCiphertext(ctx, eng.ID, "digitalocean")
	if err != nil {
		t.Fatalf("get ciphertext: %v", err)
	}
	decrypted, err := enc.Decrypt(secrets.Envelope{Ciphertext: ct, Nonce: nonce, KeyID: keyID})
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted mismatch: got %q, want %q", decrypted, plaintext)
	}
}

func TestAllPrivilegedActionsAudited(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()

	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)
	svcInfra := buildInfraService(t, s, hub)
	saveTestTopology(t, ctx, svcInfra, eng.ID)

	_, err := svcInfra.Deploy(ctx, eng.ID, "op1")
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}

	required := []string{
		"engagement.create",
		"engagement.authorize",
		"infra.deploy",
	}
	for _, action := range required {
		if !hasAuditAction(s.audit, action, "") {
			t.Errorf("missing audit event for action: %s", action)
		}
	}
}

// TestCredentialJSONRoundTrip verifies that MarshalCredentials + DecryptCredentials
// produce the original Raw map.
func TestCredentialJSONRoundTrip(t *testing.T) {
	enc := testEnc(t)

	input := map[string]string{
		"DIGITALOCEAN_TOKEN": "dop_v1_test123",
		"EXTRA_KEY":          "extra_value",
	}

	// Marshal to JSON bytes.
	plaintext, err := service.MarshalCredentials(input)
	if err != nil {
		t.Fatalf("MarshalCredentials: %v", err)
	}

	// Verify raw JSON round-trip.
	var decoded map[string]string
	if err := json.Unmarshal(plaintext, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	for k, v := range input {
		if decoded[k] != v {
			t.Errorf("key %q: want %q, got %q", k, v, decoded[k])
		}
	}

	// Encrypt then DecryptCredentials.
	env, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	creds, err := service.DecryptCredentials(enc, domain.CloudDigitalOcean, env.Ciphertext, env.Nonce, env.KeyID)
	if err != nil {
		t.Fatalf("DecryptCredentials: %v", err)
	}
	if creds.Provider != domain.CloudDigitalOcean {
		t.Errorf("provider: want %s, got %s", domain.CloudDigitalOcean, creds.Provider)
	}
	for k, v := range input {
		if creds.Raw[k] != v {
			t.Errorf("Raw[%q]: want %q, got %q", k, v, creds.Raw[k])
		}
	}
}

// TestDeployRealProviderNoCreds verifies that a node whose cloud provider is a
// real (ProgramBuilder) provider fails with ErrNoCloudCredentials when no
// credentials are stored, before engine.Deploy is ever called.
func TestDeployRealProviderNoCreds(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()

	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)

	svcInfra := buildInfraService(t, s, hub)

	// Use a DO node — DO provider implements ProgramBuilder so the engine path
	// is taken. We wire a nil-backend engine (orchestration.New with a temp dir)
	// so the engine is present but DO builder is NOT registered, which means the
	// engine path is triggered by the ProgramBuilder check on cloud.Get(DO).
	// Wait — the DO provider IS a ProgramBuilder (registered in cloud registry).
	// We need to ensure the engine is wired. Use a real engine (no Pulumi CLI
	// needed because engine.Deploy will never be reached — creds load fails first).
	stateDir := t.TempDir()
	testEngine := orchestration.New(stateDir, testLogger())
	// Register the DO ProgramBuilder from the cloud registry.
	doProv, err := cloud.Get(domain.CloudDigitalOcean)
	if err != nil {
		t.Skipf("DO provider not registered (import may be missing): %v", err)
	}
	doBuilder, ok := doProv.(orchestration.ProgramBuilder)
	if !ok {
		t.Skip("DO provider does not implement ProgramBuilder")
	}
	testEngine.RegisterBuilder(domain.CloudDigitalOcean, doBuilder)
	svcInfra.WithEngine(testEngine)

	// Save a valid topology with DO nodes (real provider) so deploy passes
	// validation and reaches the async provisioning path (where the no-creds
	// failure is surfaced).
	topology := domain.Topology{
		EngagementID: eng.ID,
		Nodes: []domain.Node{
			{
				ID:     "node-redir-do",
				Spec:   domain.NodeSpec{Type: domain.NodeRedirector, Cloud: domain.CloudDigitalOcean, Region: "nyc3", Size: "s-1vcpu-1gb", ProfileName: "https"},
				Canvas: domain.NodeCanvas{Name: "redir-do", FrontDomain: "cdn.example.com"},
			},
			{
				ID:     "node-c2-do",
				Spec:   domain.NodeSpec{Type: domain.NodeC2Server, Cloud: domain.CloudDigitalOcean, Region: "nyc3", Size: "s-2vcpu-4gb", C2Framework: "sliver"},
				Canvas: domain.NodeCanvas{Name: "c2-do"},
			},
		},
		Edges: []domain.Edge{{FromNodeID: "node-redir-do", ToNodeID: "node-c2-do"}},
	}
	if err := svcInfra.SaveTopology(ctx, eng.ID, topology, "op1"); err != nil {
		t.Fatalf("save topology: %v", err)
	}

	// Deploy with NO credentials stored → should fail with nodes marked failed,
	// and credential.missing audit event should be emitted.
	jobID, err := svcInfra.Deploy(ctx, eng.ID, "op1")
	if err != nil {
		t.Fatalf("deploy returned error (want async failure): %v", err)
	}

	status := waitForJob(t, ctx, s.job, jobID)
	if status != domain.JobFailed {
		t.Errorf("deploy job: want failed (no creds), got %s", status)
	}

	// All nodes should be marked failed.
	topo, err := svcInfra.GetTopology(ctx, eng.ID)
	if err != nil {
		t.Fatalf("get topology: %v", err)
	}
	for _, n := range topo.Nodes {
		if n.Status != domain.NodeFailed {
			t.Errorf("node %s: want failed (no creds), got %s", n.ID, n.Status)
		}
	}

	// credential.missing must be audited.
	if !hasAuditAction(s.audit, "credential.missing", eng.ID) {
		t.Error("expected credential.missing audit event when creds are absent")
	}
}
