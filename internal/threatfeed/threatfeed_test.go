package threatfeed_test

import (
	"context"
	"os"
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

func TestParseAdvisories(t *testing.T) {
	// Top-level array, mixed: one pre-mapped, one needing heuristic suggestions.
	arr := `[
	  {"id":"X-1","title":"RCE in portal","summary":"remote code execution","source":"Intel"},
	  {"id":"X-2","title":"Bypass","summary":"authentication bypass","suggestedTtps":[{"attackId":"T9999","name":"Custom","tactic":"Impact","confidence":"high"}]}
	]`
	adv, err := threatfeed.ParseAdvisories([]byte(arr))
	if err != nil {
		t.Fatalf("ParseAdvisories array: %v", err)
	}
	if len(adv) != 2 {
		t.Fatalf("got %d advisories, want 2", len(adv))
	}
	if adv[0].Suggested[0].AttackID != "T1190" {
		t.Errorf("X-1 should get heuristic T1190, got %+v", adv[0].Suggested)
	}
	if adv[1].Suggested[0].AttackID != "T9999" {
		t.Errorf("X-2 explicit mapping should be preserved, got %+v", adv[1].Suggested)
	}

	// Object wrapper form.
	obj := `{"advisories":[{"id":"Y-1","title":"web shell upload","summary":"webshell"}]}`
	adv2, err := threatfeed.ParseAdvisories([]byte(obj))
	if err != nil || len(adv2) != 1 || adv2[0].ID != "Y-1" {
		t.Fatalf("wrapper parse failed: %v %+v", err, adv2)
	}
}

func TestJSONSource_File(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/feed.json"
	if err := os.WriteFile(path, []byte(`[{"id":"F-1","title":"command injection","summary":"os command"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	src := &threatfeed.JSONSource{File: path, SourceName: "Local Intel"}
	adv, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(adv) != 1 || adv[0].ID != "F-1" {
		t.Fatalf("got %+v", adv)
	}
	if adv[0].Source != "Local Intel" {
		t.Errorf("source label not applied, got %q", adv[0].Source)
	}
}

// failSource always errors; used to exercise MultiSource fault tolerance.
type failSource struct{}

func (failSource) Name() string { return "broken" }
func (failSource) Fetch(context.Context) ([]threatfeed.Advisory, error) {
	return nil, context.DeadlineExceeded
}

func TestMultiSource_MergeDedupAndTolerateErrors(t *testing.T) {
	ms := threatfeed.MultiSource{Sources: []threatfeed.Source{
		threatfeed.BundledSource{},
		failSource{}, // one dead feed must not blank the list
	}}
	adv, err := ms.Fetch(context.Background())
	if err != nil {
		t.Fatalf("MultiSource with one good source should not error: %v", err)
	}
	if len(adv) == 0 {
		t.Fatal("expected advisories from the bundled source")
	}
	// Newest-first ordering by Published.
	for i := 1; i < len(adv); i++ {
		if adv[i-1].Published < adv[i].Published {
			t.Errorf("not sorted newest-first at %d: %q before %q", i, adv[i-1].Published, adv[i].Published)
		}
	}
	// De-dup by id across sources.
	seen := map[string]bool{}
	for _, a := range adv {
		if seen[a.ID] {
			t.Errorf("duplicate id %s", a.ID)
		}
		seen[a.ID] = true
	}

	// All sources failing yields an error.
	if _, err := (threatfeed.MultiSource{Sources: []threatfeed.Source{failSource{}}}).Fetch(context.Background()); err == nil {
		t.Error("expected error when every source fails")
	}
}

// fakeFeedStore is an in-test threatfeed.FeedStore.
type fakeFeedStore struct{ feeds []threatfeed.Feed }

func (s *fakeFeedStore) ListFeeds(context.Context) ([]threatfeed.Feed, error) { return s.feeds, nil }
func (s *fakeFeedStore) CreateFeed(_ context.Context, f threatfeed.Feed) (threatfeed.Feed, error) {
	s.feeds = append(s.feeds, f)
	return f, nil
}
func (s *fakeFeedStore) DeleteFeed(_ context.Context, id string) error {
	for i, f := range s.feeds {
		if f.ID == id {
			s.feeds = append(s.feeds[:i], s.feeds[i+1:]...)
			return nil
		}
	}
	return nil
}

func TestFeedValidate(t *testing.T) {
	bad := []threatfeed.Feed{
		{Name: "", Kind: "url", URL: "https://x"},
		{Name: "n", Kind: "url", URL: "not a url"},
		{Name: "n", Kind: "url", URL: "ftp://x"},
		{Name: "n", Kind: "inline", Inline: ""},
		{Name: "n", Kind: "inline", Inline: "{ not json"},
		{Name: "n", Kind: "bogus"},
	}
	for i, f := range bad {
		if err := f.Validate(); err == nil {
			t.Errorf("case %d: expected validation error for %+v", i, f)
		}
	}
	good := []threatfeed.Feed{
		{Name: "n", Kind: "url", URL: "https://intel.example.com/a.json"},
		{Name: "n", Kind: "inline", Inline: `[{"id":"A","title":"rce","summary":"remote code execution"}]`},
	}
	for i, f := range good {
		if err := f.Validate(); err != nil {
			t.Errorf("good case %d unexpectedly invalid: %v", i, err)
		}
	}
}

func TestServiceFeeds_MergeAndCRUD(t *testing.T) {
	ctx := context.Background()
	svc := threatfeed.New(threatfeed.BundledSource{}).WithStore(&fakeFeedStore{})

	// Add an inline feed with one advisory.
	f, err := svc.AddFeed(ctx, threatfeed.Feed{
		Name:   "Internal Intel",
		Kind:   "inline",
		Inline: `[{"id":"INT-1","title":"web shell upload","summary":"webshell"}]`,
	})
	if err != nil {
		t.Fatalf("AddFeed: %v", err)
	}
	if f.ID == "" || !f.Enabled {
		t.Errorf("AddFeed should assign id and enable: %+v", f)
	}

	// List should now include the feed advisory alongside the bundled ones.
	adv, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, a := range adv {
		if a.ID == "INT-1" {
			found = true
			if a.Source != "Internal Intel" {
				t.Errorf("feed advisory source = %q, want feed name", a.Source)
			}
		}
	}
	if !found {
		t.Error("collected advisories should include the inline feed entry")
	}

	// SourceNames should include the feed name.
	names := svc.SourceNames(ctx)
	if !contains(names, "Internal Intel") {
		t.Errorf("SourceNames = %v, want it to include the feed", names)
	}

	// Delete and confirm it's gone.
	if err := svc.DeleteFeed(ctx, f.ID); err != nil {
		t.Fatalf("DeleteFeed: %v", err)
	}
	adv2, _ := svc.List(ctx)
	for _, a := range adv2 {
		if a.ID == "INT-1" {
			t.Error("deleted feed advisory still collected")
		}
	}
}

func TestServiceFeeds_NoStore(t *testing.T) {
	svc := threatfeed.New(threatfeed.BundledSource{})
	if _, err := svc.AddFeed(context.Background(), threatfeed.Feed{Name: "x", Kind: "url", URL: "https://x.test/a"}); err == nil {
		t.Error("AddFeed without a store should return ErrFeedsUnsupported")
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
