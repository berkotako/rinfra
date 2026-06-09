// Package mythic adapts the Mythic C2 framework to RInfra. Orchestrated-tier:
// Mythic exposes a scripting/GraphQL API and modular C2 profiles, so it supports
// automated emulation. This adapter deploys and drives upstream Mythic; it does
// not implement Mythic, agents, or payloads.
package mythic

import (
	"context"
	"errors"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
)

func init() { c2.Register(&provider{}) }

type provider struct{}

func (p *provider) Name() string         { return "mythic" }
func (p *provider) Tier() c2.SupportTier { return c2.TierOrchestrated }

func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	return c2.Teamserver{}, errors.New("mythic.Deploy: not implemented")
}

func (p *provider) RedirectorConfig(prof domain.Profile) (string, error) {
	return "", errors.New("mythic.RedirectorConfig: not implemented")
}

func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	return &operator{ts: ts}, true
}

type operator struct{ ts c2.Teamserver }

func (o *operator) StartListener(ctx context.Context, spec c2.ListenerSpec) error {
	return errors.New("mythic.StartListener: not implemented")
}

func (o *operator) Sessions(ctx context.Context) ([]c2.Session, error) {
	return nil, errors.New("mythic.Sessions: not implemented")
}

func (o *operator) Execute(ctx context.Context, sessionID string, t domain.Technique) (domain.Result, error) {
	return domain.Result{}, errors.New("mythic.Execute: not implemented")
}
