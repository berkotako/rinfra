"use client";
import React from "react";
import Link from "next/link";
import { Icons } from "../components/icons";
import { useStore } from "../lib/store";

const REPO = "https://github.com/berkotako/rinfra";

/* ----------------------------------------------------------------------------
   Inline SVG illustrations — theme-aware via design tokens (var(--...)).
   Hand-drawn so they stay crisp at any size and match the app's aesthetic.
---------------------------------------------------------------------------- */

function TopologyArt() {
  // A stylized attack-infrastructure graph echoing the builder canvas:
  // redirectors → C2 server → staging host, on a dotted grid.
  const node = (
    x: number,
    y: number,
    label: string,
    sub: string,
    icon: keyof typeof Icons,
    accent: boolean,
    live: boolean
  ) => {
    const Ico = Icons[icon] || Icons.Server;
    return (
      <g transform={`translate(${x} ${y})`}>
        <rect
          width="150"
          height="62"
          rx="11"
          fill="var(--surface)"
          stroke={accent ? "var(--accent)" : "var(--border-2)"}
          strokeWidth={accent ? 2 : 1.25}
        />
        <g transform="translate(12 15)">
          <rect width="32" height="32" rx="8" fill="var(--accent-soft)" />
          <g transform="translate(8 8)" style={{ color: "var(--accent)" }}>
            <Ico size={16} />
          </g>
        </g>
        <text x="54" y="27" fontSize="12.5" fontWeight={600} fill="var(--text)">
          {label}
        </text>
        <text x="54" y="44" fontSize="10.5" fill="var(--text-3)" className="mono">
          {sub}
        </text>
        <circle
          cx="138"
          cy="14"
          r="4"
          fill={live ? "var(--ok)" : "var(--text-4)"}
        />
      </g>
    );
  };

  return (
    <svg
      viewBox="0 0 560 380"
      width="100%"
      style={{ display: "block", maxWidth: 560 }}
      role="img"
      aria-label="Attack-infrastructure topology: redirectors fronting a C2 server and a staging host"
    >
      <defs>
        <pattern id="dots" width="22" height="22" patternUnits="userSpaceOnUse">
          <circle cx="1.5" cy="1.5" r="1.3" fill="var(--grid-dot)" />
        </pattern>
        <marker id="arrow" markerWidth="9" markerHeight="9" refX="7" refY="4.5" orient="auto">
          <path
            d="M1 1 L7 4.5 L1 8"
            fill="none"
            stroke="var(--accent)"
            strokeWidth="1.5"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </marker>
      </defs>

      <rect x="0" y="0" width="560" height="380" rx="16" fill="var(--canvas-bg)" />
      <rect x="0" y="0" width="560" height="380" rx="16" fill="url(#dots)" />

      {/* edges */}
      {[
        "M 170 66 C 250 66, 230 150, 320 150",
        "M 170 250 C 250 250, 240 178, 320 166",
        "M 470 158 C 500 158, 510 158, 500 158",
      ].slice(0, 2).map((d, i) => (
        <path
          key={i}
          d={d}
          fill="none"
          stroke="var(--accent)"
          strokeWidth="2"
          opacity="0.85"
          markerEnd="url(#arrow)"
        />
      ))}
      <path
        d="M 470 150 C 510 150, 500 230, 410 250"
        fill="none"
        stroke="var(--accent)"
        strokeWidth="2"
        opacity="0.85"
        markerEnd="url(#arrow)"
      />

      {/* nodes */}
      {node(20, 35, "edge-https-01", "203.0.113.18", "Globe", false, true)}
      {node(20, 219, "edge-dns-01", "ns1.sync.com", "Dns", false, true)}
      {node(320, 120, "teamserver-01", "Sliver · mTLS", "Server", true, true)}
      {node(330, 220, "stage-host-01", "203.0.113.44", "HardDrive", false, true)}
    </svg>
  );
}

function ArchitectureArt() {
  const box = (
    x: number,
    y: number,
    w: number,
    h: number,
    title: string,
    accent?: boolean
  ) => (
    <g transform={`translate(${x} ${y})`}>
      <rect
        width={w}
        height={h}
        rx="9"
        fill={accent ? "var(--accent-soft)" : "var(--surface-2)"}
        stroke={accent ? "var(--accent-soft-border)" : "var(--border)"}
        strokeWidth="1"
      />
      <text
        x={w / 2}
        y={h / 2 + 4}
        fontSize="12"
        fontWeight={accent ? 600 : 500}
        textAnchor="middle"
        fill={accent ? "var(--accent)" : "var(--text-2)"}
      >
        {title}
      </text>
    </g>
  );

  return (
    <svg
      viewBox="0 0 720 360"
      width="100%"
      style={{ display: "block" }}
      role="img"
      aria-label="RInfra architecture: web console over a Go control plane that drives cloud providers, C2 frameworks, an emulation engine, and Postgres"
    >
      <defs>
        <marker id="aarrow" markerWidth="8" markerHeight="8" refX="6" refY="4" orient="auto">
          <path d="M1 1 L6 4 L1 7" fill="none" stroke="var(--text-4)" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
        </marker>
      </defs>

      {/* frontend */}
      {box(255, 16, 210, 46, "Web console — Next.js + React", true)}

      {/* REST/SSE link */}
      <path d="M 360 62 L 360 96" fill="none" stroke="var(--text-4)" strokeWidth="1.4" markerEnd="url(#aarrow)" />
      <text x="372" y="84" fontSize="10.5" fill="var(--text-3)" className="mono">REST · SSE</text>

      {/* control plane container */}
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

      {/* downstream */}
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
   Sections
---------------------------------------------------------------------------- */

const CAPABILITIES: { icon: keyof typeof Icons; title: string; body: string }[] = [
  { icon: "Network", title: "Visual infrastructure builder", body: "Drag redirectors, C2 servers and staging hosts onto a canvas and wire traffic flow. Validate, deploy, and tear down with a live asset count and burn rate." },
  { icon: "Cloud", title: "Four clouds, one model", body: "Provision into the customer's own AWS, GCP, Azure or DigitalOcean account via Pulumi. Ingress and DNS are written deliberately per provider." },
  { icon: "Server", title: "Eight C2 frameworks", body: "Deploy and front Sliver, Mythic, Metasploit, Havoc, PoshC2, Cobalt Strike and Brute Ratel — tiered by how much RInfra can automate." },
  { icon: "Crosshair", title: "ATT&CK emulation", body: "Run portable, payload-free scenarios (APT29, FIN7, ransomware) through the framework's operator API with a live, per-technique timeline." },
  { icon: "FileText", title: "Coverage & reporting", body: "Roll runs up into an ATT&CK coverage matrix and an engagement report, and export an ATT&CK Navigator layer." },
  { icon: "ShieldCheck", title: "Authorized & audited", body: "Every deploy is gated on an authorized engagement, and every privileged action lands in an append-only, immutable audit log." },
];

const STEPS: { title: string; body: string }[] = [
  { title: "Create an engagement", body: "Record the client, scope, rules of engagement, and a named authorizing party. Nothing provisions until authorization is on file." },
  { title: "Compose the infrastructure", body: "Build the topology on the canvas and configure each node's provider, region, C2 framework and listener." },
  { title: "Validate & deploy", body: "Check the topology, then deploy. Nodes go pending → provisioning → live; deploy is blocked unless the engagement is authorized." },
  { title: "Run an emulation", body: "Pick an ATT&CK scenario and run it; watch techniques execute live with per-step status." },
  { title: "Review & tear down", body: "Read the coverage matrix and report, then tear the infrastructure down with a guaranteed, reconciled teardown." },
];

const SCREENS: { href: string; icon: keyof typeof Icons; title: string; desc: string }[] = [
  { href: "/engagements", icon: "Target", title: "Engagements", desc: "Authorized operations with scope, RoE, and the authorization gate." },
  { href: "/infrastructure", icon: "Network", title: "Infrastructure Builder", desc: "Drag-and-drop attack-infrastructure canvas with deploy / tear-down." },
  { href: "/c2", icon: "Server", title: "C2 Frameworks", desc: "Frameworks by orchestration tier, with license gating where needed." },
  { href: "/emulation", icon: "Crosshair", title: "Emulation Runner", desc: "ATT&CK scenarios with a live, per-technique execution timeline." },
  { href: "/reporting", icon: "FileText", title: "Coverage & Reports", desc: "ATT&CK coverage matrix and the engagement report." },
];

const STACK = ["Go 1.24", "Next.js", "React", "Pulumi", "Postgres", "pgx", "chi", "SSE", "AES-256-GCM", "MITRE ATT&CK"];

function Section({
  id,
  children,
  alt,
}: {
  id?: string;
  children: React.ReactNode;
  alt?: boolean;
}) {
  return (
    <section
      id={id}
      style={{
        background: alt ? "var(--surface)" : "var(--bg)",
        borderTop: "1px solid var(--border)",
      }}
    >
      <div style={{ maxWidth: 1080, margin: "0 auto", padding: "56px 24px" }}>
        {children}
      </div>
    </section>
  );
}

export default function Home() {
  const { preferences, setTheme } = useStore();
  const dark = preferences.theme === "dark";

  return (
    <div style={{ height: "100%", overflowY: "auto", background: "var(--bg)" }}>
      {/* top nav */}
      <header
        style={{
          position: "sticky",
          top: 0,
          zIndex: 20,
          background: "color-mix(in oklch, var(--surface) 86%, transparent)",
          backdropFilter: "blur(8px)",
          borderBottom: "1px solid var(--border)",
        }}
      >
        <div
          style={{
            maxWidth: 1080,
            margin: "0 auto",
            padding: "11px 24px",
            display: "flex",
            alignItems: "center",
            gap: 14,
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <div
              style={{
                width: 30,
                height: 30,
                borderRadius: 8,
                background: "var(--accent)",
                color: "var(--accent-contrast)",
                display: "grid",
                placeItems: "center",
                boxShadow: "var(--shadow-sm)",
              }}
            >
              <Icons.Logo size={18} />
            </div>
            <span style={{ fontSize: 15.5, fontWeight: 600, letterSpacing: "-0.02em" }}>
              RInfra
            </span>
          </div>
          <nav style={{ display: "flex", gap: 4, marginLeft: 8 }} className="lp-nav">
            {[
              ["Features", "#features"],
              ["Architecture", "#architecture"],
              ["Workflow", "#workflow"],
            ].map(([l, h]) => (
              <a
                key={h}
                href={h}
                className="btn ghost sm"
                style={{ textDecoration: "none" }}
              >
                {l}
              </a>
            ))}
          </nav>
          <div style={{ flex: 1 }} />
          <button
            className="btn ghost sm"
            onClick={() => setTheme(dark ? "light" : "dark")}
            aria-label={dark ? "Switch to light theme" : "Switch to dark theme"}
            title={dark ? "Light theme" : "Dark theme"}
            style={{ padding: 8, width: 34, justifyContent: "center" }}
          >
            <Icons.Sliders size={16} />
          </button>
          <a href={REPO} target="_blank" rel="noreferrer" className="btn sm" style={{ textDecoration: "none" }}>
            <Icons.FileText size={15} /> GitHub
          </a>
          <Link href="/infrastructure" style={{ textDecoration: "none" }}>
            <button className="btn primary sm">
              <Icons.Bolt size={15} /> Launch demo
            </button>
          </Link>
        </div>
      </header>

      {/* hero */}
      <div style={{ maxWidth: 1080, margin: "0 auto", padding: "56px 24px 48px" }}>
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "minmax(0, 1.05fr) minmax(0, 1fr)",
            gap: 40,
            alignItems: "center",
          }}
          className="lp-hero"
        >
          <div>
            <span className="pill accent">
              <span className="dot" /> Red-team &amp; purple-team operations
            </span>
            <h1
              style={{
                fontSize: 44,
                lineHeight: 1.05,
                fontWeight: 600,
                letterSpacing: "-0.03em",
                marginTop: 16,
              }}
            >
              Compose attack infrastructure,
              <br />
              <span style={{ color: "var(--accent)" }}>deploy it anywhere</span>, prove coverage.
            </h1>
            <p
              style={{
                fontSize: 16,
                color: "var(--text-2)",
                lineHeight: 1.6,
                marginTop: 18,
                maxWidth: 540,
              }}
            >
              RInfra is an enterprise platform for offensive-security teams: visually
              build redirector / C2 / staging topologies, provision them across four
              clouds into the customer&apos;s own account, front real C2 frameworks, and
              run ATT&amp;CK-mapped emulations — all gated on an authorized engagement
              with a full audit trail.
            </p>
            <div style={{ display: "flex", gap: 10, marginTop: 26, flexWrap: "wrap" }}>
              <Link href="/infrastructure" style={{ textDecoration: "none" }}>
                <button className="btn primary" style={{ height: 40, padding: "0 18px" }}>
                  <Icons.Network size={16} /> Try the live demo
                </button>
              </Link>
              <a href="#architecture" className="btn" style={{ height: 40, padding: "0 16px", textDecoration: "none" }}>
                <Icons.Layers size={16} /> See the architecture
              </a>
            </div>
            <div style={{ display: "flex", gap: 26, marginTop: 28, flexWrap: "wrap" }}>
              {[
                ["4", "cloud providers"],
                ["8", "C2 frameworks"],
                ["100%", "audited actions"],
              ].map(([n, l]) => (
                <div key={l}>
                  <div className="mono" style={{ fontSize: 24, fontWeight: 600, letterSpacing: "-0.02em" }}>
                    {n}
                  </div>
                  <div style={{ fontSize: 12.5, color: "var(--text-3)" }}>{l}</div>
                </div>
              ))}
            </div>
          </div>

          <div className="card" style={{ padding: 16, boxShadow: "var(--shadow-lg)" }}>
            <TopologyArt />
          </div>
        </div>

        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 10,
            marginTop: 36,
            padding: "12px 16px",
            borderRadius: "var(--r-md)",
            background: "var(--surface-2)",
            border: "1px solid var(--border)",
            fontSize: 13,
            color: "var(--text-2)",
            lineHeight: 1.5,
          }}
        >
          <span style={{ color: "var(--accent)", flex: "none" }}>
            <Icons.Shield size={17} />
          </span>
          <span>
            <strong style={{ color: "var(--text)" }}>Legitimate tooling by design.</strong> RInfra
            composes existing, publicly available frameworks — it authors no implants, payloads,
            exploits or evasion. Bring-your-own cloud credentials; nothing runs on RInfra&apos;s tenancy.
          </span>
        </div>
      </div>

      {/* features */}
      <Section id="features" alt>
        <div className="eyebrow">Capabilities</div>
        <h2 style={{ fontSize: 26, fontWeight: 600, letterSpacing: "-0.02em", marginTop: 8, marginBottom: 24 }}>
          One platform, from canvas to coverage
        </h2>
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fill, minmax(300px, 1fr))",
            gap: 14,
          }}
        >
          {CAPABILITIES.map((f) => {
            const Ico = Icons[f.icon] || Icons.Target;
            return (
              <div key={f.title} className="card" style={{ padding: "18px 20px" }}>
                <span
                  style={{
                    width: 38,
                    height: 38,
                    borderRadius: 9,
                    display: "grid",
                    placeItems: "center",
                    background: "var(--accent-soft)",
                    color: "var(--accent)",
                    border: "1px solid var(--accent-soft-border)",
                  }}
                >
                  <Ico size={19} />
                </span>
                <div style={{ fontSize: 15, fontWeight: 600, marginTop: 13 }}>{f.title}</div>
                <div style={{ fontSize: 13, color: "var(--text-3)", lineHeight: 1.55, marginTop: 5 }}>
                  {f.body}
                </div>
              </div>
            );
          })}
        </div>
      </Section>

      {/* architecture */}
      <Section id="architecture">
        <div className="eyebrow">Architecture</div>
        <h2 style={{ fontSize: 26, fontWeight: 600, letterSpacing: "-0.02em", marginTop: 8, marginBottom: 8 }}>
          A thin web console over a Go control plane
        </h2>
        <p style={{ fontSize: 14.5, color: "var(--text-2)", lineHeight: 1.6, maxWidth: 640, marginBottom: 22 }}>
          The console talks to the control plane over REST + SSE. Services enforce the
          authorization gate and emit audit events; an orchestration engine compiles the
          canvas topology into Pulumi stacks per provider, while C2 and emulation adapters
          drive existing frameworks. State and the immutable audit log live in Postgres.
        </p>
        <div className="card" style={{ padding: "26px 22px", overflowX: "auto" }}>
          <div style={{ minWidth: 680 }}>
            <ArchitectureArt />
          </div>
        </div>
        <div style={{ display: "flex", gap: 16, flexWrap: "wrap", marginTop: 18 }}>
          {STACK.map((s) => (
            <span key={s} className="pill" style={{ background: "var(--surface-2)" }}>
              {s}
            </span>
          ))}
        </div>
      </Section>

      {/* workflow */}
      <Section id="workflow" alt>
        <div className="eyebrow">How it works</div>
        <h2 style={{ fontSize: 26, fontWeight: 600, letterSpacing: "-0.02em", marginTop: 8, marginBottom: 24 }}>
          The operator flow, end to end
        </h2>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))", gap: 14 }}>
          {STEPS.map((s, i) => (
            <div key={s.title} className="card" style={{ padding: "16px 18px", position: "relative" }}>
              <div
                className="mono"
                style={{
                  width: 28,
                  height: 28,
                  borderRadius: 99,
                  display: "grid",
                  placeItems: "center",
                  background: "var(--accent)",
                  color: "var(--accent-contrast)",
                  fontSize: 13,
                  fontWeight: 600,
                }}
              >
                {i + 1}
              </div>
              <div style={{ fontSize: 14, fontWeight: 600, marginTop: 12 }}>{s.title}</div>
              <div style={{ fontSize: 12.5, color: "var(--text-3)", lineHeight: 1.5, marginTop: 4 }}>
                {s.body}
              </div>
            </div>
          ))}
        </div>
      </Section>

      {/* screens */}
      <Section id="screens">
        <div className="eyebrow">Explore the demo</div>
        <h2 style={{ fontSize: 26, fontWeight: 600, letterSpacing: "-0.02em", marginTop: 8, marginBottom: 6 }}>
          Five screens, fully interactive
        </h2>
        <p style={{ fontSize: 14, color: "var(--text-3)", marginBottom: 22 }}>
          The demo runs entirely in your browser on mock data — nothing is provisioned.
        </p>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(300px, 1fr))", gap: 12 }}>
          {SCREENS.map((s) => {
            const Ico = Icons[s.icon] || Icons.Target;
            return (
              <Link key={s.href} href={s.href} style={{ textDecoration: "none" }}>
                <div className="card menu-item" style={{ padding: "16px 18px", height: "100%", cursor: "pointer" }}>
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
                    <span style={{ flex: 1 }} />
                    <span style={{ color: "var(--text-4)" }}>
                      <Icons.ArrowRight size={16} />
                    </span>
                  </div>
                  <div style={{ fontSize: 12.5, color: "var(--text-3)", lineHeight: 1.5 }}>{s.desc}</div>
                </div>
              </Link>
            );
          })}
        </div>
      </Section>

      {/* CTA */}
      <Section alt>
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            gap: 24,
            flexWrap: "wrap",
            padding: "8px 4px",
          }}
        >
          <div>
            <h2 style={{ fontSize: 24, fontWeight: 600, letterSpacing: "-0.02em" }}>
              See it in action
            </h2>
            <p style={{ fontSize: 14, color: "var(--text-3)", marginTop: 6, maxWidth: 520, lineHeight: 1.5 }}>
              Launch the console and walk the full flow — build a topology, deploy it,
              run an emulation, and read the coverage report.
            </p>
          </div>
          <div style={{ display: "flex", gap: 10 }}>
            <Link href="/infrastructure" style={{ textDecoration: "none" }}>
              <button className="btn primary" style={{ height: 40, padding: "0 18px" }}>
                <Icons.Bolt size={16} /> Launch the demo
              </button>
            </Link>
            <a href={REPO} target="_blank" rel="noreferrer" className="btn" style={{ height: 40, padding: "0 16px", textDecoration: "none" }}>
              <Icons.FileText size={16} /> Read the source
            </a>
          </div>
        </div>
      </Section>

      {/* footer */}
      <footer style={{ borderTop: "1px solid var(--border)", background: "var(--bg)" }}>
        <div
          style={{
            maxWidth: 1080,
            margin: "0 auto",
            padding: "24px",
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            gap: 16,
            flexWrap: "wrap",
            fontSize: 12.5,
            color: "var(--text-3)",
          }}
        >
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
