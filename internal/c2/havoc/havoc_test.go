package havoc_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/c2/deploy"
	havocpkg "github.com/rinfra/rinfra/internal/c2/havoc"
	"github.com/rinfra/rinfra/internal/domain"
)

// FakeHavocClient is a test double for HavocClient.
type FakeHavocClient struct {
	sessions   []havocpkg.HavocSession
	execResult string
	execErr    error
	lastCmd    string
}

func (f *FakeHavocClient) Execute(_ context.Context, _, cmd string) (string, error) {
	f.lastCmd = cmd
	return f.execResult, f.execErr
}
func (f *FakeHavocClient) Sessions(_ context.Context) ([]havocpkg.HavocSession, error) {
	return f.sessions, nil
}
func (f *FakeHavocClient) StartListener(_ context.Context, _, _ string, _ int) error {
	return nil
}

func TestTier(t *testing.T) {
	p, err := c2.Get("havoc")
	if err != nil {
		t.Fatalf("havoc not registered: %v", err)
	}
	if p.Tier() != c2.TierScripted {
		t.Errorf("expected TierScripted, got %v", p.Tier())
	}
}

func TestControl_ReturnsOperator(t *testing.T) {
	p, err := c2.Get("havoc")
	if err != nil {
		t.Fatalf("havoc not registered: %v", err)
	}
	op, ok := p.Control(c2.Teamserver{})
	if !ok {
		t.Fatal("expected ok=true from Scripted provider")
	}
	if op == nil {
		t.Fatal("expected non-nil Operator")
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
	if !strings.Contains(script, "HavocFramework") {
		t.Error("script should reference upstream Havoc release URL")
	}
}

func TestRedirectorConfig(t *testing.T) {
	p, err := c2.Get("havoc")
	if err != nil {
		t.Fatalf("havoc not registered: %v", err)
	}

	prof := domain.Profile{
		Name:        "default",
		RewriteHost: "updates.example.com",
	}
	cfg, err := p.RedirectorConfig(prof)
	if err != nil {
		t.Fatalf("RedirectorConfig: %v", err)
	}

	checks := []string{
		"proxy_pass",
		"updates.example.com",
		"ssl",
	}
	for _, want := range checks {
		if !strings.Contains(cfg, want) {
			t.Errorf("redirector config missing %q", want)
		}
	}
}

func TestOperator_Execute_SupportedTechnique(t *testing.T) {
	client := &FakeHavocClient{execResult: "NT AUTHORITY\\SYSTEM"}
	op := havocpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	tech := domain.Technique{AttackID: "T1057", Name: "Process Discovery"}
	result, err := op.Execute(context.Background(), "demon-001", tech)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != domain.ExecSuccess {
		t.Errorf("expected ExecSuccess, got %v", result.Status)
	}
	if client.lastCmd != "ps" {
		t.Errorf("expected cmd 'ps', got %q", client.lastCmd)
	}
}

func TestOperator_Execute_UnsupportedTechnique_Skipped(t *testing.T) {
	client := &FakeHavocClient{}
	op := havocpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	// T1547.001 is NOT in the Havoc supported subset.
	tech := domain.Technique{AttackID: "T1547.001", Name: "Registry Run Keys"}
	result, err := op.Execute(context.Background(), "demon-001", tech)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != domain.ExecSkipped {
		t.Errorf("expected ExecSkipped for out-of-subset technique, got %v", result.Status)
	}
	if !strings.Contains(result.Output, "scripted-tier") {
		t.Errorf("ExecSkipped output should explain the scripted-tier limit, got: %q", result.Output)
	}
}

func TestOperator_Execute_Failed(t *testing.T) {
	client := &FakeHavocClient{execErr: errors.New("connection lost")}
	op := havocpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	tech := domain.Technique{AttackID: "T1082"}
	result, err := op.Execute(context.Background(), "d1", tech)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != domain.ExecFailed {
		t.Errorf("expected ExecFailed, got %v", result.Status)
	}
}

func TestOperator_Sessions(t *testing.T) {
	client := &FakeHavocClient{
		sessions: []havocpkg.HavocSession{
			{ID: "d1", Hostname: "HOST01", Username: "admin", OS: "windows", Arch: "x64"},
		},
	}
	op := havocpkg.NewOperatorWithClient(c2.Teamserver{}, client)
	sessions, err := op.Sessions(context.Background())
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Host != "HOST01" {
		t.Errorf("expected host HOST01, got %q", sessions[0].Host)
	}
}
