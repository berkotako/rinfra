// Package digitalocean adapts DigitalOcean to RInfra's cloud.CloudProvider
// interface. Per the build order, this is the FIRST cloud to implement: most
// permissive AUP, cheapest to iterate. Provisioning uses the customer's
// per-engagement credentials — never a shared RInfra account.
package digitalocean

import (
	"context"
	"errors"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
)

func init() { cloud.Register(&provider{}) }

type provider struct{}

func (p *provider) Type() domain.CloudProviderType { return domain.CloudDigitalOcean }

func (p *provider) ProvisionNode(ctx context.Context, creds cloud.Credentials, spec domain.NodeSpec) (domain.Node, error) {
	// TODO(claude-code): create a droplet via Pulumi (Go SDK) using creds,
	// return the Node with ProviderRef + PublicIP populated.
	return domain.Node{}, errors.New("digitalocean.ProvisionNode: not implemented")
}

func (p *provider) ConfigureIngress(ctx context.Context, creds cloud.Credentials, node domain.Node, rules []domain.Rule) error {
	// TODO(claude-code): translate rules to a DO Cloud Firewall. This differs
	// from AWS security groups / GCP firewall / Azure NSG — implement carefully.
	return errors.New("digitalocean.ConfigureIngress: not implemented")
}

func (p *provider) AssignStaticIP(ctx context.Context, creds cloud.Credentials, node domain.Node) (string, error) {
	// TODO(claude-code): reserve + assign a DO Reserved IP.
	return "", errors.New("digitalocean.AssignStaticIP: not implemented")
}

func (p *provider) ManageDNS(ctx context.Context, creds cloud.Credentials, rec domain.Record) error {
	// TODO(claude-code): upsert a DO DNS record.
	return errors.New("digitalocean.ManageDNS: not implemented")
}

func (p *provider) Destroy(ctx context.Context, creds cloud.Credentials, node domain.Node) error {
	// TODO(claude-code): destroy the droplet + associated resources; idempotent.
	return errors.New("digitalocean.Destroy: not implemented")
}
