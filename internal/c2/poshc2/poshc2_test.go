package poshc2_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rinfra/rinfra/internal/c2"
	poshpkg "github.com/rinfra/rinfra/internal/c2/poshc2"
	"github.com/rinfra/rinfra/internal/domain"
)

// FakePoshC2Client is a test double for PoshC2Client.
type FakePoshC2Client struct {
	implants   []poshpkg.PoshC2Implant
	execResult string
	execErr    error
	lastCmd    string
}

func (f *FakePoshC2Client) Execute(_ context.Context, _, cmd string) (string, error) {
	f.lastCmd = cmd
	return f.execResult, f.execErr
}
func (f *FakePoshC2Client) Implants(_ context.Context) ([]poshpkg.PoshC2Implant, error) {
	return f.implants, nil
}
func (f *FakePoshC2Client) StartListener(_ context.Context, _ string, _ int) error { return nil }

func TestTier(t *testing.T) {
	p, err := c2.Get("poshc2")
	if err != nil {
		t.Fatalf("poshc2 not registered: %v", err)
	}
	if p.Tier() != c2.TierScripted {
		t.Errorf("expected TierScripted, got %v", p.Tier())
	}
}

func TestControl_ReturnsOperator(t *testing.T) {
	p, err := c2.Get("poshc2")
	if err != nil {
		t.Fatalf("poshc2 not registered: %v", err)
	}
	op, ok := p.Control(c2.Teamserver{})
	if !ok {
		t.Fatal("expected ok=true for Scripted provider")
	}
	if op == nil {
		t.Fatal("expected non-nil Operator")
	}
}

func TestOperator_Execute_Supported(t *testing.T) {
	client := &FakePoshC2Client{execResult: "whoami output"}
	op := poshpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	tech := domain.Technique{AttackID: "T1082", Name: "System Information Discovery"}
	result, err := op.Execute(context.Background(), "implant-1", tech)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != domain.ExecSuccess {
		t.Errorf("expected ExecSuccess, got %v", result.Status)
	}
}

func TestOperator_Execute_Unsupported_Skipped(t *testing.T) {
	client := &FakePoshC2Client{}
	op := poshpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	// T1547.001 not in poshc2's supported set
	tech := domain.Technique{AttackID: "T1547.001"}
	result, err := op.Execute(context.Background(), "implant-1", tech)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != domain.ExecSkipped {
		t.Errorf("expected ExecSkipped, got %v", result.Status)
	}
	if !strings.Contains(result.Output, "scripted-tier") {
		t.Error("ExecSkipped output should explain scripted-tier limit")
	}
}

func TestOperator_Execute_Error(t *testing.T) {
	client := &FakePoshC2Client{execErr: errors.New("implant gone")}
	op := poshpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	tech := domain.Technique{AttackID: "T1057"}
	result, err := op.Execute(context.Background(), "implant-1", tech)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != domain.ExecFailed {
		t.Errorf("expected ExecFailed, got %v", result.Status)
	}
}
