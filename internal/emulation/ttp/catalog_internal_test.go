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
		c2.PrimRemoteSystemDiscovery, c2.PrimAccountDiscovery,
		c2.PrimPermissionGroupDiscovery, c2.PrimServiceDiscovery, c2.PrimShareDiscovery,
	}
	for _, k := range all {
		if !validPrimitives[k] {
			t.Errorf("primitive %q is missing from validPrimitives (catalog entries using it would panic at startup)", k)
		}
	}
}
