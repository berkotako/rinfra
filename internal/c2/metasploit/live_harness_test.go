//go:build c2live

// Live Metasploit operator smoke — OPT-IN, not run by normal CI.
//
// Validates the real msfrpcd client against a deployed Metasploit RPC daemon:
// it performs auth.login over the MessagePack-over-HTTP protocol and issues a
// session.list — the path most exposed to upstream RPC method/field drift,
// which the in-process tests (metasploit_live_test.go) cannot cover.
//
// Point it at a reachable msfrpcd and run the harness:
//
//	RINFRA_MSF_RPC_URL=https://10.0.0.5:55553 \
//	RINFRA_MSF_RPC_USER=msf RINFRA_MSF_RPC_PASSWORD=... \
//	go test -tags c2live ./internal/c2/metasploit/...
//
// Skipped unless RINFRA_MSF_RPC_URL is set.
package metasploit

import (
	"context"
	"os"
	"testing"
	"time"
)

// EnvMsfRPCURL is the harness-only base URL of a live msfrpcd. In production
// Control() derives the URL from the deployed teamserver; the smoke takes it
// from the environment so it can target any reachable instance.
const EnvMsfRPCURL = "RINFRA_MSF_RPC_URL"

func TestC2Live_MetasploitSessionList(t *testing.T) {
	baseURL := os.Getenv(EnvMsfRPCURL)
	if baseURL == "" {
		t.Skipf("%s not set; skipping live Metasploit operator smoke", EnvMsfRPCURL)
	}
	user := os.Getenv(EnvMsfRPCUser)
	if user == "" {
		user = defaultRPCUser
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// NewLiveClient performs auth.login; success proves the RPC credentials and
	// MessagePack transport reach a real msfrpcd.
	client, err := NewLiveClient(ctx, LiveConfig{
		BaseURL:            baseURL,
		Username:           user,
		Password:           os.Getenv(EnvMsfRPCPassword),
		InsecureSkipVerify: true, // msfrpcd ships a self-signed cert
	})
	if err != nil {
		t.Fatalf("auth.login to msfrpcd (%s): %v", baseURL, err)
	}

	// session.list is a no-side-effect read: a real round-trip against the
	// deployed daemon (an empty session list is fine).
	if _, err := client.SessionList(ctx); err != nil {
		t.Fatalf("session.list RPC: %v", err)
	}
}
