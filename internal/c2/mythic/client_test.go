package mythic_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mythicpkg "github.com/rinfra/rinfra/internal/c2/mythic"
)

// httpMythicServer stands up the /auth + /graphql endpoints that the live
// httpMythicClient drives. It records whether /auth was hit and asserts the
// bearer token is presented on every /graphql call once login has completed.
type httpMythicServer struct {
	token       string
	authHits    int32
	bearerSeen  int32
	bearerMatch int32
	failGraphQL bool // when true, /graphql returns a GraphQL error payload
}

func (s *httpMythicServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&s.authHits, 1)
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		if body.Username == "" || body.Password == "" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"missing credentials"}`))
			return
		}
		writeJSONResp(w, map[string]any{"access_token": s.token, "refresh_token": "refresh-xyz"})
	})

	mux.HandleFunc("/graphql/", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "" {
			atomic.AddInt32(&s.bearerSeen, 1)
			if auth == "Bearer "+s.token {
				atomic.AddInt32(&s.bearerMatch, 1)
			} else {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}

		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.Unmarshal(raw, &req)

		if s.failGraphQL {
			writeJSONResp(w, map[string]any{"errors": []map[string]any{{"message": "boom: field not found"}}})
			return
		}

		switch {
		case strings.Contains(req.Query, "callback(where"):
			writeGraphData(w, map[string]any{"callback": []map[string]any{
				{"id": 9, "host": "dc01", "user": "CORP\\bob", "os": "windows", "architecture": "x64"},
			}})
		case strings.Contains(req.Query, "createTask"):
			writeGraphData(w, map[string]any{"createTask": map[string]any{"status": "success", "id": 101, "error": ""}})
		case strings.Contains(req.Query, "task_by_pk"):
			writeGraphData(w, map[string]any{"task_by_pk": map[string]any{"id": 101, "status": "completed", "completed": true}})
		case strings.Contains(req.Query, "response(where"):
			enc := base64.StdEncoding.EncodeToString([]byte("output bytes"))
			writeGraphData(w, map[string]any{"response": []map[string]any{{"response_text": enc}}})
		case strings.Contains(req.Query, "c2profile(where"):
			writeGraphData(w, map[string]any{"c2profile": []map[string]any{{"id": 1, "name": "http", "running": true}}})
		default:
			writeGraphData(w, map[string]any{})
		}
	})

	return httptest.NewServer(mux)
}

func writeJSONResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeGraphData(w http.ResponseWriter, data any) {
	writeJSONResp(w, map[string]any{"data": data})
}

func newHTTPMythicClient(t *testing.T, s *httptest.Server, srvURL string) mythicpkg.MythicClient {
	t.Helper()
	return mythicpkg.NewHTTPClient(mythicpkg.LiveConfig{
		BaseURL:      srvURL,
		Username:     "mythic_admin",
		Password:     "operator-pw",
		PollInterval: 5 * time.Millisecond,
		HTTPClient:   s.Client(),
	})
}

func TestHTTPClient_RoundTripsAllMethods(t *testing.T) {
	srvSpec := &httpMythicServer{token: "jwt-abc"}
	srv := srvSpec.start(t)
	defer srv.Close()

	client := newHTTPMythicClient(t, srv, srv.URL)
	ctx := context.Background()

	// Auth is lazy: nothing should have hit /auth before the first call.
	if got := atomic.LoadInt32(&srvSpec.authHits); got != 0 {
		t.Fatalf("auth hit %d times before any API call, want 0 (lazy auth)", got)
	}

	t.Run("Callbacks", func(t *testing.T) {
		cbs, err := client.Callbacks(ctx)
		if err != nil {
			t.Fatalf("Callbacks: %v", err)
		}
		if len(cbs) != 1 || cbs[0].ID != "9" || cbs[0].Host != "dc01" || cbs[0].Arch != "x64" {
			t.Fatalf("unexpected callbacks: %+v", cbs)
		}
	})

	t.Run("IssueTasking", func(t *testing.T) {
		id, err := client.IssueTasking(ctx, "9", "shell", map[string]string{"command": "whoami"})
		if err != nil {
			t.Fatalf("IssueTasking: %v", err)
		}
		if id != "101" {
			t.Fatalf("task id = %q, want 101", id)
		}
	})

	t.Run("TaskOutput", func(t *testing.T) {
		out, err := client.TaskOutput(ctx, "101")
		if err != nil {
			t.Fatalf("TaskOutput: %v", err)
		}
		if out != "output bytes" {
			t.Fatalf("output = %q, want decoded %q", out, "output bytes")
		}
	})

	t.Run("CreateListener", func(t *testing.T) {
		if err := client.CreateListener(ctx, "http", "0.0.0.0", 443); err != nil {
			t.Fatalf("CreateListener: %v", err)
		}
	})

	t.Run("CreateCallback_operatorRejects", func(t *testing.T) {
		// Callbacks are created by agents checking in, not by the operator API;
		// the live client surfaces this as an error rather than fabricating one.
		if _, err := client.CreateCallback(ctx, "h", "u", "windows"); err == nil {
			t.Fatal("expected CreateCallback to be rejected by the operator API")
		}
	})

	// Login happened exactly once and every /graphql call carried the bearer.
	if got := atomic.LoadInt32(&srvSpec.authHits); got != 1 {
		t.Fatalf("auth hit %d times, want exactly 1 (cached token)", got)
	}
	if seen, match := atomic.LoadInt32(&srvSpec.bearerSeen), atomic.LoadInt32(&srvSpec.bearerMatch); seen == 0 || seen != match {
		t.Fatalf("bearer header seen=%d match=%d; want all graphql calls to carry the login token", seen, match)
	}
}

func TestHTTPClient_AuthFailureSurfaces(t *testing.T) {
	srvSpec := &httpMythicServer{token: "jwt"}
	srv := srvSpec.start(t)
	defer srv.Close()

	// Empty password makes /auth return 403; the error must propagate out of the
	// first API call (lazy auth).
	client := mythicpkg.NewHTTPClient(mythicpkg.LiveConfig{
		BaseURL:    srv.URL,
		Username:   "mythic_admin",
		Password:   "",
		HTTPClient: srv.Client(),
	})
	if _, err := client.Callbacks(context.Background()); err == nil {
		t.Fatal("expected auth failure to surface as a Go error")
	}
}

func TestHTTPClient_GraphQLErrorBecomesGoError(t *testing.T) {
	srvSpec := &httpMythicServer{token: "jwt", failGraphQL: true}
	srv := srvSpec.start(t)
	defer srv.Close()

	client := newHTTPMythicClient(t, srv, srv.URL)
	_, err := client.Callbacks(context.Background())
	if err == nil || !strings.Contains(err.Error(), "field not found") {
		t.Fatalf("expected propagated graphql error, got %v", err)
	}
}
