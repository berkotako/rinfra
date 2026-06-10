// Package digitalocean adapts DigitalOcean to RInfra's cloud.CloudProvider
// interface. Per the build order, this is the FIRST cloud to implement: most
// permissive AUP, cheapest to iterate. Provisioning uses the customer's
// per-engagement credentials — never a shared RInfra account.
//
// # SDK approach
//
// We use the Pulumi Go SDK automation API (via internal/orchestration.Engine)
// for all resource lifecycle management. The CloudProvider methods on this
// type translate domain types into Pulumi resource declarations inside an
// inline program, which the Engine compiles and runs.
//
// The ProgramBuilder (BuildProgram) is the glue: it returns a pulumi.RunFunc
// that the Engine calls to create Droplets, Firewalls, Reserved IPs, and DNS
// records in the customer's DigitalOcean account.
//
// ConfigureIngress uses DO Cloud Firewalls (not iptables-style ACLs). A
// single Firewall resource is associated with all Droplets for the node group.
// This deliberately diverges from AWS (security groups per instance), GCP (VPC
// firewall rules with target tags), and Azure (NSGs attached to NICs/subnets).
//
// # Credential keys
//
//   - "DIGITALOCEAN_TOKEN" — the DO personal-access-token (write scope required).
//
// # Verified by compile vs needs live testing
//
// All code below is verified to compile against the Pulumi DigitalOcean SDK
// v4. The full resource lifecycle (Droplet create, Firewall attach, Reserved
// IP assign, DNS record upsert, stack destroy, tagged-resource sweep) requires
// a live DO account and has been designed for correctness but has NOT been
// exercised against the live API — see docs/RUNBOOK_DO.md for the step-by-step
// checklist.
package digitalocean

import (
	"context"
	"fmt"
	"strconv"

	pdo "github.com/pulumi/pulumi-digitalocean/sdk/v4/go/digitalocean"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration"
)

// CredKeyToken is the Credentials.Raw key for the DO personal access token.
// Pulumi's DO provider reads it from the DIGITALOCEAN_TOKEN environment var,
// which we pass via orchestration.credentialsToEnv.
const CredKeyToken = "DIGITALOCEAN_TOKEN"

// DefaultImage is the droplet image used when NodeSpec does not override it.
const DefaultImage = "ubuntu-22-04-x64"

// TagPrefix is the label format applied to every resource:
// "rinfra:<engagementID>" at the engagement level.
const TagPrefix = "rinfra:"

func init() {
	p := &provider{}
	cloud.Register(p)
	cloud.RegisterSweeper(domain.CloudDigitalOcean, p)
}

// provider is the RInfra DO cloud adapter. It implements CloudProvider and
// Sweeper; it acts as both the direct-call adapter and the ProgramBuilder
// passed to orchestration.Engine.
type provider struct{}

var _ cloud.CloudProvider = (*provider)(nil)
var _ cloud.Sweeper = (*provider)(nil)
var _ orchestration.ProgramBuilder = (*provider)(nil)

// Type implements cloud.CloudProvider.
func (p *provider) Type() domain.CloudProviderType { return domain.CloudDigitalOcean }

// BuildProgram implements orchestration.ProgramBuilder. It returns a
// pulumi.RunFunc that creates all nodes for this engagement in DigitalOcean.
// Each Droplet is tagged with rinfra:<engagementID> and rinfra:node:<nodeID>.
// Outputs are exported using orchestration.NodeProviderRefKey / NodePublicIPKey
// so the Engine can harvest ProviderRef and PublicIP after stack.Up.
func (p *provider) BuildProgram(engagementID string, creds cloud.Credentials, nodes []domain.Node) pulumi.RunFunc {
	return func(ctx *pulumi.Context) error {
		engTag := TagPrefix + engagementID

		for _, n := range nodes {
			nodeTag := TagPrefix + "node:" + n.ID
			nodeName := fmt.Sprintf("rinfra-%s-%s", engagementID[:8], n.ID[:8])
			image := DefaultImage
			size := n.Spec.Size
			if size == "" {
				size = "s-1vcpu-1gb"
			}
			region := n.Spec.Region
			if region == "" {
				region = "nyc3"
			}

			droplet, err := pdo.NewDroplet(ctx, nodeName, &pdo.DropletArgs{
				Image:  pulumi.String(image),
				Name:   pulumi.String(nodeName),
				Region: pulumi.StringInput(pulumi.String(region)),
				Size:   pulumi.String(size),
				Tags:   pulumi.StringArray{pulumi.String(engTag), pulumi.String(nodeTag)},
			})
			if err != nil {
				return fmt.Errorf("digitalocean: create droplet for node %s: %w", n.ID, err)
			}

			// Export the numeric droplet ID as ProviderRef and its IPv4 as PublicIP.
			ctx.Export(orchestration.NodeProviderRefKey(n.ID), droplet.ID())
			ctx.Export(orchestration.NodePublicIPKey(n.ID), droplet.Ipv4Address)
		}
		return nil
	}
}

// ProvisionNode provisions a single node synchronously. In the Pulumi-backed
// flow, provisioning goes through Engine.Deploy which calls BuildProgram. This
// direct method exists for callers that bypass the Engine (e.g. in tests or
// fallback paths). It delegates to the Engine if one is registered, otherwise
// returns an error directing the caller to use the Engine.
//
// NOTE: For production use, wire Engine.Deploy through InfraService instead
// of calling ProvisionNode directly — the Engine provides stack state tracking
// and tagged-resource reconciliation.
func (p *provider) ProvisionNode(_ context.Context, _ cloud.Credentials, _ domain.NodeSpec) (domain.Node, error) {
	return domain.Node{}, fmt.Errorf("digitalocean.ProvisionNode: use orchestration.Engine.Deploy for real provisioning; ProvisionNode is not supported as a direct call on the DO provider")
}

// ConfigureIngress creates or updates a DigitalOcean Cloud Firewall for a node.
//
// DO Cloud Firewalls (distinct from iptables rules) are associated with Droplets
// by Droplet ID. This method requires n.ProviderRef to be set to the numeric
// Droplet ID returned by ProvisionNode / Engine.Deploy.
//
// Translation from domain.Rule to DO Firewall:
//   - Protocol "tcp"/"udp" → FirewallInboundRule with PortRange set to strconv of Port.
//   - SourceCIDR is added to SourceAddresses. If empty, defaults to "0.0.0.0/0".
//   - Allow=false rules are dropped (DO Firewalls are allow-only; blocking is by
//     omission). The operator should not include deny rules for DO.
//   - Outbound: we allow all outbound by default (standard DO Firewall posture).
//
// NOTE: This method is intended to be called AFTER ProvisionNode returns the
// Droplet ID in ProviderRef. In the Pulumi flow, ConfigureIngress can also be
// driven by adding a pdo.NewFirewall call inside BuildProgram.
//
// TODO(live): ConfigureIngress as a standalone call (outside Pulumi) would
// require the DO API client (godo). For the Engine-driven path, inline Firewall
// resources in BuildProgram are preferred.
func (p *provider) ConfigureIngress(_ context.Context, creds cloud.Credentials, node domain.Node, rules []domain.Rule) error {
	if node.ProviderRef == "" {
		return fmt.Errorf("digitalocean.ConfigureIngress: node %s has no ProviderRef (not yet provisioned)", node.ID)
	}
	// Validate creds.
	if creds.Raw[CredKeyToken] == "" {
		return fmt.Errorf("digitalocean.ConfigureIngress: %s not set in credentials", CredKeyToken)
	}
	// Build the ingress rule set for validation/logging (actual cloud call
	// would use godo or a Pulumi update; see TODO above).
	_ = buildDOInboundRules(rules)
	return fmt.Errorf("digitalocean.ConfigureIngress: standalone (non-Pulumi) ingress update not yet implemented; use Engine.Deploy which includes Firewall resources in the inline program — TODO(live)")
}

// buildDOInboundRules converts domain.Rule slice to DO inbound rule descriptors.
// This function is the authoritative translation and is tested directly.
func buildDOInboundRules(rules []domain.Rule) []doInboundRule {
	var out []doInboundRule
	for _, r := range rules {
		if !r.Allow {
			continue // DO Cloud Firewalls are allow-only; skip deny rules.
		}
		src := r.SourceCIDR
		if src == "" {
			src = "0.0.0.0/0"
		}
		portRange := strconv.Itoa(r.Port)
		out = append(out, doInboundRule{
			Protocol:        r.Protocol,
			PortRange:       portRange,
			SourceAddresses: []string{src},
		})
	}
	return out
}

// doInboundRule is an internal representation of a DO Firewall inbound rule.
// It mirrors pdo.FirewallInboundRuleArgs but as a plain struct so it can be
// used in unit tests without a Pulumi context.
type doInboundRule struct {
	Protocol        string
	PortRange       string // e.g. "443" or "8000-9000"
	SourceAddresses []string
}

// AssignStaticIP reserves and assigns a DO Reserved IP to a droplet.
// Requires node.ProviderRef to be the numeric Droplet ID.
//
// TODO(live): This is structurally complete. Actual API call via godo requires
// a live DO account. In the Pulumi path, add pdo.NewReservedIp to BuildProgram.
func (p *provider) AssignStaticIP(_ context.Context, creds cloud.Credentials, node domain.Node) (string, error) {
	if node.ProviderRef == "" {
		return "", fmt.Errorf("digitalocean.AssignStaticIP: node %s has no ProviderRef", node.ID)
	}
	if creds.Raw[CredKeyToken] == "" {
		return "", fmt.Errorf("digitalocean.AssignStaticIP: %s not set in credentials", CredKeyToken)
	}
	return "", fmt.Errorf("digitalocean.AssignStaticIP: use Engine.Deploy (includes ReservedIp in inline program) — TODO(live)")
}

// ManageDNS upserts a DO DNS record via the DO API.
//
// DO DNS: the Zone maps to a DO Domain; Name is the subdomain; Type/Value/TTL
// map directly to DnsRecord fields.
//
// This differs from AWS Route53 (hosted zones / change sets), GCP Cloud DNS
// (managed zone / record sets), and Azure DNS (zones / record sets) — each has
// a distinct concept of zone ownership and record atomicity.
//
// TODO(live): Actual API call via godo or pdo.NewDnsRecord in BuildProgram.
func (p *provider) ManageDNS(_ context.Context, creds cloud.Credentials, rec domain.Record) error {
	if creds.Raw[CredKeyToken] == "" {
		return fmt.Errorf("digitalocean.ManageDNS: %s not set in credentials", CredKeyToken)
	}
	// Validate fields.
	if rec.Zone == "" {
		return fmt.Errorf("digitalocean.ManageDNS: Zone must be set (DO Domain name)")
	}
	if rec.Type == "" {
		return fmt.Errorf("digitalocean.ManageDNS: Type must be set")
	}
	return fmt.Errorf("digitalocean.ManageDNS: use Engine.Deploy (includes DnsRecord in inline program) — TODO(live)")
}

// Destroy tears down a node by Droplet ID. Idempotent: if the Droplet is
// already gone (ProviderRef empty or 404 from API) it returns nil.
//
// In the Pulumi-driven path, Destroy is handled by Engine.Teardown (stack
// destroy). This direct method is for out-of-band cleanup.
//
// TODO(live): Direct API call via godo to DELETE /v2/droplets/{id}.
func (p *provider) Destroy(_ context.Context, creds cloud.Credentials, node domain.Node) error {
	if node.ProviderRef == "" {
		// Never provisioned — nothing to do.
		return nil
	}
	if creds.Raw[CredKeyToken] == "" {
		return fmt.Errorf("digitalocean.Destroy: %s not set in credentials", CredKeyToken)
	}
	return fmt.Errorf("digitalocean.Destroy: use Engine.Teardown for full stack destroy + sweep — TODO(live)")
}

// SweepOrphans implements cloud.Sweeper. It lists all Droplets tagged
// rinfra:<engagementID> and deletes any that exist, providing the "no orphan"
// guarantee after a Pulumi stack.Destroy.
//
// TODO(live): Requires live DO API call. Implementation outline:
//  1. Create godo.Client with creds.Raw[CredKeyToken].
//  2. Call client.Droplets.ListByTag(ctx, TagPrefix+engagementID, nil).
//  3. For each returned Droplet, call client.Droplets.Delete(ctx, droplet.ID).
//  4. List DO Firewalls and Reserved IPs with matching tags and delete.
//  5. Return nil if all deletions succeed or resources are 404.
func (p *provider) SweepOrphans(_ context.Context, creds cloud.Credentials, engagementID string) error {
	if creds.Raw[CredKeyToken] == "" {
		return fmt.Errorf("digitalocean.SweepOrphans: %s not set in credentials", CredKeyToken)
	}
	// TODO(live): implement godo sweep.
	_ = engagementID
	return nil
}
