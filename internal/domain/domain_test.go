package domain_test

import (
	"testing"
	"time"

	"github.com/rinfra/rinfra/internal/domain"
)

func TestEngagement_NewFields(t *testing.T) {
	e := domain.Engagement{
		ID:             "eng-1",
		Client:         "Acme Corp",
		Codename:       "OPERATION COBALT",
		LeadOperator:   "alice@example.com",
		EngagementType: domain.EngagementTypeRedTeam,
		Status:         domain.EngagementDraft,
		Scope: domain.Scope{
			AllowedTargets: []string{"10.0.0.0/8"},
			Exclusions:     []string{"10.0.1.0/24"},
			Notes:          "avoid prod",
		},
	}

	if e.Codename != "OPERATION COBALT" {
		t.Errorf("Codename = %q", e.Codename)
	}
	if e.LeadOperator != "alice@example.com" {
		t.Errorf("LeadOperator = %q", e.LeadOperator)
	}
	if e.EngagementType != domain.EngagementTypeRedTeam {
		t.Errorf("EngagementType = %q", e.EngagementType)
	}
	if len(e.Scope.Exclusions) != 1 {
		t.Errorf("Exclusions len = %d, want 1", len(e.Scope.Exclusions))
	}
}

func TestNode_CanvasFields(t *testing.T) {
	n := domain.Node{
		ID:           "node-1",
		EngagementID: "eng-1",
		Spec: domain.NodeSpec{
			Type:    domain.NodeRedirector,
			Cloud:   domain.CloudAWS,
			Region:  "us-east-1",
			Size:    "t3.small",
			Subtype: "https",
		},
		Canvas: domain.NodeCanvas{
			Name:         "HTTPS Redirector",
			Listener:     "0.0.0.0:443",
			FrontDomain:  "cdn.example.com",
			CostEstimate: 10.50,
			X:            100,
			Y:            200,
		},
	}

	if n.Canvas.Name != "HTTPS Redirector" {
		t.Errorf("Canvas.Name = %q", n.Canvas.Name)
	}
	if n.Canvas.CostEstimate != 10.50 {
		t.Errorf("Canvas.CostEstimate = %v", n.Canvas.CostEstimate)
	}
	if n.Spec.Subtype != "https" {
		t.Errorf("Spec.Subtype = %q", n.Spec.Subtype)
	}
}

func TestJob_StatusEnum(t *testing.T) {
	tests := []struct {
		kind   domain.JobKind
		status domain.JobStatus
	}{
		{domain.JobDeploy, domain.JobPending},
		{domain.JobTeardown, domain.JobRunning},
		{domain.JobScenarioRun, domain.JobDone},
		{domain.JobDeploy, domain.JobFailed},
	}
	for _, tc := range tests {
		j := domain.Job{
			EngagementID: "eng-1",
			Kind:         tc.kind,
			Status:       tc.status,
		}
		if j.Kind != tc.kind {
			t.Errorf("Kind = %q, want %q", j.Kind, tc.kind)
		}
		if j.Status != tc.status {
			t.Errorf("Status = %q, want %q", j.Status, tc.status)
		}
	}
}

func TestCanDeploy_StillWorks(t *testing.T) {
	now := time.Now()
	e := domain.Engagement{
		Status: domain.EngagementAuthorized,
		Scope:  domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
		Authorization: domain.Authorization{
			GrantedAt: now.Add(-1 * time.Hour),
			ExpiresAt: now.Add(1 * time.Hour),
		},
	}
	if err := e.CanDeploy(now); err != nil {
		t.Errorf("CanDeploy should succeed: %v", err)
	}
}
