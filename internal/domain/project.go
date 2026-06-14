package domain

import "time"

// Project groups one or more Engagements under a named client programme.
// A Project has a designated lead and an explicit membership list.
type Project struct {
	ID          string
	Name        string
	Description string
	ClientName  string
	LeadID      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ProjectMember associates a user with a project.
type ProjectMember struct {
	ProjectID string
	UserID    string
	AddedAt   time.Time
}
