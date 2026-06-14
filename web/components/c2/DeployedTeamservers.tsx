"use client";
import React, { useEffect, useState } from "react";
import { Icons } from "../icons";
import { StatusPill, TierBadge } from "../ui";
import { getClient } from "../../lib/client";
import { useStore } from "../../lib/store";
import { ManualAccessBody } from "./ManualAccess";
import type { DeployedC2 } from "../../lib/types";

const TIER_LABEL: Record<string, string> = {
  orchestrated: "Orchestrated",
  scripted: "Scripted",
  fronted: "Fronted",
};

function TeamserverCard({ d }: { d: DeployedC2 }) {
  return (
    <div className="card" style={{ padding: 18 }}>
      {/* header */}
      <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
        <div
          style={{
            width: 38,
            height: 38,
            borderRadius: 9,
            display: "grid",
            placeItems: "center",
            background: "var(--surface-3)",
            border: "1px solid var(--border)",
            color: "var(--text-3)",
          }}
        >
          <Icons.Server size={18} />
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 9, flexWrap: "wrap" }}>
            <span style={{ fontWeight: 600, fontSize: 15 }}>{d.frameworkName}</span>
            <TierBadge tier={d.tier} label={TIER_LABEL[d.tier] || d.tier} />
          </div>
          <div style={{ fontSize: 12, color: "var(--text-3)", marginTop: 3 }}>
            {d.name} · <span className="mono">{d.ip}</span> · listener{" "}
            <span className="mono">{d.listener}</span>
          </div>
        </div>
        <StatusPill status={d.status} />
      </div>

      <div style={{ marginTop: 14, paddingTop: 14, borderTop: "1px solid var(--border)" }}>
        <ManualAccessBody d={d} />
      </div>
    </div>
  );
}

export default function DeployedTeamservers() {
  const { activeEngagementId } = useStore();
  const [items, setItems] = useState<DeployedC2[] | null>(null);

  useEffect(() => {
    let alive = true;
    getClient()
      .listDeployedC2(activeEngagementId)
      .then((d) => alive && setItems(d))
      .catch(() => alive && setItems([]));
    return () => {
      alive = false;
    };
  }, [activeEngagementId]);

  if (!items || items.length === 0) return null;

  return (
    <div style={{ marginTop: 34 }}>
      <div style={{ marginBottom: 14 }}>
        <h2 style={{ fontSize: 16, fontWeight: 600, letterSpacing: "-0.01em" }}>
          Deployed teamservers
        </h2>
        <div style={{ fontSize: 12.5, color: "var(--text-3)", marginTop: 3 }}>
          Two ways to drive each server: automated emulation via the operator API, or manual
          access — connect your native client over an SSH tunnel to the operator port.
        </div>
      </div>
      <div style={{ display: "grid", gap: 14 }}>
        {items.map((d) => (
          <TeamserverCard key={d.nodeId} d={d} />
        ))}
      </div>
    </div>
  );
}
