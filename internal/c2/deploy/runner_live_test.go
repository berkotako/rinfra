package deploy

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// genKeyPEM returns a fresh RSA private key in PEM form plus its ssh.Signer.
func genKeyPEM(t *testing.T) ([]byte, ssh.Signer) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return pemBytes, signer
}

// fakeSSHServer is a minimal in-process SSH server that accepts any public key
// and serves exec requests. For `cat > path` commands it captures stdin keyed
// by path; for other commands it echoes a canned response. It records every
// executed command.
type fakeSSHServer struct {
	addr     string
	hostKey  ssh.Signer
	ln       net.Listener
	mu       sync.Mutex
	commands []string
	uploads  map[string]string
}

func newFakeSSHServer(t *testing.T) *fakeSSHServer {
	t.Helper()
	_, hostSigner := genKeyPEM(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeSSHServer{
		addr:    ln.Addr().String(),
		hostKey: hostSigner,
		ln:      ln,
		uploads: map[string]string{},
	}
	go s.serve()
	t.Cleanup(func() { ln.Close() })
	return s
}

func (s *fakeSSHServer) serve() {
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
			return &ssh.Permissions{}, nil
		},
	}
	cfg.AddHostKey(s.hostKey)
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn, cfg)
	}
}

func (s *fakeSSHServer) handleConn(conn net.Conn, cfg *ssh.ServerConfig) {
	sconn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	defer sconn.Close()
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			nc.Reject(ssh.UnknownChannelType, "only session")
			continue
		}
		ch, chReqs, err := nc.Accept()
		if err != nil {
			continue
		}
		go s.handleSession(ch, chReqs)
	}
}

func (s *fakeSSHServer) handleSession(ch ssh.Channel, reqs <-chan *ssh.Request) {
	for req := range reqs {
		if req.Type != "exec" {
			req.Reply(false, nil)
			continue
		}
		// exec payload: 4-byte length + command string.
		cmd := string(req.Payload[4:])
		req.Reply(true, nil)

		s.mu.Lock()
		s.commands = append(s.commands, cmd)
		s.mu.Unlock()

		if strings.HasPrefix(cmd, "cat > ") {
			path := strings.TrimSpace(strings.TrimPrefix(cmd, "cat > "))
			path = strings.Trim(path, "'")
			data, _ := io.ReadAll(ch)
			s.mu.Lock()
			s.uploads[path] = string(data)
			s.mu.Unlock()
		} else {
			io.WriteString(ch, "ok: "+cmd+"\n")
		}
		ch.SendRequest("exit-status", false, exitStatus(0))
		ch.Close()
		return
	}
}

func exitStatus(code uint32) []byte {
	return []byte{byte(code >> 24), byte(code >> 16), byte(code >> 8), byte(code)}
}

func (s *fakeSSHServer) cmds() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.commands))
	copy(out, s.commands)
	return out
}

func (s *fakeSSHServer) upload(path string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.uploads[path]
	return v, ok
}

func newTestRunner(t *testing.T, srv *fakeSSHServer) *SSHRunner {
	t.Helper()
	keyPEM, _ := genKeyPEM(t)
	host, port, _ := net.SplitHostPort(srv.addr)
	r, err := NewSSHRunner(SSHConfig{
		Host:            host,
		Port:            atoi(t, port),
		User:            "root",
		PrivateKeyPEM:   keyPEM,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewSSHRunner: %v", err)
	}
	return r
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

func TestSSHRunner_Run(t *testing.T) {
	srv := newFakeSSHServer(t)
	r := newTestRunner(t, srv)
	out, err := r.Run(context.Background(), "whoami")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "ok: whoami") {
		t.Errorf("output = %q, want it to contain 'ok: whoami'", out)
	}
	if got := srv.cmds(); len(got) != 1 || got[0] != "whoami" {
		t.Errorf("server commands = %v, want [whoami]", got)
	}
}

func TestSSHRunner_Upload(t *testing.T) {
	srv := newFakeSSHServer(t)
	r := newTestRunner(t, srv)
	if err := r.Upload(context.Background(), "/tmp/rinfra-install.sh", "#!/bin/bash\necho hi\n"); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	got, ok := srv.upload("/tmp/rinfra-install.sh")
	if !ok {
		t.Fatalf("no upload recorded")
	}
	if !strings.Contains(got, "echo hi") {
		t.Errorf("uploaded content = %q", got)
	}
}

func TestSSHRunner_RunInstall_EndToEnd(t *testing.T) {
	srv := newFakeSSHServer(t)
	r := newTestRunner(t, srv)
	err := RunInstall(context.Background(), r, InstallParams{
		ReleaseURL:  "https://example.com/tool",
		SHA256:      "deadbeef",
		DestPath:    "/usr/local/bin/tool",
		SystemdUnit: "tool",
	})
	if err != nil {
		t.Fatalf("RunInstall: %v", err)
	}
	// The install script should have been uploaded then executed via bash.
	if _, ok := srv.upload("/tmp/rinfra-install.sh"); !ok {
		t.Error("install script was not uploaded")
	}
	foundBash := false
	for _, c := range srv.cmds() {
		if strings.Contains(c, "bash /tmp/rinfra-install.sh") {
			foundBash = true
		}
	}
	if !foundBash {
		t.Errorf("install script was not executed; commands=%v", srv.cmds())
	}
}

func TestNewSSHRunner_Validation(t *testing.T) {
	if _, err := NewSSHRunner(SSHConfig{PrivateKeyPEM: []byte("x")}); err == nil {
		t.Error("expected error when host missing")
	}
	if _, err := NewSSHRunner(SSHConfig{Host: "h"}); err == nil {
		t.Error("expected error when key missing")
	}
	if _, err := NewSSHRunner(SSHConfig{Host: "h", PrivateKeyPEM: []byte("not-a-key")}); err == nil {
		t.Error("expected error when key unparseable")
	}
}

func TestNewNodeRunner_NoKeyConfigured(t *testing.T) {
	t.Setenv(EnvSSHPrivateKey, "")
	t.Setenv(EnvSSHKeyFile, "")
	r := NewNodeRunner("10.0.0.1")
	if _, err := r.Run(context.Background(), "x"); err == nil {
		t.Error("expected configuration error when no SSH key is set")
	}
}

func TestNewNodeRunner_WithKey(t *testing.T) {
	keyPEM, _ := genKeyPEM(t)
	t.Setenv(EnvSSHPrivateKey, string(keyPEM))
	r := NewNodeRunner("10.0.0.1")
	if _, ok := r.(*SSHRunner); !ok {
		t.Errorf("expected *SSHRunner, got %T", r)
	}
}
