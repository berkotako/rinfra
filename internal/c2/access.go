package c2

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
)

// Manual access is RInfra's second usage mode. Instead of driving a framework
// through the automated Operator, an operator can connect their *native* client
// (or a browser, for web UIs) to the deployed teamserver and run the engagement
// by hand. This mode works for every framework regardless of tier — including
// Fronted-tier frameworks (Cobalt Strike, Brute Ratel) that expose no Operator.
//
// RInfra never publishes a teamserver's operator port to the internet. Manual
// access is delivered over an SSH local port-forward to the provisioned machine:
// the operator's native client talks to a local address that is tunneled to the
// teamserver's operator port over the per-engagement SSH key.

// AccessProtocol describes how a native operator client speaks to a teamserver.
type AccessProtocol string

const (
	AccessGRPCMTLS AccessProtocol = "grpc-mtls" // e.g. Sliver operator gRPC
	AccessHTTPS    AccessProtocol = "https"     // e.g. msfrpcd, HTTP APIs
	AccessTCP      AccessProtocol = "tcp"       // e.g. Cobalt Strike team server
	AccessWebUI    AccessProtocol = "web-ui"    // e.g. Mythic browser UI
)

// ManualAccess describes the "drive it yourself" path for a deployed teamserver.
type ManualAccess struct {
	Framework    string
	Client       string         // native operator client, e.g. "sliver-client"
	Protocol     AccessProtocol //
	OperatorPort int            // port on the teamserver machine the client targets
	Tunnel       TunnelSpec     // SSH local port-forward used to reach OperatorPort
	Instructions string         // human guidance for connecting the native client
}

// TunnelSpec describes an SSH local port-forward from the operator workstation
// (or control plane) to the teamserver's operator port.
type TunnelSpec struct {
	LocalHost  string // bind address for the local end (default 127.0.0.1)
	LocalPort  int    // local port; 0 lets the OS choose (used by OpenLocalForward)
	RemoteHost string // host as seen from the SSH server (usually 127.0.0.1)
	RemotePort int    // operator port on the teamserver
	SSHUser    string // SSH user on the teamserver machine
	SSHHost    string // SSH endpoint (teamserver public IP)
	SSHPort    int    // SSH port (default 22)
}

// LocalAddr returns the local bind address, defaulting the host to 127.0.0.1.
func (s TunnelSpec) LocalAddr() string {
	host := s.LocalHost
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(s.LocalPort))
}

// RemoteAddr returns the teamserver-side address to forward to.
func (s TunnelSpec) RemoteAddr() string {
	host := s.RemoteHost
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(s.RemotePort))
}

// SSHCommand renders a ready-to-run OpenSSH local-forward command for operators
// who prefer to open the tunnel themselves.
func (s TunnelSpec) SSHCommand(keyPath string) string {
	sshPort := s.SSHPort
	if sshPort == 0 {
		sshPort = 22
	}
	host := s.LocalHost
	if host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("ssh -i %s -N -L %s:%d:%s:%d %s@%s -p %d",
		keyPath, host, s.LocalPort, s.RemoteHost, s.RemotePort, s.SSHUser, s.SSHHost, sshPort)
}

// ManualAccessProvider is an optional interface a C2Provider may implement to
// describe how its native client connects. Providers that don't implement it
// get a generic descriptor from ManualAccessFor.
type ManualAccessProvider interface {
	ManualAccess(ts Teamserver) (ManualAccess, error)
}

// ManualAccessFor returns the manual-access descriptor for a provider+teamserver,
// using the provider's own implementation when available or a generic default.
// The default tunnels the teamserver's reported port — enough to connect a
// native client even for frameworks that have not customized this.
func ManualAccessFor(p C2Provider, ts Teamserver) (ManualAccess, error) {
	if ap, ok := p.(ManualAccessProvider); ok {
		return ap.ManualAccess(ts)
	}
	return ManualAccess{
		Framework:    p.Name(),
		Client:       p.Name() + " operator client",
		Protocol:     AccessTCP,
		OperatorPort: ts.Port,
		Tunnel:       DefaultTunnel(ts, ts.Port),
		Instructions: fmt.Sprintf("Open the tunnel, then point your %s client at the local address.", p.Name()),
	}, nil
}

// DefaultTunnel builds a standard local-forward spec to a teamserver operator
// port, mirroring the local port to the operator port for predictability.
func DefaultTunnel(ts Teamserver, operatorPort int) TunnelSpec {
	return TunnelSpec{
		LocalHost:  "127.0.0.1",
		LocalPort:  operatorPort,
		RemoteHost: "127.0.0.1",
		RemotePort: operatorPort,
		SSHUser:    "root",
		SSHHost:    ts.Host,
		SSHPort:    22,
	}
}

// RemoteDialer opens a connection to an address as seen from the SSH server.
// *golang.org/x/crypto/ssh.Client satisfies this directly (its Dial method), so
// the production caller passes an SSH client; tests pass a fake.
type RemoteDialer interface {
	Dial(network, addr string) (net.Conn, error)
}

// Tunnel is a running local port-forward. Close stops accepting and tears down
// in-flight connections.
type Tunnel struct {
	listener net.Listener
	remote   string
	dialer   RemoteDialer

	closeOnce sync.Once
	quit      chan struct{}
	wg        sync.WaitGroup

	mu     sync.Mutex
	conns  map[net.Conn]struct{} // active local connections
	closed bool
}

// LocalAddr is the actual local address the tunnel is listening on (useful when
// the spec requested an OS-assigned port).
func (t *Tunnel) LocalAddr() string { return t.listener.Addr().String() }

// Close stops the tunnel: it closes the listener and tears down any in-flight
// connections, then waits for the handlers to finish. Safe to call while an
// operator is still connected.
func (t *Tunnel) Close() error {
	var err error
	t.closeOnce.Do(func() { err = t.shutdown() })
	t.wg.Wait()
	return err
}

// shutdown closes the listener and all tracked connections exactly once. The
// connection close is what unblocks handlers parked in io.Copy.
func (t *Tunnel) shutdown() error {
	t.mu.Lock()
	t.closed = true
	conns := make([]net.Conn, 0, len(t.conns))
	for c := range t.conns {
		conns = append(conns, c)
	}
	t.mu.Unlock()

	close(t.quit)
	err := t.listener.Close()
	for _, c := range conns {
		_ = c.Close()
	}
	return err
}

func (t *Tunnel) track(c net.Conn) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return false
	}
	t.conns[c] = struct{}{}
	return true
}

func (t *Tunnel) untrack(c net.Conn) {
	t.mu.Lock()
	delete(t.conns, c)
	t.mu.Unlock()
}

// OpenLocalForward starts a local TCP listener that forwards each accepted
// connection to spec.RemoteAddr() via the RemoteDialer. The operator points
// their native C2 client at Tunnel.LocalAddr(). This is the mechanism behind
// manual-access mode; the service layer supplies an SSH-backed RemoteDialer
// built from the engagement key, and audits the access. The tunnel runs until
// Close is called or ctx is cancelled.
func OpenLocalForward(ctx context.Context, dialer RemoteDialer, spec TunnelSpec) (*Tunnel, error) {
	if dialer == nil {
		return nil, fmt.Errorf("c2: OpenLocalForward requires a RemoteDialer")
	}
	ln, err := net.Listen("tcp", spec.LocalAddr())
	if err != nil {
		return nil, fmt.Errorf("c2: open local forward listener: %w", err)
	}
	t := &Tunnel{
		listener: ln,
		remote:   spec.RemoteAddr(),
		dialer:   dialer,
		quit:     make(chan struct{}),
		conns:    make(map[net.Conn]struct{}),
	}
	t.wg.Add(1)
	go t.serve(ctx)
	return t, nil
}

func (t *Tunnel) serve(ctx context.Context) {
	defer t.wg.Done()
	// Tear down if the context is cancelled; exit cleanly once shut down so the
	// watcher goroutine does not leak.
	go func() {
		select {
		case <-ctx.Done():
			t.closeOnce.Do(func() { _ = t.shutdown() })
		case <-t.quit:
		}
	}()
	for {
		local, err := t.listener.Accept()
		if err != nil {
			return // listener closed
		}
		if !t.track(local) {
			_ = local.Close()
			continue
		}
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			defer t.untrack(local)
			t.handle(local)
		}()
	}
}

func (t *Tunnel) handle(local net.Conn) {
	defer local.Close()
	remote, err := t.dialer.Dial("tcp", t.remote)
	if err != nil {
		return
	}
	defer remote.Close()
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		// Unblock the peer copy by closing both ends.
		_ = dst.Close()
		_ = src.Close()
		done <- struct{}{}
	}
	go cp(remote, local)
	go cp(local, remote)
	<-done
	<-done
}
