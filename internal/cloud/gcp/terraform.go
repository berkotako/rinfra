package gcp

import (
	"fmt"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration/terraform"
)

// BuildConfig implements terraform.Builder. It mirrors the Pulumi BuildProgram:
// a regional static Address plus a Compute instance per node, with network tags
// for firewall targeting and rinfra labels. The google provider reads
// GOOGLE_CREDENTIALS and GOOGLE_PROJECT from the environment.
func (p *provider) BuildConfig(engagementID string, creds cloud.Credentials, nodes []domain.Node) (*terraform.Config, error) {
	project := creds.Raw[CredKeyProject]

	addresses := map[string]any{}
	instances := map[string]any{}
	outputs := map[string]any{}

	for _, n := range nodes {
		nodeName := fmt.Sprintf("rinfra-%s-%s", engagementID[:8], n.ID[:8])
		netTag := "rinfra-" + n.ID[:8]
		engTag := "rinfra-" + engagementID[:8]
		machineType := n.Spec.Size
		if machineType == "" {
			machineType = DefaultMachineType
		}
		zone := n.Spec.Region
		if zone == "" {
			zone = "us-central1-a"
		}
		region := zone
		if len(zone) > 2 && zone[len(zone)-2] == '-' {
			region = zone[:len(zone)-2]
		}
		key := terraform.SafeName(n.ID)
		labels := map[string]any{"rinfra": engagementID, "rinfra-node": n.ID}

		addresses[key] = map[string]any{
			"name":   nodeName + "-ip",
			"region": region,
			"labels": labels,
		}
		instances[key] = map[string]any{
			"name":         nodeName,
			"machine_type": machineType,
			"zone":         zone,
			"tags":         []string{netTag, engTag},
			"labels":       labels,
			"boot_disk": []any{map[string]any{
				"initialize_params": []any{map[string]any{"image": DefaultImage}},
			}},
			"network_interface": []any{map[string]any{
				"network": "default",
				"access_config": []any{map[string]any{
					"nat_ip": fmt.Sprintf("${google_compute_address.%s.address}", key),
				}},
			}},
		}
		outputs[terraform.ProviderRefOutput(n.ID)] = map[string]any{
			"value": fmt.Sprintf("${google_compute_instance.%s.instance_id}", key),
		}
		outputs[terraform.PublicIPOutput(n.ID)] = map[string]any{
			"value": fmt.Sprintf("${google_compute_address.%s.address}", key),
		}
	}

	providerCfg := map[string]any{}
	if project != "" {
		providerCfg["project"] = project
	}

	return &terraform.Config{
		Terraform: map[string]any{
			"required_providers": map[string]any{
				"google": map[string]any{"source": "hashicorp/google"},
			},
		},
		Provider: map[string]any{"google": providerCfg},
		Resource: map[string]any{
			"google_compute_address":  addresses,
			"google_compute_instance": instances,
		},
		Output: outputs,
	}, nil
}

var _ terraform.Builder = (*provider)(nil)
