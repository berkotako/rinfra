package c2

import "strings"

// Capabilities describes WHAT a framework can automate, for the technique
// routing layer. It is richer than SupportTier (which says HOW MUCH RInfra
// drives a framework): two Orchestrated frameworks may automate different
// platforms or tactics, and routing must pick one that actually fits the
// technique and the available agent.
//
// Empty slices mean "no constraint" (matches anything), so a framework that
// declares nothing is treated as broadly capable — providers narrow this by
// declaring the platforms/tactics/techniques they truly support.
type Capabilities struct {
	// Platforms is the implant OS support: "windows", "linux", "macos".
	Platforms []string
	// Tactics is the set of ATT&CK tactics it can automate (e.g. "execution").
	Tactics []string
	// Techniques is an explicit ATT&CK technique-ID allowlist. When non-empty it
	// takes precedence over Tactics (the framework automates exactly these).
	Techniques []string
	// ListenerProtocols is the set of listener types it can stand up.
	ListenerProtocols []string
}

// CapabilityProvider is implemented by C2Providers that expose routing metadata.
// Providers that don't implement it fall back to DefaultCapabilities(Tier()).
type CapabilityProvider interface {
	Capabilities() Capabilities
}

func containsFold(xs []string, v string) bool {
	for _, x := range xs {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

// SupportsPlatform reports whether the framework can operate on platform p.
// An empty p (unknown/any) or an unconstrained Platforms list always matches.
func (c Capabilities) SupportsPlatform(p string) bool {
	if p == "" || len(c.Platforms) == 0 {
		return true
	}
	return containsFold(c.Platforms, p)
}

// SupportsTactic reports whether the framework can automate the given tactic.
func (c Capabilities) SupportsTactic(tactic string) bool {
	if tactic == "" || len(c.Tactics) == 0 {
		return true
	}
	return containsFold(c.Tactics, tactic)
}

// SupportsTechnique reports whether the framework can automate the technique.
// An explicit Techniques allowlist wins; otherwise it falls back to the tactic.
func (c Capabilities) SupportsTechnique(attackID, tactic string) bool {
	if len(c.Techniques) > 0 {
		return containsFold(c.Techniques, attackID)
	}
	return c.SupportsTactic(tactic)
}

// CapabilitiesFor returns p's declared capabilities, or a tier-derived default
// when the provider does not implement CapabilityProvider.
func CapabilitiesFor(p C2Provider) Capabilities {
	if cp, ok := p.(CapabilityProvider); ok {
		return cp.Capabilities()
	}
	return DefaultCapabilities(p.Tier())
}

// DefaultCapabilities is the conservative fallback for a provider that declares
// no metadata: broadly capable (matches any platform/tactic). The router still
// excludes Fronted-tier providers from automation via the tier check, so a
// Fronted framework's default capabilities only mark it as a manual option.
func DefaultCapabilities(_ SupportTier) Capabilities {
	return Capabilities{}
}
