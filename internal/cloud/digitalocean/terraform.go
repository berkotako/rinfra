package digitalocean

import (
	"fmt"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration/terraform"
)

// BuildConfig implements terraform.Builder. It emits the same resources the
// Pulumi BuildProgram creates — a tagged Droplet plus a bound Reserved IP per
// node — as Terraform JSON. The digitalocean provider reads DIGITALOCEAN_TOKEN
// from the environment.
func (p *provider) BuildConfig(engagementID string, _ cloud.Credentials, nodes []domain.Node) (*terraform.Config, error) {
	engTag := TagPrefix + engagementID
	droplets := map[string]any{}
	reservedIPs := map[string]any{}
	outputs := map[string]any{}

	for _, n := range nodes {
		nodeTag := TagPrefix + "node:" + n.ID
		nodeName := fmt.Sprintf("rinfra-%s-%s", shortID(engagementID), shortID(n.ID))
		size := n.Spec.Size
		if size == "" {
			size = "s-1vcpu-1gb"
		}
		region := n.Spec.Region
		if region == "" {
			region = "nyc3"
		}
		key := terraform.SafeName(n.ID)
		droplets[key] = map[string]any{
			"image":  DefaultImage,
			"name":   nodeName,
			"region": region,
			"size":   size,
			"tags":   []string{engTag, nodeTag},
		}
		// Stable Reserved IP bound to the droplet — the durable address exported as
		// PublicIP (the droplet's own ipv4_address is ephemeral). Mirrors the Pulumi
		// BuildProgram and the AWS/GCP/Azure static-address posture.
		reservedIPs[key] = map[string]any{
			"region":     region,
			"droplet_id": fmt.Sprintf("${digitalocean_droplet.%s.id}", key),
		}
		outputs[terraform.ProviderRefOutput(n.ID)] = map[string]any{
			"value": fmt.Sprintf("${digitalocean_droplet.%s.id}", key),
		}
		outputs[terraform.PublicIPOutput(n.ID)] = map[string]any{
			"value": fmt.Sprintf("${digitalocean_reserved_ip.%s.ip_address}", key),
		}
	}

	return &terraform.Config{
		Terraform: map[string]any{
			"required_providers": map[string]any{
				"digitalocean": map[string]any{"source": "digitalocean/digitalocean"},
			},
		},
		Provider: map[string]any{"digitalocean": map[string]any{}},
		Resource: map[string]any{
			"digitalocean_droplet":     droplets,
			"digitalocean_reserved_ip": reservedIPs,
		},
		Output: outputs,
	}, nil
}

var _ terraform.Builder = (*provider)(nil)
