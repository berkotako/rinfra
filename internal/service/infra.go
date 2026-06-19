package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rinfra/rinfra/internal/audit"
	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration"
	"github.com/rinfra/rinfra/internal/secrets"
	"github.com/rinfra/rinfra/internal/store"
)

// ErrJobRunning is returned when a deploy or teardown job is already in
// progress for an engagement.
var ErrJobRunning = errors.New("a deploy or teardown job is already running for this engagement")

// ErrInvalidTopology is returned by Deploy when the topology fails validation.
var ErrInvalidTopology = errors.New("topology validation failed")

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
	// provisioners holds the registered IaC backends (e.g. "pulumi",
	// "terraform"), each satisfying Provisioner. Providers that implement
	// orchestration.ProgramBuilder are routed through the selected backend
	// instead of the per-node ProvisionNode path; the fake provider never is.
	provisioners   map[string]Provisioner
	defaultBackend string
	settings       store.SettingStore // optional: persists the selected backend
}

// IaC backend keys.
const (
	BackendPulumi    = "pulumi"
	BackendTerraform = "terraform"
)

// iacSettingKey is the server_settings key holding the selected IaC backend.
const iacSettingKey = "iac_backend"

// Provisioner abstracts the IaC backend. Both orchestration.Engine (Pulumi) and
// orchestration/terraform.Engine satisfy it, so the backend is swappable.
type Provisioner interface {
	Deploy(ctx context.Context, engagementID string, nodes []domain.Node, creds map[domain.CloudProviderType]cloud.Credentials) ([]orchestration.NodeResult, error)
	Teardown(ctx context.Context, engagementID string, nodes []domain.Node, creds map[domain.CloudProviderType]cloud.Credentials) error
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

// WithEngine registers the Pulumi orchestration engine as the "pulumi" backend
// (and makes it the default if none is set). Kept for existing call sites/tests.
func (s *InfraService) WithEngine(e *orchestration.Engine) {
	s.RegisterProvisioner(BackendPulumi, e)
}

// RegisterProvisioner registers an IaC backend under a key. The first one
// registered becomes the default backend until WithSettings overrides it.
func (s *InfraService) RegisterProvisioner(backend string, p Provisioner) {
	if s.provisioners == nil {
		s.provisioners = make(map[string]Provisioner)
	}
	s.provisioners[backend] = p
	if s.defaultBackend == "" {
		s.defaultBackend = backend
	}
}

// WithSettings attaches a SettingStore (for persisting the selected backend) and
// the default backend used when nothing is stored.
func (s *InfraService) WithSettings(st store.SettingStore, defaultBackend string) {
	s.settings = st
	if defaultBackend != "" {
		s.defaultBackend = defaultBackend
	}
}

// AvailableBackends returns the registered backend keys (sorted: pulumi first).
func (s *InfraService) AvailableBackends() []string {
	out := make([]string, 0, len(s.provisioners))
	for _, b := range []string{BackendPulumi, BackendTerraform} {
		if _, ok := s.provisioners[b]; ok {
			out = append(out, b)
		}
	}
	for b := range s.provisioners {
		if b != BackendPulumi && b != BackendTerraform {
			out = append(out, b)
		}
	}
	return out
}

// IaCBackend returns the currently selected backend key: the persisted setting
// if present and registered, otherwise the default.
func (s *InfraService) IaCBackend(ctx context.Context) string {
	if s.settings != nil {
		if v, ok, err := s.settings.Get(ctx, iacSettingKey); err == nil && ok {
			if _, registered := s.provisioners[v]; registered {
				return v
			}
		}
	}
	return s.defaultBackend
}

// SetIaCBackend persists the selected IaC backend. The backend must be
// registered. Audited as a privileged configuration change.
func (s *InfraService) SetIaCBackend(ctx context.Context, actor, backend string) error {
	if _, ok := s.provisioners[backend]; !ok {
		return fmt.Errorf("%w: unknown or unavailable IaC backend %q", ErrInvalidTopology, backend)
	}
	if s.settings == nil {
		return fmt.Errorf("iac backend selection requires a settings store (database)")
	}
	if err := s.settings.Set(ctx, iacSettingKey, backend); err != nil {
		return fmt.Errorf("persist iac backend: %w", err)
	}
	_ = s.audit.Record(ctx, audit.Event{
		Actor:  actor,
		Action: "config.iac_backend",
		Target: backend,
		At:     time.Now().UTC(),
	})
	return nil
}

// provisioner resolves the currently selected backend, or nil if none.
func (s *InfraService) provisioner(ctx context.Context) Provisioner {
	if len(s.provisioners) == 0 {
		return nil
	}
	return s.provisioners[s.IaCBackend(ctx)]
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
	return topologyProblems(t, eng), nil
}

// topologyProblems returns every validation problem for a topology; an empty
// result means it is deployable. It checks topology shape (nodes present, at
// least one c2_server + redirector, each c2_server fronted by a redirector),
// per-node validity (registered cloud provider, region/size present, a
// registered C2 framework for c2_servers, a profile for redirectors), and the
// engagement authorization gate.
func topologyProblems(t domain.Topology, eng domain.Engagement) []string {
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

	// Per-node validity.
	for _, n := range t.Nodes {
		label := n.Canvas.Name
		if label == "" {
			label = n.ID
		}
		if _, err := cloud.Get(n.Spec.Cloud); err != nil {
			problems = append(problems, fmt.Sprintf("node %q: unknown cloud provider %q", label, n.Spec.Cloud))
		}
		if strings.TrimSpace(n.Spec.Region) == "" {
			problems = append(problems, fmt.Sprintf("node %q: missing region", label))
		}
		if strings.TrimSpace(n.Spec.Size) == "" {
			problems = append(problems, fmt.Sprintf("node %q: missing size", label))
		}
		switch n.Spec.Type {
		case domain.NodeC2Server:
			if strings.TrimSpace(n.Spec.C2Framework) == "" {
				problems = append(problems, fmt.Sprintf("c2_server %q: missing C2 framework", label))
			} else if _, err := c2.Get(n.Spec.C2Framework); err != nil {
				problems = append(problems, fmt.Sprintf("c2_server %q: unknown C2 framework %q", label, n.Spec.C2Framework))
			}
			if !inboundFromRedirector[n.ID] {
				problems = append(problems, fmt.Sprintf("c2_server %q has no inbound edge from a redirector", label))
			}
		case domain.NodeRedirector:
			if strings.TrimSpace(n.Spec.ProfileName) == "" {
				problems = append(problems, fmt.Sprintf("redirector %q: missing profile", label))
			}
		}
	}

	// Surface authorization gate errors.
	if err := eng.CanDeploy(time.Now()); err != nil {
		problems = append(problems, fmt.Sprintf("authorization check: %v", err))
	}

	return problems
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

	// Mandatory topology validation — never provision a malformed topology even
	// if the client skipped the validate endpoint.
	t, err := s.infra.GetTopology(ctx, engagementID)
	if err != nil {
		return "", err
	}
	if problems := topologyProblems(t, eng); len(problems) > 0 {
		return "", fmt.Errorf("%w: %s", ErrInvalidTopology, strings.Join(problems, "; "))
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
//
// Routing logic:
//   - Nodes whose cloud provider implements orchestration.ProgramBuilder AND
//     the engine is wired → grouped and sent through engine.Deploy (real cloud).
//   - All other nodes (fake provider or engine == nil) → per-node ProvisionNode
//     path (dev/test path).
func (s *InfraService) runDeploy(ctx context.Context, engagementID, jobID, actor string) {
	_ = s.jobs.UpdateStatus(ctx, jobID, domain.JobRunning, "")

	t, err := s.infra.GetTopology(ctx, engagementID)
	if err != nil {
		s.failJob(ctx, jobID, fmt.Sprintf("get topology: %v", err))
		return
	}

	// Partition pending nodes into engine-routed vs per-node paths.
	var engineNodes []domain.Node  // for real cloud via the selected IaC backend
	var directNodes []*domain.Node // for fake/direct ProvisionNode path

	nodeByID := make(map[string]*domain.Node, len(t.Nodes))
	for i := range t.Nodes {
		nodeByID[t.Nodes[i].ID] = &t.Nodes[i]
	}

	// Resolve the active IaC backend (Pulumi/Terraform) for this deploy.
	activeProv := s.provisioner(ctx)

	for i := range t.Nodes {
		n := &t.Nodes[i]
		if n.Status != domain.NodePending {
			continue
		}
		if activeProv != nil {
			if prov, err := cloud.Get(n.Spec.Cloud); err == nil {
				if _, ok := prov.(orchestration.ProgramBuilder); ok {
					engineNodes = append(engineNodes, *n)
					continue
				}
			}
		}
		directNodes = append(directNodes, n)
	}

	anyFailed := false

	// --- Engine path (real cloud providers) ---
	if len(engineNodes) > 0 {
		// Build the per-provider creds map. Group unique provider types.
		providerTypes := make(map[domain.CloudProviderType]struct{})
		for _, n := range engineNodes {
			providerTypes[n.Spec.Cloud] = struct{}{}
		}

		credsMap := make(map[domain.CloudProviderType]cloud.Credentials)
		var missingProviders []domain.CloudProviderType
		for pt := range providerTypes {
			ct, nonce, keyID, err := s.creds.GetCiphertext(ctx, engagementID, string(pt))
			if err != nil {
				missingProviders = append(missingProviders, pt)
				continue
			}
			creds, err := DecryptCredentials(s.enc, pt, ct, nonce, keyID)
			if err != nil {
				s.log.Error("decrypt credentials", "provider", pt, "err", err)
				missingProviders = append(missingProviders, pt)
				continue
			}
			_ = s.creds.TouchLastUsed(ctx, engagementID, string(pt))
			_ = s.audit.Record(ctx, audit.Event{
				EngagementID: engagementID,
				Actor:        actor,
				Action:       "credential.use",
				Target:       string(pt),
				Detail:       "loaded for deploy",
				At:           time.Now().UTC(),
			})
			credsMap[pt] = creds
		}

		// Mark nodes for providers with missing creds as failed immediately.
		if len(missingProviders) > 0 {
			missing := make(map[domain.CloudProviderType]bool, len(missingProviders))
			for _, pt := range missingProviders {
				missing[pt] = true
			}
			var remaining []domain.Node
			for _, n := range engineNodes {
				if missing[n.Spec.Cloud] {
					np := nodeByID[n.ID]
					np.Status = domain.NodeFailed
					_ = s.infra.UpdateNodeStatus(ctx, np.ID, domain.NodeFailed, domain.HealthUnknown)
					s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(np)})
					_ = s.audit.Record(ctx, audit.Event{
						EngagementID: engagementID,
						Actor:        actor,
						Action:       "credential.missing",
						Target:       string(np.Spec.Cloud),
						Detail:       fmt.Sprintf("node=%s %v", np.ID, ErrNoCloudCredentials),
						At:           time.Now().UTC(),
					})
					anyFailed = true
				} else {
					remaining = append(remaining, n)
				}
			}
			engineNodes = remaining
		}

		if len(engineNodes) > 0 {
			// Transition all engine nodes to provisioning.
			for _, n := range engineNodes {
				np := nodeByID[n.ID]
				np.Status = domain.NodeProvisioning
				_ = s.infra.UpdateNodeStatus(ctx, np.ID, domain.NodeProvisioning, domain.HealthUnknown)
				s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(np)})
			}

			results, err := activeProv.Deploy(ctx, engagementID, engineNodes, credsMap)
			if err != nil {
				// Whole-engine error — mark all remaining engine nodes failed.
				for _, n := range engineNodes {
					np := nodeByID[n.ID]
					np.Status = domain.NodeFailed
					_ = s.infra.UpdateNodeStatus(ctx, np.ID, domain.NodeFailed, domain.HealthUnknown)
					s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(np)})
				}
				anyFailed = true
				s.log.Error("engine.Deploy failed", "engagement", engagementID, "err", err)
			} else {
				for _, r := range results {
					np, ok := nodeByID[r.NodeID]
					if !ok {
						continue
					}
					if r.Err != nil {
						s.log.Error("engine deploy node failed", "node", r.NodeID, "err", r.Err)
						np.Status = domain.NodeFailed
						_ = s.infra.UpdateNodeStatus(ctx, np.ID, domain.NodeFailed, domain.HealthUnknown)
						s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(np)})
						anyFailed = true
					} else {
						// Write ProviderRef before status=live (crash-safety invariant).
						np.ProviderRef = r.ProviderRef
						np.PublicIP = r.PublicIP
						np.Status = domain.NodeLive
						np.Health = domain.HealthHealthy
						_ = s.persistNodeFields(ctx, np)
						_ = s.infra.UpdateNodeStatus(ctx, np.ID, domain.NodeLive, domain.HealthHealthy)
						s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(np)})
					}
				}
			}
		}
	}

	// --- Direct per-node path (fake provider or no engine) ---
	for _, n := range directNodes {
		// Transition to provisioning.
		n.Status = domain.NodeProvisioning
		_ = s.infra.UpdateNodeStatus(ctx, n.ID, domain.NodeProvisioning, domain.HealthUnknown)
		s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(n)})

		// Provision via the registered cloud provider.
		prov, err := cloud.Get(n.Spec.Cloud)
		if err != nil {
			s.log.Error("no provider for cloud", "cloud", n.Spec.Cloud, "node", n.ID)
			n.Status = domain.NodeFailed
			_ = s.infra.UpdateNodeStatus(ctx, n.ID, domain.NodeFailed, domain.HealthUnknown)
			s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(n)})
			anyFailed = true
			continue
		}

		// For direct-path nodes, use empty creds (fake provider doesn't need them).
		directCreds := cloud.Credentials{Provider: n.Spec.Cloud}

		provisioned, err := prov.ProvisionNode(ctx, directCreds, n.Spec)
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
//
// Routing logic mirrors runDeploy:
//   - Nodes whose cloud provider implements orchestration.ProgramBuilder AND
//     engine is wired → engine.Teardown (stack destroy + sweep).
//   - All other nodes → per-node Destroy (fake provider or no engine).
//
// Teardown is ungated (no CanDeploy check) and best-effort.
func (s *InfraService) runTeardown(ctx context.Context, engagementID, jobID, actor string) {
	_ = s.jobs.UpdateStatus(ctx, jobID, domain.JobRunning, "")

	t, err := s.infra.GetTopology(ctx, engagementID)
	if err != nil {
		s.failJob(ctx, jobID, fmt.Sprintf("get topology: %v", err))
		return
	}

	// Partition non-destroyed nodes by routing path.
	var engineNodes []domain.Node  // real cloud via the selected IaC backend
	var directNodes []*domain.Node // fake/direct Destroy path

	// Resolve the active IaC backend (Pulumi/Terraform) for this teardown.
	activeProv := s.provisioner(ctx)

	for i := range t.Nodes {
		n := &t.Nodes[i]
		if n.Status == domain.NodeDestroyed {
			continue
		}
		if activeProv != nil {
			if prov, err := cloud.Get(n.Spec.Cloud); err == nil {
				if _, ok := prov.(orchestration.ProgramBuilder); ok {
					engineNodes = append(engineNodes, *n)
					continue
				}
			}
		}
		directNodes = append(directNodes, n)
	}

	anyFailed := false

	// --- Engine path (real cloud providers) ---
	if len(engineNodes) > 0 {
		// Drain all engine nodes first.
		for i := range t.Nodes {
			n := &t.Nodes[i]
			for _, en := range engineNodes {
				if n.ID == en.ID {
					n.Status = domain.NodeDraining
					_ = s.infra.UpdateNodeStatus(ctx, n.ID, domain.NodeDraining, domain.HealthUnknown)
					s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(n)})
					break
				}
			}
		}

		// Build creds map for engine teardown (best-effort — missing creds means
		// sweep can't run but we still try the stack destroy).
		providerTypes := make(map[domain.CloudProviderType]struct{})
		for _, n := range engineNodes {
			providerTypes[n.Spec.Cloud] = struct{}{}
		}
		credsMap := make(map[domain.CloudProviderType]cloud.Credentials)
		for pt := range providerTypes {
			ct, nonce, keyID, cerr := s.creds.GetCiphertext(ctx, engagementID, string(pt))
			if cerr != nil {
				s.log.Warn("teardown: no stored creds for provider (sweep will be skipped)", "provider", pt)
				continue
			}
			creds, cerr := DecryptCredentials(s.enc, pt, ct, nonce, keyID)
			if cerr != nil {
				s.log.Warn("teardown: could not decrypt creds for provider", "provider", pt, "err", cerr)
				continue
			}
			_ = s.creds.TouchLastUsed(ctx, engagementID, string(pt))
			credsMap[pt] = creds
		}

		if engineErr := activeProv.Teardown(ctx, engagementID, engineNodes, credsMap); engineErr != nil {
			s.log.Error("engine.Teardown error", "engagement", engagementID, "err", engineErr)
			// Best-effort: continue to mark nodes destroyed anyway.
			anyFailed = true
		}

		// Mark engine nodes destroyed.
		for i := range t.Nodes {
			n := &t.Nodes[i]
			for _, en := range engineNodes {
				if n.ID == en.ID {
					n.Status = domain.NodeDestroyed
					_ = s.infra.UpdateNodeStatus(ctx, n.ID, domain.NodeDestroyed, domain.HealthUnknown)
					s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(n)})
					break
				}
			}
		}
	}

	// --- Direct per-node path (fake provider or no engine) ---
	for _, n := range directNodes {
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

		directCreds := cloud.Credentials{Provider: n.Spec.Cloud}

		if err := prov.Destroy(ctx, directCreds, *n); err != nil {
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
