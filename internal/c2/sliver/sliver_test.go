package sliver_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/c2/deploy"
	sliverpkg "github.com/rinfra/rinfra/internal/c2/sliver"
	"github.com/rinfra/rinfra/internal/domain"
)

// FakeSliverClient is a test double for SliverClient.
type FakeSliverClient struct {
	sessions      []sliverpkg.SliverSession
	executeResult string
	executeErr    error
	LastCmd       string
}

func (f *FakeSliverClient) StartMTLSListener(_ context.Context, _ string, _ uint32) error {
	return nil
}
func (f *FakeSliverClient) StartHTTPSListener(_ context.Context, _ string, _ uint32) error {
	return nil
}
func (f *FakeSliverClient) StartDNSListener(_ context.Context, _ []string) error {
	return nil
}
func (f *FakeSliverClient) Sessions(_ context.Context) ([]sliverpkg.SliverSession, error) {
	return f.sessions, nil
}
func (f *FakeSliverClient) Execute(_ context.Context, _, cmd string) (string, error) {
	f.LastCmd = cmd
	return f.executeResult, f.executeErr
}

func TestTier(t *testing.T) {
	p, err := c2.Get("sliver")
	if err != nil {
		t.Fatalf("sliver not registered: %v", err)
	}
	if p.Tier() != c2.TierOrchestrated {
		t.Errorf("expected TierOrchestrated, got %v", p.Tier())
	}
}

func TestControl_ReturnsOperator(t *testing.T) {
	p, err := c2.Get("sliver")
	if err != nil {
		t.Fatalf("sliver not registered: %v", err)
	}
	op, ok := p.Control(c2.Teamserver{Host: "10.0.0.1", Port: 31337})
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
		PublicIP: "203.0.113.10",
		Spec:     domain.NodeSpec{Type: domain.NodeC2Server, C2Framework: "sliver"},
	}
	cfg := c2.Config{}

	ts, err := sliverpkg.DeployWithRunner(context.Background(), runner, node, cfg)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if ts.Host != "203.0.113.10" {
		t.Errorf("expected host 203.0.113.10, got %q", ts.Host)
	}
	if ts.Port == 0 {
		t.Error("expected non-zero port")
	}

	// Verify install script was uploaded.
	script, ok := runner.Uploaded("/tmp/rinfra-install.sh")
	if !ok {
		t.Fatal("install script was not uploaded")
	}
	// Script must reference the official release URL.
	if !strings.Contains(script, "github.com/BishopFox/sliver") {
		t.Error("install script missing upstream release URL")
	}
	// Script must verify checksum.
	if !strings.Contains(script, "sha256sum") {
		t.Error("install script missing sha256 verification")
	}
}

func TestRedirectorConfig(t *testing.T) {
	p, err := c2.Get("sliver")
	if err != nil {
		t.Fatalf("sliver not registered: %v", err)
	}

	prof := domain.Profile{
		Name:        "default",
		RewriteHost: "cdn.example.com",
		PathRules:   []string{"location /api { return 403; }"},
	}

	cfg, err := p.RedirectorConfig(prof)
	if err != nil {
		t.Fatalf("RedirectorConfig: %v", err)
	}

	checks := []string{
		"proxy_pass",
		"cdn.example.com",
		"ssl",
		"proxy_http_version 1.1",
	}
	for _, want := range checks {
		if !strings.Contains(cfg, want) {
			t.Errorf("redirector config missing %q", want)
		}
	}
}

func TestOperator_Execute_KnownTechnique(t *testing.T) {
	client := &FakeSliverClient{executeResult: "desktop-01\\Administrator"}
	op := sliverpkg.NewOperatorWithClient(c2.Teamserver{Host: "10.0.0.1"}, client)

	tech := domain.Technique{
		AttackID: "T1082",
		Name:     "System Information Discovery",
		Tactic:   "discovery",
		Source:   domain.SourceAtomicRedTeam,
		SourceID: "5b92b7b4-5d87-4285-9c07-a6c31a7e1e0e",
	}

	result, err := op.Execute(context.Background(), "session-001", tech)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != domain.ExecSuccess {
		t.Errorf("expected ExecSuccess, got %v", result.Status)
	}
	if client.LastCmd != "sysinfo" {
		t.Errorf("expected 'sysinfo' command, got %q", client.LastCmd)
	}
}

func TestOperator_Execute_UnknownTechnique_Skipped(t *testing.T) {
	client := &FakeSliverClient{}
	op := sliverpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	technique := domain.Technique{
		AttackID: "T9999.999",
		Name:     "Totally Unknown",
		Source:   domain.SourceAtomicRedTeam,
	}

	result, err := op.Execute(context.Background(), "s1", technique)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != domain.ExecSkipped {
		t.Errorf("expected ExecSkipped for unknown technique, got %v", result.Status)
	}
}

func TestOperator_Execute_Failed(t *testing.T) {
	client := &FakeSliverClient{executeErr: errors.New("session disconnected")}
	op := sliverpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	technique := domain.Technique{AttackID: "T1057"}
	result, err := op.Execute(context.Background(), "s1", technique)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result.Status != domain.ExecFailed {
		t.Errorf("expected ExecFailed, got %v", result.Status)
	}
	if result.Err == "" {
		t.Error("expected non-empty Err field")
	}
}

func TestOperator_Sessions(t *testing.T) {
	client := &FakeSliverClient{
		sessions: []sliverpkg.SliverSession{
			{ID: "abc123", Hostname: "workstation01", Username: "CORP\\user1", OS: "windows", Arch: "amd64"},
		},
	}
	op := sliverpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	sessions, err := op.Sessions(context.Background())
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].ID != "abc123" {
		t.Errorf("expected session ID abc123, got %q", sessions[0].ID)
	}
	if sessions[0].Metadata["os"] != "windows" {
		t.Error("expected os=windows in metadata")
	}
}

func TestOperator_StartListener_HTTP(t *testing.T) {
	client := &FakeSliverClient{}
	op := sliverpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	spec := c2.ListenerSpec{
		Name:     "c2.example.com",
		Protocol: "https",
		Bind:     "0.0.0.0",
	}
	if err := op.StartListener(context.Background(), spec); err != nil {
		t.Fatalf("StartListener: %v", err)
	}
}

func TestOperator_StartListener_UnsupportedProtocol(t *testing.T) {
	client := &FakeSliverClient{}
	op := sliverpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	spec := c2.ListenerSpec{Protocol: "ftp"}
	err := op.StartListener(context.Background(), spec)
	if err == nil {
		t.Fatal("expected error for unsupported protocol")
	}
}
