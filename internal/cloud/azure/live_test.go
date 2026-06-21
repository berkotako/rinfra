package azure

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	azpolicy "github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
)

// fakeCred is a stand-in azcore.TokenCredential. The ARM clients call GetToken
// before every request; returning a dummy token keeps the pipeline happy
// without contacting Azure AD.
type fakeCred struct{}

func (fakeCred) GetToken(_ context.Context, _ azpolicy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: "fake-token", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

// fakeARM is an in-process azcore Transporter that routes by request path and
// serves canned ARM JSON. It records every method+path so tests can assert which
// ARM operations fired. For long-running operations (PUT/DELETE) it returns a
// terminal 200 with provisioningState "Succeeded" so PollUntilDone completes
// immediately without async polling.
type fakeARM struct {
	mu        sync.Mutex
	hits      []string
	notFound  map[string]bool // method+path → return 404
	listEmpty bool            // resource-group list returns no groups
}

func (f *fakeARM) record(method, path string) {
	f.mu.Lock()
	f.hits = append(f.hits, method+" "+path)
	f.mu.Unlock()
}

// hit reports whether a request matching method and a path containing substr
// was seen.
func (f *fakeARM) hit(method, substr string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, h := range f.hits {
		if strings.HasPrefix(h, method+" ") && strings.Contains(h, substr) {
			return true
		}
	}
	return false
}

func jsonResp(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

// Do implements azcore/policy.Transporter.
func (f *fakeARM) Do(req *http.Request) (*http.Response, error) {
	path := req.URL.Path
	f.record(req.Method, path)

	if f.notFound[req.Method+" "+path] {
		return jsonResp(req, http.StatusNotFound, `{"error":{"code":"ResourceGroupNotFound","message":"not found"}}`), nil
	}

	switch {
	// Resource-group list (SweepOrphans).
	case req.Method == http.MethodGet && strings.HasSuffix(path, "/resourcegroups"):
		if f.listEmpty {
			return jsonResp(req, http.StatusOK, `{"value":[]}`), nil
		}
		return jsonResp(req, http.StatusOK, `{"value":[{"id":"/rg/rinfra-eng-1234","name":"rinfra-eng-1234","location":"eastus","tags":{"rinfra":"eng-12345678"}}]}`), nil

	// Resource-group delete (Destroy / SweepOrphans) — terminal 200.
	case req.Method == http.MethodDelete && strings.Contains(path, "/resourcegroups/"):
		return jsonResp(req, http.StatusOK, `{}`), nil

	// Security-rule create-or-update (ConfigureIngress) — terminal Succeeded.
	case req.Method == http.MethodPut && strings.Contains(path, "/securityRules/"):
		return jsonResp(req, http.StatusOK, `{"name":"rule-1","properties":{"provisioningState":"Succeeded","access":"Allow","direction":"Inbound","protocol":"Tcp","priority":100,"destinationPortRange":"443"}}`), nil

	// Public-IP create-or-update (AssignStaticIP) — terminal Succeeded with address.
	case req.Method == http.MethodPut && strings.Contains(path, "/publicIPAddresses/"):
		return jsonResp(req, http.StatusOK, `{"name":"node-pip","location":"eastus","properties":{"provisioningState":"Succeeded","publicIPAllocationMethod":"Static","ipAddress":"20.0.0.42"}}`), nil

	// DNS record-set create-or-update (ManageDNS).
	case req.Method == http.MethodPut && (strings.Contains(path, "/A/") || strings.Contains(path, "/CNAME/") || strings.Contains(path, "/TXT/") || strings.Contains(path, "/dnsZones/")):
		return jsonResp(req, http.StatusOK, `{"name":"cdn","properties":{"TTL":300}}`), nil

	default:
		// Unknown GETs (e.g. PIP read-back) return an empty success.
		return jsonResp(req, http.StatusOK, `{}`), nil
	}
}

func newFakeProvider(f *fakeARM) *provider {
	return &provider{transport: f, cred: fakeCred{}}
}

func testCreds() cloud.Credentials {
	return cloud.Credentials{Raw: map[string]string{
		CredKeySubscriptionID:   "sub-123",
		CredKeyTenantID:         "tenant-123",
		CredKeyClientID:         "client-123",
		CredKeyClientSecret:     "secret-123",
		CredKeySSHPublicKey:     "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITESTKEY operator@rinfra",
		CredKeyDNSResourceGroup: "dns-rg",
	}}
}

func TestConfigureIngress_CreatesNSGRules(t *testing.T) {
	f := &fakeARM{}
	p := newFakeProvider(f)
	node := domain.Node{ID: "node-1234abcd", EngagementID: "eng-12345678", ProviderRef: "/vm/id"}
	rules := []domain.Rule{
		{Protocol: "tcp", Port: 443, SourceCIDR: "0.0.0.0/0", Allow: true},
		{Protocol: "tcp", Port: 22, SourceCIDR: "10.0.0.0/8", Allow: false},
	}
	if err := p.ConfigureIngress(t.Context(), testCreds(), node, rules); err != nil {
		t.Fatalf("ConfigureIngress: %v", err)
	}
	// Two rules → two PUTs on the node's NSG.
	if !f.hit("PUT", "/networkSecurityGroups/rinfra-eng-1234-node-123-nsg/securityRules/rule-1") {
		t.Errorf("expected first NSG rule PUT; hits=%v", f.hits)
	}
	if !f.hit("PUT", "/securityRules/rule-2") {
		t.Errorf("expected second NSG rule PUT; hits=%v", f.hits)
	}
}

func TestConfigureIngress_RequiresProviderRef(t *testing.T) {
	p := newFakeProvider(&fakeARM{})
	node := domain.Node{ID: "node-1", EngagementID: "eng-1"} // no ProviderRef
	if err := p.ConfigureIngress(t.Context(), testCreds(), node, nil); err == nil {
		t.Fatal("expected error for node without ProviderRef")
	}
}

func TestAssignStaticIP_ReturnsIP(t *testing.T) {
	f := &fakeARM{}
	p := newFakeProvider(f)
	node := domain.Node{ID: "node-1234abcd", EngagementID: "eng-12345678", ProviderRef: "/vm/id"}
	ip, err := p.AssignStaticIP(t.Context(), testCreds(), node)
	if err != nil {
		t.Fatalf("AssignStaticIP: %v", err)
	}
	if ip != "20.0.0.42" {
		t.Errorf("ip = %q, want 20.0.0.42", ip)
	}
	if !f.hit("PUT", "/publicIPAddresses/rinfra-eng-1234-node-123-pip") {
		t.Errorf("expected public IP PUT; hits=%v", f.hits)
	}
}

func TestManageDNS_UpsertsRecord(t *testing.T) {
	f := &fakeARM{}
	p := newFakeProvider(f)
	rec := domain.Record{Zone: "example.com", Name: "cdn", Type: "A", Value: "20.0.0.42", TTL: 300}
	if err := p.ManageDNS(t.Context(), testCreds(), rec); err != nil {
		t.Fatalf("ManageDNS: %v", err)
	}
	if !f.hit("PUT", "/dnsZones/example.com/A/cdn") {
		t.Errorf("expected DNS A record PUT; hits=%v", f.hits)
	}
}

func TestManageDNS_RejectsBadType(t *testing.T) {
	p := newFakeProvider(&fakeARM{})
	rec := domain.Record{Zone: "example.com", Name: "cdn", Type: "MX", Value: "mail.example.com"}
	if err := p.ManageDNS(t.Context(), testCreds(), rec); err == nil {
		t.Fatal("expected error for unsupported record type MX")
	}
}

func TestDestroy_DeletesResourceGroup(t *testing.T) {
	f := &fakeARM{}
	p := newFakeProvider(f)
	node := domain.Node{ID: "node-1234abcd", EngagementID: "eng-12345678", ProviderRef: "/vm/id"}
	if err := p.Destroy(t.Context(), testCreds(), node); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if !f.hit("DELETE", "/resourcegroups/rinfra-eng-1234") {
		t.Errorf("expected resource group DELETE; hits=%v", f.hits)
	}
}

func TestDestroy_NoProviderRefIsNoop(t *testing.T) {
	f := &fakeARM{}
	p := newFakeProvider(f)
	if err := p.Destroy(t.Context(), testCreds(), domain.Node{ID: "x"}); err != nil {
		t.Fatalf("Destroy with no ref should be a no-op: %v", err)
	}
	if len(f.hits) != 0 {
		t.Errorf("expected no ARM calls for empty ProviderRef, got %v", f.hits)
	}
}

func TestDestroy_NotFoundIsIdempotent(t *testing.T) {
	f := &fakeARM{notFound: map[string]bool{
		"DELETE /subscriptions/sub-123/resourcegroups/rinfra-eng-1234": true,
	}}
	p := newFakeProvider(f)
	node := domain.Node{ID: "node-1234abcd", EngagementID: "eng-12345678", ProviderRef: "/vm/id"}
	if err := p.Destroy(t.Context(), testCreds(), node); err != nil {
		t.Fatalf("Destroy should treat 404 as success: %v", err)
	}
}

func TestSweepOrphans_DeletesTaggedResourceGroups(t *testing.T) {
	f := &fakeARM{}
	p := newFakeProvider(f)
	if err := p.SweepOrphans(t.Context(), testCreds(), "eng-12345678"); err != nil {
		t.Fatalf("SweepOrphans: %v", err)
	}
	if !f.hit("GET", "/resourcegroups") {
		t.Errorf("expected resource group list; hits=%v", f.hits)
	}
	if !f.hit("DELETE", "/resourcegroups/rinfra-eng-1234") {
		t.Errorf("expected tagged resource group DELETE; hits=%v", f.hits)
	}
}

func TestSweepOrphans_NoOrphansIsClean(t *testing.T) {
	f := &fakeARM{listEmpty: true}
	p := newFakeProvider(f)
	if err := p.SweepOrphans(t.Context(), testCreds(), "eng-12345678"); err != nil {
		t.Fatalf("SweepOrphans with no orphans: %v", err)
	}
	if f.hit("DELETE", "/resourcegroups/") {
		t.Errorf("expected no deletes when there are no orphans; hits=%v", f.hits)
	}
}

func TestMethods_RejectMissingCreds(t *testing.T) {
	p := newFakeProvider(&fakeARM{})
	bad := cloud.Credentials{Raw: map[string]string{}}
	node := domain.Node{ID: "n", EngagementID: "e", ProviderRef: "/vm"}
	if err := p.ConfigureIngress(t.Context(), bad, node, nil); err == nil {
		t.Error("ConfigureIngress should reject missing creds")
	}
	if _, err := p.AssignStaticIP(t.Context(), bad, node); err == nil {
		t.Error("AssignStaticIP should reject missing creds")
	}
	if err := p.SweepOrphans(t.Context(), bad, "e"); err == nil {
		t.Error("SweepOrphans should reject missing creds")
	}
}

// TestBuildLinuxAuthConfig_SSHKeyOnly is the security-fix assertion: VMs use SSH
// public-key auth and NEVER a password. The auth config must disable password
// auth, carry the supplied public key, and a missing key must fail closed.
func TestBuildLinuxAuthConfig_SSHKeyOnly(t *testing.T) {
	creds := testCreds()
	cfg, err := buildLinuxAuthConfig(creds)
	if err != nil {
		t.Fatalf("buildLinuxAuthConfig: %v", err)
	}
	if !cfg.DisablePasswordAuth {
		t.Error("expected password authentication to be disabled")
	}
	if cfg.SSHPublicKey != creds.Raw[CredKeySSHPublicKey] {
		t.Errorf("SSHPublicKey = %q, want the supplied key", cfg.SSHPublicKey)
	}
	if cfg.AdminUsername != AdminUsername {
		t.Errorf("AdminUsername = %q, want %q", cfg.AdminUsername, AdminUsername)
	}

	// Missing key → fail closed (no password fallback).
	noKey := cloud.Credentials{Raw: map[string]string{
		CredKeySubscriptionID: "s", CredKeyTenantID: "t",
		CredKeyClientID: "c", CredKeyClientSecret: "x",
	}}
	if _, err := buildLinuxAuthConfig(noKey); err == nil {
		t.Error("expected error when SSH public key is absent (must not provision a password VM)")
	}

	// Non-key garbage is rejected.
	badKey := testCreds()
	badKey.Raw[CredKeySSHPublicKey] = "not-a-key"
	if _, err := buildLinuxAuthConfig(badKey); err == nil {
		t.Error("expected error for a value that is not an OpenSSH public key")
	}
}
