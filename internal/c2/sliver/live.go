package sliver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rinfra/rinfra/internal/c2"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protowire"
)

// This file is the live operator client: it issues the subset of Sliver's
// gRPC RPCs that RInfra drives over the mTLS *grpc.ClientConn produced by
// DialOperator (transport.go).
//
// # Thin in-house stubs (no protoc, no bishopfox dependency)
//
// Rather than vendor Sliver's large generated protobuf tree, we define minimal
// Go structs for only the messages we use and (de)serialize them with
// google.golang.org/protobuf/encoding/protowire. The field numbers below are
// pinned to the upstream Sliver protobuf definitions for the deployed release
// (v1.5.42): commonpb/common.proto, sliverpb/sliver.proto, clientpb/client.proto.
// The bytes we produce ARE standard protobuf wire format, so the real
// teamserver decodes them with its own generated codec — wireCodec reports its
// name as "proto" so the standard application/grpc+proto content-type is used.
//
// NOTE (untestable seam): field-number/shape correctness against a *live*
// teamserver cannot be verified in CI (we never talk to live infra here). It is
// verified by construction against the pinned .proto and exercised end-to-end
// against an in-process gRPC server in live_test.go. If the deployed Sliver
// version changes its protobufs, update the field numbers here in lockstep.

const (
	methodGetSessions  = "/rpcpb.SliverRPC/GetSessions"
	methodExecute      = "/rpcpb.SliverRPC/Execute"
	methodStartMTLS    = "/rpcpb.SliverRPC/StartMTLSListener"
	methodStartDNS     = "/rpcpb.SliverRPC/StartDNSListener"
	methodStartHTTPS   = "/rpcpb.SliverRPC/StartHTTPSListener"
	defaultExecTimeout = 60 * time.Second
)

var errBadWire = errors.New("sliver: malformed protobuf wire data")

// grpcConn is the slice of *grpc.ClientConn the live client needs; narrowing it
// keeps the client unit-testable.
type grpcConn interface {
	Invoke(ctx context.Context, method string, args, reply any, opts ...grpc.CallOption) error
}

// liveClient implements SliverClient over a real gRPC connection.
type liveClient struct {
	conn    grpcConn
	timeout time.Duration
}

// NewLiveClient returns a SliverClient that issues real RPCs over conn (built by
// DialOperator). timeout bounds implant taskings; <=0 uses the default.
func NewLiveClient(conn *grpc.ClientConn, timeout time.Duration) SliverClient {
	return newLiveClient(conn, timeout)
}

func newLiveClient(conn grpcConn, timeout time.Duration) *liveClient {
	if timeout <= 0 {
		timeout = defaultExecTimeout
	}
	return &liveClient{conn: conn, timeout: timeout}
}

func (c *liveClient) invoke(ctx context.Context, method string, req, resp wireMessage) error {
	return c.conn.Invoke(ctx, method, req, resp, grpc.ForceCodec(wireCodec{}))
}

func (c *liveClient) StartMTLSListener(ctx context.Context, host string, port uint32) error {
	return c.invoke(ctx, methodStartMTLS, &mtlsListenerReq{Host: host, Port: port}, &jobResp{})
}

func (c *liveClient) StartHTTPSListener(ctx context.Context, domain string, port uint32) error {
	return c.invoke(ctx, methodStartHTTPS, &httpListenerReq{Domain: domain, Port: port, Secure: true}, &jobResp{})
}

func (c *liveClient) StartDNSListener(ctx context.Context, domains []string) error {
	return c.invoke(ctx, methodStartDNS, &dnsListenerReq{Domains: domains, Canaries: true}, &jobResp{})
}

func (c *liveClient) Sessions(ctx context.Context) ([]SliverSession, error) {
	resp := &sessionsResp{}
	if err := c.invoke(ctx, methodGetSessions, &emptyMsg{}, resp); err != nil {
		return nil, err
	}
	out := make([]SliverSession, 0, len(resp.Sessions))
	for _, s := range resp.Sessions {
		out = append(out, SliverSession{
			ID:       s.ID,
			Hostname: s.Hostname,
			Username: s.Username,
			OS:       s.OS,
			Arch:     s.Arch,
		})
	}
	return out, nil
}

// Execute runs command on the given session via the Execute RPC. The command
// is parsed into a program path + args (shell-style, honoring double quotes).
//
// NOTE: this is a literal "run this command line" mapping. Sliver console verbs
// that are not real programs (e.g. sysinfo, ps) have dedicated RPCs; mapping the
// portable technique catalog to those per-RPC is a follow-up. The transport and
// codec here are the reusable foundation for that work.
func (c *liveClient) Execute(ctx context.Context, sessionID, command string) (string, error) {
	path, args := splitCommand(command)
	if path == "" {
		return "", fmt.Errorf("sliver: empty command")
	}
	req := &executeReq{
		Path:   path,
		Args:   args,
		Output: true,
		Request: &requestEnvelope{
			SessionID: sessionID,
			Timeout:   int64(c.timeout / time.Second),
		},
	}
	resp := &executeResp{}
	if err := c.invoke(ctx, methodExecute, req, resp); err != nil {
		return "", err
	}
	out := string(resp.Stdout)
	if len(resp.Stderr) > 0 {
		if out != "" {
			out += "\n"
		}
		out += string(resp.Stderr)
	}
	if resp.Response != nil && resp.Response.Err != "" {
		return out, fmt.Errorf("sliver: implant returned error: %s", resp.Response.Err)
	}
	return out, nil
}

// LiveOperator dials the teamserver with cfg and returns a fully live Operator
// plus the underlying connection (the caller must Close it). This is the
// service-layer entry point to drive Sliver automatically; provider.Control
// returns a noop-backed operator until a config is supplied here.
func LiveOperator(ctx context.Context, cfg OperatorConfig, timeout time.Duration) (c2.Operator, *grpc.ClientConn, error) {
	conn, err := DialOperator(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	ts := teamserverFromConfig(cfg)
	return NewOperatorWithClient(ts, NewLiveClient(conn, timeout)), conn, nil
}

func teamserverFromConfig(cfg OperatorConfig) c2.Teamserver {
	return c2.Teamserver{
		Host:           cfg.LHost,
		Port:           cfg.LPort,
		Status:         "running",
		ConnectionInfo: fmt.Sprintf("sliver multiplayer @ %s (mTLS, operator=%s)", cfg.Address(), cfg.Operator),
	}
}

// splitCommand tokenizes a command line into program + args, honoring simple
// double-quoted segments.
func splitCommand(s string) (string, []string) {
	var toks []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ' ' && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	if len(toks) == 0 {
		return "", nil
	}
	return toks[0], toks[1:]
}

// ---- wire codec ----

const codecName = "proto" // standard application/grpc+proto content subtype

// wireMessage is implemented by the minimal Sliver message structs below.
type wireMessage interface {
	marshalWire() []byte
	unmarshalWire([]byte) error
}

// wireCodec is a grpc encoding.Codec that (de)serializes our wireMessage
// structs as protobuf. It reports the standard "proto" name so the real Sliver
// server decodes our bytes with its own generated codec.
type wireCodec struct{}

func (wireCodec) Name() string { return codecName }

func (wireCodec) Marshal(v any) ([]byte, error) {
	m, ok := v.(wireMessage)
	if !ok {
		return nil, fmt.Errorf("sliver: wireCodec cannot marshal %T", v)
	}
	return m.marshalWire(), nil
}

func (wireCodec) Unmarshal(data []byte, v any) error {
	m, ok := v.(wireMessage)
	if !ok {
		return fmt.Errorf("sliver: wireCodec cannot unmarshal into %T", v)
	}
	return m.unmarshalWire(data)
}

// ---- protowire helpers ----

func putString(b []byte, num protowire.Number, s string) []byte {
	if s == "" {
		return b
	}
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendString(b, s)
}

func putBytes(b []byte, num protowire.Number, v []byte) []byte {
	if len(v) == 0 {
		return b
	}
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendBytes(b, v)
}

func putBool(b []byte, num protowire.Number, v bool) []byte {
	if !v {
		return b
	}
	b = protowire.AppendTag(b, num, protowire.VarintType)
	return protowire.AppendVarint(b, 1)
}

func putUint32(b []byte, num protowire.Number, v uint32) []byte {
	if v == 0 {
		return b
	}
	b = protowire.AppendTag(b, num, protowire.VarintType)
	return protowire.AppendVarint(b, uint64(v))
}

func putInt64(b []byte, num protowire.Number, v int64) []byte {
	if v == 0 {
		return b
	}
	b = protowire.AppendTag(b, num, protowire.VarintType)
	return protowire.AppendVarint(b, uint64(v))
}

func putMessage(b []byte, num protowire.Number, sub []byte) []byte {
	if len(sub) == 0 {
		return b
	}
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendBytes(b, sub)
}

// wireField is one decoded protobuf field.
type wireField struct {
	num protowire.Number
	typ protowire.Type
	u   uint64 // varint/fixed value
	b   []byte // length-delimited payload (aliases input)
}

// consumeFields decodes all top-level fields of a protobuf message.
func consumeFields(buf []byte) ([]wireField, error) {
	var out []wireField
	for len(buf) > 0 {
		num, typ, n := protowire.ConsumeTag(buf)
		if n < 0 {
			return nil, errBadWire
		}
		buf = buf[n:]
		f := wireField{num: num, typ: typ}
		switch typ {
		case protowire.VarintType:
			v, m := protowire.ConsumeVarint(buf)
			if m < 0 {
				return nil, errBadWire
			}
			f.u = v
			buf = buf[m:]
		case protowire.BytesType:
			v, m := protowire.ConsumeBytes(buf)
			if m < 0 {
				return nil, errBadWire
			}
			f.b = v
			buf = buf[m:]
		default:
			m := protowire.ConsumeFieldValue(num, typ, buf)
			if m < 0 {
				return nil, errBadWire
			}
			buf = buf[m:]
		}
		out = append(out, f)
	}
	return out, nil
}

// ---- messages: commonpb ----

type emptyMsg struct{}

func (*emptyMsg) marshalWire() []byte        { return nil }
func (*emptyMsg) unmarshalWire([]byte) error { return nil }

// requestEnvelope is commonpb.Request.
type requestEnvelope struct {
	Async     bool
	Timeout   int64
	BeaconID  string
	SessionID string
}

func (r *requestEnvelope) marshalWire() []byte {
	var b []byte
	b = putBool(b, 1, r.Async)
	b = putInt64(b, 2, r.Timeout)
	b = putString(b, 8, r.BeaconID)
	b = putString(b, 9, r.SessionID)
	return b
}

func (r *requestEnvelope) unmarshalWire(data []byte) error {
	fields, err := consumeFields(data)
	if err != nil {
		return err
	}
	for _, f := range fields {
		switch f.num {
		case 1:
			r.Async = f.u != 0
		case 2:
			r.Timeout = int64(f.u)
		case 8:
			r.BeaconID = string(f.b)
		case 9:
			r.SessionID = string(f.b)
		}
	}
	return nil
}

// responseEnvelope is commonpb.Response (only Err is consumed).
type responseEnvelope struct {
	Err string
}

func (r *responseEnvelope) marshalWire() []byte {
	return putString(nil, 1, r.Err)
}

func (r *responseEnvelope) unmarshalWire(data []byte) error {
	fields, err := consumeFields(data)
	if err != nil {
		return err
	}
	for _, f := range fields {
		if f.num == 1 {
			r.Err = string(f.b)
		}
	}
	return nil
}

// ---- messages: sliverpb ----

// executeReq is sliverpb.ExecuteReq.
type executeReq struct {
	Path    string
	Args    []string
	Output  bool
	Request *requestEnvelope
}

func (e *executeReq) marshalWire() []byte {
	var b []byte
	b = putString(b, 1, e.Path)
	for _, a := range e.Args {
		b = protowire.AppendTag(b, 2, protowire.BytesType)
		b = protowire.AppendString(b, a)
	}
	b = putBool(b, 3, e.Output)
	if e.Request != nil {
		b = putMessage(b, 9, e.Request.marshalWire())
	}
	return b
}

func (e *executeReq) unmarshalWire(data []byte) error {
	fields, err := consumeFields(data)
	if err != nil {
		return err
	}
	for _, f := range fields {
		switch f.num {
		case 1:
			e.Path = string(f.b)
		case 2:
			e.Args = append(e.Args, string(f.b))
		case 3:
			e.Output = f.u != 0
		case 9:
			e.Request = &requestEnvelope{}
			if err := e.Request.unmarshalWire(f.b); err != nil {
				return err
			}
		}
	}
	return nil
}

// executeResp is sliverpb.Execute.
type executeResp struct {
	Status   uint32
	Stdout   []byte
	Stderr   []byte
	Pid      uint32
	Response *responseEnvelope
}

func (e *executeResp) marshalWire() []byte {
	var b []byte
	b = putUint32(b, 1, e.Status)
	b = putBytes(b, 2, e.Stdout)
	b = putBytes(b, 3, e.Stderr)
	b = putUint32(b, 4, e.Pid)
	if e.Response != nil {
		b = putMessage(b, 9, e.Response.marshalWire())
	}
	return b
}

func (e *executeResp) unmarshalWire(data []byte) error {
	fields, err := consumeFields(data)
	if err != nil {
		return err
	}
	for _, f := range fields {
		switch f.num {
		case 1:
			e.Status = uint32(f.u)
		case 2:
			e.Stdout = append([]byte(nil), f.b...)
		case 3:
			e.Stderr = append([]byte(nil), f.b...)
		case 4:
			e.Pid = uint32(f.u)
		case 9:
			e.Response = &responseEnvelope{}
			if err := e.Response.unmarshalWire(f.b); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---- messages: clientpb ----

// sessionMsg is the subset of clientpb.Session RInfra needs.
type sessionMsg struct {
	ID       string
	Hostname string
	Username string
	OS       string
	Arch     string
}

func (s *sessionMsg) marshalWire() []byte {
	var b []byte
	b = putString(b, 1, s.ID)
	b = putString(b, 3, s.Hostname)
	b = putString(b, 5, s.Username)
	b = putString(b, 8, s.OS)
	b = putString(b, 9, s.Arch)
	return b
}

func (s *sessionMsg) unmarshalWire(data []byte) error {
	fields, err := consumeFields(data)
	if err != nil {
		return err
	}
	for _, f := range fields {
		switch f.num {
		case 1:
			s.ID = string(f.b)
		case 3:
			s.Hostname = string(f.b)
		case 5:
			s.Username = string(f.b)
		case 8:
			s.OS = string(f.b)
		case 9:
			s.Arch = string(f.b)
		}
	}
	return nil
}

// sessionsResp is clientpb.Sessions.
type sessionsResp struct {
	Sessions []sessionMsg
}

func (s *sessionsResp) marshalWire() []byte {
	var b []byte
	for i := range s.Sessions {
		b = putMessage(b, 1, s.Sessions[i].marshalWire())
	}
	return b
}

func (s *sessionsResp) unmarshalWire(data []byte) error {
	fields, err := consumeFields(data)
	if err != nil {
		return err
	}
	for _, f := range fields {
		if f.num == 1 {
			var sess sessionMsg
			if err := sess.unmarshalWire(f.b); err != nil {
				return err
			}
			s.Sessions = append(s.Sessions, sess)
		}
	}
	return nil
}

// mtlsListenerReq is clientpb.MTLSListenerReq.
type mtlsListenerReq struct {
	Host string
	Port uint32
}

func (m *mtlsListenerReq) marshalWire() []byte {
	var b []byte
	b = putString(b, 1, m.Host)
	b = putUint32(b, 2, m.Port)
	return b
}

func (m *mtlsListenerReq) unmarshalWire(data []byte) error {
	fields, err := consumeFields(data)
	if err != nil {
		return err
	}
	for _, f := range fields {
		switch f.num {
		case 1:
			m.Host = string(f.b)
		case 2:
			m.Port = uint32(f.u)
		}
	}
	return nil
}

// dnsListenerReq is clientpb.DNSListenerReq.
type dnsListenerReq struct {
	Domains  []string
	Canaries bool
}

func (d *dnsListenerReq) marshalWire() []byte {
	var b []byte
	for _, dom := range d.Domains {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendString(b, dom)
	}
	b = putBool(b, 2, d.Canaries)
	return b
}

func (d *dnsListenerReq) unmarshalWire(data []byte) error {
	fields, err := consumeFields(data)
	if err != nil {
		return err
	}
	for _, f := range fields {
		switch f.num {
		case 1:
			d.Domains = append(d.Domains, string(f.b))
		case 2:
			d.Canaries = f.u != 0
		}
	}
	return nil
}

// httpListenerReq is clientpb.HTTPListenerReq.
type httpListenerReq struct {
	Domain string
	Port   uint32
	Secure bool
}

func (h *httpListenerReq) marshalWire() []byte {
	var b []byte
	b = putString(b, 1, h.Domain)
	b = putUint32(b, 3, h.Port)
	b = putBool(b, 4, h.Secure)
	return b
}

func (h *httpListenerReq) unmarshalWire(data []byte) error {
	fields, err := consumeFields(data)
	if err != nil {
		return err
	}
	for _, f := range fields {
		switch f.num {
		case 1:
			h.Domain = string(f.b)
		case 3:
			h.Port = uint32(f.u)
		case 4:
			h.Secure = f.u != 0
		}
	}
	return nil
}

// jobResp is the shared listener response shape (clientpb.{MTLS,DNS,HTTP}Listener),
// each carrying just a JobID at field 1.
type jobResp struct {
	JobID uint32
}

func (j *jobResp) marshalWire() []byte {
	return putUint32(nil, 1, j.JobID)
}

func (j *jobResp) unmarshalWire(data []byte) error {
	fields, err := consumeFields(data)
	if err != nil {
		return err
	}
	for _, f := range fields {
		if f.num == 1 {
			j.JobID = uint32(f.u)
		}
	}
	return nil
}
