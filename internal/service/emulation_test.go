package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/service"
	"github.com/rinfra/rinfra/internal/store"
	"github.com/rinfra/rinfra/internal/store/memstore"
)

// ---------- Emulation service tests ----------

// TestEmulationStart_FakeResolver verifies the full emulation lifecycle:
//   - Start persists initial ScenarioRun (status=running)
//   - Each technique result is saved incrementally (SaveResult path)
//   - GetRun returns the completed run with all technique results
//   - Appropriate audit events are emitted
//   - SSE events include per-technique run_status events
func TestEmulationStart_FakeResolver(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()

	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)

	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, hub)
	svcEmu.WithResolver(service.NewFakeResolver())

	// Subscribe to SSE events before starting the run.
	ch, unsub := hub.Subscribe(eng.ID)
	defer unsub()

	// Start the run with the "apt29" scenario.
	runID, err := svcEmu.Start(ctx, eng.ID, "apt29", "test-op")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if runID == "" {
		t.Fatal("expected non-empty runID")
	}

	// Collect SSE events for up to 3 seconds.
	var events []service.Event
	deadline := time.Now().Add(3 * time.Second)
	func() {
		for time.Now().Before(deadline) {
			select {
			case ev, ok := <-ch:
				if !ok {
					return
				}
				events = append(events, ev)
				// Once we see a non-per-technique run_status (final status), stop.
				if data, ok := ev.Data.(map[string]any); ok {
					if _, hasTech := data["techniqueId"]; !hasTech {
						return
					}
				}
			default:
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	// Wait for the async goroutine to complete and persist.
	deadline2 := time.Now().Add(3 * time.Second)
	var run domain.ScenarioRun
	for time.Now().Before(deadline2) {
		run, err = svcEmu.GetRun(ctx, runID)
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if run.Status != domain.ExecRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Verify run is complete.
	if run.Status == domain.ExecRunning {
		t.Errorf("run still running after timeout")
	}
	if run.Status != domain.ExecSuccess {
		t.Errorf("run status: want success, got %s", run.Status)
	}

	// Verify technique results are persisted.
	if len(run.Results) == 0 {
		t.Error("expected technique results in completed run")
	}
	for _, r := range run.Results {
		if r.TechniqueAttackID == "" {
			t.Error("technique result: empty AttackID")
		}
		if r.Status != domain.ExecSuccess {
			t.Errorf("technique %s: want success, got %s", r.TechniqueAttackID, r.Status)
		}
	}

	// Verify SSE events were published.
	if len(events) == 0 {
		t.Error("expected SSE events, got none")
	}
	hasTechniqueEvent := false
	for _, ev := range events {
		if ev.Kind != service.EventRunStatus {
			continue
		}
		data, ok := ev.Data.(map[string]any)
		if !ok {
			continue
		}
		if _, hasTech := data["techniqueId"]; hasTech {
			hasTechniqueEvent = true
		}
	}
	if !hasTechniqueEvent {
		t.Error("expected at least one per-technique SSE event (with techniqueId)")
	}

	// Verify audit events.
	if !hasAuditAction(s.audit, "emulation.start", eng.ID) {
		t.Error("expected emulation.start audit event")
	}
}

// TestEmulationStart_DraftEngagementBlocked verifies that CanDeploy gate
// blocks emulation on a draft (unauthorized) engagement.
func TestEmulationStart_DraftEngagementBlocked(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()

	svcEng := service.NewEngagementService(s.eng, s.audit)

	// Create a draft engagement (not authorized).
	draft, err := svcEng.Create(ctx, domain.Engagement{
		Client:         "Draft Co",
		Codename:       "DRAFT-EMU",
		Status:         domain.EngagementDraft,
		Scope:          domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
		EngagementType: domain.EngagementTypeRedTeam,
	}, "op1")
	if err != nil {
		t.Fatalf("create draft engagement: %v", err)
	}

	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, hub)
	svcEmu.WithResolver(service.NewFakeResolver())

	_, err = svcEmu.Start(ctx, draft.ID, "apt29", "op1")
	if err == nil {
		t.Fatal("expected error starting emulation on draft engagement, got nil")
	}
	if !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("want ErrNotAuthorized, got %v", err)
	}
}

// TestEmulationStart_FrontedTier verifies that a Fronted-tier resolver causes
// all techniques to be recorded as Skipped.
func TestEmulationStart_FrontedTier(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()

	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)

	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, hub)
	svcEmu.WithResolver(service.NewFrontedResolver())

	runID, err := svcEmu.Start(ctx, eng.ID, "apt29", "op1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for completion.
	var run domain.ScenarioRun
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, _ = svcEmu.GetRun(ctx, runID)
		if run.Status != domain.ExecRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Fronted-tier: every technique is manual_required (a human runs it), and the
	// run summarizes to the same — NOT counted as an execution attempt.
	if run.Status != domain.ExecManualRequired {
		t.Errorf("fronted tier run status: want manual_required, got %s", run.Status)
	}
	for _, r := range run.Results {
		if r.Status != domain.ExecManualRequired {
			t.Errorf("technique %s: want manual_required, got %s", r.TechniqueAttackID, r.Status)
		}
	}
}

// TestEmulationStart_UnknownScenario returns ErrNotFound for an unknown scenario.
func TestEmulationStart_UnknownScenario(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()

	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)

	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, hub)
	svcEmu.WithResolver(service.NewFakeResolver())

	_, err := svcEmu.Start(ctx, eng.ID, "nonexistent-scenario", "op1")
	if err == nil {
		t.Fatal("expected error for unknown scenario, got nil")
	}
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("want store.ErrNotFound, got %v", err)
	}
}

// TestGetRun_PersistenceRoundTrip ensures that per-technique results are
// persisted by SaveResult and returned by GetRun.
func TestGetRun_PersistenceRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()

	// Directly exercise the store.
	runID, err := s.scenario.SaveRun(ctx, domain.ScenarioRun{
		EngagementID: "eng-1",
		ScenarioID:   "apt29",
		Status:       domain.ExecRunning,
		StartedAt:    time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	results := []domain.Result{
		{TechniqueAttackID: "T1566.002", Status: domain.ExecSuccess, Output: "ok", StartedAt: time.Now(), FinishedAt: time.Now()},
		{TechniqueAttackID: "T1059.001", Status: domain.ExecFailed, Output: "", Err: "failed", StartedAt: time.Now(), FinishedAt: time.Now()},
	}
	for _, r := range results {
		if err := s.scenario.SaveResult(ctx, runID, r); err != nil {
			t.Fatalf("SaveResult: %v", err)
		}
	}

	// Update run status.
	_, err = s.scenario.SaveRun(ctx, domain.ScenarioRun{
		ID:         runID,
		Status:     domain.ExecFailed,
		FinishedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("SaveRun (update): %v", err)
	}

	// Retrieve and verify.
	run, err := s.scenario.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != domain.ExecFailed {
		t.Errorf("run status: want failed, got %s", run.Status)
	}
	if len(run.Results) != 2 {
		t.Errorf("expected 2 technique results, got %d", len(run.Results))
	}
	// Verify the right statuses.
	found := map[string]domain.ExecutionStatus{}
	for _, r := range run.Results {
		found[r.TechniqueAttackID] = r.Status
	}
	if found["T1566.002"] != domain.ExecSuccess {
		t.Errorf("T1566.002: want success, got %s", found["T1566.002"])
	}
	if found["T1059.001"] != domain.ExecFailed {
		t.Errorf("T1059.001: want failed, got %s", found["T1059.001"])
	}
}

// ---------- Coverage rollup tests ----------

// TestGetCoverage_Rollup seeds two runs with known results and verifies the
// coverage levels are correctly computed.
func TestGetCoverage_Rollup(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()

	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)

	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, hub)

	// Seed a run with known results.
	runID, err := s.scenario.SaveRun(ctx, domain.ScenarioRun{
		EngagementID: eng.ID,
		ScenarioID:   "apt29",
		Status:       domain.ExecSuccess,
		StartedAt:    time.Now().UTC(),
		FinishedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	// T1566.002 → Success            (level 2 Executed, an attempt)
	// T1059.001 → Failed             (level 1 Attempted, an attempt)
	// T1055     → ManualRequired     (level 0, NOT an attempt — reported separately)
	seedResults := []domain.Result{
		{TechniqueAttackID: "T1566.002", Status: domain.ExecSuccess, StartedAt: time.Now(), FinishedAt: time.Now()},
		{TechniqueAttackID: "T1059.001", Status: domain.ExecFailed, StartedAt: time.Now(), FinishedAt: time.Now()},
		{TechniqueAttackID: "T1055", Status: domain.ExecManualRequired, StartedAt: time.Now(), FinishedAt: time.Now()},
	}
	for _, r := range seedResults {
		if err := s.scenario.SaveResult(ctx, runID, r); err != nil {
			t.Fatalf("SaveResult: %v", err)
		}
	}

	coverage, err := svcEmu.GetCoverage(ctx, eng.ID)
	if err != nil {
		t.Fatalf("GetCoverage: %v", err)
	}

	if coverage.EngagementID != eng.ID {
		t.Errorf("coverage.EngagementID: got %s", coverage.EngagementID)
	}
	if len(coverage.Tactics) == 0 {
		t.Error("expected non-empty tactics in coverage")
	}
	if coverage.TotalTechniques == 0 {
		t.Error("expected non-zero total techniques")
	}

	// Find coverage levels for the seeded techniques.
	levels := map[string]service.CoverageLevel{}
	for _, tac := range coverage.Tactics {
		for _, tc := range tac.Techniques {
			levels[tc.AttackID] = tc.Level
		}
	}

	if levels["T1566.002"] != service.CoverageLevelExecuted {
		t.Errorf("T1566.002: want executed(2), got %d", levels["T1566.002"])
	}
	if levels["T1059.001"] != service.CoverageLevelAttempted {
		t.Errorf("T1059.001: want attempted(1), got %d", levels["T1059.001"])
	}
	// Manual technique must NOT be counted as an attempt — level 0.
	if levels["T1055"] != service.CoverageLevelNone {
		t.Errorf("T1055 (manual): want none(0), got %d", levels["T1055"])
	}

	// Exercised counts only genuine attempts: the executed + the failed = 2.
	// The manual technique is reported in its own bucket, not as exercised.
	if coverage.ExercisedCount != 2 {
		t.Errorf("ExercisedCount: want 2 (manual excluded), got %d", coverage.ExercisedCount)
	}
	if coverage.ManualCount < 1 {
		t.Errorf("ManualCount: want >= 1, got %d", coverage.ManualCount)
	}
	if coverage.ExecutedCount != 1 {
		t.Errorf("ExecutedCount: want 1, got %d", coverage.ExecutedCount)
	}
}

// chainOperator is a fake Operator for the fact-chaining test: T1016 yields
// output containing a routable IP (parsed into host.ip); any technique with a
// "command" input echoes that input back so the test can observe ${fact}
// substitution.
type chainOperator struct{}

func (chainOperator) StartListener(_ context.Context, _ c2.ListenerSpec) error { return nil }
func (chainOperator) Sessions(_ context.Context) ([]c2.Session, error) {
	return []c2.Session{{ID: "s1", Host: "10.10.10.10", User: "SYSTEM"}}, nil
}
func (chainOperator) Execute(_ context.Context, _ string, t domain.Technique) (domain.Result, error) {
	out := "ran " + t.AttackID
	if cmd := t.Inputs["command"]; cmd != "" {
		out = cmd // echo the (substituted) command so the test sees the fact value
	} else if t.AttackID == "T1016" {
		out = "IPv4 Address. . . : 10.20.30.40"
	}
	return domain.Result{
		TechniqueAttackID: t.AttackID, Status: domain.ExecSuccess, Output: out,
		StartedAt: time.Now(), FinishedAt: time.Now(),
	}, nil
}

// chainResolver returns the chainOperator (legacy single-operator path).
type chainResolver struct{}

func (chainResolver) Resolve(_ context.Context, _ domain.Engagement) (c2.Operator, string, bool) {
	return chainOperator{}, "s1", true
}

func resultByID(run domain.ScenarioRun, id string) (domain.Result, bool) {
	for _, r := range run.Results {
		if r.TechniqueAttackID == id {
			return r, true
		}
	}
	return domain.Result{}, false
}

// TestEmulation_FactChaining_ProduceConsume verifies that a discovery technique's
// output is parsed into a fact and substituted into a later technique's input
// (${host.ip}) — the core fact-chaining behaviour, exercised through the live
// service run path.
func TestEmulation_FactChaining_ProduceConsume(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)

	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, hub)
	svcEmu.WithUserScenarios(memstore.NewUserScenarioStore())
	svcEmu.WithResolver(chainResolver{})

	sc, err := svcEmu.CreateScenario(ctx, domain.Scenario{
		Name: "chain",
		Techniques: []domain.Technique{
			{AttackID: "T1016", Name: "Network Config"}, // produces host.ip
			{AttackID: "T1059.001", Name: "PowerShell",
				Inputs:   map[string]string{"command": "ping ${host.ip}"},
				Requires: []string{"host.ip"}},
		},
	}, "op")
	if err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}

	runID, err := svcEmu.Start(ctx, eng.ID, sc.ID, "op")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	run := waitForRun(t, svcEmu, runID)

	consumer, ok := resultByID(run, "T1059.001")
	if !ok {
		t.Fatal("consumer technique missing from results")
	}
	if consumer.Status != domain.ExecSuccess {
		t.Errorf("consumer status: want success, got %s (%s)", consumer.Status, consumer.Output)
	}
	if consumer.Output != "ping 10.20.30.40" {
		t.Errorf("expected ${host.ip} substituted to the discovered IP, got output %q", consumer.Output)
	}
}

// captureReverter is an Operator that also implements c2.Reverter, recording
// the AttackIDs it is asked to revert so the cleanup path can be asserted.
type captureReverter struct{ reverted *[]string }

func (captureReverter) StartListener(_ context.Context, _ c2.ListenerSpec) error { return nil }
func (captureReverter) Sessions(_ context.Context) ([]c2.Session, error) {
	return []c2.Session{{ID: "s1", Host: "10.10.10.10", User: "SYSTEM"}}, nil
}
func (captureReverter) Execute(_ context.Context, _ string, t domain.Technique) (domain.Result, error) {
	return domain.Result{
		TechniqueAttackID: t.AttackID, Status: domain.ExecSuccess, Output: "ok",
		StartedAt: time.Now(), FinishedAt: time.Now(),
	}, nil
}
func (c captureReverter) Revert(_ context.Context, _ string, t domain.Technique) (domain.Result, error) {
	*c.reverted = append(*c.reverted, t.AttackID)
	return domain.Result{
		TechniqueAttackID: t.AttackID, Status: domain.ExecSuccess, Output: "reverted",
		StartedAt: time.Now(), FinishedAt: time.Now(),
	}, nil
}

type captureResolver struct{ op captureReverter }

func (r captureResolver) Resolve(_ context.Context, _ domain.Engagement) (c2.Operator, string, bool) {
	return r.op, "s1", true
}

// TestEmulation_Cleanup_RevertsPersistence verifies that a successfully-executed
// persistence technique is reverted at the end of the run (via c2.Reverter),
// that non-persistence techniques are not, and that cleanup is audited without
// being recorded as a technique result (so coverage is not inflated).
func TestEmulation_Cleanup_RevertsPersistence(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)

	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, hub)
	svcEmu.WithUserScenarios(memstore.NewUserScenarioStore())
	var reverted []string
	svcEmu.WithResolver(captureResolver{op: captureReverter{reverted: &reverted}})

	sc, err := svcEmu.CreateScenario(ctx, domain.Scenario{
		Name: "persist",
		Techniques: []domain.Technique{
			{AttackID: "T1082", Name: "System Info"},        // not cleanable
			{AttackID: "T1053.005", Name: "Scheduled Task"}, // cleanable
		},
	}, "op")
	if err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}

	runID, err := svcEmu.Start(ctx, eng.ID, sc.ID, "op")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	run := waitForRun(t, svcEmu, runID)

	if len(reverted) != 1 || reverted[0] != "T1053.005" {
		t.Fatalf("want exactly T1053.005 reverted, got %v", reverted)
	}
	if !hasAuditAction(s.audit, "emulation.cleanup", eng.ID) {
		t.Error("expected an emulation.cleanup audit event")
	}
	// Cleanup must NOT appear as an extra technique result (coverage stays honest):
	// exactly one result per executed technique.
	n := 0
	for _, r := range run.Results {
		if r.TechniqueAttackID == "T1053.005" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("scheduled-task results: want 1 (cleanup not a result), got %d", n)
	}
}

// fanoutOperator's T1016 reports two routable IPs; any technique with a
// "command" input echoes it back so the test can count fan-out executions.
type fanoutOperator struct{}

func (fanoutOperator) StartListener(_ context.Context, _ c2.ListenerSpec) error { return nil }
func (fanoutOperator) Sessions(_ context.Context) ([]c2.Session, error) {
	return []c2.Session{{ID: "s1", Host: "10.10.10.10", User: "SYSTEM"}}, nil
}
func (fanoutOperator) Execute(_ context.Context, _ string, t domain.Technique) (domain.Result, error) {
	out := "ran " + t.AttackID
	if cmd := t.Inputs["command"]; cmd != "" {
		out = cmd
	} else if t.AttackID == "T1016" {
		out = "Addr 10.20.30.40\nAddr 10.20.30.41"
	}
	return domain.Result{
		TechniqueAttackID: t.AttackID, Status: domain.ExecSuccess, Output: out,
		StartedAt: time.Now(), FinishedAt: time.Now(),
	}, nil
}

type fanoutResolver struct{}

func (fanoutResolver) Resolve(_ context.Context, _ domain.Engagement) (c2.Operator, string, bool) {
	return fanoutOperator{}, "s1", true
}

// TestEmulation_FactChaining_FanOut verifies that when a discovery technique
// collects several values for a fact, a later technique that references it runs
// once per value — the multi-value fan-out — through the live service path.
func TestEmulation_FactChaining_FanOut(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)

	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, hub)
	svcEmu.WithUserScenarios(memstore.NewUserScenarioStore())
	svcEmu.WithResolver(fanoutResolver{})

	sc, err := svcEmu.CreateScenario(ctx, domain.Scenario{
		Name: "fanout",
		Techniques: []domain.Technique{
			{AttackID: "T1016", Name: "Network Config"}, // produces 2 host.ip facts
			{AttackID: "T1059.001", Name: "PowerShell",
				Inputs:   map[string]string{"command": "ping ${host.ip}"},
				Requires: []string{"host.ip"}},
		},
	}, "op")
	if err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}

	runID, err := svcEmu.Start(ctx, eng.ID, sc.ID, "op")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	run := waitForRun(t, svcEmu, runID)

	var pings []string
	for _, r := range run.Results {
		if r.TechniqueAttackID == "T1059.001" {
			pings = append(pings, r.Output)
		}
	}
	if len(pings) != 2 {
		t.Fatalf("want 2 fan-out executions of the consumer, got %d (%v)", len(pings), pings)
	}
	want := map[string]bool{"ping 10.20.30.40": true, "ping 10.20.30.41": true}
	for _, p := range pings {
		if !want[p] {
			t.Errorf("unexpected fan-out output %q", p)
		}
	}
}

// TestEmulation_FactChaining_RequirementSkips verifies that a technique whose
// required fact was never collected is honestly recorded not_run (not executed),
// through the live run path.
func TestEmulation_FactChaining_RequirementSkips(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)

	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, hub)
	svcEmu.WithUserScenarios(memstore.NewUserScenarioStore())
	svcEmu.WithResolver(chainResolver{})

	// Consumer first, with no producer before it → requirement unmet.
	sc, err := svcEmu.CreateScenario(ctx, domain.Scenario{
		Name: "ungated",
		Techniques: []domain.Technique{
			{AttackID: "T1059.001", Name: "PowerShell", Requires: []string{"host.ip"}},
		},
	}, "op")
	if err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}

	runID, err := svcEmu.Start(ctx, eng.ID, sc.ID, "op")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	run := waitForRun(t, svcEmu, runID)

	res, ok := resultByID(run, "T1059.001")
	if !ok {
		t.Fatal("technique missing from results")
	}
	if res.Status != domain.ExecNotRun {
		t.Errorf("want not_run for unmet requirement, got %s", res.Status)
	}
	if res.Output == "" {
		t.Error("expected a reason explaining the skipped requirement")
	}
}

// TestGetCoverage_IncludesRanNonCatalogTechnique verifies that a technique
// which ran but is NOT part of any built-in scenario still shows up in the
// coverage rollup. Previously the heatmap universe was seeded only from the
// built-in catalog, so authored/imported techniques that were exercised were
// silently dropped (undercounting the engagement's own coverage).
func TestGetCoverage_IncludesRanNonCatalogTechnique(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()

	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)

	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, hub)

	runID, err := s.scenario.SaveRun(ctx, domain.ScenarioRun{
		EngagementID: eng.ID,
		ScenarioID:   "authored-x",
		Status:       domain.ExecSuccess,
		StartedAt:    time.Now().UTC(),
		FinishedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	// T1110.001 (Brute Force: Password Guessing) is in no built-in scenario and
	// no TTP catalog entry — the exact case that used to vanish.
	if err := s.scenario.SaveResult(ctx, runID, domain.Result{
		TechniqueAttackID: "T1110.001", Status: domain.ExecSuccess,
		StartedAt: time.Now(), FinishedAt: time.Now(),
	}); err != nil {
		t.Fatalf("SaveResult: %v", err)
	}

	coverage, err := svcEmu.GetCoverage(ctx, eng.ID)
	if err != nil {
		t.Fatalf("GetCoverage: %v", err)
	}

	var found *service.Techniquecoverage
	for _, tac := range coverage.Tactics {
		for i, tc := range tac.Techniques {
			if tc.AttackID == "T1110.001" {
				found = &tac.Techniques[i]
			}
		}
	}
	if found == nil {
		t.Fatal("ran technique T1110.001 missing from coverage (universe should include exercised techniques)")
	}
	if found.Level != service.CoverageLevelExecuted {
		t.Errorf("T1110.001: want executed(2), got %d", found.Level)
	}
	if coverage.ExecutedCount < 1 {
		t.Errorf("ExecutedCount: want >= 1, got %d", coverage.ExecutedCount)
	}
}

// TestGetCoverage_Empty verifies that an engagement with no runs returns
// a well-formed coverage with all levels at 0.
func TestGetCoverage_Empty(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()

	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)

	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, hub)

	coverage, err := svcEmu.GetCoverage(ctx, eng.ID)
	if err != nil {
		t.Fatalf("GetCoverage: %v", err)
	}

	// All levels should be 0 (no runs).
	for _, tac := range coverage.Tactics {
		for _, tc := range tac.Techniques {
			if tc.Level != 0 {
				t.Errorf("technique %s: want level 0 (no runs), got %d", tc.AttackID, tc.Level)
			}
		}
	}
}

// ---------- Navigator export tests ----------

// TestGetNavigatorLayer validates the Navigator layer JSON structure.
func TestGetNavigatorLayer(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()

	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)

	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, hub)

	// Seed one successful technique.
	runID, _ := s.scenario.SaveRun(ctx, domain.ScenarioRun{
		EngagementID: eng.ID,
		ScenarioID:   "apt29",
		Status:       domain.ExecSuccess,
		StartedAt:    time.Now().UTC(),
		FinishedAt:   time.Now().UTC(),
	})
	_ = s.scenario.SaveResult(ctx, runID, domain.Result{
		TechniqueAttackID: "T1566.002",
		Status:            domain.ExecSuccess,
		StartedAt:         time.Now(),
		FinishedAt:        time.Now(),
	})

	layer, err := svcEmu.GetNavigatorLayer(ctx, eng.ID, "Test Layer")
	if err != nil {
		t.Fatalf("GetNavigatorLayer: %v", err)
	}

	// Validate required fields per Navigator layer schema.
	if layer.Domain != "enterprise-attack" {
		t.Errorf("layer.Domain: want enterprise-attack, got %q", layer.Domain)
	}
	if layer.Name != "Test Layer" {
		t.Errorf("layer.Name: want 'Test Layer', got %q", layer.Name)
	}
	if layer.Versions.Attack == "" {
		t.Error("layer.Versions.Attack must not be empty")
	}
	if len(layer.Techniques) == 0 {
		t.Error("expected non-empty techniques list")
	}

	// Every technique must have a non-empty TechniqueID.
	for _, nt := range layer.Techniques {
		if nt.TechniqueID == "" {
			t.Error("navigator technique: empty TechniqueID")
		}
	}

	// Verify JSON marshals cleanly (Navigator consumers require valid JSON).
	b, err := json.Marshal(layer)
	if err != nil {
		t.Fatalf("json.Marshal layer: %v", err)
	}
	var reparse map[string]any
	if err := json.Unmarshal(b, &reparse); err != nil {
		t.Fatalf("json round-trip: %v", err)
	}
	if reparse["domain"] != "enterprise-attack" {
		t.Errorf("json domain: got %v", reparse["domain"])
	}

	// The seeded T1566.002 should have score=2 (executed).
	for _, nt := range layer.Techniques {
		if nt.TechniqueID == "T1566.002" && nt.Score != 2 {
			t.Errorf("T1566.002 score: want 2 (executed), got %d", nt.Score)
		}
	}
}

// TestCreateScenario_PersistAndRun verifies operator-authored scenarios are
// persisted, surfaced by ListScenarios, and runnable by Start (catalog miss
// falls back to the user-scenario store).
func TestCreateScenario_PersistAndRun(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()

	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)

	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, hub)
	svcEmu.WithResolver(service.NewFakeResolver())
	svcEmu.WithUserScenarios(memstore.NewUserScenarioStore())

	authored := domain.Scenario{
		Name:             "Insider — finance pivot",
		AdversaryProfile: "custom",
		Description:      "operator authored",
		Techniques: []domain.Technique{
			{AttackID: "T1059.001", Name: "PowerShell", Tactic: "execution"},
			{AttackID: "T1018", Name: "Remote System Discovery", Tactic: "discovery"},
		},
	}
	created, err := svcEmu.CreateScenario(ctx, authored, "test-op")
	if err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated scenario ID")
	}

	// ListScenarios must include both the catalog and the authored scenario.
	var found bool
	for _, sc := range svcEmu.ListScenarios() {
		if sc.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Error("authored scenario missing from ListScenarios")
	}

	// Start must resolve the authored scenario (not in catalog) and run it.
	runID, err := svcEmu.Start(ctx, eng.ID, created.ID, "test-op")
	if err != nil {
		t.Fatalf("Start authored scenario: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err := svcEmu.GetRun(ctx, runID)
		if err == nil && run.Status != domain.ExecRunning && len(run.Results) == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("authored scenario run did not complete with 2 results")
}

// TestCreateScenario_Validation rejects empty name / no techniques.
func TestCreateScenario_Validation(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, service.NewHub())
	svcEmu.WithUserScenarios(memstore.NewUserScenarioStore())

	if _, err := svcEmu.CreateScenario(ctx, domain.Scenario{Techniques: []domain.Technique{{AttackID: "T1059"}}}, "op"); err == nil {
		t.Error("expected error for empty name")
	}
	if _, err := svcEmu.CreateScenario(ctx, domain.Scenario{Name: "x"}, "op"); err == nil {
		t.Error("expected error for no techniques")
	}
}

// TestUpdateDeleteScenario covers the authored-scenario CRUD edges.
func TestUpdateDeleteScenario(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, service.NewHub())
	svcEmu.WithUserScenarios(memstore.NewUserScenarioStore())

	created, err := svcEmu.CreateScenario(ctx, domain.Scenario{
		Name:       "draft",
		Techniques: []domain.Technique{{AttackID: "T1059.001", Tactic: "execution"}},
	}, "op")
	if err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}

	// Update renames and swaps techniques.
	updated, err := svcEmu.UpdateScenario(ctx, domain.Scenario{
		ID:         created.ID,
		Name:       "renamed",
		Techniques: []domain.Technique{{AttackID: "T1018", Tactic: "discovery"}, {AttackID: "T1057", Tactic: "discovery"}},
	}, "op")
	if err != nil {
		t.Fatalf("UpdateScenario: %v", err)
	}
	if updated.Name != "renamed" || len(updated.Techniques) != 2 {
		t.Errorf("update not applied: %+v", updated)
	}

	// Built-in scenarios are immutable.
	if _, err := svcEmu.UpdateScenario(ctx, domain.Scenario{ID: "apt29", Name: "x", Techniques: []domain.Technique{{AttackID: "T1"}}}, "op"); err == nil {
		t.Error("expected error editing built-in scenario")
	}
	if err := svcEmu.DeleteScenario(ctx, "apt29", "op"); err == nil {
		t.Error("expected error deleting built-in scenario")
	}

	// Delete removes it from the listing.
	if err := svcEmu.DeleteScenario(ctx, created.ID, "op"); err != nil {
		t.Fatalf("DeleteScenario: %v", err)
	}
	for _, sc := range svcEmu.ListScenarios() {
		if sc.ID == created.ID {
			t.Error("deleted scenario still listed")
		}
	}
}

// TestTechniqueCRUD covers operator-authored TTP create/list/update/delete.
func TestTechniqueCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, service.NewHub())
	svcEmu.WithUserTechniques(memstore.NewUserTechniqueStore())

	created, err := svcEmu.CreateTechnique(ctx, domain.Technique{
		AttackID: "T1136.001", Name: "Create Local Account", Tactic: "Persistence",
		Description: "adds a local account", Commands: []string{"net user x y /add"},
	}, "op")
	if err != nil {
		t.Fatalf("CreateTechnique: %v", err)
	}
	if created.AttackID != "T1136.001" {
		t.Errorf("unexpected created: %+v", created)
	}

	// Duplicate attack id rejected.
	if _, err := svcEmu.CreateTechnique(ctx, domain.Technique{AttackID: "T1136.001", Name: "dup", Tactic: "Persistence"}, "op"); err == nil {
		t.Error("expected duplicate attack id to be rejected")
	}

	// Validation.
	if _, err := svcEmu.CreateTechnique(ctx, domain.Technique{Name: "no id", Tactic: "Execution"}, "op"); err == nil {
		t.Error("expected error for missing attack id")
	}

	// Update.
	updated, err := svcEmu.UpdateTechnique(ctx, domain.Technique{
		AttackID: "T1136.001", Name: "Create Local Account (edited)", Tactic: "Persistence",
	}, "op")
	if err != nil {
		t.Fatalf("UpdateTechnique: %v", err)
	}
	if updated.Name != "Create Local Account (edited)" {
		t.Errorf("update not applied: %+v", updated)
	}

	// List.
	list, err := svcEmu.ListTechniques(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListTechniques: %v len=%d", err, len(list))
	}

	// Delete.
	if err := svcEmu.DeleteTechnique(ctx, "T1136.001", "op"); err != nil {
		t.Fatalf("DeleteTechnique: %v", err)
	}
	if list, _ := svcEmu.ListTechniques(ctx); len(list) != 0 {
		t.Errorf("expected empty list after delete, got %d", len(list))
	}
}

// scopeTestOperator is a c2.Operator whose single session lives on a configurable
// host, used to verify execution-time scope enforcement.
type scopeTestOperator struct{ host string }

func (scopeTestOperator) StartListener(context.Context, c2.ListenerSpec) error { return nil }
func (o scopeTestOperator) Sessions(context.Context) ([]c2.Session, error) {
	return []c2.Session{{ID: "s1", Host: o.host, User: "SYSTEM"}}, nil
}
func (scopeTestOperator) Execute(_ context.Context, _ string, t domain.Technique) (domain.Result, error) {
	return domain.Result{TechniqueAttackID: t.AttackID, Status: domain.ExecSuccess}, nil
}

type scopeTestResolver struct{ op c2.Operator }

func (r scopeTestResolver) Resolve(context.Context, domain.Engagement) (c2.Operator, string, bool) {
	return r.op, "fallback-session", true
}

// TestEmulationScopeEnforcement verifies that when the only available agent is
// outside the engagement scope, emulation refuses to execute (all Skipped) and
// audits the scope block — scope is enforced at execution time.
func TestEmulationScopeEnforcement(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng) // scope 10.0.0.0/8

	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, hub)
	// Agent host 8.8.8.8 is out of scope.
	svcEmu.WithResolver(scopeTestResolver{op: scopeTestOperator{host: "8.8.8.8"}})

	runID, err := svcEmu.Start(ctx, eng.ID, "apt29", "op1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	var run domain.ScenarioRun
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, _ = svcEmu.GetRun(ctx, runID)
		if run.Status != domain.ExecRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if run.Status != domain.ExecBlockedByScope {
		t.Errorf("out-of-scope run status: want blocked_by_scope, got %s", run.Status)
	}
	for _, r := range run.Results {
		if r.Status != domain.ExecBlockedByScope {
			t.Errorf("technique %s: want blocked_by_scope (out of scope), got %s", r.TechniqueAttackID, r.Status)
		}
	}
	if !hasAuditAction(s.audit, "emulation.scope_block", eng.ID) {
		t.Error("expected emulation.scope_block audit event")
	}

	// Sanity: an in-scope agent runs successfully.
	svcEmu.WithResolver(scopeTestResolver{op: scopeTestOperator{host: "10.20.30.40"}})
	runID2, err := svcEmu.Start(ctx, eng.ID, "apt29", "op1")
	if err != nil {
		t.Fatalf("Start (in-scope): %v", err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, _ = svcEmu.GetRun(ctx, runID2)
		if run.Status != domain.ExecRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if run.Status != domain.ExecSuccess {
		t.Errorf("in-scope run status: want success, got %s", run.Status)
	}
}

// TestRecordDetection_FeedsTRM verifies the purple-team scoring step records a
// defender outcome and lifts the technique to "validated", raising the TRM.
func TestRecordDetection_FeedsTRM(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	svcEng := service.NewEngagementService(s.eng, s.audit)
	eng := authorizedEngagement(t, ctx, svcEng)
	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, hub)
	svcEmu.WithResolver(service.NewFakeResolver())

	runID, err := svcEmu.Start(ctx, eng.ID, "apt29", "op")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	var run domain.ScenarioRun
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, _ = svcEmu.GetRun(ctx, runID)
		if run.Status != domain.ExecRunning && len(run.Results) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(run.Results) == 0 {
		t.Fatal("run produced no results")
	}

	// Baseline: nothing detected yet → TRM 0.
	cov0, _ := svcEmu.GetCoverage(ctx, eng.ID)
	if cov0.TRM != 0 || cov0.ValidatedCount != 0 {
		t.Errorf("baseline TRM/validated should be 0, got trm=%d validated=%d", cov0.TRM, cov0.ValidatedCount)
	}

	tid := run.Results[0].TechniqueAttackID
	if err := svcEmu.RecordDetection(ctx, runID, tid, domain.DetectDetected, "op"); err != nil {
		t.Fatalf("RecordDetection: %v", err)
	}

	cov1, _ := svcEmu.GetCoverage(ctx, eng.ID)
	if cov1.ValidatedCount < 1 || cov1.TRM <= 0 {
		t.Errorf("after detection: want validated>=1 and trm>0, got validated=%d trm=%d", cov1.ValidatedCount, cov1.TRM)
	}
	if !hasAuditAction(s.audit, "emulation.detection", "") && !hasAuditAction(s.audit, "emulation.detection", eng.ID) {
		t.Error("expected emulation.detection audit event")
	}

	// Invalid outcome and unknown technique are rejected.
	if err := svcEmu.RecordDetection(ctx, runID, tid, domain.DetectionOutcome("bogus"), "op"); err == nil {
		t.Error("expected error for invalid outcome")
	}
	if err := svcEmu.RecordDetection(ctx, runID, "T9999", domain.DetectBlocked, "op"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown technique: want ErrNotFound, got %v", err)
	}
}
