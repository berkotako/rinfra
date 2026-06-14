package metasploit

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rinfra/rinfra/internal/c2"
)

// This file is the live msfrpcd client: it drives the Metasploit RPC daemon
// over its MessagePack-over-HTTP protocol (POST /api/1.0/). The request shape is
// a MessagePack array [method, token, args...]; auth.login omits the token.
//
// RInfra COMPOSES the deployed Metasploit; it authors no modules or payloads.
// The protocol is plain HTTP + MessagePack (codec in msgpack.go), so the client
// logic is fully exercised in CI against an in-process msfrpcd stand-in
// (metasploit_live_test.go). The live seam CI cannot cover is the exact RPC
// method/field names vs. a live msfrpcd (pinned to MSF 6.4.x).

const msfRPCPath = "/api/1.0/"

// LiveConfig configures a live msfrpcd client.
type LiveConfig struct {
	BaseURL            string // e.g. https://10.0.0.5:55553
	Username           string
	Password           string
	InsecureSkipVerify bool         // msfrpcd uses a self-signed cert by default
	HTTPClient         *http.Client // optional override
}

type liveClient struct {
	url   string
	httpc *http.Client
	token string
}

// NewLiveClient connects to msfrpcd and authenticates.
func NewLiveClient(ctx context.Context, cfg LiveConfig) (MsfRpcdClient, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("metasploit: LiveConfig.BaseURL is required")
	}
	httpc := cfg.HTTPClient
	if httpc == nil {
		httpc = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: cfg.InsecureSkipVerify, //nolint:gosec // operator-controlled self-signed msfrpcd
					MinVersion:         tls.VersionTLS12,
				},
			},
		}
	}
	c := &liveClient{url: strings.TrimRight(cfg.BaseURL, "/") + msfRPCPath, httpc: httpc}
	if err := c.Auth(ctx, cfg.Username, cfg.Password); err != nil {
		return nil, err
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

// rpc issues a MessagePack RPC call and returns the decoded response map.
func (c *liveClient) rpc(ctx context.Context, parts ...any) (map[string]any, error) {
	body := msgpackEncode(parts)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("metasploit: build rpc request: %w", err)
	}
	req.Header.Set("Content-Type", "binary/message-pack")
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metasploit: rpc request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusInternalServerError {
		// msfrpcd returns 200 on success and 500 with an error map on failure;
		// any other code is a transport/proxy problem.
		return nil, fmt.Errorf("metasploit: rpc http %d", resp.StatusCode)
	}
	v, _, err := msgpackDecode(raw)
	if err != nil {
		return nil, fmt.Errorf("metasploit: decode rpc response: %w", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("metasploit: unexpected rpc response type %T", v)
	}
	if isErr, _ := m["error"].(bool); isErr {
		return nil, fmt.Errorf("metasploit: rpc error: %s", asString(m["error_message"]))
	}
	return m, nil
}

func (c *liveClient) Auth(ctx context.Context, user, pass string) error {
	m, err := c.rpc(ctx, "auth.login", user, pass)
	if err != nil {
		return err
	}
	token := asString(m["token"])
	if !strings.EqualFold(asString(m["result"]), "success") || token == "" {
		return fmt.Errorf("metasploit: authentication failed")
	}
	c.token = token
	return nil
}

func (c *liveClient) ConsoleCreate(ctx context.Context) (string, error) {
	m, err := c.rpc(ctx, "console.create", c.token)
	if err != nil {
		return "", err
	}
	id := asString(m["id"])
	if id == "" {
		return "", fmt.Errorf("metasploit: console.create returned no id")
	}
	return id, nil
}

func (c *liveClient) ConsoleWrite(ctx context.Context, consoleID, command string) error {
	_, err := c.rpc(ctx, "console.write", c.token, consoleID, command)
	return err
}

func (c *liveClient) ConsoleRead(ctx context.Context, consoleID string) (string, error) {
	m, err := c.rpc(ctx, "console.read", c.token, consoleID)
	if err != nil {
		return "", err
	}
	return asString(m["data"]), nil
}

func (c *liveClient) SessionList(ctx context.Context) ([]MsfSession, error) {
	m, err := c.rpc(ctx, "session.list", c.token)
	if err != nil {
		return nil, err
	}
	out := make([]MsfSession, 0, len(m))
	for id, v := range m {
		fields, ok := v.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, MsfSession{
			ID:         id,
			Type:       asString(fields["type"]),
			Info:       asString(fields["info"]),
			ViaExploit: asString(fields["via_exploit"]),
		})
	}
	return out, nil
}

func (c *liveClient) SessionShellWrite(ctx context.Context, sessionID, command string) error {
	_, err := c.rpc(ctx, "session.shell_write", c.token, sessionArg(sessionID), command)
	return err
}

func (c *liveClient) SessionShellRead(ctx context.Context, sessionID string) (string, error) {
	m, err := c.rpc(ctx, "session.shell_read", c.token, sessionArg(sessionID))
	if err != nil {
		return "", err
	}
	return asString(m["data"]), nil
}

// sessionArg passes a numeric session id as an integer (as msfrpcd expects),
// falling back to the raw string for non-numeric ids.
func sessionArg(sessionID string) any {
	if n, err := strconv.Atoi(sessionID); err == nil {
		return n
	}
	return sessionID
}
