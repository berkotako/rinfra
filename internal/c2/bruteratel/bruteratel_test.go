package bruteratel_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/c2/bruteratel"
	"github.com/rinfra/rinfra/internal/c2/deploy"
	"github.com/rinfra/rinfra/internal/domain"
)

func TestTier(t *testing.T) {
	p, err := c2.Get("bruteratel")
	if err != nil {
		t.Fatalf("bruteratel not registered: %v", err)
	}
	if p.Tier() != c2.TierFronted {
		t.Errorf("expected TierFronted, got %v", p.Tier())
	}
}

func TestControl_ReturnsFalse(t *testing.T) {
	p, err := c2.Get("bruteratel")
	if err != nil {
		t.Fatalf("bruteratel not registered: %v", err)
	}
	op, ok := p.Control(c2.Teamserver{})
	if ok {
		t.Error("expected ok=false for Fronted-tier provider")
	}
	if op != nil {
		t.Error("expected nil Operator for Fronted-tier provider")
	}
}

func TestDeploy_MissingLicenseKey(t *testing.T) {
	p, err := c2.Get("bruteratel")
	if err != nil {
		t.Fatalf("bruteratel not registered: %v", err)
	}
	node := domain.Node{PublicIP: "203.0.113.50"}
	_, err = p.Deploy(context.Background(), node, c2.Config{})
	if err == nil {
		t.Fatal("expected error when license key is absent")
	}
	if !strings.Contains(err.Error(), "license key required") {
		t.Errorf("error should mention 'license key required', got: %v", err)
	}
}

func TestDeploy_WithLicenseKey_FakeRunner(t *testing.T) {
	runner := deploy.NewFakeRunner()
	node := domain.Node{PublicIP: "203.0.113.50"}
	licenseKey := "BRC-CUSTOMER-KEY-987"

	cfg := c2.Config{
		LicenseKey: licenseKey,
		Extra:      map[string]string{"server_sha256": "placeholder"},
	}

	ts, err := bruteratel.DeployWithRunner(context.Background(), runner, node, cfg)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if ts.Host != "203.0.113.50" {
		t.Errorf("unexpected host: %q", ts.Host)
	}

	// Security invariant: license key must NOT appear in the install script.
	script, ok := runner.Uploaded("/tmp/rinfra-install.sh")
	if !ok {
		t.Fatal("install script not uploaded")
	}
	if deploy.ContainsLicenseKey(script, licenseKey) {
		t.Error("SECURITY: license key must not appear in the install script")
	}
}

func TestDeploy_LicenseKeyNotInCommands(t *testing.T) {
	runner := deploy.NewFakeRunner()
	node := domain.Node{PublicIP: "203.0.113.50"}
	licenseKey := "SUPER-SECRET-BRC"

	cfg := c2.Config{LicenseKey: licenseKey, Extra: map[string]string{}}
	_, _ = bruteratel.DeployWithRunner(context.Background(), runner, node, cfg)

	for _, cmd := range runner.Commands() {
		if deploy.ContainsLicenseKey(cmd, licenseKey) {
			t.Errorf("SECURITY: license key found in command: %q", cmd)
		}
	}
}

func TestRedirectorConfig(t *testing.T) {
	p, err := c2.Get("bruteratel")
	if err != nil {
		t.Fatalf("bruteratel not registered: %v", err)
	}

	prof := domain.Profile{
		Name:        "default",
		RewriteHost: "telemetry.example.com",
	}
	cfg, err := p.RedirectorConfig(prof)
	if err != nil {
		t.Fatalf("RedirectorConfig: %v", err)
	}

	checks := []string{
		"proxy_pass",
		"telemetry.example.com",
		"ssl",
		"server_tokens off",
	}
	for _, want := range checks {
		if !strings.Contains(cfg, want) {
			t.Errorf("redirector config missing %q", want)
		}
	}
}
