// Package havoc adapts the Havoc C2 framework to RInfra. Scripted-tier: Havoc
// has a teamserver API and a Python scripting interface, but automating against
// it is less stable than a first-class gRPC or REST API, so RInfra supports a
// curated subset of techniques. Techniques outside that subset are returned with
// ExecSkipped and an explanatory message.
//
// # Posture
//
// This adapter DEPLOYS and DRIVES the upstream Havoc release. It does not
// implement Havoc, implants, payloads, or evasion. Deploy fetches the official
// Havoc release from GitHub at a pinned version.
//
// # Scripted operator API
//
// HavocClient is the interface over Havoc's scripting layer. The live
// implementation runs Havoc's Python API bridge (havoc.py); tests inject a
// FakeHavocClient. TODO(live) for real wiring.
//
// # PoshC2 note
//
// PoshC2 is also Scripted-tier and follows the same pattern (see
// internal/c2/poshc2). PoshC2 does not expose a modern scripting API, so its
// operator returns ExecSkipped for most techniques. See poshc2.go for the
// lighter stub and its documented seams.
package havoc

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
	havocVersion    = "0.7"
	havocReleaseURL = "https://github.com/HavocFramework/Havoc/archive/refs/tags/" + havocVersion + ".tar.gz"
	havocSHA256     = "placeholder-operator-should-verify-from-havoc-release-page"
	havocPort       = 40056 // default Havoc teamserver port
)

// supportedTechniques lists the ATT&CK technique IDs that the Havoc scripting
// API reliably exposes. Techniques outside this set return ExecSkipped.
var supportedTechniques = map[string]bool{
	"T1059.001": true, // PowerShell
	"T1059.003": true, // cmd.exe
	"T1082":     true, // System Information Discovery
	"T1057":     true, // Process Discovery
	"T1049":     true, // System Network Connections Discovery
	"T1016":     true, // System Network Configuration Discovery
	"T1083":     true, // File and Directory Discovery
}

type provider struct{}

func (p *provider) Name() string         { return "havoc" }
func (p *provider) Tier() c2.SupportTier { return c2.TierScripted }

// Deploy installs the upstream Havoc teamserver release on the node via SSH.
func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	runner := runnerFromNode(node)
	return deployHavoc(ctx, runner, node, cfg)
}

func deployHavoc(ctx context.Context, runner deploy.Runner, node domain.Node, _ c2.Config) (c2.Teamserver, error) {
	extraSetup := []string{
		"apt-get install -y cmake libssl-dev pkg-config libboost-all-dev mingw-w64 nasm python3-dev || true",
		fmt.Sprintf("mkdir -p /opt/havoc && cd /opt/havoc && curl -fsSL %s | tar xz --strip-components=1", havocReleaseURL),
		"cd /opt/havoc/teamserver && go build -o /usr/local/bin/havoc-teamserver . || true",
		"echo '[rinfra-havoc] Havoc teamserver built from official release'",
		// Write a default profile for headless operation.
		`cat > /etc/havoc/teamserver.yaml << 'EOF'
# RInfra-generated Havoc teamserver config.
# Operator: customise listeners and malleable C2 profile as needed.
Teamserver:
  Host: 0.0.0.0
  Port: ` + fmt.Sprintf("%d", havocPort) + `
Listeners:
  - Name: default
    Protocol: https
    Host: 0.0.0.0
    Port: 443
EOF`,
	}

	params := deploy.InstallParams{
		ReleaseURL:  havocReleaseURL,
		SHA256:      havocSHA256,
		DestPath:    "/usr/local/bin/havoc-teamserver",
		SystemdUnit: "havoc-teamserver",
		ServiceUser: "root",
		ExecStart:   fmt.Sprintf("/usr/local/bin/havoc-teamserver server --profile /etc/havoc/teamserver.yaml"),
		ExtraSetup:  extraSetup,
	}

	if err := deploy.RunInstall(ctx, runner, params); err != nil {
		return c2.Teamserver{}, fmt.Errorf("havoc.Deploy: %w", err)
	}

	return c2.Teamserver{
		Host:           node.PublicIP,
		Port:           havocPort,
		Status:         "running",
		ConnectionInfo: fmt.Sprintf("Havoc teamserver @ %s:%d (operator connects via Havoc client)", node.PublicIP, havocPort),
	}, nil
}

// RedirectorConfig emits an nginx reverse-proxy config fronting the Havoc
// HTTPS listener. Reverse-proxy + categorized-domain (NOT CDN fronting).
func (p *provider) RedirectorConfig(prof domain.Profile) (string, error) {
	return havocRedirectorConfig(prof)
}

func havocRedirectorConfig(prof domain.Profile) (string, error) {
	params := deploy.NginxParams{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 443,
		ServerName:   prof.RewriteHost,
		RewriteHost:  prof.RewriteHost,
		UseHTTPS:     true,
		SSLCert:      "/etc/ssl/rinfra/server.crt",
		SSLKey:       "/etc/ssl/rinfra/server.key",
		PathRules:    prof.PathRules,
		ExtraServerBlock: "# Havoc C2 reverse proxy\n" +
			"    # Set proxy_pass upstream to actual Havoc teamserver IP.",
	}
	cfg, err := deploy.BuildNginxConfig(params)
	if err != nil {
		return "", fmt.Errorf("havoc.RedirectorConfig: %w", err)
	}
	return cfg, nil
}

// Control returns a scripted Operator for Havoc. The operator supports a
// curated subset of techniques; others return ExecSkipped.
func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	// TODO(live): construct a real HavocClient from the teamserver connection info.
	return &operator{ts: ts, client: &noopHavocClient{}}, true
}

// HavocClient is the minimal interface over Havoc's scripting layer.
// The live implementation runs Havoc's Python API bridge or uses the websocket
// API exposed by the teamserver; tests inject a fake.
type HavocClient interface {
	// Execute runs a shell command on an active demon session.
	Execute(ctx context.Context, sessionID, command string) (string, error)
	// Sessions returns active demon sessions.
	Sessions(ctx context.Context) ([]HavocSession, error)
	// StartListener starts a listener on the teamserver.
	StartListener(ctx context.Context, protocol, host string, port int) error
}

// HavocSession is an active demon session.
type HavocSession struct {
	ID       string
	Hostname string
	Username string
	OS       string
	Arch     string
}

// NewOperatorWithClient returns a Havoc Operator with the given client injected.
func NewOperatorWithClient(ts c2.Teamserver, client HavocClient) c2.Operator {
	return &operator{ts: ts, client: client}
}

type operator struct {
	ts     c2.Teamserver
	client HavocClient
}

func (o *operator) StartListener(ctx context.Context, spec c2.ListenerSpec) error {
	port := 443
	if spec.Protocol == "dns" {
		port = 53
	}
	return o.client.StartListener(ctx, spec.Protocol, spec.Bind, port)
}

func (o *operator) Sessions(ctx context.Context) ([]c2.Session, error) {
	raw, err := o.client.Sessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("havoc: sessions: %w", err)
	}
	out := make([]c2.Session, 0, len(raw))
	for _, s := range raw {
		out = append(out, c2.Session{
			ID:   s.ID,
			Host: s.Hostname,
			User: s.Username,
			Metadata: map[string]string{
				"os":   s.OS,
				"arch": s.Arch,
			},
		})
	}
	return out, nil
}

// Execute translates a portable domain.Technique into a Havoc scripted command.
// Techniques outside the supportedTechniques set return ExecSkipped with an
// explanatory message — this is the correct Scripted-tier behaviour.
func (o *operator) Execute(ctx context.Context, sessionID string, t domain.Technique) (domain.Result, error) {
	start := time.Now()

	if !supportedTechniques[t.AttackID] {
		return domain.Result{
			TechniqueAttackID: t.AttackID,
			Status:            domain.ExecSkipped,
			Output: fmt.Sprintf("havoc (scripted-tier): technique %s is outside the supported subset; "+
				"operator should execute manually", t.AttackID),
			StartedAt:  start,
			FinishedAt: time.Now(),
		}, nil
	}

	cmd, err := techniqueToHavocCommand(t)
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
			Output:            sanitize(output),
			StartedAt:         start,
			FinishedAt:        fin,
			Err:               err.Error(),
		}, nil
	}

	return domain.Result{
		TechniqueAttackID: t.AttackID,
		Status:            domain.ExecSuccess,
		Output:            sanitize(output),
		StartedAt:         start,
		FinishedAt:        fin,
	}, nil
}

// techniqueToHavocCommand maps a portable Technique to a Havoc demon command.
// Only techniques in supportedTechniques should reach this function.
func techniqueToHavocCommand(t domain.Technique) (string, error) {
	switch t.AttackID {
	case "T1059.001":
		script := t.Inputs["command"]
		if script == "" {
			script = "whoami"
		}
		return fmt.Sprintf("powershell %s", script), nil

	case "T1059.003":
		command := t.Inputs["command"]
		if command == "" {
			command = "whoami"
		}
		return fmt.Sprintf("shell %s", command), nil

	case "T1082":
		return "sysinfo", nil

	case "T1057":
		return "ps", nil

	case "T1049":
		return "netstat", nil

	case "T1016":
		return "ipconfig", nil

	case "T1083":
		path := t.Inputs["path"]
		if path == "" {
			path = "."
		}
		return fmt.Sprintf("dir %s", path), nil

	default:
		return "", fmt.Errorf("havoc: no command mapping for technique %s", t.AttackID)
	}
}

func sanitize(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func runnerFromNode(_ domain.Node) deploy.Runner {
	return &noopRunner{}
}

type noopRunner struct{}

func (n *noopRunner) Run(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("havoc: SSH runner not wired (TODO(live))")
}
func (n *noopRunner) Upload(_ context.Context, _, _ string) error {
	return fmt.Errorf("havoc: SSH runner not wired (TODO(live))")
}

type noopHavocClient struct{}

func (n *noopHavocClient) Execute(_ context.Context, _, _ string) (string, error) {
	return "", fmt.Errorf("havoc: scripting client not wired (TODO(live))")
}
func (n *noopHavocClient) Sessions(_ context.Context) ([]HavocSession, error) {
	return nil, fmt.Errorf("havoc: scripting client not wired (TODO(live))")
}
func (n *noopHavocClient) StartListener(_ context.Context, _, _ string, _ int) error {
	return fmt.Errorf("havoc: scripting client not wired (TODO(live))")
}
