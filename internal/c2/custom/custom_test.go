package custom_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rinfra/rinfra/internal/c2"
	custompkg "github.com/rinfra/rinfra/internal/c2/custom"
	"github.com/rinfra/rinfra/internal/domain"
)

// FakeCustomClient is a test double for CustomClient.
type FakeCustomClient struct {
	execResult string
	execErr    error
	lastCmd    string
}

func (f *FakeCustomClient) Execute(_ context.Context, _, cmd string) (string, error) {
	f.lastCmd = cmd
	return f.execResult, f.execErr
}
func (f *FakeCustomClient) Sessions(_ context.Context) ([]custompkg.CustomSession, error) {
	return []custompkg.CustomSession{{ID: "s1", Hostname: "host1", Username: "user1"}}, nil
}
func (f *FakeCustomClient) StartListener(_ context.Context, _, _ string, _ int) error { return nil }

func TestTier(t *testing.T) {
	p, err := c2.Get("custom")
	if err != nil {
		t.Fatalf("custom not registered: %v", err)
	}
	if p.Tier() != c2.TierOrchestrated {
		t.Errorf("expected TierOrchestrated, got %v", p.Tier())
	}
}

func TestControl_ReturnsOperator(t *testing.T) {
	p, err := c2.Get("custom")
	if err != nil {
		t.Fatalf("custom not registered: %v", err)
	}
	op, ok := p.Control(c2.Teamserver{})
	if !ok {
		t.Fatal("expected ok=true for Orchestrated provider")
	}
	if op == nil {
		t.Fatal("expected non-nil Operator")
	}
}

func TestOperator_Execute_Known(t *testing.T) {
	client := &FakeCustomClient{execResult: "sysinfo output"}
	op := custompkg.NewOperatorWithClient(c2.Teamserver{}, client)

	tech := domain.Technique{AttackID: "T1082"}
	result, err := op.Execute(context.Background(), "s1", tech)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != domain.ExecSuccess {
		t.Errorf("expected ExecSuccess, got %v", result.Status)
	}
	if client.lastCmd != "sysinfo" {
		t.Errorf("expected 'sysinfo', got %q", client.lastCmd)
	}
}

func TestOperator_Execute_Unknown_Skipped(t *testing.T) {
	client := &FakeCustomClient{}
	op := custompkg.NewOperatorWithClient(c2.Teamserver{}, client)

	tech := domain.Technique{AttackID: "T9999"}
	result, err := op.Execute(context.Background(), "s1", tech)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != domain.ExecSkipped {
		t.Errorf("expected ExecSkipped for unknown technique, got %v", result.Status)
	}
}

func TestRedirectorConfig(t *testing.T) {
	p, err := c2.Get("custom")
	if err != nil {
		t.Fatalf("custom not registered: %v", err)
	}
	prof := domain.Profile{RewriteHost: "api.example.com"}
	cfg, err := p.RedirectorConfig(prof)
	if err != nil {
		t.Fatalf("RedirectorConfig: %v", err)
	}
	if !strings.Contains(cfg, "proxy_pass") {
		t.Error("nginx config missing proxy_pass")
	}
	if !strings.Contains(cfg, "api.example.com") {
		t.Error("nginx config missing server_name")
	}
}
