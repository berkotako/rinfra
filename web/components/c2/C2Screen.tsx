"use client";
import React, { useState } from "react";
import { Icons } from "../icons";
import { TierBadge, PageHead, Modal } from "../ui";
import { C2_FRAMEWORKS } from "../../lib/data";
import type { C2Framework } from "../../lib/types";

interface C2SelectorProps {
  asModal?: boolean;
  onClose?: () => void;
  selectedId?: string;
  onSelect?: (c: C2Framework) => void;
}

export function C2Selector({
  asModal,
  onClose,
  selectedId,
  onSelect,
}: C2SelectorProps) {
  const [picked, setPicked] = useState(selectedId || "sliver");
  const [licenses, setLicenses] = useState<Record<string, string>>({});
  const cur = C2_FRAMEWORKS.find((c) => c.id === picked) || C2_FRAMEWORKS[0];

  const tierTone: Record<string, string> = {
    orchestrated: "var(--ok)",
    scripted: "var(--info)",
    fronted: "var(--text-3)",
  };

  function Row({ label, children }: { label: string; children: React.ReactNode }) {
    return (
      <div style={{ display: "flex", justifyContent: "space-between", gap: 12 }}>
        <span style={{ color: "var(--text-3)" }}>{label}</span>
        {children}
      </div>
    );
  }

  const body = (
    <div style={{ display: "flex", gap: 22, alignItems: "flex-start" }}>
      {/* list */}
      <div
        style={{
          flex: "1 1 0",
          minWidth: 0,
          display: "flex",
          flexDirection: "column",
          gap: 10,
        }}
      >
        {C2_FRAMEWORKS.map((c) => {
          const active = picked === c.id;
          return (
            <div
              key={c.id}
              onClick={() => setPicked(c.id)}
              style={{
                padding: "15px 16px",
                borderRadius: "var(--r-lg)",
                cursor: "pointer",
                border: `1px solid ${active ? "var(--accent)" : "var(--border)"}`,
                background: active ? "var(--accent-soft)" : "var(--surface)",
                boxShadow: active
                  ? "0 0 0 3px var(--accent-soft)"
                  : "var(--shadow-xs)",
                transition: "all .14s",
              }}
            >
              <div style={{ display: "flex", alignItems: "flex-start", gap: 13 }}>
                <div
                  style={{
                    width: 40,
                    height: 40,
                    flex: "none",
                    borderRadius: 9,
                    display: "grid",
                    placeItems: "center",
                    background: "var(--surface-3)",
                    border: "1px solid var(--border)",
                    color: tierTone[c.tier] || "var(--text-3)",
                  }}
                >
                  <Icons.Server size={19} />
                </div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 9,
                      flexWrap: "wrap",
                    }}
                  >
                    <span style={{ fontSize: 14.5, fontWeight: 600 }}>
                      {c.name}
                    </span>
                    <TierBadge tier={c.tier} label={c.tierLabel} />
                    {c.gated && (
                      <span
                        className="pill warn"
                        style={{ height: 20 }}
                      >
                        <Icons.Lock size={11} /> License required
                      </span>
                    )}
                  </div>
                  <div
                    style={{
                      fontSize: 12.5,
                      color: "var(--text-2)",
                      marginTop: 6,
                      lineHeight: 1.5,
                    }}
                  >
                    {c.note}
                  </div>
                  <div
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 8,
                      marginTop: 9,
                      flexWrap: "wrap",
                    }}
                  >
                    <span style={{ fontSize: 11, color: "var(--text-3)" }}>
                      {c.lang}
                    </span>
                    <span style={{ color: "var(--border-strong)" }}>·</span>
                    <div style={{ display: "flex", gap: 5 }}>
                      {c.listeners.map((l) => (
                        <span
                          key={l}
                          className="mono"
                          style={{
                            fontSize: 10.5,
                            color: "var(--text-3)",
                            background: "var(--surface-3)",
                            border: "1px solid var(--border)",
                            borderRadius: 4,
                            padding: "1px 6px",
                          }}
                        >
                          {l}
                        </span>
                      ))}
                    </div>
                  </div>
                </div>
                <div
                  style={{
                    width: 20,
                    height: 20,
                    flex: "none",
                    borderRadius: 99,
                    border: `1.5px solid ${active ? "var(--accent)" : "var(--border-strong)"}`,
                    background: active ? "var(--accent)" : "transparent",
                    display: "grid",
                    placeItems: "center",
                    color: "#fff",
                    marginTop: 2,
                  }}
                >
                  {active && <Icons.Check size={13} />}
                </div>
              </div>
            </div>
          );
        })}
      </div>

      {/* detail rail */}
      <div
        style={{
          width: 280,
          flex: "none",
          position: asModal ? "static" : "sticky",
          top: 0,
        }}
      >
        <div className="card" style={{ padding: 18 }}>
          <div className="eyebrow" style={{ marginBottom: 12 }}>
            Selected framework
          </div>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 11,
              marginBottom: 14,
            }}
          >
            <div
              style={{
                width: 38,
                height: 38,
                borderRadius: 9,
                display: "grid",
                placeItems: "center",
                background: "var(--surface-3)",
                border: "1px solid var(--border)",
                color: tierTone[cur.tier] || "var(--text-3)",
              }}
            >
              <Icons.Server size={18} />
            </div>
            <div>
              <div style={{ fontWeight: 600, fontSize: 15 }}>{cur.name}</div>
              <div style={{ fontSize: 11.5, color: "var(--text-3)" }}>
                {cur.lang}
              </div>
            </div>
          </div>

          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: 10,
              fontSize: 12.5,
              paddingBottom: 14,
              borderBottom: "1px solid var(--border)",
            }}
          >
            <Row label="Orchestration">
              <span
                style={{
                  color: tierTone[cur.tier] || "var(--text-3)",
                  fontWeight: 500,
                }}
              >
                {cur.tierLabel}
              </span>
            </Row>
            <Row label="Listeners">
              <span className="mono" style={{ color: "var(--text-2)" }}>
                {cur.listeners.join(", ")}
              </span>
            </Row>
            <Row label="Automated emulation">
              <span
                style={{
                  color:
                    cur.tier === "fronted"
                      ? "var(--text-3)"
                      : "var(--ok)",
                  fontWeight: 500,
                }}
              >
                {cur.tier === "orchestrated"
                  ? "Full"
                  : cur.tier === "scripted"
                  ? "Scripted"
                  : "Manual"}
              </span>
            </Row>
          </div>

          {cur.gated ? (
            <div style={{ marginTop: 14 }}>
              <div className="field">
                <label style={{ display: "flex", alignItems: "center", gap: 6 }}>
                  <Icons.Lock size={13} /> License key
                </label>
                <input
                  className="input mono"
                  style={{ fontSize: 12 }}
                  placeholder="XXXX-XXXX-XXXX-XXXX"
                  value={licenses[cur.id] || ""}
                  onChange={(e) =>
                    setLicenses((l) => ({ ...l, [cur.id]: e.target.value }))
                  }
                />
                <div className="hint">
                  Customer-provided. Stored encrypted, scoped to this engagement
                  only.
                </div>
              </div>
            </div>
          ) : (
            <div
              style={{
                marginTop: 14,
                display: "flex",
                gap: 8,
                alignItems: "flex-start",
                fontSize: 12,
                color: "var(--text-3)",
                lineHeight: 1.5,
              }}
            >
              <span style={{ color: "var(--ok)", marginTop: 1 }}>
                <Icons.CheckCircle size={14} />
              </span>
              No license required — orchestrated directly by RInfra.
            </div>
          )}

          <button
            className="btn primary"
            style={{ width: "100%", justifyContent: "center", marginTop: 16 }}
            onClick={() => {
              if (onSelect) onSelect(cur);
              if (onClose) onClose();
            }}
          >
            <Icons.Check size={15} />{" "}
            {asModal ? "Assign to node" : "Set as default"}
          </button>
        </div>
      </div>
    </div>
  );

  if (asModal) {
    return (
      <Modal open={true} onClose={onClose || (() => {})} width={860}>
        <div
          style={{
            padding: "18px 22px",
            borderBottom: "1px solid var(--border)",
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
          }}
        >
          <div>
            <div style={{ fontSize: 16, fontWeight: 600 }}>
              C2 framework library
            </div>
            <div style={{ fontSize: 12.5, color: "var(--text-3)", marginTop: 2 }}>
              Choose the framework this server will run.
            </div>
          </div>
          <button
            className="btn ghost sm"
            onClick={onClose}
            style={{ padding: 6 }}
          >
            <Icons.X size={16} />
          </button>
        </div>
        <div className="scroll" style={{ padding: 22 }}>
          {body}
        </div>
      </Modal>
    );
  }

  return (
    <div className="scroll" style={{ height: "100%", padding: "26px 32px 40px" }}>
      <div style={{ maxWidth: 1100, margin: "0 auto" }}>
        <PageHead
          title="C2 frameworks"
          sub="Choose which command-and-control framework powers each server. Orchestration tier shows what RInfra automates for you."
        />
        {body}
      </div>
    </div>
  );
}

export default function C2Screen() {
  return <C2Selector />;
}
