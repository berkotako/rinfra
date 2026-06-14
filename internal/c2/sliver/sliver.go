// Package sliver adapts the Sliver C2 framework (Bishop Fox) to RInfra's
// c2.C2Provider interface. Sliver is Orchestrated-tier: it exposes a gRPC
// operator API (mTLS multiplayer config), so it supports automated emulation
// via the Operator.
//
// # Posture
//
// This adapter DEPLOYS and DRIVES the upstream Sliver release. It does not
// implement Sliver, implants, shellcode, encoders, or evasion. The deploy path
// fetches the official Sliver release binary from GitHub by pinned URL and
// SHA-256 checksum. The operator API surface is defined as a local interface
// (SliverClient) so tests inject a fake without pulling the full gRPC SDK.
//
// # gRPC client note (TODO:live)
//
// The SliverClient interface below maps the subset of Sliver's gRPC operator
// API used by RInfra. The live implementation wraps the official
// github.com/bishopfox/sliver/client/transport package, which generates a
// mTLS operator config and connects to the multiplayer listener. Because the
// sliver/client tree carries a large and occasionally unstable dependency
// surface, the live wiring is deferred behind TODO(live) markers rather than
// added to go.mod today. Tests use FakeSliverClient defined in this package.
package sliver

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

// Pinned upstream release coordinates for sliver-server. The operator/customer
// is responsible for verifying the official release. URL and checksum are
// parameters — never hardcoded payload content.
const (
	sliverVersion    = "v1.5.42"
	sliverReleaseURL = "https://github.com/BishopFox/sliver/releases/download/" + sliverVersion + "/sliver-server_linux"
	sliverSHA256     = "c4d8c3eadc15f07ac6a0614e0f1ea6f31e6c8c2eab9c93f265bc7a9ae6abd6c2" // pinned; operator should verify
	sliverDestPath   = "/usr/local/bin/sliver-server"
	sliverUnit       = "sliver-server"
	sliverPort       = 31337 // default multiplayer port
)

type provider struct{}

func (p *provider) Name() string         { return "sliver" }
func (p *provider) Tier() c2.SupportTier { return c2.TierOrchestrated }

// Deploy installs the upstream sliver-server release on the node via SSH.
// It writes a multiplayer operator config and starts the sliver-server
// systemd service.
//
// The runner parameter is the SSH execution seam. In production the caller
// (service layer) constructs a real SSH runner from per-engagement key material;
// tests inject a FakeRunner.
func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	runner := runnerFromNode(node)
	return deploySliver(ctx, runner, node, cfg)
}

// deploySliver is the testable inner implementation of Deploy.
func deploySliver(ctx context.Context, runner deploy.Runner, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	extraSetup := []string{
		// Generate a multiplayer operator config so the gRPC client can connect.
		fmt.Sprintf("%s daemon --lhost 0.0.0.0 --lport %d &", sliverDestPath, sliverPort),
		"sleep 2",
		"pkill -f sliver-server || true", // stop the background start
		// The daemon will be re-started by systemd.
		"echo '[rinfra-sliver] operator config: see /root/.sliver/configs/'",
	}

	params := deploy.InstallParams{
		ReleaseURL:  sliverReleaseURL,
		SHA256:      sliverSHA256,
		DestPath:    sliverDestPath,
		SystemdUnit: sliverUnit,
		ServiceUser: "root", // sliver-server requires root for raw socket ops
		ExecStart:   fmt.Sprintf("%s daemon --lhost 0.0.0.0 --lport %d", sliverDestPath, sliverPort),
		ExtraSetup:  extraSetup,
	}

	if err := deploy.RunInstall(ctx, runner, params); err != nil {
		return c2.Teamserver{}, fmt.Errorf("sliver.Deploy: %w", err)
	}

	return c2.Teamserver{
		Host:           node.PublicIP,
		Port:           sliverPort,
		Status:         "running",
		ConnectionInfo: fmt.Sprintf("sliver-server multiplayer @ %s:%d (mTLS)", node.PublicIP, sliverPort),
	}, nil
}

// RedirectorConfig emits an nginx reverse-proxy config fronting the sliver
// HTTPS listener. Uses a reverse-proxy + categorized-domain pattern (NOT CDN
// domain fronting, which is effectively dead — see CLAUDE.md redirector note).
func (p *provider) RedirectorConfig(prof domain.Profile) (string, error) {
	return redirectorConfig(prof)
}

func redirectorConfig(prof domain.Profile) (string, error) {
	upstream := "127.0.0.1" // redirector proxies to the C2 server IP set at deploy time
	params := deploy.NginxParams{
		UpstreamHost: upstream,
		UpstreamPort: 443,
		ServerName:   prof.RewriteHost,
		RewriteHost:  prof.RewriteHost,
		UseHTTPS:     true,
		SSLCert:      "/etc/ssl/rinfra/server.crt",
		SSLKey:       "/etc/ssl/rinfra/server.key",
		PathRules:    prof.PathRules,
		ExtraServerBlock: "# Sliver HTTPS C2 reverse proxy\n" +
			"    # Operator note: set proxy_pass upstream to the actual C2 server IP.",
	}
	cfg, err := deploy.BuildNginxConfig(params)
	if err != nil {
		return "", fmt.Errorf("sliver.RedirectorConfig: %w", err)
	}
	return cfg, nil
}

// Control returns an Operator backed by a SliverClient for the gRPC operator
// API. The live client requires a multiplayer operator config file generated
// by sliver-server; tests inject a FakeSliverClient.
func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	// The mTLS transport to the multiplayer listener is wired in transport.go:
	// load the operator config generated during Deploy with LoadOperatorConfig,
	// then DialOperator to obtain an authenticated *grpc.ClientConn. The
	// remaining inch is binding that conn to Sliver's generated rpcpb/sliverpb
	// stubs so SliverClient calls issue real RPCs; until those stubs are linked
	// the operator uses the noop client. Tests inject via NewOperatorWithClient.
	return &operator{ts: ts, client: noopSliverClient{}}, true
}

// SliverClient is the minimal operator API surface RInfra needs from Sliver's
// gRPC multiplayer layer. The live implementation wraps the official
// bishopfox/sliver transport; tests use FakeSliverClient.
type SliverClient interface {
	// StartMTLSListener starts an mTLS listener on the teamserver.
	StartMTLSListener(ctx context.Context, host string, port uint32) error
	// StartHTTPSListener starts an HTTPS listener.
	StartHTTPSListener(ctx context.Context, domain string, port uint32) error
	// StartDNSListener starts a DNS listener.
	StartDNSListener(ctx context.Context, domains []string) error
	// Sessions returns active implant sessions.
	Sessions(ctx context.Context) ([]SliverSession, error)
	// Execute runs a shell command on a session.
	Execute(ctx context.Context, sessionID, command string) (string, error)
}

// SliverSession is a session returned by the Sliver gRPC API.
type SliverSession struct {
	ID       string
	Hostname string
	Username string
	OS       string
	Arch     string
}

// NewOperatorWithClient returns a Sliver Operator with the given client
// injected. Useful for tests and for the live wiring path.
func NewOperatorWithClient(ts c2.Teamserver, client SliverClient) c2.Operator {
	return &operator{ts: ts, client: client}
}

type operator struct {
	ts     c2.Teamserver
	client SliverClient
}

func (o *operator) StartListener(ctx context.Context, spec c2.ListenerSpec) error {
	// TODO(live): delegate to the live gRPC client once it is wired.
	switch strings.ToLower(spec.Protocol) {
	case "mtls":
		return o.client.StartMTLSListener(ctx, spec.Bind, uint32(4444))
	case "https":
		return o.client.StartHTTPSListener(ctx, spec.Name, 443)
	case "dns":
		return o.client.StartDNSListener(ctx, []string{spec.Name})
	default:
		return fmt.Errorf("sliver: unsupported listener protocol %q", spec.Protocol)
	}
}

func (o *operator) Sessions(ctx context.Context) ([]c2.Session, error) {
	raw, err := o.client.Sessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("sliver: sessions: %w", err)
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

// Execute translates a portable domain.Technique into a Sliver operator
// command and returns a sanitized domain.Result. The concrete procedure is
// sourced from the referenced public library (Atomic Red Team / Caldera) via
// the technique's SourceID — no payload bytes are authored here.
func (o *operator) Execute(ctx context.Context, sessionID string, t domain.Technique) (domain.Result, error) {
	start := time.Now()

	cmd, err := techniqueToSliverCommand(t)
	if err != nil {
		// Technique is not supported by Sliver — should not reach Fronted/unsupported.
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

// techniqueToSliverCommand maps a portable Technique to a Sliver shell command.
// This is a REFERENCE translation: the SourceID points to an Atomic Red Team
// test or Caldera ability; the mapping tells Sliver which execute/shell invocation
// corresponds to that ATT&CK procedure. No payload content is authored — only
// the command verb and the parameters supplied by the Technique are used.
//
// In production this map is loaded from the technique catalog YAMLs
// (see Phase 6 / internal/emulation/catalog). The inline table below is the
// MVP seed for the most common ATT&CK tactics.
func techniqueToSliverCommand(t domain.Technique) (string, error) {
	// Mapping from ATT&CK technique ID → Sliver execute invocation.
	// Parameters come from t.Inputs — no hardcoded payload content.
	switch t.AttackID {
	case "T1059.001": // Command and Scripting Interpreter: PowerShell
		script := t.Inputs["command"]
		if script == "" {
			script = "whoami"
		}
		return fmt.Sprintf("execute -o powershell -c \"%s\"", escapeShell(script)), nil

	case "T1059.003": // Command and Scripting Interpreter: Windows Command Shell
		script := t.Inputs["command"]
		if script == "" {
			script = "whoami"
		}
		return fmt.Sprintf("shell %s", script), nil

	case "T1082": // System Information Discovery
		return "sysinfo", nil

	case "T1057": // Process Discovery
		return "ps", nil

	case "T1049": // System Network Connections Discovery
		return "netstat", nil

	case "T1016": // System Network Configuration Discovery
		return "ifconfig", nil

	case "T1083": // File and Directory Discovery
		path := t.Inputs["path"]
		if path == "" {
			path = "."
		}
		return fmt.Sprintf("ls %s", path), nil

	case "T1005": // Data from Local System
		path := t.Inputs["path"]
		if path == "" {
			return "", fmt.Errorf("T1005 requires inputs.path")
		}
		return fmt.Sprintf("download %s", path), nil

	case "T1053.005": // Scheduled Task/Job: Scheduled Task
		taskName := t.Inputs["task_name"]
		if taskName == "" {
			taskName = "RInfraTest"
		}
		return fmt.Sprintf("execute schtasks /create /tn \"%s\" /tr whoami /sc once /st 00:00", taskName), nil

	case "T1547.001": // Boot or Logon Autostart: Registry Run Keys
		key := t.Inputs["registry_key"]
		if key == "" {
			key = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
		}
		value := t.Inputs["registry_value"]
		if value == "" {
			value = "RInfraTest"
		}
		return fmt.Sprintf("registry write --hive HKCU --path %q --v %q --d whoami", key, value), nil

	default:
		return "", fmt.Errorf("sliver: no command mapping for technique %s (source: %s id: %s)",
			t.AttackID, t.Source, t.SourceID)
	}
}

// sanitize removes raw binary/escape sequences from framework output before
// storing in the audit log / Result.Output.
func sanitize(s string) string {
	// Strip ANSI escape codes.
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

// escapeShell does minimal shell-quoting on a parameter to prevent injection.
// For the emulation use-case the value comes from a YAML technique input, not
// from untrusted user input, but we sanitize anyway.
func escapeShell(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

// runnerFromNode builds a production SSH Runner for a node. The real
// implementation uses per-engagement SSH key material stored in the credential
// store. TODO(live): wire real SSH key auth from the credential store.
func runnerFromNode(_ domain.Node) deploy.Runner {
	// TODO(live): return a real golang.org/x/crypto/ssh Runner dialing node.PublicIP
	// with per-engagement key auth loaded from internal/secrets.
	// For now return a noop runner that will be overridden in tests.
	return &noopRunner{}
}

// noopRunner is used for the production code path before live SSH is wired.
// All operations return "not implemented" — the caller is expected to inject a
// real runner through the service layer before deploy is a supported operation.
type noopRunner struct{}

func (n *noopRunner) Run(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("sliver: SSH runner not wired (TODO(live))")
}
func (n *noopRunner) Upload(_ context.Context, _, _ string) error {
	return fmt.Errorf("sliver: SSH runner not wired (TODO(live))")
}

// noopSliverClient is the default gRPC client stub returned before the live
// client is wired. All operations return "not implemented".
type noopSliverClient struct{}

func (noopSliverClient) StartMTLSListener(_ context.Context, _ string, _ uint32) error {
	return fmt.Errorf("sliver: gRPC client not wired (TODO(live))")
}
func (noopSliverClient) StartHTTPSListener(_ context.Context, _ string, _ uint32) error {
	return fmt.Errorf("sliver: gRPC client not wired (TODO(live))")
}
func (noopSliverClient) StartDNSListener(_ context.Context, _ []string) error {
	return fmt.Errorf("sliver: gRPC client not wired (TODO(live))")
}
func (noopSliverClient) Sessions(_ context.Context) ([]SliverSession, error) {
	return nil, fmt.Errorf("sliver: gRPC client not wired (TODO(live))")
}
func (noopSliverClient) Execute(_ context.Context, _, _ string) (string, error) {
	return "", fmt.Errorf("sliver: gRPC client not wired (TODO(live))")
}
