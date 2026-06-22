package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/service"
)

func draftEngagement(t *testing.T, ctx context.Context, svc *service.EngagementService) domain.Engagement {
	t.Helper()
	e, err := svc.Create(ctx, domain.Engagement{
		Client: "Acme",
		Scope:  domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
	}, "creator")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return e
}

func TestAuthorize_RejectsIncompleteAuthorization(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	svc := service.NewEngagementService(s.eng, s.audit)
	e := draftEngagement(t, ctx, svc)

	// Missing documentRef.
	_, err := svc.Authorize(ctx, e.ID, domain.Authorization{
		AuthorizedBy: "CISO",
		ExpiresAt:    time.Now().Add(time.Hour),
	}, "lead")
	if !errors.Is(err, domain.ErrAuthIncomplete) {
		t.Fatalf("want ErrAuthIncomplete, got %v", err)
	}

	// Engagement must still be draft (not flipped to authorized).
	got, _ := svc.Get(ctx, e.ID)
	if got.Status != domain.EngagementDraft {
		t.Errorf("status = %q, want draft after failed authorize", got.Status)
	}
}

func TestAuthorize_ValidThenRejectFromTerminal(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	svc := service.NewEngagementService(s.eng, s.audit)
	e := draftEngagement(t, ctx, svc)

	auth := domain.Authorization{AuthorizedBy: "CISO", DocumentRef: "doc-1", ExpiresAt: time.Now().Add(time.Hour)}
	authd, err := svc.Authorize(ctx, e.ID, auth, "lead")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if authd.Status != domain.EngagementAuthorized {
		t.Fatalf("status = %q, want authorized", authd.Status)
	}
	if authd.Authorization.GrantedAt.IsZero() {
		t.Error("grantedAt should be defaulted to now when omitted")
	}

	// Drive to a terminal state and confirm it can't be re-authorized.
	if err := svc.UpdateStatus(ctx, e.ID, domain.EngagementActive, "lead"); err != nil {
		t.Fatalf("authorized->active: %v", err)
	}
	if err := svc.UpdateStatus(ctx, e.ID, domain.EngagementCompleted, "lead"); err != nil {
		t.Fatalf("active->completed: %v", err)
	}
	if _, err := svc.Authorize(ctx, e.ID, auth, "lead"); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("re-authorize completed: want ErrInvalidTransition, got %v", err)
	}
}

func TestUpdateStatus_RejectsInvalidTransition(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	svc := service.NewEngagementService(s.eng, s.audit)
	e := draftEngagement(t, ctx, svc)

	// draft -> active skips authorization.
	if err := svc.UpdateStatus(ctx, e.ID, domain.EngagementActive, "lead"); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("draft->active: want ErrInvalidTransition, got %v", err)
	}
	// draft -> authorized must use Authorize.
	if err := svc.UpdateStatus(ctx, e.ID, domain.EngagementAuthorized, "lead"); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("draft->authorized via UpdateStatus: want ErrInvalidTransition, got %v", err)
	}
}
