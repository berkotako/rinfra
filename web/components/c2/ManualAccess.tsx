"use client";
import React, { useState } from "react";
import { Icons } from "../icons";
import { useStore } from "../../lib/store";
import WebShell from "./WebShell";
import type { DeployedC2, OperatorMode } from "../../lib/types";

const MODE_PILL: Record<OperatorMode, { cls: string; icon: string; label: string }> = {
  live: { cls: "ok", icon: "Bolt", label: "Live operator API" },
  scripted: { cls: "info", icon: "Terminal", label: "Scripted automation" },
  manual: { cls: "", icon: "Power", label: "Manual only" },
};

export function CopyButton({ text }: { text: string }) {
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

// OperatorStatus — automated-operator mode + active sessions ("agents").
export function OperatorStatus({ d }: { d: DeployedC2 }) {
  const m = MODE_PILL[d.operatorMode];
  const MIco = Icons[m.icon] || Icons.Power;
  const automated = d.operatorMode !== "manual";
  return (
    <div>
      <div style={{ display: "flex", alignItems: "center", gap: 9, marginBottom: 10, flexWrap: "wrap" }}>
        <span className={"pill " + m.cls}>
          <MIco size={12} /> {m.label}
        </span>
        {automated && d.liveClient && (
          <span className="mono" style={{ fontSize: 11, color: "var(--text-3)" }}>{d.liveClient}</span>
        )}
      </div>
      {automated && (
        <div style={{ fontSize: 11.5, color: "var(--text-3)", marginBottom: 8 }}>
          RInfra can drive emulation through this teamserver. Active agents:
        </div>
      )}
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
  );
}

// ManualAccessControls — native-client connect path: ssh tunnel + instructions.
export function ManualAccessControls({ d }: { d: DeployedC2 }) {
  const { activeEngagementId } = useStore();
  const [tunnel, setTunnel] = useState(false);
  const [shell, setShell] = useState(false);
  return (
    <div>
      {shell && (
        <WebShell d={d} engagementId={activeEngagementId} onClose={() => setShell(false)} />
      )}
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
        <button
          className="btn"
          onClick={() => setShell(true)}
          disabled={d.status !== "live"}
          title="Open an in-browser operator shell over the tunnel"
        >
          <Icons.Terminal size={14} /> Web shell
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
  );
}

// ManualAccessBody — operator status + manual-access path, stacked with dividers.
export function ManualAccessBody({ d }: { d: DeployedC2 }) {
  return (
    <>
      <OperatorStatus d={d} />
      <div style={{ marginTop: 14, paddingTop: 14, borderTop: "1px solid var(--border)" }}>
        <ManualAccessControls d={d} />
      </div>
    </>
  );
}
