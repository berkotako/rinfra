// Package cobaltstrike adapts Cobalt Strike to RInfra. It is Fronted-tier:
// RInfra stands up the teamserver and redirectors, then hands off to a human
// operator. Cobalt Strike is license-gated — the customer's license key is
// supplied per engagement and never bundled.
package cobaltstrike

import (
	"context"
	"errors"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
)

func init() { c2.Register(&provider{}) }

type provider struct{}

func (p *provider) Name() string         { return "cobaltstrike" }
func (p *provider) Tier() c2.SupportTier { return c2.TierFronted }

func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	if cfg.LicenseKey == "" {
		return c2.Teamserver{}, errors.New("cobaltstrike.Deploy: customer license key required")
	}
	// TODO(claude-code): start the teamserver using the customer-supplied
	// license, return connection details for a human operator to connect.
	return c2.Teamserver{}, errors.New("cobaltstrike.Deploy: not implemented")
}

func (p *provider) RedirectorConfig(prof domain.Profile) (string, error) {
	// TODO(claude-code): emit reverse-proxy config fronting the teamserver.
	return "", errors.New("cobaltstrike.RedirectorConfig: not implemented")
}

// Control returns no Operator: Cobalt Strike is driven manually by the operator.
func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	return nil, false
}
