package threatfeed

import "context"

// BundledSource serves a small, static set of advisories so the demo and CI are
// hermetic (no network). These are real entries drawn from the CISA Known
// Exploited Vulnerabilities catalog (snapshot 2026-06-18); set
// RINFRA_THREATFEED=cisa-kev to pull the live catalog instead.
type BundledSource struct{}

func (BundledSource) Name() string { return "CISA KEV (bundled snapshot)" }

func (BundledSource) Fetch(_ context.Context) ([]Advisory, error) {
	return bundledAdvisories(), nil
}

func bundledAdvisories() []Advisory {
	raw := []Advisory{
		{
			ID: "CVE-2026-20253", Source: "CISA KEV", Title: "Splunk Enterprise Missing Authentication for Critical Function Vulnerability",
			Vendor: "Splunk", Product: "Enterprise", Published: "2026-06-18",
			Summary: "Splunk Enterprise contains a missing authentication for critical function vulnerability which could allow an unauthenticated user to create or truncate arbitrary files through a PostgreSQL sidecar service endpoint.",
			URL:     "https://nvd.nist.gov/vuln/detail/CVE-2026-20253",
		},
		{
			ID: "CVE-2026-54420", Source: "CISA KEV", Title: "LiteSpeed cPanel Plugin UNIX Symbolic Link (Symlink) Following Vulnerability",
			Vendor: "LiteSpeed", Product: "cPanel Plugin", Published: "2026-06-15",
			Summary: "LiteSpeed cPanel plugin contains a UNIX symbolic link (Symlink) following vulnerability that could allow a user with FTP or web shell access on a shared hosting server running CloudLinux/CageFS.",
			URL:     "https://nvd.nist.gov/vuln/detail/CVE-2026-54420",
		},
		{
			ID: "CVE-2026-10520", Source: "CISA KEV", Title: "Ivanti Sentry OS Command Injection Vulnerability",
			Vendor: "Ivanti", Product: "Sentry", Published: "2026-06-11",
			Summary: "Ivanti Sentry (formerly known as MobileIron Sentry) contains an OS command injection vulnerability which could allow a remote unauthenticated user to achieve root-level remote code execution.",
			URL:     "https://nvd.nist.gov/vuln/detail/CVE-2026-10520",
		},
		{
			ID: "CVE-2026-11645", Source: "CISA KEV", Title: "Google Chromium V8 Out-of-Bounds Read and Write Vulnerability",
			Vendor: "Google", Product: "Chromium V8", Published: "2026-06-09",
			Summary: "Google Chromium V8 out-of-bounds read and write vulnerability that could allow a remote attacker to execute arbitrary code inside a sandbox via a crafted HTML page, affecting Chromium-based browsers (Chrome, Edge, Opera).",
			URL:     "https://nvd.nist.gov/vuln/detail/CVE-2026-11645",
		},
		{
			ID: "CVE-2026-50751", Source: "CISA KEV", Title: "Check Point Security Gateway Improper Authentication Vulnerability",
			Vendor: "Check Point", Product: "Security Gateway", Published: "2026-06-08",
			Summary: "Check Point Security Gateway contains an improper authentication vulnerability in IKEv1 key exchange that could allow an unauthenticated remote attacker to bypass user authentication and establish a remote access VPN connection without a valid user password.",
			URL:     "https://nvd.nist.gov/vuln/detail/CVE-2026-50751", Ransomware: true,
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
