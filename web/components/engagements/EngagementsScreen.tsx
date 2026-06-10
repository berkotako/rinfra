"use client";
import React, { useState } from "react";
import { useRouter } from "next/navigation";
import { Icons } from "../icons";
import { StatusPill, PageHead, EmptyState } from "../ui";
import { useStore } from "../../lib/store";
import NewEngagementFlow from "./NewEngagementFlow";
import type { Engagement } from "../../lib/types";

const AUTH_META: Record<string, { cls: string; icon: string; label: string }> = {
  authorized: { cls: "ok", icon: "ShieldCheck", label: "Authorized" },
  pending: { cls: "warn", icon: "Clock", label: "Authorization pending" },
  expired: { cls: "", icon: "Lock", label: "Authorization expired" },
};

export default function EngagementsScreen() {
  const router = useRouter();
  const {
    engagements, setEngagements, activeEngagementId, setActiveEngagementId, pushToast,
    apiCreateEngagement,
  } = useStore();
  const [filter, setFilter] = useState("all");
  const [q, setQ] = useState("");
  const [newEngOpen, setNewEngOpen] = useState(false);

  const counts = {
    all: engagements.length,
    active: engagements.filter((e) => e.status === "active").length,
    provisioning: engagements.filter((e) => e.status === "provisioning").length,
    draft: engagements.filter((e) => e.status === "draft").length,
    completed: engagements.filter((e) => e.status === "completed").length,
  };

  const list = engagements.filter(
    (e) =>
      (filter === "all" || e.status === filter) &&
      (q === "" ||
        (e.client + e.codename + e.id).toLowerCase().includes(q.toLowerCase()))
  );

  const activeEng = engagements.filter(
    (e) => e.status === "active" || e.status === "provisioning"
  );
  const totalLive = engagements.reduce((a, e) => a + e.live, 0);
  const totalBurn = activeEng.reduce((a, e) => a + e.cost, 0);

  const openEngagement = (e: Engagement) => {
    setActiveEngagementId(e.id);
    if (e.status === "active" || e.status === "provisioning") {
      router.push("/infrastructure");
    }
  };

  const createEngagement = (e: Engagement) => {
    // In REST mode, apiCreateEngagement already updated the store; in mock mode
    // we still get the Engagement object back and add it manually.
    setEngagements((list) => {
      // Avoid duplicate if apiCreateEngagement already prepended it.
      if (list.some((x) => x.id === e.id)) return list;
      return [e, ...list];
    });
    setNewEngOpen(false);
    setActiveEngagementId(e.id);
    pushToast(`Engagement ${e.codename} created — authorization recorded`, "ok");
  };

  void apiCreateEngagement; // referenced so linter doesn't complain; used by NewEngagementFlow

  return (
    <div className="scroll" style={{ height: "100%", padding: "26px 32px 40px" }}>
      <div style={{ maxWidth: 1180, margin: "0 auto" }}>
        <PageHead
          title="Engagements"
          sub="Authorized red-team & purple-team operations across your clients."
        >
          <button className="btn">
            <Icons.Filter size={15} /> Export
          </button>
          <button className="btn primary" onClick={() => setNewEngOpen(true)}>
            <Icons.Plus size={16} /> New engagement
          </button>
        </PageHead>

        {/* summary stats */}
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(4,1fr)",
            gap: 12,
            marginBottom: 22,
          }}
        >
          {[
            {
              label: "Active engagements",
              value: counts.active,
              icon: "Target",
              tone: "var(--accent)",
            },
            {
              label: "Live assets",
              value: totalLive,
              icon: "Network",
              tone: "var(--ok)",
            },
            {
              label: "Combined burn rate",
              value: `$${totalBurn.toFixed(2)}/hr`,
              icon: "Dollar",
              tone: "var(--text)",
            },
            {
              label: "Awaiting authorization",
              value: counts.draft,
              icon: "Lock",
              tone: "var(--warn)",
            },
          ].map((s, i) => {
            const Ico = Icons[s.icon] || Icons.Target;
            return (
              <div key={i} className="card" style={{ padding: "15px 16px" }}>
                <div
                  style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                  }}
                >
                  <span style={{ fontSize: 12, color: "var(--text-3)" }}>
                    {s.label}
                  </span>
                  <span style={{ color: s.tone, opacity: 0.85 }}>
                    <Ico size={16} />
                  </span>
                </div>
                <div
                  className="mono"
                  style={{
                    fontSize: 24,
                    fontWeight: 600,
                    marginTop: 8,
                    letterSpacing: "-0.02em",
                  }}
                >
                  {s.value}
                </div>
              </div>
            );
          })}
        </div>

        {/* controls */}
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 10,
            marginBottom: 14,
          }}
        >
          <div className="seg">
            {(
              [
                ["all", "All"],
                ["active", "Active"],
                ["provisioning", "Provisioning"],
                ["draft", "Draft"],
                ["completed", "Completed"],
              ] as [string, string][]
            ).map(([k, lbl]) => (
              <button
                key={k}
                className={filter === k ? "active" : ""}
                onClick={() => setFilter(k)}
              >
                {lbl}{" "}
                <span style={{ color: "var(--text-4)", marginLeft: 3 }}>
                  {counts[k as keyof typeof counts]}
                </span>
              </button>
            ))}
          </div>
          <div style={{ flex: 1 }} />
          <div style={{ position: "relative", width: 240 }}>
            <span
              style={{
                position: "absolute",
                left: 10,
                top: "50%",
                transform: "translateY(-50%)",
                color: "var(--text-4)",
                pointerEvents: "none",
              }}
            >
              <Icons.Search size={15} />
            </span>
            <input
              className="input"
              placeholder="Search client or codename"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              style={{ paddingLeft: 32 }}
            />
          </div>
        </div>

        {/* table */}
        <div className="card" style={{ overflow: "hidden" }}>
          <div
            style={{
              display: "grid",
              gridTemplateColumns: "1.7fr 1.5fr 1fr 1.1fr 0.9fr 40px",
              gap: 12,
              padding: "11px 18px",
              borderBottom: "1px solid var(--border)",
              background: "var(--surface-2)",
              fontSize: 11,
              fontWeight: 600,
              letterSpacing: "0.03em",
              color: "var(--text-3)",
              textTransform: "uppercase",
            }}
          >
            <div>Client / Engagement</div>
            <div>Scope</div>
            <div>Infra status</div>
            <div>Authorization</div>
            <div>Window</div>
            <div />
          </div>
          {list.map((e, idx) => {
            const am = AUTH_META[e.auth] || AUTH_META.pending;
            const isActive = e.id === activeEngagementId;
            const AuthIco = Icons[am.icon] || Icons.Clock;
            return (
              <div
                key={e.id}
                onClick={() => openEngagement(e)}
                className={"eng-row" + (isActive ? " active" : "")}
                style={{
                  display: "grid",
                  gridTemplateColumns: "1.7fr 1.5fr 1fr 1.1fr 0.9fr 40px",
                  gap: 12,
                  padding: "15px 18px",
                  alignItems: "center",
                  borderBottom:
                    idx < list.length - 1
                      ? "1px solid var(--border)"
                      : "none",
                  cursor: "pointer",
                }}
              >
                {/* client */}
                <div
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: 12,
                    minWidth: 0,
                  }}
                >
                  <div
                    style={{
                      width: 36,
                      height: 36,
                      borderRadius: 9,
                      flex: "none",
                      display: "grid",
                      placeItems: "center",
                      background: "var(--surface-3)",
                      border: "1px solid var(--border)",
                      color: "var(--text-3)",
                    }}
                  >
                    <Icons.Building size={17} />
                  </div>
                  <div style={{ minWidth: 0 }}>
                    <div
                      style={{
                        fontWeight: 600,
                        fontSize: 13.5,
                        whiteSpace: "nowrap",
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                      }}
                    >
                      {e.client}
                    </div>
                    <div
                      style={{
                        display: "flex",
                        alignItems: "center",
                        gap: 6,
                        marginTop: 1,
                      }}
                    >
                      <span
                        className="mono"
                        style={{ fontSize: 11, color: "var(--text-3)" }}
                      >
                        {e.id}
                      </span>
                      <span style={{ color: "var(--text-4)" }}>·</span>
                      <span
                        style={{
                          fontSize: 11.5,
                          color: "var(--accent)",
                          fontWeight: 500,
                        }}
                      >
                        {e.codename}
                      </span>
                    </div>
                  </div>
                </div>
                {/* scope */}
                <div style={{ minWidth: 0 }}>
                  <div
                    style={{
                      fontSize: 12.5,
                      color: "var(--text-2)",
                      whiteSpace: "nowrap",
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                    }}
                  >
                    {e.scope}
                  </div>
                  {e.targets.length > 0 && (
                    <div
                      className="mono"
                      style={{
                        fontSize: 10.5,
                        color: "var(--text-4)",
                        marginTop: 2,
                        whiteSpace: "nowrap",
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                      }}
                    >
                      {e.targets[0]}
                      {e.targets.length > 1
                        ? ` +${e.targets.length - 1}`
                        : ""}
                    </div>
                  )}
                </div>
                {/* infra */}
                <div
                  style={{
                    display: "flex",
                    flexDirection: "column",
                    gap: 5,
                    alignItems: "flex-start",
                  }}
                >
                  <StatusPill
                    status={
                      e.status === "active"
                        ? "live"
                        : e.status === "completed"
                        ? "destroyed"
                        : e.status
                    }
                    sm
                  />
                  <span style={{ fontSize: 11, color: "var(--text-3)" }}>
                    {e.live}/{e.assets} live
                  </span>
                </div>
                {/* auth */}
                <div
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: 7,
                  }}
                >
                  <span
                    style={{
                      color:
                        am.cls === "ok"
                          ? "var(--ok)"
                          : am.cls === "warn"
                          ? "var(--warn)"
                          : "var(--text-4)",
                    }}
                  >
                    <AuthIco size={15} />
                  </span>
                  <div style={{ minWidth: 0 }}>
                    <div
                      style={{
                        fontSize: 12,
                        fontWeight: 500,
                        color: "var(--text-2)",
                      }}
                    >
                      {am.label}
                    </div>
                    {e.authBy !== "—" && (
                      <div
                        style={{
                          fontSize: 10.5,
                          color: "var(--text-4)",
                          whiteSpace: "nowrap",
                          overflow: "hidden",
                          textOverflow: "ellipsis",
                        }}
                      >
                        {e.authBy}
                      </div>
                    )}
                  </div>
                </div>
                {/* window */}
                <div
                  className="mono"
                  style={{ fontSize: 11, color: "var(--text-3)" }}
                >
                  <div>{e.start.slice(5)}</div>
                  <div style={{ color: "var(--text-4)" }}>
                    → {e.end.slice(5)}
                  </div>
                </div>
                {/* arrow */}
                <div
                  style={{
                    display: "grid",
                    placeItems: "center",
                    color: "var(--text-4)",
                  }}
                >
                  <Icons.ChevronRight size={17} />
                </div>
              </div>
            );
          })}
          {list.length === 0 && (
            <EmptyState
              icon="Search"
              title="No engagements found"
              body="Try a different filter or search term."
            />
          )}
        </div>
      </div>

      <NewEngagementFlow
        open={newEngOpen}
        onClose={() => setNewEngOpen(false)}
        onCreate={createEngagement}
      />
    </div>
  );
}
