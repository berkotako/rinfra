package memstore

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/store"
)

// --- UserStore ---

// UserStore is the in-memory implementation of store.UserStore.
type UserStore struct {
	mu   sync.RWMutex
	rows map[string]domain.User
}

// NewUserStore returns an empty UserStore.
func NewUserStore() *UserStore { return &UserStore{rows: make(map[string]domain.User)} }

var _ store.UserStore = (*UserStore)(nil)

// Create inserts u, generating a UUID if u.ID is empty, and returns the ID.
// It rejects a duplicate username.
func (s *UserStore) Create(_ context.Context, u domain.User) (string, error) {
	if u.ID == "" {
		u.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	u.CreatedAt = now
	u.UpdatedAt = now

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ex := range s.rows {
		if ex.Username == u.Username {
			return "", fmt.Errorf("username %q already exists", u.Username)
		}
	}
	s.rows[u.ID] = u
	return u.ID, nil
}

// GetByID returns the user with the given ID or ErrNotFound.
func (s *UserStore) GetByID(_ context.Context, id string) (domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.rows[id]
	if !ok {
		return domain.User{}, fmt.Errorf("user %s: %w", id, store.ErrNotFound)
	}
	return u, nil
}

// GetByUsername returns the user with the given username or ErrNotFound.
func (s *UserStore) GetByUsername(_ context.Context, username string) (domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.rows {
		if u.Username == username {
			return u, nil
		}
	}
	return domain.User{}, fmt.Errorf("user %q: %w", username, store.ErrNotFound)
}

// List returns all users.
func (s *UserStore) List(_ context.Context) ([]domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.User, 0, len(s.rows))
	for _, u := range s.rows {
		out = append(out, u)
	}
	return out, nil
}

// ListByManager returns the users whose ManagerID matches managerID.
func (s *UserStore) ListByManager(_ context.Context, managerID string) ([]domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.User, 0)
	for _, u := range s.rows {
		if u.ManagerID == managerID {
			out = append(out, u)
		}
	}
	return out, nil
}

// Update replaces the mutable fields of an existing user.
func (s *UserStore) Update(_ context.Context, u domain.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.rows[u.ID]
	if !ok {
		return fmt.Errorf("user %s: %w", u.ID, store.ErrNotFound)
	}
	// Preserve immutable/managed fields.
	u.PasswordHash = existing.PasswordHash
	u.Username = existing.Username
	u.CreatedAt = existing.CreatedAt
	u.UpdatedAt = time.Now().UTC()
	s.rows[u.ID] = u
	return nil
}

// SetPassword updates only the password hash.
func (s *UserStore) SetPassword(_ context.Context, id, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.rows[id]
	if !ok {
		return fmt.Errorf("user %s: %w", id, store.ErrNotFound)
	}
	u.PasswordHash = passwordHash
	u.UpdatedAt = time.Now().UTC()
	s.rows[id] = u
	return nil
}

// CountAll returns the number of users.
func (s *UserStore) CountAll(_ context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.rows), nil
}

// --- ProjectStore ---

// ProjectStore is the in-memory implementation of store.ProjectStore.
type ProjectStore struct {
	mu      sync.RWMutex
	rows    map[string]domain.Project
	members map[string]map[string]time.Time // projectID -> userID -> addedAt
}

// NewProjectStore returns an empty ProjectStore.
func NewProjectStore() *ProjectStore {
	return &ProjectStore{
		rows:    make(map[string]domain.Project),
		members: make(map[string]map[string]time.Time),
	}
}

var _ store.ProjectStore = (*ProjectStore)(nil)

// Create inserts p, generating a UUID if empty, and returns the ID.
func (s *ProjectStore) Create(_ context.Context, p domain.Project) (string, error) {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	p.CreatedAt = now
	p.UpdatedAt = now
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[p.ID] = p
	return p.ID, nil
}

// Get returns the project with the given ID or ErrNotFound.
func (s *ProjectStore) Get(_ context.Context, id string) (domain.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.rows[id]
	if !ok {
		return domain.Project{}, fmt.Errorf("project %s: %w", id, store.ErrNotFound)
	}
	return p, nil
}

// List returns all projects.
func (s *ProjectStore) List(_ context.Context) ([]domain.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Project, 0, len(s.rows))
	for _, p := range s.rows {
		out = append(out, p)
	}
	return out, nil
}

// ListForUser returns the projects the user leads or is a member of.
func (s *ProjectStore) ListForUser(_ context.Context, userID string) ([]domain.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Project, 0)
	for id, p := range s.rows {
		if p.LeadID == userID {
			out = append(out, p)
			continue
		}
		if m, ok := s.members[id]; ok {
			if _, isMember := m[userID]; isMember {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

// Update replaces the mutable fields of an existing project.
func (s *ProjectStore) Update(_ context.Context, p domain.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.rows[p.ID]
	if !ok {
		return fmt.Errorf("project %s: %w", p.ID, store.ErrNotFound)
	}
	p.CreatedAt = existing.CreatedAt
	p.UpdatedAt = time.Now().UTC()
	s.rows[p.ID] = p
	return nil
}

// Delete removes a project and its membership.
func (s *ProjectStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[id]; !ok {
		return fmt.Errorf("project %s: %w", id, store.ErrNotFound)
	}
	delete(s.rows, id)
	delete(s.members, id)
	return nil
}

// AddMember adds userID to projectID (idempotent).
func (s *ProjectStore) AddMember(_ context.Context, projectID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[projectID]; !ok {
		return fmt.Errorf("project %s: %w", projectID, store.ErrNotFound)
	}
	if s.members[projectID] == nil {
		s.members[projectID] = make(map[string]time.Time)
	}
	if _, exists := s.members[projectID][userID]; !exists {
		s.members[projectID][userID] = time.Now().UTC()
	}
	return nil
}

// RemoveMember removes userID from projectID (idempotent).
func (s *ProjectStore) RemoveMember(_ context.Context, projectID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.members[projectID]; ok {
		delete(m, userID)
	}
	return nil
}

// ListMembers returns the members of a project ordered by user ID.
func (s *ProjectStore) ListMembers(_ context.Context, projectID string) ([]domain.ProjectMember, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.ProjectMember, 0)
	for userID, addedAt := range s.members[projectID] {
		out = append(out, domain.ProjectMember{ProjectID: projectID, UserID: userID, AddedAt: addedAt})
	}
	return out, nil
}

// IsMember reports whether userID is a member of projectID.
func (s *ProjectStore) IsMember(_ context.Context, projectID, userID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if m, ok := s.members[projectID]; ok {
		_, isMember := m[userID]
		return isMember, nil
	}
	return false, nil
}

// --- SessionStore ---

// SessionStore is the in-memory implementation of store.SessionStore.
type SessionStore struct {
	mu   sync.RWMutex
	rows map[string]domain.Session // keyed by TokenHash
}

// NewSessionStore returns an empty SessionStore.
func NewSessionStore() *SessionStore { return &SessionStore{rows: make(map[string]domain.Session)} }

var _ store.SessionStore = (*SessionStore)(nil)

// Create stores a session keyed by its token hash.
func (s *SessionStore) Create(_ context.Context, sess domain.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[sess.TokenHash] = sess
	return nil
}

// GetByTokenHash returns the session for a token hash or ErrNotFound.
func (s *SessionStore) GetByTokenHash(_ context.Context, tokenHash string) (domain.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.rows[tokenHash]
	if !ok {
		return domain.Session{}, fmt.Errorf("session: %w", store.ErrNotFound)
	}
	return sess, nil
}

// Delete removes a session by token hash (idempotent).
func (s *SessionStore) Delete(_ context.Context, tokenHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, tokenHash)
	return nil
}

// DeleteForUser removes all sessions belonging to userID.
func (s *SessionStore) DeleteForUser(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for h, sess := range s.rows {
		if sess.UserID == userID {
			delete(s.rows, h)
		}
	}
	return nil
}
