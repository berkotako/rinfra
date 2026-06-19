package index_test

import (
	"strings"
	"testing"

	"github.com/rinfra/rinfra/internal/emulation/index"
)

const sample = `
metadata:
  prefix: SRA
  bundle:
    name: ATT&CK Index 2026
    version: 1.0.0
Defense Evasion:
  - name: Disable Windows Firewall
    description: Disable the Windows Firewall using netsh.exe
    platforms: [windows]
    guidance:
      - "CMD> netsh advfirewall set allprofiles state off"
    block: ["blocked by EDR"]
    detect: ["detected by EDR"]
    metadata:
      id: fa619a73-4ba1-4816-ada9-4504cd814908
      tid: T1562.004
      tactic: TA0005
Credential Access:
  - name: LSASS Memory
    description: Dump LSASS
    guidance:
      - "PS> rundll32 comsvcs.dll, MiniDump 640 l.dmp full"
    metadata:
      id: 11111111-1111-1111-1111-111111111111
      tid: T1003.001
      tactic: TA0006
  - name: Missing TID is skipped
    metadata:
      tid: ""
`

func TestParse(t *testing.T) {
	sc, techs, err := index.Parse([]byte(sample))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if sc.Name != "ATT&CK Index 2026" {
		t.Errorf("scenario name = %q, want bundle name", sc.Name)
	}
	if len(techs) != 2 {
		t.Fatalf("techniques = %d, want 2 (the empty-tid case is skipped)", len(techs))
	}
	// Document order preserved: Defense Evasion first.
	if techs[0].AttackID != "T1562.004" || techs[0].Tactic != "Defense Evasion" {
		t.Errorf("first technique = %+v", techs[0])
	}
	if techs[0].Name != "Disable Windows Firewall" || techs[0].Description == "" {
		t.Errorf("first technique fields = %+v", techs[0])
	}
	// Guidance prefix stripped.
	if len(techs[0].Commands) != 1 || strings.HasPrefix(techs[0].Commands[0], "CMD>") {
		t.Errorf("commands not cleaned: %v", techs[0].Commands)
	}
	if techs[1].AttackID != "T1003.001" || techs[1].Tactic != "Credential Access" {
		t.Errorf("second technique = %+v", techs[1])
	}
}

func TestParse_Errors(t *testing.T) {
	if _, _, err := index.Parse([]byte("just a string")); err == nil {
		t.Error("expected error for non-mapping document")
	}
	if _, _, err := index.Parse([]byte("metadata:\n  prefix: X\n")); err == nil {
		t.Error("expected error for index with no test cases")
	}
	if _, _, err := index.Parse([]byte(": : bad")); err == nil {
		t.Error("expected error for malformed yaml")
	}
}
