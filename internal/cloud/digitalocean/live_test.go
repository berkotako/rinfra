package digitalocean

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
)

// fakeDO is an httptest-backed stand-in for the DigitalOcean API. It records the
// method+path of every request so tests can assert which deletes/creates fired,
// and serves canned JSON for the endpoints godo calls.
type fakeDO struct {
	mu   sync.Mutex
	hits []string
}

func (f *fakeDO) record(r *http.Request) {
	f.mu.Lock()
	f.hits = append(f.hits, r.Method+" "+r.URL.Path)
	f.mu.Unlock()
}

func (f *fakeDO) hit(method, path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, h := range f.hits {
		if h == method+" "+path {
			return true
		}
	}
	return false
}

func newFakeDO(t *testing.T) (*provider, *fakeDO) {
	t.Helper()
	f := &fakeDO{}
	mux := http.NewServeMux()

	// Droplets: list-by-tag returns one droplet; delete succeeds.
	mux.HandleFunc("GET /v2/droplets", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		w.Write([]byte(`{"droplets":[{"id":12345,"name":"rinfra"}],"links":{},"meta":{"total":1}}`))
	})
	mux.HandleFunc("DELETE /v2/droplets/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		w.WriteHeader(http.StatusNoContent)
	})
	// Firewalls: list returns one tagged FW; create/update/delete succeed.
	mux.HandleFunc("GET /v2/firewalls", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		w.Write([]byte(`{"firewalls":[{"id":"fw-1","name":"old","tags":["rinfra:eng-1"]}],"links":{},"meta":{"total":1}}`))
	})
	mux.HandleFunc("POST /v2/firewalls", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"firewall":{"id":"fw-new","name":"rinfra-fw"}}`))
	})
	mux.HandleFunc("DELETE /v2/firewalls/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		w.WriteHeader(http.StatusNoContent)
	})
	// Reserved IPs: list returns one assigned to the swept droplet; create/delete succeed.
	mux.HandleFunc("GET /v2/reserved_ips", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		w.Write([]byte(`{"reserved_ips":[{"ip":"203.0.113.9","droplet":{"id":12345}}],"links":{},"meta":{"total":1}}`))
	})
	mux.HandleFunc("POST /v2/reserved_ips", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"reserved_ip":{"ip":"203.0.113.50"}}`))
	})
	mux.HandleFunc("DELETE /v2/reserved_ips/{ip}", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		w.WriteHeader(http.StatusNoContent)
	})
	// Domain records: list has no match; create succeeds.
	mux.HandleFunc("GET /v2/domains/{zone}/records", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		w.Write([]byte(`{"domain_records":[],"links":{},"meta":{"total":0}}`))
	})
	mux.HandleFunc("POST /v2/domains/{zone}/records", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"domain_record":{"id":99,"type":"A","name":"cdn"}}`))
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return &provider{apiBase: ts.URL}, f
}

func testCreds() cloud.Credentials {
	return cloud.Credentials{Raw: map[string]string{CredKeyToken: "test-token"}}
}

func TestSweepOrphans_DeletesTaggedResources(t *testing.T) {
	p, f := newFakeDO(t)
	if err := p.SweepOrphans(t.Context(), testCreds(), "eng-1"); err != nil {
		t.Fatalf("SweepOrphans: %v", err)
	}
	if !f.hit("DELETE", "/v2/droplets/12345") {
		t.Error("expected the tagged droplet to be deleted")
	}
	if !f.hit("DELETE", "/v2/firewalls/fw-1") {
		t.Error("expected the tagged firewall to be deleted")
	}
	if !f.hit("DELETE", "/v2/reserved_ips/203.0.113.9") {
		t.Error("expected the reserved IP on the swept droplet to be deleted")
	}
}

func TestDestroy_DeletesDroplet(t *testing.T) {
	p, f := newFakeDO(t)
	node := domain.Node{ID: "node-1", ProviderRef: "12345"}
	if err := p.Destroy(t.Context(), testCreds(), node); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if !f.hit("DELETE", "/v2/droplets/12345") {
		t.Error("expected droplet delete call")
	}
	// No ProviderRef → no-op, no error.
	if err := p.Destroy(t.Context(), testCreds(), domain.Node{ID: "x"}); err != nil {
		t.Errorf("Destroy with no ref should be a no-op: %v", err)
	}
}

func TestAssignStaticIP_ReturnsIP(t *testing.T) {
	p, _ := newFakeDO(t)
	node := domain.Node{ID: "node-1", ProviderRef: "12345"}
	ip, err := p.AssignStaticIP(t.Context(), testCreds(), node)
	if err != nil {
		t.Fatalf("AssignStaticIP: %v", err)
	}
	if ip != "203.0.113.50" {
		t.Errorf("ip = %q, want 203.0.113.50", ip)
	}
}

func TestConfigureIngress_CreatesFirewall(t *testing.T) {
	p, f := newFakeDO(t)
	node := domain.Node{ID: "node-9", EngagementID: "eng-1", ProviderRef: "12345"}
	rules := []domain.Rule{{Protocol: "tcp", Port: 443, SourceCIDR: "0.0.0.0/0", Allow: true}}
	if err := p.ConfigureIngress(t.Context(), testCreds(), node, rules); err != nil {
		t.Fatalf("ConfigureIngress: %v", err)
	}
	// The node's firewall name doesn't match the existing "old" FW, so it creates.
	if !f.hit("POST", "/v2/firewalls") {
		t.Error("expected a firewall to be created")
	}
}

func TestManageDNS_CreatesWhenAbsent(t *testing.T) {
	p, f := newFakeDO(t)
	rec := domain.Record{Zone: "example.com", Name: "cdn", Type: "A", Value: "203.0.113.9", TTL: 300}
	if err := p.ManageDNS(t.Context(), testCreds(), rec); err != nil {
		t.Fatalf("ManageDNS: %v", err)
	}
	if !f.hit("POST", "/v2/domains/example.com/records") {
		t.Error("expected a DNS record to be created")
	}
}
