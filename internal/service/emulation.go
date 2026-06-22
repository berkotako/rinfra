package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/rinfra/rinfra/internal/audit"
	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/emulation"
	"github.com/rinfra/rinfra/internal/emulation/catalog"
	"github.com/rinfra/rinfra/internal/emulation/index"
	"github.com/rinfra/rinfra/internal/store"
)

// ImportIndex parses an SRA-format benchmark index YAML, persists its techniques
// into the TTP library (duplicates by ATT&CK id are skipped), creates a scenario
// for the index, and audits the import. Requires authoring stores.
func (s *EmulationService) ImportIndex(ctx context.Context, data []byte, actor string) (domain.Scenario, error) {
	if s.userScenarios == nil || s.userTechniques == nil {
		return domain.Scenario{}, fmt.Errorf("index import not enabled")
	}
	sc, techs, err := index.Parse(data)
	if err != nil {
		return domain.Scenario{}, err
	}
	added := 0
	for _, t := range techs {
		if err := s.userTechniques.Create(ctx, t); err == nil {
			added++ // duplicates (existing ATT&CK id) are skipped, not fatal
		}
	}
	id, err := s.userScenarios.Create(ctx, sc)
	if err != nil {
		return domain.Scenario{}, fmt.Errorf("persist imported scenario: %w", err)
	}
	sc.ID = id
	_ = s.audit.Record(ctx, audit.Event{
		Actor:  actor,
		Action: "index.import",
		Target: id,
		Detail: fmt.Sprintf("name=%q techniques=%d new_ttps=%d", sc.Name, len(techs), added),
		At:     time.Now().UTC(),
	})
	return sc, nil
}

// selectInScopeSession returns the id of the first active operator session whose
// host is within the engagement scope (enforcing scope at execution time). It
// returns ok=false when sessions exist but none are in scope, so the caller can
// refuse to execute. When sessions can't be enumerated it falls back to the
// resolver-provided session id to preserve existing behavior.
func selectInScopeSession(ctx context.Context, eng *domain.Engagement, op c2.Operator, fallback string) (string, bool) {
	sessions, err := op.Sessions(ctx)
	if err != nil || len(sessions) == 0 {
		return fallback, true
	}
	for _, sess := range sessions {
		if sess.Host == "" || eng.TargetInScope(sess.Host) {
			return sess.ID, true
		}
	}
	return "", false
}

// EmulationService runs adversary-emulation scenarios and tracks their results.
type EmulationService struct {
	engagements    store.EngagementStore
	scenarios      store.ScenarioStore
	userScenarios  store.UserScenarioStore  // operator-authored scenarios; may be nil
	userTechniques store.UserTechniqueStore // operator-authored TTPs; may be nil
	audit          audit.Logger
	orch           *emulation.Orchestrator
	hub            *Hub
	resolver       OperatorResolver
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

// WithUserScenarios injects the store for operator-authored scenarios. When
// unset, CreateScenario is unavailable and only the built-in catalog is listed.
func (s *EmulationService) WithUserScenarios(store store.UserScenarioStore) {
	s.userScenarios = store
}

// WithUserTechniques injects the store for operator-authored TTPs.
func (s *EmulationService) WithUserTechniques(store store.UserTechniqueStore) {
	s.userTechniques = store
}

// ListTechniques returns operator-authored TTPs (the built-in library ships with
// the web client). Returns an empty slice when authoring is not enabled.
func (s *EmulationService) ListTechniques(ctx context.Context) ([]domain.Technique, error) {
	if s.userTechniques == nil {
		return nil, nil
	}
	return s.userTechniques.List(ctx)
}

func validTechnique(t domain.Technique) error {
	if t.AttackID == "" {
		return fmt.Errorf("technique attackID is required")
	}
	if t.Name == "" {
		return fmt.Errorf("technique name is required")
	}
	if t.Tactic == "" {
		return fmt.Errorf("technique tactic is required")
	}
	return nil
}

// CreateTechnique persists an operator-authored TTP.
func (s *EmulationService) CreateTechnique(ctx context.Context, t domain.Technique, actor string) (domain.Technique, error) {
	if s.userTechniques == nil {
		return domain.Technique{}, fmt.Errorf("technique authoring not enabled")
	}
	if err := validTechnique(t); err != nil {
		return domain.Technique{}, err
	}
	if t.Source == "" {
		t.Source = domain.SourceAtomicRedTeam
	}
	if err := s.userTechniques.Create(ctx, t); err != nil {
		return domain.Technique{}, err
	}
	_ = s.audit.Record(ctx, audit.Event{
		Actor:  actor,
		Action: "ttp.create",
		Target: t.AttackID,
		Detail: fmt.Sprintf("name=%q tactic=%s", t.Name, t.Tactic),
		At:     time.Now().UTC(),
	})
	return t, nil
}

// UpdateTechnique replaces an operator-authored TTP.
func (s *EmulationService) UpdateTechnique(ctx context.Context, t domain.Technique, actor string) (domain.Technique, error) {
	if s.userTechniques == nil {
		return domain.Technique{}, fmt.Errorf("technique authoring not enabled")
	}
	if err := validTechnique(t); err != nil {
		return domain.Technique{}, err
	}
	if err := s.userTechniques.Update(ctx, t); err != nil {
		return domain.Technique{}, err
	}
	_ = s.audit.Record(ctx, audit.Event{
		Actor:  actor,
		Action: "ttp.update",
		Target: t.AttackID,
		Detail: fmt.Sprintf("name=%q tactic=%s", t.Name, t.Tactic),
		At:     time.Now().UTC(),
	})
	return t, nil
}

// DeleteTechnique removes an operator-authored TTP.
func (s *EmulationService) DeleteTechnique(ctx context.Context, attackID, actor string) error {
	if s.userTechniques == nil {
		return fmt.Errorf("technique authoring not enabled")
	}
	if err := s.userTechniques.Delete(ctx, attackID); err != nil {
		return err
	}
	_ = s.audit.Record(ctx, audit.Event{
		Actor:  actor,
		Action: "ttp.delete",
		Target: attackID,
		At:     time.Now().UTC(),
	})
	return nil
}

// ListScenarios returns the built-in catalog plus any operator-authored
// scenarios, the latter newest-first after the catalog.
func (s *EmulationService) ListScenarios() []domain.Scenario {
	out := catalog.List()
	if s.userScenarios != nil {
		if custom, err := s.userScenarios.List(context.Background()); err == nil {
			out = append(out, custom...)
		}
	}
	return out
}

// CreateScenario persists an operator-authored scenario and returns it with its
// generated ID. Validation rejects an empty name or an empty technique list.
func (s *EmulationService) CreateScenario(ctx context.Context, sc domain.Scenario, actor string) (domain.Scenario, error) {
	if s.userScenarios == nil {
		return domain.Scenario{}, fmt.Errorf("scenario authoring not enabled")
	}
	if sc.Name == "" {
		return domain.Scenario{}, fmt.Errorf("scenario name is required")
	}
	if len(sc.Techniques) == 0 {
		return domain.Scenario{}, fmt.Errorf("scenario must include at least one technique")
	}
	sc.CreatedAt = time.Now().UTC()
	id, err := s.userScenarios.Create(ctx, sc)
	if err != nil {
		return domain.Scenario{}, fmt.Errorf("persist scenario: %w", err)
	}
	sc.ID = id

	_ = s.audit.Record(ctx, audit.Event{
		Actor:  actor,
		Action: "scenario.create",
		Target: id,
		Detail: fmt.Sprintf("name=%q techniques=%d", sc.Name, len(sc.Techniques)),
		At:     time.Now().UTC(),
	})
	return sc, nil
}

// UpdateScenario replaces an operator-authored scenario. Catalog scenarios are
// immutable (code-shipped), so only user-store scenarios can be edited.
func (s *EmulationService) UpdateScenario(ctx context.Context, sc domain.Scenario, actor string) (domain.Scenario, error) {
	if s.userScenarios == nil {
		return domain.Scenario{}, fmt.Errorf("scenario authoring not enabled")
	}
	if sc.ID == "" {
		return domain.Scenario{}, fmt.Errorf("scenario id is required")
	}
	if _, builtin := catalog.Get(sc.ID); builtin {
		return domain.Scenario{}, fmt.Errorf("built-in scenarios cannot be edited")
	}
	if sc.Name == "" {
		return domain.Scenario{}, fmt.Errorf("scenario name is required")
	}
	if len(sc.Techniques) == 0 {
		return domain.Scenario{}, fmt.Errorf("scenario must include at least one technique")
	}
	if err := s.userScenarios.Update(ctx, sc); err != nil {
		return domain.Scenario{}, err
	}
	_ = s.audit.Record(ctx, audit.Event{
		Actor:  actor,
		Action: "scenario.update",
		Target: sc.ID,
		Detail: fmt.Sprintf("name=%q techniques=%d", sc.Name, len(sc.Techniques)),
		At:     time.Now().UTC(),
	})
	return s.userScenarios.Get(ctx, sc.ID)
}

// DeleteScenario removes an operator-authored scenario.
func (s *EmulationService) DeleteScenario(ctx context.Context, id, actor string) error {
	if s.userScenarios == nil {
		return fmt.Errorf("scenario authoring not enabled")
	}
	if _, builtin := catalog.Get(id); builtin {
		return fmt.Errorf("built-in scenarios cannot be deleted")
	}
	if err := s.userScenarios.Delete(ctx, id); err != nil {
		return err
	}
	_ = s.audit.Record(ctx, audit.Event{
		Actor:  actor,
		Action: "scenario.delete",
		Target: id,
		At:     time.Now().UTC(),
	})
	return nil
}

// lookupScenario resolves a scenario by ID from the catalog, then the
// operator-authored store.
func (s *EmulationService) lookupScenario(ctx context.Context, id string) (domain.Scenario, bool) {
	if sc, ok := catalog.Get(id); ok {
		return sc, true
	}
	if s.userScenarios != nil {
		if sc, err := s.userScenarios.Get(ctx, id); err == nil {
			return sc, true
		}
	}
	return domain.Scenario{}, false
}

// Start begins running a scenario against an engagement. It gates on CanDeploy,
// persists the initial ScenarioRun, then runs async, publishing SSE events as
// techniques complete.
func (s *EmulationService) Start(ctx context.Context, engagementID, scenarioID, actor string) (string, error) {
	eng, err := s.engagements.Get(ctx, engagementID)
	if err != nil {
		return "", err
	}
	sc, ok := s.lookupScenario(ctx, scenarioID)
	if !ok {
		return "", fmt.Errorf("scenario %q: %w", scenarioID, store.ErrNotFound)
	}
	return s.launchRun(ctx, eng, sc, actor)
}

// launchRun enforces the per-engagement authorization gate, persists a running
// ScenarioRun, audits the start, and kicks off the async execution. It is the
// shared core of both single-engagement (Start) and project-scope
// (StartProjectRun) launches; the scenario is assumed already resolved.
func (s *EmulationService) launchRun(ctx context.Context, eng domain.Engagement, sc domain.Scenario, actor string) (string, error) {
	if err := eng.CanDeploy(time.Now()); err != nil {
		return "", fmt.Errorf("emulation start refused: %w", err)
	}

	run := domain.ScenarioRun{
		EngagementID: eng.ID,
		ScenarioID:   sc.ID,
		Status:       domain.ExecRunning,
		StartedAt:    time.Now().UTC(),
	}
	runID, err := s.scenarios.SaveRun(ctx, run)
	if err != nil {
		return "", fmt.Errorf("persist scenario run: %w", err)
	}
	run.ID = runID

	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: eng.ID,
		Actor:        actor,
		Action:       "emulation.start",
		Target:       sc.ID,
		Detail:       fmt.Sprintf("run_id=%s", runID),
		At:           time.Now().UTC(),
	})

	go s.runScenario(context.Background(), eng, sc, run, actor)

	return runID, nil
}

// ProjectRunResult reports the outcome of launching a scenario across every
// engagement in a project: which engagements started a run, and which were
// skipped (with the reason — typically a failed authorization gate).
type ProjectRunResult struct {
	ProjectID  string                 `json:"projectId"`
	ScenarioID string                 `json:"scenarioId"`
	Started    []ProjectRunEngagement `json:"started"`
	Skipped    []ProjectRunSkip       `json:"skipped"`
}

type ProjectRunEngagement struct {
	EngagementID string `json:"engagementId"`
	RunID        string `json:"runId"`
}

type ProjectRunSkip struct {
	EngagementID string `json:"engagementId"`
	Reason       string `json:"reason"`
}

// StartProjectRun launches a scenario at project scope: it fans the run out to
// every engagement in the project, each gated independently by its own
// CanDeploy. Engagements that fail the gate (not authorized, out of window, no
// scope, …) are reported as skipped rather than failing the whole batch, so a
// partially-authorized project still exercises the engagements that are ready.
func (s *EmulationService) StartProjectRun(ctx context.Context, projectID, scenarioID, actor string) (ProjectRunResult, error) {
	sc, ok := s.lookupScenario(ctx, scenarioID)
	if !ok {
		return ProjectRunResult{}, fmt.Errorf("scenario %q: %w", scenarioID, store.ErrNotFound)
	}
	engs, err := s.engagements.ListForProject(ctx, projectID)
	if err != nil {
		return ProjectRunResult{}, fmt.Errorf("list project engagements: %w", err)
	}

	res := ProjectRunResult{ProjectID: projectID, ScenarioID: scenarioID}
	for _, eng := range engs {
		runID, err := s.launchRun(ctx, eng, sc, actor)
		if err != nil {
			res.Skipped = append(res.Skipped, ProjectRunSkip{EngagementID: eng.ID, Reason: err.Error()})
			continue
		}
		res.Started = append(res.Started, ProjectRunEngagement{EngagementID: eng.ID, RunID: runID})
	}

	_ = s.audit.Record(ctx, audit.Event{
		EngagementID: "",
		Actor:        actor,
		Action:       "emulation.project_start",
		Target:       scenarioID,
		Detail:       fmt.Sprintf("project=%s started=%d skipped=%d", projectID, len(res.Started), len(res.Skipped)),
		At:           time.Now().UTC(),
	})

	return res, nil
}

// runScenario resolves the Operator, executes the scenario, and persists results.
func (s *EmulationService) runScenario(ctx context.Context, eng domain.Engagement, sc domain.Scenario, run domain.ScenarioRun, actor string) {
	var result *domain.ScenarioRun
	var err error

	// Capability routing: when the resolver can enumerate the engagement's
	// deployed frameworks, route each technique to the best operator/session
	// across them (platform/scope/privilege/capability aware) instead of pinning
	// the whole run to "the first live C2 server".
	if cr, ok := s.resolver.(CandidateResolver); ok {
		if cands := cr.Candidates(ctx, eng); len(cands) > 0 {
			result, err = s.orchRunRouted(ctx, &eng, sc, cands, run.ID)
		}
	}

	if result == nil && err == nil {
		// Legacy single-operator path (dev/test resolvers, or no deployed
		// frameworks to route across). Resolve one operator; nil → manual_required.
		op, sessionID, _ := s.resolver.Resolve(ctx, eng)
		noOpStatus := domain.ExecManualRequired

		// Enforce scope at execution time: pick the first in-scope session. If
		// sessions exist but none are in scope, refuse — record blocked_by_scope.
		if op != nil {
			sid, ok := selectInScopeSession(ctx, &eng, op, sessionID)
			if !ok {
				_ = s.audit.Record(ctx, audit.Event{
					EngagementID: eng.ID,
					Actor:        actor,
					Action:       "emulation.scope_block",
					Target:       sc.ID,
					Detail:       "no in-scope agent session; execution refused",
					At:           time.Now().UTC(),
				})
				op = nil
				noOpStatus = domain.ExecBlockedByScope
			} else {
				sessionID = sid
			}
		}
		result, err = s.orchRunWithHooks(ctx, &eng, sc, sessionID, op, run.ID, noOpStatus)
	}
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
	// noOpStatus is the status recorded for every technique when op is nil. It
	// distinguishes a Fronted-tier manual run (ExecManualRequired) from a
	// scope-refused run (ExecBlockedByScope) so reporting can tell them apart.
	noOpStatus domain.ExecutionStatus,
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
			// No operator: record the specific reason (manual vs scope-blocked)
			// rather than a generic skip, so coverage does not count it as an
			// attempt.
			res = domain.Result{
				TechniqueAttackID: t.AttackID,
				Status:            noOpStatus,
				Output:            noOpMessage(noOpStatus),
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

		s.recordResult(ctx, run, res)
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

// RecordDetection sets the defender outcome (block/detect/alert/none) for a
// technique within a run — the purple-team scoring step that feeds the TRM.
func (s *EmulationService) RecordDetection(ctx context.Context, runID, attackID string, outcome domain.DetectionOutcome, actor string) error {
	switch outcome {
	case domain.DetectNone, domain.DetectAlerted, domain.DetectDetected, domain.DetectBlocked:
	default:
		return fmt.Errorf("invalid detection outcome %q", outcome)
	}
	if err := s.scenarios.SetResultDetection(ctx, runID, attackID, outcome); err != nil {
		return err
	}
	_ = s.audit.Record(ctx, audit.Event{
		Actor:  actor,
		Action: "emulation.detection",
		Target: runID,
		Detail: fmt.Sprintf("technique=%s outcome=%s", attackID, outcome),
		At:     time.Now().UTC(),
	})
	return nil
}

// ListRuns returns all runs for an engagement.
func (s *EmulationService) ListRuns(ctx context.Context, engagementID string) ([]domain.ScenarioRun, error) {
	return s.scenarios.RunsForEngagement(ctx, engagementID)
}

// summarizeResults rolls per-technique results up to a run-level status.
// noOpMessage returns the per-technique Output explaining why a technique was
// not executed by an operator.
func noOpMessage(status domain.ExecutionStatus) string {
	switch status {
	case domain.ExecBlockedByScope:
		return "no in-scope agent session; execution refused (run manually if in scope)"
	case domain.ExecUnsupported:
		return "no deployed C2 framework can automate this technique on an available agent; run manually"
	default: // ExecManualRequired
		return "no operator API (fronted-tier framework); run manually"
	}
}

// recordResult persists a technique result, publishes the per-technique SSE
// event, and appends it to the run.
func (s *EmulationService) recordResult(ctx context.Context, run *domain.ScenarioRun, res domain.Result) {
	_ = s.scenarios.SaveResult(ctx, run.ID, res)
	s.hub.Publish(Event{Kind: EventRunStatus, EngagementID: run.EngagementID, Data: map[string]any{
		"runId":       run.ID,
		"techniqueId": res.TechniqueAttackID,
		"status":      string(res.Status),
	}})
	run.Results = append(run.Results, res)
}

// orchRunRouted drives the scenario with capability routing: each technique is
// routed to the best operator/session across all deployed frameworks (matching
// required platform, scope, privilege, and framework capability), or recorded
// with the precise non-attempt status (unsupported / manual_required /
// blocked_by_scope) when none fits.
func (s *EmulationService) orchRunRouted(ctx context.Context, eng *domain.Engagement, sc domain.Scenario, cands []Candidate, runID string) (*domain.ScenarioRun, error) {
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
	for _, t := range sc.Techniques {
		op, sid, disp, detail := Route(eng, t, cands)
		var res domain.Result
		if op != nil {
			var err error
			res, err = op.Execute(ctx, sid, t)
			if err != nil {
				res = domain.Result{
					TechniqueAttackID: t.AttackID,
					Status:            domain.ExecFailed,
					StartedAt:         time.Now(),
					FinishedAt:        time.Now(),
					Err:               err.Error(),
				}
			}
		} else {
			// No operator selected — record the precise reason. For an infra
			// failure (ExecFailed) the detail carries the operator error so it
			// surfaces instead of being masked as a scope refusal.
			res = domain.Result{
				TechniqueAttackID: t.AttackID,
				Status:            disp,
				Output:            noOpMessage(disp),
				StartedAt:         time.Now(),
				FinishedAt:        time.Now(),
			}
			if detail != "" {
				res.Output = detail
				if disp == domain.ExecFailed {
					res.Err = detail
				}
			}
		}
		s.recordResult(ctx, run, res)
	}
	run.FinishedAt = time.Now()
	run.Status = summarizeResults(run.Results)
	return run, nil
}

func summarizeResults(results []domain.Result) domain.ExecutionStatus {
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
		// No genuine attempt in the run — surface the actual reason (manual,
		// blocked, etc.) rather than a generic skip.
		return results[0].Status
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

// Techniquecoverage holds the coverage data for one technique. Level drives the
// heatmap (0-3); Disposition carries the finer BAS taxonomy so the UI can show
// WHY a technique is at level 0 (manual vs blocked vs not-run) rather than
// lumping them together.
type Techniquecoverage struct {
	AttackID    string        `json:"attackID"`
	Name        string        `json:"name"`
	Tactic      string        `json:"tactic"`
	Level       CoverageLevel `json:"level"`
	Disposition Disposition   `json:"disposition"`
}

// TacticCoverage holds all technique coverages for one tactic.
type TacticCoverage struct {
	Tactic     string              `json:"tactic"`
	Techniques []Techniquecoverage `json:"techniques"`
}

// Coverage is the full ATT&CK coverage rollup for an engagement.
//
// "Attempted/exercised" counts ONLY genuine execution attempts (executed +
// attempted-failed). Manual, unsupported, scope-blocked, policy-skipped, and
// not-run techniques are reported in their own buckets and excluded from both
// the attempted total and the TRM denominator, so the dashboard cannot look
// more complete than reality.
type Coverage struct {
	EngagementID string           `json:"engagementId"`
	Tactics      []TacticCoverage `json:"tactics"`
	// Summary fields for the dashboard cards.
	TotalTechniques int `json:"totalTechniques"`
	ExercisedCount  int `json:"exercisedCount"` // genuine attempts (executed + attempted-failed)
	ExecutedCount   int `json:"executedCount"`  // ran on an agent (>= level 2)
	ValidatedCount  int `json:"validatedCount"` // executed AND blocked/detected/alerted
	// Non-attempt buckets — reported separately, never folded into "attempted".
	ManualCount        int `json:"manualCount"`        // manual_required (Fronted-tier)
	UnsupportedCount   int `json:"unsupportedCount"`   // deployed framework can't translate
	BlockedScopeCount  int `json:"blockedScopeCount"`  // refused: no in-scope agent
	SkippedPolicyCount int `json:"skippedPolicyCount"` // skipped by policy
	NotRunCount        int `json:"notRunCount"`        // never reached
	// Validation breakdown (defender outcome on executed techniques).
	ValidatedDetectedCount int `json:"validatedDetectedCount"` // detected/alerted
	ValidatedBlockedCount  int `json:"validatedBlockedCount"`  // blocked outright
	// TRM (Threat Resilience Metric): % of ATTEMPTED techniques the defenders
	// passed. Denominator excludes manual/skipped/blocked/not-run.
	TRM int `json:"trm"`
}

// GetCoverage rolls up all runs for an engagement into a Coverage report.
func (s *EmulationService) GetCoverage(ctx context.Context, engagementID string) (Coverage, error) {
	runs, err := s.scenarios.RunsForEngagement(ctx, engagementID)
	if err != nil {
		return Coverage{}, fmt.Errorf("list runs: %w", err)
	}
	return s.coverageFromRuns(ctx, engagementID, runs)
}

// GetProjectCoverage rolls up ATT&CK coverage across every engagement in the
// project — the project-scope counterpart to GetCoverage, so emulation results
// can be viewed at both the engagement and project levels.
func (s *EmulationService) GetProjectCoverage(ctx context.Context, projectID string) (Coverage, error) {
	engs, err := s.engagements.ListForProject(ctx, projectID)
	if err != nil {
		return Coverage{}, fmt.Errorf("list project engagements: %w", err)
	}
	var runs []domain.ScenarioRun
	for _, e := range engs {
		rs, err := s.scenarios.RunsForEngagement(ctx, e.ID)
		if err != nil {
			continue
		}
		runs = append(runs, rs...)
	}
	return s.coverageFromRuns(ctx, projectID, runs)
}

// coverageFromRuns rolls a set of runs (engagement- or project-scoped) up into a
// Coverage report. scopeID is echoed back as the report's identifier.
func (s *EmulationService) coverageFromRuns(ctx context.Context, scopeID string, runs []domain.ScenarioRun) (Coverage, error) {
	// Collect full results: for each run, fetch from store to get technique_results.
	// (RunsForEngagement may not load results — call GetRun for each.) Track the
	// best (highest-ranked) disposition per technique across all runs.
	dispos := make(map[string]Disposition) // attackID → best disposition
	names := make(map[string]string)
	tactics := make(map[string]string)

	for _, run := range runs {
		full, err := s.scenarios.GetRun(ctx, run.ID)
		if err != nil {
			continue
		}
		for _, r := range full.Results {
			d := dispositionFor(r)
			if existing, ok := dispos[r.TechniqueAttackID]; !ok || dispositionRank(d) > dispositionRank(existing) {
				dispos[r.TechniqueAttackID] = d
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
			disp, ok := dispos[t.AttackID]
			if !ok {
				disp = DispNotRun // default: never reached
			}
			lvl := levelFromDisposition(disp)
			tc := Techniquecoverage{
				AttackID:    t.AttackID,
				Name:        t.Name,
				Tactic:      t.Tactic,
				Level:       lvl,
				Disposition: disp,
			}
			// Deduplicate: if already added for this tactic with the same ID, keep best.
			tacs := tacticTechs[t.Tactic]
			found := false
			for i, existing := range tacs {
				if existing.AttackID == t.AttackID {
					if dispositionRank(disp) > dispositionRank(existing.Disposition) {
						tacs[i].Level = lvl
						tacs[i].Disposition = disp
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

	// Summary counts, bucketed by disposition. "exercised" counts ONLY genuine
	// attempts so the TRM denominator and the dashboard cannot overstate reality.
	var cov Coverage
	cov.EngagementID = scopeID
	cov.Tactics = tacticList
	for _, tac := range tacticList {
		for _, tc := range tac.Techniques {
			cov.TotalTechniques++
			switch tc.Disposition {
			case DispValidatedBlocked:
				cov.ExercisedCount++
				cov.ExecutedCount++
				cov.ValidatedCount++
				cov.ValidatedBlockedCount++
			case DispValidatedDetected:
				cov.ExercisedCount++
				cov.ExecutedCount++
				cov.ValidatedCount++
				cov.ValidatedDetectedCount++
			case DispExecuted:
				cov.ExercisedCount++
				cov.ExecutedCount++
			case DispAttemptedFailed:
				cov.ExercisedCount++
			case DispManualRequired:
				cov.ManualCount++
			case DispUnsupported:
				cov.UnsupportedCount++
			case DispBlockedByScope:
				cov.BlockedScopeCount++
			case DispSkippedPolicy:
				cov.SkippedPolicyCount++
			default: // DispNotRun
				cov.NotRunCount++
			}
		}
	}

	_ = names
	_ = tactics

	if cov.ExercisedCount > 0 {
		cov.TRM = int(float64(cov.ValidatedCount) / float64(cov.ExercisedCount) * 100)
	}
	return cov, nil
}

// Disposition is the per-technique BAS outcome taxonomy. It keeps manual,
// unsupported, scope-blocked, policy-skipped, and not-run techniques distinct
// from genuine execution attempts so coverage reporting is honest. The
// validated_* dispositions layer the defender outcome (DetectionOutcome) on top
// of a successful execution; full detection validation is the deferred phase
// (see CLAUDE.md) — these are the seam.
type Disposition string

const (
	DispNotRun            Disposition = "not_run"
	DispSkippedPolicy     Disposition = "skipped_policy"
	DispBlockedByScope    Disposition = "blocked_by_scope"
	DispUnsupported       Disposition = "unsupported"
	DispManualRequired    Disposition = "manual_required"
	DispAttemptedFailed   Disposition = "attempted_failed"
	DispExecuted          Disposition = "executed"
	DispValidatedDetected Disposition = "validated_detected"
	DispValidatedBlocked  Disposition = "validated_blocked"
)

// dispositionRank orders dispositions so the best outcome across multiple runs
// of the same technique wins the rollup (a later validated run outranks an
// earlier manual/skipped one).
func dispositionRank(d Disposition) int {
	switch d {
	case DispValidatedBlocked:
		return 8
	case DispValidatedDetected:
		return 7
	case DispExecuted:
		return 6
	case DispAttemptedFailed:
		return 5
	case DispManualRequired:
		return 4
	case DispUnsupported:
		return 3
	case DispBlockedByScope:
		return 2
	case DispSkippedPolicy:
		return 1
	default: // DispNotRun
		return 0
	}
}

// dispositionFor maps a stored Result to its Disposition.
func dispositionFor(r domain.Result) Disposition {
	switch r.Status {
	case domain.ExecSuccess:
		switch r.Detection {
		case domain.DetectBlocked:
			return DispValidatedBlocked
		case domain.DetectDetected, domain.DetectAlerted:
			return DispValidatedDetected
		default:
			return DispExecuted
		}
	case domain.ExecFailed:
		return DispAttemptedFailed
	case domain.ExecManualRequired:
		return DispManualRequired
	case domain.ExecUnsupported:
		return DispUnsupported
	case domain.ExecBlockedByScope:
		return DispBlockedByScope
	case domain.ExecSkippedPolicy:
		return DispSkippedPolicy
	case domain.ExecSkipped:
		// Legacy generic skip from older runs: a human likely must run it, but
		// it is never counted as an attempt.
		return DispManualRequired
	default: // pending/running/unknown
		return DispNotRun
	}
}

// levelFromDisposition derives the heatmap coverage level (0-3) from a
// disposition. Only real attempts reach level >= 1.
func levelFromDisposition(d Disposition) CoverageLevel {
	switch d {
	case DispValidatedBlocked, DispValidatedDetected:
		return CoverageLevelValidated
	case DispExecuted:
		return CoverageLevelExecuted
	case DispAttemptedFailed:
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
