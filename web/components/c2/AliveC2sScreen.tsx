"use client";
import React, { useEffect, useMemo, useState } from "react";
import { Icons } from "../icons";
import { PageHead, StatusPill, TierBadge, EmptyState } from "../ui";
import { useStore } from "../../lib/store";
import { getClient } from "../../lib/client";
import TerminalPane from "./Terminal";
import type { DeployedC2 } from "../../lib/types";

const TIER_LABEL: Record<string, string> = {
  orchestrated: "Orchestrated",
  scripted: "Scripted",
  fronted: "Fronted",
};

export default function AliveC2sScreen() {
  const { activeEngagementId, activeEngagement } = useStore();
  const [c2s, setC2s] = useState<DeployedC2[] | null>(null);
  const [open, setOpen] = useState<Set<string>>(new Set());

  useEffect(() => {
    let alive = true;
    getClient()
      .listDeployedC2(activeEngagementId)
      .then((d) => alive && setC2s(d))
      .catch(() => alive && setC2s([]));
    return () => {
      alive = false;
    };
  }, [activeEngagementId]);

  const liveC2s = useMemo(() => (c2s ?? []).filter((c) => c.status === "live"), [c2s]);

  const toggle = (id: string) =>
    setOpen((s) => {
      const next = new Set(s);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  const openAll = () => setOpen(new Set(liveC2s.map((c) => c.nodeId)));
  const closeAll = () => setOpen(new Set());

  const openList = liveC2s.filter((c) => open.has(c.nodeId));

  return (
    <div className="scroll" style={{ height: "100%", padding: "26px 32px 40px" }}>
      <div style={{ maxWidth: 1320, margin: "0 auto" }}>
        <PageHead
          title="Alive C2s"
          sub={`Live teamservers for ${activeEngagement.codename}. Open web shells side by side and operate several frameworks at once.`}
        >
          {liveC2s.length > 0 && (
            <>
              <button className="btn" onClick={openAll}>
                <Icons.Terminal size={15} /> Open all shells
              </button>
              {openList.length > 0 && (
                <button className="btn ghost" onClick={closeAll}>
                  <Icons.X size={15} /> Close all
                </button>
              )}
            </>
          )}
        </PageHead>

        {liveC2s.length === 0 ? (
          <div className="card">
            <EmptyState
              icon="Server"
              title="No live C2 servers"
              body="Deploy a teamserver in Infrastructure and bring it live to open a web shell here."
            />
          </div>
        ) : (
          <>
            {/* selector row */}
            <div style={{ display: "flex", flexWrap: "wrap", gap: 10, marginBottom: 18 }}>
              {liveC2s.map((c) => {
                const on = open.has(c.nodeId);
                return (
                  <button
                    key={c.nodeId}
                    onClick={() => toggle(c.nodeId)}
                    className="card"
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 11,
                      padding: "11px 14px",
                      cursor: "pointer",
                      border: `1px solid ${on ? "var(--accent)" : "var(--border)"}`,
                      boxShadow: on ? "0 0 0 3px var(--accent-soft)" : "var(--shadow-xs)",
                      textAlign: "left",
                    }}
                  >
                    <span
                      style={{
                        width: 34,
                        height: 34,
                        borderRadius: 8,
                        display: "grid",
                        placeItems: "center",
                        background: "var(--surface-3)",
                        border: "1px solid var(--border)",
                        color: on ? "var(--accent)" : "var(--text-3)",
                      }}
                    >
                      <Icons.Server size={16} />
                    </span>
                    <span style={{ minWidth: 0 }}>
                      <span style={{ display: "flex", alignItems: "center", gap: 7 }}>
                        <span style={{ fontSize: 13.5, fontWeight: 600 }}>{c.frameworkName}</span>
                        <TierBadge tier={c.tier} label={TIER_LABEL[c.tier] || c.tier} />
                      </span>
                      <span className="mono" style={{ fontSize: 11, color: "var(--text-3)" }}>
                        {c.name} · {c.ip}
                      </span>
                    </span>
                    <span
                      className={"pill " + (on ? "accent" : "")}
                      style={{ height: 22, marginLeft: 4 }}
                    >
                      {on ? "open" : "open shell"}
                    </span>
                  </button>
                );
              })}
            </div>

            {/* terminal grid */}
            {openList.length === 0 ? (
              <div
                style={{
                  fontSize: 13,
                  color: "var(--text-3)",
                  textAlign: "center",
                  padding: "40px 0",
                }}
              >
                Select a teamserver above to open its web shell. Open several to operate them together.
              </div>
            ) : (
              <div
                style={{
                  display: "grid",
                  gridTemplateColumns: "repeat(auto-fit, minmax(440px, 1fr))",
                  gap: 14,
                }}
              >
                {openList.map((c) => (
                  <div key={c.nodeId} className="card" style={{ overflow: "hidden", padding: 0 }}>
                    <div
                      style={{
                        display: "flex",
                        alignItems: "center",
                        gap: 10,
                        padding: "10px 14px",
                        borderBottom: "1px solid var(--border)",
                      }}
                    >
                      <span style={{ color: "var(--text-3)" }}>
                        <Icons.Terminal size={15} />
                      </span>
                      <div style={{ flex: 1, minWidth: 0 }}>
                        <div style={{ fontSize: 13, fontWeight: 600 }}>{c.frameworkName}</div>
                        <div className="mono" style={{ fontSize: 11, color: "var(--text-3)" }}>
                          {c.name} · {c.ip}
                        </div>
                      </div>
                      <StatusPill status={c.status} sm />
                      <button
                        className="btn ghost sm"
                        onClick={() => toggle(c.nodeId)}
                        style={{ padding: 5 }}
                        title="Close shell"
                      >
                        <Icons.X size={15} />
                      </button>
                    </div>
                    <div style={{ padding: 12 }}>
                      <TerminalPane d={c} engagementId={activeEngagementId} height={340} />
                    </div>
                  </div>
                ))}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
