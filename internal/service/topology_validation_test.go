package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rinfra/rinfra/internal/cloud/fake"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/service"
)

// validTopology returns a deployable two-node topology (redirector → c2_server)
// on the fake provider with all required fields populated.
func validTopology(engID string) domain.Topology {
	prov := fake.CloudProviderTypeFake
	return domain.Topology{
		EngagementID: engID,
		Nodes: []domain.Node{
			{
				ID:     "redir-1",
				Spec:   domain.NodeSpec{Type: domain.NodeRedirector, Cloud: prov, Region: "nyc3", Size: "s-1vcpu-1gb", ProfileName: "https"},
				Canvas: domain.NodeCanvas{Name: "redir-01"},
			},
			{
				ID:     "c2-1",
				Spec:   domain.NodeSpec{Type: domain.NodeC2Server, Cloud: prov, Region: "nyc3", Size: "s-2vcpu-4gb", C2Framework: "sliver"},
				Canvas: domain.NodeCanvas{Name: "c2-01"},
			},
		},
		Edges: []domain.Edge{{FromNodeID: "redir-1", ToNodeID: "c2-1"}},
	}
}

func TestValidateTopology_Cases(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(tp *domain.Topology)
		wantProblem string // substring expected among the problems ("" = expect none)
	}{
		{"valid", func(*domain.Topology) {}, ""},
		{"no nodes", func(tp *domain.Topology) { tp.Nodes = nil; tp.Edges = nil }, "no nodes"},
		{"no c2", func(tp *domain.Topology) { tp.Nodes = tp.Nodes[:1]; tp.Edges = nil }, "at least one c2_server"},
		{"no redirector", func(tp *domain.Topology) { tp.Nodes = tp.Nodes[1:]; tp.Edges = nil }, "at least one redirector"},
		{"c2 without redirector edge", func(tp *domain.Topology) { tp.Edges = nil }, "no inbound edge from a redirector"},
		{"invalid provider", func(tp *domain.Topology) { tp.Nodes[1].Spec.Cloud = "frobnicate" }, "unknown cloud provider"},
		{"invalid c2 framework", func(tp *domain.Topology) { tp.Nodes[1].Spec.C2Framework = "notaframework" }, "unknown C2 framework"},
		{"missing c2 framework", func(tp *domain.Topology) { tp.Nodes[1].Spec.C2Framework = "" }, "missing C2 framework"},
		{"missing region", func(tp *domain.Topology) { tp.Nodes[1].Spec.Region = "" }, "missing region"},
		{"missing size", func(tp *domain.Topology) { tp.Nodes[0].Spec.Size = "" }, "missing size"},
		{"missing redirector profile", func(tp *domain.Topology) { tp.Nodes[0].Spec.ProfileName = "" }, "missing profile"},
		{"injection front domain", func(tp *domain.Topology) { tp.Nodes[0].Canvas.FrontDomain = "evil.com;\n}\nserver{}" }, "invalid front domain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStores()
			hub := service.NewHub()
			svcEng := service.NewEngagementService(s.eng, s.audit)
			eng := authorizedEngagement(t, ctx, svcEng)
			svcInfra := buildInfraService(t, s, hub)

			tp := validTopology(eng.ID)
			tt.mutate(&tp)
			if err := svcInfra.SaveTopology(ctx, eng.ID, tp, "op"); err != nil {
				t.Fatalf("SaveTopology: %v", err)
			}

			problems, err := svcInfra.ValidateTopology(ctx, eng.ID)
			if err != nil {
				t.Fatalf("ValidateTopology: %v", err)
			}
			joined := strings.Join(problems, " | ")
			if tt.wantProblem == "" {
				if len(problems) != 0 {
					t.Errorf("expected no problems, got: %s", joined)
				}
				return
			}
			if !strings.Contains(joined, tt.wantProblem) {
				t.Errorf("problems %q do not contain %q", joined, tt.wantProblem)
			}
		})
	}
}

// TestDeploy_RefusesInvalidTopology proves Deploy itself enforces validation
// even when the client skipped the validate endpoint, and no job is created.
func TestDeploy_RefusesInvalidTopology(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)
	svcInfra := buildInfraService(t, s, hub)

	// Malformed: drop the redirector→c2 edge so the c2_server is unfronted.
	tp := validTopology(eng.ID)
	tp.Edges = nil
	if err := svcInfra.SaveTopology(ctx, eng.ID, tp, "op"); err != nil {
		t.Fatalf("SaveTopology: %v", err)
	}

	_, err := svcInfra.Deploy(ctx, eng.ID, "op")
	if !errors.Is(err, service.ErrInvalidTopology) {
		t.Fatalf("Deploy: want ErrInvalidTopology, got %v", err)
	}
	if jobs, _ := s.job.ListRunning(ctx); len(jobs) != 0 {
		t.Errorf("no job should be created on invalid topology, got %d running", len(jobs))
	}

	// A valid topology deploys.
	if err := svcInfra.SaveTopology(ctx, eng.ID, validTopology(eng.ID), "op"); err != nil {
		t.Fatalf("SaveTopology valid: %v", err)
	}
	if _, err := svcInfra.Deploy(ctx, eng.ID, "op"); err != nil {
		t.Fatalf("Deploy valid topology: %v", err)
	}
}
