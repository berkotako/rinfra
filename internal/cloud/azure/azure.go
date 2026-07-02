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
//   - "RINFRA_SSH_PUBLIC_KEY"— OpenSSH public key injected into every VM
//
// The Pulumi Azure provider reads the ARM_* values from environment variables.
// The standalone CloudProvider/Sweeper methods drive the Azure Resource Manager
// SDK (github.com/Azure/azure-sdk-for-go) directly: they build an
// azidentity.ClientSecretCredential from the ARM_* creds and call the
// armnetwork / armdns / armresources clients for out-of-band reconciliation and
// the guaranteed-teardown sweep that runs after every engine destroy.
//
// # SSH-key hardening
//
// VMs are provisioned with SSH public-key auth only: password authentication is
// disabled and the operator's per-engagement public key
// (RINFRA_SSH_PUBLIC_KEY) is injected. A VM is never provisioned with a
// password — if the key is absent, provisioning fails closed.
package azure

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	azpolicy "github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	pazcompute "github.com/pulumi/pulumi-azure/sdk/v5/go/azure/compute"
	pazcore "github.com/pulumi/pulumi-azure/sdk/v5/go/azure/core"
	pazdns "github.com/pulumi/pulumi-azure/sdk/v5/go/azure/dns"
	paznetwork "github.com/pulumi/pulumi-azure/sdk/v5/go/azure/network"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration"
)

// Credential key constants. The ARM_* keys match the Pulumi Azure provider env
// vars; RINFRA_SSH_PUBLIC_KEY carries the per-engagement OpenSSH public key.
const (
	CredKeySubscriptionID = "ARM_SUBSCRIPTION_ID"
	CredKeyTenantID       = "ARM_TENANT_ID"
	CredKeyClientID       = "ARM_CLIENT_ID"
	CredKeyClientSecret   = "ARM_CLIENT_SECRET"
	CredKeySSHPublicKey   = "RINFRA_SSH_PUBLIC_KEY"
)

// DefaultSize is the VM SKU used when NodeSpec.Size is empty.
const DefaultSize = "Standard_B1s"

// DefaultLocation is the Azure region used when NodeSpec.Region is empty.
const DefaultLocation = "eastus"

// DefaultImage is the Azure image reference used for all VMs.
const DefaultImage = "Canonical:UbuntuServer:22_04-lts-gen2:latest"

// AdminUsername is the Linux admin account created on every VM. SSH key auth
// only — see sshPublicKey.
const AdminUsername = "rinfra"

// linuxAuthConfig is the resolved authentication posture for a VM. RInfra always
// disables password auth and injects an SSH public key; this struct is the
// testable, framework-independent representation of that decision.
type linuxAuthConfig struct {
	AdminUsername             string
	DisablePasswordAuth       bool
	SSHPublicKey              string
	AdminUsernameForPublicKey string
}

// sshPublicKey extracts the per-engagement OpenSSH public key from creds. RInfra
// provisions SSH-key-only VMs; if the key is absent we fail closed rather than
// fall back to a password VM (a security regression). The key is validated as a
// plausible OpenSSH public key (ssh-rsa / ssh-ed25519 / ecdsa-… prefix).
func sshPublicKey(creds cloud.Credentials) (string, error) {
	key := strings.TrimSpace(creds.Raw[CredKeySSHPublicKey])
	if key == "" {
		return "", fmt.Errorf("azure: %s not set — refusing to provision a password VM; supply a per-engagement SSH public key", CredKeySSHPublicKey)
	}
	if !looksLikeSSHPublicKey(key) {
		return "", fmt.Errorf("azure: %s does not look like an OpenSSH public key", CredKeySSHPublicKey)
	}
	return key, nil
}

// looksLikeSSHPublicKey is a cheap sanity check on an OpenSSH public key string.
func looksLikeSSHPublicKey(key string) bool {
	for _, prefix := range []string{"ssh-rsa ", "ssh-ed25519 ", "ecdsa-sha2-", "sk-ssh-", "sk-ecdsa-"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// buildLinuxAuthConfig resolves the SSH-key-only auth posture for a VM. It is
// the authoritative, framework-independent translation and is tested directly:
// password auth is always disabled and a public key is always required.
func buildLinuxAuthConfig(creds cloud.Credentials) (linuxAuthConfig, error) {
	key, err := sshPublicKey(creds)
	if err != nil {
		return linuxAuthConfig{}, err
	}
	return linuxAuthConfig{
		AdminUsername:             AdminUsername,
		DisablePasswordAuth:       true,
		SSHPublicKey:              key,
		AdminUsernameForPublicKey: AdminUsername,
	}, nil
}

func init() {
	// The registered provider talks to the live Azure Resource Manager: nil
	// transport/cred means client() builds a real azidentity credential.
	p := &provider{}
	cloud.Register(p)
	cloud.RegisterSweeper(domain.CloudAzure, p)
}

// provider is the RInfra Azure cloud adapter. It implements CloudProvider and
// Sweeper and acts as the ProgramBuilder / terraform.Builder for the engine.
//
// transport and cred exist purely for tests: when set they override the HTTP
// transport and token credential used to build the ARM clients, letting unit
// tests point the SDK at an httptest server and a dummy credential. In
// production both are nil and client() builds an azidentity credential from the
// ARM_* engagement creds and uses the SDK's default transport.
type provider struct {
	transport azpolicy.Transporter
	cred      azcore.TokenCredential
}

var _ cloud.CloudProvider = (*provider)(nil)
var _ cloud.Sweeper = (*provider)(nil)
var _ orchestration.ProgramBuilder = (*provider)(nil)

func (p *provider) Type() domain.CloudProviderType { return domain.CloudAzure }

// azClients bundles the ARM clients RInfra drives directly, all built against
// the same subscription, credential, and (optional test) transport.
type azClients struct {
	subscriptionID string
	securityRules  *armnetwork.SecurityRulesClient
	publicIPs      *armnetwork.PublicIPAddressesClient
	recordSets     *armdns.RecordSetsClient
	resourceGroups *armresources.ResourceGroupsClient
}

// clientOptions returns arm.ClientOptions wired with the test transport when one
// is configured; nil transport selects the SDK default (live Azure).
func (p *provider) clientOptions() *arm.ClientOptions {
	if p.transport == nil {
		return nil
	}
	return &arm.ClientOptions{ClientOptions: azcore.ClientOptions{Transport: p.transport}}
}

// credential returns the token credential for ARM calls. Tests inject a dummy
// via p.cred; production builds an azidentity.ClientSecretCredential from the
// ARM_* engagement credentials.
func (p *provider) credential(creds cloud.Credentials) (azcore.TokenCredential, error) {
	if p.cred != nil {
		return p.cred, nil
	}
	c, err := azidentity.NewClientSecretCredential(
		creds.Raw[CredKeyTenantID],
		creds.Raw[CredKeyClientID],
		creds.Raw[CredKeyClientSecret],
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("azure: build client-secret credential: %w", err)
	}
	return c, nil
}

// clients validates creds and constructs the ARM clients for the engagement
// subscription. All clients share the credential and (test) transport.
func (p *provider) clients(creds cloud.Credentials) (*azClients, error) {
	if err := validateAzureCreds(creds); err != nil {
		return nil, err
	}
	cred, err := p.credential(creds)
	if err != nil {
		return nil, err
	}
	sub := creds.Raw[CredKeySubscriptionID]
	opts := p.clientOptions()

	sr, err := armnetwork.NewSecurityRulesClient(sub, cred, opts)
	if err != nil {
		return nil, fmt.Errorf("azure: new security-rules client: %w", err)
	}
	pip, err := armnetwork.NewPublicIPAddressesClient(sub, cred, opts)
	if err != nil {
		return nil, fmt.Errorf("azure: new public-ip client: %w", err)
	}
	rs, err := armdns.NewRecordSetsClient(sub, cred, opts)
	if err != nil {
		return nil, fmt.Errorf("azure: new record-sets client: %w", err)
	}
	rg, err := armresources.NewResourceGroupsClient(sub, cred, opts)
	if err != nil {
		return nil, fmt.Errorf("azure: new resource-groups client: %w", err)
	}
	return &azClients{
		subscriptionID: sub,
		securityRules:  sr,
		publicIPs:      pip,
		recordSets:     rs,
		resourceGroups: rg,
	}, nil
}

// resourceGroupName returns the engagement's resource group name. BuildProgram
// creates exactly one RG per engagement named rinfra-<engagementID[:8]>, tagged
// rinfra=<engagementID>; the standalone methods must agree on this scheme.
func resourceGroupName(engagementID string) string {
	return "rinfra-" + shortID(engagementID)
}

// nodeResourceName returns the per-node base name BuildProgram assigns:
// rinfra-<engagementID[:8]>-<nodeID[:8]>.
func nodeResourceName(engagementID, nodeID string) string {
	return fmt.Sprintf("rinfra-%s-%s", shortID(engagementID), shortID(nodeID))
}

// shortID returns the first 8 chars of an id (or the whole id if shorter).
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// isAzureNotFound reports whether err is an ARM 404 (resource already gone).
func isAzureNotFound(err error) bool {
	var re *azcore.ResponseError
	if errors.As(err, &re) {
		return re.StatusCode == http.StatusNotFound
	}
	return false
}

// strPtr returns a pointer to s (Azure SDK structs use pointer fields).
func strPtr(s string) *string { return &s }

// BuildProgram implements orchestration.ProgramBuilder. Creates:
//  1. A Resource Group per engagement (Azure requires one).
//  2. Per node: Public IP + NSG + NIC + Linux VM.
//
// Azure tagging: resource tags use "rinfra" = engagementID and
// "rinfra-node" = nodeID. The resource group name encodes the engagement ID
// for easy identification.
func (p *provider) BuildProgram(engagementID string, creds cloud.Credentials, nodes []domain.Node) pulumi.RunFunc {
	return func(ctx *pulumi.Context) error {
		sshKey, err := sshPublicKey(creds)
		if err != nil {
			return err
		}

		rgName := resourceGroupName(engagementID)
		// The resource group needs a home region; use the first node's region (or
		// the default). Each node's own resources, however, are placed in that
		// node's region below — a resource group can span regions, so a node with a
		// different Spec.Region must NOT be silently relocated into the RG's region.
		location := DefaultLocation
		if len(nodes) > 0 && nodes[0].Spec.Region != "" {
			location = nodes[0].Spec.Region
		}

		engTags := pulumi.StringMap{
			"rinfra": pulumi.String(engagementID),
		}

		// Resource Group — Azure requires all resources to live in one.
		rg, err := pazcore.NewResourceGroup(ctx, rgName, &pazcore.ResourceGroupArgs{
			Name:     pulumi.String(rgName),
			Location: pulumi.String(location),
			Tags:     engTags,
		})
		if err != nil {
			return fmt.Errorf("azure: create resource group: %w", err)
		}

		for _, n := range nodes {
			nodeName := nodeResourceName(engagementID, n.ID)
			size := n.Spec.Size
			if size == "" {
				size = DefaultSize
			}
			// Honor each node's own region; fall back to the RG's region only when
			// the node doesn't specify one.
			nodeLocation := pulumi.String(location).ToStringOutput()
			if n.Spec.Region != "" {
				nodeLocation = pulumi.String(n.Spec.Region).ToStringOutput()
			}

			nodeTags := pulumi.StringMap{
				"rinfra":      pulumi.String(engagementID),
				"rinfra-node": pulumi.String(n.ID),
			}

			// Public IP (Static allocation — required for RInfra redirectors).
			pip, err := paznetwork.NewPublicIp(ctx, nodeName+"-pip", &paznetwork.PublicIpArgs{
				Name:              pulumi.String(nodeName + "-pip"),
				ResourceGroupName: rg.Name,
				Location:          nodeLocation,
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
			nsg, err := paznetwork.NewNetworkSecurityGroup(ctx, nodeName+"-nsg", &paznetwork.NetworkSecurityGroupArgs{
				Name:              pulumi.String(nodeName + "-nsg"),
				ResourceGroupName: rg.Name,
				Location:          nodeLocation,
				// Default: allow SSH so operators can access the node.
				SecurityRules: paznetwork.NetworkSecurityGroupSecurityRuleArray{
					paznetwork.NetworkSecurityGroupSecurityRuleArgs{
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
			nic, err := paznetwork.NewNetworkInterface(ctx, nodeName+"-nic", &paznetwork.NetworkInterfaceArgs{
				Name:              pulumi.String(nodeName + "-nic"),
				ResourceGroupName: rg.Name,
				Location:          nodeLocation,
				IpConfigurations: paznetwork.NetworkInterfaceIpConfigurationArray{
					paznetwork.NetworkInterfaceIpConfigurationArgs{
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
			_, err = paznetwork.NewNetworkInterfaceSecurityGroupAssociation(ctx, nodeName+"-nsg-assoc", &paznetwork.NetworkInterfaceSecurityGroupAssociationArgs{
				NetworkInterfaceId:     nic.ID(),
				NetworkSecurityGroupId: nsg.ID(),
			})
			if err != nil {
				return fmt.Errorf("azure: attach NSG to NIC for node %s: %w", n.ID, err)
			}

			// Linux Virtual Machine. SSH-key auth only: password authentication is
			// disabled and the per-engagement public key is injected. There is no
			// password fallback — sshPublicKey() above fails closed if the key is
			// missing, so a VM is never provisioned with a password.
			vm, err := pazcompute.NewLinuxVirtualMachine(ctx, nodeName, &pazcompute.LinuxVirtualMachineArgs{
				Name:                          pulumi.String(nodeName),
				ResourceGroupName:             rg.Name,
				Location:                      nodeLocation,
				Size:                          pulumi.String(size),
				AdminUsername:                 pulumi.String(AdminUsername),
				DisablePasswordAuthentication: pulumi.Bool(true),
				AdminSshKeys: pazcompute.LinuxVirtualMachineAdminSshKeyArray{
					pazcompute.LinuxVirtualMachineAdminSshKeyArgs{
						Username:  pulumi.String(AdminUsername),
						PublicKey: pulumi.String(sshKey),
					},
				},
				NetworkInterfaceIds: pulumi.StringArray{nic.ID()},
				OsDisk: pazcompute.LinuxVirtualMachineOsDiskArgs{
					Caching:            pulumi.String("ReadWrite"),
					StorageAccountType: pulumi.String("Standard_LRS"),
				},
				SourceImageReference: pazcompute.LinuxVirtualMachineSourceImageReferenceArgs{
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
// Each domain.Rule becomes a SecurityRule upserted onto the node's NSG
// (rinfra-<engagementID[:8]>-<nodeID[:8]>-nsg) inside the engagement resource
// group via armnetwork.SecurityRulesClient.BeginCreateOrUpdate; each LRO is
// awaited with PollUntilDone.
func (p *provider) ConfigureIngress(ctx context.Context, creds cloud.Credentials, node domain.Node, rules []domain.Rule) error {
	if node.ProviderRef == "" {
		return fmt.Errorf("azure.ConfigureIngress: node %s has no ProviderRef (not yet provisioned)", node.ID)
	}
	cs, err := p.clients(creds)
	if err != nil {
		return err
	}
	rg := resourceGroupName(node.EngagementID)
	nsgName := nodeResourceName(node.EngagementID, node.ID) + "-nsg"

	for _, r := range buildAzureNSGRules(rules) {
		params := armnetwork.SecurityRule{
			Properties: &armnetwork.SecurityRulePropertiesFormat{
				Access:                   toSecurityRuleAccess(r.Access),
				Direction:                toSecurityRuleDirection(r.Direction),
				Protocol:                 toSecurityRuleProtocol(r.Protocol),
				Priority:                 int32Ptr(int32(r.Priority)),
				DestinationPortRange:     strPtr(r.DestinationPortRange),
				DestinationAddressPrefix: strPtr("*"),
				SourceAddressPrefix:      strPtr(r.SourceAddressPrefix),
				SourcePortRange:          strPtr("*"),
			},
		}
		poller, err := cs.securityRules.BeginCreateOrUpdate(ctx, rg, nsgName, r.Name, params, nil)
		if err != nil {
			return fmt.Errorf("azure.ConfigureIngress: create rule %s on NSG %s: %w", r.Name, nsgName, err)
		}
		if _, err := poller.PollUntilDone(ctx, nil); err != nil {
			return fmt.Errorf("azure.ConfigureIngress: await rule %s on NSG %s: %w", r.Name, nsgName, err)
		}
	}
	return nil
}

// int32Ptr returns a pointer to v (Azure SDK uses *int32 for priorities).
func int32Ptr(v int32) *int32 { return &v }

// toSecurityRuleAccess maps the internal access string to the ARM enum.
func toSecurityRuleAccess(access string) *armnetwork.SecurityRuleAccess {
	v := armnetwork.SecurityRuleAccessAllow
	if access == "Deny" {
		v = armnetwork.SecurityRuleAccessDeny
	}
	return &v
}

// toSecurityRuleDirection maps the internal direction string to the ARM enum.
func toSecurityRuleDirection(direction string) *armnetwork.SecurityRuleDirection {
	v := armnetwork.SecurityRuleDirectionInbound
	if direction == "Outbound" {
		v = armnetwork.SecurityRuleDirectionOutbound
	}
	return &v
}

// toSecurityRuleProtocol maps the Azure title-cased protocol to the ARM enum.
func toSecurityRuleProtocol(proto string) *armnetwork.SecurityRuleProtocol {
	var v armnetwork.SecurityRuleProtocol
	switch proto {
	case "Tcp":
		v = armnetwork.SecurityRuleProtocolTCP
	case "Udp":
		v = armnetwork.SecurityRuleProtocolUDP
	default:
		v = armnetwork.SecurityRuleProtocolAsterisk
	}
	return &v
}

// nsgRuleBasePriority is where ConfigureIngress-managed rules START. It is above
// the priority (100) of the allow-ssh rule BuildProgram bakes into every NSG, so
// the first managed rule does not collide with it — Azure rejects two rules with
// the same priority + direction on one NSG.
const nsgRuleBasePriority = 200

// buildAzureNSGRules converts domain.Rule to Azure NSG security rule descriptors.
// Azure-specific shape:
//   - Access is "Allow" or "Deny" (explicit, not just by omission).
//   - Priority is assigned sequentially starting at nsgRuleBasePriority (lower =
//     higher priority), leaving room below for the baked-in allow-ssh rule.
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
			Priority:             nsgRuleBasePriority + i*10, // 200, 210, 220, …
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

// AssignStaticIP allocates (or re-asserts) a Static Public IP for a node via
// armnetwork.PublicIPAddressesClient.BeginCreateOrUpdate and returns its
// address. The PIP name and resource group match BuildProgram's scheme
// (rinfra-<engagementID[:8]>-<nodeID[:8]>-pip in rinfra-<engagementID[:8]>). The
// allocation method is Static — RInfra redirectors need stable addresses.
func (p *provider) AssignStaticIP(ctx context.Context, creds cloud.Credentials, node domain.Node) (string, error) {
	if node.ProviderRef == "" {
		return "", fmt.Errorf("azure.AssignStaticIP: node %s has no ProviderRef", node.ID)
	}
	cs, err := p.clients(creds)
	if err != nil {
		return "", err
	}
	rg := resourceGroupName(node.EngagementID)
	pipName := nodeResourceName(node.EngagementID, node.ID) + "-pip"
	location := node.Spec.Region
	if location == "" {
		location = DefaultLocation
	}
	alloc := armnetwork.IPAllocationMethodStatic
	params := armnetwork.PublicIPAddress{
		Location: strPtr(location),
		Properties: &armnetwork.PublicIPAddressPropertiesFormat{
			PublicIPAllocationMethod: &alloc,
		},
		Tags: map[string]*string{
			"rinfra":      strPtr(node.EngagementID),
			"rinfra-node": strPtr(node.ID),
		},
	}
	poller, err := cs.publicIPs.BeginCreateOrUpdate(ctx, rg, pipName, params, nil)
	if err != nil {
		return "", fmt.Errorf("azure.AssignStaticIP: create public IP %s: %w", pipName, err)
	}
	resp, err := poller.PollUntilDone(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("azure.AssignStaticIP: await public IP %s: %w", pipName, err)
	}
	if resp.Properties == nil || resp.Properties.IPAddress == nil {
		return "", fmt.Errorf("azure.AssignStaticIP: public IP %s returned no address", pipName)
	}
	return *resp.Properties.IPAddress, nil
}

// CredKeyDNSResourceGroup names the resource group that owns the customer's DNS
// zone. Azure DNS record-set operations require both the zone name AND the
// resource group it lives in (unlike DO/GCP/AWS). If unset, ManageDNS falls back
// to the engagement resource group.
const CredKeyDNSResourceGroup = "AZURE_DNS_RESOURCE_GROUP"

// ManageDNS upserts an Azure DNS record (A / CNAME / TXT) via
// armdns.RecordSetsClient.CreateOrUpdate (itself an upsert). Azure DNS requires
// both a zone name AND the resource group where the zone lives; the latter comes
// from creds.Raw[AZURE_DNS_RESOURCE_GROUP]. This differs from DO (zone is a
// top-level domain resource, no resource group), GCP (managed zone in a
// project), and AWS (hosted zone ID).
func (p *provider) ManageDNS(ctx context.Context, creds cloud.Credentials, rec domain.Record) error {
	if rec.Zone == "" {
		return fmt.Errorf("azure.ManageDNS: Zone must be set (Azure DNS zone name)")
	}
	if rec.Type == "" {
		return fmt.Errorf("azure.ManageDNS: Type must be set")
	}
	cs, err := p.clients(creds)
	if err != nil {
		return err
	}
	dnsRG := creds.Raw[CredKeyDNSResourceGroup]
	if dnsRG == "" {
		return fmt.Errorf("azure.ManageDNS: %s must be set (resource group owning the DNS zone)", CredKeyDNSResourceGroup)
	}

	recType, props, err := buildAzureRecordSet(rec)
	if err != nil {
		return err
	}
	if _, err := cs.recordSets.CreateOrUpdate(ctx, dnsRG, rec.Zone, rec.Name, recType, armdns.RecordSet{Properties: props}, nil); err != nil {
		return fmt.Errorf("azure.ManageDNS: upsert %s record %s in zone %s: %w", rec.Type, rec.Name, rec.Zone, err)
	}
	return nil
}

// buildAzureRecordSet translates a domain.Record to an Azure DNS RecordType and
// RecordSetProperties. Supports A, CNAME, and TXT (the redirector record kinds).
func buildAzureRecordSet(rec domain.Record) (armdns.RecordType, *armdns.RecordSetProperties, error) {
	ttl := int64(rec.TTL)
	if ttl == 0 {
		ttl = 300
	}
	props := &armdns.RecordSetProperties{TTL: &ttl}
	switch strings.ToUpper(rec.Type) {
	case "A":
		props.ARecords = []*armdns.ARecord{{IPv4Address: strPtr(rec.Value)}}
		return armdns.RecordTypeA, props, nil
	case "CNAME":
		props.CnameRecord = &armdns.CnameRecord{Cname: strPtr(rec.Value)}
		return armdns.RecordTypeCNAME, props, nil
	case "TXT":
		props.TxtRecords = []*armdns.TxtRecord{{Value: []*string{strPtr(rec.Value)}}}
		return armdns.RecordTypeTXT, props, nil
	default:
		return "", nil, fmt.Errorf("azure.ManageDNS: unsupported record type %q (want A, CNAME, or TXT)", rec.Type)
	}
}

// addAzureDNSRecord is the Pulumi inline program helper that creates an Azure
// DNS A record. Azure uses a ZoneName + ResourceGroupName combination (not a
// zone ID). This is the authoritative Azure DNS translation.
func addAzureDNSRecord(ctx *pulumi.Context, name string, rec domain.Record, dnsRG string) error {
	ttl := rec.TTL
	if ttl == 0 {
		ttl = 300
	}
	_, err := pazdns.NewARecord(ctx, name, &pazdns.ARecordArgs{
		Name:              pulumi.String(rec.Name),
		ZoneName:          pulumi.String(rec.Zone),
		ResourceGroupName: pulumi.String(dnsRG),
		Ttl:               pulumi.Int(ttl),
		Records:           pulumi.StringArray{pulumi.String(rec.Value)},
	})
	return err
}

// Destroy tears down a node by deleting its engagement resource group, which
// transitively removes the VM and all of its NIC / Public IP / NSG resources —
// matching how BuildProgram groups every engagement resource into a single RG
// (rinfra-<engagementID[:8]>). Idempotent: an empty ProviderRef is a no-op and a
// 404 (resource group already gone) is treated as success.
//
// NOTE: because all engagement nodes share one resource group, calling Destroy
// for any node deletes the whole engagement RG. For per-node teardown that
// preserves siblings, drive the IaC engine's Teardown instead; this direct path
// is the guaranteed-teardown reconciliation that must leave nothing orphaned.
func (p *provider) Destroy(ctx context.Context, creds cloud.Credentials, node domain.Node) error {
	if node.ProviderRef == "" {
		return nil
	}
	cs, err := p.clients(creds)
	if err != nil {
		return err
	}
	rg := resourceGroupName(node.EngagementID)
	poller, err := cs.resourceGroups.BeginDelete(ctx, rg, nil)
	if err != nil {
		if isAzureNotFound(err) {
			return nil // already gone — idempotent
		}
		return fmt.Errorf("azure.Destroy: delete resource group %s: %w", rg, err)
	}
	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		if isAzureNotFound(err) {
			return nil
		}
		return fmt.Errorf("azure.Destroy: await delete of resource group %s: %w", rg, err)
	}
	return nil
}

// SweepOrphans enumerates every resource group tagged rinfra=<engagementID> and
// deletes each one, transitively removing all engagement infrastructure (VMs,
// NICs, Public IPs, NSGs) — the "no orphan" guarantee after a stack destroy.
//
// BuildProgram creates one RG per engagement named rinfra-<engagementID[:8]> and
// tags it rinfra=<engagementID>; the server-side $filter selects exactly those.
// Per-group failures are collected with errors.Join; a 404 (already swept) is
// not an error.
func (p *provider) SweepOrphans(ctx context.Context, creds cloud.Credentials, engagementID string) error {
	cs, err := p.clients(creds)
	if err != nil {
		return err
	}
	filter := fmt.Sprintf("tagName eq 'rinfra' and tagValue eq '%s'", engagementID)
	pager := cs.resourceGroups.NewListPager(&armresources.ResourceGroupsClientListOptions{Filter: &filter})

	var errs []error
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("list resource groups: %w", err))
			break
		}
		for _, rg := range page.Value {
			if rg == nil || rg.Name == nil {
				continue
			}
			name := *rg.Name
			poller, err := cs.resourceGroups.BeginDelete(ctx, name, nil)
			if err != nil {
				if !isAzureNotFound(err) {
					errs = append(errs, fmt.Errorf("delete resource group %s: %w", name, err))
				}
				continue
			}
			if _, err := poller.PollUntilDone(ctx, nil); err != nil && !isAzureNotFound(err) {
				errs = append(errs, fmt.Errorf("await delete of resource group %s: %w", name, err))
			}
		}
	}
	return errors.Join(errs...)
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
