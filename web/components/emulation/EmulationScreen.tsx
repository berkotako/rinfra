"use client";
import React, { useState, useRef, useEffect, useCallback, useMemo } from "react";
import { Icons } from "../icons";
import { PageHead } from "../ui";
import { OperatorStatus } from "../c2/ManualAccess";
import { useStore } from "../../lib/store";
import { getClient, isRestMode } from "../../lib/client";
import { SCENARIOS, deployedC2FromNode, c2SupportsTechnique } from "../../lib/data";
import type { NodeStatus, Technique, DeployedC2 } from "../../lib/types";

type StepStatus = "pending" | "running" | "done" | "detected" | "manual";

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

export default function EmulationScreen() {
  const { activeEngagement, activeEngagementId, nodes, pushToast, apiStartRun } = useStore();
  const [scenarioId, setScenarioId] = useState(SCENARIOS[0].id);
  const [c2Id, setC2Id] = useState<string>("");
  const [running, setRunning] = useState(false);
  const [stepState, setStepState] = useState<Record<number, StepStatus>>({});
  const timers = useRef<ReturnType<typeof setTimeout>[]>([]);
  const runIdRef = useRef<string | null>(null);
  const restMode = isRestMode();
  const client = getClient();

  const scenario = SCENARIOS.find((s) => s.id === scenarioId) || SCENARIOS[0];

  // Deployed C2 teamservers from the live topology — the emulation targets.
  const c2Targets = useMemo(
    () => nodes.map(deployedC2FromNode).filter((d): d is DeployedC2 => d !== null),
    [nodes]
  );
  const liveC2s = useMemo(() => c2Targets.filter((c) => c.status === "live"), [c2Targets]);
  const selectedC2 = c2Targets.find((c) => c.nodeId === c2Id);

  // Whether the selected C2 can automate a given technique.
  const supports = useCallback(
    (techId: string) => !!selectedC2 && c2SupportsTechnique(selectedC2.framework, techId),
    [selectedC2]
  );

  // Default the C2 target to the first live, automatable teamserver.
  useEffect(() => {
    if (liveC2s.length === 0) {
      if (c2Id) setC2Id("");
      return;
    }
    if (!liveC2s.some((c) => c.nodeId === c2Id)) {
      const pref = liveC2s.find((c) => c.operatorMode !== "manual") ?? liveC2s[0];
      setC2Id(pref.nodeId);
    }
  }, [liveC2s, c2Id]);

  // Base step state for the current scenario + C2: unsupported techniques are
  // pre-marked "manual" so the TTP <-> C2 mapping is visible before any run.
  const baseStepState = useCallback((): Record<number, StepStatus> => {
    const b: Record<number, StepStatus> = {};
    scenario.techniques.forEach((t, i) => {
      if (!supports(t.id)) b[i] = "manual";
    });
    return b;
  }, [scenario, supports]);

  const reset = () => {
    timers.current.forEach(clearTimeout);
    timers.current = [];
    setStepState(baseStepState());
    setRunning(false);
  };

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      timers.current.forEach(clearTimeout);
    };
  }, []);

  // Re-derive the resting view whenever the scenario or C2 target changes.
  useEffect(() => {
    if (running) return;
    timers.current.forEach(clearTimeout);
    timers.current = [];
    setStepState(baseStepState());
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
              "done";
            setStepState((s) => ({ ...s, [idx]: st }));
          }
        } else if (status === "success" || status === "done" || status === "failed") {
          setRunning(false);
          pushToast("Emulation complete — results captured", "ok");
          unsubscribe();
        }
      });
      return unsubscribe;
    },
    [client, pushToast]
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
                "done";
              setStepState((s) => ({ ...s, [idx]: st }));
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
    [client, pushToast]
  );

  const run = () => {
    if (!selectedC2 || runnableCount === 0) return;
    timers.current.forEach(clearTimeout);
    timers.current = [];
    setStepState(baseStepState());
    setRunning(true);
    pushToast(`Emulation started — ${scenario.name} via ${selectedC2.frameworkName}`, "info");

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
      .filter(({ t }) => supports(t.id));
    runnable.forEach(({ i }, order) => {
      timers.current.push(
        setTimeout(() => setStepState((s) => ({ ...s, [i]: "running" })), order * 1500 + 200)
      );
      timers.current.push(
        setTimeout(
          () =>
            setStepState((s) => ({
              ...s,
              [i]: Math.random() < 0.28 ? "detected" : "done",
            })),
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

  const runnableCount = scenario.techniques.filter((t) => supports(t.id)).length;
  const done = scenario.techniques.filter(
    (_, i) => stepState[i] === "done" || stepState[i] === "detected"
  ).length;
  const detected = scenario.techniques.filter((_, i) => stepState[i] === "detected").length;
  const pct = runnableCount ? Math.round((done / runnableCount) * 100) : 0;
  const canRun = !!selectedC2 && runnableCount > 0;

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
        />

        {/* scenario picker */}
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(3,1fr)",
            gap: 12,
            marginBottom: 22,
          }}
        >
          {SCENARIOS.map((s) => {
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
                  <span className="pill" style={{ height: 20 }}>
                    {s.techniques.length} techniques
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
              </div>
              <div style={{ padding: "8px 0" }}>
                {scenario.techniques.map((t, i) => {
                  const st: StepStatus = stepState[i] || "pending";
                  const m = ST_META[st];
                  const hue = TACTIC_TONE[t.tactic] || 240;
                  const Ico = Icons[m.icon] || Icons.Dot;
                  const auto = supports(t.id);
                  return (
                    <div
                      key={t.id}
                      style={{
                        display: "flex",
                        gap: 14,
                        padding: "11px 18px",
                        position: "relative",
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
                          {selectedC2 && (
                            <span
                              className={"pill " + (auto ? "ok" : "")}
                              style={{ height: 19, fontSize: 10.5 }}
                            >
                              {auto ? (
                                <>
                                  <Icons.Bolt size={10} /> auto
                                </>
                              ) : (
                                <>
                                  <Icons.Power size={10} /> manual
                                </>
                              )}
                            </span>
                          )}
                        </div>
                        <div style={{ fontSize: 11.5, color: "var(--text-3)", marginTop: 2 }}>
                          {t.tactic}
                        </div>
                      </div>
                      <div style={{ flex: "none", alignSelf: "center" }}>
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

              {selectedC2 && (
                <div style={{ marginTop: 12, paddingTop: 12, borderTop: "1px solid var(--border)" }}>
                  <div className="eyebrow" style={{ marginBottom: 10 }}>
                    Agents
                  </div>
                  <OperatorStatus d={selectedC2} />
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
                    <Icons.Eye size={13} /> {detected} detected by blue team
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
                {!selectedC2
                  ? "Select a live C2 server to run automated emulation."
                  : selectedC2.operatorMode === "manual"
                  ? `${selectedC2.frameworkName} is operated manually — drive these techniques from your native client.`
                  : `Executes via ${selectedC2.frameworkName} against ${activeEngagement.codename}. ${
                      scenario.techniques.length - runnableCount
                    } technique(s) run by hand.`}
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
    </div>
  );
}
