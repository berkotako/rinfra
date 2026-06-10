package orchestration

import (
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
)

// TestStackName verifies the stack naming convention.
func TestStackName(t *testing.T) {
	id := "abc123"
	got := stackName(id)
	want := "rinfra-abc123"
	if got != want {
		t.Errorf("stackName(%q) = %q, want %q", id, got, want)
	}
}

// TestNodeOutputKeys verifies that output key helpers are stable.
func TestNodeOutputKeys(t *testing.T) {
	nodeID := "my-node"
	if got, want := NodeProviderRefKey(nodeID), "providerRef:my-node"; got != want {
		t.Errorf("NodeProviderRefKey = %q, want %q", got, want)
	}
	if got, want := NodePublicIPKey(nodeID), "publicIP:my-node"; got != want {
		t.Errorf("NodePublicIPKey = %q, want %q", got, want)
	}
}

// TestBuildEnvVars verifies that credential keys and the backend URL are merged.
func TestBuildEnvVars(t *testing.T) {
	eng := New("/tmp/test-state", nil)
	creds := cloud.Credentials{
		Provider: domain.CloudDigitalOcean,
		Raw: map[string]string{
			"DIGITALOCEAN_TOKEN": "tok-secret",
		},
	}
	env := eng.buildEnvVars(creds)

	if env["DIGITALOCEAN_TOKEN"] != "tok-secret" {
		t.Error("credential key not propagated to env vars")
	}
	if env["PULUMI_BACKEND_URL"] == "" {
		t.Error("PULUMI_BACKEND_URL not set in env vars")
	}
	if env["PULUMI_BACKEND_URL"] != "file:///tmp/test-state" {
		t.Errorf("unexpected PULUMI_BACKEND_URL: %q", env["PULUMI_BACKEND_URL"])
	}
}

// TestGroupByProvider verifies that nodes are correctly partitioned by cloud.
func TestGroupByProvider(t *testing.T) {
	nodes := []domain.Node{
		{ID: "n1", Spec: domain.NodeSpec{Cloud: domain.CloudDigitalOcean}},
		{ID: "n2", Spec: domain.NodeSpec{Cloud: domain.CloudAWS}},
		{ID: "n3", Spec: domain.NodeSpec{Cloud: domain.CloudDigitalOcean}},
	}
	grouped := groupByProvider(nodes)

	if len(grouped[domain.CloudDigitalOcean]) != 2 {
		t.Errorf("expected 2 DO nodes, got %d", len(grouped[domain.CloudDigitalOcean]))
	}
	if len(grouped[domain.CloudAWS]) != 1 {
		t.Errorf("expected 1 AWS node, got %d", len(grouped[domain.CloudAWS]))
	}
}

// TestRegisterBuilderPanic verifies that duplicate builder registration panics.
func TestRegisterBuilderPanic(t *testing.T) {
	eng := New("/tmp/test-state", nil)
	b := &stubBuilder{}
	eng.RegisterBuilder(domain.CloudProviderType("test-only-builder"), b)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate builder registration")
		}
	}()
	eng.RegisterBuilder(domain.CloudProviderType("test-only-builder"), b)
}

// stubBuilder is a minimal ProgramBuilder used for registration tests.
type stubBuilder struct{}

func (s *stubBuilder) BuildProgram(_ string, _ cloud.Credentials, _ []domain.Node) pulumi.RunFunc {
	return nil
}
