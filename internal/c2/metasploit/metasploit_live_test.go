package metasploit

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestMsgpack_RoundTrip(t *testing.T) {
	cases := []any{
		nil,
		true,
		false,
		int64(0),
		int64(127),
		int64(-1),
		int64(-32),
		int64(200),
		int64(70000),
		int64(-5000),
		"short",
		"",
		[]any{int64(1), "two", true, nil},
		map[string]any{
			"result": "success",
			"token":  "TEMP123",
			"count":  int64(3),
			"nested": map[string]any{"a": "b"},
			"list":   []any{"x", "y"},
		},
	}
	for i, in := range cases {
		data := msgpackEncode(in)
		got, rest, err := msgpackDecode(data)
		if err != nil {
			t.Fatalf("case %d decode: %v", i, err)
		}
		if len(rest) != 0 {
			t.Errorf("case %d: %d trailing bytes", i, len(rest))
		}
		if !reflect.DeepEqual(got, in) {
			t.Errorf("case %d round trip: got %#v, want %#v", i, got, in)
		}
	}
}

func TestMsgpack_LongString(t *testing.T) {
	// Exercises str8/str16 length-prefixed paths.
	for _, n := range []int{40, 300, 70000} {
		s := string(make([]byte, n))
		got, _, err := msgpackDecode(msgpackEncode(s))
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		if got.(string) != s {
			t.Errorf("n=%d: length mismatch got %d", n, len(got.(string)))
		}
	}
}

// msfrpcdMock decodes the MessagePack request array and replies per method.
type msfrpcdMock struct {
	token    string
	lastCall []any
}

func (m *msfrpcdMock) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		v, _, err := msgpackDecode(raw)
		if err != nil {
			w.WriteHeader(500)
			_, _ = w.Write(msgpackEncode(map[string]any{"error": true, "error_message": "bad request"}))
			return
		}
		parts, _ := v.([]any)
		m.lastCall = parts
		method := asString(parts[0])

		// All methods except auth.login must carry the token as arg 1.
		if method != "auth.login" {
			if len(parts) < 2 || asString(parts[1]) != m.token {
				_, _ = w.Write(msgpackEncode(map[string]any{"error": true, "error_message": "invalid token"}))
				return
			}
		}

		var resp map[string]any
		switch method {
		case "auth.login":
			resp = map[string]any{"result": "success", "token": m.token}
		case "console.create":
			resp = map[string]any{"id": "5", "prompt": "msf6 > ", "busy": false}
		case "console.write":
			resp = map[string]any{"wrote": int64(len(asString(parts[3])))}
		case "console.read":
			resp = map[string]any{"data": "[*] handler started", "prompt": "msf6 > ", "busy": false}
		case "session.list":
			resp = map[string]any{
				"1": map[string]any{
					"type":        "meterpreter",
					"info":        "NT AUTHORITY\\SYSTEM @ WS01",
					"via_exploit": "exploit/multi/handler",
				},
			}
		case "session.shell_write":
			resp = map[string]any{"write_count": int64(len(asString(parts[3])))}
		case "session.shell_read":
			resp = map[string]any{"seq": "0", "data": "command output\n"}
		default:
			resp = map[string]any{"error": true, "error_message": "unknown method " + method}
		}
		_, _ = w.Write(msgpackEncode(resp))
	}))
}

func TestLiveClient_RPCFlow(t *testing.T) {
	m := &msfrpcdMock{token: "TOK-XYZ"}
	srv := m.server()
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewLiveClient(ctx, LiveConfig{
		BaseURL:    srv.URL,
		Username:   "msf",
		Password:   "pw",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewLiveClient: %v", err)
	}

	t.Run("ConsoleCreateWriteRead", func(t *testing.T) {
		id, err := client.ConsoleCreate(ctx)
		if err != nil || id != "5" {
			t.Fatalf("ConsoleCreate id=%q err=%v", id, err)
		}
		if err := client.ConsoleWrite(ctx, id, "use exploit/multi/handler\n"); err != nil {
			t.Fatalf("ConsoleWrite: %v", err)
		}
		out, _, err := client.ConsoleRead(ctx, id)
		if err != nil || out != "[*] handler started" {
			t.Fatalf("ConsoleRead out=%q err=%v", out, err)
		}
	})

	t.Run("SessionList", func(t *testing.T) {
		sessions, err := client.SessionList(ctx)
		if err != nil {
			t.Fatalf("SessionList: %v", err)
		}
		if len(sessions) != 1 || sessions[0].ID != "1" || sessions[0].Type != "meterpreter" {
			t.Fatalf("unexpected sessions: %+v", sessions)
		}
	})

	t.Run("SessionShell", func(t *testing.T) {
		if err := client.SessionShellWrite(ctx, "1", "whoami\n"); err != nil {
			t.Fatalf("SessionShellWrite: %v", err)
		}
		// session id must be sent as an integer, not a string.
		if got, ok := m.lastCall[2].(int64); !ok || got != 1 {
			t.Fatalf("session id arg = %v (%T), want int 1", m.lastCall[2], m.lastCall[2])
		}
		out, err := client.SessionShellRead(ctx, "1")
		if err != nil || out != "command output\n" {
			t.Fatalf("SessionShellRead out=%q err=%v", out, err)
		}
	})
}

func TestLiveClient_AuthFailure(t *testing.T) {
	// Mock that always rejects the token forces auth to fail.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(msgpackEncode(map[string]any{"error": true, "error_message": "Invalid User ID or Password"}))
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := NewLiveClient(ctx, LiveConfig{BaseURL: srv.URL, Username: "x", Password: "y", HTTPClient: srv.Client()})
	if err == nil {
		t.Fatal("expected auth failure")
	}
}
