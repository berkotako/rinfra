package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rinfra/rinfra/internal/api"
	"github.com/rinfra/rinfra/internal/service"
	"github.com/rinfra/rinfra/internal/store/memstore"
)

// buildAuthRouter wires a router WITH the auth + project subsystem enabled and a
// seeded admin/admin account.
func buildAuthRouter(t *testing.T) http.Handler {
	t.Helper()
	auditLog := memstore.NewAuditLogger()
	engStore := memstore.NewEngagementStore()
	infraStore := memstore.NewInfraStore()
	scenarioStore := memstore.NewScenarioStore()
	credStore := memstore.NewCredentialStore()
	jobStore := memstore.NewJobStore()
	userStore := memstore.NewUserStore()
	projectStore := memstore.NewProjectStore()
	sessionStore := memstore.NewSessionStore()
	hub := service.NewHub()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	svcEng := service.NewEngagementService(engStore, auditLog)
	svcInfra := service.NewInfraService(engStore, infraStore, credStore, jobStore, auditLog, testEnc(t), hub, log)
	svcEmu := service.NewEmulationService(engStore, scenarioStore, auditLog, hub)
	svcAuth := service.NewAuthService(userStore, sessionStore, auditLog, log)
	svcProject := service.NewProjectService(projectStore, userStore, auditLog)
	if _, err := svcAuth.SeedAdmin(context.Background()); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	return api.NewRouter(api.Services{
		Engagement: svcEng,
		Infra:      svcInfra,
		Emulation:  svcEmu,
		Hub:        hub,
		AuditLog:   auditLog,
		Auth:       svcAuth,
		Projects:   svcProject,
	}, log)
}

func authedRequest(t *testing.T, router http.Handler, method, path, token string, body any) *http.Response {
	t.Helper()
	var br io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		br = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, br)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Result()
}

func loginViaAPI(t *testing.T, router http.Handler, username, password string) string {
	t.Helper()
	resp := authedRequest(t, router, "POST", "/api/v1/auth/login", "", map[string]any{
		"username": username, "password": password,
	})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("login: want 200, got %d: %s", resp.StatusCode, b)
	}
	var out map[string]any
	decodeBody(t, resp, &out)
	return out["token"].(string)
}

func TestAPI_AuthEnforcedAndLoginFlow(t *testing.T) {
	router := buildAuthRouter(t)

	// Protected route without a token → 401.
	resp := authedRequest(t, router, "GET", "/api/v1/engagements", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-token engagements: want 401, got %d", resp.StatusCode)
	}

	// Bad credentials → 401 invalid_credentials.
	resp = authedRequest(t, router, "POST", "/api/v1/auth/login", "", map[string]any{
		"username": "admin", "password": "nope",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad login: want 401, got %d", resp.StatusCode)
	}

	// Good login → token; /auth/me reflects the admin role.
	token := loginViaAPI(t, router, "admin", "admin")

	resp = authedRequest(t, router, "GET", "/api/v1/auth/me", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me: want 200, got %d", resp.StatusCode)
	}
	var me map[string]any
	decodeBody(t, resp, &me)
	if me["user"].(map[string]any)["role"] != "admin" {
		t.Fatalf("me role = %v, want admin", me["user"])
	}

	// Authenticated admin can list engagements.
	resp = authedRequest(t, router, "GET", "/api/v1/engagements", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authed engagements: want 200, got %d", resp.StatusCode)
	}
}

func TestAPI_ProjectAndRoleFlow(t *testing.T) {
	router := buildAuthRouter(t)
	adminToken := loginViaAPI(t, router, "admin", "admin")

	// Admin creates a lead user.
	resp := authedRequest(t, router, "POST", "/api/v1/users", adminToken, map[string]any{
		"username": "lead1", "role": "lead", "password": "pw1234",
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create lead: want 201, got %d: %s", resp.StatusCode, b)
	}

	// Admin creates a project.
	resp = authedRequest(t, router, "POST", "/api/v1/projects", adminToken, map[string]any{
		"name": "Apollo", "clientName": "Acme",
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create project: want 201, got %d: %s", resp.StatusCode, b)
	}

	// The lead logs in; an operator created by the lead cannot create users.
	leadToken := loginViaAPI(t, router, "lead1", "pw1234")
	resp = authedRequest(t, router, "POST", "/api/v1/users", leadToken, map[string]any{
		"username": "op1", "role": "operator", "password": "pw1234",
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("lead create operator: want 201, got %d: %s", resp.StatusCode, b)
	}

	// A lead may not create another lead → 403 forbidden.
	resp = authedRequest(t, router, "POST", "/api/v1/users", leadToken, map[string]any{
		"username": "lead2", "role": "lead", "password": "pw1234",
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("lead create lead: want 403, got %d", resp.StatusCode)
	}

	// Operator login; operators see only themselves in the user list.
	opToken := loginViaAPI(t, router, "op1", "pw1234")
	resp = authedRequest(t, router, "GET", "/api/v1/users", opToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("op list users: want 200, got %d", resp.StatusCode)
	}
	var out map[string]any
	decodeBody(t, resp, &out)
	if users, ok := out["users"].([]any); !ok || len(users) != 1 {
		t.Fatalf("operator should see 1 user, got %v", out["users"])
	}
}
