package domain_test

import (
	"errors"
	"testing"

	"github.com/rinfra/rinfra/internal/domain"
)

func eng(allowed, excluded []string) domain.Engagement {
	return domain.Engagement{
		Scope: domain.Scope{AllowedTargets: allowed, Exclusions: excluded},
	}
}

func TestTargetInScope(t *testing.T) {
	tests := []struct {
		name     string
		allowed  []string
		excluded []string
		target   string
		want     bool
	}{
		// IP inside / outside CIDR.
		{"ip inside cidr", []string{"10.0.0.0/8"}, nil, "10.5.6.7", true},
		{"ip outside cidr", []string{"10.0.0.0/8"}, nil, "192.168.1.1", false},
		{"ipv6 inside cidr", []string{"2001:db8::/32"}, nil, "2001:db8::1", true},
		{"ipv6 outside cidr", []string{"2001:db8::/32"}, nil, "2001:dead::1", false},

		// IP excluded by a narrower CIDR (exclusion precedence).
		{"ip excluded by narrower cidr", []string{"10.0.0.0/8"}, []string{"10.0.1.0/24"}, "10.0.1.5", false},
		{"ip allowed beside exclusion", []string{"10.0.0.0/8"}, []string{"10.0.1.0/24"}, "10.0.2.5", true},
		{"exact ip exclusion", []string{"10.0.0.0/8"}, []string{"10.0.0.9"}, "10.0.0.9", false},

		// CIDR-within-CIDR containment.
		{"cidr inside cidr", []string{"10.0.0.0/8"}, nil, "10.1.0.0/16", true},
		{"cidr not inside cidr", []string{"10.0.0.0/16"}, nil, "10.0.0.0/8", false},
		{"cidr excluded", []string{"10.0.0.0/8"}, []string{"10.0.1.0/24"}, "10.0.1.0/24", false},

		// Bare IP entry.
		{"bare ip match", []string{"203.0.113.5"}, nil, "203.0.113.5", true},
		{"bare ip mismatch", []string{"203.0.113.5"}, nil, "203.0.113.6", false},

		// Domain exact match.
		{"domain exact", []string{"example.com"}, nil, "example.com", true},
		{"domain case-insensitive", []string{"Example.COM"}, nil, "example.com", true},
		{"domain trailing dot", []string{"example.com"}, nil, "example.com.", true},
		{"domain mismatch", []string{"example.com"}, nil, "example.org", false},

		// Subdomain handling (plain entry covers subdomains).
		{"subdomain of allowed", []string{"example.com"}, nil, "api.example.com", true},
		{"deep subdomain", []string{"example.com"}, nil, "a.b.example.com", true},
		{"not a real suffix", []string{"example.com"}, nil, "notexample.com", false},
		{"suffix trick", []string{"example.com"}, nil, "example.com.evil.com", false},

		// Wildcard entry: subdomains only, not the apex.
		{"wildcard subdomain", []string{"*.example.com"}, nil, "api.example.com", true},
		{"wildcard not apex", []string{"*.example.com"}, nil, "example.com", false},

		// Exclusion precedence on domains.
		{"subdomain excluded", []string{"example.com"}, []string{"secret.example.com"}, "secret.example.com", false},
		{"sub-subdomain excluded", []string{"example.com"}, []string{"secret.example.com"}, "db.secret.example.com", false},
		{"sibling not excluded", []string{"example.com"}, []string{"secret.example.com"}, "www.example.com", true},

		// Cross-type entries don't match.
		{"domain entry vs ip target", []string{"example.com"}, nil, "10.0.0.1", false},
		{"cidr entry vs domain target", []string{"10.0.0.0/8"}, nil, "example.com", false},

		// Invalid / empty input.
		{"empty target", []string{"10.0.0.0/8"}, nil, "", false},
		{"garbage target", []string{"10.0.0.0/8"}, nil, "not a host", false},
		{"empty scope", nil, nil, "10.0.0.1", false},
		{"whitespace target trimmed", []string{"example.com"}, nil, "  example.com  ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := eng(tt.allowed, tt.excluded)
			if got := e.TargetInScope(tt.target); got != tt.want {
				t.Errorf("TargetInScope(%q) = %v, want %v", tt.target, got, tt.want)
			}
		})
	}
}

func TestEnforceTargetInScope(t *testing.T) {
	e := eng([]string{"10.0.0.0/8"}, []string{"10.0.1.0/24"})

	if err := e.EnforceTargetInScope("10.0.2.2"); err != nil {
		t.Errorf("in-scope target should not error, got %v", err)
	}
	err := e.EnforceTargetInScope("10.0.1.2")
	if !errors.Is(err, domain.ErrTargetNotInScope) {
		t.Errorf("excluded target: want ErrTargetNotInScope, got %v", err)
	}
	if err := e.EnforceTargetInScope("8.8.8.8"); !errors.Is(err, domain.ErrTargetNotInScope) {
		t.Errorf("out-of-scope target: want ErrTargetNotInScope, got %v", err)
	}
}
