package sliver

import (
	"context"
	"net"
	"testing"

	"github.com/bishopfox/sliver/protobuf/clientpb"
	"github.com/bishopfox/sliver/protobuf/commonpb"
	"github.com/bishopfox/sliver/protobuf/rpcpb"
	"github.com/bishopfox/sliver/protobuf/sliverpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// fakeSliverRPCServer is an in-process implementation of the official
// rpcpb.SliverRPCServer. It implements only the handful of RPCs RInfra wires and
// records the requests it received so tests can assert the client maps fields
// correctly. Embedding UnimplementedSliverRPCServer satisfies the rest of the
// large service interface with "unimplemented" stubs.
type fakeSliverRPCServer struct {
	rpcpb.UnimplementedSliverRPCServer

	gotMTLS  *clientpb.MTLSListenerReq
	gotHTTPS *clientpb.HTTPListenerReq
	gotDNS   *clientpb.DNSListenerReq
	gotExec  *sliverpb.ExecuteReq

	sessions   []*clientpb.Session
	execStdout []byte
	execStderr []byte
}

func (s *fakeSliverRPCServer) StartMTLSListener(_ context.Context, req *clientpb.MTLSListenerReq) (*clientpb.MTLSListener, error) {
	s.gotMTLS = req
	return &clientpb.MTLSListener{JobID: 1}, nil
}

func (s *fakeSliverRPCServer) StartHTTPSListener(_ context.Context, req *clientpb.HTTPListenerReq) (*clientpb.HTTPListener, error) {
	s.gotHTTPS = req
	return &clientpb.HTTPListener{JobID: 2}, nil
}

func (s *fakeSliverRPCServer) StartDNSListener(_ context.Context, req *clientpb.DNSListenerReq) (*clientpb.DNSListener, error) {
	s.gotDNS = req
	return &clientpb.DNSListener{JobID: 3}, nil
}

func (s *fakeSliverRPCServer) GetSessions(context.Context, *commonpb.Empty) (*clientpb.Sessions, error) {
	return &clientpb.Sessions{Sessions: s.sessions}, nil
}

func (s *fakeSliverRPCServer) Execute(_ context.Context, req *sliverpb.ExecuteReq) (*sliverpb.Execute, error) {
	s.gotExec = req
	return &sliverpb.Execute{
		Stdout: s.execStdout,
		Stderr: s.execStderr,
	}, nil
}

// startFakeServer brings up the fake Sliver RPC server on an in-memory bufconn
// listener and returns a SliverClient wired over a real gRPC connection to it.
func startFakeServer(t *testing.T, fake *fakeSliverRPCServer) *grpcSliverClient {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	rpcpb.RegisterSliverRPCServer(srv, fake)

	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return NewGRPCClient(conn)
}

func TestGRPCClient_StartMTLSListener(t *testing.T) {
	fake := &fakeSliverRPCServer{}
	client := startFakeServer(t, fake)

	if err := client.StartMTLSListener(context.Background(), "0.0.0.0", 4444); err != nil {
		t.Fatalf("StartMTLSListener: %v", err)
	}
	if fake.gotMTLS == nil {
		t.Fatal("server did not receive MTLS request")
	}
	if fake.gotMTLS.GetHost() != "0.0.0.0" || fake.gotMTLS.GetPort() != 4444 {
		t.Errorf("got host=%q port=%d, want 0.0.0.0:4444", fake.gotMTLS.GetHost(), fake.gotMTLS.GetPort())
	}
}

func TestGRPCClient_StartHTTPSListener(t *testing.T) {
	fake := &fakeSliverRPCServer{}
	client := startFakeServer(t, fake)

	if err := client.StartHTTPSListener(context.Background(), "example.com", 443); err != nil {
		t.Fatalf("StartHTTPSListener: %v", err)
	}
	if fake.gotHTTPS == nil {
		t.Fatal("server did not receive HTTPS request")
	}
	if fake.gotHTTPS.GetDomain() != "example.com" || fake.gotHTTPS.GetPort() != 443 {
		t.Errorf("got domain=%q port=%d, want example.com:443", fake.gotHTTPS.GetDomain(), fake.gotHTTPS.GetPort())
	}
	if !fake.gotHTTPS.GetSecure() {
		t.Error("expected Secure=true for HTTPS listener")
	}
}

func TestGRPCClient_StartDNSListener(t *testing.T) {
	fake := &fakeSliverRPCServer{}
	client := startFakeServer(t, fake)

	domains := []string{"c2.example.com", "alt.example.com"}
	if err := client.StartDNSListener(context.Background(), domains); err != nil {
		t.Fatalf("StartDNSListener: %v", err)
	}
	if fake.gotDNS == nil {
		t.Fatal("server did not receive DNS request")
	}
	if got := fake.gotDNS.GetDomains(); len(got) != 2 || got[0] != "c2.example.com" || got[1] != "alt.example.com" {
		t.Errorf("got domains=%v, want %v", got, domains)
	}
}

func TestGRPCClient_Sessions(t *testing.T) {
	fake := &fakeSliverRPCServer{
		sessions: []*clientpb.Session{
			{ID: "sess-1", Hostname: "WIN-DC01", Username: "Administrator", OS: "windows", Arch: "amd64"},
			{ID: "sess-2", Hostname: "lin-box", Username: "root", OS: "linux", Arch: "arm64"},
		},
	}
	client := startFakeServer(t, fake)

	got, err := client.Sessions(context.Background())
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d sessions, want 2", len(got))
	}
	want := SliverSession{ID: "sess-1", Hostname: "WIN-DC01", Username: "Administrator", OS: "windows", Arch: "amd64"}
	if got[0] != want {
		t.Errorf("session[0] = %+v, want %+v", got[0], want)
	}
	if got[1].ID != "sess-2" || got[1].Arch != "arm64" || got[1].OS != "linux" {
		t.Errorf("session[1] mapped incorrectly: %+v", got[1])
	}
}

func TestGRPCClient_Execute(t *testing.T) {
	fake := &fakeSliverRPCServer{
		execStdout: []byte("nt authority\\system"),
	}
	client := startFakeServer(t, fake)

	out, err := client.Execute(context.Background(), "sess-1", "whoami /all")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "nt authority\\system" {
		t.Errorf("stdout = %q, want %q", out, "nt authority\\system")
	}
	if fake.gotExec == nil {
		t.Fatal("server did not receive Execute request")
	}
	if fake.gotExec.GetPath() != "whoami" {
		t.Errorf("Path = %q, want whoami", fake.gotExec.GetPath())
	}
	if args := fake.gotExec.GetArgs(); len(args) != 1 || args[0] != "/all" {
		t.Errorf("Args = %v, want [/all]", args)
	}
	if !fake.gotExec.GetOutput() {
		t.Error("expected Output=true")
	}
	if fake.gotExec.GetRequest().GetSessionID() != "sess-1" {
		t.Errorf("SessionID = %q, want sess-1", fake.gotExec.GetRequest().GetSessionID())
	}
}

func TestGRPCClient_ExecuteCombinesStderr(t *testing.T) {
	fake := &fakeSliverRPCServer{
		execStdout: []byte("out"),
		execStderr: []byte("err"),
	}
	client := startFakeServer(t, fake)

	out, err := client.Execute(context.Background(), "sess-1", "cmd")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "out\nerr" {
		t.Errorf("combined output = %q, want %q", out, "out\nerr")
	}
}

func TestGRPCClient_ExecuteEmptyCommand(t *testing.T) {
	fake := &fakeSliverRPCServer{}
	client := startFakeServer(t, fake)

	if _, err := client.Execute(context.Background(), "sess-1", "   "); err == nil {
		t.Fatal("expected error for empty command")
	}
}
