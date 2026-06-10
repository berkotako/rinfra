package deploy_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rinfra/rinfra/internal/c2/deploy"
)

func TestBuildInstallScript_ContainsExpectedLines(t *testing.T) {
	params := deploy.InstallParams{
		ReleaseURL:  "https://example.com/framework-v1.0.0-linux",
		SHA256:      "abc123def456abc123def456abc123def456abc123def456abc123def456abc1",
		DestPath:    "/usr/local/bin/framework-server",
		SystemdUnit: "framework-server",
	}

	script, err := deploy.BuildInstallScript(params)
	if err != nil {
		t.Fatalf("BuildInstallScript: %v", err)
	}

	checks := []string{
		params.ReleaseURL,
		params.SHA256,
		params.DestPath,
		params.SystemdUnit,
		"sha256sum -c -",
		"systemctl enable",
		"systemctl start",
		"nobody", // default service user
	}
	for _, want := range checks {
		if !strings.Contains(script, want) {
			t.Errorf("install script missing %q", want)
		}
	}
}

func TestBuildInstallScript_ServiceUser(t *testing.T) {
	params := deploy.InstallParams{
		ReleaseURL:  "https://example.com/fw",
		SHA256:      "deadbeef",
		DestPath:    "/usr/local/bin/fw",
		SystemdUnit: "fw",
		ServiceUser: "rinfra",
	}
	script, err := deploy.BuildInstallScript(params)
	if err != nil {
		t.Fatalf("BuildInstallScript: %v", err)
	}
	if !strings.Contains(script, "User=rinfra") {
		t.Error("script should contain User=rinfra")
	}
}

func TestBuildInstallScript_ExtraSetup(t *testing.T) {
	params := deploy.InstallParams{
		ReleaseURL:  "https://example.com/fw",
		SHA256:      "deadbeef",
		DestPath:    "/usr/bin/fw",
		SystemdUnit: "fw",
		ExtraSetup:  []string{"echo configured", "mkdir -p /etc/fw"},
	}
	script, err := deploy.BuildInstallScript(params)
	if err != nil {
		t.Fatalf("BuildInstallScript: %v", err)
	}
	if !strings.Contains(script, "echo configured") {
		t.Error("extra setup line missing from script")
	}
	if !strings.Contains(script, "mkdir -p /etc/fw") {
		t.Error("extra setup line missing from script")
	}
}

func TestRunInstall_FakeRunner(t *testing.T) {
	runner := deploy.NewFakeRunner()
	params := deploy.InstallParams{
		ReleaseURL:  "https://example.com/sliver-server_linux",
		SHA256:      "0000000000000000000000000000000000000000000000000000000000000000",
		DestPath:    "/usr/local/bin/sliver-server",
		SystemdUnit: "sliver-server",
	}

	ctx := context.Background()
	if err := deploy.RunInstall(ctx, runner, params); err != nil {
		t.Fatalf("RunInstall: %v", err)
	}

	// The script should have been uploaded.
	content, ok := runner.Uploaded("/tmp/rinfra-install.sh")
	if !ok {
		t.Fatal("install script was not uploaded")
	}
	if !strings.Contains(content, params.ReleaseURL) {
		t.Error("uploaded script missing release URL")
	}

	// The runner should have been asked to execute the script.
	cmds := runner.Commands()
	if len(cmds) == 0 {
		t.Fatal("no commands recorded")
	}
	if !strings.Contains(cmds[0], "rinfra-install.sh") {
		t.Errorf("expected command to run install script, got %q", cmds[0])
	}
}

func TestRunInstall_FakeRunnerError(t *testing.T) {
	runner := deploy.NewFakeRunner()
	runner.ReturnError = context.DeadlineExceeded

	params := deploy.InstallParams{
		ReleaseURL:  "https://example.com/fw",
		SHA256:      "abc",
		DestPath:    "/usr/bin/fw",
		SystemdUnit: "fw",
	}

	// ReturnError is set, so Upload will fail first.
	err := deploy.RunInstall(context.Background(), runner, params)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildNginxConfig(t *testing.T) {
	params := deploy.NginxParams{
		UpstreamHost: "10.0.0.5",
		UpstreamPort: 8443,
		ServerName:   "www.legitimatedomain.com",
		UseHTTPS:     true,
		SSLCert:      "/etc/ssl/certs/server.crt",
		SSLKey:       "/etc/ssl/private/server.key",
	}

	cfg, err := deploy.BuildNginxConfig(params)
	if err != nil {
		t.Fatalf("BuildNginxConfig: %v", err)
	}

	// Key nginx directives must be present.
	checks := []string{
		"proxy_pass",
		"10.0.0.5:8443",
		"www.legitimatedomain.com",
		"ssl",
		"/etc/ssl/certs/server.crt",
		"proxy_http_version 1.1",
		"server_tokens off",
		"TLSv1.2",
	}
	for _, want := range checks {
		if !strings.Contains(cfg, want) {
			t.Errorf("nginx config missing %q", want)
		}
	}
}

func TestBuildNginxConfig_HTTP(t *testing.T) {
	params := deploy.NginxParams{
		UpstreamHost: "10.0.0.10",
		UpstreamPort: 8080,
		ServerName:   "cdn.example.com",
		RewriteHost:  "c2.internal",
		UseHTTPS:     false,
	}

	cfg, err := deploy.BuildNginxConfig(params)
	if err != nil {
		t.Fatalf("BuildNginxConfig: %v", err)
	}

	if !strings.Contains(cfg, "listen 80") {
		t.Error("expected listen 80 for non-HTTPS")
	}
	if !strings.Contains(cfg, "c2.internal") {
		t.Error("expected rewrite host in config")
	}
}

func TestBuildNginxConfig_PathRules(t *testing.T) {
	params := deploy.NginxParams{
		UpstreamHost: "10.0.0.1",
		UpstreamPort: 9090,
		ServerName:   "example.com",
		PathRules:    []string{"location /health { return 200; }"},
	}

	cfg, err := deploy.BuildNginxConfig(params)
	if err != nil {
		t.Fatalf("BuildNginxConfig: %v", err)
	}
	if !strings.Contains(cfg, "location /health { return 200; }") {
		t.Error("path rule missing from nginx config")
	}
}

func TestContainsLicenseKey(t *testing.T) {
	if !deploy.ContainsLicenseKey("some string with MYKEY123 in it", "MYKEY123") {
		t.Error("expected true for string containing the key")
	}
	if deploy.ContainsLicenseKey("clean output here", "MYKEY123") {
		t.Error("expected false for string not containing the key")
	}
	if deploy.ContainsLicenseKey("doesn't matter", "") {
		t.Error("empty key should always return false")
	}
}

func TestFakeRunner_RecordsUploads(t *testing.T) {
	r := deploy.NewFakeRunner()
	ctx := context.Background()

	_ = r.Upload(ctx, "/tmp/a.sh", "content-a")
	_ = r.Upload(ctx, "/tmp/b.sh", "content-b")

	uploads := r.AllUploads()
	if len(uploads) != 2 {
		t.Fatalf("expected 2 uploads, got %d", len(uploads))
	}
	if uploads["/tmp/a.sh"] != "content-a" {
		t.Error("upload content mismatch for /tmp/a.sh")
	}
}
