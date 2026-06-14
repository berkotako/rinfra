package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rinfra/rinfra/internal/audit"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/store"
)

// ProjectService manages projects, their membership, and access checks used by
// engagement authorization.
type ProjectService struct {
	projects store.ProjectStore
	users    store.UserStore
	audit    audit.Logger
}

// NewProjectService returns a ProjectService.
func NewProjectService(projects store.ProjectStore, users store.UserStore, a audit.Logger) *ProjectService {
	return &ProjectService{projects: projects, users: users, audit: a}
}

// Create persists a new project. Admins and leads may create projects. For a
// lead, LeadID is forced to the creator. For an admin, LeadID defaults to the
// admin but may be set to a chosen lead.
func (s *ProjectService) Create(ctx context.Context, actor domain.User, p domain.Project) (domain.Project, error) {
	switch actor.Role {
	case domain.RoleAdmin:
		if p.LeadID == "" {
			p.LeadID = actor.ID
		}
	case domain.RoleLead:
		p.LeadID = actor.ID
	default:
		return domain.Project{}, fmt.Errorf("%w: operators may not create projects", ErrUnauthorized)
	}
	if p.Name == "" {
		return domain.Project{}, fmt.Errorf("%w: project name is required", ErrValidation)
	}

	id, err := s.projects.Create(ctx, p)
	if err != nil {
		return domain.Project{}, fmt.Errorf("create project: %w", err)
	}
	p.ID = id
	s.record(ctx, actor.Username, "project.create", id, fmt.Sprintf("name=%s lead=%s", p.Name, p.LeadID))
	return p, nil
}

// List returns all projects for an admin, or the projects a user leads or is a
// member of for everyone else.
func (s *ProjectService) List(ctx context.Context, actor domain.User) ([]domain.Project, error) {
	if actor.Role == domain.RoleAdmin {
		return s.projects.List(ctx)
	}
	return s.projects.ListForUser(ctx, actor.ID)
}

// Get returns a project if the actor may access it.
func (s *ProjectService) Get(ctx context.Context, actor domain.User, id string) (domain.Project, error) {
	p, err := s.projects.Get(ctx, id)
	if err != nil {
		return domain.Project{}, err
	}
	ok, err := s.CanAccessProject(ctx, actor, id)
	if err != nil {
		return domain.Project{}, err
	}
	if !ok {
		return domain.Project{}, fmt.Errorf("%w: cannot access project %s", ErrUnauthorized, id)
	}
	return p, nil
}

// Update mutates project fields. Only an admin or the project's lead may update.
func (s *ProjectService) Update(ctx context.Context, actor domain.User, id string, p domain.Project) (domain.Project, error) {
	existing, err := s.projects.Get(ctx, id)
	if err != nil {
		return domain.Project{}, err
	}
	if !s.isAdminOrLead(actor, existing) {
		return domain.Project{}, fmt.Errorf("%w: cannot update project %s", ErrUnauthorized, id)
	}
	existing.Name = p.Name
	existing.Description = p.Description
	existing.ClientName = p.ClientName
	// Only an admin may reassign the lead.
	if actor.Role == domain.RoleAdmin && p.LeadID != "" {
		existing.LeadID = p.LeadID
	}
	if err := s.projects.Update(ctx, existing); err != nil {
		return domain.Project{}, fmt.Errorf("update project: %w", err)
	}
	s.record(ctx, actor.Username, "project.update", id, "project updated")
	return existing, nil
}

// Delete removes a project. Only an admin or the project's lead may delete.
func (s *ProjectService) Delete(ctx context.Context, actor domain.User, id string) error {
	existing, err := s.projects.Get(ctx, id)
	if err != nil {
		return err
	}
	if !s.isAdminOrLead(actor, existing) {
		return fmt.Errorf("%w: cannot delete project %s", ErrUnauthorized, id)
	}
	if err := s.projects.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	s.record(ctx, actor.Username, "project.delete", id, "project deleted")
	return nil
}

// AddMember adds a user to a project. Only an admin or the project's lead may do
// so. The added user typically is an operator.
func (s *ProjectService) AddMember(ctx context.Context, actor domain.User, projectID, userID string) error {
	p, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return err
	}
	if !s.isAdminOrLead(actor, p) {
		return fmt.Errorf("%w: cannot manage members of project %s", ErrUnauthorized, projectID)
	}
	// Validate the user exists.
	if _, err := s.users.GetByID(ctx, userID); err != nil {
		return err
	}
	if err := s.projects.AddMember(ctx, projectID, userID); err != nil {
		return fmt.Errorf("add member: %w", err)
	}
	s.record(ctx, actor.Username, "project.member_add", projectID, fmt.Sprintf("user=%s", userID))
	return nil
}

// RemoveMember removes a user from a project. Only an admin or lead may do so.
func (s *ProjectService) RemoveMember(ctx context.Context, actor domain.User, projectID, userID string) error {
	p, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return err
	}
	if !s.isAdminOrLead(actor, p) {
		return fmt.Errorf("%w: cannot manage members of project %s", ErrUnauthorized, projectID)
	}
	if err := s.projects.RemoveMember(ctx, projectID, userID); err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	s.record(ctx, actor.Username, "project.member_remove", projectID, fmt.Sprintf("user=%s", userID))
	return nil
}

// ListMembers returns the membership of a project. An admin, the lead, or a
// member may list.
func (s *ProjectService) ListMembers(ctx context.Context, actor domain.User, projectID string) ([]domain.ProjectMember, error) {
	ok, err := s.CanAccessProject(ctx, actor, projectID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: cannot list members of project %s", ErrUnauthorized, projectID)
	}
	return s.projects.ListMembers(ctx, projectID)
}

// CanAccessProject reports whether the actor may access the project: admin, the
// owning lead, or a member.
func (s *ProjectService) CanAccessProject(ctx context.Context, actor domain.User, projectID string) (bool, error) {
	if actor.Role == domain.RoleAdmin {
		return true, nil
	}
	p, err := s.projects.Get(ctx, projectID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("can access project: %w", err)
	}
	if p.LeadID == actor.ID {
		return true, nil
	}
	member, err := s.projects.IsMember(ctx, projectID, actor.ID)
	if err != nil {
		return false, fmt.Errorf("can access project: %w", err)
	}
	return member, nil
}

// isAdminOrLead reports whether the actor is an admin or the project's lead.
func (s *ProjectService) isAdminOrLead(actor domain.User, p domain.Project) bool {
	return actor.Role == domain.RoleAdmin || p.LeadID == actor.ID
}

func (s *ProjectService) record(ctx context.Context, actor, action, target, detail string) {
	if s.audit == nil {
		return
	}
	_ = s.audit.Record(ctx, audit.Event{
		Actor:  actor,
		Action: action,
		Target: target,
		Detail: detail,
		At:     time.Now().UTC(),
	})
}
