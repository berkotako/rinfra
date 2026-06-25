package poshc2

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/c2/deploy"
	"github.com/rinfra/rinfra/internal/domain"
)

func TestDeploy_UsesInstaller(t *testing.T) {
	runner := deploy.NewFakeRunner()
	_, err := deployPoshC2(context.Background(), runner, domain.Node{PublicIP: "203.0.113.70"}, c2.Config{})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	script, ok := runner.Uploaded("/tmp/rinfra-install.sh")
	if !ok {
		t.Fatal("install script not uploaded")
	}
	for _, want := range []string{
		"git fetch --depth 1 origin",
		"64eb5570db2ea0a83cde855001caac9d8d33da29", // pinned v9.0 commit
		"./Install.sh",                             // the upstream installer (not pip3 alone)
		"posh-api-server",                          // the REST API is started
	} {
		if !strings.Contains(script, want) {
			t.Errorf("install script missing %q", want)
		}
	}
	for _, unwanted := range []string{"v8.0.3", "placeholder", "sha256sum", ".tar.gz", "poshc2 -i"} {
		if strings.Contains(script, unwanted) {
			t.Errorf("install script should not contain %q (old/broken deploy)", unwanted)
		}
	}
}

// testClient builds an httpPoshC2Client pointed at the test server, with no poll
// delay so Execute returns as soon as output is available.
func testClient(srv *httptest.Server) *httpPoshC2Client {
	return &httpPoshC2Client{base: srv.URL, user: "poshc2", pass: "pw", hc: srv.Client(), poll: 0, timeout: 2 * time.Second}
}

func TestHTTPClient_Implants(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/liveimplants" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Write([]byte(`[{"ImplantID":"123","Hostname":"WS01","User":"CORP\\alice"}]`))
	}))
	defer srv.Close()

	implants, err := testClient(srv).Implants(context.Background())
	if err != nil {
		t.Fatalf("Implants: %v", err)
	}
	if len(implants) != 1 {
		t.Fatalf("got %d implants, want 1", len(implants))
	}
	got := implants[0]
	if got.ID != "123" || got.Hostname != "WS01" || got.Username != "CORP\\alice" {
		t.Errorf("parsed implant = %+v", got)
	}
}

func TestHTTPClient_Execute_QueuesAndPolls(t *testing.T) {
	var posted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/newtasks":
			posted = true
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && r.URL.Path == "/tasks/implant/imp-1":
			w.Write([]byte(`[{"Output":"nt authority\\system"}]`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	out, err := testClient(srv).Execute(context.Background(), "imp-1", "whoami")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !posted {
		t.Error("Execute should POST the task to /newtasks")
	}
	if out != "nt authority\\system" {
		t.Errorf("output = %q, want the task result", out)
	}
}

func TestHTTPClient_Execute_QueueError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := testClient(srv).Execute(context.Background(), "imp-1", "whoami")
	if err == nil || !strings.Contains(err.Error(), "queue task") {
		t.Fatalf("expected a queue-task error, got %v", err)
	}
}

func TestHTTPClient_SendsBasicAuth(t *testing.T) {
	var gotUser, gotPass string
	var ok bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, ok = r.BasicAuth()
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := testClient(srv)
	c.user, c.pass = "poshc2", "change_on_install"
	if _, err := c.Implants(context.Background()); err != nil {
		t.Fatalf("Implants: %v", err)
	}
	if !ok || gotUser != "poshc2" || gotPass != "change_on_install" {
		t.Errorf("basic auth = (%q,%q,ok=%v), want poshc2/change_on_install", gotUser, gotPass, ok)
	}
}
