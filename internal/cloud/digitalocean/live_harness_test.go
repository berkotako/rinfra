//go:build cloudlive

// Live DigitalOcean credential smoke — OPT-IN, not run by normal CI.
//
// This is the cloud-side counterpart to the c2live harness: it validates that a
// real engagement DO token authenticates against the live API and round-trips,
// using a strictly read-only call (no resources created or destroyed). It is the
// seam the httptest-backed tests (live_test.go) cannot cover — the real API
// surface and auth — without risking spend or orphaned infra.
//
// Run against a real token:
//
//	DIGITALOCEAN_TOKEN=dop_v1_... go test -tags cloudlive ./internal/cloud/digitalocean/...
//
// Skipped unless DIGITALOCEAN_TOKEN is set.
package digitalocean

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestCloudLive_DigitalOceanAccount(t *testing.T) {
	token := os.Getenv(CredKeyToken)
	if token == "" {
		t.Skipf("%s not set; skipping live DigitalOcean credential smoke", CredKeyToken)
	}

	p := &provider{} // empty apiBase → the live DO API
	client, err := p.client(token)
	if err != nil {
		t.Fatalf("build godo client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Account.Get is read-only: success proves the token authenticates and a real
	// API round-trip works, with no side effects on the customer's tenancy.
	acct, _, err := client.Account.Get(ctx)
	if err != nil {
		t.Fatalf("Account.Get over the live DO API: %v", err)
	}
	if acct == nil {
		t.Fatal("Account.Get returned a nil account")
	}
}
