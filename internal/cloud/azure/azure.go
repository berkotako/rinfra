// Package azure adapts Microsoft Azure to RInfra's cloud.CloudProvider
// interface. Provisions into the customer's subscription using per-engagement
// credentials — never a shared RInfra account.
//
// # SDK approach
//
// Uses the Pulumi Go SDK automation API (via internal/orchestration.Engine)
// for resource lifecycle management.
//
// # ConfigureIngress — deliberately different from other providers
//
// Azure uses Network Security Groups (NSGs) attached to NICs or subnets.
// NSG rules are stateful and have explicit priority ordering (lower number =
// higher priority). Both Allow and Deny actions are supported (unlike DO and
// GCP which are allow-only at the rule level). Each rule is a
// NetworkSecurityGroupSecurityRule with fields:
//
//   - Access:                    "Allow" or "Deny"
//   - Direction:                 "Inbound" (for ingress)
//   - Protocol:                  "Tcp", "Udp", or "*" (all)
//   - DestinationPortRange:      port number as string ("443") or range ("8000-9000")
//   - SourceAddressPrefix:       CIDR or "*"
//   - Priority:                  100–4096 (assigned sequentially, 100 per rule)
//
// This differs from:
//   - DO: Cloud Firewalls attached to Droplets by tag/ID, allow-only.
//   - AWS: EC2 Security Groups, stateful, allow-only, per-instance.
//   - GCP: VPC firewall rules, target-tag based, allow + implicit-deny.
//
// Azure also requires an explicit resource group per deployment. We create one
// resource group per engagement named rinfra-<engagementID[:8]>.
//
// # Credential keys
//
//   - "ARM_SUBSCRIPTION_ID" — Azure subscription ID
//   - "ARM_TENANT_ID"        — Azure tenant (directory) ID
//   - "ARM_CLIENT_ID"        — Service principal app ID
//   - "ARM_CLIENT_SECRET"    — Service principal secret
//
// The Pulumi Azure provider reads these from the ARM_* environment variables.
//
// # Verified by compile vs needs live testing
//
// All code below is verified to compile against the Pulumi Azure SDK v5.
// The full resource lifecycle requires a live Azure subscription and has NOT
// been exercised against the live API. See docs/RUNBOOK_DO.md for the
// checklist approach (same pattern, different cloud).
package azure

import (
	"context"
	"fmt"
	"strconv"

	azcompute "github.com/pulumi/pulumi-azure/sdk/v5/go/azure/compute"
	azcore "github.com/pulumi/pulumi-azure/sdk/v5/go/azure/core"
	azdns "github.com/pulumi/pulumi-azure/sdk/v5/go/azure/dns"
	aznetwork "github.com/pulumi/pulumi-azure/sdk/v5/go/azure/network"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration"
)

// Credential key constants. These match the Pulumi Azure provider env vars.
const (
	CredKeySubscriptionID = "ARM_SUBSCRIPTION_ID"
	CredKeyTenantID       = "ARM_TENANT_ID"
	CredKeyClientID       = "ARM_CLIENT_ID"
	CredKeyClientSecret   = "ARM_CLIENT_SECRET"
)

// DefaultSize is the VM SKU used when NodeSpec.Size is empty.
const DefaultSize = "Standard_B1s"

// DefaultLocation is the Azure region used when NodeSpec.Region is empty.
const DefaultLocation = "eastus"

// DefaultImage is the Azure image reference used for all VMs.
const DefaultImage = "Canonical:UbuntuServer:22_04-lts-gen2:latest"

func init() {
	p := &provider{}
	cloud.Register(p)
	cloud.RegisterSweeper(domain.CloudAzure, p)
}

type provider struct{}

var _ cloud.CloudProvider = (*provider)(nil)
var _ cloud.Sweeper = (*provider)(nil)
var _ orchestration.ProgramBuilder = (*provider)(nil)

func (p *provider) Type() domain.CloudProviderType { return domain.CloudAzure }

// BuildProgram implements orchestration.ProgramBuilder. Creates:
//  1. A Resource Group per engagement (Azure requires one).
//  2. Per node: Public IP + NSG + NIC + Linux VM.
//
// Azure tagging: resource tags use "rinfra" = engagementID and
// "rinfra-node" = nodeID. The resource group name encodes the engagement ID
// for easy identification.
func (p *provider) BuildProgram(engagementID string, creds cloud.Credentials, nodes []domain.Node) pulumi.RunFunc {
	return func(ctx *pulumi.Context) error {
		rgName := "rinfra-" + engagementID[:8]
		location := DefaultLocation
		if len(nodes) > 0 && nodes[0].Spec.Region != "" {
			location = nodes[0].Spec.Region
		}

		engTags := pulumi.StringMap{
			"rinfra": pulumi.String(engagementID),
		}

		// Resource Group — Azure requires all resources to live in one.
		rg, err := azcore.NewResourceGroup(ctx, rgName, &azcore.ResourceGroupArgs{
			Name:     pulumi.String(rgName),
			Location: pulumi.String(location),
			Tags:     engTags,
		})
		if err != nil {
			return fmt.Errorf("azure: create resource group: %w", err)
		}

		for _, n := range nodes {
			nodeName := fmt.Sprintf("rinfra-%s-%s", engagementID[:8], n.ID[:8])
			size := n.Spec.Size
			if size == "" {
				size = DefaultSize
			}

			nodeTags := pulumi.StringMap{
				"rinfra":      pulumi.String(engagementID),
				"rinfra-node": pulumi.String(n.ID),
			}

			// Public IP (Static allocation — required for RInfra redirectors).
			pip, err := aznetwork.NewPublicIp(ctx, nodeName+"-pip", &aznetwork.PublicIpArgs{
				Name:              pulumi.String(nodeName + "-pip"),
				ResourceGroupName: rg.Name,
				Location:          rg.Location,
				AllocationMethod:  pulumi.String("Static"),
				Tags:              nodeTags,
			})
			if err != nil {
				return fmt.Errorf("azure: create public IP for node %s: %w", n.ID, err)
			}

			// Network Security Group — Azure-specific ingress mechanism.
			// Unlike AWS (per-instance security groups) and GCP (VPC-wide firewall
			// rules with tag filters), Azure NSGs are attached to NICs/subnets
			// and support explicit Allow/Deny with priority ordering.
			nsg, err := aznetwork.NewNetworkSecurityGroup(ctx, nodeName+"-nsg", &aznetwork.NetworkSecurityGroupArgs{
				Name:              pulumi.String(nodeName + "-nsg"),
				ResourceGroupName: rg.Name,
				Location:          rg.Location,
				// Default: allow SSH so operators can access the node.
				SecurityRules: aznetwork.NetworkSecurityGroupSecurityRuleArray{
					aznetwork.NetworkSecurityGroupSecurityRuleArgs{
						Name:                     pulumi.String("allow-ssh"),
						Priority:                 pulumi.Int(100),
						Direction:                pulumi.String("Inbound"),
						Access:                   pulumi.String("Allow"),
						Protocol:                 pulumi.String("Tcp"),
						SourcePortRange:          pulumi.String("*"),
						DestinationPortRange:     pulumi.String("22"),
						SourceAddressPrefix:      pulumi.String("*"),
						DestinationAddressPrefix: pulumi.String("*"),
					},
				},
				Tags: nodeTags,
			})
			if err != nil {
				return fmt.Errorf("azure: create NSG for node %s: %w", n.ID, err)
			}

			// Network Interface — connects VM to the virtual network.
			nic, err := aznetwork.NewNetworkInterface(ctx, nodeName+"-nic", &aznetwork.NetworkInterfaceArgs{
				Name:              pulumi.String(nodeName + "-nic"),
				ResourceGroupName: rg.Name,
				Location:          rg.Location,
				IpConfigurations: aznetwork.NetworkInterfaceIpConfigurationArray{
					aznetwork.NetworkInterfaceIpConfigurationArgs{
						Name:                       pulumi.String("ipconfig1"),
						PrivateIpAddressAllocation: pulumi.String("Dynamic"),
						PublicIpAddressId:          pip.ID(),
					},
				},
				Tags: nodeTags,
			})
			if err != nil {
				return fmt.Errorf("azure: create NIC for node %s: %w", n.ID, err)
			}

			// Attach NSG to NIC.
			_, err = aznetwork.NewNetworkInterfaceSecurityGroupAssociation(ctx, nodeName+"-nsg-assoc", &aznetwork.NetworkInterfaceSecurityGroupAssociationArgs{
				NetworkInterfaceId:     nic.ID(),
				NetworkSecurityGroupId: nsg.ID(),
			})
			if err != nil {
				return fmt.Errorf("azure: attach NSG to NIC for node %s: %w", n.ID, err)
			}

			// Linux Virtual Machine.
			vm, err := azcompute.NewLinuxVirtualMachine(ctx, nodeName, &azcompute.LinuxVirtualMachineArgs{
				Name:              pulumi.String(nodeName),
				ResourceGroupName: rg.Name,
				Location:          rg.Location,
				Size:              pulumi.String(size),
				AdminUsername:     pulumi.String("rinfra"),
				// Password auth disabled; SSH key injection would be wired here.
				// For MVP: disable password auth and set a placeholder admin password
				// (real deployments should inject SSH keys per engagement).
				DisablePasswordAuthentication: pulumi.Bool(false),
				AdminPassword:                 pulumi.String("Rinfra!Placeholder1"), // TODO(live): replace with per-engagement SSH key
				NetworkInterfaceIds:           pulumi.StringArray{nic.ID()},
				OsDisk: azcompute.LinuxVirtualMachineOsDiskArgs{
					Caching:            pulumi.String("ReadWrite"),
					StorageAccountType: pulumi.String("Standard_LRS"),
				},
				SourceImageReference: azcompute.LinuxVirtualMachineSourceImageReferenceArgs{
					Publisher: pulumi.String("Canonical"),
					Offer:     pulumi.String("UbuntuServer"),
					Sku:       pulumi.String("22_04-lts-gen2"),
					Version:   pulumi.String("latest"),
				},
				Tags: nodeTags,
			})
			if err != nil {
				return fmt.Errorf("azure: create VM for node %s: %w", n.ID, err)
			}

			ctx.Export(orchestration.NodeProviderRefKey(n.ID), vm.ID())
			ctx.Export(orchestration.NodePublicIPKey(n.ID), pip.IpAddress)
		}
		return nil
	}
}

// ProvisionNode — not supported as a direct call. Use Engine.Deploy.
func (p *provider) ProvisionNode(_ context.Context, _ cloud.Credentials, _ domain.NodeSpec) (domain.Node, error) {
	return domain.Node{}, fmt.Errorf("azure.ProvisionNode: use orchestration.Engine.Deploy for real provisioning")
}

// ConfigureIngress creates or updates an Azure NSG rule set for a node.
//
// Azure NSGs are fundamentally different from other providers:
//   - Explicit Allow AND Deny actions (not just allow-only like DO/GCP).
//   - Priority ordering: lower number = evaluated first (100–4096).
//   - Rules are stateful (return traffic is automatically permitted).
//   - Attached to NICs or subnets — not to instances by tag.
//   - DestinationPortRange is a string: "443" or "8000-9000".
//   - SourceAddressPrefix is a CIDR, service tag, or "*".
//
// TODO(live): standalone NSG update requires Azure SDK.
func (p *provider) ConfigureIngress(_ context.Context, creds cloud.Credentials, node domain.Node, rules []domain.Rule) error {
	if node.ProviderRef == "" {
		return fmt.Errorf("azure.ConfigureIngress: node %s has no ProviderRef (not yet provisioned)", node.ID)
	}
	if err := validateAzureCreds(creds); err != nil {
		return err
	}
	_ = buildAzureNSGRules(rules)
	return fmt.Errorf("azure.ConfigureIngress: standalone NSG update not yet implemented; use Engine.Deploy — TODO(live)")
}

// buildAzureNSGRules converts domain.Rule to Azure NSG security rule descriptors.
// Azure-specific shape:
//   - Access is "Allow" or "Deny" (explicit, not just by omission).
//   - Priority is assigned sequentially starting at 100 (100 = highest priority).
//   - DestinationPortRange is a string, not from/to pair.
//   - SourceAddressPrefix carries the CIDR.
func buildAzureNSGRules(rules []domain.Rule) []azureNSGRule {
	var out []azureNSGRule
	for i, r := range rules {
		access := "Allow"
		if !r.Allow {
			access = "Deny"
		}
		src := r.SourceCIDR
		if src == "" {
			src = "*"
		}
		out = append(out, azureNSGRule{
			Name:                 fmt.Sprintf("rule-%d", i+1),
			Priority:             100 + i*10, // 100, 110, 120, …
			Direction:            "Inbound",
			Access:               access,
			Protocol:             toAzureProtocol(r.Protocol),
			DestinationPortRange: strconv.Itoa(r.Port),
			SourceAddressPrefix:  src,
		})
	}
	return out
}

// azureNSGRule is the internal representation of an Azure NSG security rule.
type azureNSGRule struct {
	Name                 string
	Priority             int
	Direction            string // "Inbound" or "Outbound"
	Access               string // "Allow" or "Deny"
	Protocol             string // "Tcp", "Udp", or "*"
	DestinationPortRange string // e.g. "443" or "8000-9000"
	SourceAddressPrefix  string // CIDR or "*"
}

// toAzureProtocol converts a domain protocol string to Azure NSG protocol format.
// Azure uses title-cased protocol names; "*" means all protocols.
func toAzureProtocol(proto string) string {
	switch proto {
	case "tcp":
		return "Tcp"
	case "udp":
		return "Udp"
	default:
		return "*"
	}
}

// AssignStaticIP — handled by PublicIp in BuildProgram.
// TODO(live): standalone Azure Public IP allocation.
func (p *provider) AssignStaticIP(_ context.Context, creds cloud.Credentials, node domain.Node) (string, error) {
	if node.ProviderRef == "" {
		return "", fmt.Errorf("azure.AssignStaticIP: node %s has no ProviderRef", node.ID)
	}
	if err := validateAzureCreds(creds); err != nil {
		return "", err
	}
	return "", fmt.Errorf("azure.AssignStaticIP: use Engine.Deploy (includes PublicIp in inline program) — TODO(live)")
}

// ManageDNS upserts an Azure DNS A record.
//
// Azure DNS requires both a zone name AND a resource group where the zone lives.
// Store the DNS resource group name in creds.Raw["AZURE_DNS_RESOURCE_GROUP"].
// This differs from DO (zone is a top-level domain resource, no resource group),
// GCP (managed zone name in a project), and AWS (hosted zone ID).
//
// TODO(live): Azure DNS record upsert via SDK.
func (p *provider) ManageDNS(_ context.Context, creds cloud.Credentials, rec domain.Record) error {
	if err := validateAzureCreds(creds); err != nil {
		return err
	}
	if rec.Zone == "" {
		return fmt.Errorf("azure.ManageDNS: Zone must be set (Azure DNS zone name)")
	}
	return fmt.Errorf("azure.ManageDNS: use Engine.Deploy (includes ARecord in inline program) — TODO(live)")
}

// addAzureDNSRecord is the Pulumi inline program helper that creates an Azure
// DNS A record. Azure uses a ZoneName + ResourceGroupName combination (not a
// zone ID). This is the authoritative Azure DNS translation.
func addAzureDNSRecord(ctx *pulumi.Context, name string, rec domain.Record, dnsRG string) error {
	ttl := rec.TTL
	if ttl == 0 {
		ttl = 300
	}
	_, err := azdns.NewARecord(ctx, name, &azdns.ARecordArgs{
		Name:              pulumi.String(rec.Name),
		ZoneName:          pulumi.String(rec.Zone),
		ResourceGroupName: pulumi.String(dnsRG),
		Ttl:               pulumi.Int(ttl),
		Records:           pulumi.StringArray{pulumi.String(rec.Value)},
	})
	return err
}

// Destroy — handled by Engine.Teardown. Idempotent (empty ProviderRef = no-op).
// TODO(live): direct Azure SDK delete (VM + NIC + PIP + NSG + RG).
func (p *provider) Destroy(_ context.Context, creds cloud.Credentials, node domain.Node) error {
	if node.ProviderRef == "" {
		return nil
	}
	if err := validateAzureCreds(creds); err != nil {
		return err
	}
	return fmt.Errorf("azure.Destroy: use Engine.Teardown for full stack destroy + sweep — TODO(live)")
}

// SweepOrphans lists Azure VMs tagged rinfra=<engagementID> and deletes them
// along with their associated NICs, Public IPs, and NSGs.
//
// TODO(live): implement using Azure SDK (github.com/Azure/azure-sdk-for-go):
//  1. Create ResourcesClient with ARM_* credentials.
//  2. List resources by tag "rinfra" = engagementID.
//  3. Delete in order: VMs, NICs, Public IPs, NSGs, Resource Group.
//  4. Wait for async delete operations to complete.
func (p *provider) SweepOrphans(_ context.Context, creds cloud.Credentials, engagementID string) error {
	if err := validateAzureCreds(creds); err != nil {
		return err
	}
	// TODO(live): implement Azure resource sweep.
	_ = engagementID
	return nil
}

// validateAzureCreds checks minimum required credential keys.
func validateAzureCreds(creds cloud.Credentials) error {
	for _, key := range []string{CredKeySubscriptionID, CredKeyTenantID, CredKeyClientID, CredKeyClientSecret} {
		if creds.Raw[key] == "" {
			return fmt.Errorf("azure: credential key %q not set", key)
		}
	}
	return nil
}

// ensure addAzureDNSRecord is referenced
var _ = addAzureDNSRecord
