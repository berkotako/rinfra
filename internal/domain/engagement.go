// Package domain holds RInfra's core types, shared across the cloud, c2,
// emulation, audit, and store packages. It has no dependencies on those
// packages (they depend on it), which keeps the dependency graph acyclic.
package domain

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// EngagementStatus is the lifecycle state of an engagement. Infrastructure may
// only be provisioned while an engagement is Authorized or Active.
type EngagementStatus string

const (
	EngagementDraft      EngagementStatus = "draft"
	EngagementAuthorized EngagementStatus = "authorized"
	EngagementActive     EngagementStatus = "active"
	EngagementCompleted  EngagementStatus = "completed"
	EngagementArchived   EngagementStatus = "archived"
)

// Scope defines what an engagement is permitted to touch. Targets are CIDRs or
// fully-qualified domains. Anything not in scope must be rejected.
type Scope struct {
	AllowedTargets []string // CIDRs and/or domains the engagement may target
	Exclusions     []string // CIDRs and/or domains explicitly excluded from scope
	Notes          string
}

// RulesOfEngagement captures the agreed constraints for an engagement,
// including the authorized testing window.
type RulesOfEngagement struct {
	DocumentRef string    // pointer to the signed RoE artifact
	WindowStart time.Time // earliest permitted activity
	WindowEnd   time.Time // latest permitted activity
	Constraints []string  // free-form, e.g. "no DoS", "no prod DB writes"
}

// Authorization is the explicit, time-bounded sign-off that unlocks deployment.
// No infrastructure is provisioned without a valid Authorization.
type Authorization struct {
	AuthorizedBy string // named approver on the client side
	DocumentRef  string // pointer to the authorization artifact
	GrantedAt    time.Time
	ExpiresAt    time.Time
}

// EngagementType classifies the kind of assessment, informing how results are
// reported and which emulation scenarios are appropriate.
type EngagementType string

const (
	EngagementTypeRedTeam    EngagementType = "red_team"
	EngagementTypePurpleTeam EngagementType = "purple_team"
	EngagementTypePenTest    EngagementType = "pentest"
)

// Engagement is the top-level unit of work. Every Node, ScenarioRun, and
// audit.Event is bound to an Engagement.
type Engagement struct {
	ID             string
	ProjectID      string // project this engagement belongs to; may be empty
	Client         string
	Codename       string // short operational name, e.g. "OPERATION COBALT"
	LeadOperator   string // primary operator responsible for the engagement
	EngagementType EngagementType
	Status         EngagementStatus
	Scope          Scope
	RoE            RulesOfEngagement
	Authorization  Authorization
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Sentinel errors returned by CanDeploy so callers can branch on cause.
var (
	ErrNotAuthorized    = errors.New("engagement is not in an authorizable state")
	ErrAuthExpired      = errors.New("engagement authorization has expired or not yet valid")
	ErrOutsideWindow    = errors.New("current time is outside the rules-of-engagement window")
	ErrEmptyScope       = errors.New("engagement has no in-scope targets defined")
	ErrTargetNotInScope = errors.New("target is not within the engagement scope")

	// Approval-process errors.
	ErrAuthIncomplete    = errors.New("authorization is incomplete or invalid")
	ErrInvalidTransition = errors.New("invalid engagement status transition")
)

// Validate checks that an Authorization is complete and coherent at time `now`,
// so an engagement cannot be marked authorized with a missing approver, missing
// signed-authorization reference, or a non-future / inverted validity window.
// GrantedAt is assumed already defaulted by the caller when zero.
func (a Authorization) Validate(now time.Time) error {
	if strings.TrimSpace(a.AuthorizedBy) == "" {
		return fmt.Errorf("%w: authorizedBy is required", ErrAuthIncomplete)
	}
	if strings.TrimSpace(a.DocumentRef) == "" {
		return fmt.Errorf("%w: documentRef (signed authorization artifact) is required", ErrAuthIncomplete)
	}
	if a.ExpiresAt.IsZero() {
		return fmt.Errorf("%w: expiresAt is required", ErrAuthIncomplete)
	}
	if !a.ExpiresAt.After(a.GrantedAt) {
		return fmt.Errorf("%w: expiresAt must be after grantedAt", ErrAuthIncomplete)
	}
	if !a.ExpiresAt.After(now) {
		return fmt.Errorf("%w: expiresAt must be in the future", ErrAuthIncomplete)
	}
	return nil
}

// CanAuthorize reports whether the engagement may be (re)authorized from its
// current status. Terminal states (completed/archived) cannot be revived by an
// authorization; they must not silently become deployable again.
func (e *Engagement) CanAuthorize() error {
	switch e.Status {
	case EngagementDraft, EngagementAuthorized, EngagementActive:
		return nil
	default:
		return fmt.Errorf("%w: cannot authorize from status %q", ErrInvalidTransition, e.Status)
	}
}

// statusTransitions is the allowed operator/API status moves. Authorization
// itself is NOT reachable here — it must go through the validated Authorize
// path — and terminal states are sinks. The engine sets active/completed
// internally (deploy/teardown) and does not go through this table.
var statusTransitions = map[EngagementStatus]map[EngagementStatus]bool{
	EngagementDraft:      {EngagementArchived: true},
	EngagementAuthorized: {EngagementActive: true, EngagementDraft: true, EngagementArchived: true},
	EngagementActive:     {EngagementCompleted: true, EngagementArchived: true},
	EngagementCompleted:  {EngagementArchived: true},
	EngagementArchived:   {},
}

// CanTransitionTo validates an operator-initiated status change. Transitioning
// to Authorized is rejected here on purpose: authorization must use Authorize
// (which validates the approver, document, and validity window).
func (e *Engagement) CanTransitionTo(next EngagementStatus) error {
	if next == e.Status {
		return nil
	}
	if next == EngagementAuthorized {
		return fmt.Errorf("%w: use the authorize action to move to %q", ErrInvalidTransition, next)
	}
	if statusTransitions[e.Status][next] {
		return nil
	}
	return fmt.Errorf("%w: %q -> %q", ErrInvalidTransition, e.Status, next)
}

// CanDeploy enforces the authorization gate. Call this before ANY provisioning
// path. It returns nil only when the engagement may legitimately stand up
// infrastructure at time `now`.
func (e *Engagement) CanDeploy(now time.Time) error {
	switch e.Status {
	case EngagementAuthorized, EngagementActive:
		// ok
	default:
		return fmt.Errorf("%w: status=%s", ErrNotAuthorized, e.Status)
	}
	if now.Before(e.Authorization.GrantedAt) || now.After(e.Authorization.ExpiresAt) {
		return ErrAuthExpired
	}
	if !e.RoE.WindowStart.IsZero() && now.Before(e.RoE.WindowStart) {
		return ErrOutsideWindow
	}
	if !e.RoE.WindowEnd.IsZero() && now.After(e.RoE.WindowEnd) {
		return ErrOutsideWindow
	}
	if len(e.Scope.AllowedTargets) == 0 {
		return ErrEmptyScope
	}
	return nil
}

// TargetInScope reports whether a given target (IP, CIDR, or domain) is allowed
// by the engagement scope. Exclusions take precedence: a target matching any
// exclusion is out of scope even if it also matches an allowed entry.
//
// Matching rules (entry → target):
//   - CIDR entry: matches an IP inside it, or a CIDR fully contained within it.
//   - IP entry: matches that exact IP (or a /32/-/128 single-host CIDR of it).
//   - domain entry "example.com": matches "example.com" and any subdomain
//     "*.example.com" (label-boundary suffix).
//   - wildcard entry "*.example.com": matches subdomains only, not the apex.
//
// Unparseable / empty targets are treated as out of scope.
func (e *Engagement) TargetInScope(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, ex := range e.Scope.Exclusions {
		if scopeEntryMatches(ex, target) {
			return false
		}
	}
	for _, al := range e.Scope.AllowedTargets {
		if scopeEntryMatches(al, target) {
			return true
		}
	}
	return false
}

// EnforceTargetInScope returns ErrTargetNotInScope when target is out of scope.
// Call this from every operation that acts on a target host/domain (emulation
// execution against a session host, etc.), not just at deploy time.
func (e *Engagement) EnforceTargetInScope(target string) error {
	if !e.TargetInScope(target) {
		return fmt.Errorf("%w: %q", ErrTargetNotInScope, target)
	}
	return nil
}

// scopeEntryMatches reports whether a single scope entry matches a target. It is
// used for both allowed targets and exclusions.
func scopeEntryMatches(entry, target string) bool {
	entry = strings.TrimSpace(entry)
	target = strings.TrimSpace(target)
	if entry == "" || target == "" {
		return false
	}

	// CIDR entry.
	if _, entryNet, err := net.ParseCIDR(entry); err == nil {
		if ip := net.ParseIP(target); ip != nil {
			return entryNet.Contains(ip)
		}
		if tIP, tNet, err := net.ParseCIDR(target); err == nil {
			if !entryNet.Contains(tIP) {
				return false
			}
			eOnes, eBits := entryNet.Mask.Size()
			tOnes, tBits := tNet.Mask.Size()
			return eBits == tBits && tOnes >= eOnes // target range fits inside entry
		}
		return false // domain target vs CIDR entry never matches
	}

	// Bare IP entry.
	if entryIP := net.ParseIP(entry); entryIP != nil {
		if tIP := net.ParseIP(target); tIP != nil {
			return entryIP.Equal(tIP)
		}
		if tIP, tNet, err := net.ParseCIDR(target); err == nil {
			ones, bits := tNet.Mask.Size()
			return ones == bits && entryIP.Equal(tIP) // single-host CIDR == entry IP
		}
		return false
	}

	// Domain / wildcard entry — only matches domain targets.
	if net.ParseIP(target) != nil {
		return false
	}
	if _, _, err := net.ParseCIDR(target); err == nil {
		return false
	}
	td, ok := normalizeDomain(target)
	if !ok {
		return false
	}
	if strings.HasPrefix(entry, "*.") {
		base, ok := normalizeDomain(entry[2:])
		if !ok {
			return false
		}
		return strings.HasSuffix(td, "."+base) && td != base
	}
	ed, ok := normalizeDomain(entry)
	if !ok {
		return false
	}
	return td == ed || strings.HasSuffix(td, "."+ed)
}

// normalizeDomain lowercases a hostname and strips a trailing dot. It returns
// ok=false for empty input or values that are clearly not hostnames.
func normalizeDomain(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, ".")
	if s == "" || strings.ContainsAny(s, " /\\:") {
		return "", false
	}
	return s, true
}
