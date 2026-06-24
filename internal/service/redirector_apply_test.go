package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rinfra/rinfra/internal/c2/deploy"
	"github.com/rinfra/rinfra/internal/cloud/fake"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/redirector"
	"github.com/rinfra/rinfra/internal/service"
)

// TestApplyRedirector_UploadsConfigAndInstalls verifies the on-box apply path:
// the rendered config and an install script are uploaded over the (fake) SSH
// runner, the install script is run, and the action is audited.
func TestApplyRedirector_UploadsConfigAndInstalls(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)
	svcInfra := buildInfraService(t, s, hub)

	runner := deploy.NewFakeRunner()
	svcInfra.WithRedirectorRunner(func(string) deploy.Runner { return runner })

	prov := fake.CloudProviderTypeFake
	topo := domain.Topology{
		EngagementID: eng.ID,
		Nodes: []domain.Node{
			{
				ID:       "redir-1",
				Status:   domain.NodeLive,
				PublicIP: "198.51.100.7", // the redirector box we SSH into
				Spec:     domain.NodeSpec{Type: domain.NodeRedirector, Cloud: prov, Subtype: "http", ProfileName: "api-relay"},
				Canvas:   domain.NodeCanvas{Name: "redir"},
			},
			{
				ID:       "c2-1",
				Status:   domain.NodeLive,
				PublicIP: "203.0.113.10",
				Spec:     domain.NodeSpec{Type: domain.NodeC2Server, Cloud: prov, C2Framework: "sliver"},
				Canvas:   domain.NodeCanvas{Name: "c2", Listener: "0.0.0.0:8080"},
			},
		},
		Edges: []domain.Edge{{FromNodeID: "redir-1", ToNodeID: "c2-1"}},
	}
	if err := svcInfra.SaveTopology(ctx, eng.ID, topo, "op"); err != nil {
		t.Fatalf("SaveTopology: %v", err)
	}

	if err := svcInfra.ApplyRedirector(ctx, eng.ID, "redir-1", "op"); err != nil {
		t.Fatalf("ApplyRedirector: %v", err)
	}

	cfg, ok := runner.Uploaded(redirector.StagePath)
	if !ok {
		t.Fatal("config was not uploaded to the staging path")
	}
	if !strings.Contains(cfg, "proxy_pass http://203.0.113.10:8080;") {
		t.Errorf("uploaded config does not target the resolved upstream:\n%s", cfg)
	}
	if _, ok := runner.Uploaded("/tmp/rinfra-redirector-install.sh"); !ok {
		t.Error("install script was not uploaded")
	}
	if !hasAuditAction(s.audit, "redirector.configure", eng.ID) {
		t.Error("expected redirector.configure audit event")
	}
}

// TestApplyRedirector_RefusedWhenNotDeployable verifies the CanDeploy gate: a
// draft (unauthorized) engagement cannot apply redirector config even if the
// topology has live PublicIPs.
func TestApplyRedirector_RefusedWhenNotDeployable(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)

	draft, err := svcEng.Create(ctx, domain.Engagement{
		Client: "Draft Co", Codename: "DRAFT-REDIR", Status: domain.EngagementDraft,
		Scope:          domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
		EngagementType: domain.EngagementTypeRedTeam,
	}, "op")
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}

	svcInfra := buildInfraService(t, s, hub)
	svcInfra.WithRedirectorRunner(func(string) deploy.Runner { return deploy.NewFakeRunner() })
	prov := fake.CloudProviderTypeFake
	topo := domain.Topology{
		EngagementID: draft.ID,
		Nodes: []domain.Node{
			{ID: "redir-1", Status: domain.NodeLive, PublicIP: "198.51.100.7", Spec: domain.NodeSpec{Type: domain.NodeRedirector, Cloud: prov, Subtype: "http"}},
			{ID: "c2-1", Status: domain.NodeLive, PublicIP: "203.0.113.10", Spec: domain.NodeSpec{Type: domain.NodeC2Server, Cloud: prov}, Canvas: domain.NodeCanvas{Listener: ":80"}},
		},
		Edges: []domain.Edge{{FromNodeID: "redir-1", ToNodeID: "c2-1"}},
	}
	if err := svcInfra.SaveTopology(ctx, draft.ID, topo, "op"); err != nil {
		t.Fatalf("SaveTopology: %v", err)
	}
	if err := svcInfra.ApplyRedirector(ctx, draft.ID, "redir-1", "op"); err == nil {
		t.Error("expected apply to be refused on a non-deployable engagement")
	}
}

// TestApplyRedirector_RequiresProvisionedRedirector verifies apply fails clearly
// when the redirector box has no public IP yet.
func TestApplyRedirector_RequiresProvisionedRedirector(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)
	svcInfra := buildInfraService(t, s, hub)
	svcInfra.WithRedirectorRunner(func(string) deploy.Runner { return deploy.NewFakeRunner() })

	prov := fake.CloudProviderTypeFake
	topo := domain.Topology{
		EngagementID: eng.ID,
		Nodes: []domain.Node{
			{ID: "redir-1", Spec: domain.NodeSpec{Type: domain.NodeRedirector, Cloud: prov, Subtype: "http"}}, // no PublicIP
			{ID: "c2-1", PublicIP: "203.0.113.10", Spec: domain.NodeSpec{Type: domain.NodeC2Server, Cloud: prov}, Canvas: domain.NodeCanvas{Listener: ":80"}},
		},
		Edges: []domain.Edge{{FromNodeID: "redir-1", ToNodeID: "c2-1"}},
	}
	if err := svcInfra.SaveTopology(ctx, eng.ID, topo, "op"); err != nil {
		t.Fatalf("SaveTopology: %v", err)
	}
	if err := svcInfra.ApplyRedirector(ctx, eng.ID, "redir-1", "op"); err == nil {
		t.Error("expected error applying to a redirector with no public IP")
	}
}
