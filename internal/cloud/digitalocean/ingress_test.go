package digitalocean

import (
	"testing"

	"github.com/rinfra/rinfra/internal/domain"
)

func TestBuildDOInboundRules(t *testing.T) {
	tests := []struct {
		name      string
		rules     []domain.Rule
		wantLen   int
		wantRules []doInboundRule
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
			wantRules: []doInboundRule{
				{Protocol: "tcp", PortRange: "443", SourceAddresses: []string{"0.0.0.0/0"}},
			},
		},
		{
			name: "deny rules are dropped (DO allow-only)",
			rules: []domain.Rule{
				{Protocol: "tcp", Port: 80, SourceCIDR: "10.0.0.0/8", Allow: false},
				{Protocol: "tcp", Port: 443, SourceCIDR: "0.0.0.0/0", Allow: true},
			},
			wantLen: 1,
			wantRules: []doInboundRule{
				{Protocol: "tcp", PortRange: "443", SourceAddresses: []string{"0.0.0.0/0"}},
			},
		},
		{
			name: "empty source CIDR defaults to 0.0.0.0/0",
			rules: []domain.Rule{
				{Protocol: "tcp", Port: 22, SourceCIDR: "", Allow: true},
			},
			wantLen: 1,
			wantRules: []doInboundRule{
				{Protocol: "tcp", PortRange: "22", SourceAddresses: []string{"0.0.0.0/0"}},
			},
		},
		{
			name: "multiple allow rules",
			rules: []domain.Rule{
				{Protocol: "tcp", Port: 80, SourceCIDR: "0.0.0.0/0", Allow: true},
				{Protocol: "tcp", Port: 443, SourceCIDR: "0.0.0.0/0", Allow: true},
				{Protocol: "udp", Port: 53, SourceCIDR: "192.168.1.0/24", Allow: true},
			},
			wantLen: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildDOInboundRules(tc.rules)
			if len(got) != tc.wantLen {
				t.Errorf("buildDOInboundRules() len = %d, want %d", len(got), tc.wantLen)
			}
			for i, want := range tc.wantRules {
				if i >= len(got) {
					t.Fatalf("missing rule at index %d", i)
				}
				if got[i].Protocol != want.Protocol {
					t.Errorf("rule[%d].Protocol = %q, want %q", i, got[i].Protocol, want.Protocol)
				}
				if got[i].PortRange != want.PortRange {
					t.Errorf("rule[%d].PortRange = %q, want %q", i, got[i].PortRange, want.PortRange)
				}
				if len(got[i].SourceAddresses) != len(want.SourceAddresses) {
					t.Errorf("rule[%d].SourceAddresses len = %d, want %d", i, len(got[i].SourceAddresses), len(want.SourceAddresses))
					continue
				}
				for j, addr := range want.SourceAddresses {
					if got[i].SourceAddresses[j] != addr {
						t.Errorf("rule[%d].SourceAddresses[%d] = %q, want %q", i, j, got[i].SourceAddresses[j], addr)
					}
				}
			}
		})
	}
}

// TestDOTagConstruction verifies that tag values are formatted correctly.
func TestDOTagConstruction(t *testing.T) {
	engID := "abc-123"
	nodeID := "xyz-456"

	engTag := TagPrefix + engID
	nodeTag := TagPrefix + "node:" + nodeID

	if want := "rinfra:abc-123"; engTag != want {
		t.Errorf("engagement tag = %q, want %q", engTag, want)
	}
	if want := "rinfra:node:xyz-456"; nodeTag != want {
		t.Errorf("node tag = %q, want %q", nodeTag, want)
	}
}

// TestCredKeyToken verifies that the token key matches what Pulumi expects.
func TestCredKeyToken(t *testing.T) {
	if CredKeyToken != "DIGITALOCEAN_TOKEN" {
		t.Errorf("CredKeyToken = %q, want DIGITALOCEAN_TOKEN", CredKeyToken)
	}
}
