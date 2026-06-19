package threatfeed

import "context"

// BundledSource serves a small, static set of advisories so the demo and CI are
// hermetic (no network). It mirrors the shape of real CISA KEV entries.
type BundledSource struct{}

func (BundledSource) Name() string { return "CISA KEV (bundled snapshot)" }

func (BundledSource) Fetch(_ context.Context) ([]Advisory, error) {
	return bundledAdvisories(), nil
}

func bundledAdvisories() []Advisory {
	raw := []Advisory{
		{
			ID: "CVE-2026-1041", Source: "CISA KEV", Title: "Acme Edge Gateway Remote Code Execution",
			Vendor: "Acme", Product: "Edge Gateway", Published: "2026-06-10",
			Summary: "Unauthenticated remote code execution in the management API allows arbitrary command execution.",
			URL:     "https://nvd.nist.gov/vuln/detail/CVE-2026-1041",
		},
		{
			ID: "CVE-2026-0907", Source: "CISA KEV", Title: "Globex VPN Authentication Bypass",
			Vendor: "Globex", Product: "SecureConnect VPN", Published: "2026-06-08",
			Summary: "Authentication bypass permits access with valid accounts without credentials.",
			URL:     "https://nvd.nist.gov/vuln/detail/CVE-2026-0907",
		},
		{
			ID: "CVE-2026-0455", Source: "CISA KEV", Title: "Initech Mail Server Web Shell Upload",
			Vendor: "Initech", Product: "Mail Server", Published: "2026-06-03",
			Summary: "Arbitrary file upload leads to web shell deployment and persistence.",
			URL:     "https://nvd.nist.gov/vuln/detail/CVE-2026-0455", Ransomware: true,
		},
		{
			ID: "CVE-2026-0188", Source: "CISA KEV", Title: "Umbrella ERP SQL Injection",
			Vendor: "Umbrella", Product: "ERP", Published: "2026-05-29",
			Summary: "SQL injection in the reporting module exposes credentials and enables data theft.",
			URL:     "https://nvd.nist.gov/vuln/detail/CVE-2026-0188",
		},
		{
			ID: "CVE-2026-0042", Source: "CISA KEV", Title: "Hooli Kernel Privilege Escalation",
			Vendor: "Hooli", Product: "OS Kernel", Published: "2026-05-21",
			Summary: "Local privilege escalation via improper handling allows elevation of privilege to SYSTEM.",
			URL:     "https://nvd.nist.gov/vuln/detail/CVE-2026-0042",
		},
	}
	for i := range raw {
		text := raw[i].Title + " " + raw[i].Summary
		if raw[i].Ransomware {
			text += " ransomware"
		}
		raw[i].Suggested = SuggestTTPs(text)
	}
	return raw
}
