// Package deploy provides the shared SSH-based deploy mechanics used by all C2
// framework adapters. It composes existing, publicly available framework
// release artifacts — it installs them; it does not build them.
//
// # Operator/customer responsibility
//
// License-gated frameworks (Cobalt Strike, Brute Ratel C4) require the
// customer/operator to supply a valid license key per engagement. RInfra never
// bundles, distributes, or stores a framework license in plaintext. The key is
// consumed at deploy time and redacted everywhere else.
//
// # SSH seam
//
// Runner is the interface through which adapters execute remote commands. The
// real SSHRunner dials the node host with per-engagement key auth; tests inject
// a FakeRunner that records commands without opening any real connection.
//
// # Install scripts
//
// Scripts are small shell programs that fetch official release archives by
// pinned URL + SHA-256 checksum, unpack them, and start a systemd unit. No
// payload bytes, no encoders, no evasion — only upstream artifacts. The URL,
// checksum, and unit name are parameters; nothing is hardcoded.
package deploy

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
)

// Runner is the SSH execution seam. The real implementation opens an SSH
// connection to the node; tests inject FakeRunner.
type Runner interface {
	// Run executes cmd on the remote host, returning combined stdout+stderr and
	// any error. ctx must be respected for cancellation.
	Run(ctx context.Context, cmd string) (output string, err error)

	// Upload writes content to remotePath on the remote host.
	Upload(ctx context.Context, remotePath, content string) error
}

// InstallParams parameterises the generic install script. The operator/customer
// is responsible for the upstream artifact's licensing.
type InstallParams struct {
	// ReleaseURL is the HTTPS URL of the official release archive, e.g.
	// "https://github.com/BishopFox/sliver/releases/download/v1.5.42/sliver-server_linux"
	ReleaseURL string

	// SHA256 is the expected SHA-256 hex digest of the downloaded artifact.
	// The install script verifies this before executing anything.
	SHA256 string

	// DestPath is the absolute path on the remote host where the binary lands.
	DestPath string

	// SystemdUnit is the name of the systemd service unit to create and start.
	SystemdUnit string

	// ExtraSetup is zero or more additional shell lines injected after the
	// binary is placed (e.g. writing a config file). Must not contain payload
	// content.
	ExtraSetup []string

	// ServiceUser is the user the systemd unit runs as. Defaults to "nobody".
	ServiceUser string

	// ExecStart is the ExecStart value for the unit file. Defaults to DestPath.
	ExecStart string

	// EnvironmentFile, when set, is emitted as the systemd unit's
	// EnvironmentFile directive so the service loads variables (e.g. a
	// license key) referenced by ExecStart. The "-" prefix is applied so a
	// missing file does not fail the unit. Leave empty for units that need no
	// environment file.
	EnvironmentFile string
}

// installScriptTmpl is the parameterised install script template. When
// ReleaseURL is set it:
//  1. Downloads the official release archive from ReleaseURL.
//  2. Verifies the SHA-256 checksum (skipped, with a warning, if SHA256 is
//     empty — e.g. Sliver verifies via minisign, not a published .sha256).
//  3. Places the binary at DestPath.
//
// When ReleaseURL is empty the download/verify steps are skipped entirely and
// the framework is expected to install itself within ExtraSetup (git clone +
// build, apt repo, pip, or Docker — e.g. Mythic, Metasploit). It then always:
//  4. Runs ExtraSetup lines.
//  5. Installs and starts a systemd service unit.
var installScriptTmpl = template.Must(template.New("install").Parse(`#!/usr/bin/env bash
set -euo pipefail

# RInfra upstream-artifact install script.
# This script fetches an OFFICIAL RELEASE of the named framework binary from
# the upstream project's release page and installs it as a systemd service.
# RInfra authors no payload, implant, or evasion content.

RELEASE_URL="{{ .ReleaseURL }}"
EXPECTED_SHA256="{{ .SHA256 }}"
DEST_PATH="{{ .DestPath }}"
UNIT="{{ .SystemdUnit }}"
SERVICE_USER="{{ .ServiceUser }}"

{{ if .ReleaseURL }}echo "[rinfra-install] Downloading from ${RELEASE_URL}"
curl -fsSL -o /tmp/rinfra-download "${RELEASE_URL}"

{{ if .SHA256 }}echo "[rinfra-install] Verifying checksum"
echo "${EXPECTED_SHA256}  /tmp/rinfra-download" | sha256sum -c -
{{ else }}echo "[rinfra-install] WARNING: no SHA-256 pinned; integrity must be verified out of band (e.g. minisign/GPG)" >&2
{{ end }}install -m 0755 /tmp/rinfra-download "${DEST_PATH}"
rm -f /tmp/rinfra-download
echo "[rinfra-install] Binary installed to ${DEST_PATH}"
{{ else }}# No ReleaseURL: this framework is installed from source/package manager
# (git clone + build, apt repo, pip, or Docker) entirely within ExtraSetup below.
echo "[rinfra-install] No release binary to download; using ExtraSetup install"
{{ end }}
{{ range .ExtraSetup }}{{ . }}
{{ end }}

echo "[rinfra-install] Writing systemd unit"
cat > /etc/systemd/system/${UNIT}.service <<'UNITEOF'
[Unit]
Description=RInfra-managed {{ .SystemdUnit }} service
After=network.target

[Service]
Type=simple
User={{ .ServiceUser }}
{{ if .EnvironmentFile }}EnvironmentFile=-{{ .EnvironmentFile }}
{{ end }}ExecStart={{ .ExecStart }}
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
UNITEOF

systemctl daemon-reload
systemctl enable "${UNIT}"
systemctl start "${UNIT}"
echo "[rinfra-install] Service ${UNIT} started"
`))

// BuildInstallScript renders the install script for the given params.
func BuildInstallScript(p InstallParams) (string, error) {
	if p.ServiceUser == "" {
		p.ServiceUser = "nobody"
	}
	if p.ExecStart == "" {
		p.ExecStart = p.DestPath
	}
	var buf bytes.Buffer
	if err := installScriptTmpl.Execute(&buf, p); err != nil {
		return "", fmt.Errorf("deploy: render install script: %w", err)
	}
	return buf.String(), nil
}

// RunInstall uploads and executes the install script on the remote host.
// The script is placed at /tmp/rinfra-install.sh, made executable, and run
// as root via bash.
func RunInstall(ctx context.Context, runner Runner, params InstallParams) error {
	script, err := BuildInstallScript(params)
	if err != nil {
		return err
	}

	const remotePath = "/tmp/rinfra-install.sh"
	if err := runner.Upload(ctx, remotePath, script); err != nil {
		return fmt.Errorf("deploy: upload install script: %w", err)
	}

	out, err := runner.Run(ctx, "bash "+remotePath)
	if err != nil {
		return fmt.Errorf("deploy: run install script: %w\noutput: %s", err, out)
	}
	return nil
}

// NginxTemplate generates a generic nginx reverse-proxy config snippet.
// Each C2 adapter calls this with its own parameters, then may render
// additional framework-specific customisation.
type NginxParams struct {
	// ListenPort is the public-facing port (typically 443 for HTTPS).
	ListenPort int
	// UpstreamHost is the C2 teamserver host.
	UpstreamHost string
	// UpstreamPort is the C2 teamserver port.
	UpstreamPort int
	// ServerName is the categorized domain / SNI value.
	ServerName string
	// RewriteHost is the Host header to set when proxying (for reverse-proxy
	// patterns; NOT domain fronting — classic CDN fronting is dead per CLAUDE.md).
	RewriteHost string
	// PathRules are additional nginx location blocks (raw nginx config lines).
	PathRules []string
	// ExtraServerBlock is additional directives inside the server { } block.
	ExtraServerBlock string
	// UseHTTPS indicates whether to generate an SSL/TLS server block.
	UseHTTPS bool
	// SSLCert is the path to the TLS certificate (required if UseHTTPS).
	SSLCert string
	// SSLKey is the path to the TLS key (required if UseHTTPS).
	SSLKey string
}

var nginxTmpl = template.Must(template.New("nginx").Parse(`# RInfra-generated nginx reverse-proxy config.
# This configuration fronts C2 traffic via a reverse proxy using a categorized
# domain. It does NOT use classic CDN domain fronting (which is effectively dead
# on major CDNs).
#
# OPERATOR NOTE: Ensure this node's domain is registered and categorized
# appropriately before deploying this engagement.

upstream c2_upstream_{{ .UpstreamPort }} {
    server {{ .UpstreamHost }}:{{ .UpstreamPort }};
    keepalive 32;
}

server {
    listen {{ .ListenPort }}{{ if .UseHTTPS }} ssl{{ end }};
    server_name {{ .ServerName }};

{{ if .UseHTTPS }}    ssl_certificate     {{ .SSLCert }};
    ssl_certificate_key {{ .SSLKey }};
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
{{ end }}
    # Suppress server version leakage.
    server_tokens off;

    location / {
        proxy_pass         http://c2_upstream_{{ .UpstreamPort }};
        proxy_http_version 1.1;
        proxy_set_header   Connection "";
        proxy_set_header   Host {{ if .RewriteHost }}{{ .RewriteHost }}{{ else }}$host{{ end }};
        proxy_set_header   X-Real-IP         $remote_addr;
        proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;
        proxy_connect_timeout 10s;
        proxy_read_timeout    300s;
    }
{{ range .PathRules }}
    {{ . }}
{{ end }}
{{ if .ExtraServerBlock }}    {{ .ExtraServerBlock }}{{ end }}
}
`))

// BuildNginxConfig renders the nginx reverse-proxy config for the given params.
func BuildNginxConfig(p NginxParams) (string, error) {
	if p.ListenPort == 0 {
		if p.UseHTTPS {
			p.ListenPort = 443
		} else {
			p.ListenPort = 80
		}
	}
	var buf bytes.Buffer
	if err := nginxTmpl.Execute(&buf, p); err != nil {
		return "", fmt.Errorf("deploy: render nginx config: %w", err)
	}
	return buf.String(), nil
}

// ContainsLicenseKey is a helper used in tests to verify that a string does NOT
// contain the license key (i.e. the key is not logged or emitted to stdout).
func ContainsLicenseKey(s, key string) bool {
	return key != "" && strings.Contains(s, key)
}
