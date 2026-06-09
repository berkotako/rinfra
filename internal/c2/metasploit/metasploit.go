// Package metasploit adapts the Metasploit Framework to RInfra. Orchestrated-tier:
// the msfrpcd RPC API lets RInfra start listeners and drive meterpreter sessions
// programmatically, so it supports automated emulation. It pairs with the
// msfvenom payload generator for initial-access stagers.
//
// SCOPE: deploys and drives the upstream Metasploit release; implements no
// payloads, modules, or evasion. Open source — no license key required.
package metasploit

import (
	"context"
	"errors"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
)

func init() { c2.Register(&provider{}) }

type provider struct{}

func (p *provider) Name() string         { return "metasploit" }
func (p *provider) Tier() c2.SupportTier { return c2.TierOrchestrated }

func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	// TODO(claude-code): install the pinned upstream Metasploit release on the
	// node, start msfrpcd (the RPC daemon), and return connection details.
	return c2.Teamserver{}, errors.New("metasploit.Deploy: not implemented")
}

func (p *provider) RedirectorConfig(prof domain.Profile) (string, error) {
	// TODO(claude-code): emit reverse-proxy config fronting the meterpreter
	// HTTP(S) handler for this profile.
	return "", errors.New("metasploit.RedirectorConfig: not implemented")
}

func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	// Orchestrated tier: Operator backed by the msfrpcd RPC API.
	return &operator{ts: ts}, true
}

type operator struct{ ts c2.Teamserver }

func (o *operator) StartListener(ctx context.Context, spec c2.ListenerSpec) error {
	// TODO(claude-code): start a handler (exploit/multi/handler) via msfrpcd.
	return errors.New("metasploit.StartListener: not implemented")
}

func (o *operator) Sessions(ctx context.Context) ([]c2.Session, error) {
	// TODO(claude-code): list meterpreter sessions via msfrpcd.
	return nil, errors.New("metasploit.Sessions: not implemented")
}

func (o *operator) Execute(ctx context.Context, sessionID string, t domain.Technique) (domain.Result, error) {
	// TODO(claude-code): translate the portable Technique into meterpreter/console
	// commands over msfrpcd, sourcing the concrete procedure from the referenced
	// public library (Atomic Red Team / Caldera). Return a sanitized Result.
	return domain.Result{}, errors.New("metasploit.Execute: not implemented")
}
