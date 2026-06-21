package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/rinfra/rinfra/internal/c2"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/service"
)

// fakeCandidateResolver drives the routed orchestrator path with a fixed
// candidate set. It also satisfies OperatorResolver (the legacy path) but
// Candidates is what the orchestrator uses.
type fakeCandidateResolver struct{ cands []service.Candidate }

func (fakeCandidateResolver) Resolve(context.Context, domain.Engagement) (c2.Operator, string, bool) {
	return nil, "", false
}
func (f fakeCandidateResolver) Candidates(context.Context, domain.Engagement) []service.Candidate {
	return f.cands
}

// successOperator records the session each Execute was routed to and reports
// ExecSuccess.
type successOperator struct{ routed *[]string }

func (successOperator) StartListener(context.Context, c2.ListenerSpec) error { return nil }
func (successOperator) Sessions(context.Context) ([]c2.Session, error)       { return nil, nil }
func (o successOperator) Execute(_ context.Context, sessionID string, t domain.Technique) (domain.Result, error) {
	if o.routed != nil {
		*o.routed = append(*o.routed, sessionID)
	}
	return domain.Result{TechniqueAttackID: t.AttackID, Status: domain.ExecSuccess}, nil
}

func waitForRun(t *testing.T, svc *service.EmulationService, runID string) domain.ScenarioRun {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err := svc.GetRun(context.Background(), runID)
		if err == nil && run.Status != domain.ExecRunning && run.Status != "" {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s did not finish in time", runID)
	return domain.ScenarioRun{}
}

func TestRoutedRun_ExecutesOnInScopeAgent(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	eng := authorizedEngagement(t, ctx, service.NewEngagementService(s.eng, s.audit))

	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, hub)
	var routed []string
	svcEmu.WithResolver(fakeCandidateResolver{cands: []service.Candidate{{
		Framework: "sliver", Tier: c2.TierOrchestrated,
		Caps:     c2.Capabilities{Platforms: []string{"windows", "linux", "macos"}},
		Operator: successOperator{routed: &routed},
		Sessions: []c2.Session{winSession("s1", "10.0.0.5", "NT AUTHORITY\\SYSTEM")},
	}}})

	runID, err := svcEmu.Start(ctx, eng.ID, "apt29", "op")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	run := waitForRun(t, svcEmu, runID)
	if run.Status != domain.ExecSuccess {
		t.Fatalf("run status = %s, want success (routed to the in-scope agent)", run.Status)
	}
	if len(routed) == 0 {
		t.Fatal("expected the operator to be invoked via routing")
	}
	for _, sid := range routed {
		if sid != "s1" {
			t.Errorf("technique routed to session %q, want s1", sid)
		}
	}
}

func TestRoutedRun_OutOfScopeBlocked(t *testing.T) {
	ctx := context.Background()
	s := newTestStores()
	hub := service.NewHub()
	eng := authorizedEngagement(t, ctx, service.NewEngagementService(s.eng, s.audit))

	svcEmu := service.NewEmulationService(s.eng, s.scenario, s.audit, hub)
	svcEmu.WithResolver(fakeCandidateResolver{cands: []service.Candidate{{
		Framework: "sliver", Tier: c2.TierOrchestrated,
		Caps:     c2.Capabilities{Platforms: []string{"windows", "linux", "macos"}},
		Operator: successOperator{},
		Sessions: []c2.Session{winSession("s1", "203.0.113.9", "user")}, // out of scope
	}}})

	runID, err := svcEmu.Start(ctx, eng.ID, "apt29", "op")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	run := waitForRun(t, svcEmu, runID)
	if run.Status != domain.ExecBlockedByScope {
		t.Errorf("run status = %s, want blocked_by_scope", run.Status)
	}
	for _, r := range run.Results {
		if r.Status != domain.ExecBlockedByScope {
			t.Errorf("technique %s: status %s, want blocked_by_scope", r.TechniqueAttackID, r.Status)
		}
	}
}

// idOperator is a stub c2.Operator that records its identity so a test can tell
// which candidate the router selected. Route never invokes it.
type idOperator struct{ id string }

func (idOperator) StartListener(context.Context, c2.ListenerSpec) error { return nil }
func (idOperator) Sessions(context.Context) ([]c2.Session, error)       { return nil, nil }
func (o idOperator) Execute(context.Context, string, domain.Technique) (domain.Result, error) {
	return domain.Result{Output: o.id}, nil
}

// scopedEngagement returns an engagement whose scope admits 10.0.0.0/8.
func scopedEngagement() *domain.Engagement {
	return &domain.Engagement{
		ID:    "eng-route",
		Scope: domain.Scope{AllowedTargets: []string{"10.0.0.0/8"}},
	}
}

func winSession(id, host, user string) c2.Session {
	return c2.Session{ID: id, Host: host, User: user, Metadata: map[string]string{"os": "windows"}}
}
func linuxSession(id, host string) c2.Session {
	return c2.Session{ID: id, Host: host, Metadata: map[string]string{"os": "linux"}}
}

func TestRoute_PrefersOrchestratedOverScripted(t *testing.T) {
	eng := scopedEngagement()
	tech := domain.Technique{AttackID: "T1059.001", Tactic: "execution"} // windows

	cands := []service.Candidate{
		{
			Framework: "havoc", Tier: c2.TierScripted,
			Caps:     c2.Capabilities{Platforms: []string{"windows"}, Techniques: []string{"T1059.001"}},
			Operator: idOperator{"havoc"},
			Sessions: []c2.Session{winSession("h1", "10.0.0.5", "user")},
		},
		{
			Framework: "sliver", Tier: c2.TierOrchestrated,
			Caps:     c2.Capabilities{Platforms: []string{"windows", "linux", "macos"}},
			Operator: idOperator{"sliver"},
			Sessions: []c2.Session{winSession("s1", "10.0.0.6", "user")},
		},
	}
	op, sid, disp := service.Route(eng, tech, cands)
	if disp != "" {
		t.Fatalf("expected execute, got disposition %q", disp)
	}
	if got := op.(idOperator).id; got != "sliver" {
		t.Errorf("routed to %q, want sliver (Orchestrated preferred)", got)
	}
	if sid != "s1" {
		t.Errorf("session = %q, want s1", sid)
	}
}

func TestRoute_PlatformMismatchBlocks(t *testing.T) {
	eng := scopedEngagement()
	tech := domain.Technique{AttackID: "T1547.001", Tactic: "persistence"} // windows-only

	// Only a linux agent is available — the technique can't run on it.
	cands := []service.Candidate{{
		Framework: "sliver", Tier: c2.TierOrchestrated,
		Caps:     c2.Capabilities{Platforms: []string{"windows", "linux", "macos"}},
		Operator: idOperator{"sliver"},
		Sessions: []c2.Session{linuxSession("l1", "10.0.0.7")},
	}}
	op, _, disp := service.Route(eng, tech, cands)
	if op != nil {
		t.Fatalf("expected no operator for platform mismatch")
	}
	if disp != domain.ExecBlockedByScope {
		t.Errorf("disposition = %q, want blocked_by_scope", disp)
	}
}

func TestRoute_OutOfScopeBlocks(t *testing.T) {
	eng := scopedEngagement()
	tech := domain.Technique{AttackID: "T1082", Tactic: "discovery"}

	cands := []service.Candidate{{
		Framework: "sliver", Tier: c2.TierOrchestrated,
		Caps:     c2.Capabilities{Platforms: []string{"windows", "linux"}},
		Operator: idOperator{"sliver"},
		Sessions: []c2.Session{winSession("s1", "192.168.1.10", "user")}, // out of 10/8
	}}
	op, _, disp := service.Route(eng, tech, cands)
	if op != nil || disp != domain.ExecBlockedByScope {
		t.Errorf("op=%v disp=%q, want nil + blocked_by_scope", op, disp)
	}
}

func TestRoute_OnlyFrontedIsManual(t *testing.T) {
	eng := scopedEngagement()
	tech := domain.Technique{AttackID: "T1082", Tactic: "discovery"}

	cands := []service.Candidate{{
		Framework: "cobaltstrike", Tier: c2.TierFronted,
		Caps:     c2.Capabilities{}, // broad default
		Operator: nil,               // Fronted: no operator
	}}
	op, _, disp := service.Route(eng, tech, cands)
	if op != nil || disp != domain.ExecManualRequired {
		t.Errorf("op=%v disp=%q, want nil + manual_required", op, disp)
	}
}

func TestRoute_UnsupportedWhenNoFrameworkCan(t *testing.T) {
	eng := scopedEngagement()
	tech := domain.Technique{AttackID: "T1620", Tactic: "defense-evasion"} // not in allowlist

	cands := []service.Candidate{{
		Framework: "havoc", Tier: c2.TierScripted,
		Caps:     c2.Capabilities{Platforms: []string{"windows"}, Techniques: []string{"T1082"}}, // narrow
		Operator: idOperator{"havoc"},
		Sessions: []c2.Session{winSession("h1", "10.0.0.5", "user")},
	}}
	op, _, disp := service.Route(eng, tech, cands)
	if op != nil || disp != domain.ExecUnsupported {
		t.Errorf("op=%v disp=%q, want nil + unsupported", op, disp)
	}
}

func TestRoute_NoCandidatesUnsupported(t *testing.T) {
	op, _, disp := service.Route(scopedEngagement(), domain.Technique{AttackID: "T1082"}, nil)
	if op != nil || disp != domain.ExecUnsupported {
		t.Errorf("op=%v disp=%q, want nil + unsupported", op, disp)
	}
}

func TestRoute_RequiresPrivilegePrefersAndFilters(t *testing.T) {
	eng := scopedEngagement()
	tech := domain.Technique{
		AttackID: "T1003.001", Tactic: "credential-access",
		Inputs: map[string]string{"requires_privilege": "true"},
	}
	// Unprivileged windows session can't satisfy the privilege requirement.
	cands := []service.Candidate{{
		Framework: "sliver", Tier: c2.TierOrchestrated,
		Caps:     c2.Capabilities{Platforms: []string{"windows"}},
		Operator: idOperator{"sliver"},
		Sessions: []c2.Session{winSession("low", "10.0.0.5", "user")},
	}}
	if _, _, disp := service.Route(eng, tech, cands); disp != domain.ExecBlockedByScope {
		t.Errorf("unprivileged: disp = %q, want blocked_by_scope", disp)
	}

	// Add a SYSTEM session — now it routes.
	cands[0].Sessions = append(cands[0].Sessions, winSession("high", "10.0.0.6", "NT AUTHORITY\\SYSTEM"))
	op, sid, disp := service.Route(eng, tech, cands)
	if disp != "" || op == nil {
		t.Fatalf("privileged: expected execute, got disp %q", disp)
	}
	if sid != "high" {
		t.Errorf("routed to session %q, want the SYSTEM session 'high'", sid)
	}
}

func TestRequiredPlatform(t *testing.T) {
	cases := map[string]string{
		"T1059.001": "windows",
		"T1547.001": "windows",
		"T1059.004": "linux",
		"T1082":     "", // platform-agnostic
	}
	for id, want := range cases {
		if got := service.RequiredPlatform(domain.Technique{AttackID: id}); got != want {
			t.Errorf("RequiredPlatform(%s) = %q, want %q", id, got, want)
		}
	}
	// Inputs override.
	got := service.RequiredPlatform(domain.Technique{AttackID: "T1082", Inputs: map[string]string{"platform": "macos"}})
	if got != "macos" {
		t.Errorf("Inputs platform override = %q, want macos", got)
	}
}
