// Package havoc adapts the Havoc C2 framework to RInfra. Fronted-tier: RInfra
// deploys the Havoc teamserver and stands up its redirector, then a human
// operates it with their own Havoc client.
//
// # Why Fronted, not Scripted
//
// Havoc has NO headless/scriptable operator CLI. Its only programmatic operator
// surface is an undocumented JSON-over-WebSocket API on the teamserver
// (wss://host:40056/havoc/): only the auth, listener-create, and agent-compile
// messages are publicly known (from the archived havoc-py library and CVE
// research); the session-list and command-exec event IDs are not documented and
// can't be validated without a live teamserver. Rather than ship an
// unvalidated, reverse-engineered exec client that would report misleading
// results, RInfra treats Havoc as Fronted: Control() returns (nil, false) and
// the emulation engine records every technique as ExecManualRequired. A real
// WebSocket operator client (→ Scripted) is a future upgrade gated on being able
// to validate the protocol against a live Havoc teamserver.
//
// # Posture
//
// This adapter DEPLOYS and FRONTS the upstream Havoc release. It does not
// implement Havoc, implants, payloads, or evasion. Havoc has no signed release
// tarball, so Deploy builds the teamserver from a pinned git ref of the official
// repository entirely within the install script (no download/checksum step).
package havoc

import (
	"context"
	"fmt"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/c2/deploy"
	"github.com/rinfra/rinfra/internal/domain"
)

func init() { c2.Register(&provider{}) }

const (
	// havocRepo / havocRef: Havoc publishes no signed release artifacts, so the
	// teamserver is built from source at a pinned git ref. Operators should pin a
	// reviewed commit/tag for reproducibility; "main" tracks upstream.
	havocRepo = "https://github.com/HavocFramework/Havoc"
	havocRef  = "main"
	havocPort = 40056 // default Havoc teamserver operator port
)

type provider struct{}

func (p *provider) Name() string         { return "havoc" }
func (p *provider) Tier() c2.SupportTier { return c2.TierFronted }

// Deploy builds and starts the upstream Havoc teamserver on the node via SSH.
func (p *provider) Deploy(ctx context.Context, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	return deployHavoc(ctx, runnerFromNode(node), node, cfg)
}

func deployHavoc(ctx context.Context, runner deploy.Runner, node domain.Node, cfg c2.Config) (c2.Teamserver, error) {
	// Operator credentials for the teamserver profile. Operator-supplied via
	// Config.Extra; a clearly-marked default is written when absent so the deploy
	// is functional and the operator rotates it on first connect.
	operatorPass := cfg.Extra["operator_password"]
	if operatorPass == "" {
		operatorPass = "rinfra-change-on-first-connect"
	}

	// Havoc's teamserver is built from source (no release binary). Build deps +
	// git clone + `make ts-build` (produces ./havoc) all run in ExtraSetup; the
	// install template skips its download/checksum block because ReleaseURL is
	// empty.
	profile := havocProfile(operatorPass)
	extraSetup := []string{
		"export DEBIAN_FRONTEND=noninteractive",
		"apt-get update -y || true",
		"apt-get install -y git build-essential cmake make golang-go nasm mingw-w64 " +
			"pkg-config libssl-dev libboost-all-dev python3-dev curl || true",
		fmt.Sprintf("rm -rf /opt/havoc && git clone --depth 1 --branch %s %s /opt/havoc", havocRef, havocRepo),
		"cd /opt/havoc && make ts-build", // builds the teamserver binary -> /opt/havoc/havoc
		"install -d -m 0755 /etc/havoc",
		"cat > /etc/havoc/havoc.yaotl <<'YAOTL'\n" + profile + "\nYAOTL",
		"echo '[rinfra-havoc] Havoc teamserver built from source at pinned ref'",
	}

	params := deploy.InstallParams{
		// No ReleaseURL/SHA256: Havoc has no signed release; built in ExtraSetup.
		DestPath:    "/opt/havoc/havoc",
		SystemdUnit: "havoc-teamserver",
		ServiceUser: "root",
		// Run from /opt/havoc so the teamserver finds its data/ directory.
		ExecStart:  "/bin/bash -c 'cd /opt/havoc && ./havoc server --profile /etc/havoc/havoc.yaotl'",
		ExtraSetup: extraSetup,
	}

	if err := deploy.RunInstall(ctx, runner, params); err != nil {
		return c2.Teamserver{}, fmt.Errorf("havoc.Deploy: %w", err)
	}

	return c2.Teamserver{
		Host:           node.PublicIP,
		Port:           havocPort,
		Status:         "running",
		ConnectionInfo: fmt.Sprintf("Havoc teamserver @ %s:%d (operator connects with the Havoc client)", node.PublicIP, havocPort),
	}, nil
}

// havocProfile renders a starter Havoc teamserver profile in Yaotl (HCL), the
// format the teamserver expects (NOT YAML). The operator refines listeners and
// the malleable C2 profile via their client.
func havocProfile(operatorPass string) string {
	return fmt.Sprintf(`# RInfra-generated Havoc teamserver profile (Yaotl/HCL).
# Operator: refine listeners and the malleable C2 profile as needed.
Teamserver {
    Host = "0.0.0.0"
    Port = %d
}

Operators {
    user "rinfra" {
        Password = "%s"
    }
}

Listeners {
    Http {
        Name     = "rinfra-https"
        Hosts    = ["0.0.0.0"]
        HostBind = "0.0.0.0"
        PortBind = 443
        PortConn = 443
        Secure   = true
    }
}
`, havocPort, operatorPass)
}

// RedirectorConfig emits an nginx reverse-proxy config fronting the Havoc HTTPS
// listener. Reverse-proxy + categorized-domain (NOT CDN fronting).
func (p *provider) RedirectorConfig(prof domain.Profile) (string, error) {
	params := deploy.NginxParams{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 443,
		ServerName:   prof.RewriteHost,
		RewriteHost:  prof.RewriteHost,
		UseHTTPS:     true,
		SSLCert:      "/etc/ssl/rinfra/server.crt",
		SSLKey:       "/etc/ssl/rinfra/server.key",
		PathRules:    prof.PathRules,
		ExtraServerBlock: "# Havoc C2 reverse proxy\n" +
			"    # Set proxy_pass upstream to actual Havoc teamserver IP.",
	}
	cfg, err := deploy.BuildNginxConfig(params)
	if err != nil {
		return "", fmt.Errorf("havoc.RedirectorConfig: %w", err)
	}
	return cfg, nil
}

// Control returns (nil, false): Havoc is Fronted-tier (see package doc). The
// emulation engine records every technique as ExecManualRequired.
func (p *provider) Control(_ c2.Teamserver) (c2.Operator, bool) {
	return nil, false
}

func runnerFromNode(node domain.Node) deploy.Runner {
	return deploy.NewNodeRunner(node.PublicIP)
}
