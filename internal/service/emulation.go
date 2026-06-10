package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/rinfra/rinfra/internal/audit"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/emulation"
	"github.com/rinfra/rinfra/internal/emulation/catalog"
	"github.com/rinfra/rinfra/internal/store"
)

// EmulationService runs adversary-emulation scenarios and tracks their results.
type EmulationService struct {
	engagements store.EngagementStore
	scenarios   store.ScenarioStore
	audit       audit.Logger
	orch        *emulation.Orchestrator
	hub         *Hub
	resolver    OperatorResolver
}

// NewEmulationService constructs an EmulationService with a FakeResolver
// (dev/CI default). Call WithResolver to inject a production RegistryResolver.
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
		resolver:    NewFakeResolver(),
	}
}

// WithResolver replaces the OperatorResolver. Call with NewRegistryResolver for
// production, or NewFakeResolver / NewFrontedResolver in tests.
func (s *EmulationService) WithResolver(r OperatorResolver) {
	s.resolver = r
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

// runScenario resolves the Operator, executes the scenario, and persists results.
func (s *EmulationService) runScenario(ctx context.Context, eng domain.Engagement, sc domain.Scenario, run domain.ScenarioRun, actor string) {
	// Resolve the operator: may be nil for Fronted-tier (all techniques → Skipped).
	op, sessionID, _ := s.resolver.Resolve(ctx, eng)

	// Publish per-technique SSE events as each technique completes by using a
	// wrapping orchestrator approach. We drive the orchestrator but also hook
	// into each technique result to publish SSE + persist incrementally.
	result, err := s.orchRunWithHooks(ctx, &eng, sc, sessionID, op, run.ID)
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

	// Copy IDs back to the result.
	result.ID = run.ID
	result.EngagementID = eng.ID

	// Persist the final run status (results already persisted incrementally).
	result.Results = nil // avoid double-writing; incremental path already ran
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

// orchRunWithHooks drives the orchestrator but also persists each result and
// publishes SSE events incrementally as techniques complete.
func (s *EmulationService) orchRunWithHooks(
	ctx context.Context,
	eng *domain.Engagement,
	sc domain.Scenario,
	sessionID string,
	op interface {
		Execute(context.Context, string, domain.Technique) (domain.Result, error)
	},
	runID string,
) (*domain.ScenarioRun, error) {
	// Check authorization.
	if err := eng.CanDeploy(time.Now()); err != nil {
		return nil, fmt.Errorf("emulation refused: %w", err)
	}

	run := &domain.ScenarioRun{
		ID:           runID,
		EngagementID: eng.ID,
		ScenarioID:   sc.ID,
		Status:       domain.ExecRunning,
		StartedAt:    time.Now(),
	}

	// Import c2.Operator check indirectly: op may be nil (Fronted tier).
	type executer interface {
		Execute(context.Context, string, domain.Technique) (domain.Result, error)
	}
	var opExec executer
	if op != nil {
		opExec = op
	}

	for _, t := range sc.Techniques {
		var res domain.Result
		if opExec == nil {
			// Fronted tier: skip all techniques.
			res = domain.Result{
				TechniqueAttackID: t.AttackID,
				Status:            domain.ExecSkipped,
				Output:            "no operator API (fronted-tier framework); run manually",
				StartedAt:         time.Now(),
				FinishedAt:        time.Now(),
			}
		} else {
			var err error
			res, err = opExec.Execute(ctx, sessionID, t)
			if err != nil {
				res = domain.Result{
					TechniqueAttackID: t.AttackID,
					Status:            domain.ExecFailed,
					StartedAt:         time.Now(),
					FinishedAt:        time.Now(),
					Err:               err.Error(),
				}
			}
		}

		// Persist the result immediately.
		_ = s.scenarios.SaveResult(ctx, runID, res)

		// Publish SSE per-technique event.
		s.hub.Publish(Event{Kind: EventRunStatus, EngagementID: eng.ID, Data: map[string]any{
			"runId":       runID,
			"techniqueId": res.TechniqueAttackID,
			"status":      string(res.Status),
		}})

		run.Results = append(run.Results, res)
	}

	run.FinishedAt = time.Now()
	run.Status = summarizeResults(run.Results)
	return run, nil
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

// summarizeResults rolls per-technique results up to a run-level status.
func summarizeResults(results []domain.Result) domain.ExecutionStatus {
	if len(results) == 0 {
		return domain.ExecSkipped
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
		return domain.ExecSkipped
	}
}

// ---------- Coverage rollup ----------

// CoverageLevel is the coverage score for a technique.
// 0=not exercised, 1=attempted (run but failed), 2=executed (success), 3=validated (success + detected).
type CoverageLevel int

const (
	CoverageLevelNone      CoverageLevel = 0
	CoverageLevelAttempted CoverageLevel = 1
	CoverageLevelExecuted  CoverageLevel = 2
	CoverageLevelValidated CoverageLevel = 3
)

// Techniquecoverage holds the coverage data for one technique.
type Techniquecoverage struct {
	AttackID string        `json:"attackID"`
	Name     string        `json:"name"`
	Tactic   string        `json:"tactic"`
	Level    CoverageLevel `json:"level"`
}

// TacticCoverage holds all technique coverages for one tactic.
type TacticCoverage struct {
	Tactic     string              `json:"tactic"`
	Techniques []Techniquecoverage `json:"techniques"`
}

// Coverage is the full ATT&CK coverage rollup for an engagement.
type Coverage struct {
	EngagementID string           `json:"engagementId"`
	Tactics      []TacticCoverage `json:"tactics"`
	// Summary fields for the dashboard cards.
	TotalTechniques int `json:"totalTechniques"`
	ExercisedCount  int `json:"exercisedCount"` // level >= 1
	ExecutedCount   int `json:"executedCount"`  // level >= 2
	ValidatedCount  int `json:"validatedCount"` // level == 3
}

// GetCoverage rolls up all runs for an engagement into a Coverage report.
func (s *EmulationService) GetCoverage(ctx context.Context, engagementID string) (Coverage, error) {
	runs, err := s.scenarios.RunsForEngagement(ctx, engagementID)
	if err != nil {
		return Coverage{}, fmt.Errorf("list runs: %w", err)
	}

	// Collect full results: for each run, fetch from store to get technique_results.
	// (RunsForEngagement may not load results — call GetRun for each.)
	type techKey struct{ attackID string }
	levels := make(map[string]CoverageLevel) // attackID → best level
	names := make(map[string]string)
	tactics := make(map[string]string)

	for _, run := range runs {
		full, err := s.scenarios.GetRun(ctx, run.ID)
		if err != nil {
			continue
		}
		for _, r := range full.Results {
			lvl := resultToLevel(r.Status)
			if existing, ok := levels[r.TechniqueAttackID]; !ok || lvl > existing {
				levels[r.TechniqueAttackID] = lvl
			}
		}
	}

	// Build the tactic→technique index from the catalog for display context.
	tacticOrder := []string{
		"initial-access", "execution", "persistence", "privilege-escalation",
		"defense-evasion", "credential-access", "discovery", "lateral-movement",
		"collection", "command-and-control", "exfiltration", "impact",
	}
	tacticTechs := make(map[string][]Techniquecoverage)
	for _, sc := range catalog.List() {
		for _, t := range sc.Techniques {
			names[t.AttackID] = t.Name
			tactics[t.AttackID] = t.Tactic
		}
	}

	// Build technique coverage per tactic from catalog.
	for _, sc := range catalog.List() {
		for _, t := range sc.Techniques {
			lvl := levels[t.AttackID] // default 0 = not exercised
			tc := Techniquecoverage{
				AttackID: t.AttackID,
				Name:     t.Name,
				Tactic:   t.Tactic,
				Level:    lvl,
			}
			// Deduplicate: if already added for this tactic with the same ID, keep best.
			tacs := tacticTechs[t.Tactic]
			found := false
			for i, existing := range tacs {
				if existing.AttackID == t.AttackID {
					if lvl > existing.Level {
						tacs[i].Level = lvl
					}
					found = true
					break
				}
			}
			if !found {
				tacticTechs[t.Tactic] = append(tacticTechs[t.Tactic], tc)
			}
		}
	}

	// Build ordered tactic list.
	seen := make(map[string]bool)
	var tacticList []TacticCoverage
	for _, tactic := range tacticOrder {
		techs, ok := tacticTechs[tactic]
		if !ok {
			continue
		}
		seen[tactic] = true
		tacticList = append(tacticList, TacticCoverage{
			Tactic:     tactic,
			Techniques: techs,
		})
	}
	// Any tactics not in tacticOrder (e.g. future additions).
	var extraTactics []string
	for tactic := range tacticTechs {
		if !seen[tactic] {
			extraTactics = append(extraTactics, tactic)
		}
	}
	sort.Strings(extraTactics)
	for _, tactic := range extraTactics {
		tacticList = append(tacticList, TacticCoverage{
			Tactic:     tactic,
			Techniques: tacticTechs[tactic],
		})
	}

	// Summary counts.
	total, exercised, executed, validated := 0, 0, 0, 0
	for _, tac := range tacticList {
		for _, tc := range tac.Techniques {
			total++
			if tc.Level >= 1 {
				exercised++
			}
			if tc.Level >= 2 {
				executed++
			}
			if tc.Level == 3 {
				validated++
			}
		}
	}

	_ = names
	_ = tactics

	return Coverage{
		EngagementID:    engagementID,
		Tactics:         tacticList,
		TotalTechniques: total,
		ExercisedCount:  exercised,
		ExecutedCount:   executed,
		ValidatedCount:  validated,
	}, nil
}

// resultToLevel converts a technique execution status to a coverage level.
// The rubric:
//
//	0 = not exercised (never ran)
//	1 = attempted (ran but failed/skipped)
//	2 = executed (ran and succeeded)
//	3 = validated (TODO: wire detection signal; for now same as executed)
func resultToLevel(status domain.ExecutionStatus) CoverageLevel {
	switch status {
	case domain.ExecSuccess:
		return CoverageLevelExecuted
	case domain.ExecFailed:
		return CoverageLevelAttempted
	case domain.ExecSkipped:
		return CoverageLevelAttempted
	default:
		return CoverageLevelNone
	}
}

// ---------- Navigator export ----------

// NavigatorLayer is the ATT&CK Navigator layer JSON format.
// https://github.com/mitre-attack/attack-navigator/blob/master/CHANGELOG.md
type NavigatorLayer struct {
	Name        string               `json:"name"`
	Versions    navigatorVersions    `json:"versions"`
	Domain      string               `json:"domain"`
	Description string               `json:"description"`
	Techniques  []navigatorTechnique `json:"techniques"`
	Gradient    navigatorGradient    `json:"gradient"`
}

type navigatorVersions struct {
	Attack    string `json:"attack"`
	Navigator string `json:"navigator"`
	Layer     string `json:"layer"`
}

type navigatorTechnique struct {
	TechniqueID string `json:"techniqueID"`
	Score       int    `json:"score"`
	Color       string `json:"color,omitempty"`
	Comment     string `json:"comment,omitempty"`
	Enabled     bool   `json:"enabled"`
}

type navigatorGradient struct {
	Colors   []string `json:"colors"`
	MinValue int      `json:"minValue"`
	MaxValue int      `json:"maxValue"`
}

// GetNavigatorLayer builds an ATT&CK Navigator layer JSON from coverage data.
func (s *EmulationService) GetNavigatorLayer(ctx context.Context, engagementID string, name string) (NavigatorLayer, error) {
	coverage, err := s.GetCoverage(ctx, engagementID)
	if err != nil {
		return NavigatorLayer{}, fmt.Errorf("get coverage: %w", err)
	}

	// Color scale per coverage level.
	levelColors := map[CoverageLevel]string{
		CoverageLevelNone:      "",
		CoverageLevelAttempted: "#ffd966",
		CoverageLevelExecuted:  "#6aa84f",
		CoverageLevelValidated: "#274e13",
	}

	techniques := make([]navigatorTechnique, 0)
	for _, tac := range coverage.Tactics {
		for _, tc := range tac.Techniques {
			nt := navigatorTechnique{
				TechniqueID: tc.AttackID,
				Score:       int(tc.Level),
				Color:       levelColors[tc.Level],
				Enabled:     tc.Level > 0,
			}
			techniques = append(techniques, nt)
		}
	}

	return NavigatorLayer{
		Name: name,
		Versions: navigatorVersions{
			Attack:    "14",
			Navigator: "4.9",
			Layer:     "4.5",
		},
		Domain:      "enterprise-attack",
		Description: fmt.Sprintf("RInfra coverage export for engagement %s", engagementID),
		Techniques:  techniques,
		Gradient: navigatorGradient{
			Colors:   []string{"#ffffff", "#ffd966", "#6aa84f", "#274e13"},
			MinValue: 0,
			MaxValue: 3,
		},
	}, nil
}
