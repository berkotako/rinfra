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
		// For Orchestrated/Scripted tier: use "fake-session-1" as a placeholder
		// session ID until session enumeration is wired in Phase 6 live path.
		// TODO(live): call op.Sessions() and pick the first active session.
		return op, fmt.Sprintf("session-%s", n.ID), true
	}
	return nil, "", false
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
	return []c2.Session{{ID: "fake-session-1", Host: "203.0.113.1", User: "SYSTEM"}}, nil
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
