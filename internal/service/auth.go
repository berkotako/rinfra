package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/rinfra/rinfra/internal/audit"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/store"
)

// Sentinel errors returned by the service layer. The API layer maps these to
// HTTP status codes (401/403/409).
var (
	// ErrUnauthorized indicates the actor lacks permission for the action.
	ErrUnauthorized = errors.New("not authorized to perform this action")
	// ErrInvalidCredentials is returned on a failed login; it never reveals
	// whether the username or the password was wrong.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrUsernameTaken is returned when creating a user with a duplicate username.
	ErrUsernameTaken = errors.New("username already taken")
	// ErrValidation indicates a malformed request (missing/invalid field). The
	// API layer maps it to HTTP 400.
	ErrValidation = errors.New("validation error")
)

// DefaultSessionTTL is how long a login session remains valid.
const DefaultSessionTTL = 7 * 24 * time.Hour

// AuthService handles authentication (login/logout/session validation) and user
// administration with role-based authorization.
type AuthService struct {
	users    store.UserStore
	sessions store.SessionStore
	audit    audit.Logger
	log      *slog.Logger
	ttl      time.Duration
}

// NewAuthService returns an AuthService. If log is nil a no-op logger is used.
func NewAuthService(users store.UserStore, sessions store.SessionStore, a audit.Logger, log *slog.Logger) *AuthService {
	if log == nil {
		log = slog.New(slog.NewTextHandler(noopWriter{}, nil))
	}
	return &AuthService{users: users, sessions: sessions, audit: a, log: log, ttl: DefaultSessionTTL}
}

// noopWriter discards log output; used when a nil logger is supplied.
type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }

// hashToken returns the hex-encoded SHA-256 of a raw token.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// newToken generates a 32-byte random opaque token, base64url-encoded.
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Login validates credentials and returns a freshly minted opaque token plus
// the authenticated user. Disabled users are rejected. On any failure it
// returns ErrInvalidCredentials without revealing which field was wrong.
func (s *AuthService) Login(ctx context.Context, username, password string) (string, domain.User, error) {
	u, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", domain.User{}, ErrInvalidCredentials
		}
		return "", domain.User{}, fmt.Errorf("login: %w", err)
	}
	if u.Disabled {
		return "", domain.User{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return "", domain.User{}, ErrInvalidCredentials
	}

	raw, err := newToken()
	if err != nil {
		return "", domain.User{}, err
	}
	now := time.Now().UTC()
	sess := domain.Session{
		TokenHash: hashToken(raw),
		UserID:    u.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return "", domain.User{}, fmt.Errorf("login: create session: %w", err)
	}

	s.record(ctx, u.Username, "auth.login", u.ID, "session created")
	return raw, u, nil
}

// Logout invalidates the session associated with the given raw token. A missing
// session is not an error (idempotent).
func (s *AuthService) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	if err := s.sessions.Delete(ctx, hashToken(token)); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}

// Authenticate resolves a raw bearer token to its user. It rejects unknown,
// expired tokens and disabled users.
func (s *AuthService) Authenticate(ctx context.Context, token string) (domain.User, error) {
	if token == "" {
		return domain.User{}, ErrInvalidCredentials
	}
	sess, err := s.sessions.GetByTokenHash(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return domain.User{}, ErrInvalidCredentials
		}
		return domain.User{}, fmt.Errorf("authenticate: %w", err)
	}
	if time.Now().UTC().After(sess.ExpiresAt) {
		// Best-effort cleanup of the expired session.
		_ = s.sessions.Delete(ctx, sess.TokenHash)
		return domain.User{}, ErrInvalidCredentials
	}
	u, err := s.users.GetByID(ctx, sess.UserID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return domain.User{}, ErrInvalidCredentials
		}
		return domain.User{}, fmt.Errorf("authenticate: %w", err)
	}
	if u.Disabled {
		return domain.User{}, ErrInvalidCredentials
	}
	return u, nil
}

// CreateUser creates a new user subject to role-based authorization:
//   - admin may create any role.
//   - lead may create only operators; the new operator's ManagerID is set to
//     the lead's ID.
//   - operator may not create users.
func (s *AuthService) CreateUser(ctx context.Context, actor, newUser domain.User, plaintextPassword string) (domain.User, error) {
	if !newUser.Role.Valid() {
		return domain.User{}, fmt.Errorf("%w: invalid role %q", ErrValidation, newUser.Role)
	}
	if strings.TrimSpace(newUser.Username) == "" {
		return domain.User{}, fmt.Errorf("%w: username is required", ErrValidation)
	}
	if plaintextPassword == "" {
		return domain.User{}, fmt.Errorf("%w: password is required", ErrValidation)
	}

	switch actor.Role {
	case domain.RoleAdmin:
		// admin may create any role as specified.
	case domain.RoleLead:
		if newUser.Role != domain.RoleOperator {
			return domain.User{}, fmt.Errorf("%w: a lead may only create operators", ErrUnauthorized)
		}
		newUser.ManagerID = actor.ID
	default:
		return domain.User{}, fmt.Errorf("%w: operators may not create users", ErrUnauthorized)
	}

	// Uniqueness check (defence in depth; the store also enforces it).
	if _, err := s.users.GetByUsername(ctx, newUser.Username); err == nil {
		return domain.User{}, ErrUsernameTaken
	} else if !errors.Is(err, store.ErrNotFound) {
		return domain.User{}, fmt.Errorf("create user: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(plaintextPassword), bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, fmt.Errorf("hash password: %w", err)
	}
	newUser.PasswordHash = string(hash)

	id, err := s.users.Create(ctx, newUser)
	if err != nil {
		return domain.User{}, fmt.Errorf("create user: %w", err)
	}
	newUser.ID = id

	s.record(ctx, actor.Username, "user.create", id, fmt.Sprintf("username=%s role=%s", newUser.Username, newUser.Role))
	return newUser, nil
}

// ListUsers returns users visible to the actor:
//   - admin: all users.
//   - lead: self plus the operators they manage.
//   - operator: just self.
func (s *AuthService) ListUsers(ctx context.Context, actor domain.User) ([]domain.User, error) {
	switch actor.Role {
	case domain.RoleAdmin:
		return s.users.List(ctx)
	case domain.RoleLead:
		ops, err := s.users.ListByManager(ctx, actor.ID)
		if err != nil {
			return nil, fmt.Errorf("list users: %w", err)
		}
		return append([]domain.User{actor}, ops...), nil
	default:
		return []domain.User{actor}, nil
	}
}

// GetUser returns a single user if the actor is allowed to see it.
func (s *AuthService) GetUser(ctx context.Context, actor domain.User, id string) (domain.User, error) {
	u, err := s.users.GetByID(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	switch actor.Role {
	case domain.RoleAdmin:
		return u, nil
	case domain.RoleLead:
		if u.ID == actor.ID || u.ManagerID == actor.ID {
			return u, nil
		}
	default:
		if u.ID == actor.ID {
			return u, nil
		}
	}
	return domain.User{}, fmt.Errorf("%w: cannot view user %s", ErrUnauthorized, id)
}

// UserUpdate carries the mutable fields a caller may change. A nil pointer means
// "leave unchanged".
type UserUpdate struct {
	DisplayName *string
	Email       *string
	Role        *domain.Role
	Disabled    *bool
	ManagerID   *string
}

// UpdateUser applies a partial update with role-based authorization:
//   - admin may change role/disabled/displayName/email/manager of anyone.
//   - lead may update displayName/email/disabled of their own operators.
//   - any user may update their own displayName/email.
func (s *AuthService) UpdateUser(ctx context.Context, actor domain.User, id string, upd UserUpdate) (domain.User, error) {
	u, err := s.users.GetByID(ctx, id)
	if err != nil {
		return domain.User{}, err
	}

	isAdmin := actor.Role == domain.RoleAdmin
	isOwnLead := actor.Role == domain.RoleLead && u.ManagerID == actor.ID
	isSelf := actor.ID == id

	if !isAdmin && !isOwnLead && !isSelf {
		return domain.User{}, fmt.Errorf("%w: cannot update user %s", ErrUnauthorized, id)
	}

	// Privileged fields (role, manager) only the admin may change.
	if upd.Role != nil {
		if !isAdmin {
			return domain.User{}, fmt.Errorf("%w: only an admin may change roles", ErrUnauthorized)
		}
		if !upd.Role.Valid() {
			return domain.User{}, fmt.Errorf("%w: invalid role %q", ErrValidation, *upd.Role)
		}
		u.Role = *upd.Role
	}
	if upd.ManagerID != nil {
		if !isAdmin {
			return domain.User{}, fmt.Errorf("%w: only an admin may change a user's manager", ErrUnauthorized)
		}
		u.ManagerID = *upd.ManagerID
	}
	if upd.Disabled != nil {
		if !isAdmin && !isOwnLead {
			return domain.User{}, fmt.Errorf("%w: cannot change disabled state", ErrUnauthorized)
		}
		u.Disabled = *upd.Disabled
	}
	if upd.DisplayName != nil {
		u.DisplayName = *upd.DisplayName
	}
	if upd.Email != nil {
		u.Email = *upd.Email
	}

	if err := s.users.Update(ctx, u); err != nil {
		return domain.User{}, fmt.Errorf("update user: %w", err)
	}
	s.record(ctx, actor.Username, "user.update", id, "user updated")
	return u, nil
}

// ChangePassword sets a new password for targetID. A user may always change
// their own password; an admin may change anyone's; a lead may change the
// password of operators they manage. When a user changes their OWN password,
// currentPassword must be supplied and verified against the stored hash
// (privileged resets by an admin/lead do not require it). The new password is
// hashed with bcrypt before storage.
func (s *AuthService) ChangePassword(ctx context.Context, actor domain.User, targetID, currentPassword, newPassword string) error {
	if newPassword == "" {
		return fmt.Errorf("%w: password is required", ErrValidation)
	}
	target, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return err
	}

	isSelf := actor.ID == targetID
	allowed := isSelf ||
		actor.Role == domain.RoleAdmin ||
		(actor.Role == domain.RoleLead && target.ManagerID == actor.ID)
	if !allowed {
		return fmt.Errorf("%w: cannot change password for user %s", ErrUnauthorized, targetID)
	}

	// Self-service change requires confirming the current password.
	if isSelf {
		if currentPassword == "" {
			return fmt.Errorf("%w: current password is required", ErrValidation)
		}
		if bcrypt.CompareHashAndPassword([]byte(target.PasswordHash), []byte(currentPassword)) != nil {
			return fmt.Errorf("%w: current password is incorrect", ErrUnauthorized)
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := s.users.SetPassword(ctx, targetID, string(hash)); err != nil {
		return fmt.Errorf("change password: %w", err)
	}
	s.record(ctx, actor.Username, "user.password", targetID, "password changed")
	return nil
}

// SeedAdmin creates a bootstrap admin (username "admin") with the given password
// when no users exist yet. It returns the created user, or (nil, nil) when users
// already exist. The caller decides the password — production must NOT use a
// known default (see cmd/rinfra-server: dev seeds "admin", production requires
// RINFRA_ADMIN_PASSWORD).
func (s *AuthService) SeedAdmin(ctx context.Context, password string) (*domain.User, error) {
	if password == "" {
		return nil, fmt.Errorf("seed admin: password must not be empty")
	}
	n, err := s.users.CountAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("seed admin: count users: %w", err)
	}
	if n > 0 {
		return nil, nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("seed admin: hash password: %w", err)
	}
	u := domain.User{
		Username:     "admin",
		DisplayName:  "Administrator",
		Role:         domain.RoleAdmin,
		PasswordHash: string(hash),
	}
	id, err := s.users.Create(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("seed admin: create: %w", err)
	}
	u.ID = id
	s.log.Warn("seeded bootstrap admin account; change the password before exposing the server",
		"username", "admin")
	s.record(ctx, "system", "user.seed_admin", id, "bootstrap admin created")
	return &u, nil
}

// record emits a best-effort audit event (failures are swallowed, matching the
// existing service convention).
func (s *AuthService) record(ctx context.Context, actor, action, target, detail string) {
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
