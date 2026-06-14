package sliver_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"

	sliverpkg "github.com/rinfra/rinfra/internal/c2/sliver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func TestParseOperatorConfig(t *testing.T) {
	ca := newCA(t)
	clientPEM, clientKey := ca.issue(t, "operator-alice", nil, nil, x509.ExtKeyUsageClientAuth)

	good := `{"operator":"alice","token":"abc","lhost":"127.0.0.1","lport":31337,` +
		`"ca_certificate":` + jsonStr(string(ca.certPEM)) + `,` +
		`"certificate":` + jsonStr(string(clientPEM)) + `,` +
		`"private_key":` + jsonStr(string(clientKey)) + `}`

	cfg, err := sliverpkg.ParseOperatorConfig([]byte(good))
	if err != nil {
		t.Fatalf("ParseOperatorConfig: %v", err)
	}
	if cfg.Operator != "alice" {
		t.Errorf("operator = %q, want alice", cfg.Operator)
	}
	if cfg.Address() != "127.0.0.1:31337" {
		t.Errorf("Address() = %q, want 127.0.0.1:31337", cfg.Address())
	}
}

func TestParseOperatorConfig_MissingFields(t *testing.T) {
	cases := map[string]string{
		"missing lhost": `{"lport":31337,"ca_certificate":"x","certificate":"x","private_key":"x"}`,
		"missing lport": `{"lhost":"h","ca_certificate":"x","certificate":"x","private_key":"x"}`,
		"missing ca":    `{"lhost":"h","lport":1,"certificate":"x","private_key":"x"}`,
		"missing cert":  `{"lhost":"h","lport":1,"ca_certificate":"x","private_key":"x"}`,
		"missing key":   `{"lhost":"h","lport":1,"ca_certificate":"x","certificate":"x"}`,
		"malformed":     `{not json`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := sliverpkg.ParseOperatorConfig([]byte(body)); err == nil {
				t.Errorf("expected error for %s, got nil", name)
			}
		})
	}
}

func TestTLSConfig(t *testing.T) {
	ca := newCA(t)
	clientPEM, clientKey := ca.issue(t, "operator-bob", nil, nil, x509.ExtKeyUsageClientAuth)
	cfg := sliverpkg.OperatorConfig{
		LHost:         "teamserver.internal",
		LPort:         31337,
		CACertificate: string(ca.certPEM),
		Certificate:   string(clientPEM),
		PrivateKey:    string(clientKey),
	}

	tc, err := cfg.TLSConfig()
	if err != nil {
		t.Fatalf("TLSConfig: %v", err)
	}
	if len(tc.Certificates) != 1 {
		t.Fatalf("expected 1 client certificate, got %d", len(tc.Certificates))
	}
	if tc.RootCAs == nil {
		t.Error("expected non-nil RootCAs (CA pinning)")
	}
	if tc.ServerName != "teamserver.internal" {
		t.Errorf("ServerName = %q, want teamserver.internal", tc.ServerName)
	}
	if tc.MinVersion != tls.VersionTLS13 {
		t.Error("expected TLS 1.3 minimum")
	}
}

func TestTLSConfig_BadCA(t *testing.T) {
	ca := newCA(t)
	clientPEM, clientKey := ca.issue(t, "op", nil, nil, x509.ExtKeyUsageClientAuth)
	cfg := sliverpkg.OperatorConfig{
		LHost: "h", LPort: 1,
		CACertificate: "-----BEGIN CERTIFICATE-----\nnot-base64\n-----END CERTIFICATE-----",
		Certificate:   string(clientPEM),
		PrivateKey:    string(clientKey),
	}
	if _, err := cfg.TLSConfig(); err == nil {
		t.Error("expected error for invalid CA certificate")
	}
}

// TestDialOperator_MutualTLSHandshake stands up an in-process gRPC server that
// requires and verifies client certificates, then drives a real mTLS handshake
// through DialOperator by issuing a health-check RPC. This proves the operator
// config is consumed correctly end-to-end (client cert presented, server pinned
// to the CA) without any live Sliver teamserver.
func TestDialOperator_MutualTLSHandshake(t *testing.T) {
	ca := newCA(t)

	// Server cert valid for 127.0.0.1 (both IP SAN and ServerName match).
	serverPEM, serverKey := ca.issue(t, "127.0.0.1",
		[]string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")}, x509.ExtKeyUsageServerAuth)
	serverCert, err := tls.X509KeyPair(serverPEM, serverKey)
	if err != nil {
		t.Fatalf("server keypair: %v", err)
	}
	clientPEM, clientKey := ca.issue(t, "operator-carol", nil, nil, x509.ExtKeyUsageClientAuth)

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca.certPEM) {
		t.Fatal("failed to add CA to pool")
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS13,
	})))
	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(srv, hs)
	go srv.Serve(lis)
	defer srv.Stop()

	port := lis.Addr().(*net.TCPAddr).Port
	cfg := sliverpkg.OperatorConfig{
		Operator:      "carol",
		LHost:         "127.0.0.1",
		LPort:         port,
		CACertificate: string(ca.certPEM),
		Certificate:   string(clientPEM),
		PrivateKey:    string(clientKey),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := sliverpkg.DialOperator(ctx, cfg)
	if err != nil {
		t.Fatalf("DialOperator: %v", err)
	}
	defer conn.Close()

	resp, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("mTLS health check RPC failed: %v", err)
	}
	if resp.Status != healthpb.HealthCheckResponse_SERVING {
		t.Errorf("health status = %v, want SERVING", resp.Status)
	}
}

// --- test PKI helpers ---

type testCA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
}

func newCA(t *testing.T) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "rinfra-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("ca cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse ca: %v", err)
	}
	return &testCA{
		cert:    cert,
		key:     key,
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}

// issue returns a PEM cert/key pair signed by the CA.
func (c *testCA) issue(t *testing.T, cn string, dns []string, ips []net.IP, eku x509.ExtKeyUsage) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("leaf key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{eku},
		DNSNames:     dns,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, &key.PublicKey, c.key)
	if err != nil {
		t.Fatalf("leaf cert: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

// jsonStr quotes a string as a JSON string literal (handles the embedded
// newlines in PEM blocks).
func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
