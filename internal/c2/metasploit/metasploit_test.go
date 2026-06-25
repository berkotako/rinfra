package metasploit_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/c2/deploy"
	msfpkg "github.com/rinfra/rinfra/internal/c2/metasploit"
	"github.com/rinfra/rinfra/internal/domain"
)

// FakeMsfClient is a test double for MsfRpcdClient. Reads model msfrpcd's
// polling-snapshot behaviour: the rendered output is returned by the first read
// and empty thereafter, so the operator's drain loop terminates.
type FakeMsfClient struct {
	sessions      []msfpkg.MsfSession
	shellOutput   string
	shellErr      error // returned by the dispatch call (meterpreter run / shell write)
	consoleWrites []string
	reads         int
	lastDispatch  string // "meterpreter" or "shell" — which transport ran the command
}

func (f *FakeMsfClient) popOutput() string {
	f.reads++
	if f.reads == 1 {
		return f.shellOutput
	}
	return ""
}

func (f *FakeMsfClient) Auth(_ context.Context, _, _ string) error { return nil }
func (f *FakeMsfClient) ConsoleCreate(_ context.Context) (string, error) {
	return "console-1", nil
}
func (f *FakeMsfClient) ConsoleWrite(_ context.Context, _, cmd string) error {
	f.consoleWrites = append(f.consoleWrites, cmd)
	return nil
}
func (f *FakeMsfClient) ConsoleRead(_ context.Context, _ string) (string, bool, error) {
	return "", false, nil // idle console
}
func (f *FakeMsfClient) SessionList(_ context.Context) ([]msfpkg.MsfSession, error) {
	return f.sessions, nil
}
func (f *FakeMsfClient) SessionMeterpreterRun(_ context.Context, _, _ string) error {
	f.lastDispatch = "meterpreter"
	return f.shellErr
}
func (f *FakeMsfClient) SessionMeterpreterRead(_ context.Context, _ string) (string, error) {
	return f.popOutput(), nil
}
func (f *FakeMsfClient) SessionShellWrite(_ context.Context, _, _ string) error {
	f.lastDispatch = "shell"
	return f.shellErr
}
func (f *FakeMsfClient) SessionShellRead(_ context.Context, _ string) (string, error) {
	return f.popOutput(), nil
}

func TestTier(t *testing.T) {
	p, err := c2.Get("metasploit")
	if err != nil {
		t.Fatalf("metasploit not registered: %v", err)
	}
	if p.Tier() != c2.TierOrchestrated {
		t.Errorf("expected TierOrchestrated, got %v", p.Tier())
	}
}

func TestControl_ReturnsOperator(t *testing.T) {
	p, err := c2.Get("metasploit")
	if err != nil {
		t.Fatalf("metasploit not registered: %v", err)
	}
	op, ok := p.Control(c2.Teamserver{})
	if !ok {
		t.Fatal("expected ok=true for Orchestrated provider")
	}
	if op == nil {
		t.Fatal("expected non-nil Operator")
	}
}

func TestDeploy_FakeRunner(t *testing.T) {
	runner := deploy.NewFakeRunner()
	node := domain.Node{PublicIP: "203.0.113.60"}

	ts, err := msfpkg.DeployWithRunner(context.Background(), runner, node, c2.Config{})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if ts.Host != "203.0.113.60" {
		t.Errorf("unexpected host: %q", ts.Host)
	}

	script, ok := runner.Uploaded("/tmp/rinfra-install.sh")
	if !ok {
		t.Fatal("install script not uploaded")
	}
	if !strings.Contains(script, "rapid7") {
		t.Error("install script should reference the Rapid7 official installer URL")
	}
}

func TestRedirectorConfig(t *testing.T) {
	p, err := c2.Get("metasploit")
	if err != nil {
		t.Fatalf("metasploit not registered: %v", err)
	}
	prof := domain.Profile{RewriteHost: "cdn.example.com"}
	cfg, err := p.RedirectorConfig(prof)
	if err != nil {
		t.Fatalf("RedirectorConfig: %v", err)
	}
	checks := []string{"proxy_pass", "cdn.example.com", "ssl"}
	for _, want := range checks {
		if !strings.Contains(cfg, want) {
			t.Errorf("redirector config missing %q", want)
		}
	}
}

func TestOperator_Execute_KnownTechnique(t *testing.T) {
	client := &FakeMsfClient{shellOutput: "System info output"}
	op := msfpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	tech := domain.Technique{AttackID: "T1082"}
	result, err := op.Execute(context.Background(), "1", tech)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != domain.ExecSuccess {
		t.Errorf("expected ExecSuccess, got %v", result.Status)
	}
}

// TestOperator_Execute_RoutesBySessionType verifies meterpreter sessions are
// driven via meterpreter_run_single (not shell_write), and raw shell sessions via
// shell_write — and that the drained output is returned either way.
func TestOperator_Execute_RoutesBySessionType(t *testing.T) {
	for _, tc := range []struct {
		name, sessType, wantDispatch string
	}{
		{"meterpreter", "meterpreter", "meterpreter"},
		{"shell", "shell", "shell"},
		{"unknown defaults to meterpreter", "", "meterpreter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &FakeMsfClient{
				shellOutput: "drained output",
				sessions:    []msfpkg.MsfSession{{ID: "1", Type: tc.sessType}},
			}
			op := msfpkg.NewOperatorWithClient(c2.Teamserver{}, client)
			res, err := op.Execute(context.Background(), "1", domain.Technique{AttackID: "T1082"})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if res.Status != domain.ExecSuccess {
				t.Errorf("status = %v, want ExecSuccess", res.Status)
			}
			if res.Output != "drained output" {
				t.Errorf("output = %q, want drained output", res.Output)
			}
			if client.lastDispatch != tc.wantDispatch {
				t.Errorf("dispatch = %q, want %q", client.lastDispatch, tc.wantDispatch)
			}
		})
	}
}

func TestOperator_Execute_UnknownTechnique_Skipped(t *testing.T) {
	client := &FakeMsfClient{}
	op := msfpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	tech := domain.Technique{AttackID: "T9876.543"}
	result, err := op.Execute(context.Background(), "1", tech)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != domain.ExecUnsupported {
		t.Errorf("expected ExecUnsupported, got %v", result.Status)
	}
}

func TestOperator_Execute_SessionError(t *testing.T) {
	client := &FakeMsfClient{shellErr: errors.New("session closed")}
	op := msfpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	tech := domain.Technique{AttackID: "T1057"}
	result, err := op.Execute(context.Background(), "1", tech)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != domain.ExecFailed {
		t.Errorf("expected ExecFailed, got %v", result.Status)
	}
}

func TestOperator_StartListener(t *testing.T) {
	client := &FakeMsfClient{}
	op := msfpkg.NewOperatorWithClient(c2.Teamserver{}, client)

	spec := c2.ListenerSpec{Protocol: "https", Bind: "0.0.0.0"}
	if err := op.StartListener(context.Background(), spec); err != nil {
		t.Fatalf("StartListener: %v", err)
	}
	if len(client.consoleWrites) == 0 {
		t.Error("expected console writes for listener setup")
	}
	// Verify multi/handler was used.
	joined := strings.Join(client.consoleWrites, " ")
	if !strings.Contains(joined, "exploit/multi/handler") {
		t.Error("expected multi/handler in console commands")
	}
}

func TestOperator_Sessions(t *testing.T) {
	client := &FakeMsfClient{
		sessions: []msfpkg.MsfSession{
			{ID: "1", Type: "meterpreter", Info: "WORKSTATION01", ViaExploit: "multi/handler"},
		},
	}
	op := msfpkg.NewOperatorWithClient(c2.Teamserver{}, client)
	sessions, err := op.Sessions(context.Background())
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Metadata["type"] != "meterpreter" {
		t.Errorf("expected type=meterpreter, got %q", sessions[0].Metadata["type"])
	}
}
