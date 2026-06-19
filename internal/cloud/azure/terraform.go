package azure

import (
	"fmt"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration/terraform"
)

// BuildConfig implements terraform.Builder. It mirrors the Pulumi BuildProgram:
// one resource group, then per node a static public IP, an NSG (allow-SSH), a
// NIC, an NSG↔NIC association, and a Linux VM. The azurerm provider reads
// ARM_SUBSCRIPTION_ID/ARM_TENANT_ID/ARM_CLIENT_ID/ARM_CLIENT_SECRET from env.
func (p *provider) BuildConfig(engagementID string, _ cloud.Credentials, nodes []domain.Node) (*terraform.Config, error) {
	rgName := "rinfra-" + engagementID[:8]
	location := DefaultLocation
	if len(nodes) > 0 && nodes[0].Spec.Region != "" {
		location = nodes[0].Spec.Region
	}
	rgRef := "${azurerm_resource_group.rg.name}"
	locRef := "${azurerm_resource_group.rg.location}"

	pips := map[string]any{}
	nsgs := map[string]any{}
	nics := map[string]any{}
	assocs := map[string]any{}
	vms := map[string]any{}
	outputs := map[string]any{}

	for _, n := range nodes {
		nodeName := fmt.Sprintf("rinfra-%s-%s", engagementID[:8], n.ID[:8])
		size := n.Spec.Size
		if size == "" {
			size = DefaultSize
		}
		key := terraform.SafeName(n.ID)
		nodeTags := map[string]any{"rinfra": engagementID, "rinfra-node": n.ID}

		pips[key] = map[string]any{
			"name":                nodeName + "-pip",
			"resource_group_name": rgRef,
			"location":            locRef,
			"allocation_method":   "Static",
			"tags":                nodeTags,
		}
		nsgs[key] = map[string]any{
			"name":                nodeName + "-nsg",
			"resource_group_name": rgRef,
			"location":            locRef,
			"security_rule": []any{map[string]any{
				"name":                       "allow-ssh",
				"priority":                   100,
				"direction":                  "Inbound",
				"access":                     "Allow",
				"protocol":                   "Tcp",
				"source_port_range":          "*",
				"destination_port_range":     "22",
				"source_address_prefix":      "*",
				"destination_address_prefix": "*",
			}},
			"tags": nodeTags,
		}
		nics[key] = map[string]any{
			"name":                nodeName + "-nic",
			"resource_group_name": rgRef,
			"location":            locRef,
			"ip_configuration": []any{map[string]any{
				"name":                          "ipconfig1",
				"private_ip_address_allocation": "Dynamic",
				"public_ip_address_id":          fmt.Sprintf("${azurerm_public_ip.%s.id}", key),
			}},
			"tags": nodeTags,
		}
		assocs[key] = map[string]any{
			"network_interface_id":      fmt.Sprintf("${azurerm_network_interface.%s.id}", key),
			"network_security_group_id": fmt.Sprintf("${azurerm_network_security_group.%s.id}", key),
		}
		vms[key] = map[string]any{
			"name":                            nodeName,
			"resource_group_name":             rgRef,
			"location":                        locRef,
			"size":                            size,
			"admin_username":                  "rinfra",
			"disable_password_authentication": false,
			// TODO(live): replace with a per-engagement SSH key.
			"admin_password":        "Rinfra!Placeholder1",
			"network_interface_ids": []string{fmt.Sprintf("${azurerm_network_interface.%s.id}", key)},
			"os_disk": []any{map[string]any{
				"caching":              "ReadWrite",
				"storage_account_type": "Standard_LRS",
			}},
			"source_image_reference": []any{map[string]any{
				"publisher": "Canonical",
				"offer":     "UbuntuServer",
				"sku":       "22_04-lts-gen2",
				"version":   "latest",
			}},
			"tags": nodeTags,
		}
		outputs[terraform.ProviderRefOutput(n.ID)] = map[string]any{
			"value": fmt.Sprintf("${azurerm_linux_virtual_machine.%s.id}", key),
		}
		outputs[terraform.PublicIPOutput(n.ID)] = map[string]any{
			"value": fmt.Sprintf("${azurerm_public_ip.%s.ip_address}", key),
		}
	}

	return &terraform.Config{
		Terraform: map[string]any{
			"required_providers": map[string]any{
				"azurerm": map[string]any{"source": "hashicorp/azurerm"},
			},
		},
		Provider: map[string]any{"azurerm": map[string]any{"features": map[string]any{}}},
		Resource: map[string]any{
			"azurerm_resource_group": map[string]any{
				"rg": map[string]any{
					"name":     rgName,
					"location": location,
					"tags":     map[string]any{"rinfra": engagementID},
				},
			},
			"azurerm_public_ip":                                    pips,
			"azurerm_network_security_group":                       nsgs,
			"azurerm_network_interface":                            nics,
			"azurerm_network_interface_security_group_association": assocs,
			"azurerm_linux_virtual_machine":                        vms,
		},
		Output: outputs,
	}, nil
}

var _ terraform.Builder = (*provider)(nil)
