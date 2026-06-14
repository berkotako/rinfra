package c2_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
)

func TestTunnelSpec_SSHCommand(t *testing.T) {
	spec := c2.TunnelSpec{
		LocalHost:  "127.0.0.1",
		LocalPort:  31337,
		RemoteHost: "127.0.0.1",
		RemotePort: 31337,
		SSHUser:    "root",
		SSHHost:    "203.0.113.10",
		// SSHPort defaults to 22.
	}
	got := spec.SSHCommand("/keys/eng.pem")
	want := "ssh -i /keys/eng.pem -N -L 127.0.0.1:31337:127.0.0.1:31337 root@203.0.113.10 -p 22"
	if got != want {
		t.Errorf("SSHCommand:\n got %q\nwant %q", got, want)
	}
}

// fakeProvider implements c2.C2Provider; manualProvider also implements
// c2.ManualAccessProvider.
type fakeProvider struct{ name string }

func (f fakeProvider) Name() string         { return f.name }
func (f fakeProvider) Tier() c2.SupportTier { return c2.TierFronted }
func (f fakeProvider) Deploy(context.Context, domain.Node, c2.Config) (c2.Teamserver, error) {
	return c2.Teamserver{}, nil
}
func (f fakeProvider) RedirectorConfig(domain.Profile) (string, error) { return "", nil }
func (f fakeProvider) Control(c2.Teamserver) (c2.Operator, bool)       { return nil, false }

type manualProvider struct{ fakeProvider }

func (m manualProvider) ManualAccess(ts c2.Teamserver) (c2.ManualAccess, error) {
	return c2.ManualAccess{Framework: m.name, Client: "custom-client", OperatorPort: 9999}, nil
}

func TestManualAccessFor_Default(t *testing.T) {
	p := fakeProvider{name: "havoc"}
	ts := c2.Teamserver{Host: "203.0.113.10", Port: 40056}
	ma, err := c2.ManualAccessFor(p, ts)
	if err != nil {
		t.Fatalf("ManualAccessFor: %v", err)
	}
	if ma.Framework != "havoc" || ma.OperatorPort != 40056 {
		t.Fatalf("unexpected default access: %+v", ma)
	}
	if ma.Tunnel.SSHHost != "203.0.113.10" || ma.Tunnel.RemotePort != 40056 {
		t.Fatalf("unexpected default tunnel: %+v", ma.Tunnel)
	}
}

func TestManualAccessFor_ProviderOverride(t *testing.T) {
	p := manualProvider{fakeProvider{name: "sliver"}}
	ma, err := c2.ManualAccessFor(p, c2.Teamserver{Host: "10.0.0.1"})
	if err != nil {
		t.Fatalf("ManualAccessFor: %v", err)
	}
	if ma.Client != "custom-client" || ma.OperatorPort != 9999 {
		t.Fatalf("expected provider override, got %+v", ma)
	}
}

// fakeDialer routes "remote" dials to a fixed local target, standing in for an
// SSH client's Dial. This lets us exercise the forwarder without real SSH.
type fakeDialer struct{ target string }

func (d fakeDialer) Dial(network, _ string) (net.Conn, error) {
	return net.Dial(network, d.target)
}

func TestOpenLocalForward_ProxiesTraffic(t *testing.T) {
	// Upstream "teamserver": an echo server that upper-cases each line.
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("upstream listen: %v", err)
	}
	defer upstream.Close()
	go func() {
		for {
			conn, err := upstream.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				sc := bufio.NewScanner(c)
				for sc.Scan() {
					fmt.Fprintf(c, "%s\n", strings.ToUpper(sc.Text()))
				}
			}(conn)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tun, err := c2.OpenLocalForward(ctx, fakeDialer{target: upstream.Addr().String()}, c2.TunnelSpec{
		LocalHost:  "127.0.0.1",
		LocalPort:  0, // OS-assigned
		RemoteHost: "127.0.0.1",
		RemotePort: 31337, // ignored by fakeDialer, real SSH would honor it
	})
	if err != nil {
		t.Fatalf("OpenLocalForward: %v", err)
	}
	defer tun.Close()

	conn, err := net.DialTimeout("tcp", tun.LocalAddr(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial tunnel: %v", err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "hello tunnel\n")
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read through tunnel: %v", err)
	}
	if strings.TrimSpace(line) != "HELLO TUNNEL" {
		t.Fatalf("proxied response = %q, want HELLO TUNNEL", strings.TrimSpace(line))
	}
}

func TestOpenLocalForward_NilDialer(t *testing.T) {
	if _, err := c2.OpenLocalForward(context.Background(), nil, c2.TunnelSpec{}); err == nil {
		t.Fatal("expected error for nil dialer")
	}
}
