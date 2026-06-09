// Package custom adapts an in-house C2 framework to RInfra. Orchestrated-tier:
// you own the operator surface, so it supports full automated emulation.
// Replace the framework name and wire to your own framework's API.
package custom

import (
	"context"
	"errors"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
)

func init() { c2.Register(&provider{}) }

type provider struct{}

func (p *provider) Name() string         { return "custom" }
func (p *provider) Tier() c2.SupportTier { return c2.TierOrchestrated }

func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	return c2.Teamserver{}, errors.New("custom.Deploy: not implemented")
}

func (p *provider) RedirectorConfig(prof domain.Profile) (string, error) {
	return "", errors.New("custom.RedirectorConfig: not implemented")
}

func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	return &operator{ts: ts}, true
}

type operator struct{ ts c2.Teamserver }

func (o *operator) StartListener(ctx context.Context, spec c2.ListenerSpec) error {
	return errors.New("custom.StartListener: not implemented")
}

func (o *operator) Sessions(ctx context.Context) ([]c2.Session, error) {
	return nil, errors.New("custom.Sessions: not implemented")
}

func (o *operator) Execute(ctx context.Context, sessionID string, t domain.Technique) (domain.Result, error) {
	return domain.Result{}, errors.New("custom.Execute: not implemented")
}
