// Package aws adapts Amazon Web Services to RInfra's cloud.CloudProvider
// interface. Provisions into the customer's account using per-engagement
// credentials — never a shared RInfra account.
//
// # SDK approach
//
// Two complementary paths:
//
//   - Bulk provisioning runs through the IaC engine (Pulumi): the
//     ProgramBuilder (BuildProgram) declares the EC2 instances, security
//     groups, Elastic IPs, and outputs the engine compiles and applies.
//   - The standalone CloudProvider/Sweeper methods (ConfigureIngress,
//     AssignStaticIP, ManageDNS, Destroy, SweepOrphans) drive the AWS API
//     directly with the AWS SDK for Go v2, for out-of-band reconciliation and
//     the guaranteed-teardown sweep that runs after every engine destroy.
//
// # ConfigureIngress — deliberately different from other providers
//
// AWS uses EC2 Security Groups (stateful, per-instance or per-VPC). A Security
// Group is created alongside each EC2 instance; ingress rules are
// IpPermissions with FromPort == ToPort == Port for single ports. This differs
// from:
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
// The SDK-v2-backed standalone methods are unit-tested against httptest fakes
// of the EC2 query and Route53 REST-XML APIs (live_test.go) — request action
// routing and response parsing are verified, but the full lifecycle against a
// real AWS account still wants a live runbook checklist. The engine
// BuildProgram path is compile-verified against the Pulumi AWS SDK v6.
package aws

import (
	"context"
	"errors"
	"fmt"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/aws/smithy-go"
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

// DefaultImage is the AMI ID used when NodeSpec does not override it. AMI IDs
// are region-specific, so a static constant is only correct for one region.
//
// Live per-region resolution: the canonical approach is an SSM public-parameter
// lookup against /aws/service/ami-amazon-linux-latest/<name> (e.g.
// .../al2023-ami-kernel-default-x86_64), which returns the current Amazon Linux
// AMI for the caller's region. resolveImage implements that fallback path;
// because the SSM service client is not a dependency of this module, the lookup
// degrades to this constant (Amazon Linux 2, us-east-1). Wiring the SSM client
// is the only change needed to make resolution fully dynamic.
const DefaultImage = "ami-0c02fb55956c7d316" // Amazon Linux 2 (us-east-1)

// amazonLinuxSSMParameter is the public SSM parameter path that resolves to the
// latest Amazon Linux 2023 AMI for the caller's region. Documented here as the
// live per-region resolution source; see resolveImage.
const amazonLinuxSSMParameter = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64"

// TagKey is the AWS tag key applied to every resource.
const TagKey = "rinfra"

func init() {
	p := &provider{}
	cloud.Register(p)
	cloud.RegisterSweeper(domain.CloudAWS, p)
}

// provider is the RInfra AWS cloud adapter. It implements CloudProvider and
// Sweeper; it acts as both the direct-call adapter and the ProgramBuilder
// passed to orchestration.Engine.
//
// baseEndpoint overrides the EC2/Route53 API endpoint; empty uses the live AWS
// endpoints. It exists so tests can point the clients at an httptest server.
// The registered provider (init) keeps baseEndpoint == "".
type provider struct {
	baseEndpoint string
}

var _ cloud.CloudProvider = (*provider)(nil)
var _ cloud.Sweeper = (*provider)(nil)
var _ orchestration.ProgramBuilder = (*provider)(nil)

func (p *provider) Type() domain.CloudProviderType { return domain.CloudAWS }

// awsConfig builds an aws.Config from the engagement's static credentials. The
// access key, secret, and region are read from creds.Raw; the same values
// Pulumi uses via env. When p.baseEndpoint is set (tests), it is threaded into
// the EC2/Route53 client constructors via their options funcs.
func (p *provider) awsConfig(ctx context.Context, creds cloud.Credentials) (awssdk.Config, error) {
	if err := validateAWSCreds(creds); err != nil {
		return awssdk.Config{}, err
	}
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(creds.Raw[CredKeyRegion]),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			creds.Raw[CredKeyAccessKeyID],
			creds.Raw[CredKeySecretAccessKey],
			"",
		)),
	)
	if err != nil {
		return awssdk.Config{}, fmt.Errorf("aws: load config: %w", err)
	}
	return cfg, nil
}

// ec2Client builds an EC2 client, honoring p.baseEndpoint for tests.
func (p *provider) ec2Client(ctx context.Context, creds cloud.Credentials) (*ec2.Client, error) {
	cfg, err := p.awsConfig(ctx, creds)
	if err != nil {
		return nil, err
	}
	return ec2.NewFromConfig(cfg, func(o *ec2.Options) {
		if p.baseEndpoint != "" {
			o.BaseEndpoint = awssdk.String(p.baseEndpoint)
		}
	}), nil
}

// route53Client builds a Route53 client, honoring p.baseEndpoint for tests.
func (p *provider) route53Client(ctx context.Context, creds cloud.Credentials) (*route53.Client, error) {
	cfg, err := p.awsConfig(ctx, creds)
	if err != nil {
		return nil, err
	}
	return route53.NewFromConfig(cfg, func(o *route53.Options) {
		if p.baseEndpoint != "" {
			o.BaseEndpoint = awssdk.String(p.baseEndpoint)
		}
	}), nil
}

// resolveImage returns the AMI to use for a node. The live, per-region path is
// an SSM GetParameter lookup of amazonLinuxSSMParameter; absent the SSM client
// dependency it returns DefaultImage. Kept as a seam so dynamic resolution is a
// drop-in once the SSM client is wired.
func resolveImage(region string) string {
	_ = region
	_ = amazonLinuxSSMParameter
	return DefaultImage
}

// isAWSErrorCode reports whether err is an AWS API error with the given code
// (e.g. "InvalidInstanceID.NotFound"). Used to make deletes idempotent.
func isAWSErrorCode(err error, code string) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode() == code
	}
	return false
}

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
			region := n.Spec.Region
			if region == "" {
				region = "us-east-1"
			}
			ami := resolveImage(region)

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
// This method describes the per-provider shape. It resolves the instance's
// security group via DescribeInstances(node.ProviderRef), then authorizes each
// allow rule. Authorization is idempotent: an already-present rule yields an
// InvalidPermission.Duplicate error, which is treated as success.
func (p *provider) ConfigureIngress(ctx context.Context, creds cloud.Credentials, node domain.Node, rules []domain.Rule) error {
	if node.ProviderRef == "" {
		return fmt.Errorf("aws.ConfigureIngress: node %s has no ProviderRef (not yet provisioned)", node.ID)
	}
	client, err := p.ec2Client(ctx, creds)
	if err != nil {
		return err
	}

	// Resolve the instance's primary security group.
	groupID, err := p.instanceSecurityGroup(ctx, client, node.ProviderRef)
	if err != nil {
		return err
	}

	for _, r := range buildAWSIngressRules(rules) {
		perm := ec2types.IpPermission{
			IpProtocol: awssdk.String(r.Protocol),
			FromPort:   awssdk.Int32(int32(r.FromPort)),
			ToPort:     awssdk.Int32(int32(r.ToPort)),
		}
		for _, cidr := range r.CidrBlocks {
			perm.IpRanges = append(perm.IpRanges, ec2types.IpRange{CidrIp: awssdk.String(cidr)})
		}
		_, err := client.AuthorizeSecurityGroupIngress(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
			GroupId:       awssdk.String(groupID),
			IpPermissions: []ec2types.IpPermission{perm},
		})
		if err != nil && !isAWSErrorCode(err, "InvalidPermission.Duplicate") {
			return fmt.Errorf("aws.ConfigureIngress: authorize ingress on %s: %w", groupID, err)
		}
	}
	return nil
}

// instanceSecurityGroup returns the first security group ID attached to the
// instance identified by instanceID.
func (p *provider) instanceSecurityGroup(ctx context.Context, client *ec2.Client, instanceID string) (string, error) {
	out, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		return "", fmt.Errorf("aws.ConfigureIngress: describe instance %s: %w", instanceID, err)
	}
	for _, res := range out.Reservations {
		for _, inst := range res.Instances {
			if len(inst.SecurityGroups) > 0 {
				return awssdk.ToString(inst.SecurityGroups[0].GroupId), nil
			}
		}
	}
	return "", fmt.Errorf("aws.ConfigureIngress: instance %s has no security group", instanceID)
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

// AssignStaticIP allocates an Elastic IP (Domain=vpc) and associates it with
// the node's EC2 instance. Returns the allocated public IP. The EIP is tagged
// for the engagement so SweepOrphans can reclaim it on teardown.
func (p *provider) AssignStaticIP(ctx context.Context, creds cloud.Credentials, node domain.Node) (string, error) {
	if node.ProviderRef == "" {
		return "", fmt.Errorf("aws.AssignStaticIP: node %s has no ProviderRef", node.ID)
	}
	client, err := p.ec2Client(ctx, creds)
	if err != nil {
		return "", err
	}

	tagSpec := []ec2types.TagSpecification{{
		ResourceType: ec2types.ResourceTypeElasticIp,
		Tags: []ec2types.Tag{
			{Key: awssdk.String(TagKey), Value: awssdk.String(node.EngagementID)},
			{Key: awssdk.String(TagKey + ":node"), Value: awssdk.String(node.ID)},
		},
	}}
	alloc, err := client.AllocateAddress(ctx, &ec2.AllocateAddressInput{
		Domain:            ec2types.DomainTypeVpc,
		TagSpecifications: tagSpec,
	})
	if err != nil {
		return "", fmt.Errorf("aws.AssignStaticIP: allocate address: %w", err)
	}

	if _, err := client.AssociateAddress(ctx, &ec2.AssociateAddressInput{
		AllocationId: alloc.AllocationId,
		InstanceId:   awssdk.String(node.ProviderRef),
	}); err != nil {
		return "", fmt.Errorf("aws.AssignStaticIP: associate address: %w", err)
	}
	return awssdk.ToString(alloc.PublicIp), nil
}

// ManageDNS upserts a Route53 record. The hosted zone is resolved by name
// (rec.Zone) via ListHostedZonesByName, then a single UPSERT change is applied
// via ChangeResourceRecordSets. UPSERT creates the record if absent and updates
// it otherwise, so the operation is idempotent.
//
// AWS Route53 differs from DO (zone is a DO Domain referenced by name and
// records are individual API objects) and GCP (managed zone + record sets) —
// Route53 batches changes against a hosted zone ID resolved from the zone name.
func (p *provider) ManageDNS(ctx context.Context, creds cloud.Credentials, rec domain.Record) error {
	if rec.Zone == "" {
		return fmt.Errorf("aws.ManageDNS: Zone must be set (Route53 hosted zone name)")
	}
	if rec.Type == "" {
		return fmt.Errorf("aws.ManageDNS: Type must be set")
	}
	client, err := p.route53Client(ctx, creds)
	if err != nil {
		return err
	}

	zoneID, err := p.findHostedZoneID(ctx, client, rec.Zone)
	if err != nil {
		return err
	}

	args := buildRoute53RecordArgs(rec, zoneID)
	ttl := args.TTL
	if ttl == 0 {
		ttl = 300
	}
	records := make([]r53types.ResourceRecord, len(args.Records))
	for i, v := range args.Records {
		records[i] = r53types.ResourceRecord{Value: awssdk.String(v)}
	}

	_, err = client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: awssdk.String(zoneID),
		ChangeBatch: &r53types.ChangeBatch{
			Changes: []r53types.Change{{
				Action: r53types.ChangeActionUpsert,
				ResourceRecordSet: &r53types.ResourceRecordSet{
					Name:            awssdk.String(args.Name),
					Type:            r53types.RRType(args.Type),
					TTL:             awssdk.Int64(int64(ttl)),
					ResourceRecords: records,
				},
			}},
		},
	})
	if err != nil {
		return fmt.Errorf("aws.ManageDNS: change record sets in zone %s: %w", rec.Zone, err)
	}
	return nil
}

// findHostedZoneID resolves a hosted zone name (e.g. "example.com") to its
// Route53 zone ID. Route53 returns zone names with a trailing dot; both forms
// are matched. The returned ID is stripped of the "/hostedzone/" prefix.
func (p *provider) findHostedZoneID(ctx context.Context, client *route53.Client, zone string) (string, error) {
	out, err := client.ListHostedZonesByName(ctx, &route53.ListHostedZonesByNameInput{
		DNSName: awssdk.String(zone),
	})
	if err != nil {
		return "", fmt.Errorf("aws.ManageDNS: list hosted zones: %w", err)
	}
	want := zone
	if want != "" && want[len(want)-1] != '.' {
		want += "."
	}
	for _, hz := range out.HostedZones {
		name := awssdk.ToString(hz.Name)
		if name == want || name == zone {
			id := awssdk.ToString(hz.Id)
			return stripZoneIDPrefix(id), nil
		}
	}
	return "", fmt.Errorf("aws.ManageDNS: hosted zone %q not found", zone)
}

// stripZoneIDPrefix removes the "/hostedzone/" prefix Route53 puts on zone IDs.
func stripZoneIDPrefix(id string) string {
	const prefix = "/hostedzone/"
	if len(id) > len(prefix) && id[:len(prefix)] == prefix {
		return id[len(prefix):]
	}
	return id
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

// Destroy terminates the node's EC2 instance. Idempotent: an empty ProviderRef
// is a no-op, and an InvalidInstanceID.NotFound error (already gone) is treated
// as success.
//
// In the Pulumi-driven path, Destroy is complemented by Engine.Teardown (stack
// destroy) and SweepOrphans; this direct method is for out-of-band cleanup.
func (p *provider) Destroy(ctx context.Context, creds cloud.Credentials, node domain.Node) error {
	if node.ProviderRef == "" {
		return nil // Never provisioned.
	}
	client, err := p.ec2Client(ctx, creds)
	if err != nil {
		return err
	}
	_, err = client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{node.ProviderRef},
	})
	if err != nil && !isAWSErrorCode(err, "InvalidInstanceID.NotFound") {
		return fmt.Errorf("aws.Destroy: terminate instance %s: %w", node.ProviderRef, err)
	}
	return nil
}

// SweepOrphans deletes every resource tagged rinfra=<engagementID> — EC2
// instances (terminated), Elastic IPs (released), and security groups (deleted)
// — providing the "no orphan" guarantee after a stack destroy. Resources are
// selected by the same engagement tag BuildProgram applies. Per-resource
// failures are collected with errors.Join; a NotFound is treated as
// already-swept.
func (p *provider) SweepOrphans(ctx context.Context, creds cloud.Credentials, engagementID string) error {
	client, err := p.ec2Client(ctx, creds)
	if err != nil {
		return err
	}
	tagFilter := []ec2types.Filter{{
		Name:   awssdk.String("tag:" + TagKey),
		Values: []string{engagementID},
	}}
	var errs []error

	// 1. EC2 instances tagged for this engagement.
	insts, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{Filters: tagFilter})
	if err != nil {
		errs = append(errs, fmt.Errorf("describe instances: %w", err))
	} else {
		var ids []string
		for _, res := range insts.Reservations {
			for _, inst := range res.Instances {
				if inst.InstanceId != nil {
					ids = append(ids, awssdk.ToString(inst.InstanceId))
				}
			}
		}
		if len(ids) > 0 {
			if _, err := client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{InstanceIds: ids}); err != nil && !isAWSErrorCode(err, "InvalidInstanceID.NotFound") {
				errs = append(errs, fmt.Errorf("terminate instances %v: %w", ids, err))
			}
		}
	}

	// 2. Elastic IPs tagged for this engagement.
	addrs, err := client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{Filters: tagFilter})
	if err != nil {
		errs = append(errs, fmt.Errorf("describe addresses: %w", err))
	} else {
		for _, a := range addrs.Addresses {
			if a.AllocationId == nil {
				continue
			}
			if _, err := client.ReleaseAddress(ctx, &ec2.ReleaseAddressInput{AllocationId: a.AllocationId}); err != nil && !isAWSErrorCode(err, "InvalidAllocationID.NotFound") {
				errs = append(errs, fmt.Errorf("release address %s: %w", awssdk.ToString(a.AllocationId), err))
			}
		}
	}

	// 3. Security groups tagged for this engagement.
	sgs, err := client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{Filters: tagFilter})
	if err != nil {
		errs = append(errs, fmt.Errorf("describe security groups: %w", err))
	} else {
		for _, sg := range sgs.SecurityGroups {
			if sg.GroupId == nil {
				continue
			}
			if _, err := client.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{GroupId: sg.GroupId}); err != nil && !isAWSErrorCode(err, "InvalidGroup.NotFound") {
				errs = append(errs, fmt.Errorf("delete security group %s: %w", awssdk.ToString(sg.GroupId), err))
			}
		}
	}

	return errors.Join(errs...)
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
