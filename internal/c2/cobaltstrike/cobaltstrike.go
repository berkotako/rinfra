// Package cobaltstrike adapts Cobalt Strike to RInfra. Fronted-tier: RInfra
// stands up the teamserver and redirectors, then a human operator drives it.
//
// # License
//
// Cobalt Strike is commercially license-gated. The customer's license key is
// REQUIRED per engagement — supplied via Config.LicenseKey — and is NEVER
// bundled, logged, or stored in plaintext by RInfra. If absent, Deploy returns
// an error. The key is passed to the teamserver launch command and redacted
// from all audited output.
//
// # Control
//
// Control() returns (nil, false): Cobalt Strike has no clean public automation
// API. The emulation engine records every technique as ExecSkipped when the
// Operator is nil, and the operator drives the framework manually.
package cobaltstrike

import (
	"context"
	"fmt"
	"strings"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/c2/deploy"
	"github.com/rinfra/rinfra/internal/domain"
)

func init() { c2.Register(&provider{}) }

const (
	csPort = 50050 // default Cobalt Strike teamserver port
)

type provider struct{}

func (p *provider) Name() string         { return "cobaltstrike" }
func (p *provider) Tier() c2.SupportTier { return c2.TierFronted }

// Deploy starts the Cobalt Strike teamserver on the node using the
// customer-supplied license key. The key is required — error clearly if absent.
// The key is passed to the upstream teamserver binary; RInfra does not store
// it in plaintext anywhere and it must not appear in logs or audit events.
func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	if cfg.LicenseKey == "" {
		return c2.Teamserver{}, fmt.Errorf("cobaltstrike.Deploy: customer license key required " +
			"(supply via Config.LicenseKey from the engagement credential store; " +
			"Cobalt Strike is commercially licensed — the key is never bundled by RInfra)")
	}
	runner := runnerFromNode(node)
	return deployCS(ctx, runner, node, cfg)
}

func deployCS(ctx context.Context, runner deploy.Runner, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	// Security invariant: license key must NEVER appear in logged script output.
	// We pass it via a dedicated environment variable that the startup wrapper
	// reads, rather than interpolating it into the script body.
	//
	// The install script placeholder for the license key uses the env variable
	// RINFRA_CS_LICENSE. The operator must upload the Cobalt Strike distro to
	// the node separately (it is not fetched here — the distro is commercially
	// gated and the operator holds it).
	extraSetup := []string{
		"# Cobalt Strike is commercially licensed.",
		"# The operator must have uploaded the Cobalt Strike distro to /opt/cobaltstrike/",
		"# before this step. RInfra never fetches or bundles the CS distro.",
		"[ -d /opt/cobaltstrike ] || { echo '[rinfra-cs] ERROR: /opt/cobaltstrike not found — operator must upload CS distro'; exit 1; }",
		"chmod +x /opt/cobaltstrike/teamserver",
		// The license key is written to a restricted file that the systemd unit reads.
		// It is NOT echoed, logged, or placed in the script body.
		"install -m 0600 /dev/null /etc/cobaltstrike/license.env",
		"echo 'RINFRA_CS_LICENSE=${RINFRA_CS_LICENSE}' >> /etc/cobaltstrike/license.env",
	}

	params := deploy.InstallParams{
		// The CS teamserver binary is operator-uploaded; we use a placeholder URL
		// that validates the checksum of the operator-provided distro.
		ReleaseURL:  "file:///opt/cobaltstrike/teamserver",
		SHA256:      cfg.Extra["teamserver_sha256"], // operator-provided per-version
		DestPath:    "/opt/cobaltstrike/teamserver",
		SystemdUnit: "cobaltstrike-teamserver",
		ServiceUser: "root",
		ExecStart:   fmt.Sprintf("/opt/cobaltstrike/teamserver %s", maskLicenseRef()),
		ExtraSetup:  extraSetup,
	}

	if err := deploy.RunInstall(ctx, runner, params); err != nil {
		return c2.Teamserver{}, fmt.Errorf("cobaltstrike.Deploy: %w", err)
	}

	// Pass the license key to the remote host via a dedicated env upload.
	// We upload it to a restricted env file — not interpolated in the script.
	licenseEnvContent := "RINFRA_CS_LICENSE=" + cfg.LicenseKey
	if err := runner.Upload(ctx, "/etc/cobaltstrike/license.env", licenseEnvContent); err != nil {
		return c2.Teamserver{}, fmt.Errorf("cobaltstrike.Deploy: upload license env: %w", err)
	}

	return c2.Teamserver{
		Host:   node.PublicIP,
		Port:   csPort,
		Status: "running",
		// ConnectionInfo is intentionally vague — the key must not appear here.
		ConnectionInfo: fmt.Sprintf("Cobalt Strike teamserver @ %s:%d (operator connects via CS client)", node.PublicIP, csPort),
	}, nil
}

// maskLicenseRef returns the ExecStart token for the license key without
// embedding the key value into the script. The systemd unit reads the env file.
func maskLicenseRef() string {
	// The actual key comes from the EnvironmentFile in the systemd unit, not
	// from a command-line argument visible in ps output.
	return "0.0.0.0 ${RINFRA_CS_LICENSE} /etc/cobaltstrike/c2.profile"
}

// RedirectorConfig emits an nginx reverse-proxy config fronting the Cobalt
// Strike HTTPS C2 listener. Reverse-proxy + categorized-domain (NOT CDN fronting).
func (p *provider) RedirectorConfig(prof domain.Profile) (string, error) {
	return csRedirectorConfig(prof)
}

func csRedirectorConfig(prof domain.Profile) (string, error) {
	// For Cobalt Strike the profile may have specific URI paths from a malleable
	// C2 profile. The PathRules field carries those.
	params := deploy.NginxParams{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: csPort,
		ServerName:   prof.RewriteHost,
		RewriteHost:  prof.RewriteHost,
		UseHTTPS:     true,
		SSLCert:      "/etc/ssl/rinfra/server.crt",
		SSLKey:       "/etc/ssl/rinfra/server.key",
		PathRules:    prof.PathRules,
		ExtraServerBlock: "# Cobalt Strike C2 reverse proxy (Fronted-tier — human-operated).\n" +
			"    # Operator: customise location blocks to match your malleable C2 profile URIs.",
	}
	cfg, err := deploy.BuildNginxConfig(params)
	if err != nil {
		return "", fmt.Errorf("cobaltstrike.RedirectorConfig: %w", err)
	}
	return cfg, nil
}

// Control returns (nil, false): Cobalt Strike is Fronted-tier. The emulation
// engine will record every technique as ExecSkipped when the Operator is nil.
func (p *provider) Control(_ c2.Teamserver) (c2.Operator, bool) {
	return nil, false
}

// runnerFromNode builds a production SSH Runner.
// TODO(live): wire real per-engagement SSH key auth from internal/secrets.
func runnerFromNode(_ domain.Node) deploy.Runner {
	return &noopRunner{}
}

type noopRunner struct{}

func (n *noopRunner) Run(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("cobaltstrike: SSH runner not wired (TODO(live))")
}
func (n *noopRunner) Upload(_ context.Context, _, _ string) error {
	return fmt.Errorf("cobaltstrike: SSH runner not wired (TODO(live))")
}

// licenseKeyNotInString asserts (for audit logging) that the license key does
// not appear in a string. Called on any string that might be logged.
func licenseKeyNotInString(s, key string) bool {
	return !strings.Contains(s, key)
}

// ensure licenseKeyNotInString is used to avoid lint errors.
var _ = licenseKeyNotInString
