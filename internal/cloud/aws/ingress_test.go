package aws

import (
	"testing"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
)

func TestBuildAWSIngressRules(t *testing.T) {
	tests := []struct {
		name    string
		rules   []domain.Rule
		wantLen int
		check   func(t *testing.T, got []awsIngressRule)
	}{
		{
			name:    "empty rules",
			rules:   nil,
			wantLen: 0,
		},
		{
			name: "single allow rule",
			rules: []domain.Rule{
				{Protocol: "tcp", Port: 443, SourceCIDR: "0.0.0.0/0", Allow: true},
			},
			wantLen: 1,
			check: func(t *testing.T, got []awsIngressRule) {
				r := got[0]
				if r.FromPort != 443 {
					t.Errorf("FromPort = %d, want 443", r.FromPort)
				}
				if r.ToPort != 443 {
					t.Errorf("ToPort = %d, want 443", r.ToPort)
				}
				if r.Protocol != "tcp" {
					t.Errorf("Protocol = %q, want tcp", r.Protocol)
				}
				if len(r.CidrBlocks) != 1 || r.CidrBlocks[0] != "0.0.0.0/0" {
					t.Errorf("CidrBlocks = %v, want [0.0.0.0/0]", r.CidrBlocks)
				}
			},
		},
		{
			name: "deny rules are skipped",
			rules: []domain.Rule{
				{Protocol: "tcp", Port: 22, SourceCIDR: "10.0.0.0/8", Allow: false},
				{Protocol: "tcp", Port: 443, SourceCIDR: "0.0.0.0/0", Allow: true},
			},
			wantLen: 1,
		},
		{
			name: "empty CIDR defaults to 0.0.0.0/0",
			rules: []domain.Rule{
				{Protocol: "tcp", Port: 80, SourceCIDR: "", Allow: true},
			},
			wantLen: 1,
			check: func(t *testing.T, got []awsIngressRule) {
				if len(got[0].CidrBlocks) != 1 || got[0].CidrBlocks[0] != "0.0.0.0/0" {
					t.Errorf("CidrBlocks = %v, want [0.0.0.0/0]", got[0].CidrBlocks)
				}
			},
		},
		{
			// AWS SG shape: FromPort == ToPort for single port (not range-based like GCP).
			name: "AWS shape: FromPort equals ToPort for single port",
			rules: []domain.Rule{
				{Protocol: "tcp", Port: 8080, SourceCIDR: "192.168.0.0/16", Allow: true},
			},
			wantLen: 1,
			check: func(t *testing.T, got []awsIngressRule) {
				if got[0].FromPort != got[0].ToPort {
					t.Errorf("AWS SG shape: FromPort(%d) != ToPort(%d), want equal", got[0].FromPort, got[0].ToPort)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildAWSIngressRules(tc.rules)
			if len(got) != tc.wantLen {
				t.Errorf("len = %d, want %d", len(got), tc.wantLen)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

// TestValidateAWSCreds verifies credential validation.
func TestValidateAWSCreds(t *testing.T) {
	tests := []struct {
		name    string
		creds   cloud.Credentials
		wantErr bool
	}{
		{
			name: "valid creds",
			creds: cloud.Credentials{
				Raw: map[string]string{
					CredKeyAccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
					CredKeySecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					CredKeyRegion:          "us-east-1",
				},
			},
			wantErr: false,
		},
		{
			name: "missing access key",
			creds: cloud.Credentials{
				Raw: map[string]string{
					CredKeySecretAccessKey: "secret",
					CredKeyRegion:          "us-east-1",
				},
			},
			wantErr: true,
		},
		{
			name: "missing region",
			creds: cloud.Credentials{
				Raw: map[string]string{
					CredKeyAccessKeyID:     "key",
					CredKeySecretAccessKey: "secret",
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAWSCreds(tc.creds)
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestBuildRoute53RecordArgs verifies the Route53-specific record shape.
// AWS Route53 uses StringArray values (supports multiple values per record
// set) unlike DO (single value per record) and GCP (Rrdatas array but zone
// is a managed zone name not ID).
func TestBuildRoute53RecordArgs(t *testing.T) {
	rec := domain.Record{
		Zone:  "example.com",
		Name:  "www",
		Type:  "A",
		Value: "1.2.3.4",
		TTL:   300,
	}
	args := buildRoute53RecordArgs(rec, "Z1234EXAMPLE")

	if args.ZoneID != "Z1234EXAMPLE" {
		t.Errorf("ZoneID = %q, want Z1234EXAMPLE", args.ZoneID)
	}
	if args.Name != "www.example.com" {
		t.Errorf("Name = %q, want www.example.com", args.Name)
	}
	if args.Type != "A" {
		t.Errorf("Type = %q, want A", args.Type)
	}
	if len(args.Records) != 1 || args.Records[0] != "1.2.3.4" {
		t.Errorf("Records = %v, want [1.2.3.4]", args.Records)
	}
}
