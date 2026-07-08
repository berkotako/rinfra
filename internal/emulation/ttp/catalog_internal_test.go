package ttp

import (
	"testing"

	"github.com/rinfra/rinfra/internal/c2"
)

// TestValidPrimitives_CoversAllKinds asserts that every c2.PrimitiveKind constant
// is present in validPrimitives. validPrimitives is a MANUAL mirror of the
// constants (the catalog loader rejects any entry naming a kind not in it), so a
// new primitive added to primitive.go but forgotten here would make its catalog
// entries panic at startup. This test fails loudly instead.
func TestValidPrimitives_CoversAllKinds(t *testing.T) {
	all := []c2.PrimitiveKind{
		c2.PrimPowerShell, c2.PrimShell, c2.PrimSysInfo, c2.PrimProcessList,
		c2.PrimNetConnections, c2.PrimNetConfig, c2.PrimFileList, c2.PrimDownload,
		c2.PrimScheduledTask, c2.PrimRegistryRunKey,
		c2.PrimShortcutModification, c2.PrimWMIEventSubscription, c2.PrimIFEOInjection,
		c2.PrimPortMonitor, c2.PrimActiveSetup,
		c2.PrimRemoteSystemDiscovery, c2.PrimAccountDiscovery,
		c2.PrimPermissionGroupDiscovery, c2.PrimServiceDiscovery, c2.PrimShareDiscovery,
	}
	for _, k := range all {
		if !validPrimitives[k] {
			t.Errorf("primitive %q is missing from validPrimitives (catalog entries using it would panic at startup)", k)
		}
	}
}

// TestEveryEntryHasValidRisk guards the danger tag: the loader rejects an empty
// or unknown risk, so a successful load already proves every entry is tagged —
// this asserts the accessor returns a valid level for every mapped ID (and that
// the tag is genuinely populated, not silently defaulted).
func TestEveryEntryHasValidRisk(t *testing.T) {
	c := Default()
	for _, id := range c.AttackIDs() {
		risk, ok := c.Risk(id)
		if !ok {
			t.Errorf("%s: Risk() ok=false for a mapped ID", id)
			continue
		}
		if !validRisks[risk] {
			t.Errorf("%s: risk %q not in {safe,caution,dangerous}", id, risk)
		}
	}
	if _, ok := c.Risk("T9999.999"); ok {
		t.Error("Risk() should report ok=false for an unmapped ID")
	}
}

// TestRiskTags_PanelSpotChecks pins a few panel verdicts so a careless future
// edit that flips a sensitive technique to "safe" is caught. Cloud-IAM/metadata
// and plaintext-secret exposure and state-changing persistence primitives are
// dangerous; a bare whoami is safe.
func TestRiskTags_PanelSpotChecks(t *testing.T) {
	want := map[string]string{
		"T1059.001": RiskSafe,      // whoami via PowerShell
		"T1082":     RiskSafe,      // sysinfo
		"T1552.005": RiskDangerous, // cloud instance metadata API (IAM creds)
		"T1552.001": RiskDangerous, // grep user files for passwords
		"T1558.003": RiskDangerous, // Kerberoasting
		"T1547.001": RiskDangerous, // registry_run_key primitive (state-changing)
		"T1053.005": RiskDangerous, // scheduled_task primitive (state-changing)
		"T1046":     RiskCaution,   // network service discovery
	}
	c := Default()
	for id, exp := range want {
		got, ok := c.Risk(id)
		if !ok {
			t.Errorf("%s: not in catalog", id)
			continue
		}
		if got != exp {
			t.Errorf("%s: risk = %q, want %q", id, got, exp)
		}
	}
}
