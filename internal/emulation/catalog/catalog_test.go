package catalog_test

import (
	"testing"

	"github.com/rinfra/rinfra/internal/emulation/catalog"
)

// recognizedTactics is the allow-list of valid ATT&CK tactic slugs.
var recognizedTactics = map[string]bool{
	"reconnaissance":       true,
	"resource-development": true,
	"initial-access":       true,
	"execution":            true,
	"persistence":          true,
	"privilege-escalation": true,
	"defense-evasion":      true,
	"credential-access":    true,
	"discovery":            true,
	"lateral-movement":     true,
	"collection":           true,
	"command-and-control":  true,
	"exfiltration":         true,
	"impact":               true,
}

// TestCatalogLoaded verifies that the YAML-based catalog loads successfully and
// contains the required scenario IDs (stable for the web app).
func TestCatalogLoaded(t *testing.T) {
	required := []string{"apt29", "fin7", "ransom"}
	for _, id := range required {
		sc, ok := catalog.Get(id)
		if !ok {
			t.Errorf("scenario %q: not found in catalog", id)
			continue
		}
		if sc.ID != id {
			t.Errorf("scenario %q: ID mismatch, got %q", id, sc.ID)
		}
		if sc.Name == "" {
			t.Errorf("scenario %q: empty name", id)
		}
		if len(sc.Techniques) == 0 {
			t.Errorf("scenario %q: no techniques", id)
		}
	}
}

// TestCatalogTechniqueFields validates every loaded technique has a non-empty
// AttackID, SourceID, and a recognized ATT&CK tactic. This enforces the
// payload-free posture: techniques are references, not procedures, and the
// reference fields must be populated.
func TestCatalogTechniqueFields(t *testing.T) {
	for _, sc := range catalog.List() {
		for i, tech := range sc.Techniques {
			prefix := func() string {
				return "scenario " + sc.ID + " technique[" + tech.AttackID + "]"
			}

			if tech.AttackID == "" {
				t.Errorf("%s[%d]: empty AttackID", sc.ID, i)
			}
			if tech.SourceID == "" {
				t.Errorf("%s: empty SourceID", prefix())
			}
			if tech.Name == "" {
				t.Errorf("%s: empty Name", prefix())
			}
			if !recognizedTactics[tech.Tactic] {
				t.Errorf("%s: unrecognized tactic %q", prefix(), tech.Tactic)
			}
			if tech.Source != "atomic_red_team" && tech.Source != "caldera" {
				t.Errorf("%s: unrecognized source %q (must be atomic_red_team or caldera)", prefix(), tech.Source)
			}
		}
	}
}

// TestCatalogList verifies that List() returns all scenarios and is stable.
func TestCatalogList(t *testing.T) {
	all := catalog.List()
	if len(all) < 3 {
		t.Errorf("expected at least 3 scenarios, got %d", len(all))
	}

	// Verify ordering is stable (alphabetical by ID).
	for i := 1; i < len(all); i++ {
		if all[i].ID < all[i-1].ID {
			t.Errorf("catalog.List() not sorted: %q before %q", all[i-1].ID, all[i].ID)
		}
	}
}

// TestNoPayloadContent verifies that no technique carries payload content. In
// practice this means the Inputs map (if present) must not contain keys that
// suggest payload material.
func TestNoPayloadContent(t *testing.T) {
	forbidden := []string{
		"shellcode", "payload", "exploit", "encoder",
		"msfvenom", "metasploit_module",
	}
	for _, sc := range catalog.List() {
		for _, tech := range sc.Techniques {
			for k := range tech.Inputs {
				for _, bad := range forbidden {
					if k == bad {
						t.Errorf("scenario %s technique %s: forbidden input key %q", sc.ID, tech.AttackID, k)
					}
				}
			}
		}
	}
}
