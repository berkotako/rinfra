package domain

import "time"

// CredentialMeta is the non-sensitive metadata returned when listing stored
// credentials. The ciphertext and key material are never returned to callers;
// only the Postgres store implementation holds them.
type CredentialMeta struct {
	ID           string
	EngagementID string
	Provider     string // cloud provider id or "c2:<framework>" for license keys
	KeyID        string // identifier of the wrapping data-key
	CreatedAt    time.Time
	LastUsedAt   *time.Time // nil if never used
}

// JobKind classifies the background work a Job represents.
type JobKind string

const (
	JobDeploy      JobKind = "deploy"
	JobTeardown    JobKind = "teardown"
	JobScenarioRun JobKind = "scenario_run"
)

// JobStatus tracks a background Job through its lifecycle.
type JobStatus string

const (
	JobPending JobStatus = "pending"
	JobRunning JobStatus = "running"
	JobDone    JobStatus = "done"
	JobFailed  JobStatus = "failed"
)

// Job is a durable record of a background operation (deploy, teardown, scenario
// run). It survives server restarts so the boot-time reconciler can re-adopt
// jobs that were in-flight when the process exited.
type Job struct {
	ID           string
	EngagementID string
	Kind         JobKind
	Status       JobStatus
	Detail       map[string]any // free-form JSON context
	CreatedAt    time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
	Err          string
}
