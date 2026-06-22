// Package service holds business logic for the RInfra control plane. Each
// service receives its dependencies via constructor injection (store interfaces,
// audit.Logger, Hub) — no global state, no live cloud or database in tests.
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/rinfra/rinfra/internal/audit"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/store"
)

// EngagementService manages the engagement lifecycle: creation, authorization,
// status transitions, and listing.
type EngagementService struct {
	engagements store.EngagementStore
	audit       audit.Logger
}

// NewEngagementService returns an EngagementService.
func NewEngagementService(engagements store.EngagementStore, a audit.Logger) *EngagementService {
	return &EngagementService{engagements: engagements, audit: a}
}

// Create persists a new engagement and records an engagement.create audit event.
// The engagement must have at least one scope target; status defaults to Draft.
func (s *EngagementService) Create(ctx context.Context, e domain.Engagement, actor string) (domain.Engagement, error) {
	if e.Status == "" {
		e.Status = domain.EngagementDraft
	}

	id, err := s.engagements.Create(ctx, e)
	if err != nil {
		return domain.Engagement{}, fmt.Errorf("create engagement: %w", err)
	}
	e.ID = id

	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: id,
		Actor:        actor,
		Action:       "engagement.create",
		Target:       id,
		Detail:       fmt.Sprintf("client=%s codename=%s", e.Client, e.Codename),
		At:           time.Now().UTC(),
	})

	return e, nil
}

// Get returns the engagement with the given ID.
func (s *EngagementService) Get(ctx context.Context, id string) (domain.Engagement, error) {
	return s.engagements.Get(ctx, id)
}

// List returns all engagements.
func (s *EngagementService) List(ctx context.Context) ([]domain.Engagement, error) {
	return s.engagements.List(ctx)
}

// ListForProject returns the engagements belonging to a project.
func (s *EngagementService) ListForProject(ctx context.Context, projectID string) ([]domain.Engagement, error) {
	return s.engagements.ListForProject(ctx, projectID)
}

// UpdateStatus transitions an engagement to a new status and records an audit
// event capturing the old and new values.
func (s *EngagementService) UpdateStatus(ctx context.Context, id string, newStatus domain.EngagementStatus, actor string) error {
	eng, err := s.engagements.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("update engagement status: %w", err)
	}
	old := eng.Status

	// Guard the transition: terminal states are sinks, and the Authorized state
	// is only reachable through the validated Authorize path.
	if err := eng.CanTransitionTo(newStatus); err != nil {
		return err
	}

	if err := s.engagements.UpdateStatus(ctx, id, newStatus); err != nil {
		return fmt.Errorf("update engagement status: %w", err)
	}

	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: id,
		Actor:        actor,
		Action:       "engagement.status",
		Target:       id,
		Detail:       fmt.Sprintf("old=%s new=%s", old, newStatus),
		At:           time.Now().UTC(),
	})
	return nil
}

// Authorize sets the authorization fields on an engagement and transitions it
// to the Authorized status. It records an engagement.authorize audit event.
func (s *EngagementService) Authorize(ctx context.Context, id string, auth domain.Authorization, actor string) (domain.Engagement, error) {
	eng, err := s.engagements.Get(ctx, id)
	if err != nil {
		return domain.Engagement{}, fmt.Errorf("authorize engagement: %w", err)
	}

	// Cannot authorize a completed/archived engagement back into a deployable state.
	if err := eng.CanAuthorize(); err != nil {
		return domain.Engagement{}, err
	}

	// Default the grant time to now when not supplied, then validate the
	// authorization is complete and its validity window is coherent and future.
	if auth.GrantedAt.IsZero() {
		auth.GrantedAt = time.Now().UTC()
	}
	if err := auth.Validate(time.Now()); err != nil {
		return domain.Engagement{}, err
	}

	eng.Authorization = auth
	eng.Status = domain.EngagementAuthorized

	if err := s.engagements.Update(ctx, eng); err != nil {
		return domain.Engagement{}, fmt.Errorf("authorize engagement: update: %w", err)
	}

	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: id,
		Actor:        actor,
		Action:       "engagement.authorize",
		Target:       id,
		Detail:       fmt.Sprintf("authorized_by=%s expires=%s", auth.AuthorizedBy, auth.ExpiresAt.UTC().Format(time.RFC3339)),
		At:           time.Now().UTC(),
	})

	return eng, nil
}
