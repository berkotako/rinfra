package gcp

import (
	"testing"

	"github.com/rinfra/rinfra/internal/domain"
)

// TestDestroy_ReleasesNodeAddress verifies node-scoped Destroy also releases the
// node's dedicated reserved Address (not just the instance), so a single-node
// rollback does not orphan the billed static IP.
func TestDestroy_ReleasesNodeAddress(t *testing.T) {
	p, f := newFakeGCP(t, nil)
	node := domain.Node{
		ID:           "node-1",
		EngagementID: "eng-1",
		ProviderRef:  "rinfra-eng-1-node-1",
		Spec:         domain.NodeSpec{Region: "us-central1-a"},
	}
	if err := p.Destroy(t.Context(), gcpTestCreds(), node); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if !f.hit("DELETE", "/zones/us-central1-a/instances/rinfra-eng-1-node-1") {
		t.Error("expected the instance to be deleted")
	}
	if !f.hit("DELETE", "/regions/us-central1/addresses/rinfra-eng-1-node-1-ip") {
		t.Error("expected the node's reserved address to be released")
	}
}
