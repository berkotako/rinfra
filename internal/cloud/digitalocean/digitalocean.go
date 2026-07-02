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

// doPerPage is the page size for every DO list call. The DO API caps a single
// page well below the number of resources a large engagement can hold, so every
// list MUST page to the end — otherwise SweepOrphans silently misses orphans
// past the first page and the upserts create duplicates.
const doPerPage = 200

// listAllFirewalls returns every firewall across all pages.
func listAllFirewalls(ctx context.Context, client *godo.Client) ([]godo.Firewall, error) {
	var out []godo.Firewall
	opt := &godo.ListOptions{PerPage: doPerPage}
	for {
		page, resp, err := client.Firewalls.List(ctx, opt)
		if err != nil {
			return out, err
		}
		out = append(out, page...)
		next, done, err := nextPage(resp)
		if err != nil || done {
			return out, err
		}
		opt.Page = next
	}
}

// listAllDropletsByTag returns every droplet carrying tag across all pages.
func listAllDropletsByTag(ctx context.Context, client *godo.Client, tag string) ([]godo.Droplet, error) {
	var out []godo.Droplet
	opt := &godo.ListOptions{PerPage: doPerPage}
	for {
		page, resp, err := client.Droplets.ListByTag(ctx, tag, opt)
		if err != nil {
			return out, err
		}
		out = append(out, page...)
		next, done, err := nextPage(resp)
		if err != nil || done {
			return out, err
		}
		opt.Page = next
	}
}

// listAllReservedIPs returns every reserved IP across all pages.
func listAllReservedIPs(ctx context.Context, client *godo.Client) ([]godo.ReservedIP, error) {
	var out []godo.ReservedIP
	opt := &godo.ListOptions{PerPage: doPerPage}
	for {
		page, resp, err := client.ReservedIPs.List(ctx, opt)
		if err != nil {
			return out, err
		}
		out = append(out, page...)
		next, done, err := nextPage(resp)
		if err != nil || done {
			return out, err
		}
		opt.Page = next
	}
}

// listAllDomainRecords returns every record in a DO domain across all pages.
func listAllDomainRecords(ctx context.Context, client *godo.Client, zone string) ([]godo.DomainRecord, error) {
	var out []godo.DomainRecord
	opt := &godo.ListOptions{PerPage: doPerPage}
	for {
		page, resp, err := client.Domains.Records(ctx, zone, opt)
		if err != nil {
			return out, err
		}
		out = append(out, page...)
		next, done, err := nextPage(resp)
		if err != nil || done {
			return out, err
		}
		opt.Page = next
	}
}

// nextPage reports the next page number to request (and done=true when the
// current page is the last), from a godo response's pagination links.
func nextPage(resp *godo.Response) (next int, done bool, err error) {
	if resp == nil || resp.Links == nil || resp.Links.IsLastPage() {
		return 0, true, nil
	}
	cur, err := resp.Links.CurrentPage()
	if err != nil {
		return 0, true, err
	}
	return cur + 1, false, nil
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

// firewallName is the deterministic, engagement-scoped Cloud Firewall name for a
// node. The engagement is encoded so SweepOrphans can find a node's firewall by
// name — DO firewall Tags are a droplet-TARGET selector, not metadata, so they
// can't be used to label a per-node firewall without attaching it to every
// tagged droplet.
func firewallName(engagementID, nodeID string) string {
	return "rinfra-fw-" + shortID(engagementID) + "-" + shortID(nodeID)
}

// firewallPrefix is the name prefix shared by all of an engagement's per-node
// firewalls, used by SweepOrphans to delete them.
func firewallPrefix(engagementID string) string {
	return "rinfra-fw-" + shortID(engagementID) + "-"
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
			nodeName := fmt.Sprintf("rinfra-%s-%s", shortID(engagementID), shortID(n.ID))
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

	fwName := firewallName(node.EngagementID, node.ID)
	fr := &godo.FirewallRequest{
		Name:         fwName,
		InboundRules: godoInboundRules(rules),
		// Allow-all egress is the standard DO posture (beacons dial out).
		OutboundRules: []godo.OutboundRule{
			{Protocol: "tcp", PortRange: "all", Destinations: &godo.Destinations{Addresses: []string{"0.0.0.0/0", "::/0"}}},
			{Protocol: "udp", PortRange: "all", Destinations: &godo.Destinations{Addresses: []string{"0.0.0.0/0", "::/0"}}},
			{Protocol: "icmp", Destinations: &godo.Destinations{Addresses: []string{"0.0.0.0/0", "::/0"}}},
		},
		// Target ONLY this node's droplet. Do NOT set Tags: in DO the firewall
		// Tags field applies the firewall to every droplet carrying the tag, so
		// tagging with the engagement tag would attach this per-node firewall to
		// all of the engagement's droplets and union everyone's ingress onto
		// everyone. SweepOrphans finds the firewall by its engagement-scoped name.
		DropletIDs: []int{id},
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
	fws, err := listAllFirewalls(ctx, client)
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
	// Upsert by (Type, Name): edit a matching record, else create one. Paginate
	// the full record set so a zone with >200 records doesn't miss the existing
	// record and create a duplicate on every deploy.
	records, err := listAllDomainRecords(ctx, client, rec.Zone)
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
	var errs []error

	// 1. Enumerate this engagement's Droplets first (do NOT delete yet).
	deleted := map[int]bool{}
	droplets, err := listAllDropletsByTag(ctx, client, engTag)
	if err != nil {
		errs = append(errs, fmt.Errorf("list droplets: %w", err))
	}
	engDroplets := map[int]bool{}
	for _, d := range droplets {
		engDroplets[d.ID] = true
	}

	// 2. Record which Reserved IPs are assigned to those Droplets BEFORE deleting
	//    them — once a Droplet is gone, DO reports its Reserved IP as unassigned
	//    (Droplet == nil), which would let the IP survive teardown (orphan).
	var ripsToRelease []string
	rips, err := listAllReservedIPs(ctx, client)
	if err != nil {
		errs = append(errs, fmt.Errorf("list reserved IPs: %w", err))
	}
	for _, rip := range rips {
		if rip.Droplet != nil && engDroplets[rip.Droplet.ID] {
			ripsToRelease = append(ripsToRelease, rip.IP)
		}
	}

	// 3. Delete the Droplets.
	for id := range engDroplets {
		if resp, err := client.Droplets.Delete(ctx, id); err != nil && !isNotFound(resp) {
			errs = append(errs, fmt.Errorf("delete droplet %d: %w", id, err))
		} else {
			deleted[id] = true
		}
	}

	// 4. Firewalls for this engagement, matched by their engagement-scoped name
	//    (per-node firewalls are deliberately untagged — see ConfigureIngress).
	fws, err := listAllFirewalls(ctx, client)
	if err != nil {
		errs = append(errs, fmt.Errorf("list firewalls: %w", err))
	}
	fwPrefix := firewallPrefix(engagementID)
	for _, fw := range fws {
		if strings.HasPrefix(fw.Name, fwPrefix) {
			if resp, err := client.Firewalls.Delete(ctx, fw.ID); err != nil && !isNotFound(resp) {
				errs = append(errs, fmt.Errorf("delete firewall %s: %w", fw.ID, err))
			}
		}
	}

	// 5. Release the Reserved IPs recorded in step 2 (Reserved IPs carry no tags,
	//    so the pre-recorded association is the only reliable signal).
	for _, ip := range ripsToRelease {
		if resp, err := client.ReservedIPs.Delete(ctx, ip); err != nil && !isNotFound(resp) {
			errs = append(errs, fmt.Errorf("delete reserved IP %s: %w", ip, err))
		}
	}

	return errors.Join(errs...)
}

// isNotFound reports whether a godo response is a 404 (already-deleted).
func isNotFound(resp *godo.Response) bool {
	return resp != nil && resp.StatusCode == 404
}

// PerNodeDestroy marks this provider's Destroy as node-scoped (see cloud.PerNodeDestroyer).
func (p *provider) PerNodeDestroy() {}
