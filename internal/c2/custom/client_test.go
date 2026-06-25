package custom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// customAPIServer stands up the in-house operator REST API (the contract
// defined in the package doc) with canned responses. It records the bearer
// token seen on each call and the last exec request body so tests can assert
// round-tripping and authentication.
type customAPIServer struct {
	token           string
	bearerSeen      string
	lastExecBody    string
	lastListener    map[string]any
	failExec        bool
	killedSession   string
	stoppedListener string
}

func (s *customAPIServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/listeners", func(w http.ResponseWriter, r *http.Request) {
		s.bearerSeen = r.Header.Get("Authorization")
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &s.lastListener)
		w.WriteHeader(http.StatusCreated)
	})

	mux.HandleFunc("/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		s.bearerSeen = r.Header.Get("Authorization")
		if r.Method != http.MethodGet {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, []map[string]string{
			{"id": "s1", "host": "host1", "user": "user1"},
			{"id": "s2", "host": "host2", "user": "user2"},
		})
	})

	// DELETE /api/v1/listeners/{id}
	mux.HandleFunc("/api/v1/listeners/", func(w http.ResponseWriter, r *http.Request) {
		s.bearerSeen = r.Header.Get("Authorization")
		if r.Method != http.MethodDelete {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		s.stoppedListener = strings.TrimPrefix(r.URL.Path, "/api/v1/listeners/")
		w.WriteHeader(http.StatusNoContent)
	})

	// POST /api/v1/sessions/{id}/exec and DELETE /api/v1/sessions/{id}
	mux.HandleFunc("/api/v1/sessions/", func(w http.ResponseWriter, r *http.Request) {
		s.bearerSeen = r.Header.Get("Authorization")
		if r.Method == http.MethodDelete {
			s.killedSession = strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/exec") {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if s.failExec {
			http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		s.lastExecBody = string(raw)
		var body struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(raw, &body)
		writeJSON(w, http.StatusOK, map[string]string{"output": "ran: " + body.Command})
	})

	return httptest.NewServer(mux)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func TestHTTPClient_StartListener(t *testing.T) {
	srv := &customAPIServer{token: "tok-123"}
	ts := srv.start(t)
	defer ts.Close()

	c := newHTTPCustomClient(ts.URL, srv.token)
	if err := c.StartListener(context.Background(), "https", "0.0.0.0", 443); err != nil {
		t.Fatalf("StartListener: %v", err)
	}
	if srv.bearerSeen != "Bearer tok-123" {
		t.Errorf("bearer header = %q, want %q", srv.bearerSeen, "Bearer tok-123")
	}
	if srv.lastListener["protocol"] != "https" {
		t.Errorf("listener protocol = %v, want https", srv.lastListener["protocol"])
	}
	if srv.lastListener["bind"] != "0.0.0.0" {
		t.Errorf("listener bind = %v, want 0.0.0.0", srv.lastListener["bind"])
	}
	// JSON numbers decode as float64.
	if srv.lastListener["port"].(float64) != 443 {
		t.Errorf("listener port = %v, want 443", srv.lastListener["port"])
	}
}

func TestHTTPClient_Sessions(t *testing.T) {
	srv := &customAPIServer{token: "tok-123"}
	ts := srv.start(t)
	defer ts.Close()

	c := newHTTPCustomClient(ts.URL, srv.token)
	sessions, err := c.Sessions(context.Background())
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if srv.bearerSeen != "Bearer tok-123" {
		t.Errorf("bearer header = %q, want %q", srv.bearerSeen, "Bearer tok-123")
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}
	if sessions[0].ID != "s1" || sessions[0].Hostname != "host1" || sessions[0].Username != "user1" {
		t.Errorf("session[0] = %+v, want {s1 host1 user1}", sessions[0])
	}
}

func TestHTTPClient_Execute(t *testing.T) {
	srv := &customAPIServer{token: "tok-123"}
	ts := srv.start(t)
	defer ts.Close()

	c := newHTTPCustomClient(ts.URL, srv.token)
	out, err := c.Execute(context.Background(), "s1", "whoami")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if srv.bearerSeen != "Bearer tok-123" {
		t.Errorf("bearer header = %q, want %q", srv.bearerSeen, "Bearer tok-123")
	}
	if out != "ran: whoami" {
		t.Errorf("output = %q, want %q", out, "ran: whoami")
	}
	if !strings.Contains(srv.lastExecBody, "whoami") {
		t.Errorf("exec body = %q, missing command", srv.lastExecBody)
	}
}

func TestHTTPClient_Execute_ErrorStatus(t *testing.T) {
	srv := &customAPIServer{token: "tok-123", failExec: true}
	ts := srv.start(t)
	defer ts.Close()

	c := newHTTPCustomClient(ts.URL, srv.token)
	_, err := c.Execute(context.Background(), "missing", "whoami")
	if err == nil {
		t.Fatal("expected error for non-2xx status, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v, want it to mention status 404", err)
	}
}

func TestHTTPClient_Cleanup(t *testing.T) {
	s := &customAPIServer{token: "tok"}
	srv := s.start(t)
	defer srv.Close()
	c := newHTTPCustomClient(srv.URL, "tok")

	if err := c.KillSession(context.Background(), "s1"); err != nil {
		t.Fatalf("KillSession: %v", err)
	}
	if s.killedSession != "s1" {
		t.Errorf("killed session = %q, want s1", s.killedSession)
	}
	if err := c.StopListener(context.Background(), "https-443"); err != nil {
		t.Fatalf("StopListener: %v", err)
	}
	if s.stoppedListener != "https-443" {
		t.Errorf("stopped listener = %q, want https-443", s.stoppedListener)
	}
}

func TestDeriveOperatorBaseURL(t *testing.T) {
	got := deriveOperatorBaseURL("10.0.0.5")
	want := "https://10.0.0.5:9443"
	if got != want {
		t.Errorf("deriveOperatorBaseURL = %q, want %q", got, want)
	}
}
