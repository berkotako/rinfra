"use client";
import React, { useState, useRef, useEffect } from "react";
import { Icons } from "../icons";
import { PageHead } from "../ui";
import { useStore } from "../../lib/store";
import { SCENARIOS } from "../../lib/data";
import type { NodeStatus } from "../../lib/types";

type StepStatus = "pending" | "running" | "done" | "detected";

const ST_META: Record<StepStatus, { c: string; icon: string; label: string }> = {
  pending: { c: "var(--text-4)", icon: "Dot", label: "Queued" },
  running: { c: "var(--warn)", icon: "Activity", label: "Running" },
  done: { c: "var(--ok)", icon: "CheckCircle", label: "Executed" },
  detected: { c: "var(--info)", icon: "Eye", label: "Detected" },
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
  const { activeEngagement, nodes, pushToast } = useStore();
  const [scenarioId, setScenarioId] = useState(SCENARIOS[0].id);
  const [running, setRunning] = useState(false);
  const [stepState, setStepState] = useState<Record<number, StepStatus>>({});
  const timers = useRef<ReturnType<typeof setTimeout>[]>([]);

  const scenario = SCENARIOS.find((s) => s.id === scenarioId) || SCENARIOS[0];

  const reset = () => {
    timers.current.forEach(clearTimeout);
    timers.current = [];
    setStepState({});
    setRunning(false);
  };

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      timers.current.forEach(clearTimeout);
    };
  }, []);

  useEffect(() => {
    timers.current.forEach(clearTimeout);
    timers.current = [];
    setStepState({});
    setRunning(false);
  }, [scenarioId]);

  const run = () => {
    reset();
    setRunning(true);
    pushToast(`Emulation started — ${scenario.name}`, "info");
    const n = scenario.techniques.length;
    scenario.techniques.forEach((t, i) => {
      timers.current.push(
        setTimeout(
          () => setStepState((s) => ({ ...s, [i]: "running" })),
          i * 1500 + 200
        )
      );
      timers.current.push(
        setTimeout(
          () =>
            setStepState((s) => ({
              ...s,
              [i]: Math.random() < 0.28 ? "detected" : "done",
            })),
          i * 1500 + 1300
        )
      );
    });
    timers.current.push(
      setTimeout(() => {
        setRunning(false);
        pushToast("Emulation complete — results captured", "ok");
      }, n * 1500 + 600)
    );
  };

  const done = scenario.techniques.filter(
    (_, i) => stepState[i] === "done" || stepState[i] === "detected"
  ).length;
  const detected = scenario.techniques.filter(
    (_, i) => stepState[i] === "detected"
  ).length;
  const pct = Math.round((done / scenario.techniques.length) * 100);

  // Live infrastructure from actual topology state
  const liveNodes = nodes.filter((n) => n.status === "live");
  const targetInfra = [
    ...liveNodes
      .filter((n) => n.type === "c2_server")
      .map((n) => ({ role: "C2 channel", name: n.name, status: "live" as NodeStatus })),
    ...liveNodes
      .filter((n) => n.type === "redirector")
      .map((n) => ({ role: "Redirector", name: n.name, status: "live" as NodeStatus })),
    ...liveNodes
      .filter((n) => n.type === "payload_host")
      .map((n) => ({ role: "Staging", name: n.name, status: "live" as NodeStatus })),
  ];

  const circumference = 2 * Math.PI * 26;

  return (
    <div
      className="scroll"
      style={{ height: "100%", padding: "26px 32px 40px" }}
    >
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
                  boxShadow: active
                    ? "0 0 0 3px var(--accent-soft)"
                    : "var(--shadow-xs)",
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
                  <span
                    style={{
                      color: active ? "var(--accent)" : "var(--text-3)",
                    }}
                  >
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
                  <div style={{ fontSize: 14, fontWeight: 600 }}>
                    {scenario.name}
                  </div>
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
                  return (
                    <div
                      key={t.id}
                      style={{
                        display: "flex",
                        gap: 14,
                        padding: "11px 18px",
                        position: "relative",
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
                              st === "pending"
                                ? "var(--surface-3)"
                                : `color-mix(in oklch, ${m.c} 16%, var(--surface))`,
                            border: `1.5px solid ${st === "pending" ? "var(--border-2)" : m.c}`,
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
                      <div
                        style={{
                          flex: 1,
                          minWidth: 0,
                          paddingBottom: 2,
                        }}
                      >
                        <div
                          style={{
                            display: "flex",
                            alignItems: "center",
                            gap: 9,
                            flexWrap: "wrap",
                          }}
                        >
                          <span style={{ fontSize: 13.5, fontWeight: 600 }}>
                            {t.name}
                          </span>
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
                        </div>
                        <div
                          style={{
                            fontSize: 11.5,
                            color: "var(--text-3)",
                            marginTop: 2,
                          }}
                        >
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
                          {st !== "pending" && st !== "running" && (
                            <Ico size={13} />
                          )}
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
            <div className="card" style={{ padding: 18 }}>
              <div className="eyebrow" style={{ marginBottom: 14 }}>
                Run control
              </div>
              {/* progress ring */}
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 16,
                  marginBottom: 16,
                }}
              >
                <div
                  style={{
                    position: "relative",
                    width: 62,
                    height: 62,
                  }}
                >
                  <svg
                    width="62"
                    height="62"
                    style={{ transform: "rotate(-90deg)" }}
                  >
                    <circle
                      cx="31"
                      cy="31"
                      r="26"
                      fill="none"
                      stroke="var(--surface-3)"
                      strokeWidth="6"
                    />
                    <circle
                      cx="31"
                      cy="31"
                      r="26"
                      fill="none"
                      stroke="var(--accent)"
                      strokeWidth="6"
                      strokeLinecap="round"
                      strokeDasharray={circumference}
                      strokeDashoffset={
                        circumference * (1 - pct / 100)
                      }
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
                    <b style={{ color: "var(--text)" }}>{done}</b> /{" "}
                    {scenario.techniques.length} executed
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
                >
                  <Icons.Play size={15} />{" "}
                  {done > 0 ? "Re-run scenario" : "Run scenario"}
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
                Executes against live infrastructure in {activeEngagement.codename}.
              </div>
            </div>

            <div className="card" style={{ padding: 16 }}>
              <div className="eyebrow" style={{ marginBottom: 10 }}>
                Target infrastructure
              </div>
              <div
                style={{
                  display: "flex",
                  flexDirection: "column",
                  gap: 9,
                }}
              >
                {targetInfra.length > 0 ? (
                  targetInfra.map(({ role, name, status }) => (
                    <div
                      key={name}
                      style={{
                        display: "flex",
                        alignItems: "center",
                        gap: 9,
                      }}
                    >
                      <span className={"status-dot " + status} />
                      <span
                        style={{
                          fontSize: 12,
                          color: "var(--text-2)",
                          flex: 1,
                        }}
                      >
                        {role}
                      </span>
                      <span
                        className="mono"
                        style={{ fontSize: 11, color: "var(--text-3)" }}
                      >
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
                    No live infrastructure — deploy assets first.
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
