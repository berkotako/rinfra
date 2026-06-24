package redirector_test

import (
	"strings"
	"testing"

	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/redirector"
)

func TestRenderNginx_PlainRelay(t *testing.T) {
	cfg, err := redirector.RenderNginx(
		domain.Profile{Name: "relay"},
		redirector.Upstream{Host: "10.0.0.9", Port: 8443, TLS: true},
		"http", "",
	)
	if err != nil {
		t.Fatalf("RenderNginx: %v", err)
	}
	for _, want := range []string{
		"listen 80;",
		"server_name _;",
		"proxy_pass https://10.0.0.9:8443;",
		"proxy_set_header Host $host;",
		"location / {",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config missing %q\n---\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "return 444") {
		t.Error("plain relay should not have a default-deny block")
	}
}

func TestRenderNginx_CategorizedWithRewriteHost(t *testing.T) {
	cfg, err := redirector.RenderNginx(
		domain.Profile{Name: "apt", RewriteHost: "cdn.example.com", PathRules: []string{"/jquery.min.js", "/api/v1"}},
		redirector.Upstream{Host: "10.0.0.9", Port: 443, TLS: true},
		"https", "cdn-front.example.com",
	)
	if err != nil {
		t.Fatalf("RenderNginx: %v", err)
	}
	for _, want := range []string{
		"listen 443 ssl;",
		"server_name cdn-front.example.com;",
		"proxy_set_header Host cdn.example.com;",
		"location /jquery.min.js {",
		"location /api/v1 {",
		"return 444;", // default deny for unlisted paths
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config missing %q\n---\n%s", want, cfg)
		}
	}
}

func TestRenderNginx_DedupesPathRules(t *testing.T) {
	cfg, _ := redirector.RenderNginx(
		domain.Profile{PathRules: []string{"/a", "/a", "", "  ", "/b"}},
		redirector.Upstream{Host: "h", Port: 80},
		"http", "",
	)
	if got := strings.Count(cfg, "location /a {"); got != 1 {
		t.Errorf("expected /a once, got %d", got)
	}
	if !strings.Contains(cfg, "location /b {") {
		t.Error("expected /b location")
	}
}

func TestInstallScript(t *testing.T) {
	s := redirector.InstallScript(redirector.StagePath, redirector.InstallPath)
	for _, want := range []string{
		"apt-get install -y nginx", "dnf install -y nginx", "yum install -y nginx", // package-manager detection
		"nginx -t",                // validate before restart
		"systemctl restart nginx", // reload
		redirector.StagePath,      // source
		redirector.InstallPath,    // destination
	} {
		if !strings.Contains(s, want) {
			t.Errorf("install script missing %q", want)
		}
	}
}

func TestRenderNginx_Errors(t *testing.T) {
	if _, err := redirector.RenderNginx(domain.Profile{}, redirector.Upstream{Host: "h", Port: 80}, "dns", ""); err == nil {
		t.Error("dns subtype should error")
	}
	if _, err := redirector.RenderNginx(domain.Profile{}, redirector.Upstream{Port: 80}, "http", ""); err == nil {
		t.Error("missing upstream host should error")
	}
	if _, err := redirector.RenderNginx(domain.Profile{}, redirector.Upstream{Host: "h"}, "http", ""); err == nil {
		t.Error("missing upstream port should error")
	}
}
