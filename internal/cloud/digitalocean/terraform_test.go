package digitalocean

import (
	"testing"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration/terraform"
)

// TestBuildConfig_ReservedIP verifies the DO Terraform builder emits a Reserved
// IP bound to the droplet and exports IT (not the ephemeral droplet IP) as the
// node's PublicIP — the stable-address parity with AWS/GCP/Azure.
func TestBuildConfig_ReservedIP(t *testing.T) {
	p := &provider{}
	nodes := []domain.Node{
		{ID: "node-1", Spec: domain.NodeSpec{Cloud: domain.CloudDigitalOcean, Region: "nyc3", Size: "s-1vcpu-1gb"}},
	}
	cfg, err := p.BuildConfig("eng-1", cloud.Credentials{}, nodes)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	rips, ok := cfg.Resource["digitalocean_reserved_ip"].(map[string]any)
	if !ok || len(rips) != 1 {
		t.Fatalf("expected one digitalocean_reserved_ip resource, got %v", cfg.Resource["digitalocean_reserved_ip"])
	}
	key := terraform.SafeName("node-1")
	rip, ok := rips[key].(map[string]any)
	if !ok {
		t.Fatalf("no reserved IP for key %q", key)
	}
	if rip["droplet_id"] != "${digitalocean_droplet."+key+".id}" {
		t.Errorf("reserved IP not bound to the droplet: %v", rip["droplet_id"])
	}
	if rip["region"] != "nyc3" {
		t.Errorf("reserved IP region = %v, want nyc3", rip["region"])
	}

	// PublicIP output must come from the reserved IP, not the droplet's ephemeral IP.
	pub, ok := cfg.Output[terraform.PublicIPOutput("node-1")].(map[string]any)
	if !ok {
		t.Fatalf("no PublicIP output for node-1")
	}
	if want := "${digitalocean_reserved_ip." + key + ".ip_address}"; pub["value"] != want {
		t.Errorf("PublicIP output = %v, want %v (the stable reserved IP)", pub["value"], want)
	}
}
