package sliver

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestWireCodec_RoundTrip(t *testing.T) {
	t.Run("executeReq", func(t *testing.T) {
		in := &executeReq{
			Path:    "powershell.exe",
			Args:    []string{"-c", "whoami /all"},
			Output:  true,
			Request: &requestEnvelope{SessionID: "sess-1", Timeout: 60},
		}
		out := &executeReq{}
		roundTrip(t, in, out)
		if out.Path != in.Path || out.Output != true {
			t.Fatalf("path/output mismatch: %+v", out)
		}
		if len(out.Args) != 2 || out.Args[0] != "-c" || out.Args[1] != "whoami /all" {
			t.Fatalf("args mismatch: %#v", out.Args)
		}
		if out.Request == nil || out.Request.SessionID != "sess-1" || out.Request.Timeout != 60 {
			t.Fatalf("request envelope mismatch: %+v", out.Request)
		}
	})

	t.Run("executeResp", func(t *testing.T) {
		in := &executeResp{
			Status:   1,
			Stdout:   []byte("hello"),
			Stderr:   []byte("oops"),
			Pid:      4242,
			Response: &responseEnvelope{Err: "implant boom"},
		}
		out := &executeResp{}
		roundTrip(t, in, out)
		if out.Status != 1 || out.Pid != 4242 {
			t.Fatalf("scalar mismatch: %+v", out)
		}
		if string(out.Stdout) != "hello" || string(out.Stderr) != "oops" {
			t.Fatalf("byte fields mismatch: %q %q", out.Stdout, out.Stderr)
		}
		if out.Response == nil || out.Response.Err != "implant boom" {
			t.Fatalf("response envelope mismatch: %+v", out.Response)
		}
	})

	t.Run("sessionsResp", func(t *testing.T) {
		in := &sessionsResp{Sessions: []sessionMsg{
			{ID: "a", Hostname: "host-a", Username: "ua", OS: "windows", Arch: "amd64"},
			{ID: "b", Hostname: "host-b", Username: "ub", OS: "linux", Arch: "arm64"},
		}}
		out := &sessionsResp{}
		roundTrip(t, in, out)
		if len(out.Sessions) != 2 {
			t.Fatalf("expected 2 sessions, got %d", len(out.Sessions))
		}
		if out.Sessions[1].ID != "b" || out.Sessions[1].OS != "linux" || out.Sessions[1].Arch != "arm64" {
			t.Fatalf("session[1] mismatch: %+v", out.Sessions[1])
		}
	})

	t.Run("listenerReqsAndJob", func(t *testing.T) {
		mtls := &mtlsListenerReq{Host: "0.0.0.0", Port: 4444}
		mout := &mtlsListenerReq{}
		roundTrip(t, mtls, mout)
		if mout.Host != "0.0.0.0" || mout.Port != 4444 {
			t.Fatalf("mtls req mismatch: %+v", mout)
		}

		dns := &dnsListenerReq{Domains: []string{"a.com", "b.com"}, Canaries: true}
		dout := &dnsListenerReq{}
		roundTrip(t, dns, dout)
		if len(dout.Domains) != 2 || !dout.Canaries {
			t.Fatalf("dns req mismatch: %+v", dout)
		}

		https := &httpListenerReq{Domain: "c2.example.com", Port: 443, Secure: true}
		hout := &httpListenerReq{}
		roundTrip(t, https, hout)
		if hout.Domain != "c2.example.com" || hout.Port != 443 || !hout.Secure {
			t.Fatalf("https req mismatch: %+v", hout)
		}

		job := &jobResp{JobID: 7}
		jout := &jobResp{}
		roundTrip(t, job, jout)
		if jout.JobID != 7 {
			t.Fatalf("job resp mismatch: %+v", jout)
		}
	})
}

func TestWireCodec_RejectsNonMessage(t *testing.T) {
	c := wireCodec{}
	if _, err := c.Marshal(123); err == nil {
		t.Error("expected Marshal error for non-wireMessage")
	}
	var notMsg int
	if err := c.Unmarshal([]byte{0x08, 0x01}, &notMsg); err == nil {
		t.Error("expected Unmarshal error for non-wireMessage")
	}
	if c.Name() != "proto" {
		t.Errorf("codec name = %q, want proto", c.Name())
	}
}

// roundTrip marshals in through the codec and unmarshals into out.
func roundTrip(t *testing.T, in, out wireMessage) {
	t.Helper()
	c := wireCodec{}
	data, err := c.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := c.Unmarshal(data, out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}

// TestLiveClient_OverGRPC exercises the live client against an in-process gRPC
// server that speaks the same wire codec under the rpcpb.SliverRPC service
// name. This proves ForceCodec + Invoke + the protowire (de)serialization
// interoperate over a real gRPC connection — the only piece that can't be
// verified is the field numbers vs. a live teamserver (pinned to v1.5.42).
func TestLiveClient_OverGRPC(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer(grpc.ForceServerCodec(wireCodec{}))
	srv.RegisterService(&testSliverRPCDesc, nil)
	go srv.Serve(lis)
	defer srv.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client := newLiveClient(conn, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("Sessions", func(t *testing.T) {
		sessions, err := client.Sessions(ctx)
		if err != nil {
			t.Fatalf("Sessions: %v", err)
		}
		if len(sessions) != 1 || sessions[0].ID != "s1" || sessions[0].OS != "windows" {
			t.Fatalf("unexpected sessions: %+v", sessions)
		}
	})

	t.Run("Execute_echo", func(t *testing.T) {
		out, err := client.Execute(ctx, "sess-9", `powershell -c "whoami"`)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		// Server echoes the decoded request, proving path/args/session survived
		// the wire round trip in both directions.
		for _, want := range []string{"path=powershell", "args=-c|whoami", "sid=sess-9"} {
			if !strings.Contains(out, want) {
				t.Errorf("echo missing %q in %q", want, out)
			}
		}
	})

	t.Run("Execute_implantError", func(t *testing.T) {
		_, err := client.Execute(ctx, "sess-9", "fail")
		if err == nil || !strings.Contains(err.Error(), "kaboom") {
			t.Fatalf("expected implant error, got %v", err)
		}
	})

	t.Run("Listeners", func(t *testing.T) {
		if err := client.StartMTLSListener(ctx, "0.0.0.0", 4444); err != nil {
			t.Errorf("StartMTLSListener: %v", err)
		}
		if err := client.StartHTTPSListener(ctx, "c2.example.com", 443); err != nil {
			t.Errorf("StartHTTPSListener: %v", err)
		}
		if err := client.StartDNSListener(ctx, []string{"a.com"}); err != nil {
			t.Errorf("StartDNSListener: %v", err)
		}
	})
}

// --- in-process rpcpb.SliverRPC test server ---

var testSliverRPCDesc = grpc.ServiceDesc{
	ServiceName: "rpcpb.SliverRPC",
	HandlerType: (*any)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "GetSessions", Handler: handleGetSessions},
		{MethodName: "Execute", Handler: handleExecute},
		{MethodName: "StartMTLSListener", Handler: handleListener},
		{MethodName: "StartDNSListener", Handler: handleListener},
		{MethodName: "StartHTTPSListener", Handler: handleListener},
	},
}

func handleGetSessions(_ any, _ context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
	if err := dec(&emptyMsg{}); err != nil {
		return nil, err
	}
	return &sessionsResp{Sessions: []sessionMsg{
		{ID: "s1", Hostname: "ws01", Username: "CORP\\alice", OS: "windows", Arch: "amd64"},
	}}, nil
}

func handleExecute(_ any, _ context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
	in := &executeReq{}
	if err := dec(in); err != nil {
		return nil, err
	}
	if in.Path == "fail" {
		return &executeResp{Response: &responseEnvelope{Err: "kaboom"}}, nil
	}
	sid := ""
	if in.Request != nil {
		sid = in.Request.SessionID
	}
	echo := "path=" + in.Path + " args=" + strings.Join(in.Args, "|") + " sid=" + sid
	return &executeResp{Stdout: []byte(echo)}, nil
}

func handleListener(_ any, _ context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
	// Decode into a permissive request; we only assert the call dispatched.
	if err := dec(&mtlsListenerReq{}); err != nil {
		return nil, err
	}
	return &jobResp{JobID: 1}, nil
}
