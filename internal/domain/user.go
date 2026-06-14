package domain

import "time"

// Role is a global operator role. Controls what a user may do across all projects.
type Role string

const (
	RoleAdmin    Role = "admin"
	RoleLead     Role = "lead"
	RoleOperator Role = "operator"
)

// Valid reports whether r is one of the three accepted role values.
func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleLead, RoleOperator:
		return true
	}
	return false
}

// User is an authenticated operator identity.
type User struct {
	ID           string
	Username     string
	DisplayName  string
	Email        string
	Role         Role
	PasswordHash string
	ManagerID    string // lead who owns this operator; empty if none
	Disabled     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
