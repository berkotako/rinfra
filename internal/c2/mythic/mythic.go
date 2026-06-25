// Package mythic adapts the Mythic C2 framework to RInfra. Orchestrated-tier:
// Mythic exposes a REST/GraphQL scripting API and modular C2 profiles, so it
// supports automated emulation via the Operator.
//
// # Posture
//
// This adapter DEPLOYS and DRIVES upstream Mythic. It does not implement Mythic,
// agents, payloads, or evasion. Mythic ships no release binary/checksum, so the
// deploy path installs it from source the official way — git clone at an
// immutable pinned commit, `make` to build mythic-cli, then `mythic-cli start`
// (Docker Compose).
//
// # HTTP client
//
// MythicClient is the thin HTTP interface over Mythic's REST/GraphQL API. It is
// wired live by httpMythicClient (this file), which authenticates to Mythic's
// /auth endpoint for a bearer JWT and then drives the Hasura /graphql/ endpoint
// using the shared liveClient implementation in mythic_live.go. Control()
// constructs it against the deployed teamserver, with operator credentials read
// from the per-engagement environment; auth happens lazily on first use so the
// (context-free) Control hook stays cheap. Tests inject FakeMythicClient or
// stand up an httptest server (client_test.go, mythic_live_test.go).
package mythic

import (
	"context"
	"encoding/json"
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
	// Mythic has no release binaries or SHA-256 checksums — it is installed from
	// source: git clone at an IMMUTABLE pinned commit, then `make` builds the
	// mythic-cli Go binary, then `mythic-cli start` brings up the Docker Compose
	// stack. mythicRef is the commit for the tag below; to upgrade, review a newer
	// tag's commit and update both.
	mythicRepo    = "https://github.com/its-a-feature/Mythic"
	mythicRef     = "b294c8ff5354ed57a6697da61d0524286e663c95" // tag v3.4.0.5
	mythicPort    = 7443                                       // NGINX_PORT: UI/API the operator connects to
	mythicRPCPort = 17443                                      // MYTHIC_SERVER_PORT: backend (left at default)
)

type provider struct{}

func (p *provider) Name() string         { return "mythic" }
func (p *provider) Tier() c2.SupportTier { return c2.TierOrchestrated }

// Capabilities reports Mythic's routing metadata: cross-platform agents (Apollo,
// Poseidon, etc.) over HTTP/HTTPS profiles.
func (p *provider) Capabilities() c2.Capabilities {
	return c2.Capabilities{
		Platforms:         []string{"windows", "linux", "macos"},
		ListenerProtocols: []string{"http", "https"},
	}
}

// Deploy installs Mythic on the node. Mythic's only supported install method is
// from source (git clone + `make` + `mythic-cli start`, Docker Compose based) —
// it ships no release binary or checksum — so Deploy pins an immutable commit and
// builds it in-place. It authors no payload content; it drives upstream tooling.
func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	runner := runnerFromNode(node)
	return deployMythic(ctx, runner, node, cfg)
}

func deployMythic(ctx context.Context, runner deploy.Runner, node domain.Node, _ c2.Config) (c2.Teamserver, error) {
	// Mythic is git-clone + build + docker-compose; there is no release tarball or
	// SHA-256, so ReleaseURL is empty and the whole install happens in ExtraSetup:
	// fetch the exact pinned commit, `make` the mythic-cli Go binary, then start
	// the Docker Compose stack via mythic-cli.
	extraSetup := []string{
		"export DEBIAN_FRONTEND=noninteractive",
		"apt-get update -y || true",
		"apt-get install -y git make docker.io docker-compose-plugin curl || true",
		"systemctl enable --now docker || true",
		// Immutable checkout of the pinned commit (depth 1).
		"rm -rf /opt/mythic && git init -q /opt/mythic",
		fmt.Sprintf("cd /opt/mythic && git remote add origin %s", mythicRepo),
		fmt.Sprintf("cd /opt/mythic && git fetch --depth 1 origin %s", mythicRef),
		"cd /opt/mythic && git checkout -q FETCH_HEAD",
		"cd /opt/mythic && make", // builds the mythic-cli Go binary
		// NGINX_PORT is the UI/API reverse-proxy port operators connect to
		// (default 7443 = mythicPort). The backend MYTHIC_SERVER_PORT (default
		// 17443 = mythicRPCPort) is left untouched — setting it to 7443 would
		// collide with Nginx and break `mythic-cli start`.
		fmt.Sprintf("cd /opt/mythic && ./mythic-cli config set NGINX_PORT %d", mythicPort),
		"cd /opt/mythic && ./mythic-cli start",
		"echo '[rinfra-mythic] Mythic started via mythic-cli (Docker Compose)'",
	}

	params := deploy.InstallParams{
		// No ReleaseURL/SHA256: Mythic has no release binary; built in ExtraSetup.
		DestPath:    "/opt/mythic/mythic-cli",
		SystemdUnit: "mythic",
		ServiceUser: "root",
		// mythic-cli start (idempotent) brings the Compose stack up on boot.
		ExecStart:  "/bin/bash -c 'cd /opt/mythic && ./mythic-cli start'",
		ExtraSetup: extraSetup,
	}

	if err := deploy.RunInstall(ctx, runner, params); err != nil {
		return c2.Teamserver{}, fmt.Errorf("mythic.Deploy: %w", err)
	}

	return c2.Teamserver{
		Host:           node.PublicIP,
		Port:           mythicPort,
		Status:         "running",
		ConnectionInfo: fmt.Sprintf("Mythic UI: https://%s:%d (operator credentials set during install)", node.PublicIP, mythicPort),
	}, nil
}

// RedirectorConfig emits an nginx reverse-proxy config fronting the Mythic
// HTTPS listener. Uses reverse-proxy + categorized-domain (NOT CDN fronting).
func (p *provider) RedirectorConfig(prof domain.Profile) (string, error) {
	return mythicRedirectorConfig(prof)
}

func mythicRedirectorConfig(prof domain.Profile) (string, error) {
	params := deploy.NginxParams{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: mythicPort,
		ServerName:   prof.RewriteHost,
		RewriteHost:  prof.RewriteHost,
		UseHTTPS:     true,
		SSLCert:      "/etc/ssl/rinfra/server.crt",
		SSLKey:       "/etc/ssl/rinfra/server.key",
		PathRules:    prof.PathRules,
		ExtraServerBlock: "# Mythic C2 reverse proxy\n" +
			"    # Set proxy_pass upstream to actual Mythic server IP.",
	}
	cfg, err := deploy.BuildNginxConfig(params)
	if err != nil {
		return "", fmt.Errorf("mythic.RedirectorConfig: %w", err)
	}
	return cfg, nil
}

// Control returns an Orchestrated-tier Operator backed by the live Mythic
// REST/GraphQL client. The client targets the deployed teamserver (Mythic's
// HTTPS UI/API, default port 7443) and authenticates lazily on first use with
// operator credentials read from the per-engagement environment (see
// newHTTPClientForTeamserver). The service layer may instead call LiveOperator
// (mythic_live.go) when it holds explicit credentials; both paths produce the
// same live Operator.
func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	return &operator{ts: ts, client: newHTTPClientForTeamserver(ts)}, true
}

// MythicClient is the minimal interface over Mythic's REST/GraphQL API.
// The live implementation makes HTTP calls to the deployed Mythic instance;
// tests inject a fake.
type MythicClient interface {
	// CreateCallback creates an agent callback in Mythic (analogous to registering
	// a session). Returns a callback ID.
	CreateCallback(ctx context.Context, host, user, os string) (string, error)
	// Callbacks returns active callbacks (sessions).
	Callbacks(ctx context.Context) ([]MythicCallback, error)
	// IssueTasking issues a tasking command to a callback.
	IssueTasking(ctx context.Context, callbackID, command string, params map[string]string) (string, error)
	// TaskOutput fetches the output of a completed task.
	TaskOutput(ctx context.Context, taskID string) (string, error)
	// CreateListener creates a C2 profile listener.
	CreateListener(ctx context.Context, profileName, bindAddress string, port int) error
}

// MythicCallback is an active agent session reported by Mythic.
type MythicCallback struct {
	ID        string
	Host      string
	User      string
	OS        string
	Arch      string
	C2Profile string
}

// NewOperatorWithClient returns a Mythic Operator with the given client injected.
func NewOperatorWithClient(ts c2.Teamserver, client MythicClient) c2.Operator {
	return &operator{ts: ts, client: client}
}

type operator struct {
	ts     c2.Teamserver
	client MythicClient
}

func (o *operator) StartListener(ctx context.Context, spec c2.ListenerSpec) error {
	// Drive the Mythic API: ensure the C2 profile for this listener protocol is
	// present and running on the teamserver (Mythic profiles run as containers
	// started at deploy time; CreateListener verifies/activates them).
	profile := mythicC2ProfileForProtocol(spec.Protocol)
	port := 443
	if spec.Protocol == "dns" {
		port = 53
	}
	return o.client.CreateListener(ctx, profile, spec.Bind, port)
}

func (o *operator) Sessions(ctx context.Context) ([]c2.Session, error) {
	cbs, err := o.client.Callbacks(ctx)
	if err != nil {
		return nil, fmt.Errorf("mythic: callbacks: %w", err)
	}
	out := make([]c2.Session, 0, len(cbs))
	for _, cb := range cbs {
		out = append(out, c2.Session{
			ID:   cb.ID,
			Host: cb.Host,
			User: cb.User,
			Metadata: map[string]string{
				"os":         cb.OS,
				"arch":       cb.Arch,
				"c2_profile": cb.C2Profile,
			},
		})
	}
	return out, nil
}

// Execute translates a portable domain.Technique into a Mythic tasking command
// and returns a sanitized result. The concrete procedure is referenced by the
// SourceID; no payload bytes are authored here.
func (o *operator) Execute(ctx context.Context, callbackID string, t domain.Technique) (domain.Result, error) {
	start := time.Now()

	cmd, params, err := techniqueToMythicTasking(t)
	if err != nil {
		return domain.Result{
			TechniqueAttackID: t.AttackID,
			Status:            domain.ExecUnsupported,
			Output:            err.Error(),
			StartedAt:         start,
			FinishedAt:        time.Now(),
		}, nil
	}

	taskID, err := o.client.IssueTasking(ctx, callbackID, cmd, params)
	if err != nil {
		fin := time.Now()
		return domain.Result{
			TechniqueAttackID: t.AttackID,
			Status:            domain.ExecFailed,
			StartedAt:         start,
			FinishedAt:        fin,
			Err:               err.Error(),
		}, nil
	}

	output, err := o.client.TaskOutput(ctx, taskID)
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

// techniqueToMythicTasking compiles a portable Technique to a Mythic tasking
// command + params: ttp.Compile resolves the technique to a portable primitive
// (the shared catalog), and renderMythicPrimitive renders it to Mythic's native
// tasking. No payload content is authored; the SourceID references the procedure
// in a public library (Atomic Red Team / Caldera).
func techniqueToMythicTasking(t domain.Technique) (cmd string, params map[string]string, err error) {
	prim, ok, err := ttp.Compile(t)
	if err != nil {
		return "", nil, err
	}
	if !ok {
		return "", nil, fmt.Errorf("mythic: no catalog mapping for technique %s (source: %s id: %s)",
			t.AttackID, t.Source, t.SourceID)
	}
	return renderMythicPrimitive(prim)
}

// Revert undoes a persistence technique Mythic created (deletes the scheduled
// task via a schtasks tasking). Implements c2.Reverter.
func (o *operator) Revert(ctx context.Context, callbackID string, t domain.Technique) (domain.Result, error) {
	start := time.Now()
	cmd, params, ok := mythicCleanupTasking(t)
	if !ok {
		return domain.Result{
			TechniqueAttackID: t.AttackID,
			Status:            domain.ExecUnsupported,
			Output:            "mythic: no cleanup defined for this technique",
			StartedAt:         start,
			FinishedAt:        time.Now(),
		}, nil
	}
	taskID, err := o.client.IssueTasking(ctx, callbackID, cmd, params)
	if err != nil {
		return domain.Result{
			TechniqueAttackID: t.AttackID,
			Status:            domain.ExecFailed,
			StartedAt:         start,
			FinishedAt:        time.Now(),
			Err:               err.Error(),
		}, nil
	}
	output, err := o.client.TaskOutput(ctx, taskID)
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

// mythicCleanupTasking renders the reverse of a persistence primitive Mythic
// supports. ok=false when the technique has no catalog mapping or no cleanup.
func mythicCleanupTasking(t domain.Technique) (string, map[string]string, bool) {
	prim, ok, err := ttp.Compile(t)
	if err != nil || !ok {
		return "", nil, false
	}
	if prim.Kind == c2.PrimScheduledTask {
		return "schtasks", map[string]string{"task_name": prim.Arg("task_name"), "action": "delete"}, true
	}
	return "", nil, false
}

// renderMythicPrimitive renders a portable primitive into a Mythic tasking
// command and parameter map. Primitives Mythic does not implement (e.g. a
// registry Run-key write) return an error so the caller records the technique
// unsupported.
func renderMythicPrimitive(p c2.Primitive) (string, map[string]string, error) {
	switch p.Kind {
	case c2.PrimPowerShell:
		return "powershell", map[string]string{"command": p.Arg("script")}, nil
	case c2.PrimShell:
		return "shell", map[string]string{"command": p.Arg("command")}, nil
	case c2.PrimSysInfo:
		return "sysinfo", map[string]string{}, nil
	case c2.PrimProcessList:
		return "ps", map[string]string{}, nil
	case c2.PrimNetConnections:
		return "netstat", map[string]string{}, nil
	case c2.PrimNetConfig:
		return "ipconfig", map[string]string{}, nil
	case c2.PrimFileList:
		path := p.Arg("path")
		if path == "" {
			path = "."
		}
		return "ls", map[string]string{"path": path}, nil
	case c2.PrimDownload:
		return "download", map[string]string{"file": p.Arg("path")}, nil
	case c2.PrimScheduledTask:
		return "schtasks", map[string]string{"task_name": p.Arg("task_name"), "action": "create"}, nil
	default:
		if cmd, ok := c2.DiscoveryCommand(p.Kind); ok {
			return "shell", map[string]string{"command": cmd}, nil
		}
		return "", nil, fmt.Errorf("mythic: unsupported primitive %q", p.Kind)
	}
}

// mythicC2ProfileForProtocol maps a listener protocol to a Mythic C2 profile name.
func mythicC2ProfileForProtocol(protocol string) string {
	switch strings.ToLower(protocol) {
	case "https":
		return "http"
	case "dns":
		return "dns"
	case "smb":
		return "smb"
	default:
		return "http"
	}
}

// paramsToJSON is a helper for building Mythic task parameter JSON.
func paramsToJSON(p map[string]string) string {
	b, _ := json.Marshal(p)
	return string(b)
}

// runnerFromNode builds the production SSH Runner for a node. SSH key material
// is loaded from the per-engagement environment by deploy.NewNodeRunner.
func runnerFromNode(node domain.Node) deploy.Runner {
	return deploy.NewNodeRunner(node.PublicIP)
}
