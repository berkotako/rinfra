// Package azure adapts Microsoft Azure to RInfra's cloud.CloudProvider interface.
// Provisions into the customer's account using per-engagement credentials.
// NOTE: ConfigureIngress is the most divergent method — translate rules to Network Security Groups (NSGs).
package azure

import (
	"context"
	"errors"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
)

func init() { cloud.Register(&provider{}) }

type provider struct{}

func (p *provider) Type() domain.CloudProviderType { return domain.CloudAzure }

func (p *provider) ProvisionNode(ctx context.Context, creds cloud.Credentials, spec domain.NodeSpec) (domain.Node, error) {
	return domain.Node{}, errors.New("azure.ProvisionNode: not implemented")
}

func (p *provider) ConfigureIngress(ctx context.Context, creds cloud.Credentials, node domain.Node, rules []domain.Rule) error {
	return errors.New("azure.ConfigureIngress: not implemented")
}

func (p *provider) AssignStaticIP(ctx context.Context, creds cloud.Credentials, node domain.Node) (string, error) {
	return "", errors.New("azure.AssignStaticIP: not implemented")
}

func (p *provider) ManageDNS(ctx context.Context, creds cloud.Credentials, rec domain.Record) error {
	return errors.New("azure.ManageDNS: not implemented")
}

func (p *provider) Destroy(ctx context.Context, creds cloud.Credentials, node domain.Node) error {
	return errors.New("azure.Destroy: not implemented")
}
