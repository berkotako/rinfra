// Package sliver adapts the Sliver C2 framework (Bishop Fox) to RInfra's
// c2.C2Provider interface. Sliver is Orchestrated-tier: it exposes a gRPC
// operator API, so it supports automated emulation via the Operator.
//
// SCOPE: this adapter DEPLOYS and DRIVES the upstream Sliver release; it does
// not implement Sliver, implants, or any payload/evasion logic.
package sliver

import (
	"context"
	"errors"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
)

func init() { c2.Register(&provider{}) }

type provider struct{}

func (p *provider) Name() string         { return "sliver" }
func (p *provider) Tier() c2.SupportTier { return c2.TierOrchestrated }

func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	// TODO(claude-code): install the pinned upstream sliver-server release on
	// the node, start the daemon, generate an operator config, and return the
	// teamserver connection details. Compose the release; do not build it.
	return c2.Teamserver{}, errors.New("sliver.Deploy: not implemented")
}

func (p *provider) RedirectorConfig(prof domain.Profile) (string, error) {
	// TODO(claude-code): emit reverse-proxy config fronting the sliver HTTPS
	// listener for this profile (reverse-proxy + categorized domain; no fronting).
	return "", errors.New("sliver.RedirectorConfig: not implemented")
}

func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	// Orchestrated tier: a real Operator backed by Sliver's gRPC client.
	return &operator{ts: ts}, true
}

type operator struct{ ts c2.Teamserver }

func (o *operator) StartListener(ctx context.Context, spec c2.ListenerSpec) error {
	// TODO(claude-code): call Sliver gRPC to start the listener.
	return errors.New("sliver.StartListener: not implemented")
}

func (o *operator) Sessions(ctx context.Context) ([]c2.Session, error) {
	// TODO(claude-code): list sessions via Sliver gRPC.
	return nil, errors.New("sliver.Sessions: not implemented")
}

func (o *operator) Execute(ctx context.Context, sessionID string, t domain.Technique) (domain.Result, error) {
	// TODO(claude-code): translate the portable Technique into Sliver operator
	// commands, sourcing the concrete procedure from the referenced public
	// library (Atomic Red Team / Caldera). Return a sanitized Result.
	return domain.Result{}, errors.New("sliver.Execute: not implemented")
}
