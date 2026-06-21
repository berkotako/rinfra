// Package poshc2 adapts the PoshC2 framework (Nettitude) to RInfra. Scripted-tier:
// open source with a scriptable Python server, but the API surface is less clean
// than a modern gRPC or REST API. RInfra supports deploy + redirector and a
// partial operator: only techniques that map reliably to PoshC2 implant commands
// are executed; others return ExecUnsupported.
//
// # Automation seam note
//
// PoshC2 does not expose a stable machine-readable operator API. The partial
// operator implemented here drives the poshc2 implant-handler CLI (the
// `posh` / `poshc2` console, e.g. posh -i to issue implant tasks) via SSH on the
// teamserver host and parses its textual output. This is brittle by design —
// users should prefer Sliver or Mythic for operator-API-driven emulation.
//
// If a future PoshC2 release exposes a proper API, swap the cliPoshC2Client
// command strings for it and expand supportedTechniques.
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

// Capabilities reports PoshC2's routing metadata: Windows/Linux implants over
// HTTPS, with an explicit technique allowlist matching the automated subset.
func (p *provider) Capabilities() c2.Capabilities {
	techs := make([]string, 0, len(supportedTechniques))
	for id := range supportedTechniques {
		techs = append(techs, id)
	}
	return c2.Capabilities{
		Platforms:         []string{"windows", "linux"},
		Techniques:        techs,
		ListenerProtocols: []string{"https"},
	}
}

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
// supportedTechniques return ExecUnsupported.
func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	client := newCLIPoshC2Client(deploy.NewNodeRunner(ts.Host))
	return &operator{ts: ts, client: client}, true
}

// PoshC2Client wraps the PoshC2 implant-handler CLI for limited automation.
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
			Status:            domain.ExecUnsupported,
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

func runnerFromNode(node domain.Node) deploy.Runner {
	return deploy.NewNodeRunner(node.PublicIP)
}

// cliPoshC2Client is the live PoshC2Client. It drives the upstream PoshC2
// implant-handler CLI (`poshc2`) on the teamserver host through a deploy.Runner
// and parses its textual stdout. RInfra composes the upstream tool here: every
// command string below targets PoshC2's own CLI — RInfra authors no implants,
// payloads, or evasion.
type cliPoshC2Client struct {
	runner deploy.Runner
}

// newCLIPoshC2Client constructs a live CLI-backed PoshC2Client. runner executes
// commands on the teamserver host (production: deploy.NewNodeRunner).
func newCLIPoshC2Client(runner deploy.Runner) *cliPoshC2Client {
	return &cliPoshC2Client{runner: runner}
}

// Execute tasks an implant with a command via the PoshC2 implant-handler and
// returns the task output. The handler is driven non-interactively with `-i`
// (implant id) and `-c` (command) flags.
func (c *cliPoshC2Client) Execute(ctx context.Context, implantID, command string) (string, error) {
	cmd := fmt.Sprintf("poshc2 -i %s -c %s", shellQuote(implantID), shellQuote(command))
	out, err := c.runner.Run(ctx, cmd)
	if err != nil {
		return out, fmt.Errorf("poshc2: implant-handler task on %s: %w", implantID, err)
	}
	return out, nil
}

// Implants lists active implants by parsing `poshc2 --list-implants` output.
func (c *cliPoshC2Client) Implants(ctx context.Context) ([]PoshC2Implant, error) {
	out, err := c.runner.Run(ctx, "poshc2 --list-implants")
	if err != nil {
		return nil, fmt.Errorf("poshc2: list implants: %w", err)
	}
	return parsePoshImplants(out), nil
}

// StartListener creates a listener on the PoshC2 server via the implant-handler.
func (c *cliPoshC2Client) StartListener(ctx context.Context, protocol string, port int) error {
	cmd := fmt.Sprintf("poshc2 --create-listener --name rinfra-%s --type %s --port %d",
		protocol, protocol, port)
	if _, err := c.runner.Run(ctx, cmd); err != nil {
		return fmt.Errorf("poshc2: create listener: %w", err)
	}
	return nil
}

// parsePoshImplants parses the tabular output of `poshc2 --list-implants`. Each
// implant row is whitespace/pipe separated:
//
//	ID | Hostname | Username
//
// Header lines, separators, and blanks are skipped; short rows fill what they can.
func parsePoshImplants(out string) []PoshC2Implant {
	var implants []PoshC2Implant
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "id") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "=") {
			continue
		}
		fields := splitColumns(line)
		if len(fields) == 0 {
			continue
		}
		im := PoshC2Implant{ID: fields[0]}
		if len(fields) > 1 {
			im.Hostname = fields[1]
		}
		if len(fields) > 2 {
			im.Username = fields[2]
		}
		implants = append(implants, im)
	}
	return implants
}

// splitColumns splits a PoshC2 table row on pipes or runs of whitespace.
func splitColumns(line string) []string {
	var raw []string
	if strings.Contains(line, "|") {
		raw = strings.Split(line, "|")
	} else {
		raw = strings.Fields(line)
	}
	out := make([]string, 0, len(raw))
	for _, f := range raw {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// shellQuote single-quotes s for safe use as a shell argument.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
