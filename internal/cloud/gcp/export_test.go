// export_test.go exposes internal functions for external package tests.
package gcp

import "github.com/rinfra/rinfra/internal/domain"

// GCPFirewallAllow is the exported type for cross-package tests.
type GCPFirewallAllow = gcpFirewallAllow

// ExportBuildGCPFirewallAllows exposes buildGCPFirewallAllows for cross-package tests.
func ExportBuildGCPFirewallAllows(rules []domain.Rule) []GCPFirewallAllow {
	return buildGCPFirewallAllows(rules)
}
