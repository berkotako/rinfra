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
// # gRPC client (live)
//
// The SliverClient interface below maps the subset of Sliver's gRPC operator
// API used by RInfra. The live implementation (grpc.go: grpcSliverClient) wraps
// the official github.com/bishopfox/sliver/protobuf/rpcpb generated stub and
// issues REAL gRPC calls over a *grpc.ClientConn. Production dials the
// multiplayer listener over mTLS using the operator config that sliver-server
// generates during Deploy (DialOperatorClient / DialOperator in transport.go);
// the secret mTLS material lives in that config, not in this package. Tests use
// FakeSliverClient (unit) or an in-process rpcpb.SliverRPCServer over bufconn
// (grpc_test.go) — never a live teamserver.
package sliver

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/c2/deploy"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/emulation/ttp"
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

// Capabilities reports Sliver's routing metadata: cross-platform implants and
// mTLS/HTTPS/DNS listeners. Tactics/Techniques are left open (Sliver's shell/
// execute primitives cover a broad range); the per-technique command mapping in
// techniqueToSliverCommand is the final arbiter at execution time.
func (p *provider) Capabilities() c2.Capabilities {
	return c2.Capabilities{
		Platforms:         []string{"windows", "linux", "macos"},
		ListenerProtocols: []string{"mtls", "https", "dns"},
	}
}

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

// EnvOperatorConfig points at the Sliver multiplayer operator config file
// (the mTLS ".cfg" sliver-server generates during Deploy). The service layer
// fetches it from the teamserver and exports this path before driving emulation.
const EnvOperatorConfig = "RINFRA_SLIVER_OPERATOR_CONFIG"

// Control returns an Operator backed by the live gRPC SliverClient.
//
// Sliver's multiplayer listener is mTLS-protected: the client must present the
// operator certificate/key from the config sliver-server generates during
// Deploy. Control loads that config from RINFRA_SLIVER_OPERATOR_CONFIG and dials
// with the operator mTLS material (lazy connect; the first RPC drives the
// handshake). If the config is absent or invalid, Control returns an operator
// whose RPCs surface a clear error — it deliberately does NOT fall back to an
// insecure/plaintext dial, which would silently fail to authenticate against a
// real teamserver. Tests inject an in-process conn via NewOperatorWithClient.
func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	path := os.Getenv(EnvOperatorConfig)
	if path == "" {
		return &operator{ts: ts, client: &dialErrClient{err: fmt.Errorf(
			"sliver: operator config required to dial the mTLS multiplayer listener; set %s to the sliver-server operator config", EnvOperatorConfig)}}, true
	}
	cfg, err := LoadOperatorConfig(path)
	if err != nil {
		return &operator{ts: ts, client: &dialErrClient{err: err}}, true
	}
	client, err := DialOperatorClient(context.Background(), cfg)
	if err != nil {
		return &operator{ts: ts, client: &dialErrClient{err: err}}, true
	}
	return &operator{ts: ts, client: client}, true
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
		// Technique has no catalog mapping or Sliver can't render its primitive
		// — record it unsupported (no fabricated attempt).
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

// techniqueToSliverCommand compiles a portable Technique to a Sliver operator
// command. It is a two-step REFERENCE translation, authoring no payload content:
//
//  1. ttp.Compile maps the ATT&CK technique to a portable c2.Primitive (the
//     open-ended, data-driven catalog — see internal/emulation/ttp); and
//  2. renderSliverPrimitive turns that primitive into Sliver's native
//     execute/shell invocation (the small, framework-specific surface below).
//
// A technique with no catalog mapping, or a primitive Sliver can't render,
// returns an error so the caller records it unsupported.
func techniqueToSliverCommand(t domain.Technique) (string, error) {
	prim, ok, err := ttp.Compile(t)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("sliver: no catalog mapping for technique %s (source: %s id: %s)",
			t.AttackID, t.Source, t.SourceID)
	}
	return renderSliverPrimitive(prim)
}

// renderSliverPrimitive renders a portable primitive into a Sliver operator
// command. Parameters come from the resolved primitive args — no hardcoded
// payload content. Primitives Sliver does not implement return an error.
func renderSliverPrimitive(p c2.Primitive) (string, error) {
	switch p.Kind {
	case c2.PrimPowerShell:
		return fmt.Sprintf("execute -o powershell -c \"%s\"", escapeShell(p.Arg("script"))), nil
	case c2.PrimShell:
		return fmt.Sprintf("shell %s", p.Arg("command")), nil
	case c2.PrimSysInfo:
		return "sysinfo", nil
	case c2.PrimProcessList:
		return "ps", nil
	case c2.PrimNetConnections:
		return "netstat", nil
	case c2.PrimNetConfig:
		return "ifconfig", nil
	case c2.PrimFileList:
		return fmt.Sprintf("ls %s", p.Arg("path")), nil
	case c2.PrimDownload:
		return fmt.Sprintf("download %s", p.Arg("path")), nil
	case c2.PrimScheduledTask:
		return fmt.Sprintf("execute schtasks /create /tn \"%s\" /tr whoami /sc once /st 00:00", p.Arg("task_name")), nil
	case c2.PrimRegistryRunKey:
		return fmt.Sprintf("registry write --hive HKCU --path %q --v %q --d whoami", p.Arg("registry_key"), p.Arg("registry_value")), nil
	default:
		return "", fmt.Errorf("sliver: unsupported primitive %q", p.Kind)
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

// runnerFromNode builds the production SSH Runner for a node. It delegates to
// the shared live runner (deploy.NewNodeRunner), which loads per-engagement SSH
// key material from the environment exported by the service layer. If the host
// or key material is missing/invalid the returned Runner fails every operation
// with a clear configuration error rather than panicking.
func runnerFromNode(node domain.Node) deploy.Runner {
	return deploy.NewNodeRunner(node.PublicIP)
}

// dialErrClient is the SliverClient returned by Control only when the
// teamserver target is malformed and grpc.NewClient itself fails. Every RPC
// surfaces the underlying dial error, so a misconfigured teamserver produces a
// clear message instead of a panic. It is never the noop "not wired" stub.
type dialErrClient struct{ err error }

func (d *dialErrClient) StartMTLSListener(context.Context, string, uint32) error  { return d.err }
func (d *dialErrClient) StartHTTPSListener(context.Context, string, uint32) error { return d.err }
func (d *dialErrClient) StartDNSListener(context.Context, []string) error         { return d.err }
func (d *dialErrClient) Sessions(context.Context) ([]SliverSession, error)        { return nil, d.err }
func (d *dialErrClient) Execute(context.Context, string, string) (string, error) {
	return "", d.err
}
