// Live SSH Runner implementation. This is the production execution seam used by
// every C2 adapter: it opens a real SSH connection to a provisioned node and
// runs install scripts / uploads files. Tests in this package exercise it
// against an in-process SSH server (runner_live_test.go), so the wiring is
// verified without any external host.
package deploy

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// syncBuffer is a goroutine-safe buffer. crypto/ssh copies a session's stdout
// and stderr in two separate goroutines; when both target one combined buffer
// (and the caller may read it after a context-cancelled Close), the writes and
// the read must be synchronized or output is raced/dropped.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// SSHConfig configures a live SSHRunner.
type SSHConfig struct {
	// Host is the node's address (IP or DNS name). Required.
	Host string
	// Port is the SSH port; defaults to 22.
	Port int
	// User is the login user; defaults to "root" (fresh attacker infra is
	// typically provisioned with root or a cloud-init sudo user).
	User string
	// PrivateKeyPEM is the PEM-encoded private key used for public-key auth.
	// Required — RInfra authenticates to engagement infra with per-engagement
	// key material, never passwords.
	PrivateKeyPEM []byte
	// Passphrase decrypts PrivateKeyPEM if it is encrypted. Optional.
	Passphrase []byte
	// HostKeyCallback verifies the server host key. If nil, host keys are
	// accepted on a trust-on-first-use basis: engagement infra is ephemeral and
	// freshly provisioned, so its host key is not known ahead of time. Callers
	// that pin a key (e.g. from cloud metadata) should set this.
	HostKeyCallback ssh.HostKeyCallback
	// Timeout bounds the TCP dial; defaults to 30s.
	Timeout time.Duration
}

func (c *SSHConfig) withDefaults() {
	if c.Port == 0 {
		c.Port = 22
	}
	if c.User == "" {
		c.User = "root"
	}
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	if c.HostKeyCallback == nil {
		// Ephemeral, just-provisioned infra: TOFU. Documented above.
		c.HostKeyCallback = ssh.InsecureIgnoreHostKey()
	}
}

// SSHRunner is a live Runner backed by golang.org/x/crypto/ssh. It dials a new
// connection per operation, which keeps it simple and robust against idle
// timeouts on freshly booted hosts; deploys are short-lived and low-frequency.
type SSHRunner struct {
	clientConfig *ssh.ClientConfig
	addr         string
	timeout      time.Duration
	// dial is the connection factory; overridable in tests.
	dial func(ctx context.Context, network, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error)
}

// NewSSHRunner validates cfg and returns a live runner. It does not dial until
// the first Run/Upload.
func NewSSHRunner(cfg SSHConfig) (*SSHRunner, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("deploy: SSH host is required")
	}
	if len(cfg.PrivateKeyPEM) == 0 {
		return nil, fmt.Errorf("deploy: SSH private key is required (per-engagement key material)")
	}
	cfg.withDefaults()

	var signer ssh.Signer
	var err error
	if len(cfg.Passphrase) > 0 {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(cfg.PrivateKeyPEM, cfg.Passphrase)
	} else {
		signer, err = ssh.ParsePrivateKey(cfg.PrivateKeyPEM)
	}
	if err != nil {
		return nil, fmt.Errorf("deploy: parse SSH private key: %w", err)
	}

	return &SSHRunner{
		clientConfig: &ssh.ClientConfig{
			User:            cfg.User,
			Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
			HostKeyCallback: cfg.HostKeyCallback,
			Timeout:         cfg.Timeout,
		},
		addr:    net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		timeout: cfg.Timeout,
		dial:    dialSSH,
	}, nil
}

// dialSSH is the default context-aware dialer.
func dialSSH(ctx context.Context, network, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	d := net.Dialer{Timeout: cfg.Timeout}
	conn, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// Run executes cmd on the remote host and returns combined stdout+stderr.
func (r *SSHRunner) Run(ctx context.Context, cmd string) (string, error) {
	client, err := r.dial(ctx, "tcp", r.addr, r.clientConfig)
	if err != nil {
		return "", fmt.Errorf("deploy: ssh dial %s: %w", r.addr, err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("deploy: ssh session: %w", err)
	}
	defer session.Close()

	var out syncBuffer
	session.Stdout = &out
	session.Stderr = &out

	runErr := runSessionCtx(ctx, session, func() error { return session.Run(cmd) })
	if runErr != nil {
		return out.String(), fmt.Errorf("deploy: ssh run %q: %w", cmd, runErr)
	}
	return out.String(), nil
}

// Upload writes content to remotePath by streaming it into `cat` on the remote
// host — no extra SFTP dependency, works on any POSIX target.
func (r *SSHRunner) Upload(ctx context.Context, remotePath, content string) error {
	client, err := r.dial(ctx, "tcp", r.addr, r.clientConfig)
	if err != nil {
		return fmt.Errorf("deploy: ssh dial %s: %w", r.addr, err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("deploy: ssh session: %w", err)
	}
	defer session.Close()

	session.Stdin = strings.NewReader(content)
	var out syncBuffer
	session.Stderr = &out
	cmd := fmt.Sprintf("cat > %s", shellQuote(remotePath))
	if err := runSessionCtx(ctx, session, func() error { return session.Run(cmd) }); err != nil {
		return fmt.Errorf("deploy: ssh upload to %s: %w (%s)", remotePath, err, strings.TrimSpace(out.String()))
	}
	return nil
}

// runSessionCtx runs fn (a blocking session call) and aborts the session if ctx
// is cancelled first.
func runSessionCtx(ctx context.Context, session *ssh.Session, fn func() error) error {
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		_ = session.Close()
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// shellQuote single-quotes s for safe use as a shell argument.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Environment variable names for the node SSH credentials used by NewNodeRunner.
const (
	EnvSSHUser       = "RINFRA_SSH_USER"
	EnvSSHPort       = "RINFRA_SSH_PORT"
	EnvSSHPrivateKey = "RINFRA_SSH_PRIVATE_KEY"      // PEM contents
	EnvSSHKeyFile    = "RINFRA_SSH_PRIVATE_KEY_FILE" // path to PEM file
)

// NewNodeRunner builds the production Runner for a node host. SSH key material
// is loaded from the environment (the service layer exports per-engagement key
// material before invoking a deploy):
//
//   - RINFRA_SSH_PRIVATE_KEY      — PEM-encoded private key, or
//   - RINFRA_SSH_PRIVATE_KEY_FILE — path to a PEM file
//   - RINFRA_SSH_USER             — login user (default "root")
//   - RINFRA_SSH_PORT             — SSH port (default 22)
//
// If the host or key is missing/invalid it returns a Runner whose operations
// fail with a clear configuration error, so a misconfigured deploy surfaces a
// real message rather than panicking.
func NewNodeRunner(host string) Runner {
	cfg, err := sshConfigFromEnv(host)
	if err != nil {
		return &errRunner{err: err}
	}
	r, err := NewSSHRunner(cfg)
	if err != nil {
		return &errRunner{err: err}
	}
	return r
}

func sshConfigFromEnv(host string) (SSHConfig, error) {
	if host == "" {
		return SSHConfig{}, fmt.Errorf("deploy: node has no host/PublicIP; cannot open SSH runner")
	}
	cfg := SSHConfig{Host: host, User: os.Getenv(EnvSSHUser)}
	if p := os.Getenv(EnvSSHPort); p != "" {
		port, err := strconv.Atoi(p)
		if err != nil {
			return SSHConfig{}, fmt.Errorf("deploy: invalid %s=%q: %w", EnvSSHPort, p, err)
		}
		cfg.Port = port
	}
	switch {
	case os.Getenv(EnvSSHPrivateKey) != "":
		cfg.PrivateKeyPEM = []byte(os.Getenv(EnvSSHPrivateKey))
	case os.Getenv(EnvSSHKeyFile) != "":
		b, err := os.ReadFile(os.Getenv(EnvSSHKeyFile))
		if err != nil {
			return SSHConfig{}, fmt.Errorf("deploy: read %s: %w", EnvSSHKeyFile, err)
		}
		cfg.PrivateKeyPEM = b
	default:
		return SSHConfig{}, fmt.Errorf("deploy: no SSH key configured (set %s or %s)", EnvSSHPrivateKey, EnvSSHKeyFile)
	}
	return cfg, nil
}

// errRunner is a Runner that always fails with a fixed configuration error.
type errRunner struct{ err error }

func (e *errRunner) Run(context.Context, string) (string, error)  { return "", e.err }
func (e *errRunner) Upload(context.Context, string, string) error { return e.err }
