package metasploit

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rinfra/rinfra/internal/c2"
)

// cannedMsfServer stands up an httptest server that decodes the MessagePack
// request array, switches on the method name, and returns canned MessagePack
// responses. It records the last decoded call so tests can assert argument
// wiring (e.g. that the auth token is passed as arg 1).
type cannedMsfServer struct {
	token    string
	lastCall []any
}

func (s *cannedMsfServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "binary/message-pack" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write(msgpackEncode(map[string]any{"error": true, "error_message": "bad content-type " + ct}))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		v, _, err := msgpackDecode(raw)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write(msgpackEncode(map[string]any{"error": true, "error_message": "decode: " + err.Error()}))
			return
		}
		parts, _ := v.([]any)
		s.lastCall = parts
		method := asString(parts[0])

		switch method {
		case "auth.login":
			_, _ = w.Write(msgpackEncode(map[string]any{"result": "success", "token": s.token}))
		case "console.create":
			_, _ = w.Write(msgpackEncode(map[string]any{"id": "7", "prompt": "msf6 > ", "busy": false}))
		case "console.write":
			_, _ = w.Write(msgpackEncode(map[string]any{"wrote": int64(len(asString(parts[3])))}))
		case "console.read":
			_, _ = w.Write(msgpackEncode(map[string]any{"data": "[*] listener up", "prompt": "msf6 > ", "busy": false}))
		case "session.list":
			_, _ = w.Write(msgpackEncode(map[string]any{
				"3": map[string]any{
					"type":        "meterpreter",
					"info":        "NT AUTHORITY\\SYSTEM @ HOST3",
					"via_exploit": "exploit/multi/handler",
				},
			}))
		case "session.shell_write":
			_, _ = w.Write(msgpackEncode(map[string]any{"write_count": int64(len(asString(parts[3])))}))
		case "session.shell_read":
			_, _ = w.Write(msgpackEncode(map[string]any{"seq": "0", "data": "uid=0(root)\n"}))
		default:
			// Surface anything unexpected as an msfrpcd-style error map.
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write(msgpackEncode(map[string]any{"error": true, "error_message": "unknown method " + method}))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newAuthedClient builds an httpMsfClient (liveClient) against srv and logs in.
func newAuthedClient(t *testing.T, srv *httptest.Server) *liveClient {
	t.Helper()
	c, err := newLiveClient(LiveConfig{BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("newLiveClient: %v", err)
	}
	if err := c.Auth(context.Background(), "msf", "pw"); err != nil {
		t.Fatalf("Auth: %v", err)
	}
	return c
}

func TestHTTPMsfClient_AuthStoresToken(t *testing.T) {
	s := &cannedMsfServer{token: "TOK-ABC"}
	srv := s.start(t)

	c, err := newLiveClient(LiveConfig{BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("newLiveClient: %v", err)
	}
	if c.token != "" {
		t.Fatalf("token set before Auth: %q", c.token)
	}
	if err := c.Auth(context.Background(), "msf", "pw"); err != nil {
		t.Fatalf("Auth: %v", err)
	}
	if c.token != "TOK-ABC" {
		t.Fatalf("token = %q, want TOK-ABC", c.token)
	}
}

func TestHTTPMsfClient_MethodsRoundTrip(t *testing.T) {
	s := &cannedMsfServer{token: "TOK-ABC"}
	srv := s.start(t)
	c := newAuthedClient(t, srv)
	ctx := context.Background()

	t.Run("ConsoleCreate", func(t *testing.T) {
		id, err := c.ConsoleCreate(ctx)
		if err != nil || id != "7" {
			t.Fatalf("ConsoleCreate id=%q err=%v", id, err)
		}
		// Token must be carried as arg 1 of the RPC array.
		if got := asString(s.lastCall[1]); got != "TOK-ABC" {
			t.Fatalf("console.create token arg = %q, want TOK-ABC", got)
		}
	})

	t.Run("ConsoleWriteRead", func(t *testing.T) {
		if err := c.ConsoleWrite(ctx, "7", "use exploit/multi/handler\n"); err != nil {
			t.Fatalf("ConsoleWrite: %v", err)
		}
		out, err := c.ConsoleRead(ctx, "7")
		if err != nil || out != "[*] listener up" {
			t.Fatalf("ConsoleRead out=%q err=%v", out, err)
		}
	})

	t.Run("SessionList", func(t *testing.T) {
		sessions, err := c.SessionList(ctx)
		if err != nil {
			t.Fatalf("SessionList: %v", err)
		}
		if len(sessions) != 1 {
			t.Fatalf("got %d sessions, want 1", len(sessions))
		}
		got := sessions[0]
		if got.ID != "3" || got.Type != "meterpreter" || got.ViaExploit != "exploit/multi/handler" {
			t.Fatalf("unexpected session: %+v", got)
		}
		if !strings.Contains(got.Info, "HOST3") {
			t.Fatalf("session info = %q, want it to contain HOST3", got.Info)
		}
	})

	t.Run("SessionShellWriteReadIntegerID", func(t *testing.T) {
		if err := c.SessionShellWrite(ctx, "3", "id\n"); err != nil {
			t.Fatalf("SessionShellWrite: %v", err)
		}
		// msfrpcd expects a numeric session id as an integer, not a string.
		if got, ok := s.lastCall[2].(int64); !ok || got != 3 {
			t.Fatalf("session id arg = %v (%T), want int 3", s.lastCall[2], s.lastCall[2])
		}
		out, err := c.SessionShellRead(ctx, "3")
		if err != nil || out != "uid=0(root)\n" {
			t.Fatalf("SessionShellRead out=%q err=%v", out, err)
		}
	})
}

func TestHTTPMsfClient_ErrorMapBecomesError(t *testing.T) {
	// Server always replies with an msfrpcd error map.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(msgpackEncode(map[string]any{"error": true, "error_message": "Invalid Session ID"}))
	}))
	defer srv.Close()

	c, err := newLiveClient(LiveConfig{BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("newLiveClient: %v", err)
	}
	// Bypass Auth (which would also fail) by exercising a method directly.
	c.token = "TOK"
	_, err = c.SessionList(context.Background())
	if err == nil {
		t.Fatal("expected error from error map, got nil")
	}
	if !strings.Contains(err.Error(), "Invalid Session ID") {
		t.Fatalf("error %q should surface the msfrpcd error_message", err)
	}
}

func TestControl_BuildsLiveClient(t *testing.T) {
	p := &provider{}
	op, ok := p.Control(c2.Teamserver{Host: "10.0.0.5", Port: msfRpcdPort})
	if !ok {
		t.Fatal("expected ok=true")
	}
	o, isOp := op.(*operator)
	if !isOp {
		t.Fatalf("Control returned %T, want *operator", op)
	}
	lc, isLive := o.client.(*liveClient)
	if !isLive {
		t.Fatalf("operator client is %T, want *liveClient", o.client)
	}
	wantURL := "https://10.0.0.5:55553" + msfRPCPath
	if lc.url != wantURL {
		t.Fatalf("client url = %q, want %q", lc.url, wantURL)
	}
}
