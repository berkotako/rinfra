package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/service"
	"github.com/rinfra/rinfra/internal/store/memstore"
)

func TestProjectService_Authorization(t *testing.T) {
	ctx := context.Background()
	users := memstore.NewUserStore()
	projects := memstore.NewProjectStore()
	ps := service.NewProjectService(projects, users, memstore.NewAuditLogger())

	admin := domain.User{ID: "admin-1", Username: "admin", Role: domain.RoleAdmin}
	lead := domain.User{ID: "lead-1", Username: "lead", Role: domain.RoleLead}
	otherLead := domain.User{ID: "lead-2", Username: "lead2", Role: domain.RoleLead}
	op := domain.User{ID: "op-1", Username: "op", Role: domain.RoleOperator}
	// The operator must exist for AddMember validation.
	if _, err := users.Create(ctx, op); err != nil {
		t.Fatalf("seed op: %v", err)
	}

	// Operators cannot create projects.
	if _, err := ps.Create(ctx, op, domain.Project{Name: "X"}); !errors.Is(err, service.ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}

	// Lead creates a project; LeadID is forced to the creator.
	proj, err := ps.Create(ctx, lead, domain.Project{Name: "Apollo"})
	if err != nil || proj.LeadID != lead.ID {
		t.Fatalf("lead create: %v lead=%s", err, proj.LeadID)
	}

	// Access: admin and owning lead yes; operator no (yet); other lead no.
	mustAccess := func(u domain.User, want bool) {
		ok, err := ps.CanAccessProject(ctx, u, proj.ID)
		if err != nil {
			t.Fatalf("can access (%s): %v", u.Username, err)
		}
		if ok != want {
			t.Fatalf("access for %s = %v, want %v", u.Username, ok, want)
		}
	}
	mustAccess(admin, true)
	mustAccess(lead, true)
	mustAccess(op, false)
	mustAccess(otherLead, false)

	// A non-owning lead may not add members.
	if err := ps.AddMember(ctx, otherLead, proj.ID, op.ID); !errors.Is(err, service.ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized adding member, got %v", err)
	}

	// Owning lead adds the operator; access now granted.
	if err := ps.AddMember(ctx, lead, proj.ID, op.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	mustAccess(op, true)

	// Operator sees the project in their list; other lead sees none.
	opProjects, _ := ps.List(ctx, op)
	if len(opProjects) != 1 {
		t.Fatalf("operator should see 1 project, got %d", len(opProjects))
	}
	otherProjects, _ := ps.List(ctx, otherLead)
	if len(otherProjects) != 0 {
		t.Fatalf("other lead should see 0 projects, got %d", len(otherProjects))
	}

	// Operator cannot delete; owning lead can.
	if err := ps.Delete(ctx, op, proj.ID); !errors.Is(err, service.ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized on delete, got %v", err)
	}
	if err := ps.Delete(ctx, lead, proj.ID); err != nil {
		t.Fatalf("lead delete: %v", err)
	}
}
