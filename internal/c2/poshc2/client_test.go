package poshc2

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testClient builds an httpPoshC2Client pointed at the test server, with no poll
// delay so Execute returns as soon as output is available.
func testClient(srv *httptest.Server) *httpPoshC2Client {
	return &httpPoshC2Client{base: srv.URL, hc: srv.Client(), poll: 0, timeout: 2 * time.Second}
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
		case r.Method == http.MethodPost && r.URL.Path == "/newtasksview":
			posted = true
			w.WriteHeader(http.StatusOK)
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
		t.Error("Execute should POST the task to /newtasksview")
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

func TestHTTPClient_SendsBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := testClient(srv)
	c.token = "tok-abc"
	if _, err := c.Implants(context.Background()); err != nil {
		t.Fatalf("Implants: %v", err)
	}
	if gotAuth != "Bearer tok-abc" {
		t.Errorf("Authorization = %q, want Bearer tok-abc", gotAuth)
	}
}
