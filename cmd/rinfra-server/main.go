// Command rinfra-server is the RInfra control-plane entrypoint. It wires the
// cloud and C2 registries, sets up persistence, services, and the HTTP API.
//
// # Configuration (environment variables)
//
// Core:
//   - RINFRA_ADDR         listen address (default :8080)
//   - DATABASE_URL        Postgres connection string (required unless RINFRA_DEV=1)
//   - RINFRA_MASTER_KEY   base64-encoded 32-byte AES key (required unless RINFRA_DEV=1)
//   - RINFRA_DEV          set to "1" for in-memory stores and fake cloud;
//     no Postgres or master key required in this mode.
//
// Pulumi (required for real cloud provisioning; not needed with RINFRA_DEV=1):
//   - PULUMI_CONFIG_PASSPHRASE   passphrase used to encrypt secrets in the local
//     Pulumi state backend. Set to any non-empty value for local dev. Required
//     before calling any deploy/teardown endpoint.
//   - PULUMI_BACKEND_DIR         optional: root directory for Pulumi local state
//     files (default: $HOME/.rinfra/pulumi-state). Passed to orchestration.Engine.
//
// Threat feed (which advisory resources to collect; see config/threatfeed.example.json):
//   - RINFRA_THREATFEED        comma-separated source keys: "bundled" (default,
//     no egress) and/or "cisa-kev" (live CISA KEV catalog).
//   - RINFRA_THREATFEED_URLS   comma-separated http(s) URLs serving advisories in
//     RInfra's native Advisory JSON schema ("our data style").
//   - RINFRA_THREATFEED_FILES  comma-separated local files in that same schema.
//     All selected sources are merged (deduped by id, newest first).
//
// Pulumi uses the Pulumi CLI binary on PATH to run the automation API engine.
// Install the Pulumi CLI before enabling real cloud provisioning:
// https://www.pulumi.com/docs/install/
//
// See docs/RUNBOOK_DO.md for the full live-verification checklist.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration"
	"github.com/rinfra/rinfra/internal/payload"
	"github.com/rinfra/rinfra/internal/secrets"
	"github.com/rinfra/rinfra/internal/service"
	"github.com/rinfra/rinfra/internal/store/memstore"
	storepostgres "github.com/rinfra/rinfra/internal/store/postgres"
	"github.com/rinfra/rinfra/internal/threatfeed"
)

// buildThreatFeed assembles the advisory feed from configuration. RINFRA_THREATFEED
// is a comma-separated list of source keys that select which resources we
// collect advisories from. Built-in keys:
//
//   - bundled    static snapshot of real CISA KEV entries (default; no egress)
//   - cisa-kev   live CISA Known Exploited Vulnerabilities catalog (needs egress)
//
// Operators add their own feeds in RInfra's native Advisory JSON schema ("our
// data style"; see config/threatfeed.example.json):
//
//   - RINFRA_THREATFEED_URLS    comma-separated http(s) URLs serving that schema
//   - RINFRA_THREATFEED_FILES   comma-separated local file paths in that schema
//
// All selected sources are merged (de-duplicated by CVE id, newest first). With
// nothing configured, the bundled snapshot is used.
func buildThreatFeed(log *slog.Logger) *threatfeed.Service {
	var sources []threatfeed.Source
	for _, key := range splitList(os.Getenv("RINFRA_THREATFEED")) {
		switch key {
		case "bundled":
			sources = append(sources, threatfeed.BundledSource{})
		case "cisa-kev":
			sources = append(sources, threatfeed.NewCISAKEVSource())
		default:
			log.Warn("threat feed: unknown source key, ignoring", "key", key)
		}
	}
	for _, u := range splitList(os.Getenv("RINFRA_THREATFEED_URLS")) {
		sources = append(sources, &threatfeed.JSONSource{URL: u})
	}
	for _, f := range splitList(os.Getenv("RINFRA_THREATFEED_FILES")) {
		sources = append(sources, &threatfeed.JSONSource{File: f})
	}
	if len(sources) == 0 {
		sources = append(sources, threatfeed.BundledSource{})
	}

	var src threatfeed.Source
	if len(sources) == 1 {
		src = sources[0]
	} else {
		src = threatfeed.MultiSource{Sources: sources}
	}
	log.Info("threat feed source", "source", src.Name())
	return threatfeed.New(src)
}

// splitList parses a comma-separated env value into a trimmed, non-empty slice.
func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

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

// buildEngine constructs the Pulumi orchestration engine and registers
// ProgramBuilder implementations for every real cloud provider. The stateDir
// is read from PULUMI_BACKEND_DIR (default: $HOME/.rinfra/pulumi-state).
// In RINFRA_DEV mode the engine is still constructed so the code path is
// exercised, but the fake provider never routes through it.
func buildEngine(log *slog.Logger) *orchestration.Engine {
	stateDir := envOr("PULUMI_BACKEND_DIR", "")
	eng := orchestration.New(stateDir, log)

	// Register every cloud provider that implements ProgramBuilder.
	// The real providers (aws, digitalocean, gcp, azure) implement it;
	// the fake provider does not — so it keeps the direct ProvisionNode path.
	for _, pt := range []domain.CloudProviderType{
		domain.CloudDigitalOcean,
		domain.CloudAWS,
		domain.CloudGCP,
		domain.CloudAzure,
	} {
		p, err := cloud.Get(pt)
		if err != nil {
			// Provider not registered (shouldn't happen given the imports above).
			log.Warn("buildEngine: cloud provider not registered", "provider", pt)
			continue
		}
		if builder, ok := p.(orchestration.ProgramBuilder); ok {
			eng.RegisterBuilder(pt, builder)
			log.Info("orchestration: registered ProgramBuilder", "provider", pt)
		}
	}
	return eng
}

func startWithMemstore(log *slog.Logger, enc *secrets.Encrypter, hub *service.Hub) {
	auditLog := memstore.NewAuditLogger()
	engStore := memstore.NewEngagementStore()
	infraStore := memstore.NewInfraStore()
	scenarioStore := memstore.NewScenarioStore()
	userScenarioStore := memstore.NewUserScenarioStore()
	userTechniqueStore := memstore.NewUserTechniqueStore()
	credStore := memstore.NewCredentialStore()
	jobStore := memstore.NewJobStore()
	userStore := memstore.NewUserStore()
	projectStore := memstore.NewProjectStore()
	sessionStore := memstore.NewSessionStore()

	svcEng := service.NewEngagementService(engStore, auditLog)
	svcInfra := service.NewInfraService(engStore, infraStore, credStore, jobStore, auditLog, enc, hub, log)
	svcInfra.WithEngine(buildEngine(log))
	svcEmu := service.NewEmulationService(engStore, scenarioStore, auditLog, hub)
	svcEmu.WithUserScenarios(userScenarioStore)
	svcEmu.WithUserTechniques(userTechniqueStore)
	// Dev mode: keep the fake resolver so no live teamserver is needed.
	svcEmu.WithResolver(service.NewFakeResolver())
	svcC2 := service.NewC2Service(engStore, infraStore, auditLog, log)
	svcAuth := service.NewAuthService(userStore, sessionStore, auditLog, log)
	svcProject := service.NewProjectService(projectStore, userStore, auditLog)
	seedAdmin(log, svcAuth, true) // dev/memstore mode

	svcInfra.ResumeJobs(context.Background())

	router := api.NewRouter(api.Services{
		Engagement:     svcEng,
		Infra:          svcInfra,
		Emulation:      svcEmu,
		C2:             svcC2,
		Hub:            hub,
		AuditLog:       audit.Reader(auditLog),
		Auth:           svcAuth,
		Projects:       svcProject,
		AllowedOrigins: corsOriginsFromEnv(),
		ThreatFeed:     buildThreatFeed(log),
	}, log)

	runServer(log, router, svcC2)
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
	userScenarioStore := storepostgres.NewUserScenarioStore(pool)
	userTechniqueStore := storepostgres.NewUserTechniqueStore(pool)
	credStore := storepostgres.NewCredentialStore(pool)
	jobStore := storepostgres.NewJobStore(pool)
	userStore := storepostgres.NewUserStore(pool)
	projectStore := storepostgres.NewProjectStore(pool)
	sessionStore := storepostgres.NewSessionStore(pool)

	svcEng := service.NewEngagementService(engStore, auditLog)
	svcInfra := service.NewInfraService(engStore, infraStore, credStore, jobStore, auditLog, enc, hub, log)
	svcInfra.WithEngine(buildEngine(log))
	svcEmu := service.NewEmulationService(engStore, scenarioStore, auditLog, hub)
	svcEmu.WithUserScenarios(userScenarioStore)
	svcEmu.WithUserTechniques(userTechniqueStore)
	// Production mode: use registry-backed resolver that finds the engagement's
	// deployed C2 topology and calls C2Provider.Control(teamserver).
	svcEmu.WithResolver(service.NewRegistryResolver(infraStore))
	svcC2 := service.NewC2Service(engStore, infraStore, auditLog, log)
	svcAuth := service.NewAuthService(userStore, sessionStore, auditLog, log)
	svcProject := service.NewProjectService(projectStore, userStore, auditLog)
	seedAdmin(log, svcAuth, false) // production: requires RINFRA_ADMIN_PASSWORD

	svcInfra.ResumeJobs(context.Background())

	// Production must never run with authentication disabled.
	if svcAuth == nil {
		log.Error("authentication service is required in production mode")
		os.Exit(1)
	}

	router := api.NewRouter(api.Services{
		Engagement:     svcEng,
		Infra:          svcInfra,
		Emulation:      svcEmu,
		C2:             svcC2,
		Hub:            hub,
		AuditLog:       audit.Reader(auditLog),
		Auth:           svcAuth,
		Projects:       svcProject,
		AllowedOrigins: corsOriginsFromEnv(),
		ThreatFeed:     buildThreatFeed(log),
	}, log)

	runServer(log, router, svcC2)
}

// seedAdmin bootstraps the first admin when no users exist. In dev mode it uses
// the well-known default "admin" (with a loud warning). In production it uses
// RINFRA_ADMIN_PASSWORD and refuses to seed a default — no admin is created
// until an explicit password is supplied, so an exposed deployment can never
// boot with admin/admin.
func seedAdmin(log *slog.Logger, svcAuth *service.AuthService, devMode bool) {
	password := "admin"
	if devMode {
		log.Warn("RINFRA_DEV=1: seeding default admin/admin — for local use only; never expose this server")
	} else {
		password = os.Getenv("RINFRA_ADMIN_PASSWORD")
		if password == "" {
			log.Warn("no bootstrap admin seeded: set RINFRA_ADMIN_PASSWORD to create the first admin (default admin/admin is dev-only)")
			return
		}
	}
	if _, err := svcAuth.SeedAdmin(context.Background(), password); err != nil {
		log.Error("seed admin user", "err", err)
	}
}

func runServer(log *slog.Logger, handler http.Handler, c2 *service.C2Service) {
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
	// Close any open C2 tunnels so none are orphaned across restart.
	if c2 != nil {
		c2.Shutdown()
	}
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

// corsOriginsFromEnv parses RINFRA_CORS_ORIGINS (comma-separated) into the
// allowed CORS origins. Empty leaves the default (the dev frontend); "*"
// reflects any Origin. Example: "https://console.example.com,https://ops.example.com".
func corsOriginsFromEnv() []string {
	raw := os.Getenv("RINFRA_CORS_ORIGINS")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, o := range strings.Split(raw, ",") {
		if o = strings.TrimSpace(o); o != "" {
			out = append(out, o)
		}
	}
	return out
}
