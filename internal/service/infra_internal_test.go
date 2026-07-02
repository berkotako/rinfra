package service

import (
	"context"
	"testing"

	"github.com/rinfra/rinfra/internal/cloud"
	_ "github.com/rinfra/rinfra/internal/cloud/digitalocean" // registers a ProgramBuilder cloud
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration"
)

// nilProvisioner is a non-nil Provisioner stub so desiredEngineNodes runs its
// routing logic (it only checks activeProv != nil; Deploy/Teardown aren't called).
type nilProvisioner struct{}

func (nilProvisioner) Deploy(context.Context, string, []domain.Node, map[domain.CloudProviderType]cloud.Credentials) ([]orchestration.NodeResult, error) {
	return nil, nil
}

func (nilProvisioner) Teardown(context.Context, string, []domain.Node, map[domain.CloudProviderType]cloud.Credentials) error {
	return nil
}

func TestParseListenerPort(t *testing.T) {
	cases := []struct {
		in       string
		wantPort int
		wantOK   bool
	}{
		{"0.0.0.0:443", 443, true},
		{":8443", 8443, true},
		{"[::1]:443", 443, true},
		{"https://host:8080/beacon", 8080, true},
		{"host:80/path", 80, true},
		{"", 0, false},
		{"nohostport", 0, false},
		{"host:0", 0, false},
		{"host:99999", 0, false},
	}
	for _, c := range cases {
		got, ok := parseListenerPort(c.in)
		if got != c.wantPort || ok != c.wantOK {
			t.Errorf("parseListenerPort(%q) = (%d,%v), want (%d,%v)", c.in, got, ok, c.wantPort, c.wantOK)
		}
	}
}

func TestNodeIngressRules(t *testing.T) {
	has := func(rules []domain.Rule, port int) bool {
		for _, r := range rules {
			if r.Port == port && r.Allow {
				return true
			}
		}
		return false
	}

	redir := nodeIngressRules(domain.Node{Spec: domain.NodeSpec{Type: domain.NodeRedirector}})
	for _, p := range []int{22, 80, 443} {
		if !has(redir, p) {
			t.Errorf("redirector rules missing port %d", p)
		}
	}

	c2Default := nodeIngressRules(domain.Node{Spec: domain.NodeSpec{Type: domain.NodeC2Server}})
	if !has(c2Default, 22) || !has(c2Default, 443) {
		t.Error("c2 default rules must open 22 and 443")
	}

	c2Listener := nodeIngressRules(domain.Node{
		Spec:   domain.NodeSpec{Type: domain.NodeC2Server},
		Canvas: domain.NodeCanvas{Listener: "0.0.0.0:8443"},
	})
	if !has(c2Listener, 8443) {
		t.Error("c2 rules must open the listener port 8443")
	}
	if has(c2Listener, 443) {
		t.Error("c2 with an explicit listener should not also default to 443")
	}
}

func TestDesiredEngineNodes(t *testing.T) {
	do := domain.CloudDigitalOcean
	nodes := []domain.Node{
		{ID: "pending", Spec: domain.NodeSpec{Cloud: do}, Status: domain.NodePending},
		{ID: "live", Spec: domain.NodeSpec{Cloud: do}, Status: domain.NodeLive},
		{ID: "destroyed", Spec: domain.NodeSpec{Cloud: do}, Status: domain.NodeDestroyed},
		{ID: "failed", Spec: domain.NodeSpec{Cloud: do}, Status: domain.NodeFailed},
	}
	credsMap := map[domain.CloudProviderType]cloud.Credentials{do: {Provider: do}}
	pendingIDs := map[string]bool{"pending": true}

	got := desiredEngineNodes(nodes, nilProvisioner{}, credsMap, pendingIDs)
	ids := map[string]bool{}
	for _, n := range got {
		ids[n.ID] = true
	}
	// pending + live are desired; destroyed + failed are excluded so Pulumi
	// neither recreates them nor is blocked by them — but live is KEPT so a
	// re-deploy does not destroy it.
	if !ids["pending"] || !ids["live"] {
		t.Errorf("desired set must include pending and live, got %v", ids)
	}
	if ids["destroyed"] || ids["failed"] {
		t.Errorf("desired set must exclude destroyed/failed, got %v", ids)
	}

	// No creds for the provider → nothing is sent (avoids a Deploy that would
	// return errors for already-live nodes and wrongly fail them).
	if out := desiredEngineNodes(nodes, nilProvisioner{}, map[domain.CloudProviderType]cloud.Credentials{}, pendingIDs); len(out) != 0 {
		t.Errorf("with no creds, desired set must be empty, got %d", len(out))
	}
}
