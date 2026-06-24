package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rinfra/rinfra/internal/audit"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/service"
	"github.com/rinfra/rinfra/internal/threatfeed"
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
	// admin (legacy / dev / test behavior). Production wiring MUST set Auth.
	Auth     *service.AuthService
	Projects *service.ProjectService
	// AllowedOrigins configures CORS. Empty defaults to the dev frontend
	// (http://localhost:3000); "*" reflects any request Origin.
	AllowedOrigins []string
	// ThreatFeed is optional; when nil the /advisories route reports 501.
	ThreatFeed *threatfeed.Service
}

// NewRouter builds and returns the chi router with all API v1 routes mounted.
func NewRouter(svc Services, log *slog.Logger) http.Handler {
	r := chi.NewRouter()

	if svc.Auth == nil {
		// Loud warning: in this mode every request is treated as an admin. It is
		// intended only for hermetic tests and local dev — never production.
		log.Warn("authentication is DISABLED (Services.Auth == nil); every request acts as admin — dev/test only")
	}

	// Global middleware.
	r.Use(RequestID)
	r.Use(OperatorIdentity)
	r.Use(corsMiddleware(svc.AllowedOrigins))
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
			// Project-scope emulation: run a scenario across all engagements,
			// and read aggregated coverage.
			r.Post("/runs", h.startProjectRun)
			r.Get("/coverage", h.getProjectCoverage)
		})

		// Engagements.
		r.Get("/engagements", h.listEngagements)
		r.Post("/engagements", h.createEngagement)
		r.Route("/engagements/{id}", func(r chi.Router) {
			// Authorize the actor against project membership for every
			// engagement-scoped route before any handler runs.
			r.Use(h.requireEngagementAccess)
			r.Get("/", h.getEngagement)
			// Status changes and authorization (approval) are privileged: only
			// admin/lead may approve an engagement or move its lifecycle. Project
			// membership is still enforced by requireEngagementAccess above, so an
			// admin/lead self-approving their own engagement is allowed.
			r.With(RequireRole(domain.RoleAdmin, domain.RoleLead)).Patch("/", h.patchEngagement)
			r.Get("/topology", h.getTopology)
			r.Put("/topology", h.putTopology)
			r.Get("/nodes/{nodeId}/redirector-config", h.getRedirectorConfig)
			r.Post("/validate", h.validateTopology)
			r.Post("/deploy", h.deploy)
			r.Post("/teardown", h.teardown)
			r.Put("/credentials/{provider}", h.putCredentials)
			r.Get("/credentials/{provider}", h.getCredentialsMeta)
			r.Get("/events", h.sseEvents)
			r.Get("/audit", h.listAuditEvents)
			r.Post("/runs", h.startRun)
			r.Get("/runs", h.listRuns)
			r.Get("/coverage", h.getCoverage)
			r.Get("/navigator", h.getNavigator)
			// Manual access: drive the deployed C2 by hand instead of auto-run.
			r.Get("/c2/manual-access", h.c2ManualAccess)
			r.Get("/c2/teamservers", h.c2ListTeamservers)
			r.Get("/c2/tunnels", h.c2ListTunnels)
			r.Post("/c2/tunnel", h.c2OpenTunnel)
			r.Delete("/c2/tunnel/{tunnelId}", h.c2CloseTunnel)
			// In-browser operator web shell (WebSocket) for a specific live C2 node.
			r.Get("/c2/{nodeId}/shell", h.c2Shell)
		})

		// C2 frameworks (from registry).
		r.Get("/c2/frameworks", h.listC2Frameworks)

		// Threat advisories (CISA KEV etc.) with suggested TTPs.
		r.Get("/advisories", h.listAdvisories)
		r.Get("/advisories/sources", h.listAdvisorySources)
		// Operator-managed advisory feeds (persisted; collected with base sources).
		r.Get("/advisories/feeds", h.listAdvisoryFeeds)
		r.Post("/advisories/feeds", h.createAdvisoryFeed)
		r.Delete("/advisories/feeds/{id}", h.deleteAdvisoryFeed)

		// IaC backend selection (Pulumi/Terraform). PUT is admin-only.
		r.Get("/config/iac", h.getIaCConfig)
		r.Put("/config/iac", h.setIaCConfig)

		// Scenarios — built-in catalog + operator-authored (full CRUD on authored).
		r.Get("/scenarios", h.listScenarios)
		r.Post("/scenarios", h.createScenario)
		r.Post("/scenarios/import", h.importScenario)
		r.Put("/scenarios/{id}", h.updateScenario)
		r.Delete("/scenarios/{id}", h.deleteScenario)

		// TTP library — operator-authored techniques (full CRUD).
		r.Get("/ttps", h.listTechniques)
		r.Post("/ttps", h.createTechnique)
		r.Put("/ttps/{attackId}", h.updateTechnique)
		r.Delete("/ttps/{attackId}", h.deleteTechnique)

		// Runs — fetch by ID (cross-engagement) + purple-team detection scoring.
		r.Get("/runs/{id}", h.getRun)
		r.Get("/runs/{id}/coverage", h.getRunCoverage)
		r.Post("/runs/{id}/detection", h.recordDetection)
	})

	return r
}
