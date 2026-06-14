package service

import (
	"context"
	"fmt"
	"log/slog"
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

	mu      sync.Mutex
	tunnels map[string]*c2.Tunnel
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
		tunnels:     make(map[string]*c2.Tunnel),
	}
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

	ma, err := c2.ManualAccessFor(provider, c2.Teamserver{Host: node.PublicIP, Status: string(node.Status)})
	if err != nil {
		return ManualAccessView{}, fmt.Errorf("c2.ManualAccess: %w", err)
	}

	keyHint := s.sshKeyHint
	if keyHint == "" {
		keyHint = "<engagement-ssh-key>"
	}

	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: engagementID,
		Actor:        actor,
		Action:       "c2.manual_access",
		Target:       node.ID,
		Detail:       fmt.Sprintf("framework=%s client=%s operator_port=%d", ma.Framework, ma.Client, ma.OperatorPort),
		At:           time.Now().UTC(),
	})

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

// TunnelView identifies a live local port-forward.
type TunnelView struct {
	TunnelID     string `json:"tunnelId"`
	LocalAddr    string `json:"localAddr"`
	Framework    string `json:"framework"`
	OperatorPort int    `json:"operatorPort"`
}

// OpenTunnel opens an SSH local port-forward to the engagement's C2 teamserver
// operator port and returns the local address the operator points their native
// client at. Gated by CanDeploy and audited. Requires a tunnel dialer (see
// WithTunnelDialer); otherwise it reports the feature is not configured.
func (s *C2Service) OpenTunnel(ctx context.Context, engagementID, actor string) (TunnelView, error) {
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

	id := uuid.NewString()
	s.mu.Lock()
	s.tunnels[id] = tun
	s.mu.Unlock()

	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: engagementID,
		Actor:        actor,
		Action:       "c2.tunnel_open",
		Target:       node.ID,
		Detail:       fmt.Sprintf("framework=%s local=%s operator_port=%d", ma.Framework, tun.LocalAddr(), ma.OperatorPort),
		At:           time.Now().UTC(),
	})

	return TunnelView{
		TunnelID:     id,
		LocalAddr:    tun.LocalAddr(),
		Framework:    ma.Framework,
		OperatorPort: ma.OperatorPort,
	}, nil
}

// CloseTunnel tears down a previously opened tunnel.
func (s *C2Service) CloseTunnel(ctx context.Context, engagementID, tunnelID, actor string) error {
	s.mu.Lock()
	tun, ok := s.tunnels[tunnelID]
	if ok {
		delete(s.tunnels, tunnelID)
	}
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("c2.CloseTunnel: %w", store.ErrNotFound)
	}
	err := tun.Close()
	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: engagementID,
		Actor:        actor,
		Action:       "c2.tunnel_close",
		Target:       tunnelID,
		Detail:       "tunnel closed",
		At:           time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("c2.CloseTunnel: %w", err)
	}
	return nil
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
