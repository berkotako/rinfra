// Package c2 defines the abstraction over command-and-control frameworks.
//
// RInfra COMPOSES existing, publicly available C2 frameworks — it deploys and
// fronts them. It does NOT implement implants, beacons, payloads, or evasion.
//
// Provisioning + fronting is uniform across frameworks; CONTROL is tiered. A
// framework only supports automated emulation if it exposes a usable operator
// API, surfaced here as the (Operator, ok) return of Control().
package c2

import (
	"context"

	"github.com/rinfra/rinfra/internal/domain"
)

// SupportTier describes how much of a framework RInfra can drive.
type SupportTier int

const (
	// TierOrchestrated: deploy + redirector + automated emulation via Operator.
	TierOrchestrated SupportTier = iota
	// TierScripted: deploy + redirector + partial automation.
	TierScripted
	// TierFronted: deploy + redirector only; a human operates the framework.
	TierFronted
)

func (t SupportTier) String() string {
	switch t {
	case TierOrchestrated:
		return "orchestrated"
	case TierScripted:
		return "scripted"
	case TierFronted:
		return "fronted"
	default:
		return "unknown"
	}
}

// Config is the per-deploy configuration for a C2 teamserver. LicenseKey is
// required for license-gated frameworks (e.g. Cobalt Strike) and is always
// supplied by the customer per engagement — never bundled.
type Config struct {
	ListenerProfile string
	LicenseKey      string
	Extra           map[string]string
}

// Teamserver describes a deployed C2 control server.
type Teamserver struct {
	Host           string
	Port           int
	Status         string
	ConnectionInfo string // operator connection details (redacted in audit)
}

// ListenerSpec configures a listener on a deployed teamserver.
type ListenerSpec struct {
	Name     string
	Protocol string // "https", "dns", "smb", ...
	Bind     string
	Profile  string
}

// Session is an active implant session reported by the framework.
type Session struct {
	ID       string
	Host     string
	User     string
	Metadata map[string]string
}

// C2Provider is implemented once per framework (sliver, mythic, havoc,
// cobaltstrike, custom).
type C2Provider interface {
	// Name is the framework identifier, matching domain.NodeSpec.C2Framework.
	Name() string

	// Tier reports how fully RInfra can drive this framework.
	Tier() SupportTier

	// Deploy installs and starts the framework's teamserver on a provisioned
	// node. Compose existing release artifacts; do not build the framework.
	Deploy(ctx context.Context, node domain.Node, cfg Config) (Teamserver, error)

	// RedirectorConfig emits reverse-proxy config (e.g. nginx) that fronts this
	// framework's traffic for the given profile.
	RedirectorConfig(p domain.Profile) (string, error)

	// Control returns an Operator for automated emulation. ok=false means the
	// framework is Fronted-tier: provisioned and fronted, but driven by a human.
	Control(ts Teamserver) (Operator, bool)
}

// Operator is the automation surface used by the emulation engine. Only
// Orchestrated/Scripted-tier frameworks return one from Control().
type Operator interface {
	// StartListener starts a listener on the teamserver.
	StartListener(ctx context.Context, spec ListenerSpec) error

	// Sessions lists active implant sessions.
	Sessions(ctx context.Context) ([]Session, error)

	// Execute runs one portable Technique against a session, translating it to
	// the framework's native primitives. The concrete procedure is sourced from
	// the referenced public library (Atomic Red Team / Caldera).
	Execute(ctx context.Context, sessionID string, t domain.Technique) (domain.Result, error)
}
