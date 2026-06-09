// Package cloud defines the uniform provisioning abstraction over the supported
// clouds. Compute provisioning abstracts cleanly across providers; networking
// (ConfigureIngress, ManageDNS) is where the providers genuinely diverge —
// implement those deliberately per provider, not generically.
package cloud

import (
	"context"

	"github.com/rinfra/rinfra/internal/domain"
)

// Credentials carries the customer's per-engagement cloud credentials. RInfra
// NEVER provisions into its own tenancy — every call uses credentials supplied
// for the engagement. The concrete shape is provider-specific; keep the raw
// material out of logs and the audit trail.
type Credentials struct {
	Provider domain.CloudProviderType
	Raw      map[string]string // provider-specific keys; treat as secret
}

// CloudProvider is implemented once per cloud (aws, gcp, azure, digitalocean).
// All methods take the engagement Credentials so nothing is provisioned without
// customer-supplied auth.
type CloudProvider interface {
	// Type returns the provider this implementation serves.
	Type() domain.CloudProviderType

	// ProvisionNode stands up a single node (compute) and returns it with its
	// ProviderRef and PublicIP populated.
	ProvisionNode(ctx context.Context, creds Credentials, spec domain.NodeSpec) (domain.Node, error)

	// ConfigureIngress applies ingress rules to a node. This is the most
	// divergent method across providers (security groups vs VPC firewall vs
	// cloud firewall vs NSG) — implement per provider with care.
	ConfigureIngress(ctx context.Context, creds Credentials, node domain.Node, rules []domain.Rule) error

	// AssignStaticIP attaches a stable public address to a node.
	AssignStaticIP(ctx context.Context, creds Credentials, node domain.Node) (string, error)

	// ManageDNS upserts a DNS record (for redirector / categorized-domain setups).
	ManageDNS(ctx context.Context, creds Credentials, rec domain.Record) error

	// Destroy tears a node down. Must be idempotent and safe to call during
	// reconciliation when the actual cloud state is uncertain.
	Destroy(ctx context.Context, creds Credentials, node domain.Node) error
}
