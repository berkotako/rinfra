package mythic_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rinfra/rinfra/internal/c2"
	mythicpkg "github.com/rinfra/rinfra/internal/c2/mythic"
)

// mythicMock is an in-process stand-in for Mythic's /auth + /graphql endpoints.
type mythicMock struct {
	token      string
	lastVars   map[string]any
	failAuth   bool
	gqlError   string // if set, /graphql returns this as a GraphQL error
	wantBearer bool   // assert Authorization header on /graphql
}

func (m *mythicMock) server(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		if m.failAuth {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"invalid credentials"}`))
			return
		}
		writeJSON(w, map[string]any{"access_token": m.token})
	})
	mux.HandleFunc("/graphql/", func(w http.ResponseWriter, r *http.Request) {
		if m.wantBearer && r.Header.Get("Authorization") != "Bearer "+m.token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.Unmarshal(body, &req)
		m.lastVars = req.Variables

		if m.gqlError != "" {
			writeJSON(w, map[string]any{"errors": []map[string]any{{"message": m.gqlError}}})
			return
		}

		switch {
		case strings.Contains(req.Query, "callback(where"):
			writeData(w, map[string]any{"callback": []map[string]any{
				{"id": 7, "host": "ws01", "user": "CORP\\alice", "os": "windows", "architecture": "x64"},
			}})
		case strings.Contains(req.Query, "createTask"):
			writeData(w, map[string]any{"createTask": map[string]any{"status": "success", "id": 42, "error": ""}})
		case strings.Contains(req.Query, "task_by_pk"):
			writeData(w, map[string]any{"task_by_pk": map[string]any{"id": 42, "status": "completed", "completed": true}})
		case strings.Contains(req.Query, "response(where"):
			enc := base64.StdEncoding.EncodeToString([]byte("hello from agent"))
			writeData(w, map[string]any{"response": []map[string]any{{"response_text": enc}}})
		case strings.Contains(req.Query, "c2profile(where"):
			writeData(w, map[string]any{"c2profile": []map[string]any{{"id": 1, "name": "http", "running": true}}})
		default:
			writeData(w, map[string]any{})
		}
	})
	return httptest.NewServer(mux)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeData(w http.ResponseWriter, data any) {
	writeJSON(w, map[string]any{"data": data})
}

func newTestClient(t *testing.T, m *mythicMock) (mythicpkg.MythicClient, *httptest.Server) {
	t.Helper()
	srv := m.server(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := mythicpkg.NewLiveClient(ctx, mythicpkg.LiveConfig{
		BaseURL:      srv.URL,
		Username:     "mythic_admin",
		Password:     "pw",
		PollInterval: 5 * time.Millisecond,
		HTTPClient:   srv.Client(),
	})
	if err != nil {
		srv.Close()
		t.Fatalf("NewLiveClient: %v", err)
	}
	return client, srv
}

func TestLiveClient_AuthAndFlow(t *testing.T) {
	m := &mythicMock{token: "tok-123", wantBearer: true}
	client, srv := newTestClient(t, m)
	defer srv.Close()
	ctx := context.Background()

	t.Run("Callbacks", func(t *testing.T) {
		cbs, err := client.Callbacks(ctx)
		if err != nil {
			t.Fatalf("Callbacks: %v", err)
		}
		if len(cbs) != 1 || cbs[0].ID != "7" || cbs[0].OS != "windows" || cbs[0].Arch != "x64" {
			t.Fatalf("unexpected callbacks: %+v", cbs)
		}
	})

	t.Run("IssueTasking", func(t *testing.T) {
		taskID, err := client.IssueTasking(ctx, "7", "shell", map[string]string{"command": "whoami"})
		if err != nil {
			t.Fatalf("IssueTasking: %v", err)
		}
		if taskID != "42" {
			t.Fatalf("task id = %q, want 42", taskID)
		}
		// callback_id must be sent as a JSON number, not a string.
		if v, ok := m.lastVars["callback_id"].(float64); !ok || int(v) != 7 {
			t.Fatalf("callback_id var = %v (%T), want 7", m.lastVars["callback_id"], m.lastVars["callback_id"])
		}
		if m.lastVars["command"] != "shell" {
			t.Fatalf("command var = %v, want shell", m.lastVars["command"])
		}
	})

	t.Run("TaskOutput", func(t *testing.T) {
		out, err := client.TaskOutput(ctx, "42")
		if err != nil {
			t.Fatalf("TaskOutput: %v", err)
		}
		if out != "hello from agent" {
			t.Fatalf("output = %q, want decoded agent text", out)
		}
	})

	t.Run("CreateListener_runningProfile", func(t *testing.T) {
		if err := client.CreateListener(ctx, "http", "0.0.0.0", 443); err != nil {
			t.Fatalf("CreateListener: %v", err)
		}
	})
}

func TestLiveClient_AuthFailure(t *testing.T) {
	m := &mythicMock{token: "tok", failAuth: true}
	srv := m.server(t)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := mythicpkg.NewLiveClient(ctx, mythicpkg.LiveConfig{
		BaseURL:    srv.URL,
		Username:   "bad",
		Password:   "creds",
		HTTPClient: srv.Client(),
	})
	if err == nil {
		t.Fatal("expected auth failure error")
	}
}

func TestLiveClient_GraphQLErrorPropagates(t *testing.T) {
	m := &mythicMock{token: "tok", gqlError: "field 'callback' not found"}
	client, srv := newTestClient(t, m)
	defer srv.Close()
	if _, err := client.Callbacks(context.Background()); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected propagated graphql error, got %v", err)
	}
}

func TestLiveOperator_DrivesEmulation(t *testing.T) {
	m := &mythicMock{token: "tok-xyz", wantBearer: true}
	srv := m.server(t)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	op, err := mythicpkg.LiveOperator(ctx, c2.Teamserver{Host: "10.0.0.5", Port: 7443}, mythicpkg.LiveConfig{
		BaseURL:      srv.URL,
		Username:     "mythic_admin",
		Password:     "pw",
		PollInterval: 5 * time.Millisecond,
		HTTPClient:   srv.Client(),
	})
	if err != nil {
		t.Fatalf("LiveOperator: %v", err)
	}
	sessions, err := op.Sessions(ctx)
	if err != nil {
		t.Fatalf("operator.Sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "7" {
		t.Fatalf("unexpected sessions through operator: %+v", sessions)
	}
}
