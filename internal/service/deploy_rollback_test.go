package service_test

import (
	"context"
	"errors"
	"sync/atomic"
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

// destroyErrProvider provisions fine but its Destroy always errors. It IS
// per-node-scoped, so rollback attempts Destroy — and must keep the node
// failed/reapable (not destroyed) when that Destroy fails.
type destroyErrProvider struct{}

const destroyErrCloud domain.CloudProviderType = "rollback-destroyerr"

func (destroyErrProvider) Type() domain.CloudProviderType { return destroyErrCloud }
func (destroyErrProvider) ProvisionNode(context.Context, cloud.Credentials, domain.NodeSpec) (domain.Node, error) {
	return domain.Node{ProviderRef: "der-ref", PublicIP: "203.0.113.50"}, nil
}
func (destroyErrProvider) ConfigureIngress(context.Context, cloud.Credentials, domain.Node, []domain.Rule) error {
	return nil
}
func (destroyErrProvider) AssignStaticIP(context.Context, cloud.Credentials, domain.Node) (string, error) {
	return "", nil
}
func (destroyErrProvider) ManageDNS(context.Context, cloud.Credentials, domain.Record) error {
	return nil
}
func (destroyErrProvider) Destroy(context.Context, cloud.Credentials, domain.Node) error {
	return errors.New("simulated destroy failure")
}
func (destroyErrProvider) PerNodeDestroy() {}

// scopedProvider simulates an engagement-scoped Destroy (like Azure's whole-RG
// delete): it deliberately does NOT implement cloud.PerNodeDestroyer, so
// rollback must NOT call its Destroy.
type scopedProvider struct{}

const scopedCloud domain.CloudProviderType = "rollback-scoped"

var scopedDestroyCalled atomic.Bool

func (scopedProvider) Type() domain.CloudProviderType { return scopedCloud }
func (scopedProvider) ProvisionNode(context.Context, cloud.Credentials, domain.NodeSpec) (domain.Node, error) {
	return domain.Node{ProviderRef: "scoped-ref", PublicIP: "203.0.113.51"}, nil
}
func (scopedProvider) ConfigureIngress(context.Context, cloud.Credentials, domain.Node, []domain.Rule) error {
	return nil
}
func (scopedProvider) AssignStaticIP(context.Context, cloud.Credentials, domain.Node) (string, error) {
	return "", nil
}
func (scopedProvider) ManageDNS(context.Context, cloud.Credentials, domain.Record) error { return nil }
func (scopedProvider) Destroy(context.Context, cloud.Credentials, domain.Node) error {
	scopedDestroyCalled.Store(true)
	return nil
}

func init() {
	cloud.Register(failProvider{})
	cloud.Register(destroyErrProvider{})
	cloud.Register(scopedProvider{})
}

// partialFailTopology: a redirector on liveCloud (provisions live) fronting a c2
// on the always-failing provider, so the deploy partially fails.
func partialFailTopology(engID string, liveCloud domain.CloudProviderType) domain.Topology {
	return domain.Topology{
		EngagementID: engID,
		Nodes: []domain.Node{
			{ID: "redir-1", Spec: domain.NodeSpec{Type: domain.NodeRedirector, Cloud: liveCloud, Region: "r", Size: "s", ProfileName: "plain"}, Canvas: domain.NodeCanvas{Name: "redir"}},
			{ID: "c2-1", Spec: domain.NodeSpec{Type: domain.NodeC2Server, Cloud: failCloud, Region: "r", Size: "s", C2Framework: "sliver"}, Canvas: domain.NodeCanvas{Name: "c2"}},
		},
		Edges: []domain.Edge{{FromNodeID: "redir-1", ToNodeID: "c2-1"}},
	}
}

// TestDeploy_Rollback_DestroyErrorKeepsNodeReapable: a node whose Destroy fails
// during rollback is left failed (reapable), not marked destroyed, so a later
// teardown/sweep still removes the live resource.
func TestDeploy_Rollback_DestroyErrorKeepsNodeReapable(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)
	svcInfra := buildInfraService(t, s, hub)

	if err := svcInfra.SaveTopology(ctx, eng.ID, partialFailTopology(eng.ID, destroyErrCloud), "op"); err != nil {
		t.Fatalf("SaveTopology: %v", err)
	}
	jobID, err := svcInfra.Deploy(ctx, eng.ID, "op")
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	waitForJob(t, ctx, s.job, jobID)

	top, _ := svcInfra.GetTopology(ctx, eng.ID)
	for _, n := range top.Nodes {
		if n.ID == "redir-1" && n.Status != domain.NodeFailed {
			t.Errorf("node with failed Destroy should be failed/reapable, got %s", n.Status)
		}
	}
}

// TestDeploy_Rollback_SkipsEngagementScopedProvider: rollback must NOT call
// Destroy on a provider whose Destroy is engagement-scoped (would nuke siblings);
// the node is left reapable for engagement teardown.
func TestDeploy_Rollback_SkipsEngagementScopedProvider(t *testing.T) {
	scopedDestroyCalled.Store(false)
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)
	svcInfra := buildInfraService(t, s, hub)

	if err := svcInfra.SaveTopology(ctx, eng.ID, partialFailTopology(eng.ID, scopedCloud), "op"); err != nil {
		t.Fatalf("SaveTopology: %v", err)
	}
	jobID, err := svcInfra.Deploy(ctx, eng.ID, "op")
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	waitForJob(t, ctx, s.job, jobID)

	if scopedDestroyCalled.Load() {
		t.Error("rollback must not call Destroy on an engagement-scoped provider")
	}
	top, _ := svcInfra.GetTopology(ctx, eng.ID)
	for _, n := range top.Nodes {
		if n.ID == "redir-1" && n.Status != domain.NodeFailed {
			t.Errorf("engagement-scoped node should be left failed/reapable, got %s", n.Status)
		}
	}
}

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
