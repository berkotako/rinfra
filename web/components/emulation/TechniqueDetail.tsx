"use client";
import React from "react";
import { Icons } from "../icons";
import { Modal } from "../ui";
import { CopyButton } from "../c2/ManualAccess";
import { c2SupportsTactic, C2_DELIVERY } from "../../lib/data";
import type { Technique, DeployedC2 } from "../../lib/types";

// ATT&CK technique page URL: T1059.001 -> /techniques/T1059/001
function attackUrl(id: string): string {
  return `https://attack.mitre.org/techniques/${id.replace(".", "/")}/`;
}

export default function TechniqueDetail({
  technique,
  c2,
  onClose,
}: {
  technique: Technique;
  c2?: DeployedC2 | null;
  onClose: () => void;
}) {
  const auto = !!c2 && c2SupportsTactic(c2.framework, technique.tactic);
  const commands = technique.commands ?? [];

  return (
    <Modal open onClose={onClose} width={560} label="Technique detail">
      <div
        style={{
          padding: "16px 20px",
          borderBottom: "1px solid var(--border)",
          display: "flex",
          alignItems: "flex-start",
          justifyContent: "space-between",
          gap: 12,
        }}
      >
        <div style={{ minWidth: 0 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 9, flexWrap: "wrap" }}>
            <span style={{ fontSize: 16, fontWeight: 600 }}>{technique.name}</span>
            <span className="pill" style={{ height: 20 }}>{technique.tactic}</span>
          </div>
          <a
            href={attackUrl(technique.id)}
            target="_blank"
            rel="noreferrer"
            className="mono"
            style={{
              display: "inline-flex",
              alignItems: "center",
              gap: 5,
              fontSize: 11.5,
              color: "var(--accent)",
              marginTop: 5,
            }}
          >
            {technique.id} <Icons.ArrowRight size={11} /> MITRE ATT&CK
          </a>
        </div>
        <button className="btn ghost sm" onClick={onClose} style={{ padding: 6, flex: "none" }}>
          <Icons.X size={16} />
        </button>
      </div>

      <div className="scroll" style={{ padding: 18, display: "flex", flexDirection: "column", gap: 16 }}>
        {technique.description && (
          <div>
            <div className="eyebrow" style={{ marginBottom: 8 }}>What it is</div>
            <div style={{ fontSize: 13, color: "var(--text-2)", lineHeight: 1.55 }}>
              {technique.description}
            </div>
          </div>
        )}

        <div>
          <div className="eyebrow" style={{ marginBottom: 8 }}>Execution</div>
          {c2 ? (
            <div
              style={{
                display: "flex",
                alignItems: "flex-start",
                gap: 9,
                fontSize: 12.5,
                color: "var(--text-2)",
                lineHeight: 1.5,
              }}
            >
              <span style={{ color: auto ? "var(--ok)" : "var(--text-3)", marginTop: 1 }}>
                {auto ? <Icons.Bolt size={14} /> : <Icons.Power size={14} />}
              </span>
              <span>
                {auto ? (
                  <>
                    Automated through <strong>{c2.frameworkName}</strong> —{" "}
                    delivered via <span className="mono">{C2_DELIVERY[c2.framework] || "operator API"}</span>.
                  </>
                ) : (
                  <>
                    <strong>{c2.frameworkName}</strong> can&apos;t automate this tactic — run by hand from your{" "}
                    {c2.manual.client}.
                  </>
                )}
              </span>
            </div>
          ) : (
            <div style={{ fontSize: 12.5, color: "var(--text-3)" }}>
              Select a C2 target to see how it&apos;s executed.
            </div>
          )}
        </div>

        {commands.length > 0 && (
          <div>
            <div
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                marginBottom: 8,
              }}
            >
              <div className="eyebrow">Procedure — commands</div>
              <CopyButton text={commands.join("\n")} />
            </div>
            <div
              className="mono"
              style={{
                fontSize: 12,
                background: "var(--surface-3)",
                border: "1px solid var(--border)",
                borderRadius: "var(--r-sm)",
                padding: "10px 12px",
                color: "var(--text-2)",
                lineHeight: 1.7,
                whiteSpace: "pre-wrap",
                wordBreak: "break-word",
              }}
            >
              {commands.map((c, i) => (
                <div key={i} style={{ color: c.trimStart().startsWith("#") ? "var(--text-4)" : undefined }}>
                  {c}
                </div>
              ))}
            </div>
            <div style={{ fontSize: 11, color: "var(--text-4)", marginTop: 8, lineHeight: 1.5 }}>
              Portable procedure (Atomic Red Team-style). The framework&apos;s Operator adapter translates
              it to native primitives at run time.
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
}
