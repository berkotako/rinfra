// Package threatfeed monitors external threat advisories (e.g. the CISA Known
// Exploited Vulnerabilities catalog) and surfaces them as RInfra advisories with
// heuristic ATT&CK technique suggestions, so operators can fold emerging threats
// into the TTP library and emulation scenarios.
//
// A Source fetches raw advisories; the Service caches them and maps free text to
// suggested techniques. The bundled source keeps the demo/CI hermetic; the CISA
// KEV source (cisakev.go) fetches the live catalog when egress is configured.
package threatfeed

import (
	"context"
	"strings"
	"sync"
	"time"
)

// SuggestedTTP is a heuristic ATT&CK mapping for an advisory. It is a suggestion,
// not an authoritative mapping — confidence reflects keyword-match strength.
type SuggestedTTP struct {
	AttackID   string `json:"attackId"`
	Name       string `json:"name"`
	Tactic     string `json:"tactic"`
	Confidence string `json:"confidence"` // "high" | "medium" | "low"
}

// Advisory is a normalized threat advisory.
type Advisory struct {
	ID         string         `json:"id"`     // e.g. CVE id
	Source     string         `json:"source"` // e.g. "CISA KEV"
	Title      string         `json:"title"`
	Vendor     string         `json:"vendor"`
	Product    string         `json:"product"`
	Published  string         `json:"published"` // ISO date
	Summary    string         `json:"summary"`
	URL        string         `json:"url"`
	Ransomware bool           `json:"ransomware"`
	Suggested  []SuggestedTTP `json:"suggestedTtps"`
}

// Source fetches raw advisories from an upstream feed.
type Source interface {
	Name() string
	Fetch(ctx context.Context) ([]Advisory, error)
}

// Service caches advisories from a Source and serves them. Fetched lazily and
// refreshed when the cache is older than ttl. An optional FeedStore supplies
// operator-managed feeds that are merged with the base source on every refresh.
type Service struct {
	base  Source
	store FeedStore // optional; nil disables persistent feeds
	ttl   time.Duration

	mu        sync.Mutex
	cache     []Advisory
	fetchedAt time.Time
}

// New constructs a Service over the given base source (default refresh TTL 1h).
func New(src Source) *Service {
	return &Service{base: src, ttl: time.Hour}
}

// WithStore attaches a FeedStore so operator-managed feeds are collected
// alongside the base source. Returns the service for chaining.
func (s *Service) WithStore(store FeedStore) *Service {
	s.store = store
	return s
}

// effectiveSource composes the base source with the enabled persisted feeds.
func (s *Service) effectiveSource(ctx context.Context) Source {
	if s.store == nil {
		return s.base
	}
	feeds, err := s.store.ListFeeds(ctx)
	if err != nil || len(feeds) == 0 {
		return s.base // feeds unavailable — fall back to the base source
	}
	sources := []Source{s.base}
	for _, f := range feeds {
		if f.Enabled {
			sources = append(sources, feedToSource(f))
		}
	}
	if len(sources) == 1 {
		return s.base
	}
	return MultiSource{Sources: sources}
}

// SourceNames lists the advisory sources currently collected — the base
// source(s) plus any enabled persisted feeds — so the UI can show exactly which
// resources are in scope.
func (s *Service) SourceNames(ctx context.Context) []string {
	names := baseNames(s.base)
	if s.store != nil {
		if feeds, err := s.store.ListFeeds(ctx); err == nil {
			for _, f := range feeds {
				if f.Enabled {
					names = append(names, f.Name)
				}
			}
		}
	}
	return names
}

func baseNames(src Source) []string {
	if ms, ok := src.(MultiSource); ok {
		names := make([]string, 0, len(ms.Sources))
		for _, m := range ms.Sources {
			names = append(names, m.Name())
		}
		return names
	}
	return []string{src.Name()}
}

// List returns cached advisories, refreshing if the cache is empty or stale.
func (s *Service) List(ctx context.Context) ([]Advisory, error) {
	s.mu.Lock()
	fresh := time.Since(s.fetchedAt) < s.ttl && s.cache != nil
	cached := s.cache
	s.mu.Unlock()
	if fresh {
		return cached, nil
	}
	return s.Refresh(ctx)
}

// Refresh fetches advisories from the effective source and updates the cache.
// On a fetch error the previous cache is retained and returned.
func (s *Service) Refresh(ctx context.Context) ([]Advisory, error) {
	adv, err := s.effectiveSource(ctx).Fetch(ctx)
	if err != nil {
		s.mu.Lock()
		prev := s.cache
		s.mu.Unlock()
		if prev != nil {
			return prev, nil // serve stale rather than fail
		}
		return nil, err
	}
	for i := range adv {
		if adv[i].Suggested == nil {
			adv[i].Suggested = SuggestTTPs(adv[i].Title + " " + adv[i].Summary)
		}
	}
	s.mu.Lock()
	s.cache = adv
	s.fetchedAt = time.Now()
	s.mu.Unlock()
	return adv, nil
}

// keywordRule maps a free-text keyword to a suggested ATT&CK technique.
type keywordRule struct {
	keywords []string
	ttp      SuggestedTTP
}

var rules = []keywordRule{
	{[]string{"remote code execution", "rce", "arbitrary code", "code execution"},
		SuggestedTTP{"T1190", "Exploit Public-Facing Application", "Initial Access", "high"}},
	{[]string{"command injection", "os command"},
		SuggestedTTP{"T1059", "Command and Scripting Interpreter", "Execution", "high"}},
	{[]string{"privilege escalation", "elevation of privilege", "escalate privileges"},
		SuggestedTTP{"T1068", "Exploitation for Privilege Escalation", "Privilege Escalation", "high"}},
	{[]string{"authentication bypass", "auth bypass", "improper authentication"},
		SuggestedTTP{"T1078", "Valid Accounts", "Initial Access", "medium"}},
	{[]string{"sql injection", "sqli"},
		SuggestedTTP{"T1190", "Exploit Public-Facing Application", "Initial Access", "high"}},
	{[]string{"deserialization"},
		SuggestedTTP{"T1190", "Exploit Public-Facing Application", "Initial Access", "medium"}},
	{[]string{"path traversal", "directory traversal", "arbitrary file"},
		SuggestedTTP{"T1083", "File and Directory Discovery", "Discovery", "low"}},
	{[]string{"web shell", "webshell"},
		SuggestedTTP{"T1505.003", "Web Shell", "Persistence", "high"}},
	{[]string{"ransomware", "encrypt"},
		SuggestedTTP{"T1486", "Data Encrypted for Impact", "Impact", "medium"}},
	{[]string{"credential", "password disclosure", "information disclosure"},
		SuggestedTTP{"T1003", "OS Credential Dumping", "Credential Access", "low"}},
}

// SuggestTTPs maps advisory text to suggested ATT&CK techniques. A KEV entry is
// by definition exploited, so it always yields at least Exploit Public-Facing
// Application as a baseline initial-access suggestion.
func SuggestTTPs(text string) []SuggestedTTP {
	lower := strings.ToLower(text)
	seen := map[string]bool{}
	var out []SuggestedTTP
	for _, r := range rules {
		for _, kw := range r.keywords {
			if strings.Contains(lower, kw) {
				if !seen[r.ttp.AttackID] {
					seen[r.ttp.AttackID] = true
					out = append(out, r.ttp)
				}
				break
			}
		}
	}
	if len(out) == 0 {
		out = append(out, SuggestedTTP{"T1190", "Exploit Public-Facing Application", "Initial Access", "low"})
	}
	return out
}
