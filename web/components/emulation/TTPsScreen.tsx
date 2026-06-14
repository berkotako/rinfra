"use client";
import React, { useMemo, useState } from "react";
import { Icons } from "../icons";
import { PageHead } from "../ui";
import TechniqueDetail from "./TechniqueDetail";
import {
  TECHNIQUE_LIBRARY,
  TACTIC_ORDER,
  C2_FRAMEWORKS,
  C2_TACTIC_SUPPORT,
  c2SupportsTactic,
  frameworksSupportingTactic,
} from "../../lib/data";
import type { Technique } from "../../lib/types";

// Frameworks that automate at least one tactic (exclude fronted), for the filter.
const AUTOMATING = C2_FRAMEWORKS.filter((f) => C2_TACTIC_SUPPORT[f.id]);

export default function TTPsScreen() {
  const [framework, setFramework] = useState<string>("all");
  const [detail, setDetail] = useState<Technique | null>(null);

  const groups = useMemo(
    () =>
      TACTIC_ORDER.map((tactic) => ({
        tactic,
        items: TECHNIQUE_LIBRARY.filter((t) => t.tactic === tactic),
      })).filter((g) => g.items.length > 0),
    []
  );

  const totalAutomatable =
    framework === "all"
      ? TECHNIQUE_LIBRARY.filter((t) => frameworksSupportingTactic(t.tactic).length > 0).length
      : TECHNIQUE_LIBRARY.filter((t) => c2SupportsTactic(framework, t.tactic)).length;

  return (
    <div className="scroll" style={{ height: "100%", padding: "26px 32px 40px" }}>
      <div style={{ maxWidth: 1100, margin: "0 auto" }}>
        <PageHead
          title="TTPs"
          sub="The portable technique library, mapped to what each C2 framework can automate. Click a technique for its procedure."
        >
          <select
            className="select"
            value={framework}
            onChange={(e) => setFramework(e.target.value)}
            style={{ minWidth: 180 }}
          >
            <option value="all">All frameworks</option>
            {AUTOMATING.map((f) => (
              <option key={f.id} value={f.id}>
                {f.name}
              </option>
            ))}
          </select>
        </PageHead>

        {/* summary */}
        <div style={{ display: "flex", gap: 12, marginBottom: 18 }}>
          {(
            [
              ["Techniques", String(TECHNIQUE_LIBRARY.length), "var(--text)"],
              ["Tactics", String(groups.length), "var(--accent)"],
              [
                framework === "all" ? "Automatable (any C2)" : "Automatable by selection",
                String(totalAutomatable),
                "var(--ok)",
              ],
            ] as [string, string, string][]
          ).map(([l, v, c]) => (
            <div key={l} className="card" style={{ padding: "13px 16px", flex: 1 }}>
              <div style={{ fontSize: 11.5, color: "var(--text-3)" }}>{l}</div>
              <div className="mono" style={{ fontSize: 20, fontWeight: 600, marginTop: 5, color: c }}>
                {v}
              </div>
            </div>
          ))}
        </div>

        <div style={{ display: "flex", flexDirection: "column", gap: 18 }}>
          {groups.map((g) => {
            const supIds =
              framework === "all"
                ? frameworksSupportingTactic(g.tactic)
                : c2SupportsTactic(framework, g.tactic)
                ? [framework]
                : [];
            return (
              <div key={g.tactic} className="card" style={{ padding: 16 }}>
                <div style={{ display: "flex", alignItems: "center", gap: 9, marginBottom: 12 }}>
                  <span style={{ fontSize: 14, fontWeight: 600 }}>{g.tactic}</span>
                  <span className={"pill " + (supIds.length > 0 ? "ok" : "")} style={{ height: 20 }}>
                    {supIds.length > 0
                      ? framework === "all"
                        ? `${supIds.length} C2s automate`
                        : "automated"
                      : framework === "all"
                      ? "manual only"
                      : "manual for this C2"}
                  </span>
                </div>
                <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
                  {g.items.map((t) => {
                    const fwIds = frameworksSupportingTactic(t.tactic);
                    const auto = framework === "all" ? fwIds.length > 0 : c2SupportsTactic(framework, t.tactic);
                    return (
                      <button
                        key={t.id}
                        onClick={() => setDetail(t)}
                        className="tech-row"
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: 11,
                          padding: "9px 11px",
                          borderRadius: "var(--r-sm)",
                          border: "1px solid var(--border-2)",
                          background: "var(--surface-inset)",
                          textAlign: "left",
                          cursor: "pointer",
                          opacity: framework !== "all" && !auto ? 0.55 : 1,
                        }}
                      >
                        <span style={{ flex: 1, minWidth: 0 }}>
                          <span style={{ fontSize: 13, fontWeight: 500 }}>{t.name}</span>{" "}
                          <span className="mono" style={{ fontSize: 10.5, color: "var(--text-3)" }}>
                            {t.id}
                          </span>
                        </span>
                        {/* framework chips */}
                        <span style={{ display: "flex", gap: 5, flexWrap: "wrap", justifyContent: "flex-end" }}>
                          {framework === "all" ? (
                            C2_FRAMEWORKS.filter((f) => fwIds.includes(f.id))
                              .slice(0, 4)
                              .map((f) => (
                                <span
                                  key={f.id}
                                  className="mono"
                                  style={{
                                    fontSize: 10,
                                    color: "var(--text-3)",
                                    background: "var(--surface-3)",
                                    border: "1px solid var(--border)",
                                    borderRadius: 4,
                                    padding: "1px 6px",
                                  }}
                                >
                                  {f.name}
                                </span>
                              ))
                          ) : (
                            <span className={"pill " + (auto ? "ok" : "")} style={{ height: 19, fontSize: 10.5 }}>
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
                          {framework === "all" && fwIds.length > 4 && (
                            <span style={{ fontSize: 10, color: "var(--text-4)" }}>+{fwIds.length - 4}</span>
                          )}
                          {framework === "all" && fwIds.length === 0 && (
                            <span className="pill" style={{ height: 19, fontSize: 10.5 }}>
                              <Icons.Power size={10} /> manual
                            </span>
                          )}
                        </span>
                        <span style={{ color: "var(--text-4)", flex: "none" }}>
                          <Icons.ChevronRight size={15} />
                        </span>
                      </button>
                    );
                  })}
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {detail && <TechniqueDetail technique={detail} onClose={() => setDetail(null)} />}
    </div>
  );
}
