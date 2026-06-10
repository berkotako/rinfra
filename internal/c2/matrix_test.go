// Package c2_test contains cross-framework tests that verify the support matrix
// defined in docs/SUPPORT_MATRIX.md is correctly implemented.
//
// These tests import all framework packages via blank imports to trigger their
// init() functions and populate the registry.
package c2_test

import (
	"testing"

	"github.com/rinfra/rinfra/internal/c2"

	// Trigger registration of all framework adapters.
	_ "github.com/rinfra/rinfra/internal/c2/bruteratel"
	_ "github.com/rinfra/rinfra/internal/c2/cobaltstrike"
	_ "github.com/rinfra/rinfra/internal/c2/custom"
	_ "github.com/rinfra/rinfra/internal/c2/havoc"
	_ "github.com/rinfra/rinfra/internal/c2/metasploit"
	_ "github.com/rinfra/rinfra/internal/c2/mythic"
	_ "github.com/rinfra/rinfra/internal/c2/poshc2"
	_ "github.com/rinfra/rinfra/internal/c2/sliver"
)

// supportMatrixRow describes the expected tier and Control() behaviour for a
// framework as per docs/SUPPORT_MATRIX.md.
type supportMatrixRow struct {
	name        string
	tier        c2.SupportTier
	hasOperator bool // true if Control() returns ok=true and a non-nil Operator
}

// TestSupportMatrix verifies that every framework's Tier() and Control()
// match the authoritative support matrix in docs/SUPPORT_MATRIX.md.
func TestSupportMatrix(t *testing.T) {
	matrix := []supportMatrixRow{
		// Orchestrated: automated emulation via Operator.
		{name: "sliver", tier: c2.TierOrchestrated, hasOperator: true},
		{name: "mythic", tier: c2.TierOrchestrated, hasOperator: true},
		{name: "metasploit", tier: c2.TierOrchestrated, hasOperator: true},
		{name: "custom", tier: c2.TierOrchestrated, hasOperator: true},
		// Scripted: partial Operator (subset of techniques).
		{name: "havoc", tier: c2.TierScripted, hasOperator: true},
		{name: "poshc2", tier: c2.TierScripted, hasOperator: true},
		// Fronted: deploy + redirector only; human operates.
		{name: "cobaltstrike", tier: c2.TierFronted, hasOperator: false},
		{name: "bruteratel", tier: c2.TierFronted, hasOperator: false},
	}

	for _, row := range matrix {
		row := row // capture loop variable
		t.Run(row.name, func(t *testing.T) {
			p, err := c2.Get(row.name)
			if err != nil {
				t.Fatalf("framework %q not registered: %v", row.name, err)
			}

			if p.Tier() != row.tier {
				t.Errorf("Tier() = %v, want %v", p.Tier(), row.tier)
			}

			op, ok := p.Control(c2.Teamserver{Host: "10.0.0.1", Port: 1234})
			if ok != row.hasOperator {
				t.Errorf("Control() ok = %v, want %v", ok, row.hasOperator)
			}
			if row.hasOperator && op == nil {
				t.Errorf("Control() returned ok=true but nil Operator for %q", row.name)
			}
			if !row.hasOperator && op != nil {
				t.Errorf("Control() returned non-nil Operator for Fronted-tier %q", row.name)
			}
		})
	}
}

// TestRegistryContainsAllFrameworks verifies that List() returns all 8 frameworks.
func TestRegistryContainsAllFrameworks(t *testing.T) {
	all := c2.List()

	expected := []string{
		"bruteratel",
		"cobaltstrike",
		"custom",
		"havoc",
		"metasploit",
		"mythic",
		"poshc2",
		"sliver",
	}

	found := make(map[string]bool, len(all))
	for _, p := range all {
		found[p.Name()] = true
	}

	for _, name := range expected {
		if !found[name] {
			t.Errorf("framework %q missing from registry", name)
		}
	}

	if len(all) < len(expected) {
		t.Errorf("registry has %d frameworks, expected at least %d", len(all), len(expected))
	}
}

// TestTierStringRepresentation verifies the human-readable Tier() strings.
func TestTierStringRepresentation(t *testing.T) {
	cases := []struct {
		tier c2.SupportTier
		want string
	}{
		{c2.TierOrchestrated, "orchestrated"},
		{c2.TierScripted, "scripted"},
		{c2.TierFronted, "fronted"},
	}
	for _, tc := range cases {
		if got := tc.tier.String(); got != tc.want {
			t.Errorf("SupportTier(%d).String() = %q, want %q", tc.tier, got, tc.want)
		}
	}
}

// TestFrontedTiersHaveNoOperator is a targeted invariant test: Fronted-tier
// frameworks must never return an Operator, since the emulation engine uses
// ok==false to decide whether to record techniques as Skipped.
func TestFrontedTiersHaveNoOperator(t *testing.T) {
	all := c2.List()
	for _, p := range all {
		if p.Tier() == c2.TierFronted {
			op, ok := p.Control(c2.Teamserver{})
			if ok || op != nil {
				t.Errorf("%q is Fronted-tier but Control() returned ok=%v op=%v — must be (nil, false)",
					p.Name(), ok, op)
			}
		}
	}
}

// TestOrchestrated_OperatorsAreNonNil verifies that Orchestrated-tier frameworks
// always return a non-nil Operator.
func TestOrchestrated_OperatorsAreNonNil(t *testing.T) {
	all := c2.List()
	for _, p := range all {
		if p.Tier() == c2.TierOrchestrated {
			op, ok := p.Control(c2.Teamserver{Host: "10.0.0.1", Port: 1234})
			if !ok || op == nil {
				t.Errorf("%q is Orchestrated-tier but Control() returned ok=%v op=%v",
					p.Name(), ok, op)
			}
		}
	}
}

// TestScripted_OperatorsAreNonNil verifies that Scripted-tier frameworks
// return a non-nil Operator (even though only a subset of techniques are supported).
func TestScripted_OperatorsAreNonNil(t *testing.T) {
	all := c2.List()
	for _, p := range all {
		if p.Tier() == c2.TierScripted {
			op, ok := p.Control(c2.Teamserver{Host: "10.0.0.1", Port: 1234})
			if !ok || op == nil {
				t.Errorf("%q is Scripted-tier but Control() returned ok=%v op=%v",
					p.Name(), ok, op)
			}
		}
	}
}
