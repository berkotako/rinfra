package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

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

	// All techniques should be Skipped; run status should be Skipped.
	if run.Status != domain.ExecSkipped {
		t.Errorf("fronted tier run status: want skipped, got %s", run.Status)
	}
	for _, r := range run.Results {
		if r.Status != domain.ExecSkipped {
			t.Errorf("technique %s: want skipped, got %s", r.TechniqueAttackID, r.Status)
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

	// T1566.002 → Success (level 2 Executed)
	// T1059.001 → Failed (level 1 Attempted)
	// T1055     → Skipped (level 1 Attempted)
	seedResults := []domain.Result{
		{TechniqueAttackID: "T1566.002", Status: domain.ExecSuccess, StartedAt: time.Now(), FinishedAt: time.Now()},
		{TechniqueAttackID: "T1059.001", Status: domain.ExecFailed, StartedAt: time.Now(), FinishedAt: time.Now()},
		{TechniqueAttackID: "T1055", Status: domain.ExecSkipped, StartedAt: time.Now(), FinishedAt: time.Now()},
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
	if levels["T1055"] != service.CoverageLevelAttempted {
		t.Errorf("T1055: want attempted(1), got %d", levels["T1055"])
	}

	// Verify ExercisedCount >= number of techniques we exercised.
	if coverage.ExercisedCount < 3 {
		t.Errorf("ExercisedCount: want >= 3, got %d", coverage.ExercisedCount)
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
