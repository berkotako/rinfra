package cobaltstrike_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/c2/cobaltstrike"
	"github.com/rinfra/rinfra/internal/c2/deploy"
	"github.com/rinfra/rinfra/internal/domain"
)

func TestTier(t *testing.T) {
	p, err := c2.Get("cobaltstrike")
	if err != nil {
		t.Fatalf("cobaltstrike not registered: %v", err)
	}
	if p.Tier() != c2.TierFronted {
		t.Errorf("expected TierFronted, got %v", p.Tier())
	}
}

func TestControl_ReturnsFalse(t *testing.T) {
	p, err := c2.Get("cobaltstrike")
	if err != nil {
		t.Fatalf("cobaltstrike not registered: %v", err)
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
	p, err := c2.Get("cobaltstrike")
	if err != nil {
		t.Fatalf("cobaltstrike not registered: %v", err)
	}
	node := domain.Node{PublicIP: "203.0.113.40"}
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
	node := domain.Node{PublicIP: "203.0.113.40"}
	licenseKey := "CUSTOMER-LICENSE-ABC123"

	cfg := c2.Config{
		LicenseKey: licenseKey,
		Extra:      map[string]string{"teamserver_sha256": "placeholder"},
	}

	ts, err := cobaltstrike.DeployWithRunner(context.Background(), runner, node, cfg)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if ts.Host != "203.0.113.40" {
		t.Errorf("unexpected host: %q", ts.Host)
	}

	// Critical security invariant: license key must NOT appear in the uploaded
	// install script body.
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
	node := domain.Node{PublicIP: "203.0.113.40"}
	licenseKey := "TOP-SECRET-KEY-XYZ"

	cfg := c2.Config{LicenseKey: licenseKey, Extra: map[string]string{}}
	_, _ = cobaltstrike.DeployWithRunner(context.Background(), runner, node, cfg)

	// Verify the license key does not appear in any Run commands.
	for _, cmd := range runner.Commands() {
		if deploy.ContainsLicenseKey(cmd, licenseKey) {
			t.Errorf("SECURITY: license key found in command: %q", cmd)
		}
	}
}

func TestRedirectorConfig(t *testing.T) {
	p, err := c2.Get("cobaltstrike")
	if err != nil {
		t.Fatalf("cobaltstrike not registered: %v", err)
	}

	prof := domain.Profile{
		Name:        "malleable-amazon",
		RewriteHost: "s3.amazonaws.com",
		PathRules:   []string{"location /s3 { proxy_pass http://c2_upstream_50050; }"},
	}
	cfg, err := p.RedirectorConfig(prof)
	if err != nil {
		t.Fatalf("RedirectorConfig: %v", err)
	}

	checks := []string{
		"proxy_pass",
		"s3.amazonaws.com",
		"ssl",
		"server_tokens off",
	}
	for _, want := range checks {
		if !strings.Contains(cfg, want) {
			t.Errorf("redirector config missing %q", want)
		}
	}
}
