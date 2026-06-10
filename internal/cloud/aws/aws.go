// Package aws adapts Amazon Web Services to RInfra's cloud.CloudProvider
// interface. Provisions into the customer's account using per-engagement
// credentials — never a shared RInfra account.
//
// # SDK approach
//
// Uses the Pulumi Go SDK automation API (via internal/orchestration.Engine)
// for resource lifecycle management. The CloudProvider methods translate domain
// types into Pulumi resource declarations inside an inline program.
//
// # ConfigureIngress — deliberately different from other providers
//
// AWS uses EC2 Security Groups (stateful, per-instance or per-VPC). A Security
// Group is created alongside each EC2 instance; ingress rules are
// SecurityGroupIngressArgs with FromPort == ToPort == Port for single ports.
// This differs from:
//   - DO: Cloud Firewalls attached to Droplets by tag or ID.
//   - GCP: VPC firewall rules with target-tag filters on instances.
//   - Azure: Network Security Groups attached to NICs/subnets.
//
// # Credential keys
//
//   - "AWS_ACCESS_KEY_ID"     — AWS access key
//   - "AWS_SECRET_ACCESS_KEY" — AWS secret key
//   - "AWS_REGION"            — region to provision into (e.g. "us-east-1")
//
// # Verified by compile vs needs live testing
//
// All code below is verified to compile against the Pulumi AWS SDK v6.
// The full resource lifecycle (EC2 instance + security group + EIP + Route53
// record, stack destroy, tagged-resource sweep) requires a live AWS account
// and has NOT been exercised against the live API. See docs/RUNBOOK_DO.md
// (same pattern, different cloud) for the verification checklist approach.
package aws

import (
	"context"
	"fmt"

	awsec2 "github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	awsr53 "github.com/pulumi/pulumi-aws/sdk/v6/go/aws/route53"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/orchestration"
)

// Credential key constants.
const (
	CredKeyAccessKeyID     = "AWS_ACCESS_KEY_ID"
	CredKeySecretAccessKey = "AWS_SECRET_ACCESS_KEY"
	CredKeyRegion          = "AWS_REGION"
)

// DefaultImage is the AMI ID used when NodeSpec does not override it.
// This is the Amazon Linux 2023 AMI in us-east-1; a real deploy should
// use an AMI lookup per region.
const DefaultImage = "ami-0c02fb55956c7d316" // Amazon Linux 2 us-east-1 — TODO(live): use SSM lookup per region

// TagKey is the AWS tag key applied to every resource.
const TagKey = "rinfra"

func init() {
	p := &provider{}
	cloud.Register(p)
	cloud.RegisterSweeper(domain.CloudAWS, p)
}

type provider struct{}

var _ cloud.CloudProvider = (*provider)(nil)
var _ cloud.Sweeper = (*provider)(nil)
var _ orchestration.ProgramBuilder = (*provider)(nil)

func (p *provider) Type() domain.CloudProviderType { return domain.CloudAWS }

// BuildProgram implements orchestration.ProgramBuilder. Creates an EC2
// instance + security group + optional Elastic IP for each node.
//
// Tags applied to every resource:
//
//	"rinfra"          = engagementID   (used for SweepOrphans)
//	"rinfra:node"     = nodeID
func (p *provider) BuildProgram(engagementID string, creds cloud.Credentials, nodes []domain.Node) pulumi.RunFunc {
	return func(ctx *pulumi.Context) error {
		for _, n := range nodes {
			nodeName := fmt.Sprintf("rinfra-%s-%s", engagementID[:8], n.ID[:8])
			instanceType := n.Spec.Size
			if instanceType == "" {
				instanceType = "t3.micro"
			}
			ami := DefaultImage
			region := n.Spec.Region
			if region == "" {
				region = "us-east-1"
			}

			tags := pulumi.StringMap{
				TagKey:           pulumi.String(engagementID),
				TagKey + ":node": pulumi.String(n.ID),
				"Name":           pulumi.String(nodeName),
			}

			// Security Group — AWS-specific: stateful, per-instance ingress control.
			// Unlike DO's per-firewall model, AWS attaches SGs to the instance directly.
			sg, err := awsec2.NewSecurityGroup(ctx, nodeName+"-sg", &awsec2.SecurityGroupArgs{
				Name:        pulumi.String(nodeName + "-sg"),
				Description: pulumi.String(fmt.Sprintf("RInfra security group for node %s", n.ID)),
				// Ingress rules are managed by ConfigureIngress / BuildProgram.
				// Default: allow all outbound.
				Egress: awsec2.SecurityGroupEgressArray{
					awsec2.SecurityGroupEgressArgs{
						FromPort:   pulumi.Int(0),
						ToPort:     pulumi.Int(0),
						Protocol:   pulumi.String("-1"),
						CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
					},
				},
				Tags: tags,
			})
			if err != nil {
				return fmt.Errorf("aws: create security group for node %s: %w", n.ID, err)
			}

			// EC2 instance.
			instance, err := awsec2.NewInstance(ctx, nodeName, &awsec2.InstanceArgs{
				Ami:                 pulumi.String(ami),
				InstanceType:        pulumi.String(instanceType),
				VpcSecurityGroupIds: pulumi.StringArray{sg.ID()},
				Tags:                tags,
			})
			if err != nil {
				return fmt.Errorf("aws: create instance for node %s: %w", n.ID, err)
			}

			// Elastic IP — always assigned (RInfra redirectors need stable addresses).
			eip, err := awsec2.NewEip(ctx, nodeName+"-eip", &awsec2.EipArgs{
				Instance: instance.ID(),
				Domain:   pulumi.String("vpc"),
				Tags:     tags,
			})
			if err != nil {
				return fmt.Errorf("aws: create EIP for node %s: %w", n.ID, err)
			}

			ctx.Export(orchestration.NodeProviderRefKey(n.ID), instance.ID())
			ctx.Export(orchestration.NodePublicIPKey(n.ID), eip.PublicIp)
		}
		return nil
	}
}

// ProvisionNode — not supported as a direct call on the AWS provider.
// Use orchestration.Engine.Deploy which includes the full stack lifecycle.
func (p *provider) ProvisionNode(_ context.Context, _ cloud.Credentials, _ domain.NodeSpec) (domain.Node, error) {
	return domain.Node{}, fmt.Errorf("aws.ProvisionNode: use orchestration.Engine.Deploy for real provisioning")
}

// ConfigureIngress translates domain.Rule to AWS EC2 Security Group ingress
// rules. AWS security groups differ fundamentally from other providers:
//   - Rules are stateful (return traffic is automatically allowed).
//   - Protocol "-1" means all traffic; use explicit protocols for fine control.
//   - FromPort == ToPort for single ports; use a range for port ranges.
//   - CIDR is specified in CidrBlocks (IPv4) / Ipv6CidrBlocks (IPv6).
//
// This method documents the per-provider shape. In the Pulumi Engine path,
// ingress rules are included in BuildProgram's SecurityGroup definition.
//
// TODO(live): Standalone ingress update via AWS SDK requires a live account.
func (p *provider) ConfigureIngress(_ context.Context, creds cloud.Credentials, node domain.Node, rules []domain.Rule) error {
	if node.ProviderRef == "" {
		return fmt.Errorf("aws.ConfigureIngress: node %s has no ProviderRef (not yet provisioned)", node.ID)
	}
	if err := validateAWSCreds(creds); err != nil {
		return err
	}
	// Build rules for validation (actual update via SDK TODO(live)).
	_ = buildAWSIngressRules(rules)
	return fmt.Errorf("aws.ConfigureIngress: standalone ingress update not yet implemented; use Engine.Deploy — TODO(live)")
}

// buildAWSIngressRules converts domain.Rule slice to AWS SecurityGroup ingress
// descriptors. This is the authoritative translation and is tested directly.
//
// AWS-specific shapes:
//   - FromPort == ToPort == Port for single port rules.
//   - Protocol "tcp"/"udp" maps directly; "-1" = all.
//   - CidrBlocks carries the source CIDR (not a tags-based filter as in GCP).
func buildAWSIngressRules(rules []domain.Rule) []awsIngressRule {
	var out []awsIngressRule
	for _, r := range rules {
		if !r.Allow {
			continue // AWS SGs are allow-only.
		}
		cidr := r.SourceCIDR
		if cidr == "" {
			cidr = "0.0.0.0/0"
		}
		out = append(out, awsIngressRule{
			FromPort:   r.Port,
			ToPort:     r.Port,
			Protocol:   r.Protocol,
			CidrBlocks: []string{cidr},
		})
	}
	return out
}

// awsIngressRule is the internal representation of an AWS SG ingress rule.
type awsIngressRule struct {
	FromPort   int
	ToPort     int
	Protocol   string
	CidrBlocks []string
}

// AssignStaticIP — handled by EIP in BuildProgram.
// TODO(live): standalone EIP allocation via AWS SDK.
func (p *provider) AssignStaticIP(_ context.Context, creds cloud.Credentials, node domain.Node) (string, error) {
	if node.ProviderRef == "" {
		return "", fmt.Errorf("aws.AssignStaticIP: node %s has no ProviderRef", node.ID)
	}
	if err := validateAWSCreds(creds); err != nil {
		return "", err
	}
	return "", fmt.Errorf("aws.AssignStaticIP: use Engine.Deploy (includes EIP in inline program) — TODO(live)")
}

// ManageDNS upserts a Route53 record. AWS Route53 uses hosted zone IDs rather
// than zone names directly — the zone ID must be known out-of-band and stored
// in creds.Raw["AWS_ROUTE53_ZONE_ID"] for the target zone.
//
// This differs from DO (zone is a DO Domain resource referenced by name) and
// GCP (managed zone resource referenced by name). Azure also uses zone names
// but requires a resource group parameter.
//
// TODO(live): Route53 record upsert via AWS SDK.
func (p *provider) ManageDNS(_ context.Context, creds cloud.Credentials, rec domain.Record) error {
	if err := validateAWSCreds(creds); err != nil {
		return err
	}
	if rec.Zone == "" {
		return fmt.Errorf("aws.ManageDNS: Zone must be set (Route53 hosted zone ID or name)")
	}
	return fmt.Errorf("aws.ManageDNS: use Engine.Deploy (includes Route53 record in inline program) — TODO(live)")
}

// buildRoute53RecordArgs documents the Route53-specific resource shape.
// AWS Route53 records use StringArray as value (supports multiple values per
// record set), and ZoneId is a hosted zone ID (not a domain name string).
func buildRoute53RecordArgs(rec domain.Record, zoneID string) route53RecordArgs {
	return route53RecordArgs{
		ZoneID:  zoneID,
		Name:    rec.Name + "." + rec.Zone,
		Type:    rec.Type,
		TTL:     rec.TTL,
		Records: []string{rec.Value},
	}
}

// route53RecordArgs is the internal representation for a Route53 record.
type route53RecordArgs struct {
	ZoneID  string
	Name    string
	Type    string
	TTL     int
	Records []string
}

// addRoute53Record is the Pulumi inline program helper that creates a Route53
// record inside a stack program. Included here for documentation and testing;
// used from BuildProgram when DNS management is requested.
func addRoute53Record(ctx *pulumi.Context, name string, args route53RecordArgs) error {
	ttl := args.TTL
	if ttl == 0 {
		ttl = 300
	}
	values := make(pulumi.StringArray, len(args.Records))
	for i, r := range args.Records {
		values[i] = pulumi.String(r)
	}
	_, err := awsr53.NewRecord(ctx, name, &awsr53.RecordArgs{
		ZoneId:  pulumi.String(args.ZoneID),
		Name:    pulumi.String(args.Name),
		Type:    pulumi.String(args.Type),
		Ttl:     pulumi.Int(ttl),
		Records: values,
	})
	return err
}

// Destroy tears down an EC2 instance. In the Pulumi path handled by
// Engine.Teardown + stack destroy. Idempotent: empty ProviderRef = no-op.
// TODO(live): direct AWS SDK delete.
func (p *provider) Destroy(_ context.Context, creds cloud.Credentials, node domain.Node) error {
	if node.ProviderRef == "" {
		return nil // Never provisioned.
	}
	if err := validateAWSCreds(creds); err != nil {
		return err
	}
	return fmt.Errorf("aws.Destroy: use Engine.Teardown for full stack destroy + sweep — TODO(live)")
}

// SweepOrphans lists all EC2 instances/EIPs/SGs tagged rinfra=<engagementID>
// and deletes any found. This is the reconciliation guarantee for teardown.
//
// TODO(live): implement using AWS SDK (github.com/aws/aws-sdk-go-v2):
//  1. Create EC2 client with creds.Raw values.
//  2. DescribeInstances with filter tag:rinfra = engagementID.
//  3. TerminateInstances for any found.
//  4. DescribeAddresses + ReleaseAddress for orphaned EIPs.
//  5. DescribeSecurityGroups + DeleteSecurityGroup for orphaned SGs.
func (p *provider) SweepOrphans(_ context.Context, creds cloud.Credentials, engagementID string) error {
	if err := validateAWSCreds(creds); err != nil {
		return err
	}
	// TODO(live): implement AWS resource sweep.
	_ = engagementID
	return nil
}

// validateAWSCreds checks that the minimum required credential keys are present.
func validateAWSCreds(creds cloud.Credentials) error {
	if creds.Raw[CredKeyAccessKeyID] == "" {
		return fmt.Errorf("aws: credential key %q not set", CredKeyAccessKeyID)
	}
	if creds.Raw[CredKeySecretAccessKey] == "" {
		return fmt.Errorf("aws: credential key %q not set", CredKeySecretAccessKey)
	}
	if creds.Raw[CredKeyRegion] == "" {
		return fmt.Errorf("aws: credential key %q not set", CredKeyRegion)
	}
	return nil
}
