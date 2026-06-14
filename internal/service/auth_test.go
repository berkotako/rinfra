package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/service"
	"github.com/rinfra/rinfra/internal/store/memstore"
)

func newAuth() (*service.AuthService, *memstore.UserStore) {
	users := memstore.NewUserStore()
	sessions := memstore.NewSessionStore()
	return service.NewAuthService(users, sessions, memstore.NewAuditLogger(), nil), users
}

func TestAuth_SeedLoginAuthenticate(t *testing.T) {
	ctx := context.Background()
	auth, _ := newAuth()

	u, err := auth.SeedAdmin(ctx)
	if err != nil || u == nil {
		t.Fatalf("seed admin: %v %v", err, u)
	}
	// Idempotent: second seed is a no-op.
	if u2, err := auth.SeedAdmin(ctx); err != nil || u2 != nil {
		t.Fatalf("second seed should be no-op: %v %v", err, u2)
	}

	// Wrong password rejected.
	if _, _, err := auth.Login(ctx, "admin", "wrong"); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("want ErrInvalidCredentials, got %v", err)
	}

	token, user, err := auth.Login(ctx, "admin", "admin")
	if err != nil || token == "" || user.Role != domain.RoleAdmin {
		t.Fatalf("login: %v token=%q role=%s", err, token, user.Role)
	}

	got, err := auth.Authenticate(ctx, token)
	if err != nil || got.Username != "admin" {
		t.Fatalf("authenticate: %v %+v", err, got)
	}

	// Logout invalidates the token.
	if err := auth.Logout(ctx, token); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := auth.Authenticate(ctx, token); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("want ErrInvalidCredentials after logout, got %v", err)
	}
}

func TestAuth_CreateUserAuthorization(t *testing.T) {
	ctx := context.Background()
	auth, _ := newAuth()
	admin := domain.User{ID: "admin-1", Username: "admin", Role: domain.RoleAdmin}

	lead, err := auth.CreateUser(ctx, admin, domain.User{Username: "lead1", Role: domain.RoleLead}, "pw1234")
	if err != nil {
		t.Fatalf("admin create lead: %v", err)
	}

	// Lead creates an operator; ManagerID is forced to the lead.
	op, err := auth.CreateUser(ctx, lead, domain.User{Username: "op1", Role: domain.RoleOperator}, "pw1234")
	if err != nil {
		t.Fatalf("lead create operator: %v", err)
	}
	if op.ManagerID != lead.ID {
		t.Fatalf("operator manager = %q, want %q", op.ManagerID, lead.ID)
	}

	// Lead may not create another lead.
	if _, err := auth.CreateUser(ctx, lead, domain.User{Username: "lead2", Role: domain.RoleLead}, "pw1234"); !errors.Is(err, service.ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}

	// Operator may not create anyone.
	if _, err := auth.CreateUser(ctx, op, domain.User{Username: "x", Role: domain.RoleOperator}, "pw1234"); !errors.Is(err, service.ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}

	// Duplicate username is rejected.
	if _, err := auth.CreateUser(ctx, admin, domain.User{Username: "op1", Role: domain.RoleOperator}, "pw1234"); !errors.Is(err, service.ErrUsernameTaken) {
		t.Fatalf("want ErrUsernameTaken, got %v", err)
	}

	// Missing password is a validation error.
	if _, err := auth.CreateUser(ctx, admin, domain.User{Username: "nopass", Role: domain.RoleOperator}, ""); !errors.Is(err, service.ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
}

func TestAuth_ListUsersScoping(t *testing.T) {
	ctx := context.Background()
	auth, _ := newAuth()
	admin := domain.User{ID: "admin-1", Username: "admin", Role: domain.RoleAdmin}
	lead, _ := auth.CreateUser(ctx, admin, domain.User{Username: "lead1", Role: domain.RoleLead}, "pw1234")
	op, _ := auth.CreateUser(ctx, lead, domain.User{Username: "op1", Role: domain.RoleOperator}, "pw1234")
	_, _ = auth.CreateUser(ctx, admin, domain.User{Username: "lead2", Role: domain.RoleLead}, "pw1234")

	adminList, _ := auth.ListUsers(ctx, admin)
	if len(adminList) != 3 {
		t.Fatalf("admin should see all 3 users, got %d", len(adminList))
	}
	leadList, _ := auth.ListUsers(ctx, lead)
	if len(leadList) != 2 { // self + op1
		t.Fatalf("lead should see 2 users, got %d", len(leadList))
	}
	opList, _ := auth.ListUsers(ctx, op)
	if len(opList) != 1 || opList[0].Username != "op1" {
		t.Fatalf("operator should see only self, got %+v", opList)
	}
}
