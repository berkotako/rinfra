"use client";
import React, { useState } from "react";
import { Icons } from "../icons";
import { Modal } from "../ui";
import {
  TECHNIQUE_LIBRARY,
  TACTIC_ORDER,
  frameworksSupportingTactic,
  c2SupportsTactic,
  C2_TACTIC_SUPPORT,
  C2_FRAMEWORKS,
} from "../../lib/data";
import type { Scenario, Technique } from "../../lib/types";

// Frameworks that can automate at least one tactic (for the coverage readout).
const AUTOMATING_FRAMEWORKS = C2_FRAMEWORKS.filter((f) => C2_TACTIC_SUPPORT[f.id]);

export default function ScenarioBuilder({
  initial,
  onClose,
  onSubmit,
}: {
  initial?: Scenario;
  onClose: () => void;
  onSubmit: (s: Scenario) => void;
}) {
  const editing = !!initial;
  const [name, setName] = useState(initial?.name ?? "");
  const [actor, setActor] = useState(initial?.actor ?? "");
  const [desc, setDesc] = useState(initial?.desc ?? "");
  const [selected, setSelected] = useState<Set<string>>(
    new Set(initial?.techniques.map((t) => t.id) ?? [])
  );

  const toggle = (id: string) =>
    setSelected((s) => {
      const next = new Set(s);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  // Library grouped by tactic, in ATT&CK order.
  const groups = TACTIC_ORDER.map((tactic) => ({
    tactic,
    items: TECHNIQUE_LIBRARY.filter((t) => t.tactic === tactic),
  })).filter((g) => g.items.length > 0);

  const selectedTechniques: Technique[] = TECHNIQUE_LIBRARY.filter((t) => selected.has(t.id));

  // Per-framework automated coverage of the selected techniques.
  const coverage = AUTOMATING_FRAMEWORKS.map((f) => {
    const n = selectedTechniques.filter((t) => c2SupportsTactic(f.id, t.tactic)).length;
    return { id: f.id, name: f.name, n };
  });

  const canSave = name.trim().length > 0 && selected.size > 0;

  const save = () => {
    if (!canSave) return;
    const scenario: Scenario = {
      id: initial?.id ?? "custom-" + Date.now().toString(36),
      name: name.trim(),
      actor: actor.trim() || "Custom · operator-authored",
      desc: desc.trim() || "Operator-authored scenario.",
      techniques: selectedTechniques, // already in library (ATT&CK) order
    };
    onSubmit(scenario);
  };

  return (
    <Modal open onClose={onClose} width={860} label="New scenario">
      <div
        style={{
          padding: "16px 22px",
          borderBottom: "1px solid var(--border)",
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <div>
          <div style={{ fontSize: 16, fontWeight: 600 }}>
            {editing ? "Edit scenario" : "New emulation scenario"}
          </div>
          <div style={{ fontSize: 12.5, color: "var(--text-3)", marginTop: 2 }}>
            {editing
              ? "Adjust the techniques and metadata; coverage shows which C2s can automate them."
              : "Author a scenario from scratch — pick ATT&CK techniques; coverage shows which C2s can automate them."}
          </div>
        </div>
        <button className="btn ghost sm" onClick={onClose} style={{ padding: 6 }}>
          <Icons.X size={16} />
        </button>
      </div>

      <div className="scroll" style={{ padding: 22, display: "flex", gap: 22, alignItems: "flex-start" }}>
        {/* technique library */}
        <div style={{ flex: "1 1 0", minWidth: 0 }}>
          <div className="eyebrow" style={{ marginBottom: 10 }}>
            Techniques ({selected.size} selected)
          </div>
          <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
            {groups.map((g) => {
              const supN = frameworksSupportingTactic(g.tactic).length;
              return (
                <div key={g.tactic}>
                  <div
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 8,
                      marginBottom: 7,
                    }}
                  >
                    <span style={{ fontSize: 12, fontWeight: 600, color: "var(--text-2)" }}>
                      {g.tactic}
                    </span>
                    <span
                      className={"pill " + (supN > 0 ? "ok" : "")}
                      style={{ height: 18, fontSize: 10 }}
                    >
                      {supN > 0 ? `${supN} C2s automate` : "manual only"}
                    </span>
                  </div>
                  <div style={{ display: "flex", flexDirection: "column", gap: 5 }}>
                    {g.items.map((t) => {
                      const on = selected.has(t.id);
                      return (
                        <button
                          key={t.id}
                          onClick={() => toggle(t.id)}
                          style={{
                            display: "flex",
                            alignItems: "center",
                            gap: 10,
                            padding: "8px 11px",
                            borderRadius: "var(--r-sm)",
                            textAlign: "left",
                            border: `1px solid ${on ? "var(--accent)" : "var(--border-2)"}`,
                            background: on ? "var(--accent-soft)" : "var(--surface-inset)",
                          }}
                        >
                          <span
                            style={{
                              width: 17,
                              height: 17,
                              flex: "none",
                              borderRadius: 5,
                              border: `1.5px solid ${on ? "var(--accent)" : "var(--border-strong)"}`,
                              background: on ? "var(--accent)" : "transparent",
                              display: "grid",
                              placeItems: "center",
                              color: "#fff",
                            }}
                          >
                            {on && <Icons.Check size={12} />}
                          </span>
                          <span style={{ flex: 1, minWidth: 0, fontSize: 13, fontWeight: 500 }}>
                            {t.name}
                          </span>
                          <span className="mono" style={{ fontSize: 10.5, color: "var(--text-3)" }}>
                            {t.id}
                          </span>
                        </button>
                      );
                    })}
                  </div>
                </div>
              );
            })}
          </div>
        </div>

        {/* detail rail */}
        <div style={{ width: 300, flex: "none", position: "sticky", top: 0 }}>
          <div className="card" style={{ padding: 16 }}>
            <div className="field">
              <label>Scenario name</label>
              <input
                className="input"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. Insider — finance pivot"
              />
            </div>
            <div className="field" style={{ marginTop: 12 }}>
              <label>Actor / category</label>
              <input
                className="input"
                value={actor}
                onChange={(e) => setActor(e.target.value)}
                placeholder="e.g. eCrime · hands-on-keyboard"
              />
            </div>
            <div className="field" style={{ marginTop: 12 }}>
              <label>Description</label>
              <textarea
                className="input"
                rows={3}
                value={desc}
                onChange={(e) => setDesc(e.target.value)}
                placeholder="What this scenario emulates…"
                style={{ resize: "vertical", fontFamily: "var(--font-sans)" }}
              />
            </div>

            <div style={{ marginTop: 14, paddingTop: 14, borderTop: "1px solid var(--border)" }}>
              <div className="eyebrow" style={{ marginBottom: 10 }}>
                C2 coverage
              </div>
              {selected.size === 0 ? (
                <div style={{ fontSize: 12, color: "var(--text-3)" }}>
                  Select techniques to see which C2s can automate them.
                </div>
              ) : (
                <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                  {coverage.map((c) => {
                    const pct = Math.round((c.n / selected.size) * 100);
                    return (
                      <div key={c.id}>
                        <div
                          style={{
                            display: "flex",
                            justifyContent: "space-between",
                            fontSize: 11.5,
                            marginBottom: 3,
                          }}
                        >
                          <span style={{ color: "var(--text-2)" }}>{c.name}</span>
                          <span className="mono" style={{ color: "var(--text-3)" }}>
                            {c.n}/{selected.size}
                          </span>
                        </div>
                        <span
                          style={{
                            display: "block",
                            height: 4,
                            borderRadius: 99,
                            background: "var(--surface-3)",
                            overflow: "hidden",
                          }}
                        >
                          <span
                            style={{
                              display: "block",
                              width: `${pct}%`,
                              height: "100%",
                              borderRadius: 99,
                              background: pct >= 80 ? "var(--ok)" : pct >= 40 ? "var(--warn)" : "var(--danger)",
                            }}
                          />
                        </span>
                      </div>
                    );
                  })}
                  <div style={{ fontSize: 10.5, color: "var(--text-4)", marginTop: 2, lineHeight: 1.5 }}>
                    Cobalt Strike & Brute Ratel are fronted — every technique is run manually.
                  </div>
                </div>
              )}
            </div>

            <button
              className="btn primary"
              style={{ width: "100%", justifyContent: "center", marginTop: 16 }}
              disabled={!canSave}
              onClick={save}
            >
              {editing ? (
                <>
                  <Icons.Check size={15} /> Save changes
                </>
              ) : (
                <>
                  <Icons.Plus size={15} /> Create scenario
                </>
              )}
            </button>
          </div>
        </div>
      </div>
    </Modal>
  );
}
