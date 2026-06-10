"use client";
import React, { useEffect } from "react";
import Link from "next/link";
import { Icons } from "../components/icons";

const REPO = "https://github.com/berkotako/rinfra";

/* ----------------------------------------------------------------------------
   C2 framework reference — informative "what is it" content for each solution.
   Factual descriptions of publicly documented security tooling.
---------------------------------------------------------------------------- */

type Tier = "orchestrated" | "scripted" | "fronted";

interface Framework {
  id: string;
  name: string;
  maker: string;
  license: "open" | "commercial";
  listeners: string[];
  tier: Tier;
  what: string;
}

const FRAMEWORKS: Framework[] = [
  {
    id: "sliver",
    name: "Sliver",
    maker: "BishopFox · Go",
    license: "open",
    listeners: ["mTLS", "HTTPS", "WireGuard", "DNS"],
    tier: "orchestrated",
    what: "An open-source, cross-platform adversary-emulation and C2 framework. It generates implants that beacon over mTLS, HTTP(S), DNS or WireGuard and exposes a full gRPC operator API — the reason RInfra can drive it end to end. A popular open alternative to commercial tooling.",
  },
  {
    id: "mythic",
    name: "Mythic",
    maker: "@its_a_feature_ · Python",
    license: "open",
    listeners: ["HTTP", "HTTPS", "WebSocket", "SMB"],
    tier: "orchestrated",
    what: "A modular, open-source C2 framework built around Docker: agents and C2 profiles are pluggable containers managed from a web UI and a documented API. Its plugin model makes it easy to extend with new agents and transports.",
  },
  {
    id: "metasploit",
    name: "Metasploit",
    maker: "Rapid7 · Ruby",
    license: "open",
    listeners: ["TCP", "HTTPS", "HTTP"],
    tier: "orchestrated",
    what: "The long-standing open-source penetration-testing framework. Its msfrpcd RPC service lets RInfra spin up multi/handler listeners and drive Meterpreter sessions programmatically. Ubiquitous for initial access and post-exploitation.",
  },
  {
    id: "custom",
    name: "In-house / Custom",
    maker: "Your image",
    license: "open",
    listeners: ["Custom"],
    tier: "orchestrated",
    what: "Your own framework, brought as a container image plus a listener spec. RInfra deploys and fronts it the same way as the built-ins; you own the operator surface and decide how much automation it exposes.",
  },
  {
    id: "havoc",
    name: "Havoc",
    maker: "@C5pider · C / ASM",
    license: "open",
    listeners: ["HTTP", "HTTPS", "SMB"],
    tier: "scripted",
    what: "A modern open-source post-exploitation framework. Its Demon agent supports techniques like sleep obfuscation; RInfra automates the subset its scripted operator API exposes and leaves the rest to the operator.",
  },
  {
    id: "poshc2",
    name: "PoshC2",
    maker: "Nettitude · Python / PS",
    license: "open",
    listeners: ["HTTP", "HTTPS"],
    tier: "scripted",
    what: "An open-source, proxy-aware C2 with PowerShell, C# and Python implants. It is scriptable but without a modern formal API, so RInfra automates a narrower subset of its operator actions.",
  },
  {
    id: "cobalt",
    name: "Cobalt Strike",
    maker: "Fortra",
    license: "commercial",
    listeners: ["HTTP", "HTTPS", "DNS", "SMB", "TCP"],
    tier: "fronted",
    what: "The industry-standard commercial adversary-simulation platform. Its Beacon payload and malleable C2 profiles are widely studied and emulated. License-gated: RInfra provisions and fronts the team server, and a human operates it from their own licensed client.",
  },
  {
    id: "bruteratel",
    name: "Brute Ratel C4",
    maker: "Chetan Nayak · Dark Vortex",
    license: "commercial",
    listeners: ["HTTP", "HTTPS", "DNS"],
    tier: "fronted",
    what: "A commercial C2 framework built for red teams, known for its focus on operating against modern endpoint defenses. License-gated: RInfra stands up and fronts the server while the operator drives the Badger agent manually with their own key.",
  },
];

const TIERS: { id: Tier; label: string; icon: keyof typeof Icons; cls: string; blurb: string }[] = [
  { id: "orchestrated", label: "Orchestrated", icon: "Bolt", cls: "ok", blurb: "Deploy + redirector + automated ATT&CK emulation through the framework's operator API." },
  { id: "scripted", label: "Scripted", icon: "Terminal", cls: "info", blurb: "Deploy + redirector + partial automation over the subset the framework scripts expose." },
  { id: "fronted", label: "Fronted", icon: "Power", cls: "", blurb: "Deploy + redirector only; a human operates the framework — often license-gated." },
];

const tierMeta = (t: Tier) => TIERS.find((x) => x.id === t)!;

/* ----------------------------------------------------------------------------
   Inline SVG diagrams — light-theme tokens.
---------------------------------------------------------------------------- */

function TopologyArt() {
  const node = (
    x: number, y: number, label: string, sub: string, icon: keyof typeof Icons, accent: boolean
  ) => {
    const Ico = Icons[icon] || Icons.Server;
    return (
      <g transform={`translate(${x} ${y})`}>
        <rect width="150" height="62" rx="11" fill="var(--surface)" stroke={accent ? "var(--accent)" : "var(--border-2)"} strokeWidth={accent ? 2 : 1.25} />
        <g transform="translate(12 15)">
          <rect width="32" height="32" rx="8" fill="var(--accent-soft)" />
          <g transform="translate(8 8)" style={{ color: "var(--accent)" }}><Ico size={16} /></g>
        </g>
        <text x="54" y="27" fontSize="12.5" fontWeight={600} fill="var(--text)">{label}</text>
        <text x="54" y="44" fontSize="10.5" fill="var(--text-3)" className="mono">{sub}</text>
        <circle cx="138" cy="14" r="4" fill="var(--ok)" />
      </g>
    );
  };
  return (
    <svg viewBox="0 0 560 380" width="100%" style={{ display: "block", maxWidth: 560 }} role="img" aria-label="Attack-infrastructure topology: redirectors fronting a C2 server and a staging host">
      <defs>
        <pattern id="dots" width="22" height="22" patternUnits="userSpaceOnUse"><circle cx="1.5" cy="1.5" r="1.3" fill="var(--grid-dot)" /></pattern>
        <marker id="arrow" markerWidth="9" markerHeight="9" refX="7" refY="4.5" orient="auto"><path d="M1 1 L7 4.5 L1 8" fill="none" stroke="var(--accent)" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" /></marker>
      </defs>
      <rect x="0" y="0" width="560" height="380" rx="16" fill="var(--canvas-bg)" />
      <rect x="0" y="0" width="560" height="380" rx="16" fill="url(#dots)" />
      <path d="M 170 66 C 250 66, 230 150, 320 150" fill="none" stroke="var(--accent)" strokeWidth="2" opacity="0.85" markerEnd="url(#arrow)" />
      <path d="M 170 250 C 250 250, 240 178, 320 166" fill="none" stroke="var(--accent)" strokeWidth="2" opacity="0.85" markerEnd="url(#arrow)" />
      <path d="M 470 150 C 510 150, 500 230, 410 250" fill="none" stroke="var(--accent)" strokeWidth="2" opacity="0.85" markerEnd="url(#arrow)" />
      {node(20, 35, "edge-https-01", "203.0.113.18", "Globe", false)}
      {node(20, 219, "edge-dns-01", "ns1.sync.com", "Dns", false)}
      {node(320, 120, "teamserver-01", "Sliver · mTLS", "Server", true)}
      {node(330, 220, "stage-host-01", "203.0.113.44", "HardDrive", false)}
    </svg>
  );
}

function ArchitectureArt() {
  const box = (x: number, y: number, w: number, h: number, title: string, accent?: boolean) => (
    <g transform={`translate(${x} ${y})`}>
      <rect width={w} height={h} rx="9" fill={accent ? "var(--accent-soft)" : "var(--surface-2)"} stroke={accent ? "var(--accent-soft-border)" : "var(--border)"} strokeWidth="1" />
      <text x={w / 2} y={h / 2 + 4} fontSize="12" fontWeight={accent ? 600 : 500} textAnchor="middle" fill={accent ? "var(--accent)" : "var(--text-2)"}>{title}</text>
    </g>
  );
  return (
    <svg viewBox="0 0 720 360" width="100%" style={{ display: "block" }} role="img" aria-label="RInfra architecture: web console over a Go control plane driving clouds, C2 frameworks, emulation and Postgres">
      <defs><marker id="aarrow" markerWidth="8" markerHeight="8" refX="6" refY="4" orient="auto"><path d="M1 1 L6 4 L1 7" fill="none" stroke="var(--text-4)" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" /></marker></defs>
      {box(255, 16, 210, 46, "Web console — Next.js + React", true)}
      <path d="M 360 62 L 360 96" fill="none" stroke="var(--text-4)" strokeWidth="1.4" markerEnd="url(#aarrow)" />
      <text x="372" y="84" fontSize="10.5" fill="var(--text-3)" className="mono">REST · SSE</text>
      <rect x="40" y="100" width="640" height="150" rx="12" fill="none" stroke="var(--border-2)" strokeWidth="1.25" strokeDasharray="4 4" />
      <text x="56" y="120" fontSize="11" fontWeight={600} fill="var(--text-3)" letterSpacing="0.04em">CONTROL PLANE · GO</text>
      {box(56, 132, 118, 40, "HTTP API")}
      {box(186, 132, 130, 40, "Services + auth gate")}
      {box(328, 132, 150, 40, "Orchestration (Pulumi)")}
      {box(490, 132, 80, 40, "Audit")}
      {box(582, 132, 82, 40, "Store")}
      {box(56, 192, 150, 40, "Cloud — 4 providers")}
      {box(218, 192, 150, 40, "C2 — 8 frameworks")}
      {box(380, 192, 150, 40, "Emulation engine")}
      {box(542, 192, 122, 40, "Secrets (AES-GCM)")}
      <path d="M 131 250 L 131 286" fill="none" stroke="var(--text-4)" strokeWidth="1.4" markerEnd="url(#aarrow)" />
      <path d="M 293 250 L 293 286" fill="none" stroke="var(--text-4)" strokeWidth="1.4" markerEnd="url(#aarrow)" />
      <path d="M 643 250 L 643 286" fill="none" stroke="var(--text-4)" strokeWidth="1.4" markerEnd="url(#aarrow)" />
      {box(56, 290, 150, 40, "AWS · GCP · Azure · DO")}
      {box(218, 290, 150, 40, "Sliver · Mythic · …")}
      {box(582, 290, 82, 40, "Postgres")}
    </svg>
  );
}

/* ----------------------------------------------------------------------------
   Page
---------------------------------------------------------------------------- */

function Section({ id, alt, children }: { id?: string; alt?: boolean; children: React.ReactNode }) {
  return (
    <section id={id} style={{ background: alt ? "var(--surface)" : "var(--bg)", borderTop: "1px solid var(--border)" }}>
      <div style={{ maxWidth: 1080, margin: "0 auto", padding: "56px 24px" }}>{children}</div>
    </section>
  );
}

function FrameworkCard({ f }: { f: Framework }) {
  const tm = tierMeta(f.tier);
  const TierIco = Icons[tm.icon] || Icons.Power;
  return (
    <div className="card" style={{ padding: "18px 20px", display: "flex", flexDirection: "column", gap: 10 }}>
      <div style={{ display: "flex", alignItems: "flex-start", gap: 12 }}>
        <span style={{ width: 40, height: 40, flex: "none", borderRadius: 9, display: "grid", placeItems: "center", background: "var(--surface-3)", border: "1px solid var(--border)", color: "var(--accent)" }}>
          <Icons.Server size={20} />
        </span>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
            <span style={{ fontSize: 15.5, fontWeight: 600 }}>{f.name}</span>
            <span className={"pill " + tm.cls} style={{ height: 20 }}>
              <TierIco size={12} /> {tm.label}
            </span>
            {f.license === "commercial" && (
              <span className="pill warn" style={{ height: 20 }}>
                <Icons.Lock size={11} /> Licensed
              </span>
            )}
          </div>
          <div style={{ fontSize: 11.5, color: "var(--text-3)", marginTop: 3 }}>{f.maker}</div>
        </div>
      </div>
      <div style={{ fontSize: 13, color: "var(--text-2)", lineHeight: 1.55 }}>{f.what}</div>
      <div style={{ display: "flex", gap: 5, flexWrap: "wrap", marginTop: "auto", paddingTop: 4 }}>
        {f.listeners.map((l) => (
          <span key={l} className="mono" style={{ fontSize: 10.5, color: "var(--text-3)", background: "var(--surface-3)", border: "1px solid var(--border)", borderRadius: 4, padding: "1px 6px" }}>{l}</span>
        ))}
      </div>
    </div>
  );
}

const SCREENS: { href: string; icon: keyof typeof Icons; title: string; desc: string }[] = [
  { href: "/engagements", icon: "Target", title: "Engagements", desc: "Authorized operations with scope, RoE, and the authorization gate." },
  { href: "/infrastructure", icon: "Network", title: "Infrastructure Builder", desc: "Drag-and-drop attack-infrastructure canvas with deploy / tear-down." },
  { href: "/c2", icon: "Server", title: "C2 Frameworks", desc: "Pick a framework per server; tiers and license gating built in." },
  { href: "/emulation", icon: "Crosshair", title: "Emulation Runner", desc: "ATT&CK scenarios with a live, per-technique execution timeline." },
  { href: "/reporting", icon: "FileText", title: "Coverage & Reports", desc: "ATT&CK coverage matrix and the engagement report." },
];

const STACK = ["Go 1.24", "Next.js", "React", "Pulumi", "Postgres", "chi", "SSE", "AES-256-GCM", "MITRE ATT&CK"];

export default function Home() {
  // Force light theme on the landing page; restore the app's theme on leave.
  useEffect(() => {
    const html = document.documentElement;
    const prev = html.getAttribute("data-theme");
    html.setAttribute("data-theme", "");
    return () => {
      if (prev) html.setAttribute("data-theme", prev);
    };
  }, []);

  const byTier = (t: Tier) => FRAMEWORKS.filter((f) => f.tier === t);

  return (
    <div style={{ height: "100%", overflowY: "auto", background: "var(--bg)" }}>
      {/* nav */}
      <header style={{ position: "sticky", top: 0, zIndex: 20, background: "color-mix(in oklch, var(--surface) 86%, transparent)", backdropFilter: "blur(8px)", borderBottom: "1px solid var(--border)" }}>
        <div style={{ maxWidth: 1080, margin: "0 auto", padding: "11px 24px", display: "flex", alignItems: "center", gap: 14 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <div style={{ width: 30, height: 30, borderRadius: 8, background: "var(--accent)", color: "var(--accent-contrast)", display: "grid", placeItems: "center", boxShadow: "var(--shadow-sm)" }}>
              <Icons.Logo size={18} />
            </div>
            <span style={{ fontSize: 15.5, fontWeight: 600, letterSpacing: "-0.02em" }}>RInfra</span>
          </div>
          <nav style={{ display: "flex", gap: 4, marginLeft: 8 }} className="lp-nav">
            {[["Frameworks", "#frameworks"], ["Tiers", "#tiers"], ["Architecture", "#architecture"], ["Demo", "#demo"]].map(([l, h]) => (
              <a key={h} href={h} className="btn ghost sm" style={{ textDecoration: "none" }}>{l}</a>
            ))}
          </nav>
          <div style={{ flex: 1 }} />
          <a href={REPO} target="_blank" rel="noreferrer" className="btn sm" style={{ textDecoration: "none" }}><Icons.FileText size={15} /> GitHub</a>
          <Link href="/infrastructure" style={{ textDecoration: "none" }}>
            <button className="btn primary sm"><Icons.Bolt size={15} /> Launch demo</button>
          </Link>
        </div>
      </header>

      {/* hero */}
      <div style={{ maxWidth: 1080, margin: "0 auto", padding: "56px 24px 48px" }}>
        <div style={{ display: "grid", gridTemplateColumns: "minmax(0, 1.05fr) minmax(0, 1fr)", gap: 40, alignItems: "center" }} className="lp-hero">
          <div>
            <span className="pill accent"><span className="dot" /> Red-team &amp; purple-team operations</span>
            <h1 style={{ fontSize: 44, lineHeight: 1.05, fontWeight: 600, letterSpacing: "-0.03em", marginTop: 16 }}>
              Deploy and front the C2 you need,
              <br />
              <span style={{ color: "var(--accent)" }}>from one console.</span>
            </h1>
            <p style={{ fontSize: 16, color: "var(--text-2)", lineHeight: 1.6, marginTop: 18, maxWidth: 540 }}>
              RInfra composes attack infrastructure across four clouds and stands up
              the command-and-control framework of your choice behind it — Sliver,
              Mythic, Cobalt Strike, Brute Ratel and more. Below is what each one is,
              and how much of it RInfra can automate.
            </p>
            <div style={{ display: "flex", gap: 10, marginTop: 26, flexWrap: "wrap" }}>
              <a href="#frameworks" className="btn primary" style={{ height: 40, padding: "0 18px", textDecoration: "none" }}><Icons.Server size={16} /> Browse C2 frameworks</a>
              <a href="#demo" className="btn" style={{ height: 40, padding: "0 16px", textDecoration: "none" }}><Icons.Network size={16} /> Jump to the demo</a>
            </div>
            <div style={{ display: "flex", gap: 26, marginTop: 28, flexWrap: "wrap" }}>
              {[["4", "cloud providers"], ["8", "C2 frameworks"], ["3", "support tiers"]].map(([n, l]) => (
                <div key={l}>
                  <div className="mono" style={{ fontSize: 24, fontWeight: 600, letterSpacing: "-0.02em" }}>{n}</div>
                  <div style={{ fontSize: 12.5, color: "var(--text-3)" }}>{l}</div>
                </div>
              ))}
            </div>
          </div>
          <div className="card" style={{ padding: 16, boxShadow: "var(--shadow-lg)" }}><TopologyArt /></div>
        </div>

        <div style={{ display: "flex", alignItems: "center", gap: 10, marginTop: 36, padding: "12px 16px", borderRadius: "var(--r-md)", background: "var(--surface-2)", border: "1px solid var(--border)", fontSize: 13, color: "var(--text-2)", lineHeight: 1.5 }}>
          <span style={{ color: "var(--accent)", flex: "none" }}><Icons.Shield size={17} /></span>
          <span><strong style={{ color: "var(--text)" }}>Composes, never authors.</strong> RInfra deploys and fronts existing, publicly available frameworks — it writes no implants, payloads, exploits or evasion. Commercial frameworks always use the customer&apos;s own license key, never bundled.</span>
        </div>
      </div>

      {/* frameworks index */}
      <Section id="frameworks" alt>
        <div className="eyebrow">C2 framework index</div>
        <h2 style={{ fontSize: 26, fontWeight: 600, letterSpacing: "-0.02em", marginTop: 8, marginBottom: 8 }}>
          Eight frameworks, grouped by what RInfra automates
        </h2>
        <p style={{ fontSize: 14.5, color: "var(--text-2)", lineHeight: 1.6, maxWidth: 660, marginBottom: 26 }}>
          A quick reference to each command-and-control solution RInfra supports — what it is,
          who makes it, its listeners, and its licensing. The badge shows the support tier,
          explained <a href="#tiers" style={{ color: "var(--accent)" }}>below</a>.
        </p>

        {(["orchestrated", "scripted", "fronted"] as Tier[]).map((t) => {
          const tm = tierMeta(t);
          const TierIco = Icons[tm.icon] || Icons.Power;
          return (
            <div key={t} style={{ marginBottom: 26 }}>
              <div style={{ display: "flex", alignItems: "center", gap: 9, marginBottom: 12 }}>
                <span className={"pill " + tm.cls}><TierIco size={12} /> {tm.label}</span>
                <span style={{ fontSize: 12.5, color: "var(--text-3)" }}>{tm.blurb}</span>
              </div>
              <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(330px, 1fr))", gap: 12 }}>
                {byTier(t).map((f) => <FrameworkCard key={f.id} f={f} />)}
              </div>
            </div>
          );
        })}
      </Section>

      {/* tiers explained */}
      <Section id="tiers">
        <div className="eyebrow">Support tiers</div>
        <h2 style={{ fontSize: 26, fontWeight: 600, letterSpacing: "-0.02em", marginTop: 8, marginBottom: 8 }}>
          How far RInfra can drive each framework
        </h2>
        <p style={{ fontSize: 14.5, color: "var(--text-2)", lineHeight: 1.6, maxWidth: 660, marginBottom: 24 }}>
          Provisioning and fronting are uniform across frameworks; <em>control</em> is tiered, because a
          framework can only be automated if it exposes a usable operator API. Emulation automation lights
          up on the Orchestrated and Scripted tiers.
        </p>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(260px, 1fr))", gap: 14 }}>
          {TIERS.map((t) => {
            const Ico = Icons[t.icon] || Icons.Power;
            const names = FRAMEWORKS.filter((f) => f.tier === t.id).map((f) => f.name).join(", ");
            return (
              <div key={t.id} className="card" style={{ padding: "18px 20px" }}>
                <span className={"pill " + t.cls}><Ico size={12} /> {t.label}</span>
                <div style={{ fontSize: 13.5, color: "var(--text-2)", lineHeight: 1.55, marginTop: 12 }}>{t.blurb}</div>
                <div style={{ fontSize: 12, color: "var(--text-3)", marginTop: 12, paddingTop: 12, borderTop: "1px solid var(--border)" }}>{names}</div>
              </div>
            );
          })}
        </div>
      </Section>

      {/* architecture */}
      <Section id="architecture" alt>
        <div className="eyebrow">Architecture</div>
        <h2 style={{ fontSize: 26, fontWeight: 600, letterSpacing: "-0.02em", marginTop: 8, marginBottom: 8 }}>
          A thin web console over a Go control plane
        </h2>
        <p style={{ fontSize: 14.5, color: "var(--text-2)", lineHeight: 1.6, maxWidth: 640, marginBottom: 22 }}>
          The console talks to the control plane over REST + SSE. Services enforce the authorization gate
          and emit audit events; an orchestration engine compiles the canvas topology into Pulumi stacks per
          provider, while C2 and emulation adapters drive the frameworks above. State and the immutable audit
          log live in Postgres.
        </p>
        <div className="card" style={{ padding: "26px 22px", overflowX: "auto" }}>
          <div style={{ minWidth: 680 }}><ArchitectureArt /></div>
        </div>
        <div style={{ display: "flex", gap: 16, flexWrap: "wrap", marginTop: 18 }}>
          {STACK.map((s) => <span key={s} className="pill" style={{ background: "var(--surface-2)" }}>{s}</span>)}
        </div>
      </Section>

      {/* demo */}
      <Section id="demo">
        <div className="eyebrow">Live demo</div>
        <h2 style={{ fontSize: 26, fontWeight: 600, letterSpacing: "-0.02em", marginTop: 8, marginBottom: 6 }}>
          Try the console — five screens, fully interactive
        </h2>
        <p style={{ fontSize: 14, color: "var(--text-3)", marginBottom: 22, maxWidth: 620, lineHeight: 1.5 }}>
          The demo runs entirely in your browser on mock data — nothing is provisioned and no backend is
          required. Build a topology, deploy it, run an emulation, and read the coverage report.
        </p>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(300px, 1fr))", gap: 12, marginBottom: 22 }}>
          {SCREENS.map((s) => {
            const Ico = Icons[s.icon] || Icons.Target;
            return (
              <Link key={s.href} href={s.href} style={{ textDecoration: "none" }}>
                <div className="card menu-item" style={{ padding: "16px 18px", height: "100%", cursor: "pointer" }}>
                  <div style={{ display: "flex", alignItems: "center", gap: 11, marginBottom: 8 }}>
                    <span style={{ width: 34, height: 34, borderRadius: 9, display: "grid", placeItems: "center", background: "var(--surface-3)", border: "1px solid var(--border)", color: "var(--accent)" }}><Ico size={18} /></span>
                    <span style={{ fontSize: 14.5, fontWeight: 600 }}>{s.title}</span>
                    <span style={{ flex: 1 }} />
                    <span style={{ color: "var(--text-4)" }}><Icons.ArrowRight size={16} /></span>
                  </div>
                  <div style={{ fontSize: 12.5, color: "var(--text-3)", lineHeight: 1.5 }}>{s.desc}</div>
                </div>
              </Link>
            );
          })}
        </div>
        <Link href="/infrastructure" style={{ textDecoration: "none" }}>
          <button className="btn primary" style={{ height: 40, padding: "0 18px" }}><Icons.Bolt size={16} /> Launch the console</button>
        </Link>
      </Section>

      {/* footer */}
      <footer style={{ borderTop: "1px solid var(--border)", background: "var(--bg)" }}>
        <div style={{ maxWidth: 1080, margin: "0 auto", padding: "24px", display: "flex", alignItems: "center", justifyContent: "space-between", gap: 16, flexWrap: "wrap", fontSize: 12.5, color: "var(--text-3)" }}>
          <span>RInfra — red-team &amp; purple-team operations platform.</span>
          <span>Demo runs on in-browser mock data. No infrastructure is provisioned.</span>
        </div>
      </footer>

      <style>{`
        @media (max-width: 860px) {
          .lp-hero { grid-template-columns: 1fr !important; }
          .lp-nav { display: none !important; }
        }
      `}</style>
    </div>
  );
}
