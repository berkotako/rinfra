package sliver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// OperatorConfig mirrors the Sliver multiplayer operator configuration file
// (the JSON ".cfg" emitted by `sliver-server operator --save`). It carries the
// mTLS material an operator client presents to the teamserver's gRPC
// multiplayer listener.
//
// RInfra never generates this material — the upstream sliver-server does,
// during Deploy. We only consume it to drive the operator API. The struct holds
// secret key material; never log it and redact it in audit events.
type OperatorConfig struct {
	Operator      string `json:"operator"`
	Token         string `json:"token"`
	LHost         string `json:"lhost"`
	LPort         int    `json:"lport"`
	CACertificate string `json:"ca_certificate"`
	Certificate   string `json:"certificate"`
	PrivateKey    string `json:"private_key"`
}

// ParseOperatorConfig decodes a Sliver operator config from JSON bytes and
// validates that the mTLS material required to dial is present.
func ParseOperatorConfig(data []byte) (OperatorConfig, error) {
	var cfg OperatorConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return OperatorConfig{}, fmt.Errorf("sliver: parse operator config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return OperatorConfig{}, err
	}
	return cfg, nil
}

// LoadOperatorConfig reads and parses a Sliver operator config file from disk.
func LoadOperatorConfig(path string) (OperatorConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return OperatorConfig{}, fmt.Errorf("sliver: read operator config: %w", err)
	}
	return ParseOperatorConfig(data)
}

func (c OperatorConfig) validate() error {
	switch {
	case c.LHost == "":
		return fmt.Errorf("sliver: operator config missing lhost")
	case c.LPort == 0:
		return fmt.Errorf("sliver: operator config missing lport")
	case c.CACertificate == "":
		return fmt.Errorf("sliver: operator config missing ca_certificate")
	case c.Certificate == "":
		return fmt.Errorf("sliver: operator config missing certificate")
	case c.PrivateKey == "":
		return fmt.Errorf("sliver: operator config missing private_key")
	}
	return nil
}

// Address returns the host:port of the multiplayer endpoint.
func (c OperatorConfig) Address() string {
	return fmt.Sprintf("%s:%d", c.LHost, c.LPort)
}

// TLSConfig builds the mTLS client configuration from the operator config: the
// client presents its operator certificate/key and pins the teamserver against
// the embedded CA. ServerName is the configured lhost.
func (c OperatorConfig) TLSConfig() (*tls.Config, error) {
	cert, err := tls.X509KeyPair([]byte(c.Certificate), []byte(c.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("sliver: load operator keypair: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(c.CACertificate)) {
		return nil, fmt.Errorf("sliver: invalid ca_certificate in operator config")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   c.LHost,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// DialOperator establishes an mTLS gRPC connection to the Sliver teamserver's
// multiplayer listener using the operator config. The returned ClientConn is
// the transport that the live SliverClient issues RPCs over. The connection is
// created lazily (grpc.NewClient); the first RPC drives the handshake. Callers
// own the conn and must Close it.
//
// Extra dial options are appended after the transport credentials so callers
// (and tests) can tune timeouts, interceptors, or the resolver.
func DialOperator(_ context.Context, cfg OperatorConfig, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	tlsCfg, err := cfg.TLSConfig()
	if err != nil {
		return nil, err
	}
	dialOpts := append([]grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
	}, opts...)
	conn, err := grpc.NewClient(cfg.Address(), dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("sliver: dial operator %s: %w", cfg.Address(), err)
	}
	return conn, nil
}
