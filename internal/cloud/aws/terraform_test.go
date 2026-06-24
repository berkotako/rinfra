package aws

import (
	"testing"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
)

// TestBuildConfig_AMIFromSSM verifies the Terraform AWS builder resolves the AMI
// from the region-correct SSM data source instead of a hardcoded image, so
// deploys outside us-east-1 are valid.
func TestBuildConfig_AMIFromSSM(t *testing.T) {
	p := &provider{}
	creds := cloud.Credentials{Provider: domain.CloudAWS, Raw: map[string]string{CredKeyRegion: "eu-west-1"}}
	nodes := []domain.Node{{ID: "11111111-2222-3333-4444-555555555555", Spec: domain.NodeSpec{Type: domain.NodeC2Server, Cloud: domain.CloudAWS}}}

	cfg, err := p.BuildConfig("eng-aaaaaaaa", creds, nodes)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	// The SSM data source must exist and the instance must reference it.
	ssm, ok := cfg.Data["aws_ssm_parameter"].(map[string]any)
	if !ok {
		t.Fatalf("expected aws_ssm_parameter data source, got %#v", cfg.Data)
	}
	if _, ok := ssm["rinfra_ami"]; !ok {
		t.Errorf("expected rinfra_ami data source: %#v", ssm)
	}

	instances := cfg.Resource["aws_instance"].(map[string]any)
	for k, v := range instances {
		ami := v.(map[string]any)["ami"]
		if ami != "${data.aws_ssm_parameter.rinfra_ami.value}" {
			t.Errorf("instance %s ami = %v, want SSM data reference (not a hardcoded AMI)", k, ami)
		}
	}
}
