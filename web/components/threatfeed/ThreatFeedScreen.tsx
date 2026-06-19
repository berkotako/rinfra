"use client";
import React, { useEffect, useMemo, useState } from "react";
import { Icons } from "../icons";
import { PageHead } from "../ui";
import { useStore } from "../../lib/store";
import { getClient } from "../../lib/client";
import type { Advisory, SuggestedTTP } from "../../lib/types";

const CONF_CLS: Record<string, string> = { high: "ok", medium: "info", low: "" };

export default function ThreatFeedScreen() {
  const { techniques, addTechnique, pushToast } = useStore();
  const [advisories, setAdvisories] = useState<Advisory[] | null>(null);

  useEffect(() => {
    let alive = true;
    getClient()
      .listAdvisories()
      .then((a) => alive && setAdvisories(a))
      .catch(() => alive && setAdvisories([]));
    return () => {
      alive = false;
    };
  }, []);

  const haveIds = useMemo(() => new Set(techniques.map((t) => t.id)), [techniques]);

  const addTtp = (adv: Advisory, s: SuggestedTTP) => {
    addTechnique({
      id: s.attackId,
      name: s.name,
      tactic: s.tactic,
      description: `From advisory ${adv.id} — ${adv.title}: ${adv.summary}`,
      commands: [],
    })
      .then(() => pushToast(`Added ${s.attackId} to the TTP library`, "ok"))
      .catch(() => pushToast(`Could not add ${s.attackId}`, "danger"));
  };

  return (
    <div className="scroll" style={{ height: "100%", padding: "26px 32px 40px" }}>
      <div style={{ maxWidth: 1000, margin: "0 auto" }}>
        <PageHead
          title="Threat feed"
          sub="Actively-exploited advisories (CISA KEV) with suggested ATT&CK techniques — fold emerging threats into the TTP library."
        />

        {advisories === null ? (
          <div style={{ fontSize: 13, color: "var(--text-3)" }}>Loading advisories…</div>
        ) : advisories.length === 0 ? (
          <div style={{ fontSize: 13, color: "var(--text-3)" }}>No advisories available.</div>
        ) : (
          <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
            {advisories.map((adv) => (
              <div key={adv.id} className="card" style={{ padding: 16 }}>
                <div style={{ display: "flex", alignItems: "flex-start", gap: 10, flexWrap: "wrap" }}>
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ display: "flex", alignItems: "center", gap: 9, flexWrap: "wrap" }}>
                      <span style={{ fontSize: 14.5, fontWeight: 600 }}>{adv.title}</span>
                      {adv.ransomware && (
                        <span className="pill danger" style={{ height: 20 }}>
                          <Icons.AlertTriangle size={11} /> ransomware
                        </span>
                      )}
                    </div>
                    <div style={{ fontSize: 11.5, color: "var(--text-3)", marginTop: 3 }}>
                      <a className="mono" href={adv.url} target="_blank" rel="noreferrer" style={{ color: "var(--accent)" }}>
                        {adv.id}
                      </a>{" "}
                      · {adv.source} · {adv.vendor} {adv.product} · {adv.published}
                    </div>
                  </div>
                </div>
                <div style={{ fontSize: 12.5, color: "var(--text-2)", lineHeight: 1.5, margin: "9px 0 12px" }}>
                  {adv.summary}
                </div>
                <div className="eyebrow" style={{ marginBottom: 8 }}>
                  Suggested techniques
                </div>
                <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
                  {adv.suggestedTtps.map((s) => {
                    const already = haveIds.has(s.attackId);
                    return (
                      <div
                        key={s.attackId}
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: 8,
                          border: "1px solid var(--border)",
                          borderRadius: "var(--r-sm)",
                          background: "var(--surface-inset)",
                          padding: "6px 9px",
                        }}
                      >
                        <span className="mono" style={{ fontSize: 11, color: "var(--text-3)" }}>{s.attackId}</span>
                        <span style={{ fontSize: 12 }}>{s.name}</span>
                        <span className={"pill " + (CONF_CLS[s.confidence] || "")} style={{ height: 18, fontSize: 10 }}>
                          {s.confidence}
                        </span>
                        <button
                          className="btn ghost sm"
                          style={{ padding: "3px 8px" }}
                          disabled={already}
                          onClick={() => addTtp(adv, s)}
                          title={already ? "Already in the TTP library" : "Add to the TTP library"}
                        >
                          {already ? <><Icons.Check size={12} /> in library</> : <><Icons.Plus size={12} /> Add TTP</>}
                        </button>
                      </div>
                    );
                  })}
                </div>
              </div>
            ))}
          </div>
        )}
        <div style={{ fontSize: 11, color: "var(--text-4)", marginTop: 14, lineHeight: 1.5 }}>
          Suggested techniques are heuristic mappings from advisory text — review before use. Set
          <span className="mono"> RINFRA_THREATFEED=cisa-kev </span> on the server to pull the live catalog.
        </div>
      </div>
    </div>
  );
}
