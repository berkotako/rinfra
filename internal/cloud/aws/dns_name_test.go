package aws

import (
	"testing"

	"github.com/rinfra/rinfra/internal/domain"
)

// TestRoute53RecordName_NoDoubleZone locks the fix for the double-suffixed
// record name: the service layer passes the full FQDN as rec.Name, so the
// resulting Route53 record name must NOT re-append the zone.
func TestRoute53RecordName_NoDoubleZone(t *testing.T) {
	cases := []struct {
		name string
		rec  domain.Record
		want string
	}{
		{"full fqdn (service path)", domain.Record{Name: "cdn.example.com", Zone: "example.com"}, "cdn.example.com"},
		{"relative name (direct caller)", domain.Record{Name: "cdn", Zone: "example.com"}, "cdn.example.com"},
		{"apex @", domain.Record{Name: "@", Zone: "example.com"}, "example.com"},
		{"empty name is apex", domain.Record{Name: "", Zone: "example.com"}, "example.com"},
		{"trailing dots tolerated", domain.Record{Name: "cdn.example.com.", Zone: "example.com."}, "cdn.example.com"},
		{"deep subdomain full", domain.Record{Name: "a.b.example.com", Zone: "example.com"}, "a.b.example.com"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildRoute53RecordArgs(c.rec, "Z123").Name
			if got != c.want {
				t.Errorf("record name = %q, want %q", got, c.want)
			}
		})
	}
}
