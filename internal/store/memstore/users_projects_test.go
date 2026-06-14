package memstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/store"
	"github.com/rinfra/rinfra/internal/store/memstore"
)

func TestUserStore_Roundtrip(t *testing.T) {
	ctx := context.Background()
	us := memstore.NewUserStore()

	id, err := us.Create(ctx, domain.User{Username: "alice", Role: domain.RoleLead, PasswordHash: "h"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Duplicate username rejected.
	if _, err := us.Create(ctx, domain.User{Username: "alice", Role: domain.RoleOperator}); err == nil {
		t.Fatal("expected duplicate username error")
	}

	got, err := us.GetByID(ctx, id)
	if err != nil || got.Username != "alice" {
		t.Fatalf("get by id: %v %+v", err, got)
	}
	byName, err := us.GetByUsername(ctx, "alice")
	if err != nil || byName.ID != id {
		t.Fatalf("get by username: %v %+v", err, byName)
	}

	// ManagerID scoping.
	if _, err := us.Create(ctx, domain.User{Username: "op1", Role: domain.RoleOperator, ManagerID: id}); err != nil {
		t.Fatalf("create op: %v", err)
	}
	ops, err := us.ListByManager(ctx, id)
	if err != nil || len(ops) != 1 || ops[0].Username != "op1" {
		t.Fatalf("list by manager: %v %+v", err, ops)
	}

	// SetPassword does not change other fields.
	if err := us.SetPassword(ctx, id, "newhash"); err != nil {
		t.Fatalf("set password: %v", err)
	}
	got, _ = us.GetByID(ctx, id)
	if got.PasswordHash != "newhash" {
		t.Fatalf("password not updated: %q", got.PasswordHash)
	}

	n, err := us.CountAll(ctx)
	if err != nil || n != 2 {
		t.Fatalf("count: %v %d", err, n)
	}

	if _, err := us.GetByID(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestProjectStore_MembershipAndListForUser(t *testing.T) {
	ctx := context.Background()
	ps := memstore.NewProjectStore()

	pid, err := ps.Create(ctx, domain.Project{Name: "Apollo", LeadID: "lead-1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Membership.
	if err := ps.AddMember(ctx, pid, "op-1"); err != nil {
		t.Fatalf("add member: %v", err)
	}
	// Idempotent.
	if err := ps.AddMember(ctx, pid, "op-1"); err != nil {
		t.Fatalf("add member twice: %v", err)
	}
	isMember, err := ps.IsMember(ctx, pid, "op-1")
	if err != nil || !isMember {
		t.Fatalf("is member: %v %v", err, isMember)
	}

	// ListForUser returns by lead and by membership.
	forLead, _ := ps.ListForUser(ctx, "lead-1")
	if len(forLead) != 1 {
		t.Fatalf("lead should see 1 project, got %d", len(forLead))
	}
	forOp, _ := ps.ListForUser(ctx, "op-1")
	if len(forOp) != 1 {
		t.Fatalf("member should see 1 project, got %d", len(forOp))
	}
	forStranger, _ := ps.ListForUser(ctx, "nobody")
	if len(forStranger) != 0 {
		t.Fatalf("stranger should see 0 projects, got %d", len(forStranger))
	}

	if err := ps.RemoveMember(ctx, pid, "op-1"); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	isMember, _ = ps.IsMember(ctx, pid, "op-1")
	if isMember {
		t.Fatal("op-1 should no longer be a member")
	}

	if err := ps.Delete(ctx, pid); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := ps.Get(ctx, pid); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestSessionStore_Roundtrip(t *testing.T) {
	ctx := context.Background()
	ss := memstore.NewSessionStore()

	sess := domain.Session{TokenHash: "abc", UserID: "u1", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	if err := ss.Create(ctx, sess); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := ss.GetByTokenHash(ctx, "abc")
	if err != nil || got.UserID != "u1" {
		t.Fatalf("get: %v %+v", err, got)
	}
	if err := ss.DeleteForUser(ctx, "u1"); err != nil {
		t.Fatalf("delete for user: %v", err)
	}
	if _, err := ss.GetByTokenHash(ctx, "abc"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
