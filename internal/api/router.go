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
	Hub        *service.Hub
	AuditLog   audit.Reader
}

// NewRouter builds and returns the chi router with all API v1 routes mounted.
func NewRouter(svc Services, log *slog.Logger) http.Handler {
	r := chi.NewRouter()

	// Global middleware.
	r.Use(RequestID)
	r.Use(OperatorIdentity)
	r.Use(CORS)
	r.Use(Recoverer(log))
	r.Use(RequestLogger(log))

	h := &handlers{svc: svc, log: log}

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Route("/api/v1", func(r chi.Router) {
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
		})

		// C2 frameworks (from registry).
		r.Get("/c2/frameworks", h.listC2Frameworks)

		// Scenarios catalog.
		r.Get("/scenarios", h.listScenarios)

		// Runs — fetch by ID (cross-engagement).
		r.Get("/runs/{id}", h.getRun)
	})

	return r
}
