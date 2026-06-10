"use client";
import React from "react";
import { Modal } from "../ui";
import { useStore, ACCENTS } from "../../lib/store";
import type { NodeStyle } from "../../lib/types";

export default function AppearanceMenu({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { preferences, setTheme, setAccent, setNodeStyle } = useStore();

  return (
    <Modal open={open} onClose={onClose} width={360} label="Appearance">
      <div
        style={{
          padding: "16px 18px",
          borderBottom: "1px solid var(--border)",
          fontSize: 14,
          fontWeight: 600,
        }}
      >
        Appearance
      </div>
      <div style={{ padding: "16px 18px", display: "flex", flexDirection: "column", gap: 20 }}>
        {/* dark mode toggle */}
        <div className="field">
          <label>Theme</label>
          <div
            onClick={() =>
              setTheme(preferences.theme === "dark" ? "light" : "dark")
            }
            style={{
              display: "flex",
              alignItems: "center",
              gap: 13,
              padding: "11px 13px",
              borderRadius: "var(--r-md)",
              border: "1px solid var(--border)",
              background: "var(--surface-2)",
              cursor: "pointer",
            }}
          >
            <div style={{ flex: 1 }}>
              <div style={{ fontSize: 13, fontWeight: 600 }}>Soft dark mode</div>
              <div style={{ fontSize: 12, color: "var(--text-3)", marginTop: 2 }}>
                Warm charcoal — never pure black
              </div>
            </div>
            <div className={"toggle " + (preferences.theme === "dark" ? "on" : "")} />
          </div>
        </div>

        {/* accent color */}
        <div className="field">
          <label>Primary accent</label>
          <div style={{ display: "flex", gap: 7 }}>
            {ACCENTS.map((a) => (
              <button
                key={a.id}
                title={a.name}
                onClick={() => setAccent(a.id)}
                style={{
                  flex: 1,
                  height: 30,
                  borderRadius: 7,
                  cursor: "pointer",
                  background: `oklch(0.58 0.09 ${a.h})`,
                  border:
                    preferences.accentId === a.id
                      ? "2px solid rgba(0,0,0,.55)"
                      : "2px solid transparent",
                  boxShadow:
                    preferences.accentId === a.id
                      ? "0 0 0 2px #fff inset"
                      : "none",
                }}
              />
            ))}
          </div>
          <div style={{ display: "flex", gap: 7 }}>
            {ACCENTS.map((a) => (
              <div
                key={a.id}
                style={{
                  flex: 1,
                  fontSize: 10,
                  color:
                    preferences.accentId === a.id
                      ? "var(--accent)"
                      : "var(--text-4)",
                  textAlign: "center",
                  fontWeight: preferences.accentId === a.id ? 600 : 400,
                }}
              >
                {a.name}
              </div>
            ))}
          </div>
        </div>

        {/* node card style */}
        <div className="field">
          <label>Node card style</label>
          <div className="seg" style={{ width: "100%" }}>
            {(["soft", "compact", "outline"] as NodeStyle[]).map((s) => (
              <button
                key={s}
                className={preferences.nodeStyle === s ? "active" : ""}
                onClick={() => setNodeStyle(s)}
                style={{ flex: 1, textTransform: "capitalize" }}
              >
                {s}
              </button>
            ))}
          </div>
          <div style={{ fontSize: 10.5, color: "var(--text-3)", lineHeight: 1.45 }}>
            Switch to the Infrastructure screen to preview node styles.
          </div>
        </div>
      </div>
      <div
        style={{
          padding: "12px 18px",
          borderTop: "1px solid var(--border)",
          display: "flex",
          justifyContent: "flex-end",
        }}
      >
        <button className="btn primary" onClick={onClose}>
          Done
        </button>
      </div>
    </Modal>
  );
}
