// Package poshc2 adapts the PoshC2 framework (Nettitude) to RInfra. Scripted-tier:
// open source with a scriptable Python server, but the API surface is less clean
// than a modern gRPC or REST API. RInfra supports deploy + redirector and a
// partial operator: only techniques that map reliably to PoshC2 implant commands
// are executed; others return ExecSkipped.
//
// # Automation seam note
//
// PoshC2 does not expose a stable machine-readable operator API. The partial
// operator implemented here uses the poshc2 Python CLI (posh-get-implants,
// posh-shell-command, etc.) via SSH. This is brittle by design — users should
// prefer Sliver or Mythic for operator-API-driven emulation.
//
// TODO(live): if a future PoshC2 release exposes a proper API, update
// PoshC2Client to use it and expand supportedTechniques.
package poshc2

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

const (
	poshVersion    = "8.0.3"
	poshReleaseURL = "https://github.com/nettitude/PoshC2/archive/refs/tags/v" + poshVersion + ".tar.gz"
	poshSHA256     = "placeholder-operator-should-verify-from-nettitude-release"
	poshPort       = 443
)

// supportedTechniques is the narrow set PoshC2's CLI wrapper reliably handles.
var supportedTechniques = map[string]bool{
	"T1059.001": true, // PowerShell
	"T1082":     true, // System Information Discovery
	"T1057":     true, // Process Discovery
	"T1083":     true, // File and Directory Discovery
}

type provider struct{}

func (p *provider) Name() string         { return "poshc2" }
func (p *provider) Tier() c2.SupportTier { return c2.TierScripted }

// Deploy installs PoshC2 on the node via SSH using the official release.
func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	runner := runnerFromNode(node)
	return deployPoshC2(ctx, runner, node, cfg)
}

func deployPoshC2(ctx context.Context, runner deploy.Runner, node domain.Node, _ c2.Config) (c2.Teamserver, error) {
	extraSetup := []string{
		"apt-get install -y python3 python3-pip || true",
		fmt.Sprintf("mkdir -p /opt/poshc2 && cd /opt/poshc2 && curl -fsSL %s | tar xz --strip-components=1", poshReleaseURL),
		"cd /opt/poshc2 && pip3 install -r requirements.txt || true",
		"cd /opt/poshc2 && python3 poshc2 server &",
		"sleep 2 && pkill -f 'poshc2 server' || true",
		"echo '[rinfra-poshc2] PoshC2 installed from official release'",
	}

	params := deploy.InstallParams{
		ReleaseURL:  poshReleaseURL,
		SHA256:      poshSHA256,
		DestPath:    "/opt/poshc2/poshc2",
		SystemdUnit: "poshc2-server",
		ServiceUser: "root",
		ExecStart:   "python3 /opt/poshc2/poshc2 server",
		ExtraSetup:  extraSetup,
	}

	if err := deploy.RunInstall(ctx, runner, params); err != nil {
		return c2.Teamserver{}, fmt.Errorf("poshc2.Deploy: %w", err)
	}

	return c2.Teamserver{
		Host:           node.PublicIP,
		Port:           poshPort,
		Status:         "running",
		ConnectionInfo: fmt.Sprintf("PoshC2 server @ %s:%d", node.PublicIP, poshPort),
	}, nil
}

// RedirectorConfig emits an nginx reverse-proxy config fronting PoshC2.
func (p *provider) RedirectorConfig(prof domain.Profile) (string, error) {
	params := deploy.NginxParams{
		UpstreamHost:     "127.0.0.1",
		UpstreamPort:     poshPort,
		ServerName:       prof.RewriteHost,
		RewriteHost:      prof.RewriteHost,
		UseHTTPS:         true,
		SSLCert:          "/etc/ssl/rinfra/server.crt",
		SSLKey:           "/etc/ssl/rinfra/server.key",
		PathRules:        prof.PathRules,
		ExtraServerBlock: "# PoshC2 reverse proxy (Scripted-tier).",
	}
	cfg, err := deploy.BuildNginxConfig(params)
	if err != nil {
		return "", fmt.Errorf("poshc2.RedirectorConfig: %w", err)
	}
	return cfg, nil
}

// Control returns a scripted partial Operator. Techniques outside
// supportedTechniques return ExecSkipped.
func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	return &operator{ts: ts, client: &noopPoshC2Client{}}, true
}

// PoshC2Client wraps the PoshC2 Python CLI for limited automation.
// TODO(live): replace with a proper API client if Nettitude publishes one.
type PoshC2Client interface {
	Execute(ctx context.Context, implantID, command string) (string, error)
	Implants(ctx context.Context) ([]PoshC2Implant, error)
	StartListener(ctx context.Context, protocol string, port int) error
}

// PoshC2Implant is an active implant session.
type PoshC2Implant struct {
	ID       string
	Hostname string
	Username string
}

// NewOperatorWithClient returns a PoshC2 Operator with the given client injected.
func NewOperatorWithClient(ts c2.Teamserver, client PoshC2Client) c2.Operator {
	return &operator{ts: ts, client: client}
}

type operator struct {
	ts     c2.Teamserver
	client PoshC2Client
}

func (o *operator) StartListener(ctx context.Context, spec c2.ListenerSpec) error {
	port := 443
	return o.client.StartListener(ctx, spec.Protocol, port)
}

func (o *operator) Sessions(ctx context.Context) ([]c2.Session, error) {
	implants, err := o.client.Implants(ctx)
	if err != nil {
		return nil, fmt.Errorf("poshc2: implants: %w", err)
	}
	out := make([]c2.Session, 0, len(implants))
	for _, im := range implants {
		out = append(out, c2.Session{
			ID:   im.ID,
			Host: im.Hostname,
			User: im.Username,
		})
	}
	return out, nil
}

func (o *operator) Execute(ctx context.Context, implantID string, t domain.Technique) (domain.Result, error) {
	start := time.Now()

	if !supportedTechniques[t.AttackID] {
		return domain.Result{
			TechniqueAttackID: t.AttackID,
			Status:            domain.ExecSkipped,
			Output: fmt.Sprintf("poshc2 (scripted-tier): technique %s is outside the supported subset; "+
				"operator should execute manually via PoshC2 client", t.AttackID),
			StartedAt:  start,
			FinishedAt: time.Now(),
		}, nil
	}

	cmd := techniqueToCommand(t)
	output, err := o.client.Execute(ctx, implantID, cmd)
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

func techniqueToCommand(t domain.Technique) string {
	switch t.AttackID {
	case "T1059.001":
		cmd := t.Inputs["command"]
		if cmd == "" {
			cmd = "whoami"
		}
		return cmd
	case "T1082":
		return "$env:COMPUTERNAME; [System.Environment]::OSVersion"
	case "T1057":
		return "Get-Process | Select-Object Id, ProcessName"
	case "T1083":
		path := t.Inputs["path"]
		if path == "" {
			path = "."
		}
		return fmt.Sprintf("Get-ChildItem '%s'", path)
	default:
		return "whoami"
	}
}

func runnerFromNode(_ domain.Node) deploy.Runner {
	return &noopRunner{}
}

type noopRunner struct{}

func (n *noopRunner) Run(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("poshc2: SSH runner not wired (TODO(live))")
}
func (n *noopRunner) Upload(_ context.Context, _, _ string) error {
	return fmt.Errorf("poshc2: SSH runner not wired (TODO(live))")
}

type noopPoshC2Client struct{}

func (n *noopPoshC2Client) Execute(_ context.Context, _, _ string) (string, error) {
	return "", fmt.Errorf("poshc2: client not wired (TODO(live))")
}
func (n *noopPoshC2Client) Implants(_ context.Context) ([]PoshC2Implant, error) {
	return nil, fmt.Errorf("poshc2: client not wired (TODO(live))")
}
func (n *noopPoshC2Client) StartListener(_ context.Context, _ string, _ int) error {
	return fmt.Errorf("poshc2: client not wired (TODO(live))")
}
