package service

import (
	"context"
	"fmt"
	"time"

	"github.com/rinfra/rinfra/internal/audit"
	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/emulation"
	"github.com/rinfra/rinfra/internal/emulation/catalog"
	"github.com/rinfra/rinfra/internal/store"
)

// fakeOperator is a no-op Operator used in dev/test when no real C2 is deployed.
// It returns ExecSuccess for every technique without touching any real system.
type fakeOperator struct{}

func (fakeOperator) StartListener(_ context.Context, _ c2.ListenerSpec) error {
	return nil
}
func (fakeOperator) Sessions(_ context.Context) ([]c2.Session, error) {
	return []c2.Session{{ID: "fake-session-1", Host: "203.0.113.1", User: "SYSTEM"}}, nil
}
func (fakeOperator) Execute(_ context.Context, _ string, t domain.Technique) (domain.Result, error) {
	return domain.Result{
		TechniqueAttackID: t.AttackID,
		Status:            domain.ExecSuccess,
		Output:            fmt.Sprintf("fake execution of %s (%s)", t.AttackID, t.Name),
		StartedAt:         time.Now(),
		FinishedAt:        time.Now(),
	}, nil
}

// EmulationService runs adversary-emulation scenarios and tracks their results.
type EmulationService struct {
	engagements store.EngagementStore
	scenarios   store.ScenarioStore
	audit       audit.Logger
	orch        *emulation.Orchestrator
	hub         *Hub
}

// NewEmulationService constructs an EmulationService.
func NewEmulationService(
	engagements store.EngagementStore,
	scenarios store.ScenarioStore,
	a audit.Logger,
	hub *Hub,
) *EmulationService {
	return &EmulationService{
		engagements: engagements,
		scenarios:   scenarios,
		audit:       a,
		orch:        emulation.New(a),
		hub:         hub,
	}
}

// ListScenarios returns the built-in scenario catalog.
func (s *EmulationService) ListScenarios() []domain.Scenario {
	return catalog.List()
}

// Start begins running a scenario against an engagement. It gates on CanDeploy,
// persists the initial ScenarioRun, then runs async, publishing SSE events as
// techniques complete.
func (s *EmulationService) Start(ctx context.Context, engagementID, scenarioID, actor string) (string, error) {
	eng, err := s.engagements.Get(ctx, engagementID)
	if err != nil {
		return "", err
	}
	if err := eng.CanDeploy(time.Now()); err != nil {
		return "", fmt.Errorf("emulation start refused: %w", err)
	}

	sc, ok := catalog.Get(scenarioID)
	if !ok {
		return "", fmt.Errorf("scenario %q: %w", scenarioID, store.ErrNotFound)
	}

	run := domain.ScenarioRun{
		EngagementID: engagementID,
		ScenarioID:   scenarioID,
		Status:       domain.ExecRunning,
		StartedAt:    time.Now().UTC(),
	}
	runID, err := s.scenarios.SaveRun(ctx, run)
	if err != nil {
		return "", fmt.Errorf("persist scenario run: %w", err)
	}
	run.ID = runID

	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: engagementID,
		Actor:        actor,
		Action:       "emulation.start",
		Target:       scenarioID,
		Detail:       fmt.Sprintf("run_id=%s", runID),
		At:           time.Now().UTC(),
	})

	go s.runScenario(context.Background(), eng, sc, run, actor)

	return runID, nil
}

// runScenario executes the scenario using the emulation.Orchestrator (with a
// fake operator for now) and persists the completed run.
func (s *EmulationService) runScenario(ctx context.Context, eng domain.Engagement, sc domain.Scenario, run domain.ScenarioRun, actor string) {
	result, err := s.orch.Run(ctx, &eng, sc, "fake-session-1", fakeOperator{})
	if err != nil {
		run.Status = domain.ExecFailed
		run.FinishedAt = time.Now().UTC()
		_ = s.saveRun(ctx, run)
		s.hub.Publish(Event{Kind: EventRunStatus, EngagementID: eng.ID, Data: map[string]any{
			"runId":  run.ID,
			"status": string(run.Status),
		}})
		return
	}

	// Copy results and IDs back.
	result.ID = run.ID
	result.EngagementID = eng.ID

	// Publish per-technique events and then update the persisted run.
	for _, r := range result.Results {
		s.hub.Publish(Event{Kind: EventRunStatus, EngagementID: eng.ID, Data: map[string]any{
			"runId":       run.ID,
			"techniqueId": r.TechniqueAttackID,
			"status":      string(r.Status),
		}})
	}

	_ = s.saveRun(ctx, *result)

	s.hub.Publish(Event{Kind: EventRunStatus, EngagementID: eng.ID, Data: map[string]any{
		"runId":  run.ID,
		"status": string(result.Status),
	}})

	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: eng.ID,
		Actor:        actor,
		Action:       "emulation.complete",
		Target:       sc.ID,
		Detail:       fmt.Sprintf("run_id=%s status=%s", run.ID, result.Status),
		At:           time.Now().UTC(),
	})
}

// saveRun persists the updated run by calling SaveRun with the existing ID.
func (s *EmulationService) saveRun(ctx context.Context, run domain.ScenarioRun) error {
	_, err := s.scenarios.SaveRun(ctx, run)
	return err
}

// GetRun returns a ScenarioRun by ID.
func (s *EmulationService) GetRun(ctx context.Context, id string) (domain.ScenarioRun, error) {
	return s.scenarios.GetRun(ctx, id)
}

// ListRuns returns all runs for an engagement.
func (s *EmulationService) ListRuns(ctx context.Context, engagementID string) ([]domain.ScenarioRun, error) {
	return s.scenarios.RunsForEngagement(ctx, engagementID)
}
