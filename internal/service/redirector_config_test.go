package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rinfra/rinfra/internal/cloud/fake"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/service"
)

// TestRedirectorConfig_ResolvesUpstreamAndProfile verifies the redirector
// translation layer: the rendered nginx config points at the C2 node the
// redirector has an edge to, on the port from that node's listener, with the
// named profile's path allowlist applied.
func TestRedirectorConfig_ResolvesUpstreamAndProfile(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcInfra := buildInfraService(t, s, hub)

	engID := "eng-redir"
	prov := fake.CloudProviderTypeFake
	topo := domain.Topology{
		EngagementID: engID,
		Nodes: []domain.Node{
			{
				ID:     "redir-1",
				Spec:   domain.NodeSpec{Type: domain.NodeRedirector, Cloud: prov, Subtype: "https", ProfileName: "api-relay"},
				Canvas: domain.NodeCanvas{Name: "redir", FrontDomain: "cdn.victim.test"},
			},
			{
				ID:       "c2-1",
				Spec:     domain.NodeSpec{Type: domain.NodeC2Server, Cloud: prov, C2Framework: "sliver"},
				Canvas:   domain.NodeCanvas{Name: "c2", Listener: "0.0.0.0:8443"},
				PublicIP: "203.0.113.10",
			},
		},
		Edges: []domain.Edge{{FromNodeID: "redir-1", ToNodeID: "c2-1"}},
	}
	if err := svcInfra.SaveTopology(ctx, engID, topo, "op"); err != nil {
		t.Fatalf("SaveTopology: %v", err)
	}

	cfg, err := svcInfra.RedirectorConfig(ctx, engID, "redir-1")
	if err != nil {
		t.Fatalf("RedirectorConfig: %v", err)
	}
	for _, want := range []string{
		"listen 443 ssl;",                       // https subtype
		"server_name cdn.victim.test;",          // front domain
		"proxy_pass https://203.0.113.10:8443;", // resolved upstream IP + listener port
		"location /api/ {",                      // api-relay profile path
		"return 444;",                           // default deny
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config missing %q\n---\n%s", want, cfg)
		}
	}
}

func TestRedirectorConfig_Errors(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcInfra := buildInfraService(t, s, hub)
	engID := "eng-redir-err"
	prov := fake.CloudProviderTypeFake

	// Redirector with an edge to an un-provisioned C2 (no PublicIP) → error.
	topo := domain.Topology{
		EngagementID: engID,
		Nodes: []domain.Node{
			{ID: "r", Spec: domain.NodeSpec{Type: domain.NodeRedirector, Cloud: prov, Subtype: "http"}},
			{ID: "c", Spec: domain.NodeSpec{Type: domain.NodeC2Server, Cloud: prov}},
		},
		Edges: []domain.Edge{{FromNodeID: "r", ToNodeID: "c"}},
	}
	if err := svcInfra.SaveTopology(ctx, engID, topo, "op"); err != nil {
		t.Fatalf("SaveTopology: %v", err)
	}
	if _, err := svcInfra.RedirectorConfig(ctx, engID, "c"); err == nil {
		t.Error("expected error for a non-redirector node")
	}
	if _, err := svcInfra.RedirectorConfig(ctx, engID, "r"); err == nil {
		t.Error("expected error when the target has no public IP yet")
	}
}
