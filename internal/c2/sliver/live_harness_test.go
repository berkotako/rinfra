//go:build c2live

// Live Sliver operator smoke — OPT-IN, not run by normal CI.
//
// Validates that the real gRPC operator client (rpcpb stubs over mTLS) can
// authenticate to and round-trip with a live sliver-server multiplayer
// listener. Generate an operator config on the server with:
//
//	sliver-server operator --name rinfra --lhost <host> --save ./operator.cfg
//
// then point the env var at it and run the harness:
//
//	RINFRA_SLIVER_OPERATOR_CONFIG=./operator.cfg make test-c2live
//	# (or: go test -tags c2live ./internal/c2/sliver/...)
//
// Skipped unless RINFRA_SLIVER_OPERATOR_CONFIG is set.
package sliver

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestC2Live_SliverOperatorSessions(t *testing.T) {
	path := os.Getenv(EnvOperatorConfig)
	if path == "" {
		t.Skipf("%s not set; skipping live Sliver operator smoke", EnvOperatorConfig)
	}
	cfg, err := LoadOperatorConfig(path)
	if err != nil {
		t.Fatalf("load operator config: %v", err)
	}
	client, err := DialOperatorClient(context.Background(), cfg)
	if err != nil {
		t.Fatalf("dial operator (mTLS): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Sessions is a no-side-effect RPC: success proves mTLS auth + a real
	// round-trip against the multiplayer listener (empty list is fine).
	if _, err := client.Sessions(ctx); err != nil {
		t.Fatalf("Sessions over mTLS gRPC: %v", err)
	}
}
