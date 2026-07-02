package gcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
)

// fakeGCP is an httptest-backed stand-in for the Google compute/DNS REST APIs.
// It records the method+path of every request so tests can assert which
// operations fired, and serves canned JSON (Operation objects, Firewall,
// Address, AggregatedList, Change) for the endpoints the client libraries call.
//
// notFound, when non-nil, lists path suffixes that should 404 (used to exercise
// idempotent teardown paths and the insert-when-absent firewall branch).
type fakeGCP struct {
	mu       sync.Mutex
	hits     []string
	notFound map[string]bool // path suffix -> 404
}

func (f *fakeGCP) record(r *http.Request) {
	f.mu.Lock()
	f.hits = append(f.hits, r.Method+" "+r.URL.Path)
	f.mu.Unlock()
}

func (f *fakeGCP) hit(method, pathSuffix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, h := range f.hits {
		if strings.HasPrefix(h, method+" ") && strings.HasSuffix(h, pathSuffix) {
			return true
		}
	}
	return false
}

func (f *fakeGCP) is404(path string) bool {
	for suffix := range f.notFound {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

// operationJSON is a minimal compute Operation body (DONE so any settling poll
// would also succeed, though the standalone methods do not poll).
const operationJSON = `{"kind":"compute#operation","name":"op-1","status":"DONE","progress":100}`

func newFakeGCP(t *testing.T, notFound map[string]bool) (*provider, *fakeGCP) {
	t.Helper()
	f := &fakeGCP{notFound: notFound}
	if f.notFound == nil {
		f.notFound = map[string]bool{}
	}

	h := func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if f.is404(r.URL.Path) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"code":404,"message":"notFound"}}`))
			return
		}
		switch {
		// --- compute: firewalls ---
		case strings.HasSuffix(r.URL.Path, "/global/firewalls") && r.Method == http.MethodGet:
			// List for SweepOrphans: one firewall tagged for eng-1 via description.
			w.Write([]byte(`{"kind":"compute#firewallList","items":[` +
				`{"name":"rinfra-fw-node-1","description":"rinfra-eng-1"},` +
				`{"name":"other","description":"rinfra-other"}]}`))
		case strings.HasSuffix(r.URL.Path, "/global/firewalls") && r.Method == http.MethodPost:
			w.Write([]byte(operationJSON))
		case strings.Contains(r.URL.Path, "/global/firewalls/") && r.Method == http.MethodGet:
			// Firewall Get for the upsert probe: exists by default.
			w.Write([]byte(`{"kind":"compute#firewall","name":"rinfra-fw-node-9"}`))
		case strings.Contains(r.URL.Path, "/global/firewalls/") && (r.Method == http.MethodPatch || r.Method == http.MethodDelete):
			w.Write([]byte(operationJSON))

		// --- compute: addresses ---
		case strings.HasSuffix(r.URL.Path, "/aggregated/addresses") && r.Method == http.MethodGet:
			w.Write([]byte(`{"kind":"compute#addressAggregatedList","items":{` +
				`"regions/us-central1":{"addresses":[` +
				`{"name":"rinfra-eng-1-node-1-ip","region":"https://compute.googleapis.com/compute/v1/projects/p/regions/us-central1","address":"203.0.113.7"}]}}}`))
		case strings.HasSuffix(r.URL.Path, "/addresses") && r.Method == http.MethodPost:
			w.Write([]byte(operationJSON))
		case strings.Contains(r.URL.Path, "/addresses/") && r.Method == http.MethodGet:
			// Read-back after reserve.
			w.Write([]byte(`{"kind":"compute#address","name":"a","address":"203.0.113.42"}`))
		case strings.Contains(r.URL.Path, "/addresses/") && r.Method == http.MethodDelete:
			w.Write([]byte(operationJSON))

		// --- compute: instances ---
		case strings.HasSuffix(r.URL.Path, "/aggregated/instances") && r.Method == http.MethodGet:
			w.Write([]byte(`{"kind":"compute#instanceAggregatedList","items":{` +
				`"zones/us-central1-a":{"instances":[` +
				`{"name":"rinfra-eng-1-node-1","zone":"https://compute.googleapis.com/compute/v1/projects/p/zones/us-central1-a"}]}}}`))
		case strings.Contains(r.URL.Path, "/instances/") && r.Method == http.MethodDelete:
			w.Write([]byte(operationJSON))
		// AssignStaticIP attaches the reserved IP: get instance (to find the NIC),
		// then swap its external access config.
		case strings.HasSuffix(r.URL.Path, "/addAccessConfig") && r.Method == http.MethodPost:
			w.Write([]byte(operationJSON))
		case strings.HasSuffix(r.URL.Path, "/deleteAccessConfig") && r.Method == http.MethodPost:
			w.Write([]byte(operationJSON))
		case strings.Contains(r.URL.Path, "/instances/") && r.Method == http.MethodGet:
			w.Write([]byte(`{"kind":"compute#instance","name":"rinfra-eng-1-node-1",` +
				`"networkInterfaces":[{"name":"nic0","accessConfigs":[{"name":"External NAT","type":"ONE_TO_ONE_NAT"}]}]}`))

		// --- dns: rrsets list + changes ---
		case strings.HasSuffix(r.URL.Path, "/rrsets") && r.Method == http.MethodGet:
			// No existing record by default -> pure addition (no deletions).
			w.Write([]byte(`{"kind":"dns#resourceRecordSetsListResponse","rrsets":[]}`))
		case strings.HasSuffix(r.URL.Path, "/changes") && r.Method == http.MethodPost:
			w.Write([]byte(`{"kind":"dns#change","id":"1","status":"pending"}`))

		default:
			t.Logf("unhandled fake request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"code":404,"message":"unhandled"}}`))
		}
	}

	ts := httptest.NewServer(http.HandlerFunc(h))
	t.Cleanup(ts.Close)
	return &provider{baseEndpoint: ts.URL + "/"}, f
}

func gcpTestCreds() cloud.Credentials {
	return cloud.Credentials{Raw: map[string]string{
		CredKeyCredentials: `{"type":"service_account"}`,
		CredKeyProject:     "test-project",
	}}
}

func TestConfigureIngress_InsertsFirewall(t *testing.T) {
	// Firewall Get returns 404 -> Insert path. One source CIDR → one firewall,
	// suffixed "-0" (firewalls are now emitted per distinct source CIDR).
	p, f := newFakeGCP(t, map[string]bool{"/global/firewalls/rinfra-fw-node-9-0": true})
	node := domain.Node{ID: "node-9", EngagementID: "eng-1", ProviderRef: "rinfra-eng-1-node-9", Spec: domain.NodeSpec{Region: "us-central1-a"}}
	rules := []domain.Rule{{Protocol: "tcp", Port: 443, SourceCIDR: "0.0.0.0/0", Allow: true}}
	if err := p.ConfigureIngress(t.Context(), gcpTestCreds(), node, rules); err != nil {
		t.Fatalf("ConfigureIngress: %v", err)
	}
	if !f.hit("POST", "/global/firewalls") {
		t.Error("expected a firewall insert (POST) when none exists")
	}
}

func TestConfigureIngress_PatchesExistingFirewall(t *testing.T) {
	// Default Get succeeds -> Patch path.
	p, f := newFakeGCP(t, nil)
	node := domain.Node{ID: "node-9", EngagementID: "eng-1", ProviderRef: "rinfra-eng-1-node-9", Spec: domain.NodeSpec{Region: "us-central1-a"}}
	rules := []domain.Rule{{Protocol: "tcp", Port: 443, Allow: true}}
	if err := p.ConfigureIngress(t.Context(), gcpTestCreds(), node, rules); err != nil {
		t.Fatalf("ConfigureIngress: %v", err)
	}
	if !f.hit("PATCH", "/global/firewalls/rinfra-fw-node-9-0") {
		t.Error("expected a firewall patch when the rule already exists")
	}
}

// TestConfigureIngress_PerSourceFirewalls verifies that rules with distinct
// source CIDRs produce separate firewalls, so a restricted source's ports are
// not exposed to every source (no source×port cross-product).
func TestConfigureIngress_PerSourceFirewalls(t *testing.T) {
	// Both firewall names 404 → both take the Insert path.
	p, f := newFakeGCP(t, map[string]bool{
		"/global/firewalls/rinfra-fw-node-9-0": true,
		"/global/firewalls/rinfra-fw-node-9-1": true,
	})
	node := domain.Node{ID: "node-9", EngagementID: "eng-1", ProviderRef: "rinfra-eng-1-node-9", Spec: domain.NodeSpec{Region: "us-central1-a"}}
	rules := []domain.Rule{
		{Protocol: "tcp", Port: 443, SourceCIDR: "0.0.0.0/0", Allow: true},
		{Protocol: "tcp", Port: 22, SourceCIDR: "10.0.0.0/8", Allow: true},
	}
	if err := p.ConfigureIngress(t.Context(), gcpTestCreds(), node, rules); err != nil {
		t.Fatalf("ConfigureIngress: %v", err)
	}
	// Two distinct sources → two firewall POSTs.
	posts := 0
	for _, h := range f.hits {
		if strings.HasPrefix(h, "POST ") && strings.HasSuffix(h, "/global/firewalls") {
			posts++
		}
	}
	if posts != 2 {
		t.Errorf("expected 2 per-source firewall inserts, got %d", posts)
	}
}

func TestAssignStaticIP_ReturnsIP(t *testing.T) {
	p, f := newFakeGCP(t, nil)
	node := domain.Node{ID: "node-1", EngagementID: "eng-1", ProviderRef: "rinfra-eng-1-node-1", Spec: domain.NodeSpec{Region: "us-central1-a"}}
	ip, err := p.AssignStaticIP(t.Context(), gcpTestCreds(), node)
	if err != nil {
		t.Fatalf("AssignStaticIP: %v", err)
	}
	if ip != "203.0.113.42" {
		t.Errorf("ip = %q, want 203.0.113.42", ip)
	}
	if !f.hit("POST", "/regions/us-central1/addresses") {
		t.Error("expected a regional address reservation (POST)")
	}
}

func TestManageDNS_UpsertAddition(t *testing.T) {
	p, f := newFakeGCP(t, nil)
	rec := domain.Record{Zone: "my-zone", Name: "cdn.example.com", Type: "A", Value: "203.0.113.9", TTL: 300}
	if err := p.ManageDNS(t.Context(), gcpTestCreds(), rec); err != nil {
		t.Fatalf("ManageDNS: %v", err)
	}
	if !f.hit("GET", "/managedZones/my-zone/rrsets") {
		t.Error("expected a record-set list to detect an existing record for UPSERT")
	}
	if !f.hit("POST", "/managedZones/my-zone/changes") {
		t.Error("expected a DNS change (UPSERT) to be applied")
	}
}

func TestManageDNS_UpsertReplacesExisting(t *testing.T) {
	// rrsets list returns an existing record of the same name+type; the change
	// must then include a deletion. We assert the change POST still fires.
	f := &fakeGCP{notFound: map[string]bool{}}
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		switch {
		case strings.HasSuffix(r.URL.Path, "/rrsets") && r.Method == http.MethodGet:
			w.Write([]byte(`{"rrsets":[{"name":"cdn.example.com.","type":"A","ttl":300,"rrdatas":["198.51.100.1"]}]}`))
		case strings.HasSuffix(r.URL.Path, "/changes") && r.Method == http.MethodPost:
			w.Write([]byte(`{"kind":"dns#change","id":"2","status":"pending"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"code":404,"message":"x"}}`))
		}
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	p := &provider{baseEndpoint: ts.URL + "/"}
	rec := domain.Record{Zone: "my-zone", Name: "cdn.example.com", Type: "A", Value: "203.0.113.9", TTL: 300}
	if err := p.ManageDNS(t.Context(), gcpTestCreds(), rec); err != nil {
		t.Fatalf("ManageDNS: %v", err)
	}
	if !f.hit("POST", "/managedZones/my-zone/changes") {
		t.Error("expected the UPSERT change to be applied even when a record exists")
	}
}

func TestDestroy_DeletesInstance(t *testing.T) {
	p, f := newFakeGCP(t, nil)
	node := domain.Node{ID: "node-1", ProviderRef: "rinfra-eng-1-node-1", Spec: domain.NodeSpec{Region: "us-central1-a"}}
	if err := p.Destroy(t.Context(), gcpTestCreds(), node); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if !f.hit("DELETE", "/zones/us-central1-a/instances/rinfra-eng-1-node-1") {
		t.Error("expected an instance delete in the node's zone")
	}
}

func TestDestroy_SelfLinkRef(t *testing.T) {
	p, f := newFakeGCP(t, nil)
	node := domain.Node{ID: "node-1", ProviderRef: "projects/test-project/zones/us-west1-b/instances/rinfra-x"}
	if err := p.Destroy(t.Context(), gcpTestCreds(), node); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if !f.hit("DELETE", "/zones/us-west1-b/instances/rinfra-x") {
		t.Error("expected zone+name to be parsed from a self-link ProviderRef")
	}
}

func TestDestroy_Idempotent404(t *testing.T) {
	// Instance already gone -> 404 -> nil.
	p, _ := newFakeGCP(t, map[string]bool{"/instances/rinfra-eng-1-node-1": true})
	node := domain.Node{ID: "node-1", ProviderRef: "rinfra-eng-1-node-1", Spec: domain.NodeSpec{Region: "us-central1-a"}}
	if err := p.Destroy(t.Context(), gcpTestCreds(), node); err != nil {
		t.Errorf("Destroy should treat 404 as success, got: %v", err)
	}
	// No ProviderRef -> no-op, no error.
	if err := p.Destroy(t.Context(), gcpTestCreds(), domain.Node{ID: "x"}); err != nil {
		t.Errorf("Destroy with no ref should be a no-op: %v", err)
	}
}

func TestSweepOrphans_DeletesEngagementResources(t *testing.T) {
	p, f := newFakeGCP(t, nil)
	if err := p.SweepOrphans(t.Context(), gcpTestCreds(), "eng-1"); err != nil {
		t.Fatalf("SweepOrphans: %v", err)
	}
	if !f.hit("DELETE", "/zones/us-central1-a/instances/rinfra-eng-1-node-1") {
		t.Error("expected the labeled instance to be deleted")
	}
	if !f.hit("DELETE", "/regions/us-central1/addresses/rinfra-eng-1-node-1-ip") {
		t.Error("expected the labeled address to be deleted")
	}
	if !f.hit("DELETE", "/global/firewalls/rinfra-fw-node-1") {
		t.Error("expected the engagement-tagged firewall to be deleted")
	}
}

func TestSweepOrphans_NoOrphanInvariant(t *testing.T) {
	// Aggregated lists empty and no matching firewall -> nothing deleted, no error.
	f := &fakeGCP{notFound: map[string]bool{}}
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		switch {
		case strings.HasSuffix(r.URL.Path, "/aggregated/instances"):
			w.Write([]byte(`{"items":{}}`))
		case strings.HasSuffix(r.URL.Path, "/aggregated/addresses"):
			w.Write([]byte(`{"items":{}}`))
		case strings.HasSuffix(r.URL.Path, "/global/firewalls") && r.Method == http.MethodGet:
			w.Write([]byte(`{"items":[{"name":"unrelated","description":"rinfra-other"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"code":404,"message":"x"}}`))
		}
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	p := &provider{baseEndpoint: ts.URL + "/"}
	if err := p.SweepOrphans(t.Context(), gcpTestCreds(), "eng-1"); err != nil {
		t.Fatalf("SweepOrphans (no orphans): %v", err)
	}
	for _, h := range f.hits {
		if strings.HasPrefix(h, "DELETE ") {
			t.Errorf("no-orphan sweep should delete nothing, but issued: %s", h)
		}
	}
}
