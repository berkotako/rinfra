package service_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/rinfra/rinfra/internal/c2"
	_ "github.com/rinfra/rinfra/internal/c2/sliver" // register the sliver provider in the test binary
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/service"
	"github.com/rinfra/rinfra/internal/store"
)

// opUser builds an operator-role user for tunnel tests.
func opUser(id string) domain.User {
	return domain.User{ID: id, Username: id, Role: domain.RoleOperator}
}

// deployLiveC2 authorizes an engagement, deploys the standard topology (which
// includes a live Sliver c2_server node with a public IP), and returns it.
func deployLiveC2(t *testing.T, ctx context.Context, s testStores, hub *service.Hub) domain.Engagement {
	t.Helper()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)
	svcInfra := buildInfraService(t, s, hub)
	saveTestTopology(t, ctx, svcInfra, eng.ID)
	jobID, err := svcInfra.Deploy(ctx, eng.ID, "op1")
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if status := waitForJob(t, ctx, s.job, jobID); status != domain.JobDone {
		t.Fatalf("deploy job status = %s, want done", status)
	}
	return eng
}

func TestC2ManualAccess(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	eng := deployLiveC2(t, ctx, s, hub)

	svcC2 := service.NewC2Service(s.eng, s.infra, s.audit, testLogger())
	view, err := svcC2.ManualAccess(ctx, eng.ID, "op1")
	if err != nil {
		t.Fatalf("ManualAccess: %v", err)
	}
	if view.Framework != "sliver" || view.Client != "sliver-client" {
		t.Errorf("unexpected framework/client: %+v", view)
	}
	if view.OperatorPort != 31337 {
		t.Errorf("operator port = %d, want 31337", view.OperatorPort)
	}
	if view.Host == "" || !strings.Contains(view.SSHCommand, view.Host) {
		t.Errorf("ssh command should reference the teamserver host: %q (host %q)", view.SSHCommand, view.Host)
	}
	if !strings.HasPrefix(view.SSHCommand, "ssh -i ") {
		t.Errorf("ssh command not rendered: %q", view.SSHCommand)
	}
	if !hasAuditAction(s.audit, "c2.manual_access", eng.ID) {
		t.Error("expected c2.manual_access audit event")
	}
}

func TestC2ManualAccess_GateBlocksUnauthorized(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	svcEng := service.NewEngagementService(s.eng, s.audit)

	// A draft engagement is not deployable; manual access must be refused.
	created, err := svcEng.Create(ctx, domain.Engagement{
		Client:         "C",
		Codename:       "DRAFT",
		Status:         domain.EngagementDraft,
		Scope:          domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
		EngagementType: domain.EngagementTypeRedTeam,
	}, "op1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	svcC2 := service.NewC2Service(s.eng, s.infra, s.audit, testLogger())
	if _, err := svcC2.ManualAccess(ctx, created.ID, "op1"); err == nil {
		t.Fatal("expected authorization gate to block manual access on a draft engagement")
	}
}

func TestC2OpenTunnel_NotConfigured(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	eng := deployLiveC2(t, ctx, s, hub)

	// No tunnel dialer wired -> OpenTunnel reports unsupported (but the gate and
	// node lookup still pass, so the error is specifically about the dialer).
	svcC2 := service.NewC2Service(s.eng, s.infra, s.audit, testLogger())
	_, err := svcC2.OpenTunnel(ctx, eng.ID, opUser("op1"))
	if err == nil || !strings.Contains(err.Error(), "dialer not configured") {
		t.Fatalf("expected dialer-not-configured error, got %v", err)
	}
}

// tunnelFakeDialer forwards "remote" dials to a fixed local target, standing in
// for an SSH client built from the engagement key.
type tunnelFakeDialer struct{ target string }

func (d tunnelFakeDialer) Dial(network, _ string) (net.Conn, error) {
	return net.Dial(network, d.target)
}

func TestC2OpenAndCloseTunnel(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	eng := deployLiveC2(t, ctx, s, hub)

	// Upstream "teamserver operator port": an echo server.
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

	svcC2 := service.NewC2Service(s.eng, s.infra, s.audit, testLogger()).
		WithTunnelDialer(func(_ context.Context, _ domain.Node) (c2.RemoteDialer, error) {
			return tunnelFakeDialer{target: upstream.Addr().String()}, nil
		}, "/keys/eng.pem")

	view, err := svcC2.OpenTunnel(ctx, eng.ID, opUser("op1"))
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	if view.TunnelID == "" || view.LocalAddr == "" || view.Framework != "sliver" {
		t.Fatalf("unexpected tunnel view: %+v", view)
	}
	if !hasAuditAction(s.audit, "c2.tunnel_open", eng.ID) {
		t.Error("expected c2.tunnel_open audit event")
	}

	// Traffic flows through the tunnel to the upstream echo server.
	conn, err := net.DialTimeout("tcp", view.LocalAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial tunnel: %v", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "ping\n")
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read through tunnel: %v", err)
	}
	if strings.TrimSpace(line) != "PING" {
		t.Errorf("tunnel echo = %q, want PING", strings.TrimSpace(line))
	}

	if err := svcC2.CloseTunnel(ctx, eng.ID, view.TunnelID, opUser("op1")); err != nil {
		t.Fatalf("CloseTunnel: %v", err)
	}
	if !hasAuditAction(s.audit, "c2.tunnel_close", eng.ID) {
		t.Error("expected c2.tunnel_close audit event")
	}
	// Closing an unknown tunnel is an error.
	if err := svcC2.CloseTunnel(ctx, eng.ID, "no-such-tunnel", opUser("op1")); err == nil {
		t.Error("expected error closing unknown tunnel")
	}
}

func TestC2OpenShell(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	eng := deployLiveC2(t, ctx, s, hub)

	svcC2 := service.NewC2Service(s.eng, s.infra, s.audit, testLogger())

	// Resolve the live node id via the manual-access descriptor.
	view, err := svcC2.ManualAccess(ctx, eng.ID, "op1")
	if err != nil {
		t.Fatalf("ManualAccess: %v", err)
	}

	info, err := svcC2.OpenShell(ctx, eng.ID, view.NodeID, "op1")
	if err != nil {
		t.Fatalf("OpenShell: %v", err)
	}
	if info.Framework != "sliver" || info.OperatorPort != 31337 || info.NodeID != view.NodeID {
		t.Errorf("unexpected shell info: %+v", info)
	}
	if !hasAuditAction(s.audit, "c2.shell_open", eng.ID) {
		t.Error("expected c2.shell_open audit event")
	}

	// Interpreter behaviour.
	if out, closed := service.RespondShell(info, "info"); closed || !strings.Contains(out, "sliver") {
		t.Errorf("info command: out=%q closed=%v", out, closed)
	}
	if out, _ := service.RespondShell(info, "clear"); out != service.ShellClear {
		t.Errorf("clear should emit the clear sentinel, got %q", out)
	}
	if _, closed := service.RespondShell(info, "exit"); !closed {
		t.Error("exit should close the session")
	}
	if out, _ := service.RespondShell(info, "bogus"); !strings.Contains(out, "unknown command") {
		t.Errorf("unknown command output: %q", out)
	}

	// Unknown node id is rejected.
	if _, err := svcC2.OpenShell(ctx, eng.ID, "no-such-node", "op1"); err == nil {
		t.Error("expected error for unknown node id")
	}
}

func TestC2OpenShell_GateBlocksUnauthorized(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	draft, err := svcEng.Create(ctx, domain.Engagement{
		Client:         "Draft Co",
		Codename:       "DRAFT-SHELL",
		Status:         domain.EngagementDraft,
		Scope:          domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
		EngagementType: domain.EngagementTypeRedTeam,
	}, "op1")
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	svcC2 := service.NewC2Service(s.eng, s.infra, s.audit, testLogger())
	if _, err := svcC2.OpenShell(ctx, draft.ID, "any-node", "op1"); err == nil {
		t.Fatal("expected CanDeploy gate to block shell on draft engagement")
	}
}

func TestC2ListTeamservers(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	eng := deployLiveC2(t, ctx, s, hub)

	svcC2 := service.NewC2Service(s.eng, s.infra, s.audit, testLogger())
	views, err := svcC2.ListTeamservers(ctx, eng.ID, "op1")
	if err != nil {
		t.Fatalf("ListTeamservers: %v", err)
	}
	if len(views) == 0 {
		t.Fatal("expected at least one live teamserver")
	}
	found := false
	for _, v := range views {
		if v.Framework == "sliver" && v.OperatorPort == 31337 && v.NodeID != "" {
			found = true
		}
		if v.Host == "" || v.SSHCommand == "" {
			t.Errorf("incomplete teamserver view: %+v", v)
		}
	}
	if !found {
		t.Errorf("expected the live sliver teamserver in the list: %+v", views)
	}
	if !hasAuditAction(s.audit, "c2.teamservers_list", eng.ID) {
		t.Error("expected c2.teamservers_list audit event")
	}
}

// tunnelService builds a C2Service with a fake dialer (whose Dial is never
// invoked in these tests since no traffic flows).
func tunnelService(t *testing.T, s testStores) *service.C2Service {
	t.Helper()
	return service.NewC2Service(s.eng, s.infra, s.audit, testLogger()).
		WithTunnelDialer(func(_ context.Context, _ domain.Node) (c2.RemoteDialer, error) {
			return tunnelFakeDialer{target: "127.0.0.1:9"}, nil
		}, "/keys/eng.pem")
}

// TestC2CloseTunnel_Authorization verifies tunnel close is bound to the
// engagement and restricted to the opener or an admin/lead.
func TestC2CloseTunnel_Authorization(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	eng := deployLiveC2(t, ctx, s, hub)
	svcC2 := tunnelService(t, s)
	defer svcC2.Shutdown()

	view, err := svcC2.OpenTunnel(ctx, eng.ID, opUser("op1"))
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}

	// Wrong engagement id must not find the tunnel.
	if err := svcC2.CloseTunnel(ctx, "ENG-OTHER", view.TunnelID, opUser("op1")); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cross-engagement close: want ErrNotFound, got %v", err)
	}
	// A different operator (not the opener) is forbidden.
	if err := svcC2.CloseTunnel(ctx, eng.ID, view.TunnelID, opUser("op2")); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("non-opener close: want ErrUnauthorized, got %v", err)
	}
	// The tunnel is still active after the denied attempts.
	if got := len(svcC2.ListTunnels(ctx, eng.ID)); got != 1 {
		t.Fatalf("active tunnels after denied closes = %d, want 1", got)
	}
	// An admin (not the opener) may close it.
	admin := domain.User{ID: "admin1", Username: "admin1", Role: domain.RoleAdmin}
	if err := svcC2.CloseTunnel(ctx, eng.ID, view.TunnelID, admin); err != nil {
		t.Fatalf("admin close: %v", err)
	}
	if got := len(svcC2.ListTunnels(ctx, eng.ID)); got != 0 {
		t.Errorf("active tunnels after close = %d, want 0", got)
	}
}

// TestC2TunnelReap verifies idle and absolute TTL cleanup, and Shutdown.
func TestC2TunnelReap(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	eng := deployLiveC2(t, ctx, s, hub)

	// Idle TTL: short idle, long absolute -> reaped by idle.
	idleSvc := tunnelService(t, s).WithTunnelTTLs(10*time.Millisecond, time.Hour)
	if _, err := idleSvc.OpenTunnel(ctx, eng.ID, opUser("op1")); err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	if n := idleSvc.ReapTunnels(time.Now().Add(time.Second)); n != 1 {
		t.Errorf("idle reap count = %d, want 1", n)
	}
	if got := len(idleSvc.ListTunnels(ctx, eng.ID)); got != 0 {
		t.Errorf("tunnels after idle reap = %d, want 0", got)
	}
	if !hasAuditAction(s.audit, "c2.tunnel_expired", eng.ID) {
		t.Error("expected c2.tunnel_expired audit event")
	}
	idleSvc.Shutdown()

	// Absolute TTL: long idle, tiny absolute -> reaped by absolute cap.
	absSvc := tunnelService(t, s).WithTunnelTTLs(time.Hour, 10*time.Millisecond)
	if _, err := absSvc.OpenTunnel(ctx, eng.ID, opUser("op1")); err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	if n := absSvc.ReapTunnels(time.Now().Add(time.Second)); n != 1 {
		t.Errorf("absolute reap count = %d, want 1", n)
	}
	absSvc.Shutdown()

	// Shutdown closes any remaining tunnels.
	downSvc := tunnelService(t, s)
	if _, err := downSvc.OpenTunnel(ctx, eng.ID, opUser("op1")); err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	downSvc.Shutdown()
	if got := len(downSvc.ListTunnels(ctx, eng.ID)); got != 0 {
		t.Errorf("tunnels after shutdown = %d, want 0", got)
	}
}
