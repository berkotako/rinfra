"use client";
import React, { useEffect, useState } from "react";
import { Icons } from "../icons";
import { StatusPill, TierBadge } from "../ui";
import { getClient } from "../../lib/client";
import { useStore } from "../../lib/store";
import type { DeployedC2, OperatorMode } from "../../lib/types";

const MODE_PILL: Record<
  OperatorMode,
  { cls: string; icon: string; label: (liveClient: string) => string }
> = {
  live: { cls: "ok", icon: "Bolt", label: (lc) => `Live operator API · ${lc}` },
  scripted: { cls: "info", icon: "Terminal", label: (lc) => `Scripted · ${lc}` },
  manual: { cls: "", icon: "Power", label: () => "Manual only — human operates" },
};

const TIER_LABEL: Record<string, string> = {
  orchestrated: "Orchestrated",
  scripted: "Scripted",
  fronted: "Fronted",
};

function CopyButton({ text }: { text: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      className="btn ghost sm"
      style={{ padding: "6px 10px", flex: "none" }}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text);
          setDone(true);
          setTimeout(() => setDone(false), 1400);
        } catch {
          /* clipboard unavailable */
        }
      }}
    >
      {done ? <Icons.Check size={13} /> : <Icons.Copy size={13} />}
      {done ? "Copied" : "Copy"}
    </button>
  );
}

function TeamserverCard({ d }: { d: DeployedC2 }) {
  const [tunnel, setTunnel] = useState(false);
  const m = MODE_PILL[d.operatorMode];
  const MIco = Icons[m.icon] || Icons.Power;
  const automated = d.operatorMode !== "manual";

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

      {/* operator mode + sessions */}
      <div style={{ marginTop: 14, paddingTop: 14, borderTop: "1px solid var(--border)" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 9, marginBottom: 10, flexWrap: "wrap" }}>
          <span className={"pill " + m.cls}>
            <MIco size={12} /> {m.label(d.liveClient)}
          </span>
          {automated && (
            <span style={{ fontSize: 11.5, color: "var(--text-3)" }}>
              Automated emulation available
            </span>
          )}
        </div>
        {automated &&
          (d.sessions.length > 0 ? (
            <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
              {d.sessions.map((s) => (
                <div
                  key={s.id}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: 10,
                    fontSize: 12.5,
                    background: "var(--surface-3)",
                    border: "1px solid var(--border)",
                    borderRadius: "var(--r-sm)",
                    padding: "7px 11px",
                  }}
                >
                  <span className="status-dot live" style={{ width: 7, height: 7 }} />
                  <span style={{ fontWeight: 500 }}>{s.host}</span>
                  <span style={{ color: "var(--text-3)" }}>{s.user}</span>
                  <span className="mono" style={{ marginLeft: "auto", fontSize: 11, color: "var(--text-3)" }}>
                    {s.os}
                  </span>
                  <span className="mono" style={{ fontSize: 11, color: "var(--text-3)" }}>
                    #{s.id}
                  </span>
                </div>
              ))}
            </div>
          ) : (
            <div style={{ fontSize: 12, color: "var(--text-3)" }}>
              {d.status === "live" ? "No active sessions yet." : "Teamserver still provisioning."}
            </div>
          ))}
      </div>

      {/* manual access */}
      <div style={{ marginTop: 14, paddingTop: 14, borderTop: "1px solid var(--border)" }}>
        <div className="eyebrow" style={{ marginBottom: 10 }}>
          Manual access
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap", marginBottom: 10 }}>
          <span style={{ fontSize: 12.5 }}>
            Connect <strong>{d.manual.client}</strong>
          </span>
          <span className="pill" style={{ height: 20 }}>
            <Icons.Link size={11} /> {d.manual.protocol}
          </span>
          <span className="mono" style={{ fontSize: 11, color: "var(--text-3)" }}>
            operator port {d.manual.operatorPort}
          </span>
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <div
            className="mono"
            style={{
              flex: 1,
              minWidth: 0,
              fontSize: 12,
              background: "var(--surface-3)",
              border: "1px solid var(--border)",
              borderRadius: "var(--r-sm)",
              padding: "8px 12px",
              color: "var(--text-2)",
              overflowX: "auto",
              whiteSpace: "nowrap",
            }}
          >
            {d.manual.sshCommand}
          </div>
          <CopyButton text={d.manual.sshCommand} />
        </div>
        <div style={{ fontSize: 12, color: "var(--text-3)", marginTop: 10, lineHeight: 1.5 }}>
          {d.manual.instructions}
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 10, marginTop: 12, flexWrap: "wrap" }}>
          <button
            className={"btn " + (tunnel ? "danger" : "primary")}
            onClick={() => setTunnel((t) => !t)}
            disabled={d.status !== "live"}
          >
            {tunnel ? (
              <>
                <Icons.X size={14} /> Close tunnel
              </>
            ) : (
              <>
                <Icons.Link size={14} /> Open tunnel
              </>
            )}
          </button>
          {tunnel && (
            <span className="pill ok" style={{ height: 22 }}>
              <span className="status-dot live" style={{ width: 7, height: 7 }} /> Tunnel active ·
              127.0.0.1:{d.manual.operatorPort}
            </span>
          )}
          {d.status !== "live" && (
            <span style={{ fontSize: 11.5, color: "var(--text-3)" }}>
              available once the teamserver is live
            </span>
          )}
        </div>
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
