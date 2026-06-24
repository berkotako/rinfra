package redirector

import (
	"sort"

	"github.com/rinfra/rinfra/internal/domain"
)

// Built-in redirector profiles. A node's NodeSpec.ProfileName selects one; this
// is the data the renderer needs (RewriteHost / PathRules) that the node record
// itself does not carry. Operators get a small, sane starter set; richer
// operator-authored profiles are a future store-backed addition (the lookup
// seam below is where that plugs in).
var builtinProfiles = map[string]domain.Profile{
	// Plain relay: forward everything, preserve Host. The default.
	"plain": {Name: "plain"},

	// CDN/jQuery categorized relay: only the paths a benign CDN would serve are
	// proxied to the C2; everything else is dropped. Host rewritten to the
	// fronted CDN domain.
	"cdn-jquery": {
		Name:        "cdn-jquery",
		RewriteHost: "ajax.googleapis.com",
		PathRules:   []string{"/ajax/libs/jquery/", "/jquery.min.js"},
	},

	// API-style relay: a small allowlist of API paths, host preserved.
	"api-relay": {
		Name:      "api-relay",
		PathRules: []string{"/api/", "/health", "/static/"},
	},
}

// LookupProfile returns the named built-in profile. ok=false for an unknown
// name; callers should fall back to PlainProfile so a redirector still renders.
func LookupProfile(name string) (domain.Profile, bool) {
	p, ok := builtinProfiles[name]
	return p, ok
}

// PlainProfile is the safe default used when a node names no/unknown profile.
func PlainProfile() domain.Profile { return builtinProfiles["plain"] }

// ProfileNames lists the built-in profile names, sorted — for the UI/API to
// offer a picker.
func ProfileNames() []string {
	names := make([]string, 0, len(builtinProfiles))
	for n := range builtinProfiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
