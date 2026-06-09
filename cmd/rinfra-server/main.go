// Command rinfra-server is the RInfra control-plane entrypoint. It wires the
// cloud and C2 registries, sets up persistence and the HTTP API, and serves.
//
// This is intentionally minimal: handlers and service wiring are filled in as
// the build order in CLAUDE.md progresses. It uses only the standard library
// so the scaffold compiles before external dependencies (Pulumi, pgx, chi) are
// added.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	// Blank imports register the cloud and C2 adapters with their registries.
	_ "github.com/rinfra/rinfra/internal/cloud/aws"
	_ "github.com/rinfra/rinfra/internal/cloud/azure"
	_ "github.com/rinfra/rinfra/internal/cloud/digitalocean"
	_ "github.com/rinfra/rinfra/internal/cloud/gcp"

	_ "github.com/rinfra/rinfra/internal/c2/bruteratel"
	_ "github.com/rinfra/rinfra/internal/c2/cobaltstrike"
	_ "github.com/rinfra/rinfra/internal/c2/custom"
	_ "github.com/rinfra/rinfra/internal/c2/havoc"
	_ "github.com/rinfra/rinfra/internal/c2/metasploit"
	_ "github.com/rinfra/rinfra/internal/c2/mythic"
	_ "github.com/rinfra/rinfra/internal/c2/poshc2"
	_ "github.com/rinfra/rinfra/internal/c2/sliver"

	// Blank imports register the payload generators.
	_ "github.com/rinfra/rinfra/internal/payload/msfvenom"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/payload"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Surface the registered C2 frameworks and their support tiers at boot.
	for _, p := range c2.List() {
		log.Info("c2 framework registered", "name", p.Name(), "tier", p.Tier().String())
	}
	// Surface the registered payload generators at boot.
	for _, g := range payload.List() {
		log.Info("payload generator registered", "name", g.Name(), "pairs_with", g.PairsWith())
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	// TODO(claude-code): mount engagement, infrastructure, c2, and emulation
	// API handlers here as services land (see build order in CLAUDE.md).

	addr := envOr("RINFRA_ADDR", ":8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
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
