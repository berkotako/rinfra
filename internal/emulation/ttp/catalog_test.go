package ttp_test

import (
	"testing"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/emulation/ttp"
)

func TestCompile_NoArgsPrimitive(t *testing.T) {
	prim, ok, err := ttp.Compile(domain.Technique{AttackID: "T1082"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !ok {
		t.Fatal("expected T1082 to have a catalog mapping")
	}
	if prim.Kind != c2.PrimSysInfo {
		t.Errorf("kind = %q, want %q", prim.Kind, c2.PrimSysInfo)
	}
	if len(prim.Args) != 0 {
		t.Errorf("args = %v, want empty", prim.Args)
	}
}

func TestCompile_DefaultAndInputBinding(t *testing.T) {
	// Default applies when the input is absent.
	prim, _, err := ttp.Compile(domain.Technique{AttackID: "T1059.001"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if prim.Kind != c2.PrimPowerShell || prim.Arg("script") != "whoami" {
		t.Errorf("got kind=%q script=%q, want powershell/whoami", prim.Kind, prim.Arg("script"))
	}

	// A supplied input overrides the default.
	prim, _, err = ttp.Compile(domain.Technique{
		AttackID: "T1059.001",
		Inputs:   map[string]string{"command": "Get-Process"},
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if prim.Arg("script") != "Get-Process" {
		t.Errorf("script = %q, want Get-Process", prim.Arg("script"))
	}
}

func TestCompile_RequiredInputMissing(t *testing.T) {
	// T1005 (download) requires a path; absent → found but error.
	_, ok, err := ttp.Compile(domain.Technique{AttackID: "T1005"})
	if !ok {
		t.Fatal("expected T1005 to have a catalog mapping")
	}
	if err == nil {
		t.Fatal("expected an error for missing required input 'path'")
	}

	prim, _, err := ttp.Compile(domain.Technique{
		AttackID: "T1005",
		Inputs:   map[string]string{"path": "C:/loot.txt"},
	})
	if err != nil {
		t.Fatalf("Compile with path: %v", err)
	}
	if prim.Kind != c2.PrimDownload || prim.Arg("path") != "C:/loot.txt" {
		t.Errorf("got kind=%q path=%q, want download/C:/loot.txt", prim.Kind, prim.Arg("path"))
	}
}

func TestCompile_UnknownTechnique(t *testing.T) {
	prim, ok, err := ttp.Compile(domain.Technique{AttackID: "T9999.999"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if ok {
		t.Errorf("expected no mapping for unknown technique, got %v", prim)
	}
}

func TestDefaultCatalog_LoadedAndConsistent(t *testing.T) {
	c := ttp.Default()
	if c == nil {
		t.Fatal("Default() returned nil")
	}
	ids := c.AttackIDs()
	if len(ids) == 0 {
		t.Fatal("catalog has no entries")
	}
	for _, id := range ids {
		if !c.Has(id) {
			t.Errorf("AttackIDs reported %q but Has() says no", id)
		}
	}
	if c.Has("nope") {
		t.Error("Has() should be false for an unmapped ID")
	}
}
