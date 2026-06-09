// Package poshc2 adapts the PoshC2 framework (Nettitude) to RInfra. Scripted-tier:
// open source with a scriptable server, but automation is less clean than a
// modern gRPC API, so expect partial scenario coverage. Deploys/drives the
// upstream release; implements nothing offensive.
package poshc2

import (
	"context"
	"errors"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
)

func init() { c2.Register(&provider{}) }

type provider struct{}

func (p *provider) Name() string         { return "poshc2" }
func (p *provider) Tier() c2.SupportTier { return c2.TierScripted }

func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	return c2.Teamserver{}, errors.New("poshc2.Deploy: not implemented")
}

func (p *provider) RedirectorConfig(prof domain.Profile) (string, error) {
	return "", errors.New("poshc2.RedirectorConfig: not implemented")
}

func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	return &operator{ts: ts}, true
}

type operator struct{ ts c2.Teamserver }

func (o *operator) StartListener(ctx context.Context, spec c2.ListenerSpec) error {
	return errors.New("poshc2.StartListener: not implemented")
}

func (o *operator) Sessions(ctx context.Context) ([]c2.Session, error) {
	return nil, errors.New("poshc2.Sessions: not implemented")
}

func (o *operator) Execute(ctx context.Context, sessionID string, t domain.Technique) (domain.Result, error) {
	return domain.Result{}, errors.New("poshc2.Execute: not implemented")
}
