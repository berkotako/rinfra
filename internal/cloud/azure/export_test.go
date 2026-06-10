// export_test.go exposes internal functions for external package tests.
package azure

import "github.com/rinfra/rinfra/internal/domain"

// AzureNSGRule is the exported type for cross-package tests.
type AzureNSGRule = azureNSGRule

// ExportBuildAzureNSGRules exposes buildAzureNSGRules for cross-package tests.
func ExportBuildAzureNSGRules(rules []domain.Rule) []AzureNSGRule {
	return buildAzureNSGRules(rules)
}
