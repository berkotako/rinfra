package emulation

import (
	"testing"

	"github.com/rinfra/rinfra/internal/domain"
)

func TestFactStore_AddDedupAndResolve(t *testing.T) {
	f := NewFactStore()
	f.Add("host.ip", "10.0.0.5")
	f.Add("host.ip", "10.0.0.5") // dup ignored
	f.Add("host.ip", "10.0.0.6")
	if got := f.Values("host.ip"); len(got) != 2 {
		t.Fatalf("want 2 values, got %v", got)
	}

	out, missing := f.Resolve("ping ${host.ip}")
	if len(missing) != 0 {
		t.Errorf("unexpected missing: %v", missing)
	}
	if out != "ping 10.0.0.5" { // first value wins
		t.Errorf("resolve = %q", out)
	}

	out, missing = f.Resolve("connect ${host.user.name}")
	if len(missing) != 1 || missing[0] != "host.user.name" {
		t.Errorf("want missing [host.user.name], got %v", missing)
	}
	if out != "connect ${host.user.name}" {
		t.Errorf("unresolved token should remain, got %q", out)
	}
}

func TestPlanner_Prepare_RequirementGate(t *testing.T) {
	p := NewPlanner()
	tech := domain.Technique{AttackID: "T1059.001", Requires: []string{"host.ip"}}

	// No fact yet → skip as not-run.
	if _, skip, reason := p.Prepare(tech); !skip || reason == "" {
		t.Fatalf("expected skip with reason, got skip=%v reason=%q", skip, reason)
	}

	// Collect the fact → no longer skipped.
	p.Facts.Add("host.ip", "192.168.1.10")
	if _, skip, _ := p.Prepare(tech); skip {
		t.Fatal("should not skip once requirement is satisfied")
	}
}

func TestPlanner_Prepare_InputSubstitution(t *testing.T) {
	p := NewPlanner()
	p.Facts.Add("host.ip", "192.168.1.10")
	tech := domain.Technique{
		AttackID: "T1059.001",
		Inputs:   map[string]string{"command": "Test-Connection ${host.ip}"},
	}
	prepared, skip, _ := p.Prepare(tech)
	if skip {
		t.Fatal("should not skip with the fact present")
	}
	if got := prepared.Inputs["command"]; got != "Test-Connection 192.168.1.10" {
		t.Errorf("substituted input = %q", got)
	}
	// Original technique must be untouched (Prepare returns a copy of Inputs).
	if tech.Inputs["command"] != "Test-Connection ${host.ip}" {
		t.Error("Prepare mutated the caller's technique inputs")
	}
}

func TestPlanner_Prepare_UnresolvedInputSkips(t *testing.T) {
	p := NewPlanner()
	tech := domain.Technique{
		AttackID: "T1059.001",
		Inputs:   map[string]string{"command": "echo ${host.ip}"},
	}
	if _, skip, reason := p.Prepare(tech); !skip || reason == "" {
		t.Fatalf("unresolved ${host.ip} should skip, got skip=%v", skip)
	}
}

func TestPlanner_Observe_ParsesIPsFromNetConfig(t *testing.T) {
	p := NewPlanner()
	// T1016 → net_config primitive. A successful result with ipconfig-like
	// output should yield routable host.ip facts and drop loopback/unspecified.
	res := domain.Result{
		TechniqueAttackID: "T1016",
		Status:            domain.ExecSuccess,
		Output:            "IPv4 Address. . . : 10.1.2.3\nDefault Gateway . : 10.1.2.1\nLoopback: 127.0.0.1\n",
	}
	p.Observe(domain.Technique{AttackID: "T1016"}, res)

	got := p.Facts.Values("host.ip")
	want := map[string]bool{"10.1.2.3": true, "10.1.2.1": true}
	if len(got) != 2 {
		t.Fatalf("want 2 routable IPs, got %v", got)
	}
	for _, ip := range got {
		if !want[ip] {
			t.Errorf("unexpected ip parsed: %s", ip)
		}
	}
}

func TestPlanner_Observe_IgnoresNonSuccess(t *testing.T) {
	p := NewPlanner()
	p.Observe(domain.Technique{AttackID: "T1016"}, domain.Result{
		TechniqueAttackID: "T1016",
		Status:            domain.ExecFailed,
		Output:            "10.1.2.3",
	})
	if p.Facts.Has("host.ip") {
		t.Error("facts should not be parsed from a failed technique")
	}
}
