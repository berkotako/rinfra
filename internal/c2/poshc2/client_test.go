package poshc2

import (
	"context"
	"strings"
	"testing"
)

// cannedRunner is a minimal deploy.Runner fake: it records commands and returns
// canned stdout per call (in order), letting the CLI client tests assert both
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

func TestCLIClient_Implants_ParsesOutput(t *testing.T) {
	runner := &cannedRunner{outputs: []string{
		"ID | Hostname | Username\n" +
			"-------------------------\n" +
			"1 | WIN-01 | DOMAIN\\admin\n" +
			"2 | WIN-02 | svc_sql\n",
	}}
	c := newCLIPoshC2Client(runner)

	implants, err := c.Implants(context.Background())
	if err != nil {
		t.Fatalf("Implants: %v", err)
	}
	if len(implants) != 2 {
		t.Fatalf("expected 2 implants, got %d: %+v", len(implants), implants)
	}
	want := PoshC2Implant{ID: "1", Hostname: "WIN-01", Username: "DOMAIN\\admin"}
	if implants[0] != want {
		t.Errorf("implant[0] = %+v, want %+v", implants[0], want)
	}
	if implants[1].ID != "2" || implants[1].Hostname != "WIN-02" {
		t.Errorf("implant[1] = %+v", implants[1])
	}

	cmds := runner.Commands()
	if len(cmds) != 1 || !strings.Contains(cmds[0], "--list-implants") {
		t.Errorf("expected a `--list-implants` command, got %v", cmds)
	}
}

func TestCLIClient_Execute_ReturnsOutput(t *testing.T) {
	runner := &cannedRunner{outputs: []string{"whoami output\n"}}
	c := newCLIPoshC2Client(runner)

	out, err := c.Execute(context.Background(), "1", "whoami")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "whoami output") {
		t.Errorf("unexpected output: %q", out)
	}

	cmds := runner.Commands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %v", cmds)
	}
	if !strings.Contains(cmds[0], "poshc2 -i '1' -c 'whoami'") {
		t.Errorf("exec command did not target the implant-handler CLI: %q", cmds[0])
	}
}

func TestCLIClient_StartListener_IssuesCommand(t *testing.T) {
	runner := &cannedRunner{}
	c := newCLIPoshC2Client(runner)

	if err := c.StartListener(context.Background(), "https", 443); err != nil {
		t.Fatalf("StartListener: %v", err)
	}

	cmds := runner.Commands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %v", cmds)
	}
	for _, want := range []string{"--create-listener", "--type https", "--port 443"} {
		if !strings.Contains(cmds[0], want) {
			t.Errorf("StartListener command missing %q: %q", want, cmds[0])
		}
	}
}
