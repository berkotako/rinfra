package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/service"
)

func projectEngagement(t *testing.T, ctx context.Context, svc *service.EngagementService, projectID, client string, authorize bool) domain.Engagement {
	t.Helper()
	e, err := svc.Create(ctx, domain.Engagement{
		ProjectID: projectID,
		Client:    client,
		Scope:     domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
	}, "creator")
	if err != nil {
		t.Fatalf("create %s: %v", client, err)
	}
	if authorize {
		_, err = svc.Authorize(ctx, e.ID, domain.Authorization{
			AuthorizedBy: "CISO", DocumentRef: "doc-1",
			GrantedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour),
		}, "lead")
		if err != nil {
			t.Fatalf("authorize %s: %v", client, err)
		}
	}
	return e
}

func TestStartProjectRun_FansOutGatedPerEngagement(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	engSvc := service.NewEngagementService(s.eng, s.audit)

	// Two engagements in the project: one authorized (will run), one left draft
	// (must be skipped, not fail the whole batch). A third in another project
	// must be untouched.
	authd := projectEngagement(t, ctx, engSvc, "p1", "Acme", true)
	draft := projectEngagement(t, ctx, engSvc, "p1", "Globex", false)
	_ = projectEngagement(t, ctx, engSvc, "p2", "Other", true)

	emu := service.NewEmulationService(s.eng, s.scenario, s.audit, hub)
	emu.WithResolver(service.NewFakeResolver())

	res, err := emu.StartProjectRun(ctx, "p1", "apt29", "lead")
	if err != nil {
		t.Fatalf("StartProjectRun: %v", err)
	}
	if len(res.Started) != 1 || res.Started[0].EngagementID != authd.ID {
		t.Errorf("Started = %+v, want only the authorized engagement %s", res.Started, authd.ID)
	}
	if res.Started[0].RunID == "" {
		t.Error("started run has no run id")
	}
	if len(res.Skipped) != 1 || res.Skipped[0].EngagementID != draft.ID {
		t.Errorf("Skipped = %+v, want the draft engagement %s", res.Skipped, draft.ID)
	}
}

func TestStartProjectRun_UnknownScenario(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	emu := service.NewEmulationService(s.eng, s.scenario, s.audit, service.NewHub())
	emu.WithResolver(service.NewFakeResolver())
	if _, err := emu.StartProjectRun(ctx, "p1", "no-such-scenario", "lead"); err == nil {
		t.Error("expected error for unknown scenario")
	}
}

func TestGetProjectCoverage_Aggregates(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	engSvc := service.NewEngagementService(s.eng, s.audit)
	projectEngagement(t, ctx, engSvc, "p1", "Acme", true)

	emu := service.NewEmulationService(s.eng, s.scenario, s.audit, service.NewHub())
	cov, err := emu.GetProjectCoverage(ctx, "p1")
	if err != nil {
		t.Fatalf("GetProjectCoverage: %v", err)
	}
	if cov.EngagementID != "p1" {
		t.Errorf("scope id = %q, want p1", cov.EngagementID)
	}
	if cov.TotalTechniques == 0 {
		t.Error("expected the catalog technique total in the rollup")
	}
}
