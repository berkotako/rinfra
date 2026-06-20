package mythic

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/rinfra/rinfra/internal/c2"
)

// Environment variable names for the operator credentials used when Control()
// builds a live client against a deployed teamserver. These mirror the SSH
// runner's env convention (deploy.NewNodeRunner): the service layer exports
// per-engagement Mythic operator credentials before driving emulation. Mythic
// is open source, so there is no license key — just the operator login set
// during install (mythic-cli config), or a pre-issued API token.
const (
	EnvMythicUser     = "RINFRA_MYTHIC_USER"
	EnvMythicPassword = "RINFRA_MYTHIC_PASSWORD"
	EnvMythicToken    = "RINFRA_MYTHIC_API_TOKEN"
	// EnvMythicInsecureTLS, when set to a truthy value, skips TLS verification.
	// Mythic ships a self-signed cert by default, so engagements commonly set
	// this for the operator API hop (which rides the internal tunnel, not the
	// open internet).
	EnvMythicInsecureTLS = "RINFRA_MYTHIC_INSECURE_TLS"
)

// httpMythicClient is the live MythicClient over HTTP+JSON. It authenticates to
// Mythic's /auth endpoint for a bearer JWT and then drives the Hasura /graphql/
// endpoint, reusing the liveClient implementation (mythic_live.go) for the wire
// mechanics. Authentication is performed lazily on the first API call so that
// the context-free Control() hook can build a client cheaply; every interface
// method ensures the bearer token is present before issuing its request.
type httpMythicClient struct {
	cfg LiveConfig

	mu       sync.Mutex
	delegate MythicClient // built+authenticated on first use
}

// NewHTTPClient returns a live MythicClient for the given base URL and
// credentials. Authentication is deferred to the first API call. Provide either
// an API token or username+password in cfg.
func NewHTTPClient(cfg LiveConfig) MythicClient {
	return &httpMythicClient{cfg: cfg}
}

// newHTTPClientForTeamserver builds a live client pointed at a deployed Mythic
// teamserver. The base URL is derived from the teamserver host/port (Mythic's
// HTTPS UI/API, default 7443); operator credentials come from the
// per-engagement environment.
func newHTTPClientForTeamserver(ts c2.Teamserver) MythicClient {
	port := ts.Port
	if port == 0 {
		port = mythicPort
	}
	cfg := LiveConfig{
		BaseURL:            "https://" + net.JoinHostPort(ts.Host, strconv.Itoa(port)),
		Username:           os.Getenv(EnvMythicUser),
		Password:           os.Getenv(EnvMythicPassword),
		APIToken:           os.Getenv(EnvMythicToken),
		InsecureSkipVerify: isTruthy(os.Getenv(EnvMythicInsecureTLS)),
	}
	return NewHTTPClient(cfg)
}

// ensure builds and authenticates the underlying live client once, using the
// first caller's context for the /auth round-trip.
func (c *httpMythicClient) ensure(ctx context.Context) (MythicClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.delegate != nil {
		return c.delegate, nil
	}
	d, err := NewLiveClient(ctx, c.cfg)
	if err != nil {
		return nil, fmt.Errorf("mythic: connect to teamserver: %w", err)
	}
	c.delegate = d
	return d, nil
}

func (c *httpMythicClient) CreateCallback(ctx context.Context, host, user, os string) (string, error) {
	d, err := c.ensure(ctx)
	if err != nil {
		return "", err
	}
	return d.CreateCallback(ctx, host, user, os)
}

func (c *httpMythicClient) Callbacks(ctx context.Context) ([]MythicCallback, error) {
	d, err := c.ensure(ctx)
	if err != nil {
		return nil, err
	}
	return d.Callbacks(ctx)
}

func (c *httpMythicClient) IssueTasking(ctx context.Context, callbackID, command string, params map[string]string) (string, error) {
	d, err := c.ensure(ctx)
	if err != nil {
		return "", err
	}
	return d.IssueTasking(ctx, callbackID, command, params)
}

func (c *httpMythicClient) TaskOutput(ctx context.Context, taskID string) (string, error) {
	d, err := c.ensure(ctx)
	if err != nil {
		return "", err
	}
	return d.TaskOutput(ctx, taskID)
}

func (c *httpMythicClient) CreateListener(ctx context.Context, profileName, bindAddress string, port int) error {
	d, err := c.ensure(ctx)
	if err != nil {
		return err
	}
	return d.CreateListener(ctx, profileName, bindAddress, port)
}

func isTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
