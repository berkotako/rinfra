"use client";
import React, { useState, useRef, useEffect, useCallback, useMemo } from "react";
import { Icons } from "../icons";
import { PageHead, Modal } from "../ui";
import { OperatorStatus } from "../c2/ManualAccess";
import { useStore } from "../../lib/store";
import { getClient, isRestMode } from "../../lib/client";
import { deployedC2FromNode, c2SupportsTactic, SCENARIOS, SAMPLE_INDEX_YAML } from "../../lib/data";
import TechniqueDetail from "./TechniqueDetail";
import ScenarioBuilder from "./ScenarioBuilder";
import RunGantt from "./RunGantt";
import type { NodeStatus, Technique, DeployedC2, Scenario, OperatorSession } from "../../lib/types";

type StepStatus = "pending" | "running" | "done" | "detected" | "manual";

// isManualDisposition reports whether a result status is a non-attempt that a
// human must run (or that was skipped/blocked) — it must NOT render as "done",
// which would imply the technique was actually executed.
function isManualDisposition(status: string): boolean {
  return (
    status === "manual_required" ||
    status === "blocked_by_scope" ||
    status === "unsupported" ||
    status === "skipped_policy" ||
    status === "skipped" ||
    status === "not_run"
  );
}

// Sentinel target id: route each technique to the best live agent automatically.
const AUTO = "auto";
const TIER_RANK: Record<string, number> = { orchestrated: 0, scripted: 1, fronted: 2 };

interface TechniqueRoute {
  c2: DeployedC2;
  agent?: OperatorSession;
}

const ST_META: Record<StepStatus, { c: string; icon: string; label: string }> = {
  pending: { c: "var(--text-4)", icon: "Dot", label: "Queued" },
  running: { c: "var(--warn)", icon: "Activity", label: "Running" },
  done: { c: "var(--ok)", icon: "CheckCircle", label: "Executed" },
  detected: { c: "var(--info)", icon: "Eye", label: "Detected" },
  manual: { c: "var(--text-3)", icon: "Power", label: "Manual" },
};

const TACTIC_TONE: Record<string, number> = {
  "Initial Access": 250,
  Execution: 280,
  Persistence: 200,
  "Defense Evasion": 75,
  "Credential Access": 25,
  Discovery: 168,
  "Lateral Movement": 240,
  Exfiltration: 300,
  Impact: 25,
};

// Defender outcome (SRA three-part evaluation). "passed" = alerted/detected/blocked.
type DetectionOutcome = "none" | "alerted" | "detected" | "blocked";
const DET_META: Record<DetectionOutcome, { label: string; cls: string }> = {
  none: { label: "missed", cls: "danger" },
  alerted: { label: "alerted", cls: "info" },
  detected: { label: "detected", cls: "ok" },
  blocked: { label: "blocked", cls: "ok" },
};
const DET_OPTIONS: DetectionOutcome[] = ["none", "alerted", "detected", "blocked"];
const detPassed = (d?: DetectionOutcome) => d === "alerted" || d === "detected" || d === "blocked";

// simDetection picks a plausible defender outcome for the demo run.
function simDetection(): DetectionOutcome {
  const r = Math.random();
  if (r < 0.4) return "none";
  if (r < 0.65) return "detected";
  if (r < 0.85) return "alerted";
  return "blocked";
}

export default function EmulationScreen() {
  const {
    activeEngagement,
    activeEngagementId,
    nodes,
    pushToast,
    apiStartRun,
    scenarios,
    addScenario,
    updateScenario,
    deleteScenario,
    importIndex,
  } = useStore();
  const [scenarioId, setScenarioId] = useState(scenarios[0].id);
  const [c2Id, setC2Id] = useState<string>(AUTO);
  const [running, setRunning] = useState(false);
  const [stepState, setStepState] = useState<Record<number, StepStatus>>({});
  const [detailIdx, setDetailIdx] = useState<number | null>(null);
  const [builderOpen, setBuilderOpen] = useState(false);
  const [editing, setEditing] = useState<Scenario | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<Scenario | null>(null);
  const [view, setView] = useState<"steps" | "timeline">("steps");
  const [detection, setDetection] = useState<Record<number, DetectionOutcome>>({});
  const [importOpen, setImportOpen] = useState(false);
  const [importText, setImportText] = useState(SAMPLE_INDEX_YAML);
  const [timeline, setTimeline] = useState<Record<number, { start: number; end?: number }>>({});
  const [nowTick, setNowTick] = useState(0);
  const runStartRef = useRef<number>(0);
  const timers = useRef<ReturnType<typeof setTimeout>[]>([]);
  const runIdRef = useRef<string | null>(null);
  const restMode = isRestMode();
  const client = getClient();

  const scenario = scenarios.find((s) => s.id === scenarioId) || scenarios[0];

  // Built-in catalog scenarios are immutable; only authored ones support edit/delete.
  const builtinIds = useMemo(() => new Set(SCENARIOS.map((s) => s.id)), []);
  const isCustom = (s: Scenario) => !builtinIds.has(s.id);

  const handleSubmitScenario = (s: Scenario) => {
    const op = editing ? updateScenario(s) : addScenario(s);
    op.then((saved) => setScenarioId(saved.id)).catch(() => undefined);
    setBuilderOpen(false);
    setEditing(null);
  };

  const handleImportIndex = () => {
    importIndex(importText)
      .then((sc) => setScenarioId(sc.id))
      .catch(() => undefined);
    setImportOpen(false);
  };

  const handleDeleteScenario = (s: Scenario) => {
    deleteScenario(s.id)
      .then(() => {
        if (scenarioId === s.id) setScenarioId(SCENARIOS[0].id);
      })
      .catch(() => undefined);
    setConfirmDelete(null);
  };

  // Deployed C2 teamservers from the live topology — the emulation targets.
  const c2Targets = useMemo(
    () => nodes.map(deployedC2FromNode).filter((d): d is DeployedC2 => d !== null),
    [nodes]
  );
  const liveC2s = useMemo(() => c2Targets.filter((c) => c.status === "live"), [c2Targets]);
  const selectedC2 = c2Id === AUTO ? undefined : c2Targets.find((c) => c.nodeId === c2Id);

  // routeFor finds the right agent to execute a technique: a live C2 whose
  // framework can automate the technique's tactic (preferring Orchestrated over
  // Scripted), plus an active session on it. In auto mode every live C2 is a
  // candidate; otherwise only the chosen one.
  const routeFor = useCallback(
    (t: Technique): TechniqueRoute | null => {
      const pool = c2Id === AUTO ? liveC2s : liveC2s.filter((c) => c.nodeId === c2Id);
      const candidates = pool.filter((c) => c2SupportsTactic(c.framework, t.tactic));
      if (candidates.length === 0) return null;
      const best = [...candidates].sort((a, b) => TIER_RANK[a.tier] - TIER_RANK[b.tier])[0];
      return { c2: best, agent: best.sessions[0] };
    },
    [c2Id, liveC2s]
  );

  // Whether some live C2 can automate this technique under the current target.
  const supports = useCallback((t: Technique) => routeFor(t) !== null, [routeFor]);

  // Reset to auto-route if the chosen specific C2 is no longer live.
  useEffect(() => {
    if (c2Id !== AUTO && !liveC2s.some((c) => c.nodeId === c2Id)) {
      setC2Id(AUTO);
    }
  }, [liveC2s, c2Id]);

  // Base step state for the current scenario + C2: unsupported techniques are
  // pre-marked "manual" so the TTP <-> C2 mapping is visible before any run.
  const baseStepState = useCallback((): Record<number, StepStatus> => {
    const b: Record<number, StepStatus> = {};
    scenario.techniques.forEach((t, i) => {
      if (!supports(t)) b[i] = "manual";
    });
    return b;
  }, [scenario, supports]);

  const reset = () => {
    timers.current.forEach(clearTimeout);
    timers.current = [];
    setStepState(baseStepState());
    setTimeline({});
    setDetection({});
    setRunning(false);
  };

  // markStep updates a technique's status and records its run-timeline window
  // (start on first "running", end on completion) for the Gantt view.
  const markStep = useCallback((i: number, st: StepStatus) => {
    setStepState((s) => ({ ...s, [i]: st }));
    const elapsed = runStartRef.current ? Date.now() - runStartRef.current : 0;
    setTimeline((tl) => {
      const cur = tl[i];
      if (st === "running") {
        return cur?.start !== undefined ? tl : { ...tl, [i]: { start: elapsed } };
      }
      if (st === "done" || st === "detected") {
        return { ...tl, [i]: { start: cur?.start ?? elapsed, end: elapsed } };
      }
      return tl;
    });
  }, []);

  // setDet records a defender outcome for a technique and, in REST mode, sends
  // it to the backend (purple-team scoring → TRM).
  const setDet = useCallback(
    (i: number, d: DetectionOutcome) => {
      setDetection((m) => ({ ...m, [i]: d }));
      const tid = scenario.techniques[i]?.id;
      const runID = runIdRef.current;
      if (tid && runID) {
        client.recordDetection(runID, tid, d).catch(() => undefined);
      }
    },
    [client, scenario]
  );

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      timers.current.forEach(clearTimeout);
    };
  }, []);

  // While running, tick so the Gantt bars grow live.
  useEffect(() => {
    if (!running) return;
    const id = setInterval(() => setNowTick((t) => t + 1), 250);
    return () => clearInterval(id);
  }, [running]);

  // Re-derive the resting view whenever the scenario or C2 target changes.
  useEffect(() => {
    if (running) return;
    timers.current.forEach(clearTimeout);
    timers.current = [];
    setStepState(baseStepState());
    setTimeline({});
    setDetection({});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scenarioId, c2Id]);

  // SSE subscription for run events in REST mode.
  const subscribeRunEvents = useCallback(
    (engagementId: string, localRunId: string, techniques: Technique[]) => {
      const unsubscribe = client.subscribeEvents(engagementId, (ev) => {
        if (ev.kind !== "run_status") return;
        if (ev.data.runId !== localRunId) return;

        const { techniqueId, status } = ev.data;
        if (techniqueId) {
          const idx = techniques.findIndex((t) => t.id === techniqueId);
          if (idx >= 0) {
            const st: StepStatus =
              status === "success" ? "done" :
              status === "detected" ? "detected" :
              status === "running" ? "running" :
              isManualDisposition(status) ? "manual" :
              "done";
            markStep(idx, st);
          }
        } else if (status !== "running" && status !== "pending") {
          // Any terminal run-level status completes the run — including
          // manual_required / blocked_by_scope / unsupported, which otherwise
          // leave the UI spinning until the polling fallback fires.
          setRunning(false);
          pushToast("Emulation complete — results captured", "ok");
          unsubscribe();
        }
      });
      return unsubscribe;
    },
    [client, pushToast, markStep]
  );

  // Polling fallback for REST mode: poll GET /runs/{id} every 2 s until done.
  const pollRun = useCallback(
    (runId: string, techniques: Technique[]) => {
      const interval = setInterval(async () => {
        try {
          const run = await client.getRun(runId);
          for (const r of run.results) {
            const idx = techniques.findIndex((t) => t.id === r.techniqueId);
            if (idx >= 0) {
              const st: StepStatus =
                r.status === "success" ? "done" :
                r.status === "detected" ? "detected" :
                r.status === "running" ? "running" :
                isManualDisposition(r.status) ? "manual" :
                "done";
              markStep(idx, st);
            }
          }
          if (run.status !== "running") {
            clearInterval(interval);
            setRunning(false);
            pushToast("Emulation complete — results captured", "ok");
          }
        } catch {
          // ignore transient errors
        }
      }, 2000);
      timers.current.push(interval as unknown as ReturnType<typeof setTimeout>);
    },
    [client, pushToast, markStep]
  );

  const run = () => {
    if (runnableCount === 0) return;
    timers.current.forEach(clearTimeout);
    timers.current = [];
    setStepState(baseStepState());
    setTimeline({});
    setDetection({});
    runStartRef.current = Date.now();
    setRunning(true);
    const via = selectedC2 ? selectedC2.frameworkName : "auto-routed agents";
    pushToast(`Emulation started — ${scenario.name} via ${via}`, "info");

    if (restMode) {
      apiStartRun(activeEngagementId, scenarioId).then((runId) => {
        runIdRef.current = runId;
        const unsub = subscribeRunEvents(activeEngagementId, runId, scenario.techniques);
        const fallbackTimer = setTimeout(() => {
          unsub();
          pollRun(runId, scenario.techniques);
        }, 5000);
        timers.current.push(fallbackTimer);
      }).catch((err: unknown) => {
        setRunning(false);
        const msg = err instanceof Error ? err.message : "Failed to start emulation";
        pushToast(msg, "danger");
      });
      return;
    }

    // Mock mode: local simulation — animate only techniques the C2 can automate.
    const runnable = scenario.techniques
      .map((t, i) => ({ t, i }))
      .filter(({ t }) => supports(t));
    runnable.forEach(({ i }, order) => {
      timers.current.push(
        setTimeout(() => markStep(i, "running"), order * 1500 + 200)
      );
      timers.current.push(
        setTimeout(
          () => {
            markStep(i, "done");
            setDet(i, simDetection());
          },
          order * 1500 + 1300
        )
      );
    });
    timers.current.push(
      setTimeout(() => {
        setRunning(false);
        pushToast("Emulation complete — results captured", "ok");
      }, runnable.length * 1500 + 600)
    );
  };

  const runnableCount = scenario.techniques.filter((t) => supports(t)).length;
  const done = scenario.techniques.filter(
    (_, i) => stepState[i] === "done" || stepState[i] === "detected"
  ).length;
  const passed = scenario.techniques.filter((_, i) => detPassed(detection[i])).length;
  const liveTRM = done > 0 ? Math.round((passed / done) * 100) : 0;
  const pct = runnableCount ? Math.round((done / runnableCount) * 100) : 0;
  const canRun = runnableCount > 0;

  // Gantt time axis: the longest observed window, extended to "now" while a run
  // is in flight (nowTick forces this to recompute on the live ticker).
  void nowTick;
  const maxEnd = Object.values(timeline).reduce((m, w) => Math.max(m, w.end ?? w.start), 0);
  const ganttNowMs = running ? Math.max(maxEnd, Date.now() - runStartRef.current) : Math.max(maxEnd, 1);

  // Live infrastructure from actual topology state
  const liveNodes = nodes.filter((n) => n.status === "live");
  const targetInfra = [
    ...liveNodes
      .filter((n) => n.type === "redirector")
      .map((n) => ({ role: "Redirector", name: n.name, status: "live" as NodeStatus })),
    ...liveNodes
      .filter((n) => n.type === "payload_host")
      .map((n) => ({ role: "Staging", name: n.name, status: "live" as NodeStatus })),
  ];

  const circumference = 2 * Math.PI * 26;

  return (
    <div className="scroll" style={{ height: "100%", padding: "26px 32px 40px" }}>
      <div style={{ maxWidth: 1080, margin: "0 auto" }}>
        <PageHead
          title="Adversary emulation"
          sub={`Run ATT&CK-mapped scenarios against ${activeEngagement.codename}'s deployed infrastructure.`}
        >
          <button className="btn" onClick={() => setImportOpen(true)}>
            <Icons.Plus size={15} /> Import index
          </button>
          <button
            className="btn primary"
            onClick={() => {
              setEditing(null);
              setBuilderOpen(true);
            }}
          >
            <Icons.Plus size={15} /> New scenario
          </button>
        </PageHead>

        {/* scenario picker */}
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(3,1fr)",
            gap: 12,
            marginBottom: 22,
          }}
        >
          {scenarios.map((s) => {
            const active = s.id === scenarioId;
            return (
              <div
                key={s.id}
                onClick={() => !running && setScenarioId(s.id)}
                style={{
                  padding: "15px 16px",
                  borderRadius: "var(--r-lg)",
                  cursor: running ? "default" : "pointer",
                  border: `1px solid ${active ? "var(--accent)" : "var(--border)"}`,
                  background: active ? "var(--accent-soft)" : "var(--surface)",
                  boxShadow: active ? "0 0 0 3px var(--accent-soft)" : "var(--shadow-xs)",
                  opacity: running && !active ? 0.55 : 1,
                  transition: "all .14s",
                }}
              >
                <div
                  style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    marginBottom: 8,
                  }}
                >
                  <span style={{ color: active ? "var(--accent)" : "var(--text-3)" }}>
                    <Icons.Crosshair size={18} />
                  </span>
                  <span style={{ display: "flex", gap: 6 }}>
                    {isCustom(s) && (
                      <span className="pill accent" style={{ height: 20 }}>
                        custom
                      </span>
                    )}
                    <span className="pill" style={{ height: 20 }}>
                      {s.techniques.length} techniques
                    </span>
                  </span>
                </div>
                <div style={{ fontSize: 14, fontWeight: 600 }}>{s.name}</div>
                <div style={{ fontSize: 11.5, color: "var(--text-3)", marginTop: 2 }}>
                  {s.actor}
                </div>
              </div>
            );
          })}
        </div>

        <div style={{ display: "flex", gap: 22, alignItems: "flex-start" }}>
          {/* timeline */}
          <div style={{ flex: "1 1 0", minWidth: 0 }}>
            <div className="card" style={{ overflow: "hidden" }}>
              <div
                style={{
                  padding: "15px 18px",
                  borderBottom: "1px solid var(--border)",
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "space-between",
                }}
              >
                <div>
                  <div style={{ fontSize: 14, fontWeight: 600 }}>{scenario.name}</div>
                  <div
                    style={{
                      fontSize: 12,
                      color: "var(--text-3)",
                      marginTop: 3,
                      maxWidth: 520,
                      lineHeight: 1.5,
                    }}
                  >
                    {scenario.desc}
                  </div>
                </div>
                <div style={{ display: "flex", alignItems: "center", gap: 8, flex: "none" }}>
                  <div className="seg">
                    <button
                      className={view === "steps" ? "active" : ""}
                      onClick={() => setView("steps")}
                    >
                      Steps
                    </button>
                    <button
                      className={view === "timeline" ? "active" : ""}
                      onClick={() => setView("timeline")}
                    >
                      Timeline
                    </button>
                  </div>
                  {isCustom(scenario) && (
                    <>
                      <button
                        className="btn ghost sm"
                        onClick={() => {
                          setEditing(scenario);
                          setBuilderOpen(true);
                        }}
                        disabled={running}
                      >
                        <Icons.Sliders size={14} /> Edit
                      </button>
                      <button
                        className="btn ghost sm"
                        onClick={() => setConfirmDelete(scenario)}
                        disabled={running}
                        title="Delete scenario"
                      >
                        <Icons.Trash size={14} />
                      </button>
                    </>
                  )}
                </div>
              </div>
              {view === "timeline" && (
                <RunGantt
                  techniques={scenario.techniques}
                  stepState={stepState}
                  timeline={timeline}
                  nowMs={ganttNowMs}
                  routeFor={routeFor}
                />
              )}
              <div style={{ padding: "8px 0", display: view === "timeline" ? "none" : undefined }}>
                {scenario.techniques.map((t, i) => {
                  const st: StepStatus = stepState[i] || "pending";
                  const m = ST_META[st];
                  const hue = TACTIC_TONE[t.tactic] || 240;
                  const Ico = Icons[m.icon] || Icons.Dot;
                  const route = routeFor(t);
                  return (
                    <div
                      key={t.id}
                      onClick={() => setDetailIdx(i)}
                      className="tech-row"
                      title="View technique detail"
                      style={{
                        display: "flex",
                        gap: 14,
                        padding: "11px 18px",
                        position: "relative",
                        cursor: "pointer",
                        opacity: st === "manual" ? 0.7 : 1,
                      }}
                    >
                      {/* rail */}
                      <div
                        style={{
                          display: "flex",
                          flexDirection: "column",
                          alignItems: "center",
                          flex: "none",
                        }}
                      >
                        <div
                          style={{
                            width: 26,
                            height: 26,
                            borderRadius: 99,
                            display: "grid",
                            placeItems: "center",
                            flex: "none",
                            background:
                              st === "pending" || st === "manual"
                                ? "var(--surface-3)"
                                : `color-mix(in oklch, ${m.c} 16%, var(--surface))`,
                            border: `1.5px solid ${
                              st === "pending" ? "var(--border-2)" : st === "manual" ? "var(--border-2)" : m.c
                            }`,
                            color: m.c,
                            transition: "all .3s",
                          }}
                        >
                          {st === "running" ? (
                            <span
                              style={{
                                width: 9,
                                height: 9,
                                borderRadius: 99,
                                border: "2px solid currentColor",
                                borderTopColor: "transparent",
                                animation: "spin 0.7s linear infinite",
                                display: "block",
                              }}
                            />
                          ) : (
                            <Ico size={14} />
                          )}
                        </div>
                        {i < scenario.techniques.length - 1 && (
                          <div
                            style={{
                              width: 2,
                              flex: 1,
                              minHeight: 14,
                              background: "var(--border)",
                              marginTop: 2,
                            }}
                          />
                        )}
                      </div>
                      {/* content */}
                      <div style={{ flex: 1, minWidth: 0, paddingBottom: 2 }}>
                        <div
                          style={{
                            display: "flex",
                            alignItems: "center",
                            gap: 9,
                            flexWrap: "wrap",
                          }}
                        >
                          <span style={{ fontSize: 13.5, fontWeight: 600 }}>{t.name}</span>
                          <span
                            className="mono"
                            style={{
                              fontSize: 10.5,
                              color: `oklch(0.5 0.08 ${hue})`,
                              background: `oklch(0.96 0.025 ${hue})`,
                              border: `1px solid oklch(0.9 0.04 ${hue})`,
                              borderRadius: 4,
                              padding: "1px 6px",
                            }}
                          >
                            {t.id}
                          </span>
                          {route ? (
                            <span
                              className="pill ok"
                              style={{ height: 19, fontSize: 10.5 }}
                              title={`Routed to ${route.c2.frameworkName} (${route.c2.name})${route.agent ? " · agent " + route.agent.host : ""}`}
                            >
                              <Icons.Bolt size={10} /> {route.c2.frameworkName}
                              {route.agent ? ` · ${route.agent.host}` : ""}
                            </span>
                          ) : (
                            <span className="pill" style={{ height: 19, fontSize: 10.5 }}>
                              <Icons.Power size={10} /> manual
                            </span>
                          )}
                        </div>
                        <div style={{ fontSize: 11.5, color: "var(--text-3)", marginTop: 2 }}>
                          {t.tactic}
                        </div>
                      </div>
                      <div
                        style={{
                          flex: "none",
                          alignSelf: "center",
                          display: "flex",
                          alignItems: "center",
                          gap: 6,
                        }}
                      >
                        <span
                          style={{
                            fontSize: 11.5,
                            fontWeight: 500,
                            color: m.c,
                            display: "inline-flex",
                            alignItems: "center",
                            gap: 5,
                          }}
                        >
                          {st !== "pending" && st !== "running" && <Ico size={13} />}
                          {m.label}
                        </span>
                        {(st === "done" || st === "detected") && (
                          <select
                            className="select"
                            value={detection[i] ?? "none"}
                            onClick={(e) => e.stopPropagation()}
                            onChange={(e) => setDet(i, e.target.value as DetectionOutcome)}
                            title="Defender outcome (purple-team scoring)"
                            style={{
                              height: 24,
                              fontSize: 11,
                              padding: "0 6px",
                              color: `var(--${DET_META[detection[i] ?? "none"].cls === "danger" ? "danger" : DET_META[detection[i] ?? "none"].cls === "info" ? "info" : "ok"})`,
                            }}
                          >
                            {DET_OPTIONS.map((o) => (
                              <option key={o} value={o}>
                                {DET_META[o].label}
                              </option>
                            ))}
                          </select>
                        )}
                        <span style={{ color: "var(--text-4)" }}>
                          <Icons.ChevronRight size={15} />
                        </span>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          </div>

          {/* control rail */}
          <div
            style={{
              width: 280,
              flex: "none",
              position: "sticky",
              top: 0,
              display: "flex",
              flexDirection: "column",
              gap: 12,
            }}
          >
            {/* C2 target & agents */}
            <div className="card" style={{ padding: 16 }}>
              <div className="eyebrow" style={{ marginBottom: 10 }}>
                C2 target
              </div>
              {liveC2s.length > 0 ? (
                <div style={{ display: "flex", flexDirection: "column", gap: 7 }}>
                  {/* Auto-route option: find the right agent per technique. */}
                  <button
                    onClick={() => !running && setC2Id(AUTO)}
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 9,
                      padding: "9px 11px",
                      borderRadius: "var(--r-sm)",
                      textAlign: "left",
                      border: `1px solid ${c2Id === AUTO ? "var(--accent)" : "var(--border-2)"}`,
                      background: c2Id === AUTO ? "var(--accent-soft)" : "var(--surface-inset)",
                      boxShadow: c2Id === AUTO ? "0 0 0 2px var(--accent-soft)" : "none",
                      cursor: running ? "default" : "pointer",
                    }}
                  >
                    <span style={{ color: c2Id === AUTO ? "var(--accent)" : "var(--text-3)" }}>
                      <Icons.Zap size={15} />
                    </span>
                    <span style={{ flex: 1, minWidth: 0 }}>
                      <span style={{ fontSize: 12.5, fontWeight: 600, display: "block" }}>
                        Auto-route
                      </span>
                      <span style={{ fontSize: 10.5, color: "var(--text-3)" }}>
                        best agent per technique
                      </span>
                    </span>
                    {c2Id === AUTO && (
                      <span style={{ color: "var(--accent)" }}>
                        <Icons.Check size={15} />
                      </span>
                    )}
                  </button>
                  {liveC2s.map((c) => {
                    const active = c.nodeId === c2Id;
                    return (
                      <button
                        key={c.nodeId}
                        onClick={() => !running && setC2Id(c.nodeId)}
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: 9,
                          padding: "9px 11px",
                          borderRadius: "var(--r-sm)",
                          textAlign: "left",
                          border: `1px solid ${active ? "var(--accent)" : "var(--border-2)"}`,
                          background: active ? "var(--accent-soft)" : "var(--surface-inset)",
                          boxShadow: active ? "0 0 0 2px var(--accent-soft)" : "none",
                          cursor: running ? "default" : "pointer",
                        }}
                      >
                        <span style={{ color: active ? "var(--accent)" : "var(--text-3)" }}>
                          <Icons.Server size={15} />
                        </span>
                        <span style={{ flex: 1, minWidth: 0 }}>
                          <span style={{ fontSize: 12.5, fontWeight: 600, display: "block" }}>
                            {c.frameworkName}
                          </span>
                          <span className="mono" style={{ fontSize: 10.5, color: "var(--text-3)" }}>
                            {c.name}
                          </span>
                        </span>
                        {active && (
                          <span style={{ color: "var(--accent)" }}>
                            <Icons.Check size={15} />
                          </span>
                        )}
                      </button>
                    );
                  })}
                </div>
              ) : (
                <div style={{ fontSize: 12, color: "var(--text-3)", padding: "4px 0" }}>
                  No live C2 server — deploy a teamserver in Infrastructure first.
                </div>
              )}

              {(selectedC2 ? [selectedC2] : liveC2s.filter((c) => c.operatorMode !== "manual")).length > 0 && (
                <div style={{ marginTop: 12, paddingTop: 12, borderTop: "1px solid var(--border)" }}>
                  <div className="eyebrow" style={{ marginBottom: 10 }}>
                    {selectedC2 ? "Agents" : "Available agents"}
                  </div>
                  <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
                    {(selectedC2 ? [selectedC2] : liveC2s.filter((c) => c.operatorMode !== "manual")).map((c) => (
                      <div key={c.nodeId}>
                        {!selectedC2 && (
                          <div style={{ fontSize: 11, fontWeight: 600, color: "var(--text-2)", marginBottom: 6 }}>
                            {c.frameworkName} · <span className="mono" style={{ color: "var(--text-3)" }}>{c.name}</span>
                          </div>
                        )}
                        <OperatorStatus d={c} />
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>

            {/* run control */}
            <div className="card" style={{ padding: 18 }}>
              <div className="eyebrow" style={{ marginBottom: 14 }}>
                Run control
              </div>
              {/* progress ring */}
              <div style={{ display: "flex", alignItems: "center", gap: 16, marginBottom: 16 }}>
                <div style={{ position: "relative", width: 62, height: 62 }}>
                  <svg width="62" height="62" style={{ transform: "rotate(-90deg)" }}>
                    <circle cx="31" cy="31" r="26" fill="none" stroke="var(--surface-3)" strokeWidth="6" />
                    <circle
                      cx="31"
                      cy="31"
                      r="26"
                      fill="none"
                      stroke="var(--accent)"
                      strokeWidth="6"
                      strokeLinecap="round"
                      strokeDasharray={circumference}
                      strokeDashoffset={circumference * (1 - pct / 100)}
                      style={{ transition: "stroke-dashoffset .4s" }}
                    />
                  </svg>
                  <div
                    className="mono"
                    style={{
                      position: "absolute",
                      inset: 0,
                      display: "grid",
                      placeItems: "center",
                      fontSize: 14,
                      fontWeight: 600,
                    }}
                  >
                    {pct}%
                  </div>
                </div>
                <div>
                  <div style={{ fontSize: 12.5, color: "var(--text-2)" }}>
                    <b style={{ color: "var(--text)" }}>{done}</b> / {runnableCount} automated
                  </div>
                  <div
                    style={{
                      fontSize: 12,
                      color: "var(--info)",
                      marginTop: 3,
                      display: "flex",
                      alignItems: "center",
                      gap: 5,
                    }}
                  >
                    <Icons.Eye size={13} /> {passed} passed (block/detect/alert)
                  </div>
                  <div style={{ fontSize: 12, color: "var(--text-2)", marginTop: 3 }}>
                    TRM <b style={{ color: liveTRM >= 80 ? "var(--ok)" : liveTRM >= 50 ? "var(--warn)" : "var(--danger)" }}>{liveTRM}%</b> of executed
                  </div>
                </div>
              </div>
              {running ? (
                <button
                  className="btn"
                  style={{ width: "100%", justifyContent: "center" }}
                  onClick={reset}
                >
                  <Icons.Pause size={15} /> Stop run
                </button>
              ) : (
                <button
                  className="btn primary"
                  style={{ width: "100%", justifyContent: "center" }}
                  onClick={run}
                  disabled={!canRun}
                >
                  <Icons.Play size={15} /> {done > 0 ? "Re-run scenario" : "Run scenario"}
                </button>
              )}
              <div
                style={{
                  fontSize: 11,
                  color: "var(--text-4)",
                  marginTop: 10,
                  textAlign: "center",
                  lineHeight: 1.5,
                }}
              >
                {liveC2s.length === 0
                  ? "No live C2 server — deploy a teamserver to run automated emulation."
                  : c2Id === AUTO
                  ? `Auto-routing each technique to a capable live agent. ${
                      scenario.techniques.length - runnableCount
                    } technique(s) have no capable C2 — run by hand.`
                  : selectedC2 && selectedC2.operatorMode === "manual"
                  ? `${selectedC2.frameworkName} is operated manually — drive these techniques from your native client.`
                  : selectedC2
                  ? `Executes via ${selectedC2.frameworkName} against ${activeEngagement.codename}. ${
                      scenario.techniques.length - runnableCount
                    } technique(s) run by hand.`
                  : ""}
              </div>
            </div>

            {/* supporting infrastructure */}
            <div className="card" style={{ padding: 16 }}>
              <div className="eyebrow" style={{ marginBottom: 10 }}>
                Supporting infrastructure
              </div>
              <div style={{ display: "flex", flexDirection: "column", gap: 9 }}>
                {targetInfra.length > 0 ? (
                  targetInfra.map(({ role, name, status }) => (
                    <div key={name} style={{ display: "flex", alignItems: "center", gap: 9 }}>
                      <span className={"status-dot " + status} />
                      <span style={{ fontSize: 12, color: "var(--text-2)", flex: 1 }}>{role}</span>
                      <span className="mono" style={{ fontSize: 11, color: "var(--text-3)" }}>
                        {name}
                      </span>
                    </div>
                  ))
                ) : (
                  <div
                    style={{
                      fontSize: 12,
                      color: "var(--text-3)",
                      textAlign: "center",
                      padding: "8px 0",
                    }}
                  >
                    No live redirectors or staging hosts.
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      </div>

      {detailIdx !== null && scenario.techniques[detailIdx] && (
        <TechniqueDetail
          technique={scenario.techniques[detailIdx]}
          c2={selectedC2 ?? routeFor(scenario.techniques[detailIdx])?.c2 ?? null}
          onClose={() => setDetailIdx(null)}
        />
      )}
      {importOpen && (
        <Modal open onClose={() => setImportOpen(false)} width={640} label="Import index">
          <div
            style={{
              padding: "16px 20px",
              borderBottom: "1px solid var(--border)",
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
            }}
          >
            <div>
              <div style={{ fontSize: 16, fontWeight: 600 }}>Import a benchmark index</div>
              <div style={{ fontSize: 12.5, color: "var(--text-3)", marginTop: 2 }}>
                Paste an SRA-format index (SecurityRiskAdvisors/indexes YAML). It becomes a
                scenario and its techniques are added to the TTP library.
              </div>
            </div>
            <button className="btn ghost sm" onClick={() => setImportOpen(false)} style={{ padding: 6 }}>
              <Icons.X size={16} />
            </button>
          </div>
          <div className="scroll" style={{ padding: 18 }}>
            <textarea
              className="input mono"
              value={importText}
              onChange={(e) => setImportText(e.target.value)}
              rows={14}
              spellCheck={false}
              style={{ width: "100%", resize: "vertical", fontSize: 12 }}
            />
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginTop: 12, gap: 10 }}>
              <button className="btn ghost sm" onClick={() => setImportText(SAMPLE_INDEX_YAML)}>
                Reset to example
              </button>
              <button
                className="btn primary"
                onClick={handleImportIndex}
                disabled={!importText.trim()}
              >
                <Icons.Plus size={15} /> Import
              </button>
            </div>
          </div>
        </Modal>
      )}
      {builderOpen && (
        <ScenarioBuilder
          initial={editing ?? undefined}
          onClose={() => {
            setBuilderOpen(false);
            setEditing(null);
          }}
          onSubmit={handleSubmitScenario}
        />
      )}
      {confirmDelete && (
        <Modal open onClose={() => setConfirmDelete(null)} width={420} label="Delete scenario">
          <div style={{ padding: 22 }}>
            <div style={{ fontSize: 15, fontWeight: 600, marginBottom: 8 }}>Delete scenario?</div>
            <div style={{ fontSize: 13, color: "var(--text-2)", lineHeight: 1.5 }}>
              <b>{confirmDelete.name}</b> will be permanently removed. This cannot be undone.
            </div>
            <div style={{ display: "flex", gap: 8, marginTop: 18, justifyContent: "flex-end" }}>
              <button className="btn ghost" onClick={() => setConfirmDelete(null)}>
                Cancel
              </button>
              <button className="btn danger" onClick={() => handleDeleteScenario(confirmDelete)}>
                <Icons.Trash size={15} /> Delete
              </button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
}
