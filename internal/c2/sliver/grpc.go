package sliver

import (
	"context"
	"fmt"

	"github.com/bishopfox/sliver/protobuf/clientpb"
	"github.com/bishopfox/sliver/protobuf/commonpb"
	"github.com/bishopfox/sliver/protobuf/rpcpb"
	"github.com/bishopfox/sliver/protobuf/sliverpb"
	"google.golang.org/grpc"
)

// grpcSliverClient is the live SliverClient implementation. It issues REAL
// gRPC calls against the upstream Sliver teamserver via the official generated
// stub (rpcpb.SliverRPCClient). RInfra authors no implants or payloads here; it
// only drives the operator API exposed by sliver-server's multiplayer listener.
//
// The mTLS transport is supplied by the caller: in production Control builds a
// *grpc.ClientConn from the operator config generated during Deploy
// (see DialOperator in transport.go), so the secret mTLS material lives in the
// operator config, not in this package. Tests inject an in-process conn over a
// plaintext/bufconn listener.
type grpcSliverClient struct {
	rpc  rpcpb.SliverRPCClient
	conn *grpc.ClientConn // owned only when created via DialAndWrap; nil when wrapping a borrowed conn
}

// NewGRPCClient wraps an existing, already-dialed *grpc.ClientConn with the
// generated Sliver RPC stub. The caller retains ownership of conn and is
// responsible for closing it. Use this when the operator config / mTLS dial is
// managed elsewhere (e.g. Control or a test harness).
func NewGRPCClient(conn *grpc.ClientConn) *grpcSliverClient {
	return &grpcSliverClient{rpc: rpcpb.NewSliverRPCClient(conn)}
}

// DialOperatorClient loads the Sliver operator config, dials the multiplayer
// listener over mTLS (DialOperator), and returns a live SliverClient bound to
// that connection. The returned client owns the connection; call Close when
// done. This is the production entry point used when an operator config path is
// available.
func DialOperatorClient(ctx context.Context, cfg OperatorConfig, opts ...grpc.DialOption) (*grpcSliverClient, error) {
	conn, err := DialOperator(ctx, cfg, opts...)
	if err != nil {
		return nil, err
	}
	return &grpcSliverClient{rpc: rpcpb.NewSliverRPCClient(conn), conn: conn}, nil
}

// Close closes the underlying connection if this client owns it (created via
// DialOperatorClient). Clients that wrap a borrowed conn (NewGRPCClient) are a
// no-op so the caller's conn lifecycle is untouched.
func (c *grpcSliverClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// StartMTLSListener -> SliverRPC.StartMTLSListener.
func (c *grpcSliverClient) StartMTLSListener(ctx context.Context, host string, port uint32) error {
	_, err := c.rpc.StartMTLSListener(ctx, &clientpb.MTLSListenerReq{
		Host: host,
		Port: port,
	})
	if err != nil {
		return fmt.Errorf("sliver: StartMTLSListener: %w", err)
	}
	return nil
}

// StartHTTPSListener -> SliverRPC.StartHTTPSListener. Secure=true selects HTTPS.
func (c *grpcSliverClient) StartHTTPSListener(ctx context.Context, domain string, port uint32) error {
	_, err := c.rpc.StartHTTPSListener(ctx, &clientpb.HTTPListenerReq{
		Domain: domain,
		Port:   port,
		Secure: true,
	})
	if err != nil {
		return fmt.Errorf("sliver: StartHTTPSListener: %w", err)
	}
	return nil
}

// StartDNSListener -> SliverRPC.StartDNSListener.
func (c *grpcSliverClient) StartDNSListener(ctx context.Context, domains []string) error {
	_, err := c.rpc.StartDNSListener(ctx, &clientpb.DNSListenerReq{
		Domains: domains,
	})
	if err != nil {
		return fmt.Errorf("sliver: StartDNSListener: %w", err)
	}
	return nil
}

// Sessions -> SliverRPC.GetSessions, converting clientpb.Sessions into the
// package-local SliverSession shape.
func (c *grpcSliverClient) Sessions(ctx context.Context) ([]SliverSession, error) {
	resp, err := c.rpc.GetSessions(ctx, &commonpb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("sliver: GetSessions: %w", err)
	}
	out := make([]SliverSession, 0, len(resp.GetSessions()))
	for _, s := range resp.GetSessions() {
		if s == nil {
			continue
		}
		out = append(out, SliverSession{
			ID:       s.GetID(),
			Hostname: s.GetHostname(),
			Username: s.GetUsername(),
			OS:       s.GetOS(),
			Arch:     s.GetArch(),
		})
	}
	return out, nil
}

// Execute -> SliverRPC.Execute. Sliver's Execute runs a binary (Path) with Args
// on the target session, not a shell string, so the command is split into a
// program and its arguments. Output is requested and combined (stdout+stderr)
// for the caller. The session is referenced via the commonpb.Request envelope.
func (c *grpcSliverClient) Execute(ctx context.Context, sessionID, command string) (string, error) {
	path, args := splitCommand(command)
	if path == "" {
		return "", fmt.Errorf("sliver: Execute: empty command")
	}
	resp, err := c.rpc.Execute(ctx, &sliverpb.ExecuteReq{
		Path:   path,
		Args:   args,
		Output: true,
		Request: &commonpb.Request{
			SessionID: sessionID,
		},
	})
	if err != nil {
		return "", fmt.Errorf("sliver: Execute: %w", err)
	}
	combined := string(resp.GetStdout())
	if errOut := string(resp.GetStderr()); errOut != "" {
		if combined != "" {
			combined += "\n"
		}
		combined += errOut
	}
	return combined, nil
}
