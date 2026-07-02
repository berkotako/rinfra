package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rinfra/rinfra/internal/cloud"
	_ "github.com/rinfra/rinfra/internal/cloud/digitalocean" // registers a ProgramBuilder cloud
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration"
	"github.com/rinfra/rinfra/internal/service"
)

// TestDeploy_ConfiguresIngress verifies the deploy path now applies ingress on
// each node that comes live — previously ConfigureIngress was never called, so
// real-cloud nodes came up with no inbound rules (unreachable).
func TestDeploy_ConfiguresIngress(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)
	svcInfra := buildInfraService(t, s, hub)

	saveTestTopology(t, ctx, svcInfra, eng.ID)
	jobID, err := svcInfra.Deploy(ctx, eng.ID, "op")
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if st := waitForJob(t, ctx, s.job, jobID); st != domain.JobDone {
		t.Fatalf("deploy job status = %s, want done", st)
	}
	if !hasAuditAction(s.audit, "infra.ingress", eng.ID) {
		t.Error("expected ingress to be configured (infra.ingress audit) for the live nodes")
	}
}

// errTeardownProvisioner is a registered IaC backend whose Teardown always
// errors, to exercise the failed-teardown path.
type errTeardownProvisioner struct{}

func (errTeardownProvisioner) Deploy(context.Context, string, []domain.Node, map[domain.CloudProviderType]cloud.Credentials) ([]orchestration.NodeResult, error) {
	return nil, nil
}
func (errTeardownProvisioner) Teardown(context.Context, string, []domain.Node, map[domain.CloudProviderType]cloud.Credentials) error {
	return errors.New("simulated stack destroy + sweep failure")
}

// TestTeardown_EngineErrorKeepsNodesReapable verifies that when the IaC backend's
// Teardown fails, nodes are left FAILED (reapable) rather than marked destroyed,
// and the engagement is NOT completed — so a failed teardown is not silently
// reported as success over still-live cloud resources.
func TestTeardown_EngineErrorKeepsNodesReapable(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)
	svcInfra := buildInfraService(t, s, hub)
	// Register a backend whose Teardown fails; DO nodes route through it (the DO
	// provider implements ProgramBuilder).
	svcInfra.RegisterProvisioner(service.BackendPulumi, errTeardownProvisioner{})

	topology := domain.Topology{
		EngagementID: eng.ID,
		Nodes: []domain.Node{
			{ID: "do-1", Spec: domain.NodeSpec{Type: domain.NodeC2Server, Cloud: domain.CloudDigitalOcean, C2Framework: "sliver"}, Status: domain.NodeLive, ProviderRef: "12345"},
		},
	}
	if err := s.infra.SaveTopology(ctx, topology); err != nil {
		t.Fatalf("SaveTopology: %v", err)
	}

	jobID, err := svcInfra.Teardown(ctx, eng.ID, "op")
	if err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if st := waitForJob(t, ctx, s.job, jobID); st != domain.JobFailed {
		t.Errorf("teardown job status = %s, want failed", st)
	}

	top, _ := svcInfra.GetTopology(ctx, eng.ID)
	for _, n := range top.Nodes {
		if n.ID == "do-1" && n.Status != domain.NodeFailed {
			t.Errorf("node after failed teardown = %s, want failed (reapable), not destroyed", n.Status)
		}
	}
	got, err := s.eng.Get(ctx, eng.ID)
	if err != nil {
		t.Fatalf("get engagement: %v", err)
	}
	if got.Status == domain.EngagementCompleted {
		t.Error("engagement must NOT be completed when teardown failed")
	}
}

// TestTeardown_NeverProvisionedNoCreds verifies that tearing down an engagement
// whose engine nodes were never provisioned (empty ProviderRef) and whose creds
// are absent SUCCEEDS — there is nothing to destroy, so missing creds must not
// wedge teardown. Uses the real Pulumi engine; its missing-creds branch returns
// before ever invoking Pulumi, so no CLI is needed.
func TestTeardown_NeverProvisionedNoCreds(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)
	svcInfra := buildInfraService(t, s, hub)

	doProv, err := cloud.Get(domain.CloudDigitalOcean)
	if err != nil {
		t.Skipf("DO provider not registered: %v", err)
	}
	doBuilder, ok := doProv.(orchestration.ProgramBuilder)
	if !ok {
		t.Skip("DO provider does not implement ProgramBuilder")
	}
	engine := orchestration.New(t.TempDir(), testLogger())
	engine.RegisterBuilder(domain.CloudDigitalOcean, doBuilder)
	svcInfra.WithEngine(engine)

	// A DO node that failed to provision (no ProviderRef) with no stored creds.
	topology := domain.Topology{
		EngagementID: eng.ID,
		Nodes: []domain.Node{
			{ID: "do-unprov", Spec: domain.NodeSpec{Type: domain.NodeC2Server, Cloud: domain.CloudDigitalOcean, C2Framework: "sliver"}, Status: domain.NodeFailed},
		},
	}
	if err := s.infra.SaveTopology(ctx, topology); err != nil {
		t.Fatalf("SaveTopology: %v", err)
	}

	jobID, err := svcInfra.Teardown(ctx, eng.ID, "op")
	if err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if st := waitForJob(t, ctx, s.job, jobID); st != domain.JobDone {
		t.Errorf("teardown job status = %s, want done (nothing to destroy)", st)
	}
	top, _ := svcInfra.GetTopology(ctx, eng.ID)
	for _, n := range top.Nodes {
		if n.ID == "do-unprov" && n.Status != domain.NodeDestroyed {
			t.Errorf("never-provisioned node should be marked destroyed, got %s", n.Status)
		}
	}
	if got, _ := s.eng.Get(ctx, eng.ID); got.Status != domain.EngagementCompleted {
		t.Errorf("engagement should complete when there was nothing to tear down, got %s", got.Status)
	}
}
