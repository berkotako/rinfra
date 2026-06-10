// Package cloud_test contains cross-provider tests that document and verify
// the deliberate per-provider divergence in ConfigureIngress translation.
//
// CLAUDE.md: "ConfigureIngress and DNS are where the providers genuinely diverge —
// implement those per provider deliberately; a wrong ingress rule is a dead engagement."
package cloud_test

import (
	"context"
	"testing"

	// Import providers to register them; tests operate via the CloudProvider interface.
	_ "github.com/rinfra/rinfra/internal/cloud/aws"
	_ "github.com/rinfra/rinfra/internal/cloud/azure"
	_ "github.com/rinfra/rinfra/internal/cloud/digitalocean"
	_ "github.com/rinfra/rinfra/internal/cloud/gcp"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
)

// TestConfigureIngressRequiresProviderRef verifies that all real providers
// reject ConfigureIngress calls on nodes with no ProviderRef (not provisioned).
// This is a safety invariant: you cannot configure ingress for a node that
// doesn't exist yet.
func TestConfigureIngressRequiresProviderRef(t *testing.T) {
	providers := []domain.CloudProviderType{
		domain.CloudDigitalOcean,
		domain.CloudAWS,
		domain.CloudGCP,
		domain.CloudAzure,
	}

	rules := []domain.Rule{
		{Protocol: "tcp", Port: 443, SourceCIDR: "0.0.0.0/0", Allow: true},
	}
	unprovisioned := domain.Node{
		ID:          "test-node",
		ProviderRef: "", // no provider ref = not provisioned
	}

	for _, pt := range providers {
		t.Run(string(pt), func(t *testing.T) {
			p, err := cloud.Get(pt)
			if err != nil {
				t.Fatalf("provider not registered: %v", err)
			}
			err = p.ConfigureIngress(context.Background(), cloud.Credentials{Raw: map[string]string{}}, unprovisioned, rules)
			if err == nil {
				t.Errorf("%s.ConfigureIngress: expected error for unprovisioned node (no ProviderRef), got nil", pt)
			}
		})
	}
}

// TestDestroyIdempotentForAllProviders verifies that all real providers return
// nil when Destroy is called on a node with empty ProviderRef (idempotent).
func TestDestroyIdempotentForAllProviders(t *testing.T) {
	providers := []domain.CloudProviderType{
		domain.CloudDigitalOcean,
		domain.CloudAWS,
		domain.CloudGCP,
		domain.CloudAzure,
	}

	unprovisioned := domain.Node{
		ID:          "test-node",
		ProviderRef: "", // never provisioned
	}

	for _, pt := range providers {
		t.Run(string(pt), func(t *testing.T) {
			p, err := cloud.Get(pt)
			if err != nil {
				t.Fatalf("provider not registered: %v", err)
			}
			err = p.Destroy(context.Background(), cloud.Credentials{Raw: map[string]string{}}, unprovisioned)
			if err != nil {
				t.Errorf("%s.Destroy: expected nil for empty ProviderRef (idempotent), got: %v", pt, err)
			}
		})
	}
}

// TestManageDNSRequiresZone verifies that all real providers reject ManageDNS
// calls with an empty Zone.
func TestManageDNSRequiresZone(t *testing.T) {
	providers := []domain.CloudProviderType{
		domain.CloudDigitalOcean,
		domain.CloudAWS,
		domain.CloudGCP,
		domain.CloudAzure,
	}
	minimalCreds := map[domain.CloudProviderType]cloud.Credentials{
		domain.CloudDigitalOcean: {Raw: map[string]string{"DIGITALOCEAN_TOKEN": "tok"}},
		domain.CloudAWS: {Raw: map[string]string{
			"AWS_ACCESS_KEY_ID":     "key",
			"AWS_SECRET_ACCESS_KEY": "secret",
			"AWS_REGION":            "us-east-1",
		}},
		domain.CloudGCP: {Raw: map[string]string{
			"GOOGLE_CREDENTIALS": `{"type":"service_account"}`,
			"GOOGLE_PROJECT":     "proj",
		}},
		domain.CloudAzure: {Raw: map[string]string{
			"ARM_SUBSCRIPTION_ID": "sub",
			"ARM_TENANT_ID":       "ten",
			"ARM_CLIENT_ID":       "cli",
			"ARM_CLIENT_SECRET":   "sec",
		}},
	}

	rec := domain.Record{
		Zone:  "", // intentionally empty
		Name:  "www",
		Type:  "A",
		Value: "1.2.3.4",
	}

	for _, pt := range providers {
		t.Run(string(pt), func(t *testing.T) {
			p, err := cloud.Get(pt)
			if err != nil {
				t.Fatalf("provider not registered: %v", err)
			}
			err = p.ManageDNS(context.Background(), minimalCreds[pt], rec)
			if err == nil {
				t.Errorf("%s.ManageDNS: expected error for empty Zone, got nil", pt)
			}
		})
	}
}
