// Package bruteratel adapts Brute Ratel C4 to RInfra. Fronted-tier: commercial,
// license-gated, EDR-evasion-focused, with no clean public automation API —
// RInfra stands up the server and redirectors, then a human operates it. The
// customer's license is supplied per engagement and never bundled.
package bruteratel

import (
	"context"
	"errors"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
)

func init() { c2.Register(&provider{}) }

type provider struct{}

func (p *provider) Name() string         { return "bruteratel" }
func (p *provider) Tier() c2.SupportTier { return c2.TierFronted }

func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	if cfg.LicenseKey == "" {
		return c2.Teamserver{}, errors.New("bruteratel.Deploy: customer license key required")
	}
	return c2.Teamserver{}, errors.New("bruteratel.Deploy: not implemented")
}

func (p *provider) RedirectorConfig(prof domain.Profile) (string, error) {
	return "", errors.New("bruteratel.RedirectorConfig: not implemented")
}

// Control returns no Operator: Brute Ratel is driven manually by the operator.
func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	return nil, false
}
