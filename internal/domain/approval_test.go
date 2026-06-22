package domain

import (
	"errors"
	"testing"
	"time"
)

func TestAuthorization_Validate(t *testing.T) {
	now := time.Now()
	valid := Authorization{
		AuthorizedBy: "CISO",
		DocumentRef:  "doc-1",
		GrantedAt:    now.Add(-time.Hour),
		ExpiresAt:    now.Add(time.Hour),
	}
	if err := valid.Validate(now); err != nil {
		t.Fatalf("valid authorization rejected: %v", err)
	}

	cases := map[string]Authorization{
		"missing approver":    {DocumentRef: "d", ExpiresAt: now.Add(time.Hour)},
		"missing document":    {AuthorizedBy: "a", ExpiresAt: now.Add(time.Hour)},
		"missing expiry":      {AuthorizedBy: "a", DocumentRef: "d"},
		"expiry before grant": {AuthorizedBy: "a", DocumentRef: "d", GrantedAt: now, ExpiresAt: now.Add(-time.Minute)},
		"expiry in the past":  {AuthorizedBy: "a", DocumentRef: "d", GrantedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)},
	}
	for name, a := range cases {
		t.Run(name, func(t *testing.T) {
			if err := a.Validate(now); !errors.Is(err, ErrAuthIncomplete) {
				t.Errorf("want ErrAuthIncomplete, got %v", err)
			}
		})
	}
}

func TestEngagement_CanAuthorize(t *testing.T) {
	for _, s := range []EngagementStatus{EngagementDraft, EngagementAuthorized, EngagementActive} {
		if err := (&Engagement{Status: s}).CanAuthorize(); err != nil {
			t.Errorf("status %q should be authorizable, got %v", s, err)
		}
	}
	for _, s := range []EngagementStatus{EngagementCompleted, EngagementArchived} {
		if err := (&Engagement{Status: s}).CanAuthorize(); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("status %q must not be re-authorizable, got %v", s, err)
		}
	}
}

func TestEngagement_CanTransitionTo(t *testing.T) {
	ok := []struct{ from, to EngagementStatus }{
		{EngagementDraft, EngagementArchived},
		{EngagementAuthorized, EngagementActive},
		{EngagementAuthorized, EngagementDraft},
		{EngagementActive, EngagementCompleted},
		{EngagementActive, EngagementArchived},
		{EngagementCompleted, EngagementArchived},
		{EngagementActive, EngagementActive}, // no-op
	}
	for _, c := range ok {
		if err := (&Engagement{Status: c.from}).CanTransitionTo(c.to); err != nil {
			t.Errorf("%q->%q should be allowed, got %v", c.from, c.to, err)
		}
	}

	bad := []struct{ from, to EngagementStatus }{
		{EngagementDraft, EngagementActive},      // skips authorization
		{EngagementDraft, EngagementAuthorized},  // must use Authorize
		{EngagementActive, EngagementAuthorized}, // must use Authorize
		{EngagementCompleted, EngagementActive},  // terminal revival
		{EngagementArchived, EngagementActive},   // terminal sink
	}
	for _, c := range bad {
		if err := (&Engagement{Status: c.from}).CanTransitionTo(c.to); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("%q->%q should be rejected, got %v", c.from, c.to, err)
		}
	}
}
