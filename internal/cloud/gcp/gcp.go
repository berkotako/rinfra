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
// # SDK split: bulk provisioning vs standalone reconciliation
//
// Bulk provisioning runs through the IaC engine (Pulumi/Terraform) via
// BuildProgram / BuildConfig. The standalone CloudProvider/Sweeper methods
// (ConfigureIngress, AssignStaticIP, ManageDNS, Destroy, SweepOrphans) drive
// the Google Cloud REST APIs directly with the google.golang.org/api client
// libraries, for out-of-band reconciliation and the guaranteed-teardown sweep.
//
// The teardown/sweep methods (Destroy, SweepOrphans) poll the returned
// long-running Operation to completion via waitOp, so "teardown succeeded" means
// the resource is actually gone rather than merely enqueued for deletion — the
// guaranteed-teardown promise. The deploy-time helpers (ConfigureIngress,
// AssignStaticIP) enqueue their operations and surface the immediate call error;
// the engine path owns full deploy-lifecycle convergence (Pulumi polls).
//
// # Verified by compile vs needs live testing
//
// The client-backed standalone methods are unit-tested against an httptest fake
// of the compute/DNS REST APIs (live_test.go) — request routing and response
// parsing are verified, but the full lifecycle against a real GCP project still
// wants the docs/RUNBOOK_DO.md checklist. The engine BuildProgram path is
// compile-verified against the Pulumi GCP SDK v8.
package gcp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	gcpcompute "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/compute"
	gcpdns "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/dns"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	compute "google.golang.org/api/compute/v1"
	dns "google.golang.org/api/dns/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

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

// provider is the RInfra GCP cloud adapter. It implements CloudProvider and
// Sweeper and acts as the ProgramBuilder/terraform.Builder for the engine.
//
// baseEndpoint overrides the compute/DNS REST API root; empty uses the live
// Google APIs. It exists so tests can point the clients at an httptest server.
// When set, clients are built with option.WithoutAuthentication() so the fake
// server need not implement OAuth.
type provider struct {
	baseEndpoint string
}

var _ cloud.CloudProvider = (*provider)(nil)
var _ cloud.Sweeper = (*provider)(nil)
var _ orchestration.ProgramBuilder = (*provider)(nil)

func (p *provider) Type() domain.CloudProviderType { return domain.CloudGCP }

// clientOptions builds the option.ClientOption slice shared by the compute and
// DNS service constructors. With a baseEndpoint set (tests) it disables auth and
// points at the fake server; otherwise it authenticates with the inline service
// account JSON from creds.
func (p *provider) clientOptions(creds cloud.Credentials) []option.ClientOption {
	if p.baseEndpoint != "" {
		return []option.ClientOption{
			option.WithEndpoint(p.baseEndpoint),
			option.WithoutAuthentication(),
		}
	}
	return []option.ClientOption{
		option.WithCredentialsJSON([]byte(creds.Raw[CredKeyCredentials])),
	}
}

// computeService builds an authenticated GCE compute client for the engagement.
func (p *provider) computeService(ctx context.Context, creds cloud.Credentials) (*compute.Service, error) {
	svc, err := compute.NewService(ctx, p.clientOptions(creds)...)
	if err != nil {
		return nil, fmt.Errorf("gcp: build compute service: %w", err)
	}
	return svc, nil
}

// dnsService builds an authenticated Cloud DNS client for the engagement.
func (p *provider) dnsService(ctx context.Context, creds cloud.Credentials) (*dns.Service, error) {
	svc, err := dns.NewService(ctx, p.clientOptions(creds)...)
	if err != nil {
		return nil, fmt.Errorf("gcp: build dns service: %w", err)
	}
	return svc, nil
}

// project returns the GCP project ID from credentials.
func project(creds cloud.Credentials) string { return creds.Raw[CredKeyProject] }

// isNotFound reports whether err is a GCP 404 (already-deleted / missing),
// which the idempotent teardown paths treat as success.
func isNotFound(err error) bool {
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		return gerr.Code == 404
	}
	return false
}

// waitOp blocks until a compute long-running Operation reaches DONE (or ctx is
// cancelled), routing to the zone/region/global Operations service by the
// operation's scope. It returns the operation's terminal error, if any. A nil op
// (or an immediate-DONE op) returns immediately. This is what makes the
// standalone teardown/sweep path actually reliable: without it, Delete returns
// while the resource is still being torn down, so the sweep can report success
// against infrastructure that still exists.
func waitOp(ctx context.Context, svc *compute.Service, proj string, op *compute.Operation) error {
	for op != nil && op.Status != "DONE" {
		var err error
		switch {
		case op.Zone != "":
			op, err = svc.ZoneOperations.Wait(proj, lastPathSegment(op.Zone), op.Name).Context(ctx).Do()
		case op.Region != "":
			op, err = svc.RegionOperations.Wait(proj, lastPathSegment(op.Region), op.Name).Context(ctx).Do()
		default:
			op, err = svc.GlobalOperations.Wait(proj, op.Name).Context(ctx).Do()
		}
		if err != nil {
			return err
		}
	}
	if op != nil && op.Error != nil && len(op.Error.Errors) > 0 {
		return fmt.Errorf("gcp operation %s failed: %s", op.Name, op.Error.Errors[0].Message)
	}
	return nil
}

// lastPathSegment returns the final "/"-delimited segment of s. GCP REST
// responses express zone/region as full resource URLs (e.g.
// ".../zones/us-central1-a"); this extracts the bare name.
func lastPathSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// shortID returns the first 8 chars of an id (or the whole id if shorter).
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// regionFromZone derives a GCP region from a zone by dropping the trailing
// "-<letter>" (e.g. "us-central1-a" -> "us-central1"). If s is not zone-shaped
// it is returned unchanged.
func regionFromZone(s string) string {
	if len(s) > 2 && s[len(s)-2] == '-' {
		return s[:len(s)-2]
	}
	return s
}

// firewallName is the deterministic firewall resource name for a node, used by
// both ConfigureIngress (insert/patch) and SweepOrphans (delete by name).
func firewallName(nodeID string) string { return "rinfra-fw-" + shortID(nodeID) }

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
// The rule targets the node via its network tag ("rinfra-<nodeID[:8]>", matching
// BuildProgram). The firewall is upserted: if a rule of the same name already
// exists it is patched, otherwise it is inserted. The returned compute Operation
// is not polled to completion (see package doc).
func (p *provider) ConfigureIngress(ctx context.Context, creds cloud.Credentials, node domain.Node, rules []domain.Rule) error {
	if node.ProviderRef == "" {
		return fmt.Errorf("gcp.ConfigureIngress: node %s has no ProviderRef (not yet provisioned)", node.ID)
	}
	if err := validateGCPCreds(creds); err != nil {
		return err
	}
	svc, err := p.computeService(ctx, creds)
	if err != nil {
		return err
	}
	proj := project(creds)

	// Collect source ranges from allow rules; default to anywhere if unset.
	var sources []string
	seen := map[string]bool{}
	for _, r := range rules {
		if !r.Allow {
			continue
		}
		src := r.SourceCIDR
		if src == "" {
			src = "0.0.0.0/0"
		}
		if !seen[src] {
			seen[src] = true
			sources = append(sources, src)
		}
	}
	if len(sources) == 0 {
		sources = []string{"0.0.0.0/0"}
	}

	var allowed []*compute.FirewallAllowed
	for _, a := range buildGCPFirewallAllows(rules) {
		allowed = append(allowed, &compute.FirewallAllowed{
			IPProtocol: a.Protocol,
			Ports:      a.Ports,
		})
	}

	fwName := firewallName(node.ID)
	fw := &compute.Firewall{
		Name:         fwName,
		Network:      "global/networks/default",
		Direction:    "INGRESS",
		TargetTags:   []string{"rinfra-" + shortID(node.ID)},
		SourceRanges: sources,
		Allowed:      allowed,
		// Carry the engagement so SweepOrphans can match by description.
		Description: TagPrefix + node.EngagementID,
	}

	// Upsert: patch if the named rule exists, else insert.
	if _, err := svc.Firewalls.Get(proj, fwName).Context(ctx).Do(); err == nil {
		if _, err := svc.Firewalls.Patch(proj, fwName, fw).Context(ctx).Do(); err != nil {
			return fmt.Errorf("gcp.ConfigureIngress: patch firewall %s: %w", fwName, err)
		}
		return nil
	} else if !isNotFound(err) {
		return fmt.Errorf("gcp.ConfigureIngress: get firewall %s: %w", fwName, err)
	}
	if _, err := svc.Firewalls.Insert(proj, fw).Context(ctx).Do(); err != nil {
		return fmt.Errorf("gcp.ConfigureIngress: insert firewall %s: %w", fwName, err)
	}
	return nil
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

// AssignStaticIP reserves a regional external static Address for the node,
// attaches it to the instance's external access config, and returns the IP.
//
// GCP reserves the address asynchronously: addresses.Insert returns an
// Operation, after which the allocated IP is read back from the Address
// resource (addresses.Get). The reserved IP is then bound to the instance's
// primary NIC by replacing its external access config (delete + add with NatIP),
// so the node actually answers on the stable address — DNS pointed at the
// returned IP reaches the node. Reserving without attaching would leave the VM
// on its old ephemeral IP.
//
// The region is derived from the node's zone (Spec.Region holds the zone, as in
// BuildProgram). The address resource is named to match the engine naming so
// reconciliation lines up.
func (p *provider) AssignStaticIP(ctx context.Context, creds cloud.Credentials, node domain.Node) (string, error) {
	if node.ProviderRef == "" {
		return "", fmt.Errorf("gcp.AssignStaticIP: node %s has no ProviderRef", node.ID)
	}
	if err := validateGCPCreds(creds); err != nil {
		return "", err
	}
	svc, err := p.computeService(ctx, creds)
	if err != nil {
		return "", err
	}
	proj := project(creds)

	zone, instName := instanceZoneName(node)
	region := regionFromZone(zone)

	addrName := fmt.Sprintf("rinfra-%s-%s-ip", shortID(node.EngagementID), shortID(node.ID))
	addr := &compute.Address{
		Name:        addrName,
		AddressType: "EXTERNAL",
		Labels: map[string]string{
			"rinfra":      node.EngagementID,
			"rinfra-node": node.ID,
		},
	}
	if _, err := svc.Addresses.Insert(proj, region, addr).Context(ctx).Do(); err != nil {
		return "", fmt.Errorf("gcp.AssignStaticIP: reserve address %s in %s: %w", addrName, region, err)
	}

	// Read back the allocated IP. The insert Operation may still be settling,
	// but the Address resource exists and carries the assigned address.
	got, err := svc.Addresses.Get(proj, region, addrName).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("gcp.AssignStaticIP: read back address %s: %w", addrName, err)
	}

	// Attach the reserved IP to the instance's primary NIC so traffic to it
	// reaches the node. Replace the existing external access config (a NIC may
	// have at most one ONE_TO_ONE_NAT config) with one pinned to the reserved IP.
	if err := attachAddress(ctx, svc, proj, zone, instName, got.Address); err != nil {
		return got.Address, fmt.Errorf("gcp.AssignStaticIP: attach %s to %s: %w", got.Address, instName, err)
	}
	return got.Address, nil
}

// attachAddress binds natIP to the instance's primary network interface by
// swapping its external access config. The existing config is removed first
// because GCP permits only one ONE_TO_ONE_NAT access config per NIC.
func attachAddress(ctx context.Context, svc *compute.Service, proj, zone, instName, natIP string) error {
	inst, err := svc.Instances.Get(proj, zone, instName).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("get instance: %w", err)
	}
	if len(inst.NetworkInterfaces) == 0 {
		return fmt.Errorf("instance %s has no network interface", instName)
	}
	nic := inst.NetworkInterfaces[0]
	for _, ac := range nic.AccessConfigs {
		if _, err := svc.Instances.DeleteAccessConfig(proj, zone, instName, ac.Name, nic.Name).Context(ctx).Do(); err != nil && !isNotFound(err) {
			return fmt.Errorf("remove existing access config %q: %w", ac.Name, err)
		}
	}
	newAC := &compute.AccessConfig{
		Name:  "External NAT",
		Type:  "ONE_TO_ONE_NAT",
		NatIP: natIP,
	}
	if _, err := svc.Instances.AddAccessConfig(proj, zone, instName, nic.Name, newAC).Context(ctx).Do(); err != nil {
		return fmt.Errorf("add access config: %w", err)
	}
	return nil
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
// Cloud DNS has no native upsert: a Change atomically applies additions and
// deletions. To UPSERT we add the new record set and, if a record of the same
// name+type already exists, include the current record in deletions so the
// Change replaces it in one transaction. rec.Zone is the managed-zone name.
func (p *provider) ManageDNS(ctx context.Context, creds cloud.Credentials, rec domain.Record) error {
	if err := validateGCPCreds(creds); err != nil {
		return err
	}
	if rec.Zone == "" {
		return fmt.Errorf("gcp.ManageDNS: Zone must be set (GCP managed zone name)")
	}
	if rec.Type == "" {
		return fmt.Errorf("gcp.ManageDNS: Type must be set")
	}
	svc, err := p.dnsService(ctx, creds)
	if err != nil {
		return err
	}
	proj := project(creds)

	// GCP record names are FQDN with a trailing dot.
	name := rec.Name
	if !strings.HasSuffix(name, ".") {
		name += "."
	}
	ttl := int64(rec.TTL)
	if ttl == 0 {
		ttl = 300
	}
	addition := &dns.ResourceRecordSet{
		Name:    name,
		Type:    rec.Type,
		Ttl:     ttl,
		Rrdatas: []string{rec.Value},
	}

	change := &dns.Change{Additions: []*dns.ResourceRecordSet{addition}}

	// If a record set of the same name+type exists, it must be deleted in the
	// same change for the addition to be accepted (UPSERT semantics).
	existing, err := svc.ResourceRecordSets.List(proj, rec.Zone).Name(name).Type(rec.Type).Context(ctx).Do()
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("gcp.ManageDNS: list record sets in %s: %w", rec.Zone, err)
	}
	if err == nil {
		for _, rr := range existing.Rrsets {
			if rr.Name == name && rr.Type == rec.Type {
				change.Deletions = append(change.Deletions, rr)
			}
		}
	}

	if _, err := svc.Changes.Create(proj, rec.Zone, change).Context(ctx).Do(); err != nil {
		return fmt.Errorf("gcp.ManageDNS: apply change in zone %s: %w", rec.Zone, err)
	}
	return nil
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

// Destroy deletes the node's GCE instance. Idempotent: an empty ProviderRef is a
// no-op, and a 404/notFound from the API (instance already gone) is treated as
// success. The delete Operation is polled to completion (waitOp).
//
// node.ProviderRef holds the instance identifier exported by BuildProgram. GCP
// instance IDs/self-links embed the zone (".../zones/<zone>/instances/<name>");
// when the ref is a bare name we fall back to the node's zone (Spec.Region) and
// the deterministic instance name.
func (p *provider) Destroy(ctx context.Context, creds cloud.Credentials, node domain.Node) error {
	if node.ProviderRef == "" {
		return nil
	}
	if err := validateGCPCreds(creds); err != nil {
		return err
	}
	svc, err := p.computeService(ctx, creds)
	if err != nil {
		return err
	}
	proj := project(creds)

	zone, name := instanceZoneName(node)
	op, err := svc.Instances.Delete(proj, zone, name).Context(ctx).Do()
	if err != nil {
		if isNotFound(err) {
			return nil // already gone — idempotent
		}
		return fmt.Errorf("gcp.Destroy: delete instance %s in %s: %w", name, zone, err)
	}
	// Poll to completion so teardown only reports success once the instance is
	// actually gone, not merely enqueued for deletion.
	if err := waitOp(ctx, svc, proj, op); err != nil {
		return fmt.Errorf("gcp.Destroy: await delete of instance %s in %s: %w", name, zone, err)
	}
	return nil
}

// instanceZoneName resolves the (zone, instance-name) pair to delete from a
// node. If ProviderRef is a self-link of the form ".../zones/<zone>/instances/
// <name>" both are taken from it; otherwise the zone comes from Spec.Region (the
// engine stores the zone there) and the name is the deterministic instance name.
func instanceZoneName(node domain.Node) (zone, name string) {
	ref := node.ProviderRef
	if i := strings.Index(ref, "/zones/"); i >= 0 {
		rest := ref[i+len("/zones/"):]
		if j := strings.Index(rest, "/"); j >= 0 {
			zone = rest[:j]
		}
		name = lastPathSegment(ref)
		if zone != "" && name != "" {
			return zone, name
		}
	}
	zone = node.Spec.Region
	if zone == "" {
		zone = "us-central1-a"
	}
	name = lastPathSegment(ref)
	return zone, name
}

// SweepOrphans deletes every resource labeled/named for the engagement,
// providing the "no orphan" guarantee after a stack destroy:
//
//  1. GCE instances labeled rinfra=<engagementID> (found via aggregated list,
//     which spans all zones) — deleted in their owning zone.
//  2. Regional external addresses labeled rinfra=<engagementID> — deleted in
//     their owning region.
//  3. Firewall rules whose description carries the engagement tag
//     (TagPrefix+engagementID) — deleted by name.
//
// Failures are collected with errors.Join and returned together; a notFound on
// any delete is treated as already-swept. Each delete is polled to completion
// (waitOp) so the sweep returns only once resources are actually gone.
func (p *provider) SweepOrphans(ctx context.Context, creds cloud.Credentials, engagementID string) error {
	if err := validateGCPCreds(creds); err != nil {
		return err
	}
	svc, err := p.computeService(ctx, creds)
	if err != nil {
		return err
	}
	proj := project(creds)
	labelFilter := fmt.Sprintf("labels.rinfra=%s", engagementID)
	engTag := TagPrefix + engagementID
	var errs []error

	// 1. Instances across all zones (aggregated list).
	instList, err := svc.Instances.AggregatedList(proj).Filter(labelFilter).Context(ctx).Do()
	if err != nil {
		errs = append(errs, fmt.Errorf("aggregated list instances: %w", err))
	} else {
		for _, scoped := range instList.Items {
			for _, inst := range scoped.Instances {
				zone := lastPathSegment(inst.Zone)
				op, err := svc.Instances.Delete(proj, zone, inst.Name).Context(ctx).Do()
				if err != nil && !isNotFound(err) {
					errs = append(errs, fmt.Errorf("delete instance %s in %s: %w", inst.Name, zone, err))
				} else if err == nil {
					if werr := waitOp(ctx, svc, proj, op); werr != nil {
						errs = append(errs, fmt.Errorf("await delete instance %s in %s: %w", inst.Name, zone, werr))
					}
				}
			}
		}
	}

	// 2. Regional addresses (aggregated list spans regions).
	addrList, err := svc.Addresses.AggregatedList(proj).Filter(labelFilter).Context(ctx).Do()
	if err != nil {
		errs = append(errs, fmt.Errorf("aggregated list addresses: %w", err))
	} else {
		for _, scoped := range addrList.Items {
			for _, addr := range scoped.Addresses {
				region := lastPathSegment(addr.Region)
				op, err := svc.Addresses.Delete(proj, region, addr.Name).Context(ctx).Do()
				if err != nil && !isNotFound(err) {
					errs = append(errs, fmt.Errorf("delete address %s in %s: %w", addr.Name, region, err))
				} else if err == nil {
					if werr := waitOp(ctx, svc, proj, op); werr != nil {
						errs = append(errs, fmt.Errorf("await delete address %s in %s: %w", addr.Name, region, werr))
					}
				}
			}
		}
	}

	// 3. Firewall rules tagged for the engagement (matched via description).
	fwList, err := svc.Firewalls.List(proj).Context(ctx).Do()
	if err != nil {
		errs = append(errs, fmt.Errorf("list firewalls: %w", err))
	} else {
		for _, fw := range fwList.Items {
			if fw.Description != engTag {
				continue
			}
			op, err := svc.Firewalls.Delete(proj, fw.Name).Context(ctx).Do()
			if err != nil && !isNotFound(err) {
				errs = append(errs, fmt.Errorf("delete firewall %s: %w", fw.Name, err))
			} else if err == nil {
				if werr := waitOp(ctx, svc, proj, op); werr != nil {
					errs = append(errs, fmt.Errorf("await delete firewall %s: %w", fw.Name, werr))
				}
			}
		}
	}

	return errors.Join(errs...)
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
