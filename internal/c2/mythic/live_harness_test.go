//go:build c2live

// Live Mythic operator smoke — OPT-IN, not run by normal CI.
//
// Validates the real live client against a deployed Mythic teamserver: it
// authenticates to the /auth endpoint (or uses a pre-issued API token) and
// issues a GraphQL query — the path most exposed to upstream schema drift,
// which the in-process tests (mythic_live_test.go) cannot cover.
//
// Point it at a reachable Mythic instance and run the harness:
//
//	RINFRA_MYTHIC_URL=https://10.0.0.5:7443 \
//	RINFRA_MYTHIC_USER=mythic_admin RINFRA_MYTHIC_PASSWORD=... \
//	RINFRA_MYTHIC_INSECURE_TLS=1 \
//	go test -tags c2live ./internal/c2/mythic/...
//
// Skipped unless RINFRA_MYTHIC_URL is set.
package mythic

import (
	"context"
	"os"
	"testing"
	"time"
)

// EnvMythicURL is the harness-only base URL of a live Mythic teamserver. In
// production Control() derives the URL from the deployed teamserver; the smoke
// takes it from the environment so it can target any reachable instance.
const EnvMythicURL = "RINFRA_MYTHIC_URL"

func TestC2Live_MythicCallbacks(t *testing.T) {
	baseURL := os.Getenv(EnvMythicURL)
	if baseURL == "" {
		t.Skipf("%s not set; skipping live Mythic operator smoke", EnvMythicURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := NewLiveClient(ctx, LiveConfig{
		BaseURL:            baseURL,
		Username:           os.Getenv(EnvMythicUser),
		Password:           os.Getenv(EnvMythicPassword),
		APIToken:           os.Getenv(EnvMythicToken),
		InsecureSkipVerify: isTruthy(os.Getenv(EnvMythicInsecureTLS)),
	})
	if err != nil {
		t.Fatalf("authenticate to Mythic (%s): %v", baseURL, err)
	}

	// Callbacks is a no-side-effect read: success proves /auth + a real GraphQL
	// round-trip against the deployed schema (an empty callback list is fine).
	if _, err := client.Callbacks(ctx); err != nil {
		t.Fatalf("Callbacks GraphQL query: %v", err)
	}
}
