// Package poshc2 adapts the PoshC2 framework (Nettitude) to RInfra. Scripted-tier:
// RInfra deploys PoshC2 + a redirector and drives a PARTIAL operator over PoshC2
// v9.0's REST API; techniques outside the renderable subset return
// ExecUnsupported (no fabricated attempt).
//
// # Operator API (PoshC2 v9.0 "Revival")
//
// PoshC2 v8.x had NO machine-readable operator API (only an interactive console),
// so RInfra targets v9.0, which ships posh-api-server — a basic REST API on
// :5000. The live client (httpPoshC2Client) drives the v9.0 endpoints (verified
// against start_api.py at the pinned commit):
//   - GET  /liveimplants        — list active implants
//   - POST /newtasks            — queue a command (model: implant_id, command, user)
//   - GET  /tasks/implant/{id}  — read task output
//
// posh-api-server protects these with HTTP Basic auth (default poshc2 /
// change_on_install; override via RINFRA_POSHC2_API_USER/PASSWORD). There is no
// listener-create endpoint — listeners are configured at deploy time. JSON
// shapes are parsed leniently; field names want validation against a live v9.0
// server (see docs/RUNBOOK). RInfra composes the upstream tool: it authors no
// implants, payloads, or evasion.
package poshc2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/c2/deploy"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/emulation/ttp"
)

func init() { c2.Register(&provider{}) }

const (
	// PoshC2 installs from source at an IMMUTABLE pinned commit (the v9.0 tag) —
	// v9.0 is the first release with the posh-api-server REST API. v8.x had no
	// API and the previously-pinned "v8.0.3" tag never existed.
	poshRepo    = "https://github.com/nettitude/PoshC2"
	poshRef     = "64eb5570db2ea0a83cde855001caac9d8d33da29" // tag v9.0
	poshPort    = 443                                        // implant HTTPS listener (redirector upstream)
	poshAPIPort = 5000                                       // posh-api-server REST API
)

type provider struct{}

func (p *provider) Name() string         { return "poshc2" }
func (p *provider) Tier() c2.SupportTier { return c2.TierScripted }

// Capabilities reports PoshC2's routing metadata: Windows/Linux implants over
// HTTPS, with an explicit technique allowlist matching the automated subset.
func (p *provider) Capabilities() c2.Capabilities {
	return c2.Capabilities{
		Platforms:         []string{"windows", "linux"},
		Techniques:        supportedAttackIDs(),
		ListenerProtocols: []string{"https"},
	}
}

// supportedAttackIDs returns the catalog techniques PoshC2 can run automatically:
// those that compile to a primitive renderPoshPrimitive renders. This derives
// the Scripted-tier subset from the catalog instead of a hand-kept list.
func supportedAttackIDs() []string {
	var ids []string
	for _, id := range ttp.Default().AttackIDs() {
		prim, ok, err := ttp.Compile(domain.Technique{AttackID: id})
		if !ok || err != nil {
			continue
		}
		if _, rok := renderPoshPrimitive(prim); rok {
			ids = append(ids, id)
		}
	}
	return ids
}

// Deploy installs PoshC2 v9.0 on the node via SSH (source build at the pinned
// commit), then runs the C2 server and the posh-api-server REST API.
func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	return deployPoshC2(ctx, runnerFromNode(node), node, cfg)
}

func deployPoshC2(ctx context.Context, runner deploy.Runner, node domain.Node, _ c2.Config) (c2.Teamserver, error) {
	// PoshC2 has no release binary; install from source at the pinned commit
	// (ReleaseURL empty ⇒ the template skips its download/checksum block). Run the
	// C2 server plus the v9.0 REST API (posh-api-server) under one unit.
	extraSetup := []string{
		"export DEBIAN_FRONTEND=noninteractive",
		"apt-get update -y || true",
		"apt-get install -y git python3 python3-pip || true",
		"rm -rf /opt/poshc2 && git init -q /opt/poshc2",
		fmt.Sprintf("cd /opt/poshc2 && git remote add origin %s", poshRepo),
		fmt.Sprintf("cd /opt/poshc2 && git fetch --depth 1 origin %s", poshRef),
		"cd /opt/poshc2 && git checkout -q FETCH_HEAD",
		// Install.sh is the upstream installer: it sets up the pipenv deps and
		// symlinks posh-server / posh-api-server / posh-project into /usr/local/bin.
		// pip3 alone does NOT create those entrypoints.
		"cd /opt/poshc2 && ./Install.sh",
		"posh-project -n rinfra || true", // create the engagement project
		"echo '[rinfra-poshc2] PoshC2 v9.0 installed via Install.sh; REST API on :5000'",
	}

	params := deploy.InstallParams{
		// No ReleaseURL/SHA256: built from source in ExtraSetup.
		DestPath:    "/opt/poshc2/posh-server",
		SystemdUnit: "poshc2-server",
		ServiceUser: "root",
		// Start the C2 server (background) and the REST API (foreground keeps the
		// unit alive). Operators refine listeners/project config via posh-project.
		ExecStart:  "/bin/bash -lc '(posh-server &) && exec posh-api-server'",
		ExtraSetup: extraSetup,
	}

	if err := deploy.RunInstall(ctx, runner, params); err != nil {
		return c2.Teamserver{}, fmt.Errorf("poshc2.Deploy: %w", err)
	}

	return c2.Teamserver{
		Host:           node.PublicIP,
		Port:           poshPort,
		Status:         "running",
		ConnectionInfo: fmt.Sprintf("PoshC2 v9.0 @ %s:%d (REST API on :%d)", node.PublicIP, poshPort, poshAPIPort),
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

// Control returns a scripted partial Operator backed by the v9.0 REST API. The
// API base/token are read from the per-engagement environment
// (RINFRA_POSHC2_API_URL / RINFRA_POSHC2_API_TOKEN); the default base is the
// teamserver host on the REST port.
func (p *provider) Control(ts c2.Teamserver) (c2.Operator, bool) {
	return &operator{ts: ts, client: newHTTPClient(ts)}, true
}

// PoshC2Client wraps PoshC2 v9.0's REST API for limited automation.
type PoshC2Client interface {
	// Execute queues a command for an implant and returns its task output.
	Execute(ctx context.Context, implantID, command string) (string, error)
	// Implants lists active implants.
	Implants(ctx context.Context) ([]PoshC2Implant, error)
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

// StartListener is a no-op for PoshC2: listeners are configured at deploy time
// (posh-project), and the v9.0 REST API exposes no listener-create endpoint. It
// returns nil so an emulation run isn't aborted; the deploy-time listener is what
// implants call back to.
func (o *operator) StartListener(_ context.Context, _ c2.ListenerSpec) error {
	return nil
}

func (o *operator) Sessions(ctx context.Context) ([]c2.Session, error) {
	implants, err := o.client.Implants(ctx)
	if err != nil {
		return nil, fmt.Errorf("poshc2: implants: %w", err)
	}
	out := make([]c2.Session, 0, len(implants))
	for _, im := range implants {
		out = append(out, c2.Session{ID: im.ID, Host: im.Hostname, User: im.Username})
	}
	return out, nil
}

func (o *operator) Execute(ctx context.Context, implantID string, t domain.Technique) (domain.Result, error) {
	start := time.Now()

	cmd, ok := techniqueToCommand(t)
	if !ok {
		return domain.Result{
			TechniqueAttackID: t.AttackID,
			Status:            domain.ExecUnsupported,
			Output: fmt.Sprintf("poshc2 (scripted-tier): technique %s is outside the renderable subset; "+
				"operator should execute manually via the PoshC2 client", t.AttackID),
			StartedAt:  start,
			FinishedAt: time.Now(),
		}, nil
	}

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

// techniqueToCommand compiles a portable Technique to a PoshC2 PowerShell command
// via the shared catalog (ttp.Compile) + renderPoshPrimitive. ok=false derives
// the Scripted-tier support set from the catalog rather than a hand-kept list.
func techniqueToCommand(t domain.Technique) (string, bool) {
	prim, ok, err := ttp.Compile(t)
	if !ok || err != nil {
		return "", false
	}
	return renderPoshPrimitive(prim)
}

// renderPoshPrimitive renders a portable primitive into a PoshC2 implant command
// (PowerShell). ok=false for primitives outside PoshC2's reliable subset.
func renderPoshPrimitive(p c2.Primitive) (string, bool) {
	switch p.Kind {
	case c2.PrimPowerShell:
		return p.Arg("script"), true
	case c2.PrimSysInfo:
		return "$env:COMPUTERNAME; [System.Environment]::OSVersion", true
	case c2.PrimProcessList:
		return "Get-Process | Select-Object Id, ProcessName", true
	case c2.PrimFileList:
		path := p.Arg("path")
		if path == "" {
			path = "."
		}
		return fmt.Sprintf("Get-ChildItem '%s'", path), true
	default:
		// net.exe discovery built-ins run verbatim from the PowerShell implant.
		if cmd, ok := c2.DiscoveryCommand(p.Kind); ok {
			return cmd, true
		}
		return "", false
	}
}

func runnerFromNode(node domain.Node) deploy.Runner {
	return deploy.NewNodeRunner(node.PublicIP)
}

// httpPoshC2Client is the live PoshC2Client. It drives PoshC2 v9.0's REST API
// (posh-api-server) over HTTP. RInfra composes the upstream tool — it authors no
// implants, payloads, or evasion.
type httpPoshC2Client struct {
	base    string // e.g. http://host:5000
	user    string // HTTP Basic auth user (posh-api-server uses HTTPBasicAuth)
	pass    string // HTTP Basic auth password
	hc      *http.Client
	poll    time.Duration // delay between task-output polls
	timeout time.Duration // overall budget to wait for task output
}

// posh-api-server defaults (start_api.py): HTTPBasicAuth with this user/pass.
const (
	defaultAPIUser = "poshc2"
	defaultAPIPass = "change_on_install"
)

// newHTTPClient builds the live REST client from the teamserver, allowing the
// API URL/credentials to be overridden per engagement via the environment.
func newHTTPClient(ts c2.Teamserver) *httpPoshC2Client {
	base := os.Getenv("RINFRA_POSHC2_API_URL")
	if base == "" {
		base = fmt.Sprintf("http://%s:%d", ts.Host, poshAPIPort)
	}
	return &httpPoshC2Client{
		base:    strings.TrimRight(base, "/"),
		user:    envOr("RINFRA_POSHC2_API_USER", defaultAPIUser),
		pass:    envOr("RINFRA_POSHC2_API_PASSWORD", defaultAPIPass),
		hc:      &http.Client{Timeout: 30 * time.Second},
		poll:    500 * time.Millisecond,
		timeout: 30 * time.Second,
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func (c *httpPoshC2Client) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.user != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("poshc2 api %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

// Implants lists active implants via GET /liveimplants. Fields are parsed
// leniently to tolerate the v9.0 API's loosely-documented shape.
func (c *httpPoshC2Client) Implants(ctx context.Context) ([]PoshC2Implant, error) {
	data, err := c.do(ctx, http.MethodGet, "/liveimplants", nil)
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("poshc2: decode liveimplants: %w", err)
	}
	out := make([]PoshC2Implant, 0, len(rows))
	for _, r := range rows {
		out = append(out, PoshC2Implant{
			ID:       firstString(r, "ImplantID", "RandomURI", "id"),
			Hostname: firstString(r, "Hostname", "hostname"),
			Username: firstString(r, "User", "Domain\\User", "user"),
		})
	}
	return out, nil
}

// Execute queues a command for an implant (POST /newtasks — the create endpoint;
// /newtasksview is GET-only) then polls GET /tasks/implant/{id} for the output.
// The v9.0 NewTask model requires implant_id, command, and user.
func (c *httpPoshC2Client) Execute(ctx context.Context, implantID, command string) (string, error) {
	if _, err := c.do(ctx, http.MethodPost, "/newtasks", map[string]string{
		"implant_id": implantID,
		"command":    command,
		"user":       c.user,
	}); err != nil {
		return "", fmt.Errorf("poshc2: queue task on implant %s: %w", implantID, err)
	}

	// Poll for the task output: the implant runs the command on its next check-in.
	deadline := time.Now().Add(c.timeout)
	for time.Now().Before(deadline) {
		data, err := c.do(ctx, http.MethodGet, "/tasks/implant/"+implantID, nil)
		if err != nil {
			return "", err
		}
		if out := latestTaskOutput(data); out != "" {
			return out, nil
		}
		select {
		case <-time.After(c.poll):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "", nil // no output before timeout — implant may not have checked in yet
}

// latestTaskOutput extracts the most recent task's output text from the
// /tasks/implant/{id} response, tolerating the loosely-documented shape.
func latestTaskOutput(data []byte) string {
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) == 0 {
		return ""
	}
	return firstString(rows[len(rows)-1], "Output", "output", "TaskResult", "result")
}

// firstString returns the first non-empty string value among the given keys.
func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
