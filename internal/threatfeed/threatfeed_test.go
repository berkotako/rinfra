package threatfeed_test

import (
	"context"
	"testing"

	"github.com/rinfra/rinfra/internal/threatfeed"
)

func TestSuggestTTPs(t *testing.T) {
	tests := []struct {
		text string
		want string // an AttackID expected among suggestions
	}{
		{"Unauthenticated remote code execution in the API", "T1190"},
		{"Local privilege escalation to SYSTEM", "T1068"},
		{"Authentication bypass allows access", "T1078"},
		{"Arbitrary file upload leads to web shell", "T1505.003"},
		{"used in ransomware campaigns to encrypt files", "T1486"},
		{"a benign informational note", "T1190"}, // baseline fallback
	}
	for _, tt := range tests {
		got := threatfeed.SuggestTTPs(tt.text)
		found := false
		for _, s := range got {
			if s.AttackID == tt.want {
				found = true
			}
		}
		if !found {
			t.Errorf("SuggestTTPs(%q) = %+v, want %s among them", tt.text, got, tt.want)
		}
	}
}

const kevSample = `{
  "vulnerabilities": [
    {"cveID":"CVE-2024-0001","vendorProject":"Old","product":"Thing","vulnerabilityName":"Old RCE","dateAdded":"2024-01-01","shortDescription":"remote code execution","knownRansomwareCampaignUse":"Unknown"},
    {"cveID":"CVE-2026-0455","vendorProject":"Initech","product":"Mail","vulnerabilityName":"Web Shell Upload","dateAdded":"2026-06-03","shortDescription":"arbitrary file upload to web shell","knownRansomwareCampaignUse":"Known"}
  ]
}`

func TestParseKEV(t *testing.T) {
	adv, err := threatfeed.ParseKEV([]byte(kevSample), 0)
	if err != nil {
		t.Fatalf("ParseKEV: %v", err)
	}
	if len(adv) != 2 {
		t.Fatalf("advisories = %d, want 2", len(adv))
	}
	// Newest first (CVE-2026-0455 added later).
	if adv[0].ID != "CVE-2026-0455" {
		t.Errorf("first advisory = %s, want newest CVE-2026-0455", adv[0].ID)
	}
	if !adv[0].Ransomware {
		t.Error("CVE-2026-0455 should be flagged ransomware (Known)")
	}
	if adv[1].Ransomware {
		t.Error("CVE-2024-0001 (Unknown) should not be flagged ransomware")
	}
	if len(adv[0].Suggested) == 0 {
		t.Error("expected suggested TTPs")
	}

	// Limit keeps the most recent N.
	adv2, _ := threatfeed.ParseKEV([]byte(kevSample), 1)
	if len(adv2) != 1 || adv2[0].ID != "CVE-2026-0455" {
		t.Errorf("limit=1 should keep newest, got %+v", adv2)
	}
}

func TestService_BundledList(t *testing.T) {
	svc := threatfeed.New(threatfeed.BundledSource{})
	adv, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(adv) == 0 {
		t.Fatal("bundled source returned no advisories")
	}
	for _, a := range adv {
		if a.ID == "" || len(a.Suggested) == 0 {
			t.Errorf("advisory missing id or suggestions: %+v", a)
		}
	}
}
