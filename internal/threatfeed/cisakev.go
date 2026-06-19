package threatfeed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// CISAKEVURL is the public CISA Known Exploited Vulnerabilities catalog (JSON,
// no auth). Fetching it requires outbound egress.
const CISAKEVURL = "https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json"

// CISAKEVSource fetches the live CISA KEV catalog. It implements Source.
type CISAKEVSource struct {
	URL    string
	Client *http.Client
	Limit  int // most-recent N entries to keep (0 = all)
}

// NewCISAKEVSource returns a CISA KEV source with sensible defaults.
func NewCISAKEVSource() *CISAKEVSource {
	return &CISAKEVSource{
		URL:    CISAKEVURL,
		Client: &http.Client{Timeout: 20 * time.Second},
		Limit:  40,
	}
}

func (s *CISAKEVSource) Name() string { return "CISA KEV" }

type kevCatalog struct {
	Vulnerabilities []kevEntry `json:"vulnerabilities"`
}

type kevEntry struct {
	CveID                      string `json:"cveID"`
	VendorProject              string `json:"vendorProject"`
	Product                    string `json:"product"`
	VulnerabilityName          string `json:"vulnerabilityName"`
	DateAdded                  string `json:"dateAdded"`
	ShortDescription           string `json:"shortDescription"`
	KnownRansomwareCampaignUse string `json:"knownRansomwareCampaignUse"`
}

// Fetch retrieves and parses the catalog. Exported ParseKEV does the conversion
// so it can be unit-tested without network.
func (s *CISAKEVSource) Fetch(ctx context.Context) ([]Advisory, error) {
	url := s.URL
	if url == "" {
		url = CISAKEVURL
	}
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("cisa kev: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cisa kev: fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cisa kev: unexpected status %d", resp.StatusCode)
	}
	dec := json.NewDecoder(resp.Body)
	var cat kevCatalog
	if err := dec.Decode(&cat); err != nil {
		return nil, fmt.Errorf("cisa kev: decode: %w", err)
	}
	return kevToAdvisories(cat, s.Limit), nil
}

// ParseKEV converts raw CISA KEV JSON bytes to advisories (testable, no network).
func ParseKEV(data []byte, limit int) ([]Advisory, error) {
	var cat kevCatalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return nil, fmt.Errorf("cisa kev: parse: %w", err)
	}
	return kevToAdvisories(cat, limit), nil
}

func kevToAdvisories(cat kevCatalog, limit int) []Advisory {
	entries := cat.Vulnerabilities
	// KEV is appended chronologically; keep the most recent `limit` entries.
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	out := make([]Advisory, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- { // newest first
		e := entries[i]
		ransom := e.KnownRansomwareCampaignUse != "" &&
			e.KnownRansomwareCampaignUse != "Unknown"
		text := e.VulnerabilityName + " " + e.ShortDescription
		if ransom {
			text += " ransomware"
		}
		out = append(out, Advisory{
			ID:         e.CveID,
			Source:     "CISA KEV",
			Title:      e.VulnerabilityName,
			Vendor:     e.VendorProject,
			Product:    e.Product,
			Published:  e.DateAdded,
			Summary:    e.ShortDescription,
			URL:        "https://nvd.nist.gov/vuln/detail/" + e.CveID,
			Ransomware: ransom,
			Suggested:  SuggestTTPs(text),
		})
	}
	return out
}
