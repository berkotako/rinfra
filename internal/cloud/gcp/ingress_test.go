package gcp

import (
	"testing"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
)

// TestBuildGCPFirewallAllows verifies the GCP VPC firewall allow rule
// translation. GCP groups rules by protocol (one FirewallAllow per protocol)
// and uses string port lists, unlike AWS (FromPort/ToPort) and DO (PortRange).
func TestBuildGCPFirewallAllows(t *testing.T) {
	tests := []struct {
		name    string
		rules   []domain.Rule
		wantLen int
		check   func(t *testing.T, got []gcpFirewallAllow)
	}{
		{
			name:    "empty rules",
			rules:   nil,
			wantLen: 0,
		},
		{
			name: "deny rules are skipped (GCP allow-only in this context)",
			rules: []domain.Rule{
				{Protocol: "tcp", Port: 22, Allow: false},
				{Protocol: "tcp", Port: 443, Allow: true},
			},
			wantLen: 1, // only the tcp allow group
		},
		{
			name: "multiple ports same protocol grouped together",
			rules: []domain.Rule{
				{Protocol: "tcp", Port: 80, Allow: true},
				{Protocol: "tcp", Port: 443, Allow: true},
			},
			wantLen: 1, // grouped into one FirewallAllow
			check: func(t *testing.T, got []gcpFirewallAllow) {
				if got[0].Protocol != "tcp" {
					t.Errorf("Protocol = %q, want tcp", got[0].Protocol)
				}
				if len(got[0].Ports) != 2 {
					t.Errorf("Ports len = %d, want 2 (80 and 443 grouped)", len(got[0].Ports))
				}
			},
		},
		{
			// GCP groups by protocol — distinct from AWS (one rule per port) and DO
			// (one rule per port). This test asserts the GCP-specific shape.
			name: "different protocols create separate FirewallAllow entries",
			rules: []domain.Rule{
				{Protocol: "tcp", Port: 443, Allow: true},
				{Protocol: "udp", Port: 53, Allow: true},
			},
			wantLen: 2, // separate protocol groups
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildGCPFirewallAllows(tc.rules)
			if len(got) != tc.wantLen {
				t.Errorf("len = %d, want %d", len(got), tc.wantLen)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

// TestValidateGCPCreds verifies credential validation.
func TestValidateGCPCreds(t *testing.T) {
	tests := []struct {
		name    string
		creds   cloud.Credentials
		wantErr bool
	}{
		{
			name: "valid",
			creds: cloud.Credentials{Raw: map[string]string{
				CredKeyCredentials: `{"type":"service_account"}`,
				CredKeyProject:     "my-project",
			}},
			wantErr: false,
		},
		{
			name: "missing credentials JSON",
			creds: cloud.Credentials{Raw: map[string]string{
				CredKeyProject: "my-project",
			}},
			wantErr: true,
		},
		{
			name: "missing project",
			creds: cloud.Credentials{Raw: map[string]string{
				CredKeyCredentials: `{"type":"service_account"}`,
			}},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateGCPCreds(tc.creds)
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestGCPFirewallShapeDiffersFromAWSAndDO documents the per-provider shape
// difference for ingress rules. This is a declarative test verifying the
// deliberate design divergence noted in CLAUDE.md.
func TestGCPFirewallShapeDiffersFromAWSAndDO(t *testing.T) {
	rules := []domain.Rule{
		{Protocol: "tcp", Port: 443, Allow: true},
		{Protocol: "tcp", Port: 80, Allow: true},
	}

	// GCP: ports are strings, grouped by protocol.
	gcpAllows := buildGCPFirewallAllows(rules)
	if len(gcpAllows) != 1 {
		t.Fatalf("GCP: expected 1 FirewallAllow (grouped by protocol), got %d", len(gcpAllows))
	}
	if len(gcpAllows[0].Ports) != 2 {
		t.Errorf("GCP: expected 2 ports in one group, got %d", len(gcpAllows[0].Ports))
	}
	// GCP ports are strings (not int pairs), e.g. "443" not FromPort=443,ToPort=443.
	for _, p := range gcpAllows[0].Ports {
		if p != "80" && p != "443" {
			t.Errorf("GCP port %q is not a string port value", p)
		}
	}
}
