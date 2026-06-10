// Command rinfra-server is the RInfra control-plane entrypoint. It wires the
// cloud and C2 registries, sets up persistence, services, and the HTTP API.
//
// Configuration (environment variables):
//   - RINFRA_ADDR         listen address (default :8080)
//   - DATABASE_URL        Postgres connection string (required unless RINFRA_DEV=1)
//   - RINFRA_MASTER_KEY   base64-encoded 32-byte AES key (required unless RINFRA_DEV=1)
//   - RINFRA_DEV          set to "1" for in-memory stores and fake cloud;
//     no Postgres or master key required in this mode.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	// Cloud adapters register themselves on import.
	_ "github.com/rinfra/rinfra/internal/cloud/aws"
	_ "github.com/rinfra/rinfra/internal/cloud/azure"
	_ "github.com/rinfra/rinfra/internal/cloud/digitalocean"
	_ "github.com/rinfra/rinfra/internal/cloud/fake"
	_ "github.com/rinfra/rinfra/internal/cloud/gcp"

	// C2 adapters register themselves on import.
	_ "github.com/rinfra/rinfra/internal/c2/bruteratel"
	_ "github.com/rinfra/rinfra/internal/c2/cobaltstrike"
	_ "github.com/rinfra/rinfra/internal/c2/custom"
	_ "github.com/rinfra/rinfra/internal/c2/havoc"
	_ "github.com/rinfra/rinfra/internal/c2/metasploit"
	_ "github.com/rinfra/rinfra/internal/c2/mythic"
	_ "github.com/rinfra/rinfra/internal/c2/poshc2"
	_ "github.com/rinfra/rinfra/internal/c2/sliver"

	// Payload generators register themselves on import.
	_ "github.com/rinfra/rinfra/internal/payload/msfvenom"

	"github.com/rinfra/rinfra/internal/api"
	"github.com/rinfra/rinfra/internal/audit"
	auditpostgres "github.com/rinfra/rinfra/internal/audit/postgres"
	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/payload"
	"github.com/rinfra/rinfra/internal/secrets"
	"github.com/rinfra/rinfra/internal/service"
	"github.com/rinfra/rinfra/internal/store/memstore"
	storepostgres "github.com/rinfra/rinfra/internal/store/postgres"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Log registered C2 frameworks and payload generators.
	for _, p := range c2.List() {
		log.Info("c2 framework registered", "name", p.Name(), "tier", p.Tier().String())
	}
	for _, g := range payload.List() {
		log.Info("payload generator registered", "name", g.Name(), "pairs_with", g.PairsWith())
	}

	devMode := os.Getenv("RINFRA_DEV") == "1"

	// Build the encrypter. In dev mode generate an ephemeral key.
	enc := buildEncrypter(log, devMode)

	hub := service.NewHub()

	if devMode {
		log.Info("starting in dev/memstore mode")
		startWithMemstore(log, enc, hub)
		return
	}
	if os.Getenv("DATABASE_URL") == "" {
		// Refuse to run on in-memory stores outside dev mode: the audit log
		// must be durable. Set RINFRA_DEV=1 explicitly for local development.
		log.Error("DATABASE_URL is required (or set RINFRA_DEV=1 for in-memory dev mode)")
		os.Exit(1)
	}

	startWithPostgres(log, enc, hub)
}

func buildEncrypter(log *slog.Logger, devMode bool) *secrets.Encrypter {
	if devMode {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			log.Error("generate ephemeral master key", "err", err)
			os.Exit(1)
		}
		enc, err := secrets.New(base64.StdEncoding.EncodeToString(key))
		if err != nil {
			log.Error("create encrypter", "err", err)
			os.Exit(1)
		}
		log.Warn("RINFRA_DEV=1: using ephemeral master key — credentials will not survive restarts")
		return enc
	}
	enc, err := secrets.NewFromEnv()
	if err != nil {
		log.Error("RINFRA_MASTER_KEY required in production mode", "err", err)
		os.Exit(1)
	}
	return enc
}

func startWithMemstore(log *slog.Logger, enc *secrets.Encrypter, hub *service.Hub) {
	auditLog := memstore.NewAuditLogger()
	engStore := memstore.NewEngagementStore()
	infraStore := memstore.NewInfraStore()
	scenarioStore := memstore.NewScenarioStore()
	credStore := memstore.NewCredentialStore()
	jobStore := memstore.NewJobStore()

	svcEng := service.NewEngagementService(engStore, auditLog)
	svcInfra := service.NewInfraService(engStore, infraStore, credStore, jobStore, auditLog, enc, hub, log)
	svcEmu := service.NewEmulationService(engStore, scenarioStore, auditLog, hub)

	svcInfra.ResumeJobs(context.Background())

	router := api.NewRouter(api.Services{
		Engagement: svcEng,
		Infra:      svcInfra,
		Emulation:  svcEmu,
		Hub:        hub,
		AuditLog:   audit.Reader(auditLog),
	}, log)

	runServer(log, router)
}

func startWithPostgres(log *slog.Logger, enc *secrets.Encrypter, hub *service.Hub) {
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Error("connect to postgres", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(context.Background()); err != nil {
		log.Error("ping postgres", "err", err)
		os.Exit(1)
	}
	log.Info("connected to postgres")

	auditLog := auditpostgres.New(pool)
	engStore := storepostgres.NewEngagementStore(pool)
	infraStore := storepostgres.NewInfraStore(pool)
	scenarioStore := storepostgres.NewScenarioStore(pool)
	credStore := storepostgres.NewCredentialStore(pool)
	jobStore := storepostgres.NewJobStore(pool)

	svcEng := service.NewEngagementService(engStore, auditLog)
	svcInfra := service.NewInfraService(engStore, infraStore, credStore, jobStore, auditLog, enc, hub, log)
	svcEmu := service.NewEmulationService(engStore, scenarioStore, auditLog, hub)

	svcInfra.ResumeJobs(context.Background())

	router := api.NewRouter(api.Services{
		Engagement: svcEng,
		Infra:      svcInfra,
		Emulation:  svcEmu,
		Hub:        hub,
		AuditLog:   audit.Reader(auditLog),
	}, log)

	runServer(log, router)
}

func runServer(log *slog.Logger, handler http.Handler) {
	addr := envOr("RINFRA_ADDR", ":8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go func() {
		log.Info("rinfra control plane listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
