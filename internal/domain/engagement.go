// Package domain holds RInfra's core types, shared across the cloud, c2,
// emulation, audit, and store packages. It has no dependencies on those
// packages (they depend on it), which keeps the dependency graph acyclic.
package domain

import (
	"errors"
	"fmt"
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

// Engagement is the top-level unit of work. Every Node, ScenarioRun, and
// audit.Event is bound to an Engagement.
type Engagement struct {
	ID            string
	Client        string
	Status        EngagementStatus
	Scope         Scope
	RoE           RulesOfEngagement
	Authorization Authorization
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Sentinel errors returned by CanDeploy so callers can branch on cause.
var (
	ErrNotAuthorized    = errors.New("engagement is not in an authorizable state")
	ErrAuthExpired      = errors.New("engagement authorization has expired or not yet valid")
	ErrOutsideWindow    = errors.New("current time is outside the rules-of-engagement window")
	ErrEmptyScope       = errors.New("engagement has no in-scope targets defined")
	ErrTargetNotInScope = errors.New("target is not within the engagement scope")
)

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

// TargetInScope reports whether a given target (IP, CIDR, or domain) falls
// within the engagement scope. NOTE: implement real CIDR/domain matching when
// wiring this up; this signature is the contract callers rely on.
func (e *Engagement) TargetInScope(target string) bool {
	for _, t := range e.Scope.AllowedTargets {
		if t == target {
			return true
		}
	}
	// TODO(claude-code): CIDR containment + domain suffix matching.
	return false
}
