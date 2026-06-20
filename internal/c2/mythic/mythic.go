// Package mythic adapts the Mythic C2 framework to RInfra. Orchestrated-tier:
// Mythic exposes a REST/GraphQL scripting API and modular C2 profiles, so it
// supports automated emulation via the Operator.
//
// # Posture
//
// This adapter DEPLOYS and DRIVES the upstream Mythic release. It does not
// implement Mythic, agents, payloads, or evasion. The deploy path installs
// Mythic via docker-compose (the official install method) by fetching the
// official install script from the Mythic GitHub repo at a pinned version.
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
)

func init() { c2.Register(&provider{}) }

const (
	mythicVersion    = "v3.3.1"
	mythicInstallURL = "https://github.com/its-a-feature/Mythic/archive/refs/tags/" + mythicVersion + ".tar.gz"
	mythicSHA256     = "placeholder-operator-should-verify-from-mythic-release-page"
	mythicPort       = 7443 // default Mythic HTTPS port
	mythicRPCPort    = 17443
)

type provider struct{}

func (p *provider) Name() string         { return "mythic" }
func (p *provider) Tier() c2.SupportTier { return c2.TierOrchestrated }

// Deploy installs Mythic via docker-compose on the node. Mythic's official
// install method is docker-compose; the install script fetches Mythic from the
// official GitHub release.
func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	runner := runnerFromNode(node)
	return deployMythic(ctx, runner, node, cfg)
}

func deployMythic(ctx context.Context, runner deploy.Runner, node domain.Node, _ c2.Config) (c2.Teamserver, error) {
	// Step 1: ensure docker and docker-compose are available.
	// Step 2: download the official Mythic release archive.
	// Step 3: extract and run ./install_docker_<os>.sh (official install).
	// Step 4: start Mythic via docker-compose.
	// None of these steps author payload content — we're invoking upstream tooling.
	extraSetup := []string{
		"# Mythic uses docker-compose as its official install method.",
		"apt-get install -y docker.io docker-compose-plugin curl || true",
		fmt.Sprintf("mkdir -p /opt/mythic && cd /opt/mythic && curl -fsSL %s | tar xz --strip-components=1", mythicInstallURL),
		"cd /opt/mythic && python3 mythic-cli install",
		"cd /opt/mythic && python3 mythic-cli config set MYTHIC_SERVER_PORT " + fmt.Sprintf("%d", mythicPort),
		"cd /opt/mythic && python3 mythic-cli start",
		"echo '[rinfra-mythic] Mythic started via docker-compose'",
	}

	// For the Mythic install we use a docker-compose-based approach; the
	// systemd unit wraps docker-compose start/stop.
	params := deploy.InstallParams{
		// Mythic uses docker-compose, not a single binary. We still need a
		// placeholder binary path and unit for the systemd wrapper.
		ReleaseURL:  mythicInstallURL,
		SHA256:      mythicSHA256,
		DestPath:    "/opt/mythic/mythic-cli",
		SystemdUnit: "mythic",
		ServiceUser: "root",
		ExecStart:   "/usr/bin/docker compose -f /opt/mythic/docker-compose.yml start",
		ExtraSetup:  extraSetup,
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
			Status:            domain.ExecSkipped,
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

// techniqueToMythicTasking maps a portable Technique to a Mythic tasking
// command + params. No payload content is authored; the SourceID references
// the procedure in a public library (Atomic Red Team / Caldera).
func techniqueToMythicTasking(t domain.Technique) (cmd string, params map[string]string, err error) {
	p := make(map[string]string)

	switch t.AttackID {
	case "T1059.001":
		script := t.Inputs["command"]
		if script == "" {
			script = "whoami"
		}
		return "powershell", map[string]string{"command": script}, nil

	case "T1059.003":
		command := t.Inputs["command"]
		if command == "" {
			command = "whoami"
		}
		return "shell", map[string]string{"command": command}, nil

	case "T1082":
		return "sysinfo", p, nil

	case "T1057":
		return "ps", p, nil

	case "T1049":
		return "netstat", p, nil

	case "T1016":
		return "ipconfig", p, nil

	case "T1083":
		path := t.Inputs["path"]
		if path == "" {
			path = "."
		}
		return "ls", map[string]string{"path": path}, nil

	case "T1005":
		path := t.Inputs["path"]
		if path == "" {
			return "", nil, fmt.Errorf("T1005 requires inputs.path")
		}
		return "download", map[string]string{"file": path}, nil

	case "T1053.005":
		taskName := t.Inputs["task_name"]
		if taskName == "" {
			taskName = "RInfraTest"
		}
		return "schtasks", map[string]string{"task_name": taskName, "action": "create"}, nil

	default:
		return "", nil, fmt.Errorf("mythic: no tasking mapping for technique %s (source: %s id: %s)",
			t.AttackID, t.Source, t.SourceID)
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

// sanitize removes ANSI escape codes and trims whitespace from tool output.
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
