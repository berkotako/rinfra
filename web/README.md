# RInfra Web Frontend

Static Next.js (App Router, TypeScript) frontend for the RInfra operations platform.

## Stack

- **Next.js 15** (App Router, `output: "export"` for static export)
- **React 19**
- **@xyflow/react** — Infrastructure Builder canvas
- **geist** — GeistSans + GeistMono fonts

## Dev

```bash
cd web
npm install
npm run dev      # http://localhost:3000
```

## Build

```bash
npm run build    # produces static export in out/
npm run lint     # ESLint
npx tsc --noEmit # TypeScript check
```

Or from the repo root:

```bash
make web-dev
make web-build
make web-lint
```

## Architecture

```
app/
  layout.tsx          # Root layout: fonts, StoreProvider, AppShell
  AppShell.tsx        # Client shell: Sidebar, TopBar, appearance, toasts
  page.tsx            # Redirects / → /infrastructure
  engagements/        # Engagement dashboard + New Engagement flow
  infrastructure/     # Infrastructure Builder (canvas)
  c2/                 # C2 Framework selector
  emulation/          # Emulation Runner (ATT&CK timeline)
  reporting/          # Coverage matrix + engagement report

components/
  icons.tsx           # Custom Lucide-style icon set (no external icon lib)
  ui/                 # Design system atoms: Pill, Button, Modal, etc.
  shell/              # Sidebar, TopBar, AppearanceMenu
  builder/            # Canvas, NodeCard (3 styles), Inspector, Toolbar
  engagements/        # Dashboard table + 4-step New Engagement modal
  c2/                 # C2Selector (page + modal variants)
  emulation/          # Timeline, progress ring, run control
  reporting/          # ATT&CK matrix + engagement report

lib/
  types.ts            # Domain-aligned TypeScript types
  data.ts             # Mock data (8 C2 frameworks, 6 engagements, 3 scenarios)
  client.ts           # RInfraClient interface + MockClient (API seam)
  store.tsx           # React context: engagements, topology, preferences, toasts
```

## API seam

All data comes from `lib/client.ts → MockClient`. When the Go control plane
gains REST endpoints, implement `RInfraClient` against them and swap
`MockClient` for the real client. No changes to screens required.

## Preferences

User preferences (theme, accent, node style) persist to `localStorage` under
the key `rinfra-prefs` and are applied via `data-theme` attribute and
`--accent-h` CSS variable on `<html>`.

## Notes

- This is a self-contained workspace inside the rinfra Go repo (`web/`).
  Moving it to a separate repo later is a `git mv`.
- Static export can later be served from `cmd/rinfra-server` via `go:embed`.
- Settings screen, SIEM/EDR validation, PDF export, and ATT&CK Navigator export
  are stub buttons only (deferred per CLAUDE.md).
