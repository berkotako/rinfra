package terraform_test

import (
	"encoding/json"
	"testing"

	"github.com/rinfra/rinfra/internal/cloud"
	// Blank imports register the cloud providers (and their terraform.Builder).
	_ "github.com/rinfra/rinfra/internal/cloud/aws"
	_ "github.com/rinfra/rinfra/internal/cloud/azure"
	_ "github.com/rinfra/rinfra/internal/cloud/digitalocean"
	_ "github.com/rinfra/rinfra/internal/cloud/gcp"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration/terraform"
)

// builderFor fetches a registered provider and asserts it implements Builder.
func builderFor(t *testing.T, pt domain.CloudProviderType) terraform.Builder {
	t.Helper()
	p, err := cloud.Get(pt)
	if err != nil {
		t.Fatalf("provider %s not registered: %v", pt, err)
	}
	b, ok := p.(terraform.Builder)
	if !ok {
		t.Fatalf("provider %s does not implement terraform.Builder", pt)
	}
	return b
}

func node(id string, cl domain.CloudProviderType) domain.Node {
	return domain.Node{ID: id, Spec: domain.NodeSpec{Cloud: cl}}
}

func TestDOBuildConfig(t *testing.T) {
	b := builderFor(t, domain.CloudDigitalOcean)
	cfg, err := b.BuildConfig("eng12345678abcd", cloud.Credentials{}, []domain.Node{node("node-aaaa1111", domain.CloudDigitalOcean)})
	if err != nil {
		t.Fatal(err)
	}
	// Marshals to valid JSON.
	if _, err := json.Marshal(cfg); err != nil {
		t.Fatalf("config not marshalable: %v", err)
	}
	droplets, ok := cfg.Resource["digitalocean_droplet"].(map[string]any)
	if !ok || len(droplets) != 1 {
		t.Fatalf("expected one droplet resource, got %v", cfg.Resource)
	}
	// Outputs use the engine's per-node keys.
	if _, ok := cfg.Output[terraform.ProviderRefOutput("node-aaaa1111")]; !ok {
		t.Error("missing providerref output")
	}
	if _, ok := cfg.Output[terraform.PublicIPOutput("node-aaaa1111")]; !ok {
		t.Error("missing publicip output")
	}
}

func TestAllProvidersBuildValidJSON(t *testing.T) {
	for _, pt := range []domain.CloudProviderType{
		domain.CloudDigitalOcean, domain.CloudAWS, domain.CloudGCP, domain.CloudAzure,
	} {
		b := builderFor(t, pt)
		cfg, err := b.BuildConfig("eng12345678abcd", cloud.Credentials{}, []domain.Node{node("nodeabcd1234", pt)})
		if err != nil {
			t.Fatalf("%s BuildConfig: %v", pt, err)
		}
		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("%s config not marshalable: %v", pt, err)
		}
		// required_providers + at least one resource + per-node outputs.
		if cfg.Terraform == nil || cfg.Resource == nil || len(cfg.Output) != 2 {
			t.Errorf("%s: incomplete config: %s", pt, data)
		}
	}
}

func TestSafeName(t *testing.T) {
	if got := terraform.SafeName("node-abc.123:x"); got != "node_abc_123_x" {
		t.Errorf("SafeName = %q", got)
	}
}
