// Package bruteratel adapts Brute Ratel C4 to RInfra. Fronted-tier: commercial,
// license-gated — RInfra stands up the server and redirectors, then a human
// operates it. (BRc4 v2.2+ ships bruteratel.py, an asyncio WebSocket automation
// library; a future Scripted-tier upgrade could build on it, but it is delivered
// only inside the licensed package and is not a public, stable API — so RInfra
// keeps BRc4 Fronted for now.)
//
// # License
//
// Brute Ratel C4 is commercially license-gated. The customer's license key is
// REQUIRED per engagement — supplied via Config.LicenseKey — and is NEVER
// bundled, logged, or stored in plaintext. If absent, Deploy returns an error.
//
// # Control
//
// Control() returns (nil, false): no public, stable automation API (the v2.2+
// bruteratel.py WebSocket library ships only inside the licensed package). The
// emulation engine records every technique as ExecManualRequired when Operator
// is nil.
package bruteratel

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
	brcPort = 443 // Brute Ratel typically listens on HTTPS/443
)

type provider struct{}

func (p *provider) Name() string         { return "bruteratel" }
func (p *provider) Tier() c2.SupportTier { return c2.TierFronted }

// Deploy starts the Brute Ratel C4 server on the node using the
// customer-supplied license key. Key is required — error clearly if absent.
// The key must not appear in any logged or audited string.
func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	if cfg.LicenseKey == "" {
		return c2.Teamserver{}, fmt.Errorf("bruteratel.Deploy: customer license key required " +
			"(supply via Config.LicenseKey from the engagement credential store; " +
			"Brute Ratel C4 is commercially licensed — the key is never bundled by RInfra)")
	}
	runner := runnerFromNode(node)
	return deployBRC(ctx, runner, node, cfg)
}

func deployBRC(ctx context.Context, runner deploy.Runner, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	// Security invariant: license key must NOT appear in the install script body.
	// The operator must have uploaded the BRC distro to the node separately.
	extraSetup := []string{
		"# Brute Ratel C4 is commercially licensed.",
		"# The operator must have uploaded the BRC distro to /opt/bruteratel/",
		"# before this step. RInfra never fetches or bundles the BRC distro.",
		"[ -d /opt/bruteratel ] || { echo '[rinfra-brc] ERROR: /opt/bruteratel not found — operator must upload BRC distro'; exit 1; }",
		"chmod +x /opt/bruteratel/badger",
		// License key goes into a restricted env file, not the script body. Its
		// contents are written out-of-band; the systemd unit loads it via
		// EnvironmentFile so the badger process sees RINFRA_BRC_LICENSE.
		"mkdir -p /etc/bruteratel",
		"install -m 0600 /dev/null /etc/bruteratel/license.env",
	}

	params := deploy.InstallParams{
		ReleaseURL:      "file:///opt/bruteratel/badger",
		SHA256:          cfg.Extra["server_sha256"],
		DestPath:        "/opt/bruteratel/badger",
		SystemdUnit:     "bruteratel",
		ServiceUser:     "root",
		ExecStart:       "/opt/bruteratel/badger --port 443 --config /etc/bruteratel/server.json",
		EnvironmentFile: "/etc/bruteratel/license.env",
		ExtraSetup:      extraSetup,
	}

	if err := deploy.RunInstall(ctx, runner, params); err != nil {
		return c2.Teamserver{}, fmt.Errorf("bruteratel.Deploy: %w", err)
	}

	// Upload the license key to a restricted env file — never in script body.
	licenseEnvContent := "RINFRA_BRC_LICENSE=" + cfg.LicenseKey
	if err := runner.Upload(ctx, "/etc/bruteratel/license.env", licenseEnvContent); err != nil {
		return c2.Teamserver{}, fmt.Errorf("bruteratel.Deploy: upload license env: %w", err)
	}

	return c2.Teamserver{
		Host:   node.PublicIP,
		Port:   brcPort,
		Status: "running",
		// ConnectionInfo is vague — the key must not appear here.
		ConnectionInfo: fmt.Sprintf("Brute Ratel C4 server @ %s:%d (operator connects via BRC client)", node.PublicIP, brcPort),
	}, nil
}

// RedirectorConfig emits an nginx reverse-proxy config fronting the Brute Ratel
// C4 listener. Reverse-proxy + categorized-domain (NOT CDN fronting).
func (p *provider) RedirectorConfig(prof domain.Profile) (string, error) {
	return brcRedirectorConfig(prof)
}

func brcRedirectorConfig(prof domain.Profile) (string, error) {
	params := deploy.NginxParams{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: brcPort,
		ServerName:   prof.RewriteHost,
		RewriteHost:  prof.RewriteHost,
		UseHTTPS:     true,
		SSLCert:      "/etc/ssl/rinfra/server.crt",
		SSLKey:       "/etc/ssl/rinfra/server.key",
		PathRules:    prof.PathRules,
		ExtraServerBlock: "# Brute Ratel C4 reverse proxy (Fronted-tier — human-operated).\n" +
			"    # Operator: customise location blocks to match BRC C2 listener URI patterns.",
	}
	cfg, err := deploy.BuildNginxConfig(params)
	if err != nil {
		return "", fmt.Errorf("bruteratel.RedirectorConfig: %w", err)
	}
	return cfg, nil
}

// Control returns (nil, false): Brute Ratel is Fronted-tier. The emulation
// engine records every technique as ExecManualRequired when the Operator is nil.
func (p *provider) Control(_ c2.Teamserver) (c2.Operator, bool) {
	return nil, false
}

// runnerFromNode builds the production SSH Runner for a node, loading
// per-engagement key material from the environment (see deploy.NewNodeRunner).
func runnerFromNode(node domain.Node) deploy.Runner {
	return deploy.NewNodeRunner(node.PublicIP)
}

// licenseKeyNotInString asserts (for audit logging) that the license key does
// not appear in a string.
func licenseKeyNotInString(s, key string) bool {
	return !strings.Contains(s, key)
}

var _ = licenseKeyNotInString
