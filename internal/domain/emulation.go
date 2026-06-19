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

// ExecutionStatus is the outcome of running a single technique.
type ExecutionStatus string

const (
	ExecPending ExecutionStatus = "pending"
	ExecRunning ExecutionStatus = "running"
	ExecSuccess ExecutionStatus = "success"
	ExecFailed  ExecutionStatus = "failed"
	ExecSkipped ExecutionStatus = "skipped" // e.g. Fronted-tier C2: no Operator
)

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
