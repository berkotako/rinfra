package service_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/cloud/fake"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/service"
)

// blockingProvider provisions a node only after the test signals `proceed`, and
// announces entry via `started` — letting a test delete the node from the
// topology at the exact moment it's mid-provision. Its Destroy records that a
// rollback reached it. It IS a PerNodeDestroyer so rollback actually destroys it.
type blockingProvider struct {
	mu        sync.Mutex
	started   chan struct{}
	proceed   chan struct{}
	destroyed atomic.Bool
}

const blockCloud domain.CloudProviderType = "blockrollback"

var blockProv = &blockingProvider{started: make(chan struct{}), proceed: make(chan struct{})}

func (p *blockingProvider) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.started = make(chan struct{})
	p.proceed = make(chan struct{})
	p.destroyed.Store(false)
}

func (p *blockingProvider) chans() (started, proceed chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.started, p.proceed
}

func (p *blockingProvider) Type() domain.CloudProviderType { return blockCloud }

func (p *blockingProvider) ProvisionNode(_ context.Context, _ cloud.Credentials, _ domain.NodeSpec) (domain.Node, error) {
	started, proceed := p.chans()
	close(started)
	<-proceed
	return domain.Node{ProviderRef: "block-ref", PublicIP: "203.0.113.77", Status: domain.NodeLive, Health: domain.HealthHealthy}, nil
}

func (p *blockingProvider) Destroy(context.Context, cloud.Credentials, domain.Node) error {
	p.destroyed.Store(true)
	return nil
}
func (p *blockingProvider) PerNodeDestroy() {}
func (p *blockingProvider) ConfigureIngress(context.Context, cloud.Credentials, domain.Node, []domain.Rule) error {
	return nil
}
func (p *blockingProvider) AssignStaticIP(context.Context, cloud.Credentials, domain.Node) (string, error) {
	return "", nil
}
func (p *blockingProvider) ManageDNS(context.Context, cloud.Credentials, domain.Record) error {
	return nil
}

func init() { cloud.Register(blockProv) }

// TestDeploy_RollsBackNodeDeletedDuringProvisioning verifies the fix for the
// delete-during-provision orphan: if a node is removed from the topology while
// its provisioning call is in flight, the just-created cloud resource is rolled
// back (Destroy called) instead of being treated as live and orphaned.
func TestDeploy_RollsBackNodeDeletedDuringProvisioning(t *testing.T) {
	blockProv.reset()
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)
	svcInfra := buildInfraService(t, s, hub)

	// Redirector on the fake (provisions first), C2 on the blocking provider.
	redir := domain.Node{ID: "redir-1", Spec: domain.NodeSpec{Type: domain.NodeRedirector, Cloud: fake.CloudProviderTypeFake, Region: "r", Size: "s", ProfileName: "plain"}, Canvas: domain.NodeCanvas{Name: "redir", FrontDomain: "cdn.example.com"}}
	c2 := domain.Node{ID: "c2-1", Spec: domain.NodeSpec{Type: domain.NodeC2Server, Cloud: blockCloud, Region: "r", Size: "s", C2Framework: "sliver"}, Canvas: domain.NodeCanvas{Name: "c2"}}
	if err := svcInfra.SaveTopology(ctx, eng.ID, domain.Topology{
		EngagementID: eng.ID,
		Nodes:        []domain.Node{redir, c2},
		Edges:        []domain.Edge{{FromNodeID: "redir-1", ToNodeID: "c2-1"}},
	}, "op"); err != nil {
		t.Fatalf("SaveTopology: %v", err)
	}

	started, proceed := blockProv.chans()
	jobID, err := svcInfra.Deploy(ctx, eng.ID, "op")
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	// Wait until the blocking provider is mid-provision on c2, delete c2 from the
	// topology (concurrent edit), then let its provisioning finish.
	<-started
	if err := s.infra.SaveTopology(ctx, domain.Topology{
		EngagementID: eng.ID,
		Nodes:        []domain.Node{{ID: "redir-1", EngagementID: eng.ID, Status: domain.NodeLive, Spec: redir.Spec, Canvas: redir.Canvas}},
	}); err != nil {
		t.Fatalf("delete c2 via SaveTopology: %v", err)
	}
	close(proceed)

	waitForJob(t, ctx, s.job, jobID)

	if !blockProv.destroyed.Load() {
		t.Error("node deleted during provisioning must be rolled back (Destroy not called)")
	}
	if !hasAuditAction(s.audit, "infra.rollback", eng.ID) {
		t.Error("expected an infra.rollback audit for the deleted-during-provision node")
	}
	// c2 must not linger in the topology.
	top, _ := svcInfra.GetTopology(ctx, eng.ID)
	for _, n := range top.Nodes {
		if n.ID == "c2-1" {
			t.Error("deleted node should not be present in the topology")
		}
	}
}
