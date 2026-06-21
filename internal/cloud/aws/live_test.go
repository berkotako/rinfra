package aws

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
)

// fakeAWS is an httptest-backed stand-in for the AWS EC2 (query protocol) and
// Route53 (REST-XML) APIs. EC2 routes by the form "Action" param; Route53 by
// HTTP method + path. It records the Actions/paths seen so tests can assert
// which operations fired, and serves minimal-but-valid XML so the SDK
// deserializers unmarshal cleanly.
type fakeAWS struct {
	mu      sync.Mutex
	actions []string // EC2 Action names + Route53 "METHOD path" entries
}

func (f *fakeAWS) record(s string) {
	f.mu.Lock()
	f.actions = append(f.actions, s)
	f.mu.Unlock()
}

func (f *fakeAWS) saw(s string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.actions {
		if a == s {
			return true
		}
	}
	return false
}

const ec2NS = `xmlns="http://ec2.amazonaws.com/doc/2016-11-15/"`
const r53NS = `xmlns="https://route53.amazonaws.com/doc/2013-04-01/"`

// newFakeAWS stands up a single httptest server that multiplexes EC2 (POST to
// "/" with an Action form value) and Route53 (REST paths) requests, and returns
// a provider pointed at it. The opts map lets a test override the XML body for a
// given EC2 Action (e.g. to return an empty instance set for the no-orphan case).
func newFakeAWS(t *testing.T, ec2Bodies map[string]string) (*provider, *fakeAWS) {
	t.Helper()
	f := &fakeAWS{}

	defaults := map[string]string{
		"DescribeInstances": `<DescribeInstancesResponse ` + ec2NS + `>
			<reservationSet><item>
				<instancesSet><item>
					<instanceId>i-1234567890abcdef0</instanceId>
					<groupSet><item><groupId>sg-0abc</groupId></item></groupSet>
				</item></instancesSet>
			</item></reservationSet>
		</DescribeInstancesResponse>`,
		"AuthorizeSecurityGroupIngress": `<AuthorizeSecurityGroupIngressResponse ` + ec2NS + `><return>true</return></AuthorizeSecurityGroupIngressResponse>`,
		"AllocateAddress": `<AllocateAddressResponse ` + ec2NS + `>
			<allocationId>eipalloc-0abc</allocationId>
			<publicIp>198.51.100.7</publicIp>
		</AllocateAddressResponse>`,
		"AssociateAddress":       `<AssociateAddressResponse ` + ec2NS + `><return>true</return><associationId>eipassoc-0abc</associationId></AssociateAddressResponse>`,
		"TerminateInstances":     `<TerminateInstancesResponse ` + ec2NS + `><instancesSet><item><instanceId>i-1234567890abcdef0</instanceId></item></instancesSet></TerminateInstancesResponse>`,
		"DescribeAddresses":      `<DescribeAddressesResponse ` + ec2NS + `><addressesSet><item><allocationId>eipalloc-0abc</allocationId><publicIp>198.51.100.7</publicIp></item></addressesSet></DescribeAddressesResponse>`,
		"ReleaseAddress":         `<ReleaseAddressResponse ` + ec2NS + `><return>true</return></ReleaseAddressResponse>`,
		"DescribeSecurityGroups": `<DescribeSecurityGroupsResponse ` + ec2NS + `><securityGroupInfo><item><groupId>sg-0abc</groupId></item></securityGroupInfo></DescribeSecurityGroupsResponse>`,
		"DeleteSecurityGroup":    `<DeleteSecurityGroupResponse ` + ec2NS + `><return>true</return></DeleteSecurityGroupResponse>`,
	}
	for k, v := range ec2Bodies {
		defaults[k] = v
	}

	mux := http.NewServeMux()

	// Route53 REST-XML: list hosted zones by name.
	mux.HandleFunc("GET /2013-04-01/hostedzonesbyname", func(w http.ResponseWriter, r *http.Request) {
		f.record("GET /hostedzonesbyname")
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, `<ListHostedZonesByNameResponse `+r53NS+`>
			<HostedZones>
				<HostedZone>
					<Id>/hostedzone/Z123EXAMPLE</Id>
					<Name>example.com.</Name>
					<CallerReference>ref</CallerReference>
				</HostedZone>
			</HostedZones>
			<DNSName>example.com</DNSName>
			<IsTruncated>false</IsTruncated>
			<MaxItems>100</MaxItems>
		</ListHostedZonesByNameResponse>`)
	})

	// Route53 REST-XML: change resource record sets (UPSERT).
	mux.HandleFunc("POST /2013-04-01/hostedzone/{id}/rrset", func(w http.ResponseWriter, r *http.Request) {
		f.record("POST /hostedzone/" + r.PathValue("id") + "/rrset")
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, `<ChangeResourceRecordSetsResponse `+r53NS+`>
			<ChangeInfo><Id>/change/C1</Id><Status>PENDING</Status><SubmittedAt>2026-01-01T00:00:00Z</SubmittedAt></ChangeInfo>
		</ChangeResourceRecordSetsResponse>`)
	})

	// EC2 query protocol: everything else is a POST to "/" with an Action form value.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		action := r.Form.Get("Action")
		f.record(action)
		body, ok := defaults[action]
		if !ok {
			http.Error(w, "unexpected EC2 action: "+action, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/xml")
		io.WriteString(w, body)
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return &provider{baseEndpoint: ts.URL}, f
}

func testCreds() cloud.Credentials {
	return cloud.Credentials{Raw: map[string]string{
		CredKeyAccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		CredKeySecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		CredKeyRegion:          "us-east-1",
	}}
}

func TestConfigureIngress_Authorizes(t *testing.T) {
	p, f := newFakeAWS(t, nil)
	node := domain.Node{ID: "node-1", EngagementID: "eng-1", ProviderRef: "i-1234567890abcdef0"}
	rules := []domain.Rule{
		{Protocol: "tcp", Port: 443, SourceCIDR: "0.0.0.0/0", Allow: true},
		{Protocol: "tcp", Port: 22, SourceCIDR: "10.0.0.0/8", Allow: false}, // skipped (deny)
	}
	if err := p.ConfigureIngress(t.Context(), testCreds(), node, rules); err != nil {
		t.Fatalf("ConfigureIngress: %v", err)
	}
	if !f.saw("DescribeInstances") {
		t.Error("expected DescribeInstances to resolve the security group")
	}
	if !f.saw("AuthorizeSecurityGroupIngress") {
		t.Error("expected AuthorizeSecurityGroupIngress for the allow rule")
	}
}

func TestConfigureIngress_NoProviderRef(t *testing.T) {
	p, _ := newFakeAWS(t, nil)
	err := p.ConfigureIngress(t.Context(), testCreds(), domain.Node{ID: "x"}, nil)
	if err == nil {
		t.Error("expected an error when ProviderRef is empty")
	}
}

func TestAssignStaticIP_ReturnsIP(t *testing.T) {
	p, f := newFakeAWS(t, nil)
	node := domain.Node{ID: "node-1", EngagementID: "eng-1", ProviderRef: "i-1234567890abcdef0"}
	ip, err := p.AssignStaticIP(t.Context(), testCreds(), node)
	if err != nil {
		t.Fatalf("AssignStaticIP: %v", err)
	}
	if ip != "198.51.100.7" {
		t.Errorf("ip = %q, want 198.51.100.7", ip)
	}
	if !f.saw("AllocateAddress") || !f.saw("AssociateAddress") {
		t.Error("expected AllocateAddress + AssociateAddress")
	}
}

func TestManageDNS_Upserts(t *testing.T) {
	p, f := newFakeAWS(t, nil)
	rec := domain.Record{Zone: "example.com", Name: "cdn", Type: "A", Value: "198.51.100.7", TTL: 300}
	if err := p.ManageDNS(t.Context(), testCreds(), rec); err != nil {
		t.Fatalf("ManageDNS: %v", err)
	}
	if !f.saw("GET /hostedzonesbyname") {
		t.Error("expected the hosted zone to be resolved by name")
	}
	if !f.saw("POST /hostedzone/Z123EXAMPLE/rrset") {
		t.Error("expected an UPSERT change against the resolved zone ID")
	}
}

func TestDestroy_TerminatesInstance(t *testing.T) {
	p, f := newFakeAWS(t, nil)
	node := domain.Node{ID: "node-1", ProviderRef: "i-1234567890abcdef0"}
	if err := p.Destroy(t.Context(), testCreds(), node); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if !f.saw("TerminateInstances") {
		t.Error("expected TerminateInstances")
	}
	// No ProviderRef → no-op, no error, no call.
	p2, f2 := newFakeAWS(t, nil)
	if err := p2.Destroy(t.Context(), testCreds(), domain.Node{ID: "x"}); err != nil {
		t.Errorf("Destroy with no ref should be a no-op: %v", err)
	}
	if f2.saw("TerminateInstances") {
		t.Error("Destroy with no ProviderRef should not call the API")
	}
}

func TestDestroy_IdempotentOnNotFound(t *testing.T) {
	// TerminateInstances returns InvalidInstanceID.NotFound → treated as success.
	notFound := map[string]string{
		"TerminateInstances": `<Response><Errors><Error><Code>InvalidInstanceID.NotFound</Code><Message>gone</Message></Error></Errors></Response>`,
	}
	p, f := newFakeAWSWithErrors(t, notFound)
	node := domain.Node{ID: "node-1", ProviderRef: "i-deadbeef"}
	if err := p.Destroy(t.Context(), testCreds(), node); err != nil {
		t.Fatalf("Destroy should be idempotent on NotFound, got: %v", err)
	}
	if !f.saw("TerminateInstances") {
		t.Error("expected TerminateInstances to have been attempted")
	}
}

func TestSweepOrphans_DeletesTaggedResources(t *testing.T) {
	p, f := newFakeAWS(t, nil)
	if err := p.SweepOrphans(t.Context(), testCreds(), "eng-1"); err != nil {
		t.Fatalf("SweepOrphans: %v", err)
	}
	for _, want := range []string{
		"DescribeInstances", "TerminateInstances",
		"DescribeAddresses", "ReleaseAddress",
		"DescribeSecurityGroups", "DeleteSecurityGroup",
	} {
		if !f.saw(want) {
			t.Errorf("expected sweep to call %s", want)
		}
	}
}

func TestSweepOrphans_NoOrphans(t *testing.T) {
	// All describes return empty sets → no terminate/release/delete calls fire.
	empty := map[string]string{
		"DescribeInstances":      `<DescribeInstancesResponse ` + ec2NS + `><reservationSet/></DescribeInstancesResponse>`,
		"DescribeAddresses":      `<DescribeAddressesResponse ` + ec2NS + `><addressesSet/></DescribeAddressesResponse>`,
		"DescribeSecurityGroups": `<DescribeSecurityGroupsResponse ` + ec2NS + `><securityGroupInfo/></DescribeSecurityGroupsResponse>`,
	}
	p, f := newFakeAWS(t, empty)
	if err := p.SweepOrphans(t.Context(), testCreds(), "eng-1"); err != nil {
		t.Fatalf("SweepOrphans (no orphans): %v", err)
	}
	for _, mustNot := range []string{"TerminateInstances", "ReleaseAddress", "DeleteSecurityGroup"} {
		if f.saw(mustNot) {
			t.Errorf("no-orphan invariant: %s should not have been called", mustNot)
		}
	}
}

// newFakeAWSWithErrors stands up a fake that returns EC2 error XML (HTTP 400)
// for the given Actions, and the normal success bodies otherwise. Used to
// exercise idempotent NotFound handling.
func newFakeAWSWithErrors(t *testing.T, errorBodies map[string]string) (*provider, *fakeAWS) {
	t.Helper()
	f := &fakeAWS{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		action := r.Form.Get("Action")
		f.record(action)
		if body, ok := errorBodies[action]; ok {
			w.Header().Set("Content-Type", "text/xml")
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, body)
			return
		}
		w.Header().Set("Content-Type", "text/xml")
		io.WriteString(w, `<`+action+`Response `+ec2NS+`><return>true</return></`+action+`Response>`)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return &provider{baseEndpoint: ts.URL}, f
}
