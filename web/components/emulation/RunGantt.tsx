"use client";
import React from "react";
import type { Technique, DeployedC2, OperatorSession } from "../../lib/types";

type StepStatus = "pending" | "running" | "done" | "detected" | "manual";
type TimelineWindow = { start: number; end?: number };

const BAR_COLOR: Record<StepStatus, string> = {
  pending: "var(--border-2)",
  running: "var(--warn)",
  done: "var(--ok)",
  detected: "var(--info)",
  manual: "var(--text-4)",
};

// RunGantt renders the emulation run as a Gantt-style timeline: each technique is
// a bar placed on a shared time axis (start → finish), so the operator can watch
// the ordered TTP chain progress and see which steps overlap or run long.
export default function RunGantt({
  techniques,
  stepState,
  timeline,
  nowMs,
  routeFor,
}: {
  techniques: Technique[];
  stepState: Record<number, StepStatus>;
  timeline: Record<number, TimelineWindow>;
  nowMs: number;
  routeFor: (t: Technique) => { c2: DeployedC2; agent?: OperatorSession } | null;
}) {
  const total = Math.max(nowMs, 1);

  return (
    <div style={{ padding: "14px 18px 16px" }}>
      {techniques.map((t, i) => {
        const st: StepStatus = stepState[i] || "pending";
        const w = timeline[i];
        const start = w?.start ?? 0;
        const end = w?.end ?? (st === "running" ? nowMs : start);
        const leftPct = total > 0 ? (start / total) * 100 : 0;
        const widthPct = Math.max(((end - start) / total) * 100, st === "manual" ? 0 : 2);
        const route = routeFor(t);
        const dur = w?.end !== undefined ? `${((w.end - (w.start ?? 0)) / 1000).toFixed(1)}s` : st === "running" ? "running…" : "";

        return (
          <div key={t.id} style={{ display: "flex", alignItems: "center", gap: 10, padding: "5px 0" }}>
            {/* label */}
            <span style={{ width: 168, flex: "none", minWidth: 0 }}>
              <span style={{ display: "block", fontSize: 12, fontWeight: 500, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
                {t.name}
              </span>
              <span className="mono" style={{ fontSize: 10, color: "var(--text-3)" }}>
                {t.id}
                {route ? ` · ${route.c2.framework}` : st === "manual" ? " · manual" : ""}
              </span>
            </span>
            {/* track */}
            <span
              style={{
                flex: 1,
                position: "relative",
                height: 16,
                background: "var(--surface-3)",
                border: "1px solid var(--border)",
                borderRadius: 5,
                minWidth: 0,
              }}
            >
              {st === "manual" ? (
                <span style={{ position: "absolute", left: 6, top: 0, lineHeight: "16px", fontSize: 10, color: "var(--text-4)" }}>
                  run by hand
                </span>
              ) : w ? (
                <span
                  style={{
                    position: "absolute",
                    left: `${leftPct}%`,
                    width: `${widthPct}%`,
                    top: 0,
                    bottom: 0,
                    background: BAR_COLOR[st],
                    borderRadius: 4,
                    transition: "width .2s linear, left .2s linear",
                    opacity: st === "running" ? 0.85 : 1,
                  }}
                />
              ) : null}
            </span>
            {/* duration */}
            <span className="mono" style={{ width: 64, flex: "none", textAlign: "right", fontSize: 10.5, color: "var(--text-3)" }}>
              {dur}
            </span>
          </div>
        );
      })}
      <div className="eyebrow" style={{ marginTop: 12 }}>
        ordered TTP chain · elapsed {(total / 1000).toFixed(1)}s
      </div>
    </div>
  );
}
