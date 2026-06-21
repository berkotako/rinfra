package havoc

import (
	"context"
	"strings"
	"testing"
)

// cannedRunner is a minimal deploy.Runner fake: it records commands and returns
// canned stdout per call (in order). It lets the CLI client tests assert both
// the issued command strings and the parsing of framework output.
type cannedRunner struct {
	outputs []string
	cmds    []string
}

func (r *cannedRunner) Run(_ context.Context, cmd string) (string, error) {
	r.cmds = append(r.cmds, cmd)
	if len(r.outputs) == 0 {
		return "", nil
	}
	out := r.outputs[0]
	r.outputs = r.outputs[1:]
	return out, nil
}

func (r *cannedRunner) Upload(_ context.Context, _, _ string) error { return nil }

func (r *cannedRunner) Commands() []string { return r.cmds }

func TestCLIClient_Sessions_ParsesOutput(t *testing.T) {
	runner := &cannedRunner{outputs: []string{
		"ID | Hostname | Username | OS | Arch\n" +
			"------------------------------------\n" +
			"d1 | HOST01 | admin | windows | x64\n" +
			"d2 | HOST02 | svc | windows | x86\n",
	}}
	c := newCLIHavocClient(runner, "203.0.113.30", havocPort)

	sessions, err := c.Sessions(context.Background())
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d: %+v", len(sessions), sessions)
	}
	got := sessions[0]
	want := HavocSession{ID: "d1", Hostname: "HOST01", Username: "admin", OS: "windows", Arch: "x64"}
	if got != want {
		t.Errorf("session[0] = %+v, want %+v", got, want)
	}
	if sessions[1].ID != "d2" || sessions[1].Arch != "x86" {
		t.Errorf("session[1] = %+v", sessions[1])
	}

	cmds := runner.Commands()
	if len(cmds) != 1 || !strings.Contains(cmds[0], "demon list") {
		t.Errorf("expected a `demon list` command, got %v", cmds)
	}
}

func TestCLIClient_Execute_ReturnsOutput(t *testing.T) {
	runner := &cannedRunner{outputs: []string{"NT AUTHORITY\\SYSTEM\n"}}
	c := newCLIHavocClient(runner, "host", 0) // port 0 -> default havocPort

	out, err := c.Execute(context.Background(), "d1", "whoami")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "NT AUTHORITY\\SYSTEM") {
		t.Errorf("unexpected output: %q", out)
	}

	cmds := runner.Commands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %v", cmds)
	}
	if !strings.Contains(cmds[0], "demon 'd1' exec whoami") {
		t.Errorf("exec command did not target the demon CLI: %q", cmds[0])
	}
}

func TestCLIClient_StartListener_IssuesCommand(t *testing.T) {
	runner := &cannedRunner{}
	c := newCLIHavocClient(runner, "host", 40056)

	if err := c.StartListener(context.Background(), "https", "0.0.0.0", 443); err != nil {
		t.Fatalf("StartListener: %v", err)
	}

	cmds := runner.Commands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %v", cmds)
	}
	for _, want := range []string{"listener add", "--protocol https", "--port 443"} {
		if !strings.Contains(cmds[0], want) {
			t.Errorf("StartListener command missing %q: %q", want, cmds[0])
		}
	}
}
