// export_test.go exposes internal functions for external package tests.
package aws

import "github.com/rinfra/rinfra/internal/domain"

// AWSIngressRule is the exported type for cross-package tests.
type AWSIngressRule = awsIngressRule

// ExportBuildAWSIngressRules exposes buildAWSIngressRules for cross-package tests.
func ExportBuildAWSIngressRules(rules []domain.Rule) []AWSIngressRule {
	return buildAWSIngressRules(rules)
}
