package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rinfra/rinfra/internal/audit"
	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/store"
)

// C2Service exposes the manual-access usage mode: connecting a native operator
// client (or browser) to a deployed teamserver by hand, as an alternative to
// automated emulation. Like every privileged path it enforces the engagement
// authorization gate (CanDeploy) and audits each action. The teamserver's
// operator port is never exposed publicly — access is delivered over an SSH
// local port-forward to the provisioned machine.
type C2Service struct {
	engagements store.EngagementStore
	infra       store.InfraStore
	audit       audit.Logger
	log         *slog.Logger

	// dialer builds a RemoteDialer (an SSH client) to a teamserver node for
	// OpenTunnel. It is an injected seam: production wires it from the
	// per-engagement SSH key store; when nil, OpenTunnel reports unsupported and
	// only the descriptor path (ManualAccess) is available.
	dialer     TunnelDialerFactory
	sshKeyHint string // path shown in the rendered ssh -L command (informational)

	mu          sync.Mutex
	tunnels     map[string]*tunnelRecord
	idleTTL     time.Duration // close after this long with no activity
	absTTL      time.Duration // hard cap on a tunnel's total lifetime
	stopJanitor chan struct{}
	janitorOnce sync.Once
	stopOnce    sync.Once
}

// tunnelRecord binds a live tunnel to the engagement, node, opener, and lifetime
// metadata used for authorization, reconciliation, and cleanup.
type tunnelRecord struct {
	tun          *c2.Tunnel
	engagementID string
	nodeID       string
	framework    string
	openerID     string
	openerName   string
	createdAt    time.Time
	lastUsedAt   time.Time
	expiresAt    time.Time
}

// TunnelInfo is the non-sensitive view of an active tunnel for reconcile/audit.
type TunnelInfo struct {
	TunnelID  string `json:"tunnelId"`
	NodeID    string `json:"nodeId"`
	Framework string `json:"framework"`
	LocalAddr string `json:"localAddr"`
	Opener    string `json:"opener"`
	CreatedAt string `json:"createdAt"`
	ExpiresAt string `json:"expiresAt"`
}

// TunnelDialerFactory opens a RemoteDialer (e.g. an *ssh.Client) to the given
// teamserver node. *golang.org/x/crypto/ssh.Client satisfies c2.RemoteDialer.
type TunnelDialerFactory func(ctx context.Context, node domain.Node) (c2.RemoteDialer, error)

// NewC2Service constructs a C2Service.
func NewC2Service(engagements store.EngagementStore, infra store.InfraStore, a audit.Logger, log *slog.Logger) *C2Service {
	return &C2Service{
		engagements: engagements,
		infra:       infra,
		audit:       a,
		log:         log,
		tunnels:     make(map[string]*tunnelRecord),
		idleTTL:     30 * time.Minute,
		absTTL:      8 * time.Hour,
		stopJanitor: make(chan struct{}),
	}
}

// WithTunnelTTLs overrides the idle and absolute tunnel lifetimes (used in tests
// and tunable in production).
func (s *C2Service) WithTunnelTTLs(idle, absolute time.Duration) *C2Service {
	s.idleTTL = idle
	s.absTTL = absolute
	return s
}

// WithTunnelDialer enables OpenTunnel by supplying the dialer factory and the
// key path to render in operator-facing ssh commands.
func (s *C2Service) WithTunnelDialer(f TunnelDialerFactory, sshKeyHint string) *C2Service {
	s.dialer = f
	s.sshKeyHint = sshKeyHint
	return s
}

// ManualAccessView is the operator-facing description of how to drive a
// deployed teamserver by hand.
type ManualAccessView struct {
	Framework    string `json:"framework"`
	Client       string `json:"client"`
	Protocol     string `json:"protocol"`
	OperatorPort int    `json:"operatorPort"`
	NodeID       string `json:"nodeId"`
	Host         string `json:"host"`
	SSHCommand   string `json:"sshCommand"`
	Instructions string `json:"instructions"`
}

// ManualAccess returns how to connect a native client to the engagement's
// deployed C2 teamserver. It is gated by CanDeploy and audited.
func (s *C2Service) ManualAccess(ctx context.Context, engagementID, actor string) (ManualAccessView, error) {
	eng, err := s.engagements.Get(ctx, engagementID)
	if err != nil {
		return ManualAccessView{}, fmt.Errorf("c2.ManualAccess: %w", err)
	}
	if err := eng.CanDeploy(time.Now()); err != nil {
		return ManualAccessView{}, fmt.Errorf("c2.ManualAccess: %w", err)
	}

	node, provider, err := s.liveC2Node(ctx, engagementID)
	if err != nil {
		return ManualAccessView{}, err
	}

	view, err := s.manualAccessView(node, provider)
	if err != nil {
		return ManualAccessView{}, fmt.Errorf("c2.ManualAccess: %w", err)
	}

	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: engagementID,
		Actor:        actor,
		Action:       "c2.manual_access",
		Target:       node.ID,
		Detail:       fmt.Sprintf("framework=%s client=%s operator_port=%d", view.Framework, view.Client, view.OperatorPort),
		At:           time.Now().UTC(),
	})

	return view, nil
}

// manualAccessView builds the manual-access descriptor for one teamserver node.
func (s *C2Service) manualAccessView(node domain.Node, provider c2.C2Provider) (ManualAccessView, error) {
	ma, err := c2.ManualAccessFor(provider, c2.Teamserver{Host: node.PublicIP, Status: string(node.Status)})
	if err != nil {
		return ManualAccessView{}, err
	}
	keyHint := s.sshKeyHint
	if keyHint == "" {
		keyHint = "<engagement-ssh-key>"
	}
	return ManualAccessView{
		Framework:    ma.Framework,
		Client:       ma.Client,
		Protocol:     string(ma.Protocol),
		OperatorPort: ma.OperatorPort,
		NodeID:       node.ID,
		Host:         node.PublicIP,
		SSHCommand:   ma.Tunnel.SSHCommand(keyHint),
		Instructions: ma.Instructions,
	}, nil
}

// ListTeamservers returns a manual-access descriptor for every live C2 server
// node in the engagement (the Alive C2s view). Gated by CanDeploy and audited.
func (s *C2Service) ListTeamservers(ctx context.Context, engagementID, actor string) ([]ManualAccessView, error) {
	eng, err := s.engagements.Get(ctx, engagementID)
	if err != nil {
		return nil, fmt.Errorf("c2.ListTeamservers: %w", err)
	}
	if err := eng.CanDeploy(time.Now()); err != nil {
		return nil, fmt.Errorf("c2.ListTeamservers: %w", err)
	}

	nodes, err := s.infra.NodesForEngagement(ctx, engagementID)
	if err != nil {
		return nil, fmt.Errorf("c2.ListTeamservers: load nodes: %w", err)
	}

	out := make([]ManualAccessView, 0)
	for _, n := range nodes {
		if n.Spec.Type != domain.NodeC2Server || n.Status != domain.NodeLive || n.Spec.C2Framework == "" {
			continue
		}
		provider, err := c2.Get(n.Spec.C2Framework)
		if err != nil {
			continue
		}
		view, err := s.manualAccessView(n, provider)
		if err != nil {
			continue
		}
		out = append(out, view)
	}

	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: engagementID,
		Actor:        actor,
		Action:       "c2.teamservers_list",
		Target:       engagementID,
		Detail:       fmt.Sprintf("count=%d", len(out)),
		At:           time.Now().UTC(),
	})

	return out, nil
}

// TunnelView identifies a live local port-forward.
type TunnelView struct {
	TunnelID     string `json:"tunnelId"`
	LocalAddr    string `json:"localAddr"`
	Framework    string `json:"framework"`
	OperatorPort int    `json:"operatorPort"`
	ExpiresAt    string `json:"expiresAt"`
}

// OpenTunnel opens an SSH local port-forward to the engagement's C2 teamserver
// operator port and returns the local address the operator points their native
// client at. Gated by CanDeploy and audited. Requires a tunnel dialer (see
// WithTunnelDialer); otherwise it reports the feature is not configured.
func (s *C2Service) OpenTunnel(ctx context.Context, engagementID string, actor domain.User) (TunnelView, error) {
	eng, err := s.engagements.Get(ctx, engagementID)
	if err != nil {
		return TunnelView{}, fmt.Errorf("c2.OpenTunnel: %w", err)
	}
	if err := eng.CanDeploy(time.Now()); err != nil {
		return TunnelView{}, fmt.Errorf("c2.OpenTunnel: %w", err)
	}
	if s.dialer == nil {
		return TunnelView{}, fmt.Errorf("c2.OpenTunnel: manual tunnel dialer not configured (SSH key store not wired); use ManualAccess for connect instructions")
	}

	node, provider, err := s.liveC2Node(ctx, engagementID)
	if err != nil {
		return TunnelView{}, err
	}
	ma, err := c2.ManualAccessFor(provider, c2.Teamserver{Host: node.PublicIP, Status: string(node.Status)})
	if err != nil {
		return TunnelView{}, fmt.Errorf("c2.OpenTunnel: %w", err)
	}

	dialer, err := s.dialer(ctx, node)
	if err != nil {
		return TunnelView{}, fmt.Errorf("c2.OpenTunnel: connect to teamserver: %w", err)
	}

	spec := ma.Tunnel
	spec.LocalPort = 0 // OS-assigned to avoid local port clashes across engagements
	// The tunnel must outlive this request; its lifetime is bounded by CloseTunnel.
	tun, err := c2.OpenLocalForward(context.Background(), dialer, spec)
	if err != nil {
		return TunnelView{}, fmt.Errorf("c2.OpenTunnel: %w", err)
	}

	now := time.Now()
	id := uuid.NewString()
	rec := &tunnelRecord{
		tun:          tun,
		engagementID: engagementID,
		nodeID:       node.ID,
		framework:    ma.Framework,
		openerID:     actor.ID,
		openerName:   actor.Username,
		createdAt:    now,
		lastUsedAt:   now,
		expiresAt:    now.Add(s.absTTL),
	}
	s.mu.Lock()
	s.tunnels[id] = rec
	s.mu.Unlock()
	s.startJanitor()

	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: engagementID,
		Actor:        actor.Username,
		Action:       "c2.tunnel_open",
		Target:       node.ID,
		Detail:       fmt.Sprintf("framework=%s local=%s operator_port=%d expires=%s", ma.Framework, tun.LocalAddr(), ma.OperatorPort, rec.expiresAt.UTC().Format(time.RFC3339)),
		At:           now.UTC(),
	})

	return TunnelView{
		TunnelID:     id,
		LocalAddr:    tun.LocalAddr(),
		Framework:    ma.Framework,
		OperatorPort: ma.OperatorPort,
		ExpiresAt:    rec.expiresAt.UTC().Format(time.RFC3339),
	}, nil
}

// canManageTunnel reports whether the actor may close a tunnel: the opener, or
// an admin/lead.
func canManageTunnel(actor domain.User, rec *tunnelRecord) bool {
	if actor.Role == domain.RoleAdmin || actor.Role == domain.RoleLead {
		return true
	}
	return actor.ID != "" && actor.ID == rec.openerID
}

// CloseTunnel tears down a previously opened tunnel. It verifies the tunnel
// belongs to the given engagement and that the caller is the opener or an
// admin/lead.
func (s *C2Service) CloseTunnel(ctx context.Context, engagementID, tunnelID string, actor domain.User) error {
	s.mu.Lock()
	rec, ok := s.tunnels[tunnelID]
	if !ok || rec.engagementID != engagementID {
		s.mu.Unlock()
		return fmt.Errorf("c2.CloseTunnel: %w", store.ErrNotFound)
	}
	if !canManageTunnel(actor, rec) {
		s.mu.Unlock()
		return fmt.Errorf("c2.CloseTunnel: %w: only the opener or an admin/lead may close this tunnel", ErrUnauthorized)
	}
	delete(s.tunnels, tunnelID)
	s.mu.Unlock()

	err := rec.tun.Close()
	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: engagementID,
		Actor:        actor.Username,
		Action:       "c2.tunnel_close",
		Target:       tunnelID,
		Detail:       fmt.Sprintf("closed by %s (opener=%s)", actor.Username, rec.openerName),
		At:           time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("c2.CloseTunnel: %w", err)
	}
	return nil
}

// ListTunnels returns metadata for the engagement's active tunnels (reconcile /
// audit view). Secrets are never included.
func (s *C2Service) ListTunnels(_ context.Context, engagementID string) []TunnelInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TunnelInfo, 0)
	for id, rec := range s.tunnels {
		if rec.engagementID != engagementID {
			continue
		}
		out = append(out, TunnelInfo{
			TunnelID:  id,
			NodeID:    rec.nodeID,
			Framework: rec.framework,
			LocalAddr: rec.tun.LocalAddr(),
			Opener:    rec.openerName,
			CreatedAt: rec.createdAt.UTC().Format(time.RFC3339),
			ExpiresAt: rec.expiresAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

// ReapTunnels closes every tunnel past its idle or absolute TTL at time `now`,
// auditing each. It is invoked on a timer by the janitor and can be called
// directly. Returns the number of tunnels reaped.
func (s *C2Service) ReapTunnels(now time.Time) int {
	type expired struct {
		id  string
		rec *tunnelRecord
	}
	var dead []expired
	s.mu.Lock()
	for id, rec := range s.tunnels {
		if now.After(rec.expiresAt) || now.Sub(rec.lastUsedAt) > s.idleTTL {
			dead = append(dead, expired{id, rec})
			delete(s.tunnels, id)
		}
	}
	s.mu.Unlock()

	for _, d := range dead {
		_ = d.rec.tun.Close()
		_ = s.audit.Record(context.Background(), audit.Event{
			EngagementID: d.rec.engagementID,
			Actor:        "system",
			Action:       "c2.tunnel_expired",
			Target:       d.id,
			Detail:       fmt.Sprintf("idle/absolute TTL reached; opener=%s", d.rec.openerName),
			At:           now.UTC(),
		})
	}
	return len(dead)
}

// startJanitor launches the background reaper exactly once.
func (s *C2Service) startJanitor() {
	s.janitorOnce.Do(func() {
		go func() {
			t := time.NewTicker(s.janitorInterval())
			defer t.Stop()
			for {
				select {
				case <-s.stopJanitor:
					return
				case <-t.C:
					s.ReapTunnels(time.Now())
				}
			}
		}()
	})
}

func (s *C2Service) janitorInterval() time.Duration {
	iv := s.idleTTL
	if s.absTTL < iv {
		iv = s.absTTL
	}
	iv /= 4
	if iv < time.Second {
		iv = time.Second
	}
	if iv > time.Minute {
		iv = time.Minute
	}
	return iv
}

// Shutdown stops the janitor and closes all active tunnels. Wire it into the
// server's graceful-shutdown path so no tunnel is orphaned on exit.
func (s *C2Service) Shutdown() {
	s.stopOnce.Do(func() { close(s.stopJanitor) })
	s.mu.Lock()
	recs := s.tunnels
	s.tunnels = make(map[string]*tunnelRecord)
	s.mu.Unlock()
	for id, rec := range recs {
		_ = rec.tun.Close()
		_ = s.audit.Record(context.Background(), audit.Event{
			EngagementID: rec.engagementID,
			Actor:        "system",
			Action:       "c2.tunnel_close",
			Target:       id,
			Detail:       "closed on shutdown",
			At:           time.Now().UTC(),
		})
	}
}

// liveC2Node finds the engagement's first live C2 server node and its provider,
// mirroring RegistryResolver's selection.
func (s *C2Service) liveC2Node(ctx context.Context, engagementID string) (domain.Node, c2.C2Provider, error) {
	nodes, err := s.infra.NodesForEngagement(ctx, engagementID)
	if err != nil {
		return domain.Node{}, nil, fmt.Errorf("c2: load nodes: %w", err)
	}
	for _, n := range nodes {
		if n.Spec.Type != domain.NodeC2Server || n.Status != domain.NodeLive || n.Spec.C2Framework == "" {
			continue
		}
		provider, err := c2.Get(n.Spec.C2Framework)
		if err != nil {
			continue
		}
		return n, provider, nil
	}
	return domain.Node{}, nil, fmt.Errorf("c2: no live C2 server node for engagement %s: %w", engagementID, store.ErrNotFound)
}

// c2NodeByID resolves a specific live C2 server node (and its provider) by node
// id within an engagement.
func (s *C2Service) c2NodeByID(ctx context.Context, engagementID, nodeID string) (domain.Node, c2.C2Provider, error) {
	nodes, err := s.infra.NodesForEngagement(ctx, engagementID)
	if err != nil {
		return domain.Node{}, nil, fmt.Errorf("c2: load nodes: %w", err)
	}
	for _, n := range nodes {
		if n.ID != nodeID {
			continue
		}
		if n.Spec.Type != domain.NodeC2Server || n.Spec.C2Framework == "" {
			return domain.Node{}, nil, fmt.Errorf("c2: node %s is not a C2 server: %w", nodeID, store.ErrNotFound)
		}
		if n.Status != domain.NodeLive {
			return domain.Node{}, nil, fmt.Errorf("c2: node %s is not live: %w", nodeID, store.ErrNotFound)
		}
		provider, err := c2.Get(n.Spec.C2Framework)
		if err != nil {
			return domain.Node{}, nil, fmt.Errorf("c2: framework %q: %w", n.Spec.C2Framework, err)
		}
		return n, provider, nil
	}
	return domain.Node{}, nil, fmt.Errorf("c2: node %s not found in engagement %s: %w", nodeID, engagementID, store.ErrNotFound)
}

// ShellInfo describes a live operator shell session bound to one teamserver.
// It is the context the in-browser web shell interpreter operates against.
type ShellInfo struct {
	NodeID       string
	Framework    string
	Listener     string
	Host         string
	OperatorPort int
	Client       string
	Protocol     string
}

// OpenShell authorizes and describes a web-shell session for a specific live C2
// node. Like every privileged path it gates on CanDeploy and is audited; the
// caller (WebSocket handler) then streams commands through RespondShell.
func (s *C2Service) OpenShell(ctx context.Context, engagementID, nodeID, actor string) (ShellInfo, error) {
	eng, err := s.engagements.Get(ctx, engagementID)
	if err != nil {
		return ShellInfo{}, fmt.Errorf("c2.OpenShell: %w", err)
	}
	if err := eng.CanDeploy(time.Now()); err != nil {
		return ShellInfo{}, fmt.Errorf("c2.OpenShell: %w", err)
	}

	node, provider, err := s.c2NodeByID(ctx, engagementID, nodeID)
	if err != nil {
		return ShellInfo{}, err
	}
	ma, err := c2.ManualAccessFor(provider, c2.Teamserver{Host: node.PublicIP, Status: string(node.Status)})
	if err != nil {
		return ShellInfo{}, fmt.Errorf("c2.OpenShell: %w", err)
	}

	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: engagementID,
		Actor:        actor,
		Action:       "c2.shell_open",
		Target:       node.ID,
		Detail:       fmt.Sprintf("framework=%s operator_port=%d", ma.Framework, ma.OperatorPort),
		At:           time.Now().UTC(),
	})

	return ShellInfo{
		NodeID:       node.ID,
		Framework:    ma.Framework,
		Listener:     node.Spec.ProfileName,
		Host:         node.PublicIP,
		OperatorPort: ma.OperatorPort,
		Client:       ma.Client,
		Protocol:     string(ma.Protocol),
	}, nil
}

// ShellClear is the sentinel the terminal interprets as "clear the screen".
// It mirrors the web client's CLEAR constant.
const ShellClear = "\x00CLEAR\x00"

const shellHelp = `Commands:
  help        show this help
  info        teamserver / listener details
  sessions    list active agent sessions
  whoami      current operator identity
  ps          processes on the active session
  netstat     active connections on the teamserver
  clear       clear the screen
  exit        close this shell
`

// ShellBanner is the greeting written when a shell session opens.
func ShellBanner(info ShellInfo) string {
	return fmt.Sprintf(
		"RInfra web shell — %s operator console\nConnected to %s (%s) over the control plane.\nType 'help' for commands.\n\n",
		info.Framework, info.NodeID, info.Host,
	)
}

// RespondShell interprets one operator command line against a live teamserver
// and returns (output, closed). It is a controlled, read-only command surface —
// it never executes arbitrary commands on the control plane — so it is safe to
// expose over the authenticated, engagement-gated WebSocket.
func RespondShell(info ShellInfo, line string) (string, bool) {
	cmd := strings.TrimSpace(line)
	if cmd == "" {
		return "", false
	}
	fields := strings.Fields(cmd)
	switch fields[0] {
	case "help":
		return shellHelp, false
	case "clear":
		return ShellClear, false
	case "exit", "quit":
		return "closing session…\n", true
	case "info":
		return fmt.Sprintf(
			"Framework : %s\nListener  : %s\nHost      : %s\nOperator  : %s :%d (%s)\n",
			info.Framework, info.Listener, info.Host, info.Client, info.OperatorPort, info.Protocol,
		), false
	case "sessions":
		return "No active sessions reported by the operator API.\n", false
	case "whoami":
		return "operator\n", false
	case "ps":
		return "no active session — agents connect through the operator API\n", false
	case "netstat":
		return fmt.Sprintf("Proto  Local                 State\n tcp   %s:%d            LISTEN\n", info.Host, info.OperatorPort), false
	default:
		return fmt.Sprintf("%s: unknown command (try 'help')\n", fields[0]), false
	}
}
