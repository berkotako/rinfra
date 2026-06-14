package mythic

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rinfra/rinfra/internal/c2"
)

// This file is the live operator client: it drives Mythic's scripting API over
// HTTPS. Mythic 3.x exposes a Hasura GraphQL endpoint (/graphql/) guarded by a
// JWT obtained from the /auth login endpoint (or a pre-issued API token).
//
// # Posture
//
// RInfra COMPOSES the deployed Mythic instance; it authors no agents or
// payloads. The queries/mutations below are pinned to Mythic v3.3.1's schema
// (the version Deploy installs). The wire format is plain JSON, so the client
// logic is fully exercised in CI against an in-process HTTP server
// (mythic_live_test.go). The one seam CI cannot cover is GraphQL field/mutation
// names vs. a live Mythic; if the deployed version changes its schema, update
// the query strings here in lockstep.

// LiveConfig configures a live Mythic client. Provide either APIToken or
// Username+Password. Mythic instances commonly use self-signed certs, so
// InsecureSkipVerify is exposed (default: verify) — set it per engagement.
type LiveConfig struct {
	BaseURL            string // e.g. https://10.0.0.5:7443
	Username           string
	Password           string
	APIToken           string
	InsecureSkipVerify bool
	HTTPClient         *http.Client  // optional; overrides the default
	PollInterval       time.Duration // task-completion poll cadence
}

type liveClient struct {
	baseURL string
	httpc   *http.Client
	token   string
	poll    time.Duration
}

// NewLiveClient authenticates to Mythic and returns a live MythicClient.
func NewLiveClient(ctx context.Context, cfg LiveConfig) (MythicClient, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("mythic: LiveConfig.BaseURL is required")
	}
	httpc := cfg.HTTPClient
	if httpc == nil {
		httpc = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: cfg.InsecureSkipVerify, //nolint:gosec // operator-controlled, self-signed Mythic
					MinVersion:         tls.VersionTLS12,
				},
			},
		}
	}
	poll := cfg.PollInterval
	if poll <= 0 {
		poll = 500 * time.Millisecond
	}
	c := &liveClient{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		httpc:   httpc,
		token:   cfg.APIToken,
		poll:    poll,
	}
	if c.token == "" {
		if cfg.Username == "" {
			return nil, fmt.Errorf("mythic: provide APIToken or Username/Password")
		}
		if err := c.authenticate(ctx, cfg.Username, cfg.Password); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// LiveOperator composes a live client into a ready Operator for the service layer.
func LiveOperator(ctx context.Context, ts c2.Teamserver, cfg LiveConfig) (c2.Operator, error) {
	client, err := NewLiveClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return NewOperatorWithClient(ts, client), nil
}

func (c *liveClient) authenticate(ctx context.Context, user, pass string) error {
	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/auth", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mythic: build auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("mythic: auth request: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mythic: auth failed (http %d): %s", resp.StatusCode, snippet(data))
	}
	var ar struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &ar); err != nil {
		return fmt.Errorf("mythic: decode auth response: %w", err)
	}
	if ar.AccessToken == "" {
		return fmt.Errorf("mythic: auth response missing access_token")
	}
	c.token = ar.AccessToken
	return nil
}

// gql issues a GraphQL query/mutation and unmarshals the data field into out.
func (c *liveClient) gql(ctx context.Context, query string, vars map[string]any, out any) error {
	reqBody, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/graphql/", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("mythic: build graphql request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("mythic: graphql request: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mythic: graphql http %d: %s", resp.StatusCode, snippet(data))
	}
	var gr struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &gr); err != nil {
		return fmt.Errorf("mythic: decode graphql response: %w", err)
	}
	if len(gr.Errors) > 0 {
		return fmt.Errorf("mythic: graphql error: %s", gr.Errors[0].Message)
	}
	if out != nil && len(gr.Data) > 0 {
		if err := json.Unmarshal(gr.Data, out); err != nil {
			return fmt.Errorf("mythic: decode graphql data: %w", err)
		}
	}
	return nil
}

// CreateCallback is not a live operator action: callbacks are created by agents
// checking in to a listener. It exists on the interface for test fakes.
func (c *liveClient) CreateCallback(_ context.Context, _, _, _ string) (string, error) {
	return "", fmt.Errorf("mythic: callbacks are created by agents checking in, not via the operator API")
}

const callbacksQuery = `query Callbacks {
  callback(where: {active: {_eq: true}}) {
    id
    host
    user
    os
    architecture
  }
}`

func (c *liveClient) Callbacks(ctx context.Context) ([]MythicCallback, error) {
	var out struct {
		Callback []struct {
			ID           int    `json:"id"`
			Host         string `json:"host"`
			User         string `json:"user"`
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
		} `json:"callback"`
	}
	if err := c.gql(ctx, callbacksQuery, nil, &out); err != nil {
		return nil, err
	}
	cbs := make([]MythicCallback, 0, len(out.Callback))
	for _, cb := range out.Callback {
		cbs = append(cbs, MythicCallback{
			ID:   strconv.Itoa(cb.ID),
			Host: cb.Host,
			User: cb.User,
			OS:   cb.OS,
			Arch: cb.Architecture,
		})
	}
	return cbs, nil
}

const createTaskMutation = `mutation IssueTask($callback_id: Int!, $command: String!, $params: String!) {
  createTask(callback_id: $callback_id, command: $command, params: $params) {
    status
    id
    error
  }
}`

func (c *liveClient) IssueTasking(ctx context.Context, callbackID, command string, params map[string]string) (string, error) {
	cbID, err := strconv.Atoi(callbackID)
	if err != nil {
		return "", fmt.Errorf("mythic: invalid callback id %q: %w", callbackID, err)
	}
	paramStr := ""
	if len(params) > 0 {
		paramStr = paramsToJSON(params)
	}
	var out struct {
		CreateTask struct {
			Status string `json:"status"`
			ID     int    `json:"id"`
			Error  string `json:"error"`
		} `json:"createTask"`
	}
	vars := map[string]any{"callback_id": cbID, "command": command, "params": paramStr}
	if err := c.gql(ctx, createTaskMutation, vars, &out); err != nil {
		return "", err
	}
	if strings.EqualFold(out.CreateTask.Status, "error") || out.CreateTask.Error != "" {
		return "", fmt.Errorf("mythic: createTask rejected: %s", out.CreateTask.Error)
	}
	return strconv.Itoa(out.CreateTask.ID), nil
}

const taskStatusQuery = `query TaskStatus($id: Int!) {
  task_by_pk(id: $id) {
    id
    status
    completed
  }
}`

const taskResponsesQuery = `query TaskResponses($id: Int!) {
  response(where: {task_id: {_eq: $id}}, order_by: {id: asc}) {
    response_text
  }
}`

func (c *liveClient) TaskOutput(ctx context.Context, taskID string) (string, error) {
	id, err := strconv.Atoi(taskID)
	if err != nil {
		return "", fmt.Errorf("mythic: invalid task id %q: %w", taskID, err)
	}
	// Poll until the task completes or the context expires.
	for {
		var st struct {
			Task *struct {
				Status    string `json:"status"`
				Completed bool   `json:"completed"`
			} `json:"task_by_pk"`
		}
		if err := c.gql(ctx, taskStatusQuery, map[string]any{"id": id}, &st); err != nil {
			return "", err
		}
		if st.Task != nil && st.Task.Completed {
			break
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("mythic: waiting for task %d: %w", id, ctx.Err())
		case <-time.After(c.poll):
		}
	}

	var rr struct {
		Response []struct {
			ResponseText string `json:"response_text"`
		} `json:"response"`
	}
	if err := c.gql(ctx, taskResponsesQuery, map[string]any{"id": id}, &rr); err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, r := range rr.Response {
		sb.WriteString(decodeMythicResponse(r.ResponseText))
	}
	return sb.String(), nil
}

const c2ProfileQuery = `query C2Profile($name: String!) {
  c2profile(where: {name: {_eq: $name}}) {
    id
    name
    running
  }
}`

// CreateListener ensures the named Mythic C2 profile is available and running.
// Mythic C2 profiles run as docker containers started at deploy time; this
// verifies the profile the engagement expects is up rather than creating ad-hoc
// listeners (which Mythic does not expose the way Sliver does).
func (c *liveClient) CreateListener(ctx context.Context, profileName, _ string, _ int) error {
	var out struct {
		C2Profile []struct {
			ID      int    `json:"id"`
			Name    string `json:"name"`
			Running bool   `json:"running"`
		} `json:"c2profile"`
	}
	if err := c.gql(ctx, c2ProfileQuery, map[string]any{"name": profileName}, &out); err != nil {
		return err
	}
	if len(out.C2Profile) == 0 {
		return fmt.Errorf("mythic: c2 profile %q not found on teamserver", profileName)
	}
	if !out.C2Profile[0].Running {
		return fmt.Errorf("mythic: c2 profile %q is installed but not running; start it on the Mythic server", profileName)
	}
	return nil
}

// decodeMythicResponse returns the response text, base64-decoding it when Mythic
// returns it encoded (the raw response column), else the value as-is.
func decodeMythicResponse(s string) string {
	if s == "" {
		return ""
	}
	if dec, err := base64.StdEncoding.DecodeString(s); err == nil {
		return string(dec)
	}
	return s
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
