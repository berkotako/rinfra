package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rinfra/rinfra/internal/audit"
	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/c2/deploy"
	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration"
	"github.com/rinfra/rinfra/internal/redirector"
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
	// runnerFactory builds the SSH runner used to apply redirector config on the
	// box; nil means the default live node runner (tests inject a fake).
	runnerFactory func(host string) deploy.Runner
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

// RedirectorConfig renders the reverse-proxy configuration for a redirector
// node, resolving the upstream it fronts from the topology Edge (the node the
// redirector forwards to) and its Profile from the built-in profile catalog.
// This turns the abstract canvas (a redirector with a profile + an edge to a C2)
// into concrete, inspectable nginx config. Applying it on the box (cloud-init /
// SSH) is the live-infra step layered on top of this.
func (s *InfraService) RedirectorConfig(ctx context.Context, engagementID, nodeID string) (string, error) {
	cfg, _, err := s.redirectorPlan(ctx, engagementID, nodeID)
	return cfg, err
}

// redirectorPlan resolves and renders a redirector's config and returns it along
// with the redirector node (so callers that also need to reach the box — e.g.
// ApplyRedirector — don't re-load the topology).
func (s *InfraService) redirectorPlan(ctx context.Context, engagementID, nodeID string) (string, domain.Node, error) {
	t, err := s.infra.GetTopology(ctx, engagementID)
	if err != nil {
		return "", domain.Node{}, err
	}
	byID := make(map[string]domain.Node, len(t.Nodes))
	var node *domain.Node
	for i := range t.Nodes {
		byID[t.Nodes[i].ID] = t.Nodes[i]
		if t.Nodes[i].ID == nodeID {
			node = &t.Nodes[i]
		}
	}
	if node == nil {
		return "", domain.Node{}, fmt.Errorf("node %s: %w", nodeID, store.ErrNotFound)
	}
	if node.Spec.Type != domain.NodeRedirector {
		return "", domain.Node{}, fmt.Errorf("node %s is not a redirector", nodeID)
	}

	target, ok := redirectorTarget(t, nodeID, byID)
	if !ok {
		return "", *node, fmt.Errorf("redirector %s has no edge to a target node to front", nodeID)
	}
	if target.PublicIP == "" {
		return "", *node, fmt.Errorf("redirector target %s has no public IP yet — provision it first", target.ID)
	}

	profile, ok := redirector.LookupProfile(node.Spec.ProfileName)
	if !ok {
		profile = redirector.PlainProfile()
	}
	up := redirector.Upstream{
		Host: target.PublicIP,
		Port: upstreamPort(node.Spec.Subtype, target),
		TLS:  strings.EqualFold(node.Spec.Subtype, "https"),
	}
	cfg, err := redirector.RenderNginx(profile, up, node.Spec.Subtype, node.Canvas.FrontDomain)
	return cfg, *node, err
}

// redirectorRunner builds the SSH runner used to apply config to a redirector
// box. Overridable in tests via WithRedirectorRunner; defaults to the live SSH
// node runner.
func (s *InfraService) redirectorRunner(host string) deploy.Runner {
	if s.runnerFactory != nil {
		return s.runnerFactory(host)
	}
	return deploy.NewNodeRunner(host)
}

// WithRedirectorRunner injects the SSH runner factory used by ApplyRedirector
// (tests pass a fake; production uses the default live runner).
func (s *InfraService) WithRedirectorRunner(factory func(host string) deploy.Runner) {
	s.runnerFactory = factory
}

// ApplyRedirector renders the redirector's reverse-proxy config and applies it
// on the box: it uploads the config + an idempotent install script over SSH,
// installs/reloads nginx, and (best-effort) points the front domain at the
// redirector via the cloud provider's DNS. Audited as redirector.configure.
//
// The redirector node and its upstream must both be provisioned (have public
// IPs). DNS failures are logged/audited but do not fail the apply.
func (s *InfraService) ApplyRedirector(ctx context.Context, engagementID, nodeID, actor string) error {
	// Authorization gate — this is a provisioning path (installs software, mutates
	// DNS), so it must be refused outside an authorized, in-window engagement,
	// even if stale live PublicIPs linger in the topology.
	eng, err := s.engagements.Get(ctx, engagementID)
	if err != nil {
		return err
	}
	if err := eng.CanDeploy(time.Now()); err != nil {
		return fmt.Errorf("redirector apply refused: %w", err)
	}

	cfg, node, err := s.redirectorPlan(ctx, engagementID, nodeID)
	if err != nil {
		return err
	}
	if node.PublicIP == "" {
		return fmt.Errorf("redirector %s has no public IP yet — provision it before applying config", nodeID)
	}

	runner := s.redirectorRunner(node.PublicIP)
	if err := runner.Upload(ctx, redirector.StagePath, cfg); err != nil {
		return fmt.Errorf("upload redirector config: %w", err)
	}
	script := redirector.InstallScript(redirector.StagePath, redirector.InstallPath)
	if err := runner.Upload(ctx, redirectorInstallScriptPath, script); err != nil {
		return fmt.Errorf("upload redirector install script: %w", err)
	}
	if _, err := runner.Run(ctx, "sudo bash "+redirectorInstallScriptPath); err != nil {
		return fmt.Errorf("apply redirector config: %w", err)
	}

	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: engagementID,
		Actor:        actor,
		Action:       "redirector.configure",
		Target:       nodeID,
		Detail:       fmt.Sprintf("nginx reverse-proxy applied (profile=%s, subtype=%s)", node.Spec.ProfileName, node.Spec.Subtype),
		At:           time.Now().UTC(),
	})

	// Best-effort: point the categorized front domain at the redirector.
	if node.Canvas.FrontDomain != "" {
		if err := s.pointFrontDomain(ctx, engagementID, node, actor); err != nil {
			s.log.Warn("redirector: front-domain DNS not set", "node", nodeID, "domain", node.Canvas.FrontDomain, "err", err)
		}
	}
	return nil
}

// redirectorInstallScriptPath is where the install script is staged on the box.
const redirectorInstallScriptPath = "/tmp/rinfra-redirector-install.sh"

// pointFrontDomain creates/updates an A record for the redirector's front domain
// pointing at the redirector's public IP, via the node's cloud provider DNS.
func (s *InfraService) pointFrontDomain(ctx context.Context, engagementID string, node domain.Node, actor string) error {
	prov, err := cloud.Get(node.Spec.Cloud)
	if err != nil {
		return err
	}
	creds, err := s.loadCreds(ctx, engagementID, node.Spec.Cloud)
	if err != nil {
		return err
	}
	rec := dnsRecordFor(node.Spec.Cloud, node.Canvas.FrontDomain, node.PublicIP)
	if err := prov.ManageDNS(ctx, creds, rec); err != nil {
		return err
	}
	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: engagementID,
		Actor:        actor,
		Action:       "redirector.dns",
		Target:       node.ID,
		Detail:       fmt.Sprintf("%s A %s (zone %s)", rec.Name, rec.Value, rec.Zone),
		At:           time.Now().UTC(),
	})
	return nil
}

// loadCreds fetches and decrypts the engagement's credentials for a provider.
func (s *InfraService) loadCreds(ctx context.Context, engagementID string, provider domain.CloudProviderType) (cloud.Credentials, error) {
	ct, nonce, keyID, err := s.creds.GetCiphertext(ctx, engagementID, string(provider))
	if err != nil {
		return cloud.Credentials{}, err
	}
	creds, err := DecryptCredentials(s.enc, provider, ct, nonce, keyID)
	if err != nil {
		return cloud.Credentials{}, err
	}
	_ = s.creds.TouchLastUsed(ctx, engagementID, string(provider))
	return creds, nil
}

// registrableDomain returns the apex (last two labels) of an FQDN, the
// conventional DNS zone name. "cdn.front.victim.test" → "victim.test".
func registrableDomain(fqdn string) string {
	fqdn = strings.TrimSuffix(strings.TrimSpace(fqdn), ".")
	parts := strings.Split(fqdn, ".")
	if len(parts) <= 2 {
		return fqdn
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

// dnsRecordFor builds the A record for a front domain in the shape each
// provider's ManageDNS expects — the record identifiers genuinely diverge:
//
//   - DigitalOcean / Azure: Zone is the apex domain, Name is the record relative
//     to the zone ("cdn"; "@" for the apex itself).
//   - AWS Route53: Zone is the apex (hosted zone resolved by name), Name is the
//     full FQDN.
//   - GCP Cloud DNS: Zone is the managed-zone name (defaulted to the apex, the
//     common convention), Name is the FQDN with a trailing dot.
//
// Zone is a best-effort default; operators whose managed zone / hosted zone is
// not named after the apex can still drive ManageDNS directly.
func dnsRecordFor(provider domain.CloudProviderType, frontDomain, ip string) domain.Record {
	fqdn := strings.TrimSuffix(strings.TrimSpace(frontDomain), ".")
	apex := registrableDomain(fqdn)
	rec := domain.Record{Zone: apex, Type: "A", Value: ip, TTL: 300}
	switch provider {
	case domain.CloudGCP:
		rec.Name = fqdn + "."
	case domain.CloudAWS:
		rec.Name = fqdn
	default: // DigitalOcean, Azure: relative record name
		if fqdn == apex {
			rec.Name = "@"
		} else {
			rec.Name = strings.TrimSuffix(fqdn, "."+apex)
		}
	}
	return rec
}

// redirectorTarget returns the node a redirector forwards to: the destination of
// its first outgoing edge, preferring a C2 server, then a payload host.
func redirectorTarget(t domain.Topology, redirectorID string, byID map[string]domain.Node) (domain.Node, bool) {
	var fallback *domain.Node
	for _, e := range t.Edges {
		if e.FromNodeID != redirectorID {
			continue
		}
		dst, ok := byID[e.ToNodeID]
		if !ok {
			continue
		}
		if dst.Spec.Type == domain.NodeC2Server {
			return dst, true
		}
		if fallback == nil {
			d := dst
			fallback = &d
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return domain.Node{}, false
}

// upstreamPort picks the port the redirector proxies to: a port embedded in the
// target's listener string ("host:port" or ":port") if present, else the
// conventional default for the redirector subtype.
func upstreamPort(subtype string, target domain.Node) int {
	if p, ok := parseListenerPort(target.Canvas.Listener); ok {
		return p
	}
	if strings.EqualFold(subtype, "https") {
		return 443
	}
	return 80
}

// parseListenerPort extracts the TCP port from a listener string such as
// "0.0.0.0:443", ":8443", "[::1]:443", or "https://host:443/beacon". Returns
// ok=false when no valid port (1–65535) is present. Bracketed IPv6 and a
// trailing path are handled so an IPv6 listener isn't mis-split on an inner ":".
func parseListenerPort(l string) (int, bool) {
	l = strings.TrimSpace(l)
	if l == "" {
		return 0, false
	}
	if i := strings.Index(l, "://"); i >= 0 { // drop a scheme ("https://…")
		l = l[i+3:]
	}
	if i := strings.IndexByte(l, '/'); i >= 0 { // drop any trailing path
		l = l[:i]
	}
	if j := strings.LastIndex(l, "]:"); j >= 0 { // bracketed IPv6 → port after "]:"
		l = l[j+1:]
	}
	i := strings.LastIndex(l, ":")
	if i < 0 || i >= len(l)-1 {
		return 0, false
	}
	p, err := strconv.Atoi(l[i+1:])
	if err != nil || p <= 0 || p > 65535 {
		return 0, false
	}
	return p, true
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
	seenID := make(map[string]bool, len(t.Nodes))
	for _, n := range t.Nodes {
		if n.ID != "" && seenID[n.ID] {
			// Duplicate IDs collide in the IaC program (same Pulumi URN / Terraform
			// resource key), silently dropping one node and pointing both results at
			// one box. Reject before provisioning.
			problems = append(problems, fmt.Sprintf("duplicate node id %q", n.ID))
		}
		seenID[n.ID] = true
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
			if !redirector.ValidFrontDomain(n.Canvas.FrontDomain) {
				problems = append(problems, fmt.Sprintf("redirector %q: invalid front domain %q", label, n.Canvas.FrontDomain))
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
//
// rollbackEnabled reports whether partial-failure rollback is active. It is on
// by default; set RINFRA_DEPLOY_ROLLBACK=off to keep partially-deployed infra
// for fix-forward instead (the operator then tears down manually).
func rollbackEnabled() bool {
	return os.Getenv("RINFRA_DEPLOY_ROLLBACK") != "off"
}

// desiredEngineNodes returns the COMPLETE set of engine-routed nodes that should
// exist for the surviving providers: the pending nodes being deployed now PLUS
// the engagement's already-standing engine nodes. Pulumi treats an inline
// program as the whole desired state of its stack, so a re-deploy that declares
// only the new nodes would destroy the already-live ones — this set prevents
// that. Only providers present in credsMap (creds available) are included, and
// destroyed/failed nodes are excluded (Pulumi won't recreate them; their partial
// resources are reclaimed by the tag sweep).
func desiredEngineNodes(nodes []domain.Node, activeProv Provisioner, credsMap map[domain.CloudProviderType]cloud.Credentials, pendingIDs map[string]bool) []domain.Node {
	if activeProv == nil {
		return nil
	}
	var out []domain.Node
	for _, n := range nodes {
		if _, ok := credsMap[n.Spec.Cloud]; !ok {
			continue
		}
		prov, err := cloud.Get(n.Spec.Cloud)
		if err != nil {
			continue
		}
		if _, ok := prov.(orchestration.ProgramBuilder); !ok {
			continue
		}
		if pendingIDs[n.ID] {
			out = append(out, n)
			continue
		}
		switch n.Status {
		case domain.NodeLive, domain.NodeProvisioning, domain.NodeDraining:
			out = append(out, n)
		}
	}
	return out
}

// nodeIngressRules derives the default inbound rules for a node from its role.
// Provisioned infrastructure must be reachable — a security group / firewall with
// no ingress blocks everything, including the SSH the redirector-apply step
// needs. These defaults open only what the role requires; operators can tighten
// them afterward via ConfigureIngress.
func nodeIngressRules(node domain.Node) []domain.Rule {
	// SSH for management + redirector-apply on every node.
	rules := []domain.Rule{{Protocol: "tcp", Port: 22, SourceCIDR: "0.0.0.0/0", Allow: true}}
	switch node.Spec.Type {
	case domain.NodeRedirector, domain.NodePayloadHost:
		rules = append(rules,
			domain.Rule{Protocol: "tcp", Port: 80, SourceCIDR: "0.0.0.0/0", Allow: true},
			domain.Rule{Protocol: "tcp", Port: 443, SourceCIDR: "0.0.0.0/0", Allow: true},
		)
	case domain.NodeC2Server:
		port := 443 // common C2 HTTPS listener default
		if p, ok := parseListenerPort(node.Canvas.Listener); ok {
			port = p
		}
		rules = append(rules, domain.Rule{Protocol: "tcp", Port: port, SourceCIDR: "0.0.0.0/0", Allow: true})
	}
	return rules
}

// configureNodeIngress applies the node's role-derived default ingress on its
// provider. Best-effort: a failure leaves the node live but marks it degraded
// (it may be unreachable) and is audited, rather than failing the whole deploy —
// closing the gap where nodes came up with no inbound rules at all.
func (s *InfraService) configureNodeIngress(ctx context.Context, engagementID string, node *domain.Node, creds cloud.Credentials) {
	prov, err := cloud.Get(node.Spec.Cloud)
	if err != nil {
		return
	}
	rules := nodeIngressRules(*node)
	if err := prov.ConfigureIngress(ctx, creds, *node, rules); err != nil {
		s.log.Warn("configure ingress failed; node may be unreachable", "node", node.ID, "cloud", node.Spec.Cloud, "err", err)
		node.Health = domain.HealthDegraded
		_ = s.infra.UpdateNodeStatus(ctx, node.ID, node.Status, domain.HealthDegraded)
		s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(node)})
		_ = s.audit.Record(ctx, audit.Event{
			EngagementID: engagementID,
			Actor:        "system",
			Action:       "infra.ingress.failed",
			Target:       node.ID,
			Detail:       fmt.Sprintf("configure ingress failed (cloud=%s): %v", node.Spec.Cloud, err),
			At:           time.Now().UTC(),
		})
		return
	}
	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: engagementID,
		Actor:        "system",
		Action:       "infra.ingress",
		Target:       node.ID,
		Detail:       fmt.Sprintf("ingress configured (cloud=%s, %d rule(s))", node.Spec.Cloud, len(rules)),
		At:           time.Now().UTC(),
	})
}

// rollbackProvisioned destroys the nodes a failed deploy DID bring live, in
// reverse order, marking each destroyed and auditing infra.rollback. It is
// best-effort (a Destroy error is logged, the node is still marked destroyed)
// and idempotent — Destroy treats an already-gone resource as success, and the
// tag-based teardown sweep is the backstop. Only nodes provisioned in this
// deploy are passed in, so pre-existing live infra is never touched. Returns the
// count rolled back.
func (s *InfraService) rollbackProvisioned(ctx context.Context, engagementID, actor string, nodes []*domain.Node) int {
	n := 0
	for i := len(nodes) - 1; i >= 0; i-- {
		node := nodes[i]
		prov, err := cloud.Get(node.Spec.Cloud)
		if err != nil {
			// Can't reach the provider — leave the node reapable so a later
			// teardown/sweep removes it rather than orphaning it.
			s.log.Warn("rollback: no provider; left reapable", "node", node.ID, "cloud", node.Spec.Cloud, "err", err)
			s.markRolledBack(ctx, engagementID, actor, node, domain.NodeFailed, "no provider; left for teardown")
			continue
		}

		// Skip providers whose Destroy is engagement-scoped (e.g. Azure deletes
		// the whole resource group): destroying this node would also remove
		// pre-existing siblings from earlier deploys. Leave it reapable for
		// engagement-level teardown (which uses the tag sweep) instead.
		if _, perNode := prov.(cloud.PerNodeDestroyer); !perNode {
			s.log.Warn("rollback: provider Destroy is engagement-scoped; deferring to teardown", "node", node.ID, "cloud", node.Spec.Cloud)
			s.markRolledBack(ctx, engagementID, actor, node, domain.NodeFailed, "engagement-scoped destroy; deferred to teardown")
			continue
		}

		// Real providers need creds; the fake/direct path uses empty creds.
		creds := cloud.Credentials{Provider: node.Spec.Cloud}
		if c, err := s.loadCreds(ctx, engagementID, node.Spec.Cloud); err == nil {
			creds = c
		}
		if err := prov.Destroy(ctx, creds, *node); err != nil {
			// Destroy failed — keep the node failed/reapable so teardown/the reaper
			// retry it. Marking it destroyed here would orphan the live resource
			// (both runTeardown and the reaper skip destroyed nodes).
			s.log.Warn("rollback: destroy failed; left reapable for teardown sweep", "node", node.ID, "err", err)
			s.markRolledBack(ctx, engagementID, actor, node, domain.NodeFailed, fmt.Sprintf("destroy failed (%v); left for teardown", err))
			continue
		}
		s.markRolledBack(ctx, engagementID, actor, node, domain.NodeDestroyed, "rolled back after partial deploy failure")
		n++
	}
	return n
}

// markRolledBack records a rollback outcome for a node: sets its status (destroyed
// on a confirmed cleanup, failed/reapable otherwise), publishes the change, and
// audits infra.rollback.
func (s *InfraService) markRolledBack(ctx context.Context, engagementID, actor string, node *domain.Node, status domain.NodeStatus, detail string) {
	node.Status = status
	node.Health = domain.HealthUnknown
	_ = s.infra.UpdateNodeStatus(ctx, node.ID, status, domain.HealthUnknown)
	s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(node)})
	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: engagementID,
		Actor:        actor,
		Action:       "infra.rollback",
		Target:       node.ID,
		Detail:       fmt.Sprintf("%s (cloud=%s, status=%s)", detail, node.Spec.Cloud, status),
		At:           time.Now().UTC(),
	})
}

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
	// provisioned tracks ONLY the nodes this deploy brought live (pre-existing
	// live nodes are never pending, so are never partitioned in here). On a
	// partial failure these are rolled back so a failed deploy leaves no live
	// orphans — making deploy closer to transactional.
	var liveNodes []*domain.Node

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
			// pendingIDs are the nodes THIS deploy is bringing up (to transition,
			// report, and roll back on failure). Already-live nodes are handled
			// separately below — never rolled back, only refreshed.
			pendingIDs := make(map[string]bool, len(engineNodes))
			for _, n := range engineNodes {
				pendingIDs[n.ID] = true
			}

			// programNodes is the COMPLETE desired state for the engine stacks: the
			// pending nodes PLUS the engagement's already-standing engine nodes on the
			// surviving providers. Pulumi treats the inline program as the whole
			// stack, so omitting already-live nodes would make this Up destroy them —
			// breaking incremental re-deploy. Only providers we hold creds for are
			// included (credsMap), so a node whose provider lost creds is never sent.
			programNodes := desiredEngineNodes(t.Nodes, activeProv, credsMap, pendingIDs)

			// Transition the pending nodes to provisioning (already-live nodes keep
			// their status).
			for _, n := range engineNodes {
				np := nodeByID[n.ID]
				np.Status = domain.NodeProvisioning
				_ = s.infra.UpdateNodeStatus(ctx, np.ID, domain.NodeProvisioning, domain.HealthUnknown)
				s.hub.Publish(Event{Kind: EventNodeStatus, EngagementID: engagementID, Data: nodeStatusPayload(np)})
			}

			results, err := activeProv.Deploy(ctx, engagementID, programNodes, credsMap)
			if err != nil {
				// Whole-engine error — mark only the PENDING nodes failed; do not
				// downgrade already-live siblings that this deploy did not touch.
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
					if !pendingIDs[r.NodeID] {
						// Already-live node re-declared for desired state. Refresh its
						// ref/IP if the provider reported them; never fail or roll it back.
						if r.Err == nil && (r.ProviderRef != "" || r.PublicIP != "") {
							np.ProviderRef = r.ProviderRef
							np.PublicIP = r.PublicIP
							_ = s.persistNodeFields(ctx, np)
						}
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
						liveNodes = append(liveNodes, np)
						s.configureNodeIngress(ctx, engagementID, np, credsMap[np.Spec.Cloud])
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
		liveNodes = append(liveNodes, n)
		s.configureNodeIngress(ctx, engagementID, n, directCreds)
	}

	// Partial-failure rollback: if any node failed, tear down the nodes this
	// deploy DID bring live so a half-finished deploy leaves no live orphans.
	rolledBack := 0
	if anyFailed && len(liveNodes) > 0 && rollbackEnabled() {
		rolledBack = s.rollbackProvisioned(ctx, engagementID, actor, liveNodes)
	}

	status := domain.JobDone
	msg := ""
	if anyFailed {
		status = domain.JobFailed
		msg = "one or more nodes failed to provision"
		if rolledBack > 0 {
			msg += fmt.Sprintf("; rolled back %d provisioned node(s)", rolledBack)
		}
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

		engineErr := activeProv.Teardown(ctx, engagementID, engineNodes, credsMap)
		if engineErr != nil {
			s.log.Error("engine.Teardown error", "engagement", engagementID, "err", engineErr)
			anyFailed = true
		}

		// On success, mark the engine nodes destroyed. On failure, leave them
		// FAILED (reapable) — NOT destroyed — so the reaper and any later teardown
		// retry them. Marking a node destroyed after a failed stack-destroy/sweep
		// would hide still-live cloud resources from every future cleanup (the
		// reaper and runTeardown both skip destroyed nodes), which is exactly the
		// orphan the guaranteed-teardown promise exists to prevent.
		endStatus := domain.NodeDestroyed
		if engineErr != nil {
			endStatus = domain.NodeFailed
		}
		for i := range t.Nodes {
			n := &t.Nodes[i]
			for _, en := range engineNodes {
				if n.ID == en.ID {
					n.Status = endStatus
					_ = s.infra.UpdateNodeStatus(ctx, n.ID, endStatus, domain.HealthUnknown)
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
