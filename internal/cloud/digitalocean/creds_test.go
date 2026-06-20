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

// SweepOrphans with a valid token now performs real godo API calls against the
// customer's account; that path is covered against an httptest fake in
// live_test.go (TestSweepOrphans_DeletesTaggedResources).

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
