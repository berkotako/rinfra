"use client";
import React, { useEffect, useState } from "react";
import { Icons } from "../icons";
import { PageHead } from "../ui";
import { useStore } from "../../lib/store";
import { getClient } from "../../lib/client";
import type { Project, Coverage } from "../../lib/types";

// Format a tactic label, accepting both backend ("initial-access") and demo
// ("Initial Access") forms.
function tacticLabel(t: string): string {
  if (t.includes("-")) {
    return t
      .split("-")
      .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
      .join(" ");
  }
  return t;
}

const LVL_COLOR = [
  "var(--surface-3)",
  "oklch(0.9 0.05 168)",
  "oklch(0.78 0.08 168)",
  "oklch(0.64 0.09 168)",
];
const LVL_TEXT = ["var(--text-4)", "var(--text-2)", "#fff", "#fff"];

export default function ReportingScreen() {
  const { activeEngagement, engagements, activeEngagementId, setActiveEngagementId, pushToast } =
    useStore();
  const [tab, setTab] = useState<"coverage" | "report">("coverage");
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectId, setProjectId] = useState("all");

  useEffect(() => {
    let alive = true;
    getClient()
      .listProjects()
      .then((p) => alive && setProjects(p))
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, []);

  // Scope the engagement picker to the selected project (best-effort match on
  // client name, since the demo engagements aren't hard-linked to projects).
  const project = projects.find((p) => p.id === projectId);
  const scopedEngagements =
    project && engagements.some((e) => e.client === project.clientName)
      ? engagements.filter((e) => e.client === project.clientName)
      : engagements;

  // Coverage rollup from the data layer — real backend in REST mode, demo data
  // in mock mode (same render path either way).
  const [coverage, setCoverage] = useState<Coverage | null>(null);
  useEffect(() => {
    let alive = true;
    getClient()
      .getCoverage(activeEngagementId)
      .then((c) => alive && setCoverage(c))
      .catch(() => alive && setCoverage(null));
    return () => {
      alive = false;
    };
  }, [activeEngagementId]);

  const total = coverage?.totalTechniques ?? 0;
  const covered = coverage?.exercisedCount ?? 0;
  const executed = coverage?.executedCount ?? 0;
  const validated = coverage?.validatedCount ?? 0;
  const coveragePct = total > 0 ? Math.round((covered / total) * 100) : 0;

  const copyNavigator = () => {
    getClient()
      .getNavigatorLayer(activeEngagementId)
      .then((layer) => navigator.clipboard?.writeText(JSON.stringify(layer, null, 2)))
      .then(() => pushToast("ATT&CK Navigator layer copied to clipboard", "ok"))
      .catch(() => pushToast("Could not export Navigator layer", "danger"));
  };

  return (
    <div
      className="scroll"
      style={{ height: "100%", padding: "26px 32px 40px" }}
    >
      <div style={{ maxWidth: 1180, margin: "0 auto" }}>
        <PageHead
          title="Coverage & reporting"
          sub={`ATT&CK coverage and engagement reporting for ${activeEngagement.client} · ${activeEngagement.codename}`}
        >
          <div className="seg">
            <button
              className={tab === "coverage" ? "active" : ""}
              onClick={() => setTab("coverage")}
            >
              Coverage matrix
            </button>
            <button
              className={tab === "report" ? "active" : ""}
              onClick={() => setTab("report")}
            >
              Engagement report
            </button>
          </div>
          <button className="btn">
            <Icons.FileText size={15} /> Export PDF
          </button>
        </PageHead>

        {tab === "coverage" && (
          <div className="fade-in">
            {/* project + engagement scope */}
            <div
              className="card"
              style={{
                display: "flex",
                alignItems: "center",
                gap: 14,
                flexWrap: "wrap",
                padding: "12px 16px",
                marginBottom: 14,
              }}
            >
              <span className="eyebrow">Scope</span>
              <label style={{ display: "flex", alignItems: "center", gap: 7, fontSize: 12.5 }}>
                <Icons.Building size={14} />
                <select
                  className="select"
                  value={projectId}
                  onChange={(e) => setProjectId(e.target.value)}
                  style={{ minWidth: 200 }}
                >
                  <option value="all">All projects</option>
                  {projects.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                </select>
              </label>
              <label style={{ display: "flex", alignItems: "center", gap: 7, fontSize: 12.5 }}>
                <Icons.Target size={14} />
                <select
                  className="select"
                  value={activeEngagementId}
                  onChange={(e) => setActiveEngagementId(e.target.value)}
                  style={{ minWidth: 220 }}
                >
                  {scopedEngagements.map((e) => (
                    <option key={e.id} value={e.id}>
                      {e.codename} · {e.client}
                    </option>
                  ))}
                </select>
              </label>
            </div>

            <div style={{ display: "flex", gap: 12, marginBottom: 18 }}>
              {(
                [
                  ["Techniques exercised", `${covered} / ${total}`, "var(--accent)"],
                  ["Executed (2+ coverage)", executed, "var(--ok)"],
                  ["Validated", validated, "var(--info)"],
                  ["Coverage score", `${coveragePct}%`, "var(--text)"],
                ] as [string, string | number, string][]
              ).map(([l, v, c], i) => (
                <div
                  key={i}
                  className="card"
                  style={{ padding: "13px 16px", flex: 1 }}
                >
                  <div style={{ fontSize: 11.5, color: "var(--text-3)" }}>
                    {l}
                  </div>
                  <div
                    className="mono"
                    style={{
                      fontSize: 20,
                      fontWeight: 600,
                      marginTop: 5,
                      color: c,
                    }}
                  >
                    {v}
                  </div>
                </div>
              ))}
            </div>

            <div className="card" style={{ padding: 18, overflowX: "auto" }}>
              <div style={{ display: "flex", gap: 10, minWidth: 880 }}>
                {(coverage?.tactics ?? []).map((tac) => (
                  <div key={tac.tactic} style={{ flex: 1, minWidth: 96 }}>
                    <div
                      style={{
                        fontSize: 11,
                        fontWeight: 600,
                        color: "var(--text-2)",
                        marginBottom: 8,
                        height: 30,
                        lineHeight: 1.25,
                      }}
                    >
                      {tacticLabel(tac.tactic)}
                    </div>
                    <div
                      style={{
                        display: "flex",
                        flexDirection: "column",
                        gap: 5,
                      }}
                    >
                      {tac.techniques.map((te, i) => (
                        <div
                          key={te.attackID + i}
                          title={`${te.name} (${te.attackID}) — coverage ${te.level}/3`}
                          style={{
                            padding: "7px 8px",
                            borderRadius: 6,
                            fontSize: 10.5,
                            lineHeight: 1.2,
                            cursor: "default",
                            background: LVL_COLOR[te.level],
                            color: LVL_TEXT[te.level],
                            border:
                              te.level === 0
                                ? "1px solid var(--border)"
                                : "1px solid transparent",
                            fontWeight: te.level >= 2 ? 500 : 400,
                            transition: "transform .1s",
                          }}
                          onMouseEnter={(e) =>
                            ((e.currentTarget as HTMLElement).style.transform =
                              "translateY(-1px)")
                          }
                          onMouseLeave={(e) =>
                            ((e.currentTarget as HTMLElement).style.transform =
                              "none")
                          }
                        >
                          {te.name}
                        </div>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
              {/* legend */}
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 18,
                  marginTop: 18,
                  paddingTop: 14,
                  borderTop: "1px solid var(--border)",
                }}
              >
                <span style={{ fontSize: 11.5, color: "var(--text-3)" }}>
                  Coverage
                </span>
                {(
                  [
                    ["Not exercised", 0],
                    ["Attempted", 1],
                    ["Executed", 2],
                    ["Validated", 3],
                  ] as [string, number][]
                ).map(([l, lvl]) => (
                  <div
                    key={lvl}
                    style={{ display: "flex", alignItems: "center", gap: 7 }}
                  >
                    <span
                      style={{
                        width: 14,
                        height: 14,
                        borderRadius: 4,
                        background: LVL_COLOR[lvl],
                        border:
                          lvl === 0
                            ? "1px solid var(--border)"
                            : "none",
                      }}
                    />
                    <span
                      style={{ fontSize: 11.5, color: "var(--text-2)" }}
                    >
                      {l}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          </div>
        )}

        {tab === "report" && (
          <div
            className="fade-in"
            style={{
              display: "flex",
              gap: 22,
              alignItems: "flex-start",
            }}
          >
            {/* report doc */}
            <div
              className="card"
              style={{
                flex: "1 1 0",
                minWidth: 0,
                padding: "34px 40px",
                boxShadow: "var(--shadow-md)",
              }}
            >
              <div
                style={{
                  display: "flex",
                  justifyContent: "space-between",
                  alignItems: "flex-start",
                  paddingBottom: 22,
                  borderBottom: "1px solid var(--border)",
                }}
              >
                <div>
                  <div
                    className="eyebrow"
                    style={{ color: "var(--accent)" }}
                  >
                    Engagement report · Confidential
                  </div>
                  <h2
                    style={{
                      fontSize: 24,
                      fontWeight: 600,
                      marginTop: 8,
                      letterSpacing: "-0.02em",
                    }}
                  >
                    {activeEngagement.client}
                  </h2>
                  <div
                    style={{
                      fontSize: 13,
                      color: "var(--text-3)",
                      marginTop: 4,
                    }}
                  >
                    {activeEngagement.scope} · Codename{" "}
                    {activeEngagement.codename}
                  </div>
                </div>
                <div
                  style={{
                    textAlign: "right",
                    fontSize: 12,
                    color: "var(--text-3)",
                  }}
                  className="mono"
                >
                  <div>{activeEngagement.id}</div>
                  <div style={{ marginTop: 3 }}>
                    {activeEngagement.start} → {activeEngagement.end}
                  </div>
                </div>
              </div>

              <div style={{ marginTop: 24 }}>
                <h3
                  style={{
                    fontSize: 14,
                    fontWeight: 600,
                    marginBottom: 10,
                  }}
                >
                  Executive summary
                </h3>
                <p
                  style={{
                    fontSize: 13.5,
                    color: "var(--text-2)",
                    lineHeight: 1.65,
                  }}
                >
                  Over the engagement window, the team established resilient
                  command-and-control infrastructure across{" "}
                  {activeEngagement.assets} cloud assets and exercised adversary
                  tradecraft mapped to MITRE ATT&CK. Initial access was achieved
                  via spearphishing; the team demonstrated credential access and
                  lateral movement to high-value segments without disrupting
                  production services, consistent with the agreed rules of
                  engagement.
                </p>
              </div>

              <div
                style={{
                  marginTop: 22,
                  display: "grid",
                  gridTemplateColumns: "1fr 1fr",
                  gap: 14,
                }}
              >
                {(
                  [
                    ["Critical findings", "3", "var(--danger)"],
                    ["High findings", "5", "var(--warn)"],
                    ["Techniques validated", "14", "var(--accent)"],
                    ["Mean time to detect", "4h 12m", "var(--info)"],
                  ] as [string, string, string][]
                ).map(([l, v, c], i) => (
                  <div
                    key={i}
                    style={{
                      padding: "13px 15px",
                      borderRadius: "var(--r-md)",
                      background: "var(--surface-2)",
                      border: "1px solid var(--border)",
                    }}
                  >
                    <div style={{ fontSize: 11.5, color: "var(--text-3)" }}>
                      {l}
                    </div>
                    <div
                      className="mono"
                      style={{
                        fontSize: 19,
                        fontWeight: 600,
                        color: c,
                        marginTop: 4,
                      }}
                    >
                      {v}
                    </div>
                  </div>
                ))}
              </div>

              <div style={{ marginTop: 24 }}>
                <h3
                  style={{
                    fontSize: 14,
                    fontWeight: 600,
                    marginBottom: 12,
                  }}
                >
                  Key findings
                </h3>
                {(
                  [
                    [
                      "critical",
                      "Domain compromise via Kerberoasting",
                      "Credential Access · T1558.003",
                      "A service account with a weak password enabled escalation to Domain Admin within 6 hours.",
                    ],
                    [
                      "high",
                      "Unrestricted egress permitted C2 over HTTPS",
                      "Exfiltration · T1071.001",
                      "Outbound traffic to categorized domains was not inspected, allowing stable beaconing.",
                    ],
                    [
                      "high",
                      "EDR bypass via process injection",
                      "Defense Evasion · T1055",
                      "Reflective loading evaded endpoint detection on three host builds.",
                    ],
                  ] as [string, string, string, string][]
                ).map(([sev, title, tag, desc], i) => (
                  <div
                    key={i}
                    style={{
                      display: "flex",
                      gap: 13,
                      padding: "13px 0",
                      borderTop: "1px solid var(--border)",
                    }}
                  >
                    <span
                      className={"pill " + (sev === "critical" ? "danger" : "warn")}
                      style={{
                        height: 22,
                        flex: "none",
                        textTransform: "capitalize",
                      }}
                    >
                      {sev}
                    </span>
                    <div>
                      <div style={{ fontSize: 13.5, fontWeight: 600 }}>
                        {title}
                      </div>
                      <div
                        className="mono"
                        style={{
                          fontSize: 11,
                          color: "var(--text-3)",
                          margin: "3px 0 5px",
                        }}
                      >
                        {tag}
                      </div>
                      <div
                        style={{
                          fontSize: 12.5,
                          color: "var(--text-2)",
                          lineHeight: 1.55,
                        }}
                      >
                        {desc}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>

            {/* meta rail */}
            <div
              style={{
                width: 240,
                flex: "none",
                display: "flex",
                flexDirection: "column",
                gap: 12,
                position: "sticky",
                top: 0,
              }}
            >
              <div className="card" style={{ padding: 16 }}>
                <div className="eyebrow" style={{ marginBottom: 12 }}>
                  Report metadata
                </div>
                {(
                  [
                    ["Lead operator", activeEngagement.lead],
                    ["Authorized by", activeEngagement.authBy],
                    ["Status", "Final draft"],
                    ["Classification", "Confidential"],
                  ] as [string, string][]
                ).map(([l, v]) => (
                  <div
                    key={l}
                    style={{
                      display: "flex",
                      justifyContent: "space-between",
                      gap: 10,
                      padding: "6px 0",
                      fontSize: 12,
                    }}
                  >
                    <span style={{ color: "var(--text-3)" }}>{l}</span>
                    <span
                      style={{
                        color: "var(--text-2)",
                        fontWeight: 500,
                        textAlign: "right",
                      }}
                    >
                      {v}
                    </span>
                  </div>
                ))}
              </div>
              <button
                className="btn primary"
                style={{ justifyContent: "center" }}
              >
                <Icons.FileText size={15} /> Generate full report
              </button>
              <button
                className="btn"
                style={{ justifyContent: "center" }}
                onClick={copyNavigator}
              >
                <Icons.Copy size={15} /> Copy ATT&CK Navigator JSON
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
