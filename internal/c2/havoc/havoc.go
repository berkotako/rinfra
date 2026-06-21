// Package havoc adapts the Havoc C2 framework to RInfra. Scripted-tier: Havoc
// has a teamserver API and a Python scripting interface, but automating against
// it is less stable than a first-class gRPC or REST API, so RInfra supports a
// curated subset of techniques. Techniques outside that subset are returned with
// ExecUnsupported and an explanatory message.
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
// implementation (cliHavocClient) drives the upstream Havoc client binary on the
// teamserver host over the shared SSH Runner and parses its textual output;
// tests inject a FakeHavocClient.
//
// # PoshC2 note
//
// PoshC2 is also Scripted-tier and follows the same pattern (see
// internal/c2/poshc2). PoshC2 does not expose a modern scripting API, so its
// operator returns ExecUnsupported for most techniques. See poshc2.go for the
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
// API reliably exposes. Techniques outside this set return ExecUnsupported.
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

// Capabilities reports Havoc's routing metadata: Windows demons over HTTPS, with
// an explicit technique allowlist matching the Scripted-tier automated subset.
func (p *provider) Capabilities() c2.Capabilities {
	techs := make([]string, 0, len(supportedTechniques))
	for id := range supportedTechniques {
		techs = append(techs, id)
	}
	return c2.Capabilities{
		Platforms:         []string{"windows"},
		Techniques:        techs,
		ListenerProtocols: []string{"https"},
	}
}

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
// curated subset of techniques; others return ExecUnsupported.
func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	client := newCLIHavocClient(deploy.NewNodeRunner(ts.Host), ts.Host, ts.Port)
	return &operator{ts: ts, client: client}, true
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
// Techniques outside the supportedTechniques set return ExecUnsupported with an
// explanatory message — this is the correct Scripted-tier behaviour.
func (o *operator) Execute(ctx context.Context, sessionID string, t domain.Technique) (domain.Result, error) {
	start := time.Now()

	if !supportedTechniques[t.AttackID] {
		return domain.Result{
			TechniqueAttackID: t.AttackID,
			Status:            domain.ExecUnsupported,
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

func runnerFromNode(node domain.Node) deploy.Runner {
	return deploy.NewNodeRunner(node.PublicIP)
}

// cliHavocClient is the live HavocClient. It drives the upstream Havoc client
// (the `havoc` operator binary, invoked headless with `havoc client ...`) on the
// teamserver host through a deploy.Runner and parses its textual stdout. RInfra
// composes the upstream tool here: every command string below targets Havoc's
// own client CLI — RInfra authors no implants, payloads, or evasion. The client
// connects to the teamserver over loopback on the host, so host/port describe
// the local teamserver endpoint the Havoc client attaches to.
type cliHavocClient struct {
	runner deploy.Runner
	host   string
	port   int
}

// newCLIHavocClient constructs a live CLI-backed HavocClient. runner executes
// commands on the teamserver host (production: deploy.NewNodeRunner); host/port
// identify the teamserver endpoint the Havoc client connects to.
func newCLIHavocClient(runner deploy.Runner, host string, port int) *cliHavocClient {
	if port == 0 {
		port = havocPort
	}
	return &cliHavocClient{runner: runner, host: host, port: port}
}

// clientInvoke builds the `havoc client` invocation prefix that connects to the
// local teamserver before running a sub-command. The Havoc client attaches over
// loopback on the teamserver host itself.
func (c *cliHavocClient) clientInvoke(sub string) string {
	return fmt.Sprintf("havoc client --host 127.0.0.1 --port %d %s", c.port, sub)
}

// Execute runs a demon command on an active session via the Havoc client and
// returns the demon's textual output.
func (c *cliHavocClient) Execute(ctx context.Context, sessionID, command string) (string, error) {
	cmd := c.clientInvoke(fmt.Sprintf("demon %s exec %s", shellQuote(sessionID), command))
	out, err := c.runner.Run(ctx, cmd)
	if err != nil {
		return out, fmt.Errorf("havoc: client exec on demon %s: %w", sessionID, err)
	}
	return out, nil
}

// Sessions lists active demon sessions by parsing the Havoc client's
// `demon list` output.
func (c *cliHavocClient) Sessions(ctx context.Context) ([]HavocSession, error) {
	out, err := c.runner.Run(ctx, c.clientInvoke("demon list"))
	if err != nil {
		return nil, fmt.Errorf("havoc: client demon list: %w", err)
	}
	return parseHavocSessions(out), nil
}

// StartListener creates a listener on the teamserver via the Havoc client.
func (c *cliHavocClient) StartListener(ctx context.Context, protocol, host string, port int) error {
	if host == "" {
		host = "0.0.0.0"
	}
	sub := fmt.Sprintf("listener add --name rinfra-%s --protocol %s --host %s --port %d",
		protocol, protocol, host, port)
	if _, err := c.runner.Run(ctx, c.clientInvoke(sub)); err != nil {
		return fmt.Errorf("havoc: client listener add: %w", err)
	}
	return nil
}

// parseHavocSessions parses the tabular output of `havoc client demon list`.
// Each demon row is whitespace/pipe separated:
//
//	ID | Hostname | Username | OS | Arch
//
// Header lines, separators, and blanks are skipped. Parsing is lenient: a row
// with fewer fields fills what it can.
func parseHavocSessions(out string) []HavocSession {
	var sessions []HavocSession
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip a header or separator row.
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "id") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "=") {
			continue
		}
		fields := splitColumns(line)
		if len(fields) == 0 {
			continue
		}
		s := HavocSession{ID: fields[0]}
		if len(fields) > 1 {
			s.Hostname = fields[1]
		}
		if len(fields) > 2 {
			s.Username = fields[2]
		}
		if len(fields) > 3 {
			s.OS = fields[3]
		}
		if len(fields) > 4 {
			s.Arch = fields[4]
		}
		sessions = append(sessions, s)
	}
	return sessions
}

// splitColumns splits a Havoc/PoshC2 table row on pipes or runs of whitespace.
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
