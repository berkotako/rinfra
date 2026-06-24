package aws

import (
	"fmt"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration/terraform"
)

// BuildConfig implements terraform.Builder. It mirrors the Pulumi BuildProgram:
// per node a security group (egress-all), an EC2 instance, and an Elastic IP,
// all tagged rinfra/rinfra:node. The aws provider reads AWS_ACCESS_KEY_ID,
// AWS_SECRET_ACCESS_KEY and AWS_REGION from the environment.
func (p *provider) BuildConfig(engagementID string, creds cloud.Credentials, nodes []domain.Node) (*terraform.Config, error) {
	region := creds.Raw[CredKeyRegion]
	if region == "" {
		region = "us-east-1"
	}

	sgs := map[string]any{}
	instances := map[string]any{}
	eips := map[string]any{}
	outputs := map[string]any{}

	for _, n := range nodes {
		nodeName := fmt.Sprintf("rinfra-%s-%s", engagementID[:8], n.ID[:8])
		instanceType := n.Spec.Size
		if instanceType == "" {
			instanceType = "t3.micro"
		}
		key := terraform.SafeName(n.ID)
		tags := map[string]any{
			TagKey:           engagementID,
			TagKey + ":node": n.ID,
			"Name":           nodeName,
		}

		sgs[key] = map[string]any{
			"name":        nodeName + "-sg",
			"description": fmt.Sprintf("RInfra security group for node %s", n.ID),
			"egress": []any{map[string]any{
				"from_port":   0,
				"to_port":     0,
				"protocol":    "-1",
				"cidr_blocks": []string{"0.0.0.0/0"},
			}},
			"tags": tags,
		}
		instances[key] = map[string]any{
			// Region-correct AMI: resolved at apply time from the public SSM
			// parameter (see the aws_ssm_parameter data source below) rather than a
			// hardcoded us-east-1 image.
			"ami":                    "${data.aws_ssm_parameter.rinfra_ami.value}",
			"instance_type":          instanceType,
			"vpc_security_group_ids": []string{fmt.Sprintf("${aws_security_group.%s.id}", key)},
			"tags":                   tags,
		}
		eips[key] = map[string]any{
			"instance": fmt.Sprintf("${aws_instance.%s.id}", key),
			"domain":   "vpc",
			"tags":     tags,
		}
		outputs[terraform.ProviderRefOutput(n.ID)] = map[string]any{
			"value": fmt.Sprintf("${aws_instance.%s.id}", key),
		}
		outputs[terraform.PublicIPOutput(n.ID)] = map[string]any{
			"value": fmt.Sprintf("${aws_eip.%s.public_ip}", key),
		}
	}

	return &terraform.Config{
		Terraform: map[string]any{
			"required_providers": map[string]any{
				"aws": map[string]any{"source": "hashicorp/aws"},
			},
		},
		Provider: map[string]any{"aws": map[string]any{"region": region}},
		Data: map[string]any{
			// Latest Amazon Linux AMI for the provider's region.
			"aws_ssm_parameter": map[string]any{
				"rinfra_ami": map[string]any{"name": amazonLinuxSSMParameter},
			},
		},
		Resource: map[string]any{
			"aws_security_group": sgs,
			"aws_instance":       instances,
			"aws_eip":            eips,
		},
		Output: outputs,
	}, nil
}

var _ terraform.Builder = (*provider)(nil)
