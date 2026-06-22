# RInfra Web UI — Implementation Plan

Source of truth: the Claude Design handoff bundle (extracted at
`/tmp/design/rinfra/` in the build container). The prototype is a fully
clickable 5-screen React app; this plan turns it into a production frontend.
The design files spell out every dimension, color, and interaction — when in
doubt, read them directly:

| Design file | What it specifies |
|---|---|
| `project/styles.css` | Full design-token system (light + soft-dark), primitives (`.btn`, `.pill`, `.card`, `.seg`, `.toggle`, `.input`, `.nav-item`…) |
| `project/icons.jsx` | Custom Lucide-style icon set (stroke 1.75, 24×24) |
| `project/data.js` | Mock data: providers, regions, C2 list, engagements, scenarios, topology |
| `project/shared.jsx` | Shared atoms: ProviderBadge, StatusPill, NodeGlyph, Avatar, HealthMeter, Modal, EmptyState, PageHead, TierBadge |
| `project/app.jsx` | App shell: sidebar, topbar (engagement switcher, cloud filter, user menu), toasts, theme/accent wiring |
| `project/screen_builder.jsx` + `screen_builder_parts.jsx` | **Hero**: Infrastructure Builder — canvas, palette drag, port-drag edges, inspector, toolbar (Validate / Deploy / Tear down), node card styles |
| `project/screen_dashboard.jsx` | Engagement dashboard: stats, filter seg, search, table |
| `project/screen_new_engagement.jsx` | 4-step New Engagement modal (RoE / scope / authorization gate) |
| `project/screen_c2.jsx` | C2 framework selector (page + modal variant), tier badges, license-key field |
| `project/screen_emulation.jsx` | Emulation runner: scenario picker, live technique timeline, progress ring |
| `project/screen_reporting.jsx` | ATT&CK coverage matrix + engagement report |

`project/tweaks-panel.jsx` is a **design-tool artifact** (host protocol for the
Claude Design editor) — do **not** port it. Its three tweaks (soft-dark theme,
accent hue, node-card style) become real user preferences (see §6).

## 1. Decisions

- **Location: `web/` in this repo.** CLAUDE.md describes the frontend as a
  separate repo, but no frontend repo exists in this session's scope, so it
  lives here as a self-contained workspace. Moving it out later is a `git mv`.
- **Stack: Next.js (App Router, TypeScript, `output: "export"`)** — per
  CLAUDE.md architecture. The exported static site can later be embedded into
  `cmd/rinfra-server` via `go:embed` (seam only, not now).
- **Builder canvas: custom (ported from the prototype), not React Flow.**
  CLAUDE.md names React Flow, but the prototype implements its own
  pointer-driven canvas (absolute-positioned nodes, SVG bezier edges, port
  drag-to-connect) and porting it directly preserves the exact dimensions and
  interactions. Swapping to `@xyflow/react` remains an isolated refactor of
  `components/builder/` if needed.
- **Icons:** port `icons.jsx` directly to a typed `components/icons.tsx`
  (pixel-identical, zero deps). Do not substitute lucide-react.
- **Fonts:** `geist` npm package (GeistSans + GeistMono), wired in the root
  layout. Mono is reserved for IPs, resource IDs, costs, dates, ATT&CK tags.
- **Data: mock adapter behind an API-client seam.** The Go control plane has
  no endpoints yet (only `/healthz`). All screens render from a typed mock
  store; a `RInfraClient` interface is the seam the REST client will implement
  later. No fetch calls now.
- **Recreate the visuals pixel-perfectly; do not copy prototype internals.**
  Inline-style JSX in the prototype becomes proper components + CSS classes
  where the prototype already defines them (`styles.css` ports ~verbatim to
  `globals.css`).

## 2. Naming alignment with `internal/domain`

The prototype invented names; the frontend must use the Go domain vocabulary
so the API integration is mechanical. UI labels stay as designed.

| Prototype | Frontend (matches Go) | UI label |
|---|---|---|
| node type `c2` | `c2_server` | "C2 Server" |
| node type `payload` | `payload_host` | "Staging" |
| node status `draft` | `pending` | "Draft" |
| provider `do` | `digitalocean` (short "DO") | "DigitalOcean" |
| C2 tier `manual` | `fronted` | "Deploy & operate manually" |
| — | node status `failed` (exists in domain) | "Failed" (danger pill, support in StatusPill) |

Node statuses: `pending → provisioning → live → draining → destroyed`, plus
`failed`. Engagement lifecycle: `draft / authorized / active / completed /
archived` (`domain.EngagementStatus`); the dashboard's "Provisioning" filter is
a *derived infra* state, not an engagement status — model it as a computed
`infraState` on the engagement view model.

**C2 catalog = the 8 frameworks registered in `internal/c2`**, not the
prototype's 5. Tiers per `docs/SUPPORT_MATRIX.md`:
- Orchestrated (pill `ok`, Bolt icon): Sliver, Mythic, Metasploit, In-house/Custom
- Scripted (pill `info`, Terminal icon): Havoc, PoshC2
- Fronted (neutral pill, Power icon): Cobalt Strike, Brute Ratel C4 — both
  `gated: true` (license-key field, "Customer-provided. Stored encrypted,
  scoped to this engagement only.")

Keep the prototype's copy/notes for the 5 it defines; write matching-tone
notes for Metasploit, PoshC2, Brute Ratel.

## 3. Project structure

```
web/
  package.json            # next, react, react-dom, geist; dev: typescript, @types/*, eslint, eslint-config-next
  next.config.ts          # output: "export"
  tsconfig.json
  app/
    layout.tsx            # fonts, ThemeProvider, AppShell (sidebar+topbar), Toasts
    page.tsx              # redirect → /infrastructure
    engagements/page.tsx
    infrastructure/page.tsx
    c2/page.tsx
    emulation/page.tsx
    reporting/page.tsx
    globals.css           # port of styles.css
  components/
    icons.tsx             # ported icon set
    ui/                   # Button, Pill, StatusPill, ProviderBadge, NodeGlyph,
                          # Avatar, HealthMeter, Modal, EmptyState, PageHead,
                          # TierBadge, Seg, Toggle, Field/Input/Select, Dropdown, Toasts
    shell/                # Sidebar, TopBar, AppearanceMenu
    builder/              # Canvas (custom, ported), NodeCard (3 styles), Palette,
                          # Inspector, Toolbar, ValidationPopover, TeardownConfirm
    engagements/          # StatsRow, EngagementTable, NewEngagementFlow
    c2/                   # C2List, C2DetailRail, C2SelectorModal
    emulation/            # ScenarioPicker, TechniqueTimeline, RunControl
    reporting/            # CoverageMatrix, EngagementReport
  lib/
    types.ts              # Engagement, Node, Edge, C2Framework, Scenario… (domain-aligned)
    data.ts               # mock data (port of data.js, renamed per §2, 8 C2s)
    client.ts             # RInfraClient interface + MockClient impl
    store.tsx             # React context: engagements, activeEngagementId,
                          # topology (nodes/edges), preferences, toasts
```

State: plain React context + `useState` (the prototype's model) — no state
library. Preferences (`theme`, `accentHue`, `nodeStyle`) persist to
`localStorage`; apply via `data-theme` attribute + `--accent-h` CSS var on
`<html>` exactly as the prototype does.

## 4. Design system port

- `globals.css` = `styles.css` ported intact: both token blocks (`:root` and
  `[data-theme="dark"]`), reset, primitives, scrollbars, keyframes
  (`pulse`, `fadeUp`, `fadeIn`, `spin`), `.nav-item`, `.eng-row`, `.menu-item`.
  Keep the oklch values, shadows, radii, and the `--accent-h` indirection
  exactly — the accent tweak works by setting that one variable.
- Accent options (Appearance menu): Indigo 262, Slate blue 245, Periwinkle 278,
  Steel 222 (default Indigo; note `:root` default is 258 — set 262 on load).

## 5. App shell

Per `app.jsx`: 226px sidebar (brand block, "Workspace" nav: Engagements /
Infrastructure / C2 Frameworks / Emulation / Coverage & Reports; footer:
Settings stub + "All systems operational / Audit logging active" chip), 56px
topbar (engagement context switcher dropdown listing active+provisioning
engagements, screen title, cloud-provider filter dropdown, activity icon
button, user dropdown — Rina Okafor, "Lead operator · Acme Offensive").
Active nav state from the current route (`usePathname`). Builder route uses
`--canvas-bg` for the content background; others `--bg`.

Toasts: bottom-center, fixed, 3.2s auto-dismiss, kinds ok/warn/info/danger
(see `Toasts` in app.jsx).

**Appearance menu** (replaces the Tweaks panel): a dropdown in the topbar (or
under the user menu) with the soft-dark toggle, the 4 accent swatches, and the
node-card style radio (soft / compact / outline).

## 6. Screens

### 6.1 Infrastructure Builder (hero — most effort)
Three-panel layout: 224px palette | canvas+toolbar | 264/300px inspector.

- **Palette:** grouped templates (Redirectors: HTTPS/HTTP/DNS; Command &
  control: C2 Server; Staging: Staging Host) + "Browse C2 frameworks" button
  (opens C2 modal). Drag onto canvas creates a `pending` node centered on the
  drop point with generated name (`edge-https-02`, `teamserver-03`,
  `stage-host-02` — zero-padded counter), defaults aws/us-east-1, toast
  "<label> added — configure it in the inspector".
- **Canvas:** custom (ported). The node component renders the three NodeCard styles
  (dims: soft 216×110, compact 200×60, outline 212×96 — see
  `screen_builder_parts.jsx` for the exact layouts). Selection ring =
  accent border + `0 0 0 3px var(--accent-soft)`. Custom bezier edge:
  inactive = `--border-strong` 1.6px, arrowhead marker; live (both ends live)
  = accent 2px plus an animated dashed overlay (`stroke-dasharray 2 8`,
  dashoffset animation, 1.1s loop). Connect by dragging from the right
  (source) handle; duplicate edges rejected; toast "Edge created — traffic
  flow added". Pan on background drag; initial viewport offset (40, 20).
  Empty state + persistent hint chip bottom-left.
- **Toolbar:** "Topology" + engagement codename pill; ASSETS `live/total` and
  BURN RATE `$x.xx/hr` counters (mono); **Validate** → popover with checks
  (assets > 0; ≥1 C2 server; redirector present; every C2 has an inbound
  redirector — else "N C2 server(s) directly exposed" warning; engagement
  authorization on file); **Tear down** (danger) → confirm modal ("drain and
  destroy… logged to the engagement audit trail and cannot be undone") →
  all live/provisioning nodes → `draining` → staggered `destroyed`;
  **Deploy** (primary) → all `pending` → `provisioning` → staggered `live`
  with randomized health 92–98 and assigned IPs; toasts at start/end of both.
- **Inspector:** empty state when nothing selected. Selected: status pill +
  health meter, name input, provider 2×2 button grid, region select (per
  provider — region lists in `data.js`), C2-only: framework select + listener
  protocol seg (listeners per framework); redirector: front-domain input
  ("Categorized domain used to mask C2 traffic."); payload host: delivery
  domain; identifiers block (resource ID, public IP, est. cost — mono);
  "Remove node" danger button (also removes touching edges).

### 6.2 Engagement Dashboard + New Engagement
Dashboard per `screen_dashboard.jsx`: PageHead + Export/New buttons; 4 stat
cards (active engagements, live assets, combined burn rate, awaiting
authorization); filter seg with counts + search input; table (grid
`1.7fr 1.5fr 1fr 1.1fr 0.9fr 40px`) with client/codename, scope+first target,
infra status pill + `live/assets`, authorization (icon + by-line), window
dates (mono), chevron. Row click sets active engagement and, if
active/provisioning, navigates to the builder.

New Engagement: 4-step modal (Engagement → Rules of engagement → Scope →
Authorization) per `screen_new_engagement.jsx`, including step progress bars,
RoE toggle cards, targets/exclusions textareas, the amber "No infrastructure
can be provisioned until a named authorizing party is recorded" callout, and
the consent checkbox gating creation. Creating appends the engagement
(status `draft`, auth from consent), activates it, toasts.

### 6.3 C2 Frameworks
Full page and modal variants share one component (`asModal` prop). Selectable
framework cards (tier badge, "License required" pill when gated, note,
author/lang line, listener chips, radio indicator) + sticky 280px detail rail
(orchestration tier, listeners, automated-emulation level, license-key input
for gated frameworks, primary action "Assign to node" / "Set as default").

### 6.4 Emulation Runner
Per `screen_emulation.jsx`: 3 scenario cards (APT29, FIN7, Ransomware
Affiliate — techniques in `data.js`); technique timeline with rail
(Queued/Running spinner/Executed/Detected ~28% chance; 1.5s cadence,
timers cleaned up on unmount/change); sticky run-control card with SVG
progress ring (r=26, stroke 6) + executed/detected counts + Run/Stop;
target-infrastructure card listing live C2/redirector/staging nodes from the
**actual topology state** (improvement over the prototype's hardcoded list).

### 6.5 Coverage & Reporting
Per `screen_reporting.jsx`: seg tab (Coverage matrix / Engagement report) +
"Export PDF" stub button. Coverage: 4 stat cards; 8 tactic columns of
technique chips colored by level 0–3 (`lvlColor` greens), hover lift, legend.
Report: document card (confidential eyebrow, exec summary, 2×2 finding stats,
key findings list with severity pills) + sticky metadata rail + action stubs.

## 7. Build order (each step compiles & lints)

1. Scaffold `web/` (package.json, tsconfig, next.config, root layout, empty
   routes), `globals.css` token port, fonts. `npm run build` green.
2. `lib/` (types, data, client, store) + `components/icons.tsx` + `ui/` atoms.
3. App shell (sidebar, topbar, dropdowns, toasts, appearance preferences).
4. Dashboard + New Engagement flow.
5. C2 selector (page + modal).
6. Infrastructure Builder (custom canvas, node cards, palette DnD,
   inspector, toolbar actions).
7. Emulation Runner.
8. Coverage & Reporting.
9. Polish pass against the prototype: spacing, dark theme, node-style
   variants, edge animation. `npm run build && npm run lint` clean.

Also: `web/README.md` (run/build instructions, mock-data note, API seam),
root `Makefile` targets `web-dev`, `web-build`, `web-lint`; `.gitignore` for
`node_modules/`, `.next/`, `out/`.

## 8. Status / out of scope

Delivered since this plan was written: real REST wiring (`RestClient` + SSE,
with `MockClient` as the offline fallback), auth/login, the Settings screen, the
audit-log feed, and ATT&CK Navigator export. The top-bar engagement selector is
grouped by project, and the Emulation screen supports both project- and
engagement-scope runs.

Still out of scope (genuine seams only):
- Serving the static export from `cmd/rinfra-server` (`go:embed` later).
- The **SIEM/EDR detection-validation** phase (deferred in CLAUDE.md): live
  detection reconciliation, coverage heatmaps, detection-as-code export.
- PDF export of the engagement report (button is a stub).
