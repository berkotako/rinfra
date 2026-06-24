// Package redirector renders a portable domain.Profile plus its resolved
// upstream into concrete reverse-proxy configuration for a redirector node.
//
// This is the translation layer the rest of the infra side was missing: the
// canvas/topology expresses a redirector (a NodeRedirector with a Profile and an
// Edge to the C2/payload node it fronts), and this package turns that into an
// nginx reverse-proxy config. It deliberately follows the reverse-proxy +
// categorized-domain posture documented in CLAUDE.md (no domain fronting): only
// allowlisted request paths are relayed to the upstream; everything else is
// dropped, so the redirector doubles as a coarse traffic filter.
package redirector

import (
	"fmt"
	"strings"

	"github.com/rinfra/rinfra/internal/domain"
)

// Upstream is the target a redirector forwards to — the C2 server or payload
// host on the other end of the topology Edge.
type Upstream struct {
	Host string // target IP or hostname
	Port int    // target listener port
	TLS  bool   // whether the upstream speaks TLS (https proxy_pass)
}

// scheme returns the proxy_pass scheme for the upstream.
func (u Upstream) scheme() string {
	if u.TLS {
		return "https"
	}
	return "http"
}

// RenderNginx renders an nginx reverse-proxy server block for an HTTP/HTTPS
// redirector. It is pure and deterministic so it can be unit-tested and diffed.
//
//   - profile.RewriteHost, if set, becomes the Host header sent upstream (a C2
//     malleable-profile staple); otherwise the original Host is preserved.
//   - profile.PathRules, if non-empty, are the ONLY request paths relayed; every
//     other path returns 444 (connection closed) — the categorized-domain filter.
//     With no rules the redirector relays everything (a plain relay).
//   - frontDomain, if set, pins server_name; otherwise it matches any host.
//
// subtype selects the listener: "https" listens on 443 (TLS), anything else
// ("http"/"") listens on 80. "dns" is not an nginx concern and returns an error.
func RenderNginx(profile domain.Profile, up Upstream, subtype, frontDomain string) (string, error) {
	if strings.EqualFold(subtype, "dns") {
		return "", fmt.Errorf("redirector: dns subtype is not an nginx reverse-proxy target")
	}
	if up.Host == "" || up.Port == 0 {
		return "", fmt.Errorf("redirector: upstream host and port are required")
	}

	serverName := "_"
	if frontDomain != "" {
		serverName = frontDomain
	}
	hostHeader := "$host"
	if profile.RewriteHost != "" {
		hostHeader = profile.RewriteHost
	}
	upstreamURL := fmt.Sprintf("%s://%s:%d", up.scheme(), up.Host, up.Port)

	var b strings.Builder
	fmt.Fprintf(&b, "# RInfra redirector — profile %q\n", profile.Name)
	b.WriteString("server {\n")
	if strings.EqualFold(subtype, "https") {
		b.WriteString("    listen 443 ssl;\n")
		b.WriteString("    # TLS material provisioned out of band (e.g. ACME on first boot).\n")
		b.WriteString("    ssl_certificate     /etc/rinfra/redirector/tls.crt;\n")
		b.WriteString("    ssl_certificate_key /etc/rinfra/redirector/tls.key;\n")
	} else {
		b.WriteString("    listen 80;\n")
	}
	fmt.Fprintf(&b, "    server_name %s;\n\n", serverName)

	proxyBlock := func(indent string) {
		fmt.Fprintf(&b, "%sproxy_pass %s;\n", indent, upstreamURL)
		fmt.Fprintf(&b, "%sproxy_set_header Host %s;\n", indent, hostHeader)
		fmt.Fprintf(&b, "%sproxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n", indent)
		fmt.Fprintf(&b, "%sproxy_set_header X-Forwarded-Proto $scheme;\n", indent)
	}

	rules := dedupeNonEmpty(profile.PathRules)
	if len(rules) == 0 {
		// Plain relay: forward everything.
		b.WriteString("    location / {\n")
		proxyBlock("        ")
		b.WriteString("    }\n")
	} else {
		// Categorized relay: only allowlisted paths reach the upstream.
		for _, r := range rules {
			fmt.Fprintf(&b, "    location %s {\n", r)
			proxyBlock("        ")
			b.WriteString("    }\n")
		}
		b.WriteString("    # Default deny: unlisted paths are dropped (no upstream exposure).\n")
		b.WriteString("    location / {\n")
		b.WriteString("        return 444;\n")
		b.WriteString("    }\n")
	}
	b.WriteString("}\n")
	return b.String(), nil
}

// dedupeNonEmpty trims, drops empties, and de-duplicates path rules while
// preserving order.
func dedupeNonEmpty(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
