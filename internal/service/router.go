package service

import (
	"context"
	"strings"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
)

// Capability routing.
//
// The naive resolver picks "the first live C2 server". Real BAS needs to route
// each technique to a C2/agent that can actually run it: the framework must be
// able to automate the technique on the agent's platform, the agent session
// must be in scope, and (where the technique demands it) sufficiently
// privileged. This file implements that selection:
//
//	technique -> required capabilities -> candidate sessions -> scope/policy
//	          -> selected operator/session   (or a precise non-attempt reason)
//
// When no candidate fits, Route returns the appropriate execution status from
// the BAS taxonomy (unsupported / manual_required / blocked_by_scope) so the
// coverage report stays honest rather than silently picking a wrong agent.

// Candidate is one deployed C2 framework available to the engagement, together
// with its routing metadata and its active agent sessions. Operator is nil for
// Fronted-tier frameworks (which can only be driven manually).
type Candidate struct {
	Framework string
	Tier      c2.SupportTier
	Caps      c2.Capabilities
	Operator  c2.Operator
	Sessions  []c2.Session
}

// CandidateResolver enumerates every deployed C2 framework (with its live
// sessions) for an engagement, so the router can choose per technique across
// frameworks. RegistryResolver implements it; resolvers that don't are driven
// through the single-operator legacy path.
type CandidateResolver interface {
	Candidates(ctx context.Context, eng domain.Engagement) []Candidate
}

// Route selects the best (operator, sessionID) to run technique t against, or
// returns a non-attempt ExecutionStatus explaining why none fit. An empty
// status with a non-nil operator means "execute".
func Route(eng *domain.Engagement, t domain.Technique, cands []Candidate) (c2.Operator, string, domain.ExecutionStatus) {
	plat := RequiredPlatform(t)
	reqPriv := strings.EqualFold(t.Inputs["requires_privilege"], "true")

	var (
		bestOp    c2.Operator
		bestSID   string
		bestScore = -1
		blocked   bool // an automatable framework supports it, but no usable session
		manual    bool // only a Fronted framework can host it
	)

	for _, c := range cands {
		supports := c.Caps.SupportsTechnique(t.AttackID, t.Tactic) && c.Caps.SupportsPlatform(plat)
		automatable := c.Tier != c2.TierFronted && c.Operator != nil
		if !automatable {
			if supports {
				manual = true
			}
			continue
		}
		if !supports {
			continue
		}
		sid, score, ok := bestSession(eng, plat, reqPriv, c.Tier, c.Sessions)
		if !ok {
			blocked = true
			continue
		}
		if score > bestScore {
			bestOp, bestSID, bestScore = c.Operator, sid, score
		}
	}

	if bestOp != nil {
		return bestOp, bestSID, ""
	}
	switch {
	case blocked:
		return nil, "", domain.ExecBlockedByScope
	case manual:
		return nil, "", domain.ExecManualRequired
	default:
		return nil, "", domain.ExecUnsupported
	}
}

// bestSession picks the highest-scoring in-scope session on a framework that can
// run technique t (platform/privilege aware). ok=false means the framework
// cannot run it on any available, in-scope agent.
func bestSession(eng *domain.Engagement, plat string, reqPriv bool, tier c2.SupportTier, sessions []c2.Session) (string, int, bool) {
	bestSID := ""
	bestScore := -1
	for _, s := range sessions {
		if s.Host != "" && eng != nil && !eng.TargetInScope(s.Host) {
			continue // out of engagement scope — never route here
		}
		sp := sessionPlatform(s)
		if plat != "" && sp != "" && !strings.EqualFold(sp, plat) {
			continue // wrong agent OS for this technique
		}
		priv := sessionPrivileged(s)
		if reqPriv && !priv {
			continue // technique needs elevation this session lacks
		}
		score := tierScore(tier)
		if plat != "" && strings.EqualFold(sp, plat) {
			score += 4 // exact platform match preferred over an "any" agent
		}
		if priv {
			score += 2
		}
		if score > bestScore {
			bestScore, bestSID = score, s.ID
		}
	}
	if bestSID == "" {
		return "", 0, false
	}
	return bestSID, bestScore, true
}

func tierScore(t c2.SupportTier) int {
	if t == c2.TierOrchestrated {
		return 10 // prefer full automation over scripted partial automation
	}
	return 5
}

// RequiredPlatform derives the OS a technique needs from its inputs or a small
// table of platform-specific ATT&CK techniques. "" means platform-agnostic.
func RequiredPlatform(t domain.Technique) string {
	if p := strings.ToLower(strings.TrimSpace(t.Inputs["platform"])); p != "" {
		return p
	}
	switch t.AttackID {
	case "T1059.001", // PowerShell
		"T1059.003", // Windows Command Shell
		"T1547.001", // Registry Run Keys
		"T1053.005", // Scheduled Task
		"T1218",     // System Binary Proxy Execution (Windows)
		"T1003.001": // LSASS Memory
		return "windows"
	case "T1059.004": // Unix Shell
		return "linux"
	default:
		return ""
	}
}

// sessionPlatform reads the agent OS from session metadata ("os"/"platform").
func sessionPlatform(s c2.Session) string {
	for _, k := range []string{"os", "platform"} {
		if v := strings.ToLower(strings.TrimSpace(s.Metadata[k])); v != "" {
			return normalizeOS(v)
		}
	}
	return ""
}

func normalizeOS(v string) string {
	switch {
	case strings.Contains(v, "win"):
		return "windows"
	case strings.Contains(v, "linux"):
		return "linux"
	case strings.Contains(v, "darwin"), strings.Contains(v, "mac"), strings.Contains(v, "osx"):
		return "macos"
	default:
		return v
	}
}

// sessionPrivileged reports whether the agent runs with elevated privileges,
// inferred from the session user (SYSTEM/root/admin) or a metadata flag.
func sessionPrivileged(s c2.Session) bool {
	if strings.EqualFold(strings.TrimSpace(s.Metadata["privileged"]), "true") {
		return true
	}
	u := strings.ToLower(s.User)
	return strings.Contains(u, "system") ||
		strings.Contains(u, "root") ||
		strings.Contains(u, "admin")
}
