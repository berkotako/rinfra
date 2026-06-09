// Package havoc adapts the Havoc C2 framework to RInfra. Scripted-tier: Havoc
// has a teamserver API and maturing headless variants, but automating against
// it is less stable, so expect partial scenario coverage. Deploys/drives the
// upstream release; implements nothing offensive.
package havoc

import (
	"context"
	"errors"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
)

func init() { c2.Register(&provider{}) }

type provider struct{}

func (p *provider) Name() string         { return "havoc" }
func (p *provider) Tier() c2.SupportTier { return c2.TierScripted }

func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	return c2.Teamserver{}, errors.New("havoc.Deploy: not implemented")
}

func (p *provider) RedirectorConfig(prof domain.Profile) (string, error) {
	return "", errors.New("havoc.RedirectorConfig: not implemented")
}

func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	return &operator{ts: ts}, true
}

type operator struct{ ts c2.Teamserver }

func (o *operator) StartListener(ctx context.Context, spec c2.ListenerSpec) error {
	return errors.New("havoc.StartListener: not implemented")
}

func (o *operator) Sessions(ctx context.Context) ([]c2.Session, error) {
	return nil, errors.New("havoc.Sessions: not implemented")
}

func (o *operator) Execute(ctx context.Context, sessionID string, t domain.Technique) (domain.Result, error) {
	return domain.Result{}, errors.New("havoc.Execute: not implemented")
}
