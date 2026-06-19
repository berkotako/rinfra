"use client";
import React, { useEffect, useMemo, useState } from "react";
import { Icons } from "../icons";
import { PageHead } from "../ui";
import { useStore } from "../../lib/store";
import { getClient } from "../../lib/client";
import { parseAdvisoryFeed } from "../../lib/data";
import type { Advisory, SuggestedTTP } from "../../lib/types";

const CONF_CLS: Record<string, string> = { high: "ok", medium: "info", low: "" };

const SAMPLE_FEED = `[
  {
    "id": "INTERNAL-2026-0001",
    "title": "Exploited deserialization in internal portal",
    "summary": "Active exploitation enabling remote code execution.",
    "vendor": "Internal",
    "product": "Portal",
    "published": "2026-06-18"
  }
]`;

export default function ThreatFeedScreen() {
  const { techniques, addTechnique, pushToast } = useStore();
  const [advisories, setAdvisories] = useState<Advisory[] | null>(null);
  const [sources, setSources] = useState<string[]>([]);
  const [extra, setExtra] = useState<Advisory[]>([]); // feeds added in this session
  const [showAdd, setShowAdd] = useState(false);
  const [feedName, setFeedName] = useState("");
  const [feedJson, setFeedJson] = useState(SAMPLE_FEED);
  const [feedErr, setFeedErr] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    const c = getClient();
    c.listAdvisories()
      .then((a) => alive && setAdvisories(a))
      .catch(() => alive && setAdvisories([]));
    c.listAdvisorySources()
      .then((s) => alive && setSources(s))
      .catch(() => alive && setSources([]));
    return () => {
      alive = false;
    };
  }, []);

  const haveIds = useMemo(() => new Set(techniques.map((t) => t.id)), [techniques]);
  const allAdvisories = useMemo(() => [...extra, ...(advisories ?? [])], [extra, advisories]);

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

  const importFeed = () => {
    const label = feedName.trim() || "Custom feed";
    try {
      const parsed = parseAdvisoryFeed(feedJson, label);
      if (parsed.length === 0) throw new Error("no advisories found");
      setExtra((cur) => [...parsed, ...cur]);
      setSources((cur) => (cur.includes(label) ? cur : [...cur, label]));
      setFeedErr(null);
      setShowAdd(false);
      setFeedName("");
      setFeedJson(SAMPLE_FEED);
      pushToast(`Collected ${parsed.length} advisory(ies) from “${label}”`, "ok");
    } catch (e) {
      setFeedErr(e instanceof Error ? e.message : "could not parse feed");
    }
  };

  return (
    <div className="scroll" style={{ height: "100%", padding: "26px 32px 40px" }}>
      <div style={{ maxWidth: 1000, margin: "0 auto" }}>
        <PageHead
          title="Threat feed"
          sub="Actively-exploited advisories with suggested ATT&CK techniques — fold emerging threats into the TTP library."
        />

        {/* Collection sources — which resources we collect advisories from. */}
        <div className="card" style={{ padding: 16, marginBottom: 16 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div className="eyebrow" style={{ marginBottom: 8 }}>Collection sources</div>
              <div style={{ display: "flex", flexWrap: "wrap", gap: 7 }}>
                {sources.length === 0 ? (
                  <span style={{ fontSize: 12, color: "var(--text-3)" }}>No sources configured.</span>
                ) : (
                  sources.map((s) => (
                    <span key={s} className="pill info" style={{ height: 22 }}>
                      <Icons.Activity size={11} /> {s}
                    </span>
                  ))
                )}
              </div>
            </div>
            <button className="btn ghost sm" onClick={() => setShowAdd((v) => !v)}>
              <Icons.Plus size={13} /> Add a feed
            </button>
          </div>

          {showAdd && (
            <div style={{ marginTop: 14, borderTop: "1px solid var(--border)", paddingTop: 14 }}>
              <div style={{ fontSize: 12, color: "var(--text-2)", lineHeight: 1.5, marginBottom: 10 }}>
                Paste advisories in RInfra&apos;s Advisory JSON schema — a top-level array or{" "}
                <span className="mono">{`{ "advisories": [...] }`}</span>. Each entry only needs{" "}
                <span className="mono">id</span>/<span className="mono">title</span>/<span className="mono">summary</span>;
                ATT&CK suggestions are derived automatically when omitted. On a deployed server, point
                <span className="mono"> RINFRA_THREATFEED_URLS</span>/<span className="mono">_FILES</span> at the same
                shape to collect it on every refresh — this preview adds it to the current view.
              </div>
              <input
                className="input"
                placeholder="Feed name (e.g. Acme Threat Intel)"
                value={feedName}
                onChange={(e) => setFeedName(e.target.value)}
                style={{ width: "100%", marginBottom: 8 }}
              />
              <textarea
                className="input mono"
                value={feedJson}
                onChange={(e) => setFeedJson(e.target.value)}
                spellCheck={false}
                style={{ width: "100%", minHeight: 150, fontSize: 12, resize: "vertical" }}
              />
              {feedErr && (
                <div style={{ fontSize: 12, color: "var(--danger)", marginTop: 8 }}>
                  <Icons.AlertTriangle size={12} /> {feedErr}
                </div>
              )}
              <div style={{ display: "flex", gap: 8, marginTop: 10 }}>
                <button className="btn primary sm" onClick={importFeed}>
                  <Icons.Plus size={13} /> Collect feed
                </button>
                <button className="btn ghost sm" onClick={() => { setShowAdd(false); setFeedErr(null); }}>
                  Cancel
                </button>
              </div>
            </div>
          )}
        </div>

        {advisories === null ? (
          <div style={{ fontSize: 13, color: "var(--text-3)" }}>Loading advisories…</div>
        ) : allAdvisories.length === 0 ? (
          <div style={{ fontSize: 13, color: "var(--text-3)" }}>No advisories available.</div>
        ) : (
          <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
            {allAdvisories.map((adv) => (
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
                      {adv.url ? (
                        <a className="mono" href={adv.url} target="_blank" rel="noreferrer" style={{ color: "var(--accent)" }}>
                          {adv.id}
                        </a>
                      ) : (
                        <span className="mono">{adv.id}</span>
                      )}{" "}
                      · {adv.source}
                      {(adv.vendor || adv.product) && <> · {adv.vendor} {adv.product}</>}
                      {adv.published && <> · {adv.published}</>}
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
          Suggested techniques are heuristic mappings from advisory text — review before use. Choose which
          resources to collect with <span className="mono">RINFRA_THREATFEED</span> (e.g.
          <span className="mono"> bundled,cisa-kev</span>), or add your own feeds in RInfra&apos;s Advisory JSON
          schema via <span className="mono">RINFRA_THREATFEED_URLS</span> /{" "}
          <span className="mono">RINFRA_THREATFEED_FILES</span> (see{" "}
          <span className="mono">config/threatfeed.example.json</span>).
        </div>
      </div>
    </div>
  );
}
