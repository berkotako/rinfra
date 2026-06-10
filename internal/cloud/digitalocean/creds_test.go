package digitalocean

import (
	"context"
	"testing"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
)

// TestSweepOrphansNoCreds verifies that SweepOrphans returns an error when
// credentials are missing (validates the guard logic, no live API call).
func TestSweepOrphansNoCreds(t *testing.T) {
	p := &provider{}
	creds := cloud.Credentials{Raw: map[string]string{}}
	err := p.SweepOrphans(context.Background(), creds, "test-engagement")
	if err == nil {
		t.Error("expected error when token is not set, got nil")
	}
}

// TestSweepOrphansWithCreds verifies that SweepOrphans does not error on the
// credential check when the token is present. The actual API call is a
// TODO(live), so the function currently returns nil after the guard.
func TestSweepOrphansWithCreds(t *testing.T) {
	p := &provider{}
	creds := cloud.Credentials{Raw: map[string]string{
		CredKeyToken: "test-token",
	}}
	err := p.SweepOrphans(context.Background(), creds, "test-engagement")
	if err != nil {
		t.Errorf("unexpected error with valid token: %v", err)
	}
}

// TestDestroyIdempotentEmptyRef verifies that Destroy with an empty ProviderRef
// returns nil (idempotent — node was never provisioned or already destroyed).
func TestDestroyIdempotentEmptyRef(t *testing.T) {
	p := &provider{}
	node := domain.Node{ProviderRef: ""}
	err := p.Destroy(context.Background(), cloud.Credentials{}, node)
	if err != nil {
		t.Errorf("Destroy with empty ProviderRef should return nil, got: %v", err)
	}
}

// TestConfigureIngressEmptyRef verifies that ConfigureIngress errors early
// when the node has no ProviderRef.
func TestConfigureIngressEmptyRef(t *testing.T) {
	p := &provider{}
	node := domain.Node{ProviderRef: ""}
	err := p.ConfigureIngress(context.Background(), cloud.Credentials{}, node, nil)
	if err == nil {
		t.Error("expected error when ProviderRef is empty")
	}
}
