package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/cloud/fake"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/service"
)

// failProvider is a registered cloud provider whose ProvisionNode always fails,
// used to force a partial deploy failure deterministically. Other methods are
// no-ops (Destroy succeeds so it's never the reason a rollback "fails").
type failProvider struct{}

const failCloud domain.CloudProviderType = "failrollback"

func (failProvider) Type() domain.CloudProviderType { return failCloud }
func (failProvider) ProvisionNode(context.Context, cloud.Credentials, domain.NodeSpec) (domain.Node, error) {
	return domain.Node{}, errors.New("simulated provision failure")
}
func (failProvider) ConfigureIngress(context.Context, cloud.Credentials, domain.Node, []domain.Rule) error {
	return nil
}
func (failProvider) AssignStaticIP(context.Context, cloud.Credentials, domain.Node) (string, error) {
	return "", nil
}
func (failProvider) ManageDNS(context.Context, cloud.Credentials, domain.Record) error { return nil }
func (failProvider) Destroy(context.Context, cloud.Credentials, domain.Node) error     { return nil }

func init() { cloud.Register(failProvider{}) }

// rollbackTopology: a redirector on the fake provider (provisions live) fronting
// a c2 on the always-failing provider (fails) — so the deploy partially fails
// with one node already live.
func rollbackTopology(engID string) domain.Topology {
	return domain.Topology{
		EngagementID: engID,
		Nodes: []domain.Node{
			{
				ID:     "redir-1",
				Spec:   domain.NodeSpec{Type: domain.NodeRedirector, Cloud: fake.CloudProviderTypeFake, Region: "nyc3", Size: "s-1", ProfileName: "plain"},
				Canvas: domain.NodeCanvas{Name: "redir"},
			},
			{
				ID:     "c2-1",
				Spec:   domain.NodeSpec{Type: domain.NodeC2Server, Cloud: failCloud, Region: "nyc3", Size: "s-1", C2Framework: "sliver"},
				Canvas: domain.NodeCanvas{Name: "c2"},
			},
		},
		Edges: []domain.Edge{{FromNodeID: "redir-1", ToNodeID: "c2-1"}},
	}
}

// TestDeploy_RollsBackOnPartialFailure verifies that when one node fails to
// provision, the node(s) the deploy DID bring live are rolled back (destroyed),
// so a partial deploy leaves no live orphans.
func TestDeploy_RollsBackOnPartialFailure(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)
	svcInfra := buildInfraService(t, s, hub)

	if err := svcInfra.SaveTopology(ctx, eng.ID, rollbackTopology(eng.ID), "op"); err != nil {
		t.Fatalf("SaveTopology: %v", err)
	}
	jobID, err := svcInfra.Deploy(ctx, eng.ID, "op")
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	waitForJob(t, ctx, s.job, jobID)

	top, err := svcInfra.GetTopology(ctx, eng.ID)
	if err != nil {
		t.Fatalf("GetTopology: %v", err)
	}
	byID := map[string]domain.Node{}
	for _, n := range top.Nodes {
		byID[n.ID] = n
	}
	if got := byID["c2-1"].Status; got != domain.NodeFailed {
		t.Errorf("failing node status = %s, want failed", got)
	}
	if got := byID["redir-1"].Status; got != domain.NodeDestroyed {
		t.Errorf("provisioned node should be rolled back to destroyed, got %s", got)
	}
	if !hasAuditAction(s.audit, "infra.rollback", eng.ID) {
		t.Error("expected an infra.rollback audit event")
	}
}

// TestDeploy_RollbackDisabled verifies the RINFRA_DEPLOY_ROLLBACK=off kill switch
// leaves a partially-deployed node live for fix-forward.
func TestDeploy_RollbackDisabled(t *testing.T) {
	t.Setenv("RINFRA_DEPLOY_ROLLBACK", "off")
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)
	svcInfra := buildInfraService(t, s, hub)

	if err := svcInfra.SaveTopology(ctx, eng.ID, rollbackTopology(eng.ID), "op"); err != nil {
		t.Fatalf("SaveTopology: %v", err)
	}
	jobID, err := svcInfra.Deploy(ctx, eng.ID, "op")
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	waitForJob(t, ctx, s.job, jobID)

	top, _ := svcInfra.GetTopology(ctx, eng.ID)
	for _, n := range top.Nodes {
		if n.ID == "redir-1" && n.Status != domain.NodeLive {
			t.Errorf("with rollback off, provisioned node should stay live, got %s", n.Status)
		}
	}
	if hasAuditAction(s.audit, "infra.rollback", eng.ID) {
		t.Error("rollback should not run when disabled")
	}
}
