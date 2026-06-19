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
	if _, err := svcAuth.SeedAdmin(context.Background(), "admin"); err != nil {
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

// ---- helpers for project-membership authorization tests ----

func mustCreateUser(t *testing.T, router http.Handler, adminTok, username, password, role string) string {
	t.Helper()
	resp := authedRequest(t, router, "POST", "/api/v1/users", adminTok, map[string]any{
		"username": username, "password": password, "role": role,
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create user %s: want 201, got %d: %s", username, resp.StatusCode, b)
	}
	var out map[string]any
	decodeBody(t, resp, &out)
	return out["user"].(map[string]any)["id"].(string)
}

func mustCreateProject(t *testing.T, router http.Handler, adminTok, name string) string {
	t.Helper()
	resp := authedRequest(t, router, "POST", "/api/v1/projects", adminTok, map[string]any{"name": name})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create project %s: want 201, got %d: %s", name, resp.StatusCode, b)
	}
	var out map[string]any
	decodeBody(t, resp, &out)
	return out["project"].(map[string]any)["id"].(string)
}

func mustAddMember(t *testing.T, router http.Handler, adminTok, projectID, userID string) {
	t.Helper()
	resp := authedRequest(t, router, "POST", "/api/v1/projects/"+projectID+"/members", adminTok,
		map[string]any{"userId": userID})
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("add member: got %d: %s", resp.StatusCode, b)
	}
}

func mustCreateEngagement(t *testing.T, router http.Handler, adminTok, projectID string) string {
	t.Helper()
	resp := authedRequest(t, router, "POST", "/api/v1/engagements", adminTok, map[string]any{
		"client": "Acme", "codename": "OP-AUTHZ", "projectId": projectID,
		"targets": []string{"10.0.0.0/8"},
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create engagement: want 201, got %d: %s", resp.StatusCode, b)
	}
	var out map[string]any
	decodeBody(t, resp, &out)
	return out["engagement"].(map[string]any)["id"].(string)
}

// TestEngagementProjectAuthorization proves an operator who is not a member of
// an engagement's project cannot read, update, deploy, teardown, upload
// credentials for, or run emulation against it — across every engagement-scoped
// route — while a member of the project can.
func TestEngagementProjectAuthorization(t *testing.T) {
	router := buildAuthRouter(t)
	admin := loginViaAPI(t, router, "admin", "admin")

	opAID := mustCreateUser(t, router, admin, "op-a", "operator-a-pw", "operator")
	opBID := mustCreateUser(t, router, admin, "op-b", "operator-b-pw", "operator")

	projA := mustCreateProject(t, router, admin, "Project A")
	projB := mustCreateProject(t, router, admin, "Project B")
	mustAddMember(t, router, admin, projA, opAID)
	mustAddMember(t, router, admin, projB, opBID)

	engA := mustCreateEngagement(t, router, admin, projA)

	tokA := loginViaAPI(t, router, "op-a", "operator-a-pw")
	tokB := loginViaAPI(t, router, "op-b", "operator-b-pw")

	// A member of project A can read its engagement.
	if resp := authedRequest(t, router, "GET", "/api/v1/engagements/"+engA, tokA, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("op-a GET own-project engagement: want 200, got %d", resp.StatusCode)
	}

	// op-b (project B) must be forbidden on every engagement-scoped operation.
	base := "/api/v1/engagements/" + engA
	cases := []struct {
		method, path string
		body         any
	}{
		{"GET", base, nil},
		{"PATCH", base, map[string]any{"status": "authorized"}},
		{"GET", base + "/topology", nil},
		{"PUT", base + "/topology", map[string]any{"nodes": []any{}, "edges": []any{}}},
		{"POST", base + "/validate", nil},
		{"POST", base + "/deploy", nil},
		{"POST", base + "/teardown", nil},
		{"PUT", base + "/credentials/aws", map[string]any{"values": map[string]string{"AWS_ACCESS_KEY_ID": "x"}}},
		{"GET", base + "/audit", nil},
		{"POST", base + "/runs", map[string]any{"scenarioId": "apt29"}},
		{"GET", base + "/coverage", nil},
		{"GET", base + "/c2/manual-access", nil},
		{"GET", base + "/c2/teamservers", nil},
	}
	for _, c := range cases {
		resp := authedRequest(t, router, c.method, c.path, tokB, c.body)
		if resp.StatusCode != http.StatusForbidden {
			b, _ := io.ReadAll(resp.Body)
			t.Errorf("op-b %s %s: want 403, got %d: %s", c.method, c.path, resp.StatusCode, b)
		}
	}
}
