package service

import (
	"testing"

	"github.com/rinfra/rinfra/internal/domain"
)

// TestDNSRecordFor verifies the front-domain A record is shaped per the
// provider's ManageDNS expectations (relative name for DO/Azure, FQDN for
// AWS/GCP, GCP with trailing dot), all anchored on the apex zone.
func TestDNSRecordFor(t *testing.T) {
	const fqdn, ip = "cdn.example.com", "203.0.113.5"
	cases := []struct {
		provider domain.CloudProviderType
		wantZone string
		wantName string
	}{
		{domain.CloudDigitalOcean, "example.com", "cdn"},
		{domain.CloudAzure, "example.com", "cdn"},
		{domain.CloudAWS, "example.com", "cdn.example.com"},
		{domain.CloudGCP, "example.com", "cdn.example.com."},
	}
	for _, c := range cases {
		rec := dnsRecordFor(c.provider, fqdn, ip)
		if rec.Zone != c.wantZone || rec.Name != c.wantName {
			t.Errorf("%s: got Zone=%q Name=%q, want Zone=%q Name=%q", c.provider, rec.Zone, rec.Name, c.wantZone, c.wantName)
		}
		if rec.Type != "A" || rec.Value != ip {
			t.Errorf("%s: got Type=%q Value=%q, want A/%s", c.provider, rec.Type, rec.Value, ip)
		}
	}

	// Apex front domain → relative name "@" for the relative-name providers.
	if rec := dnsRecordFor(domain.CloudDigitalOcean, "example.com", ip); rec.Name != "@" {
		t.Errorf("apex DO record Name = %q, want @", rec.Name)
	}
}
