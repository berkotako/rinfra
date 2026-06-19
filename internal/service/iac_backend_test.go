package service_test

import (
	"context"
	"testing"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration"
	"github.com/rinfra/rinfra/internal/service"
	"github.com/rinfra/rinfra/internal/store/memstore"
)

// fakeProvisioner is a no-op Provisioner for backend-selection tests.
type fakeProvisioner struct{}

func (fakeProvisioner) Deploy(context.Context, string, []domain.Node, map[domain.CloudProviderType]cloud.Credentials) ([]orchestration.NodeResult, error) {
	return nil, nil
}
func (fakeProvisioner) Teardown(context.Context, string, []domain.Node, map[domain.CloudProviderType]cloud.Credentials) error {
	return nil
}

func TestIaCBackendSelection(t *testing.T) {
	s := newTestStores()
	svc := buildInfraService(t, s, service.NewHub())
	svc.RegisterProvisioner(service.BackendPulumi, fakeProvisioner{})
	svc.RegisterProvisioner(service.BackendTerraform, fakeProvisioner{})
	settings := memstore.NewSettingStore()
	svc.WithSettings(settings, service.BackendPulumi)

	ctx := context.Background()

	if got := svc.IaCBackend(ctx); got != service.BackendPulumi {
		t.Fatalf("default backend = %q, want pulumi", got)
	}
	if avail := svc.AvailableBackends(); len(avail) != 2 || avail[0] != service.BackendPulumi {
		t.Fatalf("available = %v, want [pulumi terraform]", avail)
	}

	// Switch to terraform; it must persist.
	if err := svc.SetIaCBackend(ctx, "admin", service.BackendTerraform); err != nil {
		t.Fatalf("SetIaCBackend: %v", err)
	}
	if got := svc.IaCBackend(ctx); got != service.BackendTerraform {
		t.Errorf("after switch backend = %q, want terraform", got)
	}

	// Unknown backend is rejected.
	if err := svc.SetIaCBackend(ctx, "admin", "bogus"); err == nil {
		t.Error("expected error selecting an unregistered backend")
	}
}
