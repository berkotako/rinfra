package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/store"
)

// nullString converts an empty string to nil for nullable columns.
func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// --- UserStore ---

// UserStore is the Postgres implementation of store.UserStore.
type UserStore struct {
	pool *pgxpool.Pool
}

// NewUserStore returns a UserStore backed by pool.
func NewUserStore(pool *pgxpool.Pool) *UserStore { return &UserStore{pool: pool} }

var _ store.UserStore = (*UserStore)(nil)

// Create inserts a new user and returns its generated UUID.
func (s *UserStore) Create(ctx context.Context, u domain.User) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (username, display_name, email, role, password_hash, manager_id, disabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id`,
		u.Username, u.DisplayName, u.Email, string(u.Role), u.PasswordHash,
		nullString(u.ManagerID), u.Disabled,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create user: %w", err)
	}
	return id, nil
}

func scanUser(row rowScanner) (domain.User, error) {
	var u domain.User
	var role string
	var managerID *string
	if err := row.Scan(
		&u.ID, &u.Username, &u.DisplayName, &u.Email, &role,
		&u.PasswordHash, &managerID, &u.Disabled, &u.CreatedAt, &u.UpdatedAt,
	); err != nil {
		return domain.User{}, err
	}
	u.Role = domain.Role(role)
	if managerID != nil {
		u.ManagerID = *managerID
	}
	return u, nil
}

const userColumns = `id, username, display_name, email, role, password_hash, manager_id, disabled, created_at, updated_at`

// GetByID fetches a user by ID.
func (s *UserStore) GetByID(ctx context.Context, id string) (domain.User, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1`, id)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, fmt.Errorf("user %s: %w", id, store.ErrNotFound)
		}
		return domain.User{}, fmt.Errorf("get user %s: %w", id, err)
	}
	return u, nil
}

// GetByUsername fetches a user by username.
func (s *UserStore) GetByUsername(ctx context.Context, username string) (domain.User, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE username=$1`, username)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, fmt.Errorf("user %q: %w", username, store.ErrNotFound)
		}
		return domain.User{}, fmt.Errorf("get user %q: %w", username, err)
	}
	return u, nil
}

// List returns all users ordered by username.
func (s *UserStore) List(ctx context.Context) ([]domain.User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userColumns+` FROM users ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	return collectUsers(rows)
}

// ListByManager returns users managed by managerID.
func (s *UserStore) ListByManager(ctx context.Context, managerID string) ([]domain.User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userColumns+` FROM users WHERE manager_id=$1 ORDER BY username`, managerID)
	if err != nil {
		return nil, fmt.Errorf("list users by manager: %w", err)
	}
	defer rows.Close()
	return collectUsers(rows)
}

func collectUsers(rows pgx.Rows) ([]domain.User, error) {
	var out []domain.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Update replaces the mutable fields of a user (not username or password).
func (s *UserStore) Update(ctx context.Context, u domain.User) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE users SET display_name=$1, email=$2, role=$3, manager_id=$4, disabled=$5, updated_at=now()
		WHERE id=$6`,
		u.DisplayName, u.Email, string(u.Role), nullString(u.ManagerID), u.Disabled, u.ID)
	if err != nil {
		return fmt.Errorf("update user %s: %w", u.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user %s: %w", u.ID, store.ErrNotFound)
	}
	return nil
}

// SetPassword updates only the password hash.
func (s *UserStore) SetPassword(ctx context.Context, id, passwordHash string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET password_hash=$1, updated_at=now() WHERE id=$2`, passwordHash, id)
	if err != nil {
		return fmt.Errorf("set password for user %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user %s: %w", id, store.ErrNotFound)
	}
	return nil
}

// CountAll returns the number of users.
func (s *UserStore) CountAll(ctx context.Context) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// --- ProjectStore ---

// ProjectStore is the Postgres implementation of store.ProjectStore.
type ProjectStore struct {
	pool *pgxpool.Pool
}

// NewProjectStore returns a ProjectStore backed by pool.
func NewProjectStore(pool *pgxpool.Pool) *ProjectStore { return &ProjectStore{pool: pool} }

var _ store.ProjectStore = (*ProjectStore)(nil)

const projectColumns = `id, name, description, client_name, lead_id, created_at, updated_at`

func scanProject(row rowScanner) (domain.Project, error) {
	var p domain.Project
	if err := row.Scan(&p.ID, &p.Name, &p.Description, &p.ClientName, &p.LeadID, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return domain.Project{}, err
	}
	return p, nil
}

// Create inserts a new project and returns its generated UUID.
func (s *ProjectStore) Create(ctx context.Context, p domain.Project) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO projects (name, description, client_name, lead_id)
		VALUES ($1,$2,$3,$4) RETURNING id`,
		p.Name, p.Description, p.ClientName, p.LeadID).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create project: %w", err)
	}
	return id, nil
}

// Get fetches a project by ID.
func (s *ProjectStore) Get(ctx context.Context, id string) (domain.Project, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+projectColumns+` FROM projects WHERE id=$1`, id)
	p, err := scanProject(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Project{}, fmt.Errorf("project %s: %w", id, store.ErrNotFound)
		}
		return domain.Project{}, fmt.Errorf("get project %s: %w", id, err)
	}
	return p, nil
}

// List returns all projects ordered by created_at descending.
func (s *ProjectStore) List(ctx context.Context) ([]domain.Project, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+projectColumns+` FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	return collectProjects(rows)
}

// ListForUser returns projects the user leads or is a member of.
func (s *ProjectStore) ListForUser(ctx context.Context, userID string) ([]domain.Project, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT `+projectColumns+`
		FROM projects p
		LEFT JOIN project_members m ON m.project_id = p.id
		WHERE p.lead_id = $1 OR m.user_id = $1
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list projects for user: %w", err)
	}
	defer rows.Close()
	return collectProjects(rows)
}

func collectProjects(rows pgx.Rows) ([]domain.Project, error) {
	var out []domain.Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Update replaces the mutable fields of a project.
func (s *ProjectStore) Update(ctx context.Context, p domain.Project) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE projects SET name=$1, description=$2, client_name=$3, lead_id=$4, updated_at=now()
		WHERE id=$5`,
		p.Name, p.Description, p.ClientName, p.LeadID, p.ID)
	if err != nil {
		return fmt.Errorf("update project %s: %w", p.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("project %s: %w", p.ID, store.ErrNotFound)
	}
	return nil
}

// Delete removes a project (members cascade via FK).
func (s *ProjectStore) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM projects WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete project %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("project %s: %w", id, store.ErrNotFound)
	}
	return nil
}

// AddMember adds userID to projectID (idempotent).
func (s *ProjectStore) AddMember(ctx context.Context, projectID, userID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO project_members (project_id, user_id) VALUES ($1,$2)
		ON CONFLICT (project_id, user_id) DO NOTHING`, projectID, userID)
	if err != nil {
		return fmt.Errorf("add member: %w", err)
	}
	return nil
}

// RemoveMember removes userID from projectID (idempotent).
func (s *ProjectStore) RemoveMember(ctx context.Context, projectID, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, userID)
	if err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	return nil
}

// ListMembers returns the membership of a project ordered by added_at.
func (s *ProjectStore) ListMembers(ctx context.Context, projectID string) ([]domain.ProjectMember, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT project_id, user_id, added_at FROM project_members WHERE project_id=$1 ORDER BY added_at`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer rows.Close()
	var out []domain.ProjectMember
	for rows.Next() {
		var m domain.ProjectMember
		if err := rows.Scan(&m.ProjectID, &m.UserID, &m.AddedAt); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// IsMember reports whether userID is a member of projectID.
func (s *ProjectStore) IsMember(ctx context.Context, projectID, userID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM project_members WHERE project_id=$1 AND user_id=$2)`,
		projectID, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("is member: %w", err)
	}
	return exists, nil
}

// --- SessionStore ---

// SessionStore is the Postgres implementation of store.SessionStore.
type SessionStore struct {
	pool *pgxpool.Pool
}

// NewSessionStore returns a SessionStore backed by pool.
func NewSessionStore(pool *pgxpool.Pool) *SessionStore { return &SessionStore{pool: pool} }

var _ store.SessionStore = (*SessionStore)(nil)

// Create stores a session.
func (s *SessionStore) Create(ctx context.Context, sess domain.Session) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sessions (token_hash, user_id, created_at, expires_at)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (token_hash) DO UPDATE SET expires_at=EXCLUDED.expires_at`,
		sess.TokenHash, sess.UserID, sess.CreatedAt, sess.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// GetByTokenHash fetches a session by token hash.
func (s *SessionStore) GetByTokenHash(ctx context.Context, tokenHash string) (domain.Session, error) {
	var sess domain.Session
	err := s.pool.QueryRow(ctx,
		`SELECT token_hash, user_id, created_at, expires_at FROM sessions WHERE token_hash=$1`, tokenHash).
		Scan(&sess.TokenHash, &sess.UserID, &sess.CreatedAt, &sess.ExpiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Session{}, fmt.Errorf("session: %w", store.ErrNotFound)
		}
		return domain.Session{}, fmt.Errorf("get session: %w", err)
	}
	return sess, nil
}

// Delete removes a session by token hash.
func (s *SessionStore) Delete(ctx context.Context, tokenHash string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteForUser removes all sessions belonging to userID.
func (s *SessionStore) DeleteForUser(ctx context.Context, userID string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, userID); err != nil {
		return fmt.Errorf("delete sessions for user: %w", err)
	}
	return nil
}
