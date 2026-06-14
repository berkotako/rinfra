package domain_test

import (
	"testing"

	"github.com/rinfra/rinfra/internal/domain"
)

func TestRole_Valid(t *testing.T) {
	tests := []struct {
		role  domain.Role
		valid bool
	}{
		{domain.RoleAdmin, true},
		{domain.RoleLead, true},
		{domain.RoleOperator, true},
		{"", false},
		{"superuser", false},
		{"ADMIN", false},
	}
	for _, tc := range tests {
		if got := tc.role.Valid(); got != tc.valid {
			t.Errorf("Role(%q).Valid() = %v, want %v", tc.role, got, tc.valid)
		}
	}
}
