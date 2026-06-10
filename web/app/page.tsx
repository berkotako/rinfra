"use client";
import React from "react";
import Link from "next/link";
import { Icons } from "../components/icons";

const STEPS: { n: number; title: string; body: string }[] = [
  {
    n: 1,
    title: "Create an engagement",
    body: "Open Engagements → New engagement. Record the client, scope, rules of engagement, and a named authorizing party. No infrastructure can deploy until authorization is on file.",
  },
  {
    n: 2,
    title: "Compose the infrastructure",
    body: "On the Infrastructure canvas, drag redirectors, C2 servers, and staging hosts from the palette. Drag a node's right port to wire traffic flow, and configure each node in the inspector.",
  },
  {
    n: 3,
    title: "Validate & deploy",
    body: "Use Validate to check the topology, then Deploy. Nodes transition pending → provisioning → live with a running asset count and burn-rate. Deploy is blocked unless the engagement is authorized.",
  },
  {
    n: 4,
    title: "Run an emulation",
    body: "On Emulation, pick an ATT&CK-mapped scenario (APT29, FIN7, ransomware) and run it. Watch techniques execute live with per-step status.",
  },
  {
    n: 5,
    title: "Review coverage",
    body: "Coverage & Reports shows an ATT&CK coverage matrix and an engagement report built from the runs. Tear down the infrastructure when you're done.",
  },
];

const SCREENS: { href: string; icon: string; title: string; desc: string }[] = [
  { href: "/engagements", icon: "Target", title: "Engagements", desc: "Authorized operations with scope, RoE, and the authorization gate." },
  { href: "/infrastructure", icon: "Network", title: "Infrastructure Builder", desc: "Drag-and-drop attack-infrastructure canvas with deploy / tear-down." },
  { href: "/c2", icon: "Server", title: "C2 Frameworks", desc: "Sliver, Mythic, Havoc, Cobalt Strike and more, by orchestration tier." },
  { href: "/emulation", icon: "Crosshair", title: "Emulation Runner", desc: "ATT&CK scenarios with a live, per-technique execution timeline." },
  { href: "/reporting", icon: "FileText", title: "Coverage & Reports", desc: "ATT&CK coverage matrix and the engagement report." },
];

export default function Home() {
  return (
    <div className="scroll" style={{ height: "100%", padding: "32px 32px 48px" }}>
      <div style={{ maxWidth: 980, margin: "0 auto" }}>
        {/* hero */}
        <div className="card fade-up" style={{ padding: "32px 34px", marginBottom: 22 }}>
          <span className="pill accent" style={{ marginBottom: 14 }}>
            <span className="dot" /> Interactive demo · mock data
          </span>
          <h1 style={{ fontSize: 30, fontWeight: 600, letterSpacing: "-0.02em", lineHeight: 1.1 }}>
            RInfra — red-team operations platform
          </h1>
          <p style={{ fontSize: 15, color: "var(--text-2)", lineHeight: 1.6, marginTop: 12, maxWidth: 680 }}>
            Visually compose attack infrastructure across AWS, GCP, Azure and DigitalOcean,
            deploy and front C2 frameworks, and run ATT&amp;CK-mapped adversary-emulation
            scenarios — all bound to an authorized engagement with a full audit trail.
            This is a live, self-contained demo: every action runs against in-browser mock
            data, so nothing is provisioned and no backend is required.
          </p>
          <div style={{ display: "flex", gap: 10, marginTop: 22, flexWrap: "wrap" }}>
            <Link href="/infrastructure" style={{ textDecoration: "none" }}>
              <button className="btn primary" style={{ height: 38, padding: "0 16px" }}>
                <Icons.Network size={16} /> Launch the console
              </button>
            </Link>
            <Link href="/engagements" style={{ textDecoration: "none" }}>
              <button className="btn" style={{ height: 38, padding: "0 16px" }}>
                <Icons.Target size={16} /> Browse engagements
              </button>
            </Link>
            <a
              href="https://github.com/berkotako/rinfra"
              target="_blank"
              rel="noreferrer"
              style={{ textDecoration: "none" }}
            >
              <button className="btn ghost" style={{ height: 38, padding: "0 14px" }}>
                <Icons.FileText size={16} /> Source &amp; docs
              </button>
            </a>
          </div>
        </div>

        {/* how to use */}
        <div className="eyebrow" style={{ marginBottom: 12 }}>How to use</div>
        <div className="card" style={{ padding: "6px 0", marginBottom: 22 }}>
          {STEPS.map((s, i) => (
            <div
              key={s.n}
              style={{
                display: "flex",
                gap: 16,
                padding: "16px 22px",
                borderTop: i === 0 ? "none" : "1px solid var(--border)",
              }}
            >
              <div
                style={{
                  width: 30,
                  height: 30,
                  flex: "none",
                  borderRadius: 99,
                  display: "grid",
                  placeItems: "center",
                  background: "var(--accent-soft)",
                  color: "var(--accent)",
                  border: "1px solid var(--accent-soft-border)",
                  fontWeight: 600,
                  fontSize: 13,
                }}
                className="mono"
              >
                {s.n}
              </div>
              <div>
                <div style={{ fontSize: 14, fontWeight: 600 }}>{s.title}</div>
                <div style={{ fontSize: 13, color: "var(--text-2)", lineHeight: 1.55, marginTop: 3 }}>
                  {s.body}
                </div>
              </div>
            </div>
          ))}
        </div>

        {/* screens */}
        <div className="eyebrow" style={{ marginBottom: 12 }}>The five screens</div>
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))",
            gap: 12,
          }}
        >
          {SCREENS.map((s) => {
            const Ico = Icons[s.icon] || Icons.Target;
            return (
              <Link key={s.href} href={s.href} style={{ textDecoration: "none" }}>
                <div
                  className="card menu-item"
                  style={{ padding: "16px 18px", height: "100%", cursor: "pointer" }}
                >
                  <div style={{ display: "flex", alignItems: "center", gap: 11, marginBottom: 8 }}>
                    <span
                      style={{
                        width: 34,
                        height: 34,
                        borderRadius: 9,
                        display: "grid",
                        placeItems: "center",
                        background: "var(--surface-3)",
                        border: "1px solid var(--border)",
                        color: "var(--accent)",
                      }}
                    >
                      <Ico size={18} />
                    </span>
                    <span style={{ fontSize: 14.5, fontWeight: 600 }}>{s.title}</span>
                  </div>
                  <div style={{ fontSize: 12.5, color: "var(--text-3)", lineHeight: 1.5 }}>
                    {s.desc}
                  </div>
                </div>
              </Link>
            );
          })}
        </div>

        <div style={{ fontSize: 12, color: "var(--text-4)", marginTop: 26, lineHeight: 1.6 }}>
          Demo mode uses in-browser mock data — deploys, tear-downs and emulation runs are
          simulated. Connecting the console to a running RInfra control plane (set{" "}
          <span className="mono">NEXT_PUBLIC_RINFRA_API</span>) drives the same UI against the
          real REST + SSE API. Tip: the appearance menu (top bar) toggles soft-dark mode and the
          accent color.
        </div>
      </div>
    </div>
  );
}
