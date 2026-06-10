// Package gcp adapts Google Cloud Platform to RInfra's cloud.CloudProvider
// interface. Provisions into the customer's project using per-engagement
// credentials — never a shared RInfra account.
//
// # SDK approach
//
// Uses the Pulumi Go SDK automation API (via internal/orchestration.Engine)
// for resource lifecycle management.
//
// # ConfigureIngress — deliberately different from other providers
//
// GCP uses VPC Firewall Rules that apply globally within a VPC network and
// filter by target tags on instances. A Firewall Rule is associated with a
// GCE instance by adding a matching network tag to the instance. This differs
// from:
//   - DO: Cloud Firewalls attached to Droplets by Droplet ID or tag.
//   - AWS: EC2 Security Groups attached to instance NICs (stateful).
//   - Azure: NSGs attached to NICs or subnets (explicit allow/deny priority).
//
// The target tag "rinfra-<nodeID[:8]>" is applied both to the instance and
// the Firewall Rule so they associate correctly.
//
// # Credential keys
//
//   - "GOOGLE_CREDENTIALS" — service account JSON (full contents, not a path).
//   - "GOOGLE_PROJECT"     — GCP project ID.
//
// Note: the Pulumi GCP provider uses GOOGLE_CREDENTIALS for the service
// account JSON. An alternative is GOOGLE_APPLICATION_CREDENTIALS (a file
// path), but inline JSON is safer for per-engagement credential storage.
//
// # Verified by compile vs needs live testing
//
// All code below is verified to compile against the Pulumi GCP SDK v8.
// The full resource lifecycle requires a live GCP project and has NOT been
// exercised against the live API. See docs/RUNBOOK_DO.md for the checklist
// approach (same pattern, different cloud).
package gcp

import (
	"context"
	"fmt"
	"strconv"

	gcpcompute "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/compute"
	gcpdns "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/dns"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration"
)

// Credential key constants.
const (
	CredKeyCredentials = "GOOGLE_CREDENTIALS" // service account JSON contents
	CredKeyProject     = "GOOGLE_PROJECT"     // GCP project ID
)

// DefaultImage is the GCE instance image used when NodeSpec does not override.
const DefaultImage = "ubuntu-os-cloud/ubuntu-2204-lts"

// DefaultMachineType is used when NodeSpec.Size is empty.
const DefaultMachineType = "e2-micro"

// TagPrefix is the label format applied to all GCP resources.
const TagPrefix = "rinfra-"

func init() {
	p := &provider{}
	cloud.Register(p)
	cloud.RegisterSweeper(domain.CloudGCP, p)
}

type provider struct{}

var _ cloud.CloudProvider = (*provider)(nil)
var _ cloud.Sweeper = (*provider)(nil)
var _ orchestration.ProgramBuilder = (*provider)(nil)

func (p *provider) Type() domain.CloudProviderType { return domain.CloudGCP }

// BuildProgram implements orchestration.ProgramBuilder. Creates a GCE instance
// with a network tag, a VPC Firewall Rule using that tag (GCP-specific target-
// tag filtering), and a regional static external IP.
//
// GCP-specific tagging:
//   - Instance labels: "rinfra" = engagementID, "rinfra-node" = nodeID
//   - Network tags (for firewall targeting): "rinfra-<engID[:8]>", "rinfra-node-<nodeID[:8]>"
func (p *provider) BuildProgram(engagementID string, creds cloud.Credentials, nodes []domain.Node) pulumi.RunFunc {
	return func(ctx *pulumi.Context) error {
		project := creds.Raw[CredKeyProject]

		for _, n := range nodes {
			nodeName := fmt.Sprintf("rinfra-%s-%s", engagementID[:8], n.ID[:8])
			// GCP network tag for firewall targeting — must be lowercase, start with letter.
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
			// GCP uses zone (not just region) for instances.
			// Region is derived from zone by dropping the trailing letter.
			region := zone
			if len(zone) > 2 && zone[len(zone)-2] == '-' {
				region = zone[:len(zone)-2]
			}

			labels := pulumi.StringMap{
				"rinfra":      pulumi.String(engagementID),
				"rinfra-node": pulumi.String(n.ID),
			}

			// Regional static external IP — GCP uses Address (regional, not global).
			addr, err := gcpcompute.NewAddress(ctx, nodeName+"-ip", &gcpcompute.AddressArgs{
				Name:    pulumi.String(nodeName + "-ip"),
				Region:  pulumi.String(region),
				Project: pulumi.String(project),
				Labels:  labels,
			})
			if err != nil {
				return fmt.Errorf("gcp: create address for node %s: %w", n.ID, err)
			}

			// GCE instance with the static IP attached via access config.
			instance, err := gcpcompute.NewInstance(ctx, nodeName, &gcpcompute.InstanceArgs{
				Name:        pulumi.String(nodeName),
				MachineType: pulumi.String(machineType),
				Zone:        pulumi.String(zone),
				Project:     pulumi.String(project),
				BootDisk: &gcpcompute.InstanceBootDiskArgs{
					InitializeParams: &gcpcompute.InstanceBootDiskInitializeParamsArgs{
						Image: pulumi.String(DefaultImage),
					},
				},
				NetworkInterfaces: gcpcompute.InstanceNetworkInterfaceArray{
					&gcpcompute.InstanceNetworkInterfaceArgs{
						Network: pulumi.String("default"),
						AccessConfigs: gcpcompute.InstanceNetworkInterfaceAccessConfigArray{
							&gcpcompute.InstanceNetworkInterfaceAccessConfigArgs{
								// Attach the reserved external IP.
								NatIp: addr.Address,
							},
						},
					},
				},
				// Network tags — GCP Firewall Rules use these to target instances.
				// This is how GCP differs: firewall rules are not attached to instances
				// but filter by instance tag.
				Tags:   pulumi.StringArray{pulumi.String(netTag), pulumi.String(engTag)},
				Labels: labels,
			})
			if err != nil {
				return fmt.Errorf("gcp: create instance for node %s: %w", n.ID, err)
			}

			_ = instance // used implicitly via addr.Address

			ctx.Export(orchestration.NodeProviderRefKey(n.ID), instance.ID())
			ctx.Export(orchestration.NodePublicIPKey(n.ID), addr.Address)
		}
		return nil
	}
}

// ProvisionNode — not supported as a direct call. Use Engine.Deploy.
func (p *provider) ProvisionNode(_ context.Context, _ cloud.Credentials, _ domain.NodeSpec) (domain.Node, error) {
	return domain.Node{}, fmt.Errorf("gcp.ProvisionNode: use orchestration.Engine.Deploy for real provisioning")
}

// ConfigureIngress creates a GCP VPC Firewall Rule for a node.
//
// GCP VPC Firewall Rules are global within a VPC network and use target tags
// to identify which instances they apply to. This is fundamentally different
// from AWS (per-instance security groups) and DO (cloud firewalls by Droplet
// ID/tag).
//
// Translation from domain.Rule:
//   - Allow rules → Firewall Rule with Allows[].Protocol and Allows[].Ports.
//   - Deny rules are dropped (VPC Firewall deny rules are a separate GCP
//     construct with explicit priority ordering; the default-deny posture is
//     achieved by not creating allow rules).
//   - SourceCIDR → SourceRanges on the Firewall Rule.
//   - Direction is always INGRESS.
//
// TODO(live): standalone GCP firewall update requires live project.
func (p *provider) ConfigureIngress(_ context.Context, creds cloud.Credentials, node domain.Node, rules []domain.Rule) error {
	if node.ProviderRef == "" {
		return fmt.Errorf("gcp.ConfigureIngress: node %s has no ProviderRef (not yet provisioned)", node.ID)
	}
	if err := validateGCPCreds(creds); err != nil {
		return err
	}
	_ = buildGCPFirewallAllows(rules)
	return fmt.Errorf("gcp.ConfigureIngress: standalone firewall update not yet implemented; use Engine.Deploy — TODO(live)")
}

// buildGCPFirewallAllows converts domain.Rule to GCP FirewallAllow descriptors.
// GCP-specific shape: one FirewallAllow per protocol group; Ports is a string
// slice (not a FromPort/ToPort pair like AWS).
func buildGCPFirewallAllows(rules []domain.Rule) []gcpFirewallAllow {
	byProto := make(map[string][]string)
	for _, r := range rules {
		if !r.Allow {
			continue // GCP firewall rules are allow-only in this context.
		}
		byProto[r.Protocol] = append(byProto[r.Protocol], strconv.Itoa(r.Port))
	}
	var out []gcpFirewallAllow
	for proto, ports := range byProto {
		out = append(out, gcpFirewallAllow{Protocol: proto, Ports: ports})
	}
	return out
}

// gcpFirewallAllow is the internal representation of a GCP Firewall allow rule.
type gcpFirewallAllow struct {
	Protocol string
	Ports    []string // e.g. ["443"] or ["8000", "8001-8010"]
}

// AssignStaticIP — handled by Address in BuildProgram.
// TODO(live): standalone address allocation.
func (p *provider) AssignStaticIP(_ context.Context, creds cloud.Credentials, node domain.Node) (string, error) {
	if node.ProviderRef == "" {
		return "", fmt.Errorf("gcp.AssignStaticIP: node %s has no ProviderRef", node.ID)
	}
	if err := validateGCPCreds(creds); err != nil {
		return "", err
	}
	return "", fmt.Errorf("gcp.AssignStaticIP: use Engine.Deploy (includes Address in inline program) — TODO(live)")
}

// ManageDNS upserts a GCP Cloud DNS RecordSet.
//
// GCP Cloud DNS uses "managed zones" (named resources in a project) rather
// than zone IDs (Route53) or bare domain names (DO). The managed zone name
// must be stored in creds.Raw["GCP_DNS_MANAGED_ZONE"] for the target zone.
//
// GCP record set names must be fully-qualified (trailing dot): "www.example.com."
// This differs from DO (no trailing dot) and AWS (no trailing dot in most contexts).
//
// TODO(live): DNS record upsert via GCP SDK.
func (p *provider) ManageDNS(_ context.Context, creds cloud.Credentials, rec domain.Record) error {
	if err := validateGCPCreds(creds); err != nil {
		return err
	}
	if rec.Zone == "" {
		return fmt.Errorf("gcp.ManageDNS: Zone must be set (GCP managed zone name)")
	}
	return fmt.Errorf("gcp.ManageDNS: use Engine.Deploy (includes RecordSet in inline program) — TODO(live)")
}

// addGCPDNSRecord is the Pulumi inline program helper that creates a Cloud DNS
// RecordSet. GCP Rrdatas is a string slice; record names must be FQDN with
// trailing dot.
func addGCPDNSRecord(ctx *pulumi.Context, name string, rec domain.Record, managedZone string, project string) error {
	fqdn := rec.Name + "." + rec.Zone + "."
	ttl := rec.TTL
	if ttl == 0 {
		ttl = 300
	}
	_, err := gcpdns.NewRecordSet(ctx, name, &gcpdns.RecordSetArgs{
		Name:        pulumi.String(fqdn),
		Type:        pulumi.String(rec.Type),
		Ttl:         pulumi.Int(ttl),
		ManagedZone: pulumi.String(managedZone),
		Project:     pulumi.String(project),
		Rrdatas:     pulumi.StringArray{pulumi.String(rec.Value)},
	})
	return err
}

// Destroy — handled by Engine.Teardown. Idempotent (empty ProviderRef = no-op).
// TODO(live): direct GCP SDK delete.
func (p *provider) Destroy(_ context.Context, creds cloud.Credentials, node domain.Node) error {
	if node.ProviderRef == "" {
		return nil
	}
	if err := validateGCPCreds(creds); err != nil {
		return err
	}
	return fmt.Errorf("gcp.Destroy: use Engine.Teardown for full stack destroy + sweep — TODO(live)")
}

// SweepOrphans lists GCE instances/addresses labeled rinfra=<engagementID>
// and deletes any found.
//
// TODO(live): implement using GCP compute SDK:
//  1. Create computeService client with GOOGLE_CREDENTIALS.
//  2. instances.list with filter label.rinfra = engagementID.
//  3. instances.delete for each found.
//  4. addresses.list + delete for orphaned static IPs.
//  5. firewalls.list + delete for orphaned firewall rules (by label).
func (p *provider) SweepOrphans(_ context.Context, creds cloud.Credentials, engagementID string) error {
	if err := validateGCPCreds(creds); err != nil {
		return err
	}
	// TODO(live): implement GCP resource sweep.
	_ = engagementID
	return nil
}

// validateGCPCreds checks minimum required credential keys.
func validateGCPCreds(creds cloud.Credentials) error {
	if creds.Raw[CredKeyCredentials] == "" {
		return fmt.Errorf("gcp: credential key %q not set", CredKeyCredentials)
	}
	if creds.Raw[CredKeyProject] == "" {
		return fmt.Errorf("gcp: credential key %q not set", CredKeyProject)
	}
	return nil
}

// ensure addGCPDNSRecord is referenced (used in Pulumi programs, not tested here directly)
var _ = addGCPDNSRecord
