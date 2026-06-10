package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/rinfra/rinfra/internal/audit"
	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/secrets"
	"github.com/rinfra/rinfra/internal/store"
)

// ErrJobRunning is returned when a deploy or teardown job is already in
// progress for an engagement.
var ErrJobRunning = errors.New("a deploy or teardown job is already running for this engagement")

// ErrNoCloudCredentials is returned when no cloud credentials exist for the
// engagement's provider.
var ErrNoCloudCredentials = errors.New("no cloud credentials found for this engagement and provider")

// InfraService manages topology, provisioning, and teardown of infrastructure.
type InfraService struct {
	engagements store.EngagementStore
	infra       store.InfraStore
	creds       store.CredentialStore
	jobs        store.JobStore
	audit       audit.Logger
	enc         *secrets.Encrypter
	hub         *Hub
	log         *slog.Logger
}

// NewInfraService constructs an InfraService with the given dependencies.
func NewInfraService(
	engagements store.EngagementStore,
	infra store.InfraStore,
	creds store.CredentialStore,
	jobs store.JobStore,
	a audit.Logger,
	enc *secrets.Encrypter,
	hub *Hub,
	log *slog.Logger,
) *InfraService {
	return &InfraService{
		engagements: engagements,
		infra:       infra,
		creds:       creds,
		jobs:        jobs,
		audit:       a,
		enc:         enc,
		hub:         hub,
		log:         log,
	}
}

// GetTopology returns the stored topology for an engagement.
func (s *InfraService) GetTopology(ctx context.Context, engagementID string) (domain.Topology, error) {
	return s.infra.GetTopology(ctx, engagementID)
}

// SaveTopology persists a topology. Nodes that are live or draining may not be
// changed — the caller must not include live/draining nodes with modified specs.
func (s *InfraService) SaveTopology(ctx context.Context, engagementID string, t domain.Topology, actor string) error {
	// Assign IDs to new nodes.
	for i := range t.Nodes {
		if t.Nodes[i].ID == "" {
			t.Nodes[i].ID = uuid.NewString()
		}
		t.Nodes[i].EngagementID = engagementID
		if t.Nodes[i].Status == "" {
			t.Nodes[i].Status = domain.NodePending
		}
	}
	t.EngagementID = engagementID
	return s.infra.SaveTopology(ctx, t)
}

// ValidateTopology performs server-side checks on a topology:
//   - At least one node.
//   - At least one c2_server.
//   - At least one redirector.
//   - Every c2_server has an inbound edge from a redirector.
//   - The engagement passes CanDeploy (to surface gate errors early).
func (s *InfraService) ValidateTopology(ctx context.Context, engagementID string) ([]string, error) {
	t, err := s.infra.GetTopology(ctx, engagementID)
	if err != nil {
		return nil, err
	}
	eng, err := s.engagements.Get(ctx, engagementID)
	if err != nil {
		return nil, err
	}

	var problems []string
	if len(t.Nodes) == 0 {
		problems = append(problems, "topology has no nodes")
	}

	nodeByID := make(map[string]domain.Node, len(t.Nodes))
	for _, n := range t.Nodes {
		nodeByID[n.ID] = n
	}

	hasC2 := false
	hasRedirector := false
	for _, n := range t.Nodes {
		if n.Spec.Type == domain.NodeC2Server {
			hasC2 = true
		}
		if n.Spec.Type == domain.NodeRedirector {
			hasRedirector = true
		}
	}
	if !hasC2 {
		problems = append(problems, "topology must have at least one c2_server")
	}
	if !hasRedirector {
		problems = append(problems, "topology must have at least one redirector")
	}

	// Check every c2_server has an inbound edge from a redirector.
	inboundFromRedirector := make(map[string]bool)
	for _, e := range t.Edges {
		from, ok := nodeByID[e.FromNodeID]
		if !ok {
			continue
		}
		if from.Spec.Type == domain.NodeRedirector {
			inboundFromRedirector[e.ToNodeID] = true
		}
	}
	for _, n := range t.Nodes {
		if n.Spec.Type == domain.NodeC2Server && !inboundFromRedirector[n.ID] {
			problems = append(problems, fmt.Sprintf("c2_server %q has no inbound edge from a redirector", n.Canvas.Name))
		}
	}

	// Surface authorization gate errors.
	if err := eng.CanDeploy(time.Now()); err != nil {
		problems = append(problems, fmt.Sprintf("authorization check: %v", err))
	}

	return problems, nil
}

// Deploy provisions all pending nodes for an engagement asynchronously.
// It enforces the CanDeploy gate, returns ErrJobRunning if a job is already
// active, creates a Job record, then launches a goroutine to do the work.
func (s *InfraService) Deploy(ctx context.Context, engagementID string, actor string) (string, error) {
	// Authorization gate — enforced before any provisioning.
	eng, err := s.engagements.Get(ctx, engagementID)
	if err != nil {
		return "", err
	}
	if err := eng.CanDeploy(time.Now()); err != nil {
		return "", err
	}

	// Reject if a job is already running.
	if err := s.assertNoActiveJob(ctx, engagementID); err != nil {
		return "", err
	}

	// Record the deploy intent before any cloud call.
	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: engagementID,
		Actor:        actor,
		Action:       "infra.deploy",
		Target:       engagementID,
		Detail:       "deploy initiated",
		At:           time.Now().UTC(),
	})

	job := domain.Job{
		EngagementID: engagementID,
		Kind:         domain.JobDeploy,
		Status:       domain.JobPending,
	}
	jobID, err := s.jobs.Create(ctx, job)
	if err != nil {
		return "", fmt.Errorf("create deploy job: %w", err)
	}

	// Transition engagement to active.
	_ = s.engagements.UpdateStatus(ctx, engagementID, domain.EngagementActive)

	// Launch the async provisioning goroutine.
	go s.runDeploy(context.Background(), engagementID, jobID, actor)

	return jobID, nil
}

// runDeploy executes in a goroutine: provisions each pending node, updates
// status, publishes SSE events, and finishes the job.
func (s *InfraService) runDeploy(ctx context.Context, engagementID, jobID, actor string) {
	_ = s.jobs.UpdateStatus(ctx, jobID, domain.JobRunning, "")

	t, err := s.infra.GetTopology(ctx, engagementID)
	if err != nil {
		s.failJob(ctx, jobID, fmt.Sprintf("get topology: %v", err))
		return
	}

	// Attempt to load cloud credentials. For the fake provider this is optional;
	// real providers require creds.
	cloudCreds := cloud.Credentials{}
	if len(t.Nodes) > 0 {
		providerType := t.Nodes[0].Spec.Cloud
		if ct, nonce, keyID, err := s.creds.GetCiphertext(ctx, engagementID, string(providerType)); err == nil {
			raw, err := s.enc.Decrypt(secrets.Envelope{Ciphertext: ct, Nonce: nonce, KeyID: keyID})
			if err == nil {
				_ = raw // In a real provider we'd parse this into Credentials.Raw.
				_ = s.creds.TouchLastUsed(ctx, engagementID, string(providerType))
				_ = s.audit.Record(ctx, audit.Event{
					EngagementID: engagementID,
					Actor:        actor,
					Action:       "credential.use",
					Target:       string(providerType),
					Detail:       "loaded for deploy",
					At:           time.Now().UTC(),
				})
			}
		}
		cloudCreds.Provider = providerType
	}

	var anyFailed bool
	for i := range t.Nodes {
		n := &t.Nodes[i]
		if n.Status != domain.NodePending {
			continue
		}

		// Transition to provisioning.
		n.Status = domain.NodeProvisioning
		_ = s.infra.UpdateNodeStatus(ctx, n.ID, domain.NodeProvisioning, domain.HealthUnknown)
		s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(n)})

		// Provision via the registered cloud provider.
		prov, err := cloud.Get(n.Spec.Cloud)
		if err != nil {
			s.log.Error("no provider for cloud", "cloud", n.Spec.Cloud, "node", n.ID)
			_ = s.infra.UpdateNodeStatus(ctx, n.ID, domain.NodeFailed, domain.HealthUnknown)
			s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(n)})
			anyFailed = true
			continue
		}

		provisioned, err := prov.ProvisionNode(ctx, cloudCreds, n.Spec)
		if err != nil {
			s.log.Error("provision node failed", "node", n.ID, "err", err)
			n.Status = domain.NodeFailed
			_ = s.infra.UpdateNodeStatus(ctx, n.ID, domain.NodeFailed, domain.HealthUnknown)
			s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(n)})
			anyFailed = true
			continue
		}

		// Write the ProviderRef immediately (before any more work) so teardown
		// can reconcile even if we crash here.
		n.ProviderRef = provisioned.ProviderRef
		n.PublicIP = provisioned.PublicIP
		n.Status = domain.NodeLive
		n.Health = domain.HealthHealthy

		// Persist updated node. We need to re-save the topology to capture ProviderRef/IP.
		_ = s.persistNodeFields(ctx, n)
		_ = s.infra.UpdateNodeStatus(ctx, n.ID, domain.NodeLive, domain.HealthHealthy)
		s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(n)})
	}

	status := domain.JobDone
	msg := ""
	if anyFailed {
		status = domain.JobFailed
		msg = "one or more nodes failed to provision"
	}
	_ = s.jobs.UpdateStatus(ctx, jobID, status, msg)

	s.hub.Publish(Event{Kind: EventJobStatus, EngagementID: engagementID, Data: map[string]any{
		"jobId":  jobID,
		"status": string(status),
	}})

	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: engagementID,
		Actor:        actor,
		Action:       "infra.deploy.complete",
		Target:       engagementID,
		Detail:       fmt.Sprintf("job=%s status=%s", jobID, status),
		At:           time.Now().UTC(),
	})
}

// persistNodeFields re-saves a single node's IP/ProviderRef/Status by updating
// the topology. This is a best-effort write; errors are logged but do not fail
// the deploy.
func (s *InfraService) persistNodeFields(ctx context.Context, n *domain.Node) error {
	t, err := s.infra.GetTopology(ctx, n.EngagementID)
	if err != nil {
		return err
	}
	for i := range t.Nodes {
		if t.Nodes[i].ID == n.ID {
			t.Nodes[i].ProviderRef = n.ProviderRef
			t.Nodes[i].PublicIP = n.PublicIP
			t.Nodes[i].Status = n.Status
			t.Nodes[i].Health = n.Health
		}
	}
	return s.infra.SaveTopology(ctx, t)
}

// Teardown drains and destroys all nodes for an engagement. It does NOT gate
// on CanDeploy — teardown must always work. Reconciles against actual cloud
// state via the provider.
func (s *InfraService) Teardown(ctx context.Context, engagementID string, actor string) (string, error) {
	// No CanDeploy gate for teardown — it must always work.
	if err := s.assertNoActiveJob(ctx, engagementID); err != nil {
		return "", err
	}

	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: engagementID,
		Actor:        actor,
		Action:       "infra.teardown",
		Target:       engagementID,
		Detail:       "teardown initiated",
		At:           time.Now().UTC(),
	})

	job := domain.Job{
		EngagementID: engagementID,
		Kind:         domain.JobTeardown,
		Status:       domain.JobPending,
	}
	jobID, err := s.jobs.Create(ctx, job)
	if err != nil {
		return "", fmt.Errorf("create teardown job: %w", err)
	}

	go s.runTeardown(context.Background(), engagementID, jobID, actor)

	return jobID, nil
}

// runTeardown executes in a goroutine: drains then destroys each node and
// reconciles against actual cloud state.
func (s *InfraService) runTeardown(ctx context.Context, engagementID, jobID, actor string) {
	_ = s.jobs.UpdateStatus(ctx, jobID, domain.JobRunning, "")

	t, err := s.infra.GetTopology(ctx, engagementID)
	if err != nil {
		s.failJob(ctx, jobID, fmt.Sprintf("get topology: %v", err))
		return
	}

	cloudCreds := cloud.Credentials{}
	if len(t.Nodes) > 0 {
		cloudCreds.Provider = t.Nodes[0].Spec.Cloud
	}

	var anyFailed bool
	for i := range t.Nodes {
		n := &t.Nodes[i]
		if n.Status == domain.NodeDestroyed {
			continue
		}

		// Drain first.
		n.Status = domain.NodeDraining
		_ = s.infra.UpdateNodeStatus(ctx, n.ID, domain.NodeDraining, domain.HealthUnknown)
		s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(n)})

		if n.ProviderRef == "" {
			// Never provisioned — mark destroyed directly.
			n.Status = domain.NodeDestroyed
			_ = s.infra.UpdateNodeStatus(ctx, n.ID, domain.NodeDestroyed, domain.HealthUnknown)
			s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(n)})
			continue
		}

		prov, err := cloud.Get(n.Spec.Cloud)
		if err != nil {
			s.log.Error("no provider for cloud on teardown", "cloud", n.Spec.Cloud)
			anyFailed = true
			continue
		}

		if err := prov.Destroy(ctx, cloudCreds, *n); err != nil {
			s.log.Error("destroy node failed", "node", n.ID, "err", err)
			_ = s.infra.UpdateNodeStatus(ctx, n.ID, domain.NodeFailed, domain.HealthUnknown)
			s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(n)})
			anyFailed = true
			continue
		}

		n.Status = domain.NodeDestroyed
		_ = s.infra.UpdateNodeStatus(ctx, n.ID, domain.NodeDestroyed, domain.HealthUnknown)
		s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(n)})
	}

	status := domain.JobDone
	msg := ""
	if anyFailed {
		status = domain.JobFailed
		msg = "one or more nodes failed to destroy"
	} else {
		// Transition engagement to completed.
		_ = s.engagements.UpdateStatus(ctx, engagementID, domain.EngagementCompleted)
	}
	_ = s.jobs.UpdateStatus(ctx, jobID, status, msg)

	s.hub.Publish(Event{Kind: EventJobStatus, EngagementID: engagementID, Data: map[string]any{
		"jobId":  jobID,
		"status": string(status),
	}})

	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: engagementID,
		Actor:        actor,
		Action:       "infra.teardown.complete",
		Target:       engagementID,
		Detail:       fmt.Sprintf("job=%s status=%s", jobID, status),
		At:           time.Now().UTC(),
	})
}

// PutCredentials encrypts and stores credentials for an engagement/provider.
// Returns ErrNotFound if the engagement does not exist.
func (s *InfraService) PutCredentials(ctx context.Context, engagementID, provider string, plaintext []byte, actor string) error {
	if _, err := s.engagements.Get(ctx, engagementID); err != nil {
		return err
	}

	env, err := s.enc.Encrypt(plaintext)
	if err != nil {
		return fmt.Errorf("encrypt credential: %w", err)
	}

	if err := s.creds.Upsert(ctx, engagementID, provider, env.Ciphertext, env.Nonce, env.KeyID); err != nil {
		return fmt.Errorf("store credential: %w", err)
	}

	// Audit: never log the plaintext or ciphertext material.
	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: engagementID,
		Actor:        actor,
		Action:       "credential.store",
		Target:       provider,
		Detail:       fmt.Sprintf("provider=%s key_id=%s", provider, env.KeyID),
		At:           time.Now().UTC(),
	})
	return nil
}

// GetCredentialMeta returns non-sensitive metadata for stored credentials.
func (s *InfraService) GetCredentialMeta(ctx context.Context, engagementID, provider string) (domain.CredentialMeta, error) {
	meta, err := s.creds.GetMeta(ctx, engagementID, provider)
	if err != nil {
		return domain.CredentialMeta{}, err
	}
	return meta, nil
}

// ResumeJobs marks any jobs left in the "running" state as failed due to
// server restart. Call this once at boot.
func (s *InfraService) ResumeJobs(ctx context.Context) {
	running, err := s.jobs.ListRunning(ctx)
	if err != nil {
		s.log.Error("resume jobs: list running", "err", err)
		return
	}
	for _, j := range running {
		_ = s.jobs.UpdateStatus(ctx, j.ID, domain.JobFailed, "interrupted by server restart")
		s.log.Warn("marked orphaned job as failed", "job_id", j.ID, "kind", string(j.Kind))
	}
}

// assertNoActiveJob returns ErrJobRunning if there is already a running
// deploy or teardown job for the engagement.
func (s *InfraService) assertNoActiveJob(ctx context.Context, engagementID string) error {
	running, err := s.jobs.ListRunning(ctx)
	if err != nil {
		return fmt.Errorf("check active jobs: %w", err)
	}
	for _, j := range running {
		if j.EngagementID == engagementID &&
			(j.Kind == domain.JobDeploy || j.Kind == domain.JobTeardown) {
			return fmt.Errorf("%w: job_id=%s", ErrJobRunning, j.ID)
		}
	}
	return nil
}

// failJob marks a job as failed and logs the reason.
func (s *InfraService) failJob(ctx context.Context, jobID, reason string) {
	_ = s.jobs.UpdateStatus(ctx, jobID, domain.JobFailed, reason)
}

// nodeStatusPayload returns a map suitable for SSE data.
func nodeStatusPayload(n *domain.Node) map[string]any {
	return map[string]any{
		"nodeId":      n.ID,
		"status":      string(n.Status),
		"health":      string(n.Health),
		"publicIp":    n.PublicIP,
		"providerRef": n.ProviderRef,
	}
}
