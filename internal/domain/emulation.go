package domain

import "time"

// Technique is RInfra's PORTABLE internal representation of an ATT&CK procedure.
// It is authored once and translated by each c2.Operator.Execute adapter into
// the target framework's native primitives, so scenarios are reusable across
// C2s.
//
// IMPORTANT: a Technique is a REFERENCE to a procedure in a public emulation
// library (Atomic Red Team test, Caldera ability) plus its parameters. It does
// NOT carry payloads, shellcode, or exploit code. The procedure content is
// sourced from those public libraries at execution time, not authored here.
type Technique struct {
	AttackID    string            // e.g. "T1059.001"
	Name        string            // human-readable
	Tactic      string            // e.g. "execution", "persistence"
	Source      TechniqueSource   // where the concrete procedure comes from
	SourceID    string            // ability id / atomic test GUID in that source
	Inputs      map[string]string // parameter bindings for the procedure
	Description string            // plain-language summary (operator-authored TTPs)
	Commands    []string          // portable procedure commands (operator-authored TTPs)
	// Requires lists fact keys (e.g. "host.ip") that must have been collected by
	// an earlier technique in the same run before this one can execute. When a
	// required fact is absent the technique is recorded not_run (an honest
	// non-attempt) rather than executed against a missing target. Inputs may also
	// reference collected facts with ${fact.key} tokens, resolved at run time.
	Requires []string
}

// TechniqueSource identifies the public library a technique's procedure is
// pulled from.
type TechniqueSource string

const (
	SourceAtomicRedTeam TechniqueSource = "atomic_red_team"
	SourceCaldera       TechniqueSource = "caldera"
)

// Scenario is an ordered adversary-emulation plan, typically modeling a known
// threat-actor profile. Built-in scenarios ship in the catalog; operators may
// also author their own, which are persisted via store.UserScenarioStore.
type Scenario struct {
	ID               string
	Name             string
	AdversaryProfile string // e.g. "APT29-like", "ransomware-affiliate-like"
	Description      string // operator-facing summary (optional)
	Techniques       []Technique
	CreatedAt        time.Time // set for operator-authored scenarios
}

// ExecutionStatus is the outcome of attempting a single technique.
//
// For honest BAS/coverage reporting the non-execution outcomes are kept
// distinct: a technique a human must run by hand (manual_required), one the
// deployed framework cannot translate (unsupported), one refused by scope
// (blocked_by_scope) or customer policy (skipped_policy), and one never reached
// (not_run) are NOT the same as a technique we actually ran. Only success and
// failed are genuine execution attempts; everything else is reported in its own
// bucket and excluded from the "attempted" count and the TRM denominator.
//
// "success"/"failed" retain their wire values so persisted rows and the web
// client keep working; the previously-overloaded "skipped" is split into the
// specific reasons below (the bare ExecSkipped constant is retained for
// backward compatibility and treated as a non-attempt).
type ExecutionStatus string

const (
	ExecPending ExecutionStatus = "pending"
	ExecRunning ExecutionStatus = "running"

	// Genuine execution attempts (count toward "attempted" and the TRM).
	ExecSuccess ExecutionStatus = "success" // executed on an agent
	ExecFailed  ExecutionStatus = "failed"  // ran but errored (attempted-failed)

	// Non-attempts — reported separately, never counted as attempted.
	ExecManualRequired ExecutionStatus = "manual_required"  // Fronted-tier: no operator API; a human runs it
	ExecUnsupported    ExecutionStatus = "unsupported"      // deployed framework can't translate this technique
	ExecBlockedByScope ExecutionStatus = "blocked_by_scope" // no in-scope agent; refused at execution time
	ExecSkippedPolicy  ExecutionStatus = "skipped_policy"   // skipped by customer/engagement policy
	ExecNotRun         ExecutionStatus = "not_run"          // never reached (default for un-exercised techniques)

	// Deprecated: generic skip, retained for backward compatibility with older
	// persisted runs. New code emits one of the specific reasons above.
	ExecSkipped ExecutionStatus = "skipped"
)

// IsAttempt reports whether the status represents a real execution attempt
// against an agent (success or failure). Only attempts count toward coverage's
// "attempted/exercised" total and the TRM denominator — manual, unsupported,
// scope/policy-skipped, and not-run techniques are validation gaps, not attempts.
func (s ExecutionStatus) IsAttempt() bool {
	return s == ExecSuccess || s == ExecFailed
}

// IsManual reports whether the technique requires a human operator (Fronted-tier
// or an unsupported translation) — surfaced as "manual" in reporting.
func (s ExecutionStatus) IsManual() bool {
	return s == ExecManualRequired || s == ExecUnsupported
}

// DetectionOutcome is the defender's response to an executed technique — the
// SRA-style three-part evaluation. A technique "passes" when the defenders
// blocked, detected, or raised an actionable alert on it.
type DetectionOutcome string

const (
	DetectNone     DetectionOutcome = "none"     // no defensive response observed
	DetectAlerted  DetectionOutcome = "alerted"  // an actionable SOC alert fired
	DetectDetected DetectionOutcome = "detected" // detected by a control (EDR/SIEM)
	DetectBlocked  DetectionOutcome = "blocked"  // prevented outright
)

// Passed reports whether the defenders handled the technique (block/detect/alert).
func (d DetectionOutcome) Passed() bool {
	switch d {
	case DetectAlerted, DetectDetected, DetectBlocked:
		return true
	default:
		return false
	}
}

// Result records the outcome of executing one Technique.
type Result struct {
	TechniqueAttackID string
	Status            ExecutionStatus
	Output            string           // sanitized summary, not raw tool output
	Detection         DetectionOutcome // defender response (block/detect/alert/none)
	StartedAt         time.Time
	FinishedAt        time.Time
	Err               string
}

// ScenarioRun is one execution of a Scenario against an engagement's
// infrastructure.
type ScenarioRun struct {
	ID           string
	EngagementID string
	ScenarioID   string
	Status       ExecutionStatus
	Results      []Result
	StartedAt    time.Time
	FinishedAt   time.Time
}
