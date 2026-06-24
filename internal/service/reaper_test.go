package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/rinfra/rinfra/internal/cloud/fake"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/service"
)

func liveTopology(engID string) domain.Topology {
	return domain.Topology{
		EngagementID: engID,
		Nodes: []domain.Node{{
			ID:     "n1",
			Status: domain.NodeLive,
			Spec:   domain.NodeSpec{Type: domain.NodeC2Server, Cloud: fake.CloudProviderTypeFake},
		}},
	}
}

// TestReapExpired_TearsDownExpiredWithLiveInfra verifies the auto-teardown
// reaper: an authorized engagement with live infra is left alone while its
// window is open, and reaped (audited teardown) once the window has closed.
func TestReapExpired_TearsDownExpiredWithLiveInfra(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng) // expires ~1h from now
	svcInfra := buildInfraService(t, s, hub)

	if err := svcInfra.SaveTopology(ctx, eng.ID, liveTopology(eng.ID), "op"); err != nil {
		t.Fatalf("SaveTopology: %v", err)
	}

	// Window still open → not reaped.
	if reaped, err := svcInfra.ReapExpired(ctx, time.Now()); err != nil {
		t.Fatalf("ReapExpired: %v", err)
	} else if len(reaped) != 0 {
		t.Fatalf("expected no reap before expiry, got %v", reaped)
	}

	// After the authorization window closes → reaped + audited.
	future := eng.Authorization.ExpiresAt.Add(time.Hour)
	reaped, err := svcInfra.ReapExpired(ctx, future)
	if err != nil {
		t.Fatalf("ReapExpired: %v", err)
	}
	if len(reaped) != 1 || reaped[0] != eng.ID {
		t.Fatalf("expected [%s] reaped, got %v", eng.ID, reaped)
	}
	if !hasAuditAction(s.audit, "infra.auto_teardown", eng.ID) {
		t.Error("expected infra.auto_teardown audit event")
	}
}

// TestReapExpired_ReapsFailedNodes verifies that a node left in `failed` after a
// partial deploy (which may still own cloud resources in IaC state) is reaped —
// otherwise the auto-teardown path would leave exactly the orphans it prevents.
func TestReapExpired_ReapsFailedNodes(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)
	svcInfra := buildInfraService(t, s, hub)

	topo := domain.Topology{
		EngagementID: eng.ID,
		Nodes:        []domain.Node{{ID: "n1", Status: domain.NodeFailed, Spec: domain.NodeSpec{Type: domain.NodeC2Server, Cloud: fake.CloudProviderTypeFake}}},
	}
	if err := svcInfra.SaveTopology(ctx, eng.ID, topo, "op"); err != nil {
		t.Fatalf("SaveTopology: %v", err)
	}

	future := eng.Authorization.ExpiresAt.Add(time.Hour)
	reaped, err := svcInfra.ReapExpired(ctx, future)
	if err != nil {
		t.Fatalf("ReapExpired: %v", err)
	}
	if len(reaped) != 1 || reaped[0] != eng.ID {
		t.Fatalf("expected failed-node engagement reaped, got %v", reaped)
	}
}

// TestReapExpired_SkipsExpiredWithNoLiveInfra verifies an expired engagement
// with no standing infrastructure is not torn down (nothing to reap).
func TestReapExpired_SkipsExpiredWithNoLiveInfra(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)
	svcInfra := buildInfraService(t, s, hub)

	// A pending (never-provisioned) node — not standing infrastructure.
	topo := domain.Topology{
		EngagementID: eng.ID,
		Nodes:        []domain.Node{{ID: "n1", Status: domain.NodePending, Spec: domain.NodeSpec{Type: domain.NodeC2Server, Cloud: fake.CloudProviderTypeFake}}},
	}
	if err := svcInfra.SaveTopology(ctx, eng.ID, topo, "op"); err != nil {
		t.Fatalf("SaveTopology: %v", err)
	}

	future := eng.Authorization.ExpiresAt.Add(time.Hour)
	reaped, err := svcInfra.ReapExpired(ctx, future)
	if err != nil {
		t.Fatalf("ReapExpired: %v", err)
	}
	if len(reaped) != 0 {
		t.Errorf("expected no reap with no live infra, got %v", reaped)
	}
}
