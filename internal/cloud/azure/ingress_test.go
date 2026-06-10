package azure

import (
	"testing"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
)

// TestBuildAzureNSGRules verifies Azure NSG rule translation.
// Azure explicitly supports Allow AND Deny rules (unlike DO, GCP which are
// allow-only). Deny rules become "Deny" access rules, not silent drops.
func TestBuildAzureNSGRules(t *testing.T) {
	tests := []struct {
		name    string
		rules   []domain.Rule
		wantLen int
		check   func(t *testing.T, got []azureNSGRule)
	}{
		{
			name:    "empty rules",
			rules:   nil,
			wantLen: 0,
		},
		{
			name: "allow rule becomes Allow",
			rules: []domain.Rule{
				{Protocol: "tcp", Port: 443, SourceCIDR: "0.0.0.0/0", Allow: true},
			},
			wantLen: 1,
			check: func(t *testing.T, got []azureNSGRule) {
				if got[0].Access != "Allow" {
					t.Errorf("Access = %q, want Allow", got[0].Access)
				}
				if got[0].DestinationPortRange != "443" {
					t.Errorf("DestinationPortRange = %q, want 443", got[0].DestinationPortRange)
				}
				if got[0].Direction != "Inbound" {
					t.Errorf("Direction = %q, want Inbound", got[0].Direction)
				}
			},
		},
		{
			// Azure-specific: deny rules create explicit Deny NSG entries.
			// This is fundamentally different from DO/GCP (allow-only, deny by omission)
			// and AWS (allow-only SGs, but can use NACLs separately).
			name: "deny rule becomes explicit Deny (Azure-unique)",
			rules: []domain.Rule{
				{Protocol: "tcp", Port: 22, SourceCIDR: "10.0.0.0/8", Allow: false},
			},
			wantLen: 1,
			check: func(t *testing.T, got []azureNSGRule) {
				if got[0].Access != "Deny" {
					t.Errorf("Access = %q, want Deny", got[0].Access)
				}
				if got[0].SourceAddressPrefix != "10.0.0.0/8" {
					t.Errorf("SourceAddressPrefix = %q, want 10.0.0.0/8", got[0].SourceAddressPrefix)
				}
			},
		},
		{
			name: "priority assigned sequentially",
			rules: []domain.Rule{
				{Protocol: "tcp", Port: 80, Allow: true},
				{Protocol: "tcp", Port: 443, Allow: true},
				{Protocol: "tcp", Port: 8080, Allow: false},
			},
			wantLen: 3,
			check: func(t *testing.T, got []azureNSGRule) {
				for i, rule := range got {
					expectedPriority := 100 + i*10
					if rule.Priority != expectedPriority {
						t.Errorf("rule[%d].Priority = %d, want %d", i, rule.Priority, expectedPriority)
					}
				}
			},
		},
		{
			name: "empty CIDR defaults to wildcard",
			rules: []domain.Rule{
				{Protocol: "tcp", Port: 443, SourceCIDR: "", Allow: true},
			},
			wantLen: 1,
			check: func(t *testing.T, got []azureNSGRule) {
				if got[0].SourceAddressPrefix != "*" {
					t.Errorf("SourceAddressPrefix = %q, want *", got[0].SourceAddressPrefix)
				}
			},
		},
		{
			name: "tcp mapped to Tcp",
			rules: []domain.Rule{
				{Protocol: "tcp", Port: 22, Allow: true},
			},
			wantLen: 1,
			check: func(t *testing.T, got []azureNSGRule) {
				if got[0].Protocol != "Tcp" {
					t.Errorf("Protocol = %q, want Tcp (Azure title-case)", got[0].Protocol)
				}
			},
		},
		{
			name: "udp mapped to Udp",
			rules: []domain.Rule{
				{Protocol: "udp", Port: 53, Allow: true},
			},
			wantLen: 1,
			check: func(t *testing.T, got []azureNSGRule) {
				if got[0].Protocol != "Udp" {
					t.Errorf("Protocol = %q, want Udp (Azure title-case)", got[0].Protocol)
				}
			},
		},
		{
			name: "unknown protocol maps to wildcard",
			rules: []domain.Rule{
				{Protocol: "icmp", Port: 0, Allow: true},
			},
			wantLen: 1,
			check: func(t *testing.T, got []azureNSGRule) {
				if got[0].Protocol != "*" {
					t.Errorf("Protocol = %q, want *", got[0].Protocol)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildAzureNSGRules(tc.rules)
			if len(got) != tc.wantLen {
				t.Errorf("len = %d, want %d", len(got), tc.wantLen)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

// TestToAzureProtocol verifies the Azure protocol name translation.
func TestToAzureProtocol(t *testing.T) {
	cases := []struct{ in, want string }{
		{"tcp", "Tcp"},
		{"udp", "Udp"},
		{"icmp", "*"},
		{"", "*"},
		{"all", "*"},
	}
	for _, c := range cases {
		got := toAzureProtocol(c.in)
		if got != c.want {
			t.Errorf("toAzureProtocol(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestValidateAzureCreds verifies credential validation.
func TestValidateAzureCreds(t *testing.T) {
	validCreds := cloud.Credentials{
		Raw: map[string]string{
			CredKeySubscriptionID: "sub-id",
			CredKeyTenantID:       "tenant-id",
			CredKeyClientID:       "client-id",
			CredKeyClientSecret:   "client-secret",
		},
	}

	if err := validateAzureCreds(validCreds); err != nil {
		t.Errorf("unexpected error for valid creds: %v", err)
	}

	// Remove one key at a time and expect error.
	for _, k := range []string{CredKeySubscriptionID, CredKeyTenantID, CredKeyClientID, CredKeyClientSecret} {
		t.Run("missing "+k, func(t *testing.T) {
			creds := cloud.Credentials{Raw: make(map[string]string)}
			for key, val := range validCreds.Raw {
				if key != k {
					creds.Raw[key] = val
				}
			}
			if err := validateAzureCreds(creds); err == nil {
				t.Errorf("expected error when %q is missing, got nil", k)
			}
		})
	}
}

// TestAzureNSGShapeDiffersFromOthers documents that Azure explicitly supports
// Deny rules, unlike DO and GCP (allow-only) and AWS (allow-only SGs).
func TestAzureNSGShapeDiffersFromOthers(t *testing.T) {
	rules := []domain.Rule{
		{Protocol: "tcp", Port: 443, Allow: true},
		{Protocol: "tcp", Port: 22, Allow: false}, // explicit deny
	}
	got := buildAzureNSGRules(rules)

	if len(got) != 2 {
		t.Fatalf("want 2 rules, got %d", len(got))
	}
	// First rule: Allow
	if got[0].Access != "Allow" {
		t.Errorf("rule[0].Access = %q, want Allow", got[0].Access)
	}
	// Second rule: explicit Deny — Azure-unique behavior.
	if got[1].Access != "Deny" {
		t.Errorf("rule[1].Access = %q, want Deny (Azure supports explicit deny, unlike DO/GCP/AWS SGs)", got[1].Access)
	}
}
