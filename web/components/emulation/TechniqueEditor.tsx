"use client";
import React, { useState } from "react";
import { Icons } from "../icons";
import { Modal } from "../ui";
import { TACTIC_ORDER } from "../../lib/data";
import type { Technique } from "../../lib/types";

export default function TechniqueEditor({
  initial,
  existingIds,
  onClose,
  onSubmit,
}: {
  initial?: Technique;
  existingIds: Set<string>;
  onClose: () => void;
  onSubmit: (t: Technique) => void;
}) {
  const editing = !!initial;
  const [id, setId] = useState(initial?.id ?? "");
  const [name, setName] = useState(initial?.name ?? "");
  const [tactic, setTactic] = useState(initial?.tactic ?? TACTIC_ORDER[1]);
  const [description, setDescription] = useState(initial?.description ?? "");
  const [commands, setCommands] = useState((initial?.commands ?? []).join("\n"));

  const idTrim = id.trim();
  const dup = !editing && existingIds.has(idTrim);
  const canSave = idTrim.length > 0 && name.trim().length > 0 && tactic.length > 0 && !dup;

  const save = () => {
    if (!canSave) return;
    onSubmit({
      id: idTrim,
      name: name.trim(),
      tactic,
      description: description.trim() || undefined,
      commands: commands
        .split("\n")
        .map((l) => l.replace(/\s+$/, ""))
        .filter((l) => l.length > 0),
    });
  };

  const field = { marginBottom: 12 } as React.CSSProperties;

  return (
    <Modal open onClose={onClose} width={560} label="TTP editor">
      <div
        style={{
          padding: "16px 20px",
          borderBottom: "1px solid var(--border)",
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <div>
          <div style={{ fontSize: 16, fontWeight: 600 }}>{editing ? "Edit TTP" : "New TTP"}</div>
          <div style={{ fontSize: 12.5, color: "var(--text-3)", marginTop: 2 }}>
            Author a technique; it&apos;s mapped to C2 functionality by its tactic.
          </div>
        </div>
        <button className="btn ghost sm" onClick={onClose} style={{ padding: 6 }}>
          <Icons.X size={16} />
        </button>
      </div>

      <div className="scroll" style={{ padding: 20 }}>
        <div className="field" style={field}>
          <label>ATT&amp;CK ID</label>
          <input
            className="input mono"
            value={id}
            disabled={editing}
            onChange={(e) => setId(e.target.value)}
            placeholder="T1059.001"
            style={{ fontSize: 13 }}
          />
          {dup && <div className="hint" style={{ color: "var(--danger)" }}>A technique with this ID already exists.</div>}
        </div>
        <div className="field" style={field}>
          <label>Name</label>
          <input
            className="input"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. PowerShell"
          />
        </div>
        <div className="field" style={field}>
          <label>Tactic</label>
          <select className="select" value={tactic} onChange={(e) => setTactic(e.target.value)}>
            {TACTIC_ORDER.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
        </div>
        <div className="field" style={field}>
          <label>Description</label>
          <textarea
            className="input"
            rows={2}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="What this technique does…"
            style={{ resize: "vertical", fontFamily: "var(--font-sans)" }}
          />
        </div>
        <div className="field" style={field}>
          <label>Procedure commands (one per line)</label>
          <textarea
            className="input mono"
            rows={5}
            value={commands}
            onChange={(e) => setCommands(e.target.value)}
            placeholder={"# comment\npowershell -nop -enc <base64>"}
            style={{ resize: "vertical", fontSize: 12 }}
          />
        </div>

        <button
          className="btn primary"
          style={{ width: "100%", justifyContent: "center", marginTop: 6 }}
          disabled={!canSave}
          onClick={save}
        >
          {editing ? (
            <>
              <Icons.Check size={15} /> Save changes
            </>
          ) : (
            <>
              <Icons.Plus size={15} /> Create TTP
            </>
          )}
        </button>
      </div>
    </Modal>
  );
}
