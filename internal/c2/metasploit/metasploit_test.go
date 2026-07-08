package metasploit_test

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/c2/deploy"
	msfpkg "github.com/rinfra/rinfra/internal/c2/metasploit"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/emulation/ttp"
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
	lastCmd       string // the exact rendered command handed to the dispatch call
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
func (f *FakeMsfClient) SessionMeterpreterRun(_ context.Context, _, cmd string) error {
	f.lastDispatch = "meterpreter"
	f.lastCmd = cmd
	return f.shellErr
}
func (f *FakeMsfClient) SessionMeterpreterRead(_ context.Context, _ string) (string, error) {
	return f.popOutput(), nil
}
func (f *FakeMsfClient) SessionShellWrite(_ context.Context, _, cmd string) error {
	f.lastDispatch = "shell"
	f.lastCmd = strings.TrimSuffix(cmd, "\n")
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
	if !strings.Contains(script, "msfupdate.erb") {
		t.Error("install script should use the valid Rapid7 installer wrapper (msfupdate.erb)")
	}
	for _, unwanted := range []string{"install-metasploit.sh", "pkill", "placeholder", "sha256sum"} {
		if strings.Contains(script, unwanted) {
			t.Errorf("install script should not contain %q (old/broken deploy)", unwanted)
		}
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
	op := msfpkg.NewOperatorWithClient(c2.Teamserver{}, client, msfpkg.WithPoll(0))

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
			op := msfpkg.NewOperatorWithClient(c2.Teamserver{}, client, msfpkg.WithPoll(0))
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
	op := msfpkg.NewOperatorWithClient(c2.Teamserver{}, client, msfpkg.WithPoll(0))

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
	op := msfpkg.NewOperatorWithClient(c2.Teamserver{}, client, msfpkg.WithPoll(0))

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
	op := msfpkg.NewOperatorWithClient(c2.Teamserver{}, client, msfpkg.WithPoll(0))

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
	op := msfpkg.NewOperatorWithClient(c2.Teamserver{}, client, msfpkg.WithPoll(0))
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

// decodePSCommand reverses the package's encodePSCommand (base64 of a
// UTF-16LE script) into plaintext, so tests can assert on the actual
// PowerShell content delivered via -EncodedCommand without duplicating the
// encoder under test.
func decodePSCommand(t *testing.T, encoded string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	if len(raw)%2 != 0 {
		t.Fatalf("decoded payload has odd byte length %d", len(raw))
	}
	u16 := make([]uint16, len(raw)/2)
	for i := range u16 {
		u16[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	return string(utf16.Decode(u16))
}

// TestOperator_Execute_NewCleanablePrimitives_RegBased covers the render path
// (Execute) for the 3 new reg-based cleanable primitives added alongside
// PrimScheduledTask: ifeo_injection, port_monitor, active_setup. Each should
// render as a plain `shell reg add ...` — no EncodedCommand wrapping needed
// since these are single well-formed reg.exe invocations.
func TestOperator_Execute_NewCleanablePrimitives_RegBased(t *testing.T) {
	tests := []struct {
		name     string
		attackID string
		valueTag string // the /v <name> reg value written
	}{
		{"ifeo_injection", "T1546.012", "Debugger"},
		{"port_monitor", "T1547.010", "Driver"},
		{"active_setup", "T1547.014", "StubPath"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prim, ok, err := ttp.Compile(domain.Technique{AttackID: tc.attackID})
			if err != nil || !ok {
				t.Fatalf("ttp.Compile(%s): ok=%v err=%v", tc.attackID, ok, err)
			}

			client := &FakeMsfClient{shellOutput: "ok"}
			op := msfpkg.NewOperatorWithClient(c2.Teamserver{}, client, msfpkg.WithPoll(0))
			res, err := op.Execute(context.Background(), "1", domain.Technique{AttackID: tc.attackID})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if res.Status != domain.ExecSuccess {
				t.Fatalf("status = %v, want success; output=%q", res.Status, res.Output)
			}

			cmd := client.lastCmd
			if !strings.HasPrefix(cmd, "shell reg add ") {
				t.Fatalf("cmd = %q, want %q prefix", cmd, "shell reg add ")
			}

			var wantKey, wantData string
			switch prim.Kind {
			case c2.PrimIFEOInjection:
				wantKey = `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Image File Execution Options\` + prim.Arg("target_image")
				wantData = prim.Arg("debugger")
			case c2.PrimPortMonitor:
				wantKey = `HKLM\SYSTEM\CurrentControlSet\Control\Print\Monitors\` + prim.Arg("monitor_name")
				wantData = prim.Arg("driver")
			case c2.PrimActiveSetup:
				wantKey = `HKLM\SOFTWARE\Microsoft\Active Setup\Installed Components\` + prim.Arg("component_id")
				wantData = prim.Arg("stub_path")
			default:
				t.Fatalf("unexpected primitive kind %q for attack id %s", prim.Kind, tc.attackID)
			}
			if !strings.Contains(cmd, wantKey) {
				t.Errorf("cmd missing key %q: %q", wantKey, cmd)
			}
			if !strings.Contains(cmd, "/v "+tc.valueTag) {
				t.Errorf("cmd missing value name %q: %q", tc.valueTag, cmd)
			}
			if !strings.Contains(cmd, wantData) {
				t.Errorf("cmd missing data %q: %q", wantData, cmd)
			}
		})
	}
}

// TestOperator_Revert_NewCleanablePrimitives_RegBased covers the cleanup path
// (Revert) for the 3 new reg-based cleanable primitives. port_monitor and
// active_setup delete the whole dedicated subkey (like scheduled_task deletes
// its whole task); ifeo_injection deletes only the Debugger value, since the
// IFEO subkey may legitimately pre-exist for other reasons.
func TestOperator_Revert_NewCleanablePrimitives_RegBased(t *testing.T) {
	tests := []struct {
		name         string
		attackID     string
		wantValueDel string // non-empty if only a single value (not the whole key) should be deleted
	}{
		{"ifeo_injection", "T1546.012", "Debugger"},
		{"port_monitor", "T1547.010", ""},
		{"active_setup", "T1547.014", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prim, ok, err := ttp.Compile(domain.Technique{AttackID: tc.attackID})
			if err != nil || !ok {
				t.Fatalf("ttp.Compile(%s): ok=%v err=%v", tc.attackID, ok, err)
			}

			client := &FakeMsfClient{shellOutput: "ok"}
			op := msfpkg.NewOperatorWithClient(c2.Teamserver{}, client, msfpkg.WithPoll(0))
			rev, ok := op.(c2.Reverter)
			if !ok {
				t.Fatal("metasploit operator should implement c2.Reverter")
			}
			res, err := rev.Revert(context.Background(), "1", domain.Technique{AttackID: tc.attackID})
			if err != nil {
				t.Fatalf("Revert: %v", err)
			}
			if res.Status != domain.ExecSuccess {
				t.Fatalf("status = %v, want success; output=%q", res.Status, res.Output)
			}

			cmd := client.lastCmd
			if !strings.HasPrefix(cmd, "shell reg delete ") {
				t.Fatalf("cmd = %q, want %q prefix", cmd, "shell reg delete ")
			}

			var wantKey string
			switch prim.Kind {
			case c2.PrimIFEOInjection:
				wantKey = `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Image File Execution Options\` + prim.Arg("target_image")
			case c2.PrimPortMonitor:
				wantKey = `HKLM\SYSTEM\CurrentControlSet\Control\Print\Monitors\` + prim.Arg("monitor_name")
			case c2.PrimActiveSetup:
				wantKey = `HKLM\SOFTWARE\Microsoft\Active Setup\Installed Components\` + prim.Arg("component_id")
			default:
				t.Fatalf("unexpected primitive kind %q for attack id %s", prim.Kind, tc.attackID)
			}
			if !strings.Contains(cmd, wantKey) {
				t.Errorf("cmd missing key %q: %q", wantKey, cmd)
			}
			if tc.wantValueDel != "" {
				if !strings.Contains(cmd, "/v "+tc.wantValueDel) {
					t.Errorf("cmd missing value delete %q: %q", tc.wantValueDel, cmd)
				}
			} else if strings.Contains(cmd, "/v ") {
				t.Errorf("%s revert should delete the whole subkey, not a single value: %q", tc.name, cmd)
			}
		})
	}
}

// TestOperator_Execute_NewCleanablePrimitives_EncodedCommand covers the
// render path for the 2 new primitives that need real multi-statement
// PowerShell (COM object / WMI cmdlets): shortcut_modification and
// wmi_event_subscription. Both should render as a `shell powershell -NoProfile
// -EncodedCommand <base64>` command (no nested quoting), so the test decodes
// the payload and asserts on the underlying script.
func TestOperator_Execute_NewCleanablePrimitives_EncodedCommand(t *testing.T) {
	tests := []struct {
		name      string
		attackID  string
		wantParts func(prim c2.Primitive) []string
	}{
		{
			name:     "shortcut_modification",
			attackID: "T1547.009",
			wantParts: func(prim c2.Primitive) []string {
				return []string{
					"CreateShortcut", prim.Arg("shortcut_path"), prim.Arg("target"), prim.Arg("arguments"),
				}
			},
		},
		{
			name:     "wmi_event_subscription",
			attackID: "T1546.003",
			wantParts: func(prim c2.Primitive) []string {
				return []string{
					"Set-WmiInstance", "__EventFilter", "CommandLineEventConsumer", "__FilterToConsumerBinding",
					prim.Arg("filter_name"), prim.Arg("consumer_name"), prim.Arg("command"),
				}
			},
		},
	}
	const prefix = "shell powershell -NoProfile -EncodedCommand "

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prim, ok, err := ttp.Compile(domain.Technique{AttackID: tc.attackID})
			if err != nil || !ok {
				t.Fatalf("ttp.Compile(%s): ok=%v err=%v", tc.attackID, ok, err)
			}

			client := &FakeMsfClient{shellOutput: "ok"}
			op := msfpkg.NewOperatorWithClient(c2.Teamserver{}, client, msfpkg.WithPoll(0))
			res, err := op.Execute(context.Background(), "1", domain.Technique{AttackID: tc.attackID})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if res.Status != domain.ExecSuccess {
				t.Fatalf("status = %v, want success; output=%q", res.Status, res.Output)
			}

			if !strings.HasPrefix(client.lastCmd, prefix) {
				t.Fatalf("cmd = %q, want prefix %q", client.lastCmd, prefix)
			}
			plaintext := decodePSCommand(t, strings.TrimPrefix(client.lastCmd, prefix))
			for _, want := range tc.wantParts(prim) {
				if want == "" {
					continue
				}
				if !strings.Contains(plaintext, want) {
					t.Errorf("decoded script missing %q: %q", want, plaintext)
				}
			}
		})
	}
}

// TestOperator_Revert_NewCleanablePrimitives_EncodedCommand covers the
// cleanup path for the 2 EncodedCommand-delivered primitives.
func TestOperator_Revert_NewCleanablePrimitives_EncodedCommand(t *testing.T) {
	tests := []struct {
		name      string
		attackID  string
		wantParts func(prim c2.Primitive) []string
	}{
		{
			name:     "shortcut_modification",
			attackID: "T1547.009",
			wantParts: func(prim c2.Primitive) []string {
				return []string{"Remove-Item", prim.Arg("shortcut_path")}
			},
		},
		{
			name:     "wmi_event_subscription",
			attackID: "T1546.003",
			wantParts: func(prim c2.Primitive) []string {
				return []string{
					"__FilterToConsumerBinding", "__EventFilter", "CommandLineEventConsumer",
					prim.Arg("filter_name"), prim.Arg("consumer_name"),
				}
			},
		},
	}
	const prefix = "shell powershell -NoProfile -EncodedCommand "

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prim, ok, err := ttp.Compile(domain.Technique{AttackID: tc.attackID})
			if err != nil || !ok {
				t.Fatalf("ttp.Compile(%s): ok=%v err=%v", tc.attackID, ok, err)
			}

			client := &FakeMsfClient{shellOutput: "ok"}
			op := msfpkg.NewOperatorWithClient(c2.Teamserver{}, client, msfpkg.WithPoll(0))
			rev, ok := op.(c2.Reverter)
			if !ok {
				t.Fatal("metasploit operator should implement c2.Reverter")
			}
			res, err := rev.Revert(context.Background(), "1", domain.Technique{AttackID: tc.attackID})
			if err != nil {
				t.Fatalf("Revert: %v", err)
			}
			if res.Status != domain.ExecSuccess {
				t.Fatalf("status = %v, want success; output=%q", res.Status, res.Output)
			}

			if !strings.HasPrefix(client.lastCmd, prefix) {
				t.Fatalf("cmd = %q, want prefix %q", client.lastCmd, prefix)
			}
			plaintext := decodePSCommand(t, strings.TrimPrefix(client.lastCmd, prefix))
			for _, want := range tc.wantParts(prim) {
				if want == "" {
					continue
				}
				if !strings.Contains(plaintext, want) {
					t.Errorf("decoded cleanup script missing %q: %q", want, plaintext)
				}
			}
		})
	}
}

// TestOperator_Revert_ScheduledTask_StillWorks pins the pre-existing
// PrimScheduledTask cleanup behavior (msfCleanupCommand's switch conversion
// must not change it) and confirms non-cleanable/unmapped techniques still
// report unsupported rather than a fabricated cleanup.
func TestOperator_Revert_ScheduledTask_StillWorks(t *testing.T) {
	client := &FakeMsfClient{shellOutput: "ok"}
	op := msfpkg.NewOperatorWithClient(c2.Teamserver{}, client, msfpkg.WithPoll(0))
	rev, ok := op.(c2.Reverter)
	if !ok {
		t.Fatal("metasploit operator should implement c2.Reverter")
	}
	ctx := context.Background()

	prim, ok, err := ttp.Compile(domain.Technique{AttackID: "T1053.005"})
	if err != nil || !ok {
		t.Fatalf("ttp.Compile(T1053.005): ok=%v err=%v", ok, err)
	}
	res, err := rev.Revert(ctx, "1", domain.Technique{AttackID: "T1053.005"})
	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if res.Status != domain.ExecSuccess {
		t.Fatalf("status = %v, want success", res.Status)
	}
	wantCmd := `shell schtasks /delete /tn "` + prim.Arg("task_name") + `" /f`
	if client.lastCmd != wantCmd {
		t.Errorf("cmd = %q, want %q", client.lastCmd, wantCmd)
	}

	// Non-cleanable technique -> unsupported, no fabricated cleanup.
	res, _ = rev.Revert(ctx, "1", domain.Technique{AttackID: "T1082"})
	if res.Status != domain.ExecUnsupported {
		t.Errorf("non-cleanable Revert: want unsupported, got %v", res.Status)
	}
}
