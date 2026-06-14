"use client";
import React from "react";
import { Icons } from "../icons";
import { Modal } from "../ui";
import TerminalPane from "./Terminal";
import type { DeployedC2 } from "../../lib/types";

export default function WebShell({
  d,
  engagementId,
  onClose,
}: {
  d: DeployedC2;
  engagementId: string;
  onClose: () => void;
}) {
  return (
    <Modal open onClose={onClose} width={820} label="Web shell">
      <div
        style={{
          padding: "12px 16px",
          borderBottom: "1px solid var(--border)",
          display: "flex",
          alignItems: "center",
          gap: 10,
        }}
      >
        <span style={{ color: "var(--text-3)" }}>
          <Icons.Terminal size={16} />
        </span>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 14, fontWeight: 600 }}>Web shell — {d.name}</div>
          <div style={{ fontSize: 11.5, color: "var(--text-3)" }}>
            {d.frameworkName} · {d.ip}
          </div>
        </div>
        <button className="btn ghost sm" onClick={onClose} style={{ padding: 6 }}>
          <Icons.X size={16} />
        </button>
      </div>
      <div style={{ padding: 14 }}>
        <TerminalPane d={d} engagementId={engagementId} height={400} />
      </div>
    </Modal>
  );
}
