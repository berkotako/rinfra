// Package custom adapts RInfra's in-house / custom C2 framework. It is
// Orchestrated-tier: RInfra owns the operator surface, so it supports full
// automated emulation via the Operator interface.
//
// # Operator API contract
//
// Because the custom framework is in-house, RInfra defines its operator API.
// It is a small REST + JSON surface served by the teamserver on
// customOperatorAPIPort and authenticated with a per-engagement bearer token
// (Authorization: Bearer <token>). The contract, implemented live by
// httpCustomClient (client.go):
//
//	POST /api/v1/listeners
//	    req:  {"name","protocol","bind","port"}
//	    resp: 2xx on success (body ignored); non-2xx => error
//
//	GET /api/v1/sessions
//	    resp: [{"id","host","user"}, ...]
//
//	POST /api/v1/sessions/{id}/exec
//	    req:  {"command"}
//	    resp: {"output"}
//
// Any non-2xx response is surfaced as a Go error. Control() constructs the live
// client against the deployed teamserver; tests inject a fake via
// NewOperatorWithClient or stand up an httptest server (client_test.go).
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
	"github.com/rinfra/rinfra/internal/emulation/ttp"
)

func init() { c2.Register(&provider{}) }

const (
	customFrameworkName = "custom"
	// customPort is the implant-facing listener port fronted by the redirector.
	customPort = 8080
	// customOperatorAPIPort is the port the in-house teamserver serves its
	// operator REST API on (see the package doc for the contract). It is
	// separate from the implant listener port and is not exposed through the
	// redirector — operators reach it directly over the engagement's control
	// channel.
	customOperatorAPIPort = 9443
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

// Control returns an Orchestrated-tier Operator backed by the live HTTP
// operator-API client. The custom framework is RInfra's own in-house C2, so the
// operator API is defined here (see httpCustomClient for the contract). The
// client targets the teamserver's operator API, which listens on
// customOperatorAPIPort (see deriveOperatorBaseURL); its bearer token is read
// from the per-engagement environment (EnvCustomAPIToken). Tests inject a fake
// via NewOperatorWithClient or stand up an httptest server (client_test.go).
func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	return &operator{ts: ts, client: newHTTPClientForTeamserver(ts)}, true
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
			Status:            domain.ExecUnsupported,
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

// techniqueToCommand compiles a portable Technique to a custom-framework command
// via the shared catalog (ttp.Compile) + renderCustomPrimitive. The custom
// framework implements only a narrow primitive set; anything else is reported
// unsupported.
func techniqueToCommand(t domain.Technique) (string, error) {
	prim, ok, err := ttp.Compile(t)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("custom: no catalog mapping for technique %s", t.AttackID)
	}
	return renderCustomPrimitive(prim)
}

// renderCustomPrimitive renders a portable primitive into the custom framework's
// command vocabulary. Replace with the real framework's verbs; primitives it
// does not implement return an error so the caller records them unsupported.
func renderCustomPrimitive(p c2.Primitive) (string, error) {
	switch p.Kind {
	case c2.PrimSysInfo:
		return "sysinfo", nil
	case c2.PrimProcessList:
		return "ps", nil
	case c2.PrimPowerShell:
		return fmt.Sprintf("powershell %s", p.Arg("script")), nil
	default:
		return "", fmt.Errorf("custom: unsupported primitive %q", p.Kind)
	}
}

// runnerFromNode builds the production SSH Runner for a node. SSH key material
// is loaded from the per-engagement environment by deploy.NewNodeRunner.
func runnerFromNode(node domain.Node) deploy.Runner {
	return deploy.NewNodeRunner(node.PublicIP)
}
