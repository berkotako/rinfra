// Package emulation runs adversary-emulation scenarios through a deployed C2's
// Operator. It is framework-agnostic: it speaks only the portable
// domain.Technique format and the c2.Operator interface.
package emulation

import (
	"context"
	"fmt"
	"time"

	"github.com/rinfra/rinfra/internal/audit"
	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
)

// Orchestrator executes scenarios and records every step to the audit log.
type Orchestrator struct {
	Audit audit.Logger
}

// New returns an Orchestrator.
func New(a audit.Logger) *Orchestrator {
	return &Orchestrator{Audit: a}
}

// Run executes a scenario against an engagement.
//
// op may be nil when the engagement's C2 framework is Fronted-tier (no operator
// API): in that case every technique is recorded as Skipped, signalling that a
// human must drive the framework manually. The returned ScenarioRun is fully
// populated regardless.
func (o *Orchestrator) Run(
	ctx context.Context,
	eng *domain.Engagement,
	scenario domain.Scenario,
	sessionID string,
	op c2.Operator,
) (*domain.ScenarioRun, error) {
	if err := eng.CanDeploy(time.Now()); err != nil {
		// Emulation activity is in-scope only for a deployable engagement.
		return nil, fmt.Errorf("emulation refused: %w", err)
	}

	run := &domain.ScenarioRun{
		EngagementID: eng.ID,
		ScenarioID:   scenario.ID,
		Status:       domain.ExecRunning,
		StartedAt:    time.Now(),
	}

	o.record(ctx, eng.ID, "scenario.start", scenario.ID, "")

	for _, t := range scenario.Techniques {
		if op == nil { // Fronted tier: no automation available.
			run.Results = append(run.Results, domain.Result{
				TechniqueAttackID: t.AttackID,
				Status:            domain.ExecManualRequired,
				Output:            "no operator API (fronted-tier framework); run manually",
				StartedAt:         time.Now(),
				FinishedAt:        time.Now(),
			})
			o.record(ctx, eng.ID, "technique.skipped", t.AttackID, "fronted tier")
			continue
		}

		o.record(ctx, eng.ID, "technique.execute", t.AttackID, t.Name)
		res, err := op.Execute(ctx, sessionID, t)
		if err != nil {
			res = domain.Result{
				TechniqueAttackID: t.AttackID,
				Status:            domain.ExecFailed,
				StartedAt:         time.Now(),
				FinishedAt:        time.Now(),
				Err:               err.Error(),
			}
		}
		run.Results = append(run.Results, res)
		o.record(ctx, eng.ID, "technique.result", t.AttackID, string(res.Status))
	}

	run.FinishedAt = time.Now()
	run.Status = summarize(run.Results)
	o.record(ctx, eng.ID, "scenario.finish", scenario.ID, string(run.Status))
	return run, nil
}

func (o *Orchestrator) record(ctx context.Context, engID, action, target, detail string) {
	if o.Audit == nil {
		return
	}
	_ = o.Audit.Record(ctx, audit.Event{
		EngagementID: engID,
		Action:       action,
		Target:       target,
		Detail:       detail,
		At:           time.Now(),
	})
}

// summarize rolls per-technique results up to a run-level status.
func summarize(results []domain.Result) domain.ExecutionStatus {
	if len(results) == 0 {
		return domain.ExecNotRun
	}
	anyFailed, anySuccess := false, false
	for _, r := range results {
		switch r.Status {
		case domain.ExecFailed:
			anyFailed = true
		case domain.ExecSuccess:
			anySuccess = true
		}
	}
	switch {
	case anyFailed:
		return domain.ExecFailed
	case anySuccess:
		return domain.ExecSuccess
	default:
		// No genuine attempt — surface the actual reason (manual/blocked/...).
		return results[0].Status
	}
}
