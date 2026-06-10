package api

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
)

type contextKey string

const (
	ctxKeyRequestID contextKey = "request_id"
	ctxKeyActor     contextKey = "actor"
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
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-RInfra-Operator, X-Request-ID")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
