package api

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rinfra/rinfra/internal/domain"
)

type contextKey string

const (
	ctxKeyRequestID contextKey = "request_id"
	ctxKeyActor     contextKey = "actor"
	ctxKeyUser      contextKey = "user"
)

// RequestID adds a UUID request ID to the context and response headers.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		ctx := context.WithValue(r.Context(), ctxKeyRequestID, id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OperatorIdentity reads X-RInfra-Operator and stores the value in the
// context. Defaults to "anonymous" for MVP. This is the slot for real OIDC
// authn in a later phase.
func OperatorIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor := r.Header.Get("X-RInfra-Operator")
		if actor == "" {
			actor = "anonymous"
		}
		ctx := context.WithValue(r.Context(), ctxKeyActor, actor)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// actorFrom extracts the operator identity from a request context.
func actorFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyActor).(string); ok {
		return v
	}
	return "anonymous"
}

// Authenticate validates a bearer-token session and attaches the authenticated
// user to the request context. It is a NO-OP when svc.Auth is nil — this
// preserves the legacy header-only identity used by the existing test suite and
// keeps the control plane runnable without the auth subsystem. When auth IS
// wired, every route except the public ones requires a valid token.
func Authenticate(svc Services) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if svc.Auth == nil {
				next.ServeHTTP(w, r)
				return
			}
			// Public routes need no authentication.
			switch r.URL.Path {
			case "/healthz", "/api/v1/auth/login":
				next.ServeHTTP(w, r)
				return
			}
			token := bearerToken(r)
			if token == "" {
				writeErrorCode(w, http.StatusUnauthorized, "unauthorized", "authentication required")
				return
			}
			user, err := svc.Auth.Authenticate(r.Context(), token)
			if err != nil {
				writeErrorCode(w, http.StatusUnauthorized, "unauthorized", "invalid or expired session")
				return
			}
			ctx := context.WithValue(r.Context(), ctxKeyUser, &user)
			ctx = context.WithValue(ctx, ctxKeyActor, user.Username)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole returns middleware that rejects requests whose authenticated user
// is not one of the given roles. When no user is present (auth disabled / dev /
// legacy), it allows the request through — service-layer authorization is the
// authoritative gate; this is a lightweight guard for enabled-auth deployments.
func RequireRole(roles ...domain.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := userFrom(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			for _, role := range roles {
				if u.Role == role {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeErrorCode(w, http.StatusForbidden, "forbidden", "insufficient role")
		})
	}
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

// userFrom returns the authenticated user from context, if present.
func userFrom(ctx context.Context) (*domain.User, bool) {
	u, ok := ctx.Value(ctxKeyUser).(*domain.User)
	return u, ok
}

// actorUser returns the authenticated user, or a synthetic admin actor when
// auth is disabled (dev/test/legacy). This lets the user/project endpoints and
// their service-layer authorization run uniformly in every mode.
func actorUser(ctx context.Context) domain.User {
	if u, ok := userFrom(ctx); ok {
		return *u
	}
	return domain.User{ID: "dev", Username: actorFrom(ctx), Role: domain.RoleAdmin}
}

// requestIDFrom extracts the request ID from context.
func requestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return v
	}
	return ""
}

// statusWriter wraps ResponseWriter to track the written status code for
// logging. It also proxies http.Flusher so SSE handlers work correctly.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher by delegating to the underlying writer if it
// also implements Flusher.
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap exposes the underlying ResponseWriter so middleware that needs the
// original writer (e.g. chi's wrap_writer) can find it.
func (s *statusWriter) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}

// RequestLogger logs every request using structured slog output.
func RequestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(sw, r)
			log.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", requestIDFrom(r.Context()),
				"actor", actorFrom(r.Context()),
			)
		})
	}
}

// Recoverer catches panics, logs them, and returns 500.
func Recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic recovered", "panic", rec, "stack", string(debug.Stack()))
					writeJSON(w, http.StatusInternalServerError, errorBody{Error: errDetail{
						Code:    "internal_error",
						Message: "internal server error",
					}})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// CORS adds permissive CORS headers for the dev frontend at localhost:3000.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-RInfra-Operator, X-Request-ID, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
