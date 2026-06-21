package service

import (
	"context"
	"fmt"
	"time"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/store"
)

// OperatorResolver resolves a c2.Operator for a given engagement and session.
// The operator may be nil, which signals that all techniques should be Skipped
// (Fronted-tier C2 frameworks have no automation API).
//
// Two implementations exist:
//   - RegistryResolver: production — looks up the engagement's deployed C2
//     topology, calls C2Provider.Control(teamserver), returns (op, true) or
//     (nil, false) for Fronted-tier frameworks.
//   - FakeResolver: dev/CI — returns a deterministic fakeOperator that always
//     succeeds. Selected when RINFRA_DEV=1 or injected directly in tests.
type OperatorResolver interface {
	// Resolve returns an Operator for the engagement. If the engagement's C2
	// framework is Fronted-tier (or has no deployed teamserver), ok=false and
	// the caller must pass op=nil to the orchestrator so techniques are Skipped.
	Resolve(ctx context.Context, eng domain.Engagement) (op c2.Operator, sessionID string, ok bool)
}

// RegistryResolver resolves Operators using the C2 registry and the engagement's
// persisted topology. It is the production resolver.
type RegistryResolver struct {
	infra store.InfraStore
}

// NewRegistryResolver constructs a RegistryResolver.
func NewRegistryResolver(infra store.InfraStore) *RegistryResolver {
	return &RegistryResolver{infra: infra}
}

// Resolve finds the engagement's C2 server node(s), retrieves the C2Provider
// from the registry, and calls Control(Teamserver). If no c2_server node is
// found or the provider is Fronted-tier, ok=false.
func (r *RegistryResolver) Resolve(ctx context.Context, eng domain.Engagement) (c2.Operator, string, bool) {
	nodes, err := r.infra.NodesForEngagement(ctx, eng.ID)
	if err != nil {
		return nil, "", false
	}

	// Find the first live c2_server node.
	for _, n := range nodes {
		if n.Spec.Type != domain.NodeC2Server {
			continue
		}
		if n.Status != domain.NodeLive {
			continue
		}
		framework := n.Spec.C2Framework
		if framework == "" {
			continue
		}

		provider, err := c2.Get(framework)
		if err != nil {
			// Framework not registered — skip.
			continue
		}

		ts := c2.Teamserver{
			Host:   n.PublicIP,
			Port:   0, // port stored per-framework; provider knows default
			Status: string(n.Status),
		}
		op, ok := provider.Control(ts)
		if !ok {
			// Fronted-tier: no automation API.
			return nil, "", false
		}
		// Orchestrated/Scripted tier. The emulation engine enumerates op.Sessions()
		// and picks the first in-scope active agent; this fallback session id is
		// only used if enumeration is unavailable.
		return op, fmt.Sprintf("session-%s", n.ID), true
	}
	return nil, "", false
}

// Candidates implements CandidateResolver: it enumerates every live C2-server
// node for the engagement with its capability metadata and active sessions, so
// the orchestrator can route each technique across frameworks. Fronted-tier
// frameworks contribute a candidate with a nil Operator (a manual-only option).
func (r *RegistryResolver) Candidates(ctx context.Context, eng domain.Engagement) []Candidate {
	nodes, err := r.infra.NodesForEngagement(ctx, eng.ID)
	if err != nil {
		return nil
	}
	var cands []Candidate
	for _, n := range nodes {
		if n.Spec.Type != domain.NodeC2Server || n.Status != domain.NodeLive || n.Spec.C2Framework == "" {
			continue
		}
		provider, err := c2.Get(n.Spec.C2Framework)
		if err != nil {
			continue
		}
		cand := Candidate{
			Framework: n.Spec.C2Framework,
			Tier:      provider.Tier(),
			Caps:      c2.CapabilitiesFor(provider),
		}
		op, ok := provider.Control(c2.Teamserver{Host: n.PublicIP, Status: string(n.Status)})
		if ok && op != nil {
			cand.Operator = op
			sessions, err := op.Sessions(ctx)
			if err != nil {
				// Preserve the failure: an operator that can't list sessions (bad
				// creds, missing operator config) must not look like an empty
				// (scope-blocked) framework.
				cand.Err = fmt.Errorf("%s: list sessions: %w", n.Spec.C2Framework, err)
			} else {
				cand.Sessions = sessions
			}
		}
		cands = append(cands, cand)
	}
	return cands
}

// FakeResolver returns a fake Operator that produces deterministic results.
// Used in RINFRA_DEV mode and in tests. All techniques return ExecSuccess with
// a fake output message. The fake is indistinguishable from the real path from
// the orchestrator's perspective — same interface, same audit events.
type FakeResolver struct{}

// NewFakeResolver returns a FakeResolver.
func NewFakeResolver() *FakeResolver { return &FakeResolver{} }

// Resolve always returns a fakeOperator and ok=true so the orchestrator runs
// all techniques, producing ExecSuccess results for the UI demo and CI.
func (f *FakeResolver) Resolve(_ context.Context, _ domain.Engagement) (c2.Operator, string, bool) {
	return fakeOperator{}, "fake-session-1", true
}

// fakeOperator is a no-op Operator used in dev/test when no real C2 is deployed.
// It returns ExecSuccess for every technique without touching any real system.
type fakeOperator struct{}

func (fakeOperator) StartListener(_ context.Context, _ c2.ListenerSpec) error { return nil }
func (fakeOperator) Sessions(_ context.Context) ([]c2.Session, error) {
	// Host is inside the common test/dev scope (10.0.0.0/8) so scope enforcement
	// admits the fake agent.
	return []c2.Session{{ID: "fake-session-1", Host: "10.10.10.10", User: "SYSTEM"}}, nil
}
func (fakeOperator) Execute(_ context.Context, _ string, t domain.Technique) (domain.Result, error) {
	return domain.Result{
		TechniqueAttackID: t.AttackID,
		Status:            domain.ExecSuccess,
		Output:            fmt.Sprintf("fake execution of %s (%s)", t.AttackID, t.Name),
		StartedAt:         time.Now(),
		FinishedAt:        time.Now(),
	}, nil
}

// FrontedResolver is a test helper that always returns ok=false, simulating a
// Fronted-tier framework with no operator API.
type FrontedResolver struct{}

// NewFrontedResolver returns a resolver that signals Fronted-tier (Skipped).
func NewFrontedResolver() *FrontedResolver { return &FrontedResolver{} }

func (f *FrontedResolver) Resolve(_ context.Context, _ domain.Engagement) (c2.Operator, string, bool) {
	return nil, "", false
}
