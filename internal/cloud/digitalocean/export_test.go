// export_test.go exposes internal functions for external package tests.
// This file is only compiled when running tests.
package digitalocean

import "github.com/rinfra/rinfra/internal/domain"

// DOInboundRule is the exported type for cross-package tests.
type DOInboundRule = doInboundRule

// ExportBuildDOInboundRules exposes buildDOInboundRules for cross-package tests.
func ExportBuildDOInboundRules(rules []domain.Rule) []DOInboundRule {
	return buildDOInboundRules(rules)
}
