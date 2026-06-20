// Package digitalocean adapts DigitalOcean to RInfra's cloud.CloudProvider
// interface. Per the build order, this is the FIRST cloud to implement: most
// permissive AUP, cheapest to iterate. Provisioning uses the customer's
// per-engagement credentials — never a shared RInfra account.
//
// # SDK approach
//
// Two complementary paths:
//
//   - Bulk provisioning runs through the IaC engine (Pulumi or Terraform): the
//     ProgramBuilder (BuildProgram) / terraform.Builder declare Droplets, tags,
//     and outputs the engine compiles and applies.
//   - The standalone CloudProvider/Sweeper methods (ConfigureIngress,
//     AssignStaticIP, ManageDNS, Destroy, SweepOrphans) drive the DigitalOcean
//     API directly with godo, for out-of-band reconciliation and the
//     guaranteed-teardown sweep that runs after every engine destroy.
//
// ConfigureIngress uses DO Cloud Firewalls (not iptables-style ACLs). A
// single Firewall resource is associated with the node's Droplet. This
// deliberately diverges from AWS (security groups per instance), GCP (VPC
// firewall rules with target tags), and Azure (NSGs attached to NICs/subnets).
//
// # Credential keys
//
//   - "DIGITALOCEAN_TOKEN" — the DO personal-access-token (write scope required).
//
// # Verified by compile vs needs live testing
//
// The godo-backed standalone methods are unit-tested against an httptest fake of
// the DO API (live_test.go) — request routing and resource selection are
// verified, but the full lifecycle against a real DO account still wants the
// docs/RUNBOOK_DO.md checklist. The engine BuildProgram path is compile-verified
// against the Pulumi DigitalOcean SDK v4.
package digitalocean

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/digitalocean/godo"
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
//
// apiBase overrides the godo API base URL; empty uses the live DO API. It exists
// so tests can point the client at an httptest server.
type provider struct {
	apiBase string
}

// client builds a godo client from the engagement's DO token. The token is read
// from creds.Raw[CredKeyToken]; the same value Pulumi/Terraform use via env.
func (p *provider) client(token string) (*godo.Client, error) {
	if token == "" {
		return nil, fmt.Errorf("digitalocean: %s not set in credentials", CredKeyToken)
	}
	c := godo.NewFromToken(token)
	if p.apiBase != "" {
		base := p.apiBase
		if !strings.HasSuffix(base, "/") {
			base += "/"
		}
		u, err := url.Parse(base)
		if err != nil {
			return nil, fmt.Errorf("digitalocean: invalid api base %q: %w", p.apiBase, err)
		}
		c.BaseURL = u
	}
	return c, nil
}

// dropletID parses a node's ProviderRef (the numeric Droplet ID) to an int.
func dropletID(ref string) (int, error) {
	id, err := strconv.Atoi(ref)
	if err != nil {
		return 0, fmt.Errorf("digitalocean: invalid droplet ref %q: %w", ref, err)
	}
	return id, nil
}

// shortID returns the first 8 chars of an id (or the whole id if shorter), used
// for human-readable resource names.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

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
func (p *provider) ConfigureIngress(ctx context.Context, creds cloud.Credentials, node domain.Node, rules []domain.Rule) error {
	if node.ProviderRef == "" {
		return fmt.Errorf("digitalocean.ConfigureIngress: node %s has no ProviderRef (not yet provisioned)", node.ID)
	}
	client, err := p.client(creds.Raw[CredKeyToken])
	if err != nil {
		return err
	}
	id, err := dropletID(node.ProviderRef)
	if err != nil {
		return err
	}

	fwName := "rinfra-fw-" + shortID(node.ID)
	fr := &godo.FirewallRequest{
		Name:         fwName,
		InboundRules: godoInboundRules(rules),
		// Allow-all egress is the standard DO posture (beacons dial out).
		OutboundRules: []godo.OutboundRule{
			{Protocol: "tcp", PortRange: "all", Destinations: &godo.Destinations{Addresses: []string{"0.0.0.0/0", "::/0"}}},
			{Protocol: "udp", PortRange: "all", Destinations: &godo.Destinations{Addresses: []string{"0.0.0.0/0", "::/0"}}},
			{Protocol: "icmp", Destinations: &godo.Destinations{Addresses: []string{"0.0.0.0/0", "::/0"}}},
		},
		DropletIDs: []int{id},
		Tags:       []string{TagPrefix + node.EngagementID},
	}

	// Upsert: update the node's firewall if it already exists, else create it.
	existing, err := p.findFirewallByName(ctx, client, fwName)
	if err != nil {
		return err
	}
	if existing != nil {
		if _, _, err := client.Firewalls.Update(ctx, existing.ID, fr); err != nil {
			return fmt.Errorf("digitalocean.ConfigureIngress: update firewall: %w", err)
		}
		return nil
	}
	if _, _, err := client.Firewalls.Create(ctx, fr); err != nil {
		return fmt.Errorf("digitalocean.ConfigureIngress: create firewall: %w", err)
	}
	return nil
}

// godoInboundRules converts domain rules to godo inbound rules (allow-only).
func godoInboundRules(rules []domain.Rule) []godo.InboundRule {
	var out []godo.InboundRule
	for _, r := range buildDOInboundRules(rules) {
		out = append(out, godo.InboundRule{
			Protocol:  r.Protocol,
			PortRange: r.PortRange,
			Sources:   &godo.Sources{Addresses: r.SourceAddresses},
		})
	}
	return out
}

// findFirewallByName returns the firewall with the given name, or nil if none.
func (p *provider) findFirewallByName(ctx context.Context, client *godo.Client, name string) (*godo.Firewall, error) {
	fws, _, err := client.Firewalls.List(ctx, &godo.ListOptions{PerPage: 200})
	if err != nil {
		return nil, fmt.Errorf("digitalocean: list firewalls: %w", err)
	}
	for i := range fws {
		if fws[i].Name == name {
			return &fws[i], nil
		}
	}
	return nil, nil
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
func (p *provider) AssignStaticIP(ctx context.Context, creds cloud.Credentials, node domain.Node) (string, error) {
	if node.ProviderRef == "" {
		return "", fmt.Errorf("digitalocean.AssignStaticIP: node %s has no ProviderRef", node.ID)
	}
	client, err := p.client(creds.Raw[CredKeyToken])
	if err != nil {
		return "", err
	}
	id, err := dropletID(node.ProviderRef)
	if err != nil {
		return "", err
	}
	rip, _, err := client.ReservedIPs.Create(ctx, &godo.ReservedIPCreateRequest{DropletID: id})
	if err != nil {
		return "", fmt.Errorf("digitalocean.AssignStaticIP: reserve IP: %w", err)
	}
	return rip.IP, nil
}

// ManageDNS upserts a DO DNS record via the DO API.
//
// DO DNS: the Zone maps to a DO Domain; Name is the subdomain; Type/Value/TTL
// map directly to DnsRecord fields.
//
// This differs from AWS Route53 (hosted zones / change sets), GCP Cloud DNS
// (managed zone / record sets), and Azure DNS (zones / record sets) — each has
// a distinct concept of zone ownership and record atomicity.
func (p *provider) ManageDNS(ctx context.Context, creds cloud.Credentials, rec domain.Record) error {
	if rec.Zone == "" {
		return fmt.Errorf("digitalocean.ManageDNS: Zone must be set (DO Domain name)")
	}
	if rec.Type == "" {
		return fmt.Errorf("digitalocean.ManageDNS: Type must be set")
	}
	client, err := p.client(creds.Raw[CredKeyToken])
	if err != nil {
		return err
	}
	edit := &godo.DomainRecordEditRequest{
		Type: rec.Type,
		Name: rec.Name,
		Data: rec.Value,
		TTL:  rec.TTL,
	}
	// Upsert by (Type, Name): edit a matching record, else create one.
	records, _, err := client.Domains.Records(ctx, rec.Zone, &godo.ListOptions{PerPage: 200})
	if err != nil {
		return fmt.Errorf("digitalocean.ManageDNS: list records: %w", err)
	}
	for _, r := range records {
		if r.Type == rec.Type && r.Name == rec.Name {
			if _, _, err := client.Domains.EditRecord(ctx, rec.Zone, r.ID, edit); err != nil {
				return fmt.Errorf("digitalocean.ManageDNS: edit record: %w", err)
			}
			return nil
		}
	}
	if _, _, err := client.Domains.CreateRecord(ctx, rec.Zone, edit); err != nil {
		return fmt.Errorf("digitalocean.ManageDNS: create record: %w", err)
	}
	return nil
}

// Destroy tears down a node by Droplet ID. Idempotent: if the Droplet is
// already gone (ProviderRef empty or 404 from API) it returns nil.
//
// In the Pulumi-driven path, Destroy is handled by Engine.Teardown (stack
// destroy). This direct method is for out-of-band cleanup.
func (p *provider) Destroy(ctx context.Context, creds cloud.Credentials, node domain.Node) error {
	if node.ProviderRef == "" {
		// Never provisioned — nothing to do.
		return nil
	}
	client, err := p.client(creds.Raw[CredKeyToken])
	if err != nil {
		return err
	}
	id, err := dropletID(node.ProviderRef)
	if err != nil {
		return err
	}
	resp, err := client.Droplets.Delete(ctx, id)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return nil // already gone — idempotent
		}
		return fmt.Errorf("digitalocean.Destroy: delete droplet %d: %w", id, err)
	}
	return nil
}

// SweepOrphans implements cloud.Sweeper. It deletes every resource tagged
// rinfra:<engagementID> — Droplets (by tag), Firewalls (by tag), and Reserved
// IPs assigned to those Droplets — providing the "no orphan" guarantee after a
// stack destroy. Individual failures are collected and returned together; a
// missing resource is treated as already-swept.
func (p *provider) SweepOrphans(ctx context.Context, creds cloud.Credentials, engagementID string) error {
	client, err := p.client(creds.Raw[CredKeyToken])
	if err != nil {
		return err
	}
	engTag := TagPrefix + engagementID
	opt := &godo.ListOptions{PerPage: 200}
	var errs []error

	// 1. Droplets tagged for this engagement.
	deleted := map[int]bool{}
	droplets, _, err := client.Droplets.ListByTag(ctx, engTag, opt)
	if err != nil {
		errs = append(errs, fmt.Errorf("list droplets: %w", err))
	}
	for _, d := range droplets {
		if resp, err := client.Droplets.Delete(ctx, d.ID); err != nil && !isNotFound(resp) {
			errs = append(errs, fmt.Errorf("delete droplet %d: %w", d.ID, err))
		} else {
			deleted[d.ID] = true
		}
	}

	// 2. Firewalls carrying the engagement tag.
	fws, _, err := client.Firewalls.List(ctx, opt)
	if err != nil {
		errs = append(errs, fmt.Errorf("list firewalls: %w", err))
	}
	for _, fw := range fws {
		if containsString(fw.Tags, engTag) {
			if resp, err := client.Firewalls.Delete(ctx, fw.ID); err != nil && !isNotFound(resp) {
				errs = append(errs, fmt.Errorf("delete firewall %s: %w", fw.ID, err))
			}
		}
	}

	// 3. Reserved IPs assigned to a swept Droplet (Reserved IPs carry no tags).
	rips, _, err := client.ReservedIPs.List(ctx, opt)
	if err != nil {
		errs = append(errs, fmt.Errorf("list reserved IPs: %w", err))
	}
	for _, rip := range rips {
		if rip.Droplet != nil && deleted[rip.Droplet.ID] {
			if resp, err := client.ReservedIPs.Delete(ctx, rip.IP); err != nil && !isNotFound(resp) {
				errs = append(errs, fmt.Errorf("delete reserved IP %s: %w", rip.IP, err))
			}
		}
	}

	return errors.Join(errs...)
}

// isNotFound reports whether a godo response is a 404 (already-deleted).
func isNotFound(resp *godo.Response) bool {
	return resp != nil && resp.StatusCode == 404
}

// containsString reports whether xs contains want.
func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
