package custom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rinfra/rinfra/internal/c2"
)

// EnvCustomAPIToken is the environment variable holding the per-engagement
// bearer token for the custom framework's operator API. The service layer
// exports it before invoking a control path; it is never bundled.
const EnvCustomAPIToken = "RINFRA_CUSTOM_API_TOKEN"

// httpCustomClient is the live CustomClient over RInfra's in-house operator
// REST + JSON API. See the package doc for the contract. It is a thin,
// dependency-free net/http client: every call carries the bearer token and any
// non-2xx response becomes a Go error.
type httpCustomClient struct {
	baseURL string // e.g. "https://10.0.0.5:9443" — no trailing slash
	token   string
	hc      *http.Client
}

// newHTTPCustomClient builds a client against baseURL with the given bearer
// token. baseURL is normalized (trailing slash trimmed).
func newHTTPCustomClient(baseURL, token string) *httpCustomClient {
	return &httpCustomClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		hc:      &http.Client{Timeout: 30 * time.Second},
	}
}

// newHTTPClientForTeamserver derives the operator API base URL from the
// deployed teamserver and constructs a live client. The operator API listens on
// customOperatorAPIPort over HTTPS (see customOperatorAPIPort); the bearer token
// is read from the per-engagement environment (EnvCustomAPIToken).
func newHTTPClientForTeamserver(ts c2.Teamserver) CustomClient {
	host := ts.Host
	if host == "" {
		host = "127.0.0.1"
	}
	base := deriveOperatorBaseURL(host)
	return newHTTPCustomClient(base, os.Getenv(EnvCustomAPIToken))
}

// deriveOperatorBaseURL builds the operator API base URL for a teamserver host.
func deriveOperatorBaseURL(host string) string {
	return fmt.Sprintf("https://%s:%d", host, customOperatorAPIPort)
}

// StartListener creates an implant listener on the teamserver.
// POST /api/v1/listeners {name,protocol,bind,port}
func (c *httpCustomClient) StartListener(ctx context.Context, protocol, host string, port int) error {
	body := map[string]any{
		"name":     fmt.Sprintf("%s-%d", protocol, port),
		"protocol": protocol,
		"bind":     host,
		"port":     port,
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/listeners", body, nil); err != nil {
		return fmt.Errorf("custom: start listener: %w", err)
	}
	return nil
}

// Sessions lists active implant sessions.
// GET /api/v1/sessions -> [{id,host,user}]
func (c *httpCustomClient) Sessions(ctx context.Context) ([]CustomSession, error) {
	var raw []struct {
		ID   string `json:"id"`
		Host string `json:"host"`
		User string `json:"user"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/sessions", nil, &raw); err != nil {
		return nil, fmt.Errorf("custom: sessions: %w", err)
	}
	out := make([]CustomSession, 0, len(raw))
	for _, s := range raw {
		out = append(out, CustomSession{ID: s.ID, Hostname: s.Host, Username: s.User})
	}
	return out, nil
}

// Execute runs a command against a session and returns its output.
// POST /api/v1/sessions/{id}/exec {command} -> {output}
func (c *httpCustomClient) Execute(ctx context.Context, sessionID, command string) (string, error) {
	path := "/api/v1/sessions/" + url.PathEscape(sessionID) + "/exec"
	var resp struct {
		Output string `json:"output"`
	}
	if err := c.do(ctx, http.MethodPost, path, map[string]any{"command": command}, &resp); err != nil {
		return "", fmt.Errorf("custom: exec: %w", err)
	}
	return resp.Output, nil
}

// KillSession terminates an implant session.
// DELETE /api/v1/sessions/{id}
func (c *httpCustomClient) KillSession(ctx context.Context, sessionID string) error {
	path := "/api/v1/sessions/" + url.PathEscape(sessionID)
	if err := c.do(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return fmt.Errorf("custom: kill session: %w", err)
	}
	return nil
}

// StopListener removes a listener.
// DELETE /api/v1/listeners/{id}
func (c *httpCustomClient) StopListener(ctx context.Context, listenerID string) error {
	path := "/api/v1/listeners/" + url.PathEscape(listenerID)
	if err := c.do(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return fmt.Errorf("custom: stop listener: %w", err)
	}
	return nil
}

// do performs an authenticated JSON request. reqBody, if non-nil, is marshaled
// as the request body; out, if non-nil, receives the decoded 2xx response.
// Non-2xx responses are returned as Go errors that include the status and any
// response body.
func (c *httpCustomClient) do(ctx context.Context, method, path string, reqBody, out any) error {
	var rdr io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		rdr = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("%s %s: unexpected status %s: %s", method, path, strconv.Itoa(resp.StatusCode), msg)
	}

	if out != nil {
		if len(body) == 0 {
			return fmt.Errorf("%s %s: empty response body", method, path)
		}
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
