package threatfeed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// JSONSource loads advisories authored in RInfra's native Advisory JSON schema
// ("our data style") from an HTTP(S) URL or a local file. This lets operators
// add their own feeds — internal threat intel, a curated vendor mirror, a
// partner's advisory export — without writing Go: emit a JSON document in the
// documented shape (see config/threatfeed.example.json) and point a source at
// it. The schema is exactly the Advisory type's JSON form; a top-level array or
// an object {"advisories": [...]} are both accepted.
type JSONSource struct {
	SourceName string       // label applied to advisories that omit "source"
	URL        string       // remote feed (http/https); takes precedence over File
	File       string       // local file path
	Client     *http.Client // optional; defaults to a 20s-timeout client
}

func (s *JSONSource) Name() string {
	if s.SourceName != "" {
		return s.SourceName
	}
	if s.URL != "" {
		return "custom feed (" + s.URL + ")"
	}
	return "custom feed (" + s.File + ")"
}

// Fetch loads and parses the feed from the configured URL or File.
func (s *JSONSource) Fetch(ctx context.Context) ([]Advisory, error) {
	data, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	adv, err := ParseAdvisories(data)
	if err != nil {
		return nil, err
	}
	label := s.SourceName
	if label == "" {
		label = "custom"
	}
	for i := range adv {
		if adv[i].Source == "" {
			adv[i].Source = label
		}
	}
	return adv, nil
}

func (s *JSONSource) load(ctx context.Context) ([]byte, error) {
	switch {
	case s.URL != "":
		client := s.Client
		if client == nil {
			client = &http.Client{Timeout: 20 * time.Second}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
		if err != nil {
			return nil, fmt.Errorf("custom feed: build request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("custom feed: fetch %s: %w", s.URL, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("custom feed: %s: unexpected status %d", s.URL, resp.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8 MiB cap
		if err != nil {
			return nil, fmt.Errorf("custom feed: read %s: %w", s.URL, err)
		}
		return data, nil
	case s.File != "":
		data, err := os.ReadFile(s.File)
		if err != nil {
			return nil, fmt.Errorf("custom feed: read %s: %w", s.File, err)
		}
		return data, nil
	default:
		return nil, errors.New("custom feed: neither URL nor File configured")
	}
}

// ParseAdvisories parses advisories in RInfra's native schema (testable, no
// network). It accepts either a top-level JSON array of Advisory objects or an
// object {"advisories": [...]}. Advisories missing a Suggested list get one from
// the title+summary heuristic, so a custom feed need not pre-map ATT&CK.
func ParseAdvisories(data []byte) ([]Advisory, error) {
	trimmed := bytes.TrimSpace(data)
	var adv []Advisory
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var wrap struct {
			Advisories []Advisory `json:"advisories"`
		}
		if err := json.Unmarshal(trimmed, &wrap); err != nil {
			return nil, fmt.Errorf("custom feed: parse: %w", err)
		}
		adv = wrap.Advisories
	} else if err := json.Unmarshal(trimmed, &adv); err != nil {
		return nil, fmt.Errorf("custom feed: parse: %w", err)
	}
	for i := range adv {
		if len(adv[i].Suggested) == 0 {
			adv[i].Suggested = SuggestTTPs(adv[i].Title + " " + adv[i].Summary)
		}
	}
	return adv, nil
}

// MultiSource fans out to several sources and merges their advisories,
// de-duplicating by ID (first source wins) and sorting newest-first by the
// Published date. It is fault-tolerant: a source that errors is skipped and its
// error retained; an aggregate error is returned only if every source fails, so
// one dead feed never blanks the whole list.
type MultiSource struct {
	Sources []Source
}

func (m MultiSource) Name() string {
	names := make([]string, 0, len(m.Sources))
	for _, s := range m.Sources {
		names = append(names, s.Name())
	}
	return strings.Join(names, " + ")
}

func (m MultiSource) Fetch(ctx context.Context) ([]Advisory, error) {
	var merged []Advisory
	seen := map[string]bool{}
	var errs []error
	ok := 0
	for _, s := range m.Sources {
		adv, err := s.Fetch(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Name(), err))
			continue
		}
		ok++
		for _, a := range adv {
			key := a.ID
			if key == "" {
				key = a.Source + "|" + a.Title
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, a)
		}
	}
	if ok == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	// ISO-8601 dates sort lexically; newest first. Stable keeps source order
	// for equal/empty dates.
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].Published > merged[j].Published
	})
	return merged, nil
}
