//go:build c2live

// Live SSH deploy-mechanics harness — OPT-IN, not run by normal CI.
//
// These tests exercise the real SSHRunner (the seam every C2 deploy relies on)
// against a reachable SSH server, e.g. the sshd target in docker-compose.c2.yml.
// They are skipped unless the target is configured:
//
//	RINFRA_C2LIVE_SSH_ADDR   host:port           (e.g. localhost:2222)
//	RINFRA_C2LIVE_SSH_USER   login user          (default "rinfra")
//	RINFRA_C2LIVE_SSH_KEY    path to a PEM key   (the harness private key)
//
// Run:  make test-c2live   (or: go test -tags c2live ./internal/c2/deploy/...)
package deploy

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func liveRunner(t *testing.T) *SSHRunner {
	t.Helper()
	addr := os.Getenv("RINFRA_C2LIVE_SSH_ADDR")
	keyPath := os.Getenv("RINFRA_C2LIVE_SSH_KEY")
	if addr == "" || keyPath == "" {
		t.Skip("RINFRA_C2LIVE_SSH_ADDR / RINFRA_C2LIVE_SSH_KEY not set; skipping live SSH harness")
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("RINFRA_C2LIVE_SSH_ADDR %q must be host:port: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("invalid port in %q: %v", addr, err)
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read SSH key %s: %v", keyPath, err)
	}
	user := os.Getenv("RINFRA_C2LIVE_SSH_USER")
	if user == "" {
		user = "rinfra"
	}
	r, err := NewSSHRunner(SSHConfig{
		Host:            host,
		Port:            port,
		User:            user,
		PrivateKeyPEM:   key,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // throwaway harness target
		Timeout:         15 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewSSHRunner: %v", err)
	}
	return r
}

// TestC2Live_RunAndUpload validates the real SSH transport: a command round-trip
// and an upload read back from the server.
func TestC2Live_RunAndUpload(t *testing.T) {
	r := liveRunner(t)
	ctx := context.Background()

	out, err := r.Run(ctx, "echo rinfra-live")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "rinfra-live") {
		t.Errorf("Run output = %q, want it to contain 'rinfra-live'", out)
	}

	marker := "rinfra-harness-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	const remote = "/tmp/rinfra-harness.txt"
	if err := r.Upload(ctx, remote, marker+"\n"); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	back, err := r.Run(ctx, "cat "+remote)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(back, marker) {
		t.Errorf("read back = %q, want it to contain %q", back, marker)
	}
}

// TestC2Live_InstallScriptExec validates the install mechanic — upload a script
// then run it via bash — and that a non-zero exit surfaces as an error. This is
// the same upload+exec path RunInstall uses (without the artifact download,
// which needs a reachable release URL + checksum; see docs/RUNBOOK_C2.md).
func TestC2Live_InstallScriptExec(t *testing.T) {
	r := liveRunner(t)
	ctx := context.Background()

	const script = "/tmp/rinfra-harness.sh"
	if err := r.Upload(ctx, script, "#!/usr/bin/env bash\nset -euo pipefail\necho install-ok\n"); err != nil {
		t.Fatalf("upload script: %v", err)
	}
	out, err := r.Run(ctx, "bash "+script)
	if err != nil {
		t.Fatalf("run script: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "install-ok") {
		t.Errorf("script output = %q", out)
	}

	// A failing command must surface as an error (so a broken deploy is caught).
	if _, err := r.Run(ctx, "exit 7"); err == nil {
		t.Error("expected a non-zero exit to return an error")
	}
}
