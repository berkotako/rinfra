package mythic_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/c2/deploy"
	mythicpkg "github.com/rinfra/rinfra/internal/c2/mythic"
	"github.com/rinfra/rinfra/internal/domain"
)

// FakeMythicClient is a test double for MythicClient.
type FakeMythicClient struct {
	callbacks     []mythicpkg.MythicCallback
	taskOutput    string
	taskOutputErr error
	lastCmd       string
	lastParams    map[string]string
	issueTaskErr  error
}

func (f *FakeMythicClient) CreateCallback(_ context.Context, _, _, _ string) (string, error) {
	return "callback-001", nil
}
func (f *FakeMythicClient) Callbacks(_ context.Context) ([]mythicpkg.MythicCallback, error) {
	return f.callbacks, nil
}
func (f *FakeMythicClient) IssueTasking(_ context.Context, _, cmd string, params map[string]string) (string, error) {
	f.lastCmd = cmd
	f.lastParams = params
	if f.issueTaskErr != nil {
		return "", f.issueTaskErr
	}
	return "task-001", nil
}
func (f *FakeMythicClient) TaskOutput(_ context.Context, _ string) (string, error) {
	return f.taskOutput, f.taskOutputErr
}
func (f *FakeMythicClient) CreateListener(_ context.Context, _, _ string, _ int) error {
	return nil
}

func TestTier(t *testing.T) {
	p, err := c2.Get("mythic")
	if err != nil {
		t.Fatalf("mythic not registered: %v", err)
	}
	if p.Tier() != c2.TierOrchestrated {
		t.Errorf("expected TierOrchestrated, got %v", p.Tier())
	}
}

func TestControl_ReturnsOperator(t *testing.T) {
	p, err := c2.Get("mythic")
	if err != nil {
		t.Fatalf("mythic not registered: %v", err)
	}
	op, ok := p.Control(c2.Teamserver{Host: "10.0.0.1", Port: 7443})
	if !ok {
		t.Fatal("expected ok=true from Orchestrated provider")
	}
	if op == nil {
		t.Fatal("expected non-nil Operator")
	}
}

func TestDeploy_FakeRunner(t *testing.T) {
	runner := deploy.NewFakeRunner()
	node := domain.Node{
		PublicIP: "203.0.113.20",
		Spec:     domain.NodeSpec{Type: domain.NodeC2Server, C2Framework: "mythic"},
	}

	ts, err := mythicpkg.DeployWithRunner(context.Background(), runner, node, c2.Config{})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if ts.Host != "203.0.113.20" {
		t.Errorf("unexpected host: %q", ts.Host)
	}
	if ts.Port == 0 {
		t.Error("expected non-zero port")
	}

	script, ok := runner.Uploaded("/tmp/rinfra-install.sh")
	if !ok {
		t.Fatal("install script not uploaded")
	}
	// Mythic installs from source at an immutable commit + Docker Compose; there
	// is no release tarball or checksum.
	for _, want := range []string{
		"git fetch --depth 1 origin",
		"b294c8ff5354ed57a6697da61d0524286e663c95", // pinned commit (v3.4.0.5)
		"make",               // builds mythic-cli
		"./mythic-cli start", // Docker Compose up
		"docker",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("install script missing %q", want)
		}
	}
	// The UI port is set via NGINX_PORT, not the backend MYTHIC_SERVER_PORT
	// (which would collide with Nginx on 7443).
	if !strings.Contains(script, "config set NGINX_PORT 7443") {
		t.Error("install script should set NGINX_PORT (UI port), not MYTHIC_SERVER_PORT")
	}
	for _, unwanted := range []string{"placeholder", "sha256sum", "tar xz", "python3 mythic-cli", ".tar.gz", "MYTHIC_SERVER_PORT"} {
		if strings.Contains(script, unwanted) {
			t.Errorf("install script should not contain %q (wrong/old deploy model)", unwanted)
		}
	}
}

func TestRedirectorConfig(t *testing.T) {
	p, err := c2.Get("mythic")
	if err != nil {
		t.Fatalf("mythic not registered: %v", err)
	}

	prof := domain.Profile{
		Name:        "default",
		RewriteHost: "news.example.com",
	}
	cfg, err := p.RedirectorConfig(prof)
	if err != nil {
		t.Fatalf("RedirectorConfig: %v", err)
	}

	checks := []string{
		"proxy_pass",
		"news.example.com",
		"ssl",
		"proxy_http_version 1.1",
		"server_tokens off",
	}
	for _, want := range checks {
		if !strings.Contains(cfg, want) {
			t.Errorf("redirector config missing %q", want)
		}
	}
}

func TestOperator_Execute_KnownTechnique(t *testing.T) {
	client := &FakeMythicClient{taskOutput: "SystemInfo output"}
	op := mythicpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	tech := domain.Technique{
		AttackID: "T1082",
		Name:     "System Information Discovery",
		Source:   domain.SourceAtomicRedTeam,
		SourceID: "some-guid",
	}
	result, err := op.Execute(context.Background(), "callback-001", tech)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != domain.ExecSuccess {
		t.Errorf("expected ExecSuccess, got %v", result.Status)
	}
	if client.lastCmd != "sysinfo" {
		t.Errorf("expected cmd 'sysinfo', got %q", client.lastCmd)
	}
}

func TestOperator_Execute_UnknownTechnique_Skipped(t *testing.T) {
	client := &FakeMythicClient{}
	op := mythicpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	tech := domain.Technique{AttackID: "T0000.001"}
	result, err := op.Execute(context.Background(), "cb", tech)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != domain.ExecUnsupported {
		t.Errorf("expected ExecUnsupported, got %v", result.Status)
	}
}

func TestOperator_Execute_TaskingError(t *testing.T) {
	client := &FakeMythicClient{issueTaskErr: errors.New("callback not found")}
	op := mythicpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	tech := domain.Technique{AttackID: "T1057"}
	result, err := op.Execute(context.Background(), "cb", tech)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != domain.ExecFailed {
		t.Errorf("expected ExecFailed, got %v", result.Status)
	}
}

func TestOperator_Sessions(t *testing.T) {
	client := &FakeMythicClient{
		callbacks: []mythicpkg.MythicCallback{
			{ID: "cb-1", Host: "host01", User: "admin", OS: "linux", Arch: "x86_64", C2Profile: "http"},
		},
	}
	op := mythicpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	sessions, err := op.Sessions(context.Background())
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Metadata["c2_profile"] != "http" {
		t.Error("expected c2_profile=http in metadata")
	}
}

func TestOperator_StartListener(t *testing.T) {
	client := &FakeMythicClient{}
	op := mythicpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	spec := c2.ListenerSpec{Protocol: "https", Bind: "0.0.0.0", Name: "example.com"}
	if err := op.StartListener(context.Background(), spec); err != nil {
		t.Fatalf("StartListener: %v", err)
	}
}
