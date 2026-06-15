package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rinfra/rinfra/internal/audit"
	"github.com/rinfra/rinfra/internal/service"
)

// Services bundles the service layer injected into the router.
type Services struct {
	Engagement *service.EngagementService
	Infra      *service.InfraService
	Emulation  *service.EmulationService
	C2         *service.C2Service
	Hub        *service.Hub
	AuditLog   audit.Reader
	// Auth and Projects are optional: when Auth is nil the Authenticate
	// middleware is a no-op and the user/project routes act on a synthetic
	// admin (legacy / dev / test behavior).
	Auth     *service.AuthService
	Projects *service.ProjectService
}

// NewRouter builds and returns the chi router with all API v1 routes mounted.
func NewRouter(svc Services, log *slog.Logger) http.Handler {
	r := chi.NewRouter()

	// Global middleware.
	r.Use(RequestID)
	r.Use(OperatorIdentity)
	r.Use(CORS)
	r.Use(Authenticate(svc))
	r.Use(Recoverer(log))
	r.Use(RequestLogger(log))

	h := &handlers{svc: svc, log: log}

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Route("/api/v1", func(r chi.Router) {
		// Authentication.
		r.Post("/auth/login", h.login)
		r.Post("/auth/logout", h.logout)
		r.Get("/auth/me", h.me)

		// Users.
		r.Get("/users", h.listUsers)
		r.Post("/users", h.createUser)
		r.Route("/users/{id}", func(r chi.Router) {
			r.Get("/", h.getUser)
			r.Patch("/", h.patchUser)
			r.Post("/password", h.changePassword)
		})

		// Projects.
		r.Get("/projects", h.listProjects)
		r.Post("/projects", h.createProject)
		r.Route("/projects/{id}", func(r chi.Router) {
			r.Get("/", h.getProject)
			r.Patch("/", h.patchProject)
			r.Delete("/", h.deleteProject)
			r.Get("/members", h.listProjectMembers)
			r.Post("/members", h.addProjectMember)
			r.Delete("/members/{userId}", h.removeProjectMember)
			r.Get("/engagements", h.listProjectEngagements)
		})

		// Engagements.
		r.Get("/engagements", h.listEngagements)
		r.Post("/engagements", h.createEngagement)
		r.Route("/engagements/{id}", func(r chi.Router) {
			r.Get("/", h.getEngagement)
			r.Patch("/", h.patchEngagement)
			r.Get("/topology", h.getTopology)
			r.Put("/topology", h.putTopology)
			r.Post("/validate", h.validateTopology)
			r.Post("/deploy", h.deploy)
			r.Post("/teardown", h.teardown)
			r.Put("/credentials/{provider}", h.putCredentials)
			r.Get("/credentials/{provider}", h.getCredentialsMeta)
			r.Get("/events", h.sseEvents)
			r.Get("/audit", h.listAuditEvents)
			r.Post("/runs", h.startRun)
			r.Get("/coverage", h.getCoverage)
			r.Get("/navigator", h.getNavigator)
			// Manual access: drive the deployed C2 by hand instead of auto-run.
			r.Get("/c2/manual-access", h.c2ManualAccess)
			r.Post("/c2/tunnel", h.c2OpenTunnel)
			r.Delete("/c2/tunnel/{tunnelId}", h.c2CloseTunnel)
			// In-browser operator web shell (WebSocket) for a specific live C2 node.
			r.Get("/c2/{nodeId}/shell", h.c2Shell)
		})

		// C2 frameworks (from registry).
		r.Get("/c2/frameworks", h.listC2Frameworks)

		// Scenarios — built-in catalog + operator-authored (full CRUD on authored).
		r.Get("/scenarios", h.listScenarios)
		r.Post("/scenarios", h.createScenario)
		r.Put("/scenarios/{id}", h.updateScenario)
		r.Delete("/scenarios/{id}", h.deleteScenario)

		// Runs — fetch by ID (cross-engagement).
		r.Get("/runs/{id}", h.getRun)
	})

	return r
}
