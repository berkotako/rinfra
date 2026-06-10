// Package custom adapts an in-house / custom C2 framework to RInfra.
// Orchestrated-tier: you own the operator surface, so it supports full
// automated emulation.
//
// Replace the constants and client interface with your framework's actual
// deploy mechanics and operator API. This adapter is a template that
// demonstrates the Orchestrated-tier pattern.
//
// # Posture
//
// Same as all other adapters: deploy and drive the upstream framework; author
// no payload, implant, or evasion content. If the custom framework is
// commercially licensed, gate Deploy on Config.LicenseKey (see cobaltstrike
// or bruteratel for the pattern).
package custom

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/c2/deploy"
	"github.com/rinfra/rinfra/internal/domain"
)

func init() { c2.Register(&provider{}) }

// Replace these with the actual custom framework's install coordinates.
const (
	customFrameworkName = "custom"
	customPort          = 8080
	// customReleaseURL is the URL of the official framework release archive.
	// Operator: set this to the actual release URL + pin the checksum.
	customReleaseURL = "https://example.com/custom-framework/releases/latest/server_linux"
	customSHA256     = "0000000000000000000000000000000000000000000000000000000000000000"
	customDestPath   = "/usr/local/bin/custom-server"
	customUnit       = "custom-server"
)

type provider struct{}

func (p *provider) Name() string         { return customFrameworkName }
func (p *provider) Tier() c2.SupportTier { return c2.TierOrchestrated }

// Deploy installs the custom framework on the node via SSH.
func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	runner := runnerFromNode(node)
	return deployCustom(ctx, runner, node, cfg)
}

func deployCustom(ctx context.Context, runner deploy.Runner, node domain.Node, _ c2.Config) (c2.Teamserver, error) {
	params := deploy.InstallParams{
		ReleaseURL:  customReleaseURL,
		SHA256:      customSHA256,
		DestPath:    customDestPath,
		SystemdUnit: customUnit,
		ServiceUser: "nobody",
		ExecStart:   fmt.Sprintf("%s --port %d", customDestPath, customPort),
		ExtraSetup: []string{
			"echo '[rinfra-custom] Custom framework installed from official release'",
		},
	}

	if err := deploy.RunInstall(ctx, runner, params); err != nil {
		return c2.Teamserver{}, fmt.Errorf("custom.Deploy: %w", err)
	}

	return c2.Teamserver{
		Host:           node.PublicIP,
		Port:           customPort,
		Status:         "running",
		ConnectionInfo: fmt.Sprintf("Custom framework @ %s:%d", node.PublicIP, customPort),
	}, nil
}

// RedirectorConfig emits an nginx reverse-proxy config.
func (p *provider) RedirectorConfig(prof domain.Profile) (string, error) {
	params := deploy.NginxParams{
		UpstreamHost:     "127.0.0.1",
		UpstreamPort:     customPort,
		ServerName:       prof.RewriteHost,
		RewriteHost:      prof.RewriteHost,
		UseHTTPS:         true,
		SSLCert:          "/etc/ssl/rinfra/server.crt",
		SSLKey:           "/etc/ssl/rinfra/server.key",
		PathRules:        prof.PathRules,
		ExtraServerBlock: "# Custom framework reverse proxy (Orchestrated-tier).",
	}
	cfg, err := deploy.BuildNginxConfig(params)
	if err != nil {
		return "", fmt.Errorf("custom.RedirectorConfig: %w", err)
	}
	return cfg, nil
}

// Control returns an Orchestrated Operator. Replace CustomClient with the
// actual framework's API client.
func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	return &operator{ts: ts, client: &noopCustomClient{}}, true
}

// CustomClient is the operator API interface for the custom framework.
// Replace with the actual API — gRPC, REST, websocket, or stdio.
type CustomClient interface {
	Execute(ctx context.Context, sessionID, command string) (string, error)
	Sessions(ctx context.Context) ([]CustomSession, error)
	StartListener(ctx context.Context, protocol, host string, port int) error
}

// CustomSession is an active session.
type CustomSession struct {
	ID       string
	Hostname string
	Username string
}

// NewOperatorWithClient returns a custom Operator with the given client injected.
func NewOperatorWithClient(ts c2.Teamserver, client CustomClient) c2.Operator {
	return &operator{ts: ts, client: client}
}

type operator struct {
	ts     c2.Teamserver
	client CustomClient
}

func (o *operator) StartListener(ctx context.Context, spec c2.ListenerSpec) error {
	return o.client.StartListener(ctx, spec.Protocol, spec.Bind, customPort)
}

func (o *operator) Sessions(ctx context.Context) ([]c2.Session, error) {
	raw, err := o.client.Sessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("custom: sessions: %w", err)
	}
	out := make([]c2.Session, 0, len(raw))
	for _, s := range raw {
		out = append(out, c2.Session{ID: s.ID, Host: s.Hostname, User: s.Username})
	}
	return out, nil
}

// Execute translates a portable Technique to a custom framework command.
// The mapping below is the same reference pattern as other Orchestrated adapters.
// Replace with the actual framework's command vocabulary.
func (o *operator) Execute(ctx context.Context, sessionID string, t domain.Technique) (domain.Result, error) {
	start := time.Now()

	cmd, err := techniqueToCommand(t)
	if err != nil {
		return domain.Result{
			TechniqueAttackID: t.AttackID,
			Status:            domain.ExecSkipped,
			Output:            err.Error(),
			StartedAt:         start,
			FinishedAt:        time.Now(),
		}, nil
	}

	output, err := o.client.Execute(ctx, sessionID, cmd)
	fin := time.Now()
	if err != nil {
		return domain.Result{
			TechniqueAttackID: t.AttackID,
			Status:            domain.ExecFailed,
			Output:            strings.TrimSpace(output),
			StartedAt:         start,
			FinishedAt:        fin,
			Err:               err.Error(),
		}, nil
	}
	return domain.Result{
		TechniqueAttackID: t.AttackID,
		Status:            domain.ExecSuccess,
		Output:            strings.TrimSpace(output),
		StartedAt:         start,
		FinishedAt:        fin,
	}, nil
}

func techniqueToCommand(t domain.Technique) (string, error) {
	switch t.AttackID {
	case "T1082":
		return "sysinfo", nil
	case "T1057":
		return "ps", nil
	case "T1059.001":
		cmd := t.Inputs["command"]
		if cmd == "" {
			cmd = "whoami"
		}
		return fmt.Sprintf("powershell %s", cmd), nil
	default:
		return "", fmt.Errorf("custom: no command mapping for technique %s", t.AttackID)
	}
}

func runnerFromNode(_ domain.Node) deploy.Runner {
	return &noopRunner{}
}

type noopRunner struct{}

func (n *noopRunner) Run(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("custom: SSH runner not wired (TODO(live))")
}
func (n *noopRunner) Upload(_ context.Context, _, _ string) error {
	return fmt.Errorf("custom: SSH runner not wired (TODO(live))")
}

type noopCustomClient struct{}

func (n *noopCustomClient) Execute(_ context.Context, _, _ string) (string, error) {
	return "", fmt.Errorf("custom: operator client not wired (TODO(live))")
}
func (n *noopCustomClient) Sessions(_ context.Context) ([]CustomSession, error) {
	return nil, fmt.Errorf("custom: operator client not wired (TODO(live))")
}
func (n *noopCustomClient) StartListener(_ context.Context, _, _ string, _ int) error {
	return fmt.Errorf("custom: operator client not wired (TODO(live))")
}
