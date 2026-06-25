package havoc_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/c2/deploy"
	havocpkg "github.com/rinfra/rinfra/internal/c2/havoc"
	"github.com/rinfra/rinfra/internal/domain"
)

func TestTier(t *testing.T) {
	p, err := c2.Get("havoc")
	if err != nil {
		t.Fatalf("havoc not registered: %v", err)
	}
	if p.Tier() != c2.TierFronted {
		t.Errorf("expected TierFronted, got %v", p.Tier())
	}
}

// TestControl_NoOperator verifies Havoc is Fronted: Control returns (nil, false)
// so the emulation engine records techniques as ExecManualRequired rather than
// driving a fabricated/unvalidated client.
func TestControl_NoOperator(t *testing.T) {
	p, err := c2.Get("havoc")
	if err != nil {
		t.Fatalf("havoc not registered: %v", err)
	}
	op, ok := p.Control(c2.Teamserver{})
	if ok {
		t.Error("expected ok=false from Fronted provider")
	}
	if op != nil {
		t.Error("expected nil Operator for Fronted-tier Havoc")
	}
}

func TestDeploy_FakeRunner(t *testing.T) {
	runner := deploy.NewFakeRunner()
	node := domain.Node{
		PublicIP: "203.0.113.30",
		Spec:     domain.NodeSpec{Type: domain.NodeC2Server, C2Framework: "havoc"},
	}

	ts, err := havocpkg.DeployWithRunner(context.Background(), runner, node, c2.Config{})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if ts.Host != "203.0.113.30" {
		t.Errorf("unexpected host: %q", ts.Host)
	}
	if ts.Port == 0 {
		t.Error("expected non-zero port")
	}

	script, ok := runner.Uploaded("/tmp/rinfra-install.sh")
	if !ok {
		t.Fatal("install script not uploaded")
	}
	// Built from source at the pinned git ref (no download/checksum step).
	for _, want := range []string{"git clone", "HavocFramework", "make ts-build", "havoc.yaotl"} {
		if !strings.Contains(script, want) {
			t.Errorf("install script missing %q", want)
		}
	}
	// Yaotl profile, NOT YAML, and no placeholder checksum.
	if !strings.Contains(script, "Teamserver {") {
		t.Error("profile should be Yaotl/HCL (Teamserver { ... })")
	}
	if strings.Contains(script, "placeholder") {
		t.Error("install script must not contain a placeholder checksum")
	}
}

func TestDeploy_OperatorPasswordFromConfig(t *testing.T) {
	runner := deploy.NewFakeRunner()
	node := domain.Node{PublicIP: "203.0.113.31", Spec: domain.NodeSpec{C2Framework: "havoc"}}
	_, err := havocpkg.DeployWithRunner(context.Background(), runner, node,
		c2.Config{Extra: map[string]string{"operator_password": "s3cret-pw"}})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	script, _ := runner.Uploaded("/tmp/rinfra-install.sh")
	if !strings.Contains(script, "s3cret-pw") {
		t.Error("operator password from Config.Extra should appear in the profile")
	}
}

func TestRedirectorConfig(t *testing.T) {
	p, err := c2.Get("havoc")
	if err != nil {
		t.Fatalf("havoc not registered: %v", err)
	}
	cfg, err := p.RedirectorConfig(domain.Profile{Name: "default", RewriteHost: "updates.example.com"})
	if err != nil {
		t.Fatalf("RedirectorConfig: %v", err)
	}
	for _, want := range []string{"proxy_pass", "updates.example.com", "ssl"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("redirector config missing %q", want)
		}
	}
}
