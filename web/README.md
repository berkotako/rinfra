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

## Deploy to Vercel

This app deploys to Vercel out of the box (it's a mock-data demo — no backend).

In the Vercel project settings:

- **Root Directory**: `web` (the app is not at the repo root — this is the one
  setting that's easy to miss).
- **Framework Preset**: Next.js (auto-detected).
- Leave **`NEXT_PUBLIC_BASE_PATH` unset** — that's only for the GitHub Pages
  sub-path; on Vercel the app serves from the domain root.

By default the build is a static export (`output: "export"`), which Vercel serves
as a static site — matching the build that ships to GitHub Pages. To use full
Next.js features (SSR / API routes) on Vercel instead, set the env var
`NEXT_PUBLIC_SSR=true`. Security headers are applied via `web/vercel.json`.


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

## API seam — REST vs mock mode

`lib/client.ts` exports `RInfraClient` with two implementations:

| Mode | When active | Description |
|------|-------------|-------------|
| **MockClient** | `NEXT_PUBLIC_RINFRA_API` **unset** | In-memory demo; no Go server needed. Static export default. |
| **RestClient** | `NEXT_PUBLIC_RINFRA_API=http://…` | Fetch-based; talks to the Go control plane. SSE-driven node/job/run updates. |

### Running against the Go server

```bash
# Copy and edit the env file
cp web/.env.example web/.env.local
# NEXT_PUBLIC_RINFRA_API=http://localhost:8080  (already set in example)

# Start both together (Go server in background + Next.js dev):
make dev

# Or in two separate terminals:
make dev-server   # terminal 1 — RINFRA_DEV=1 Go server on :8080
make dev-web      # terminal 2 — Next.js on :3000
```

`RINFRA_DEV=1` starts the Go server with in-memory stores and a fake cloud
provider — no Postgres or cloud credentials needed for local development.

### SSE events → store updates

| SSE event kind | Payload fields | Store effect |
|---|---|---|
| `node_status` | `nodeId`, `status`, `publicIp` | Updates node status/ip/health in topology |
| `job_status` | `jobId`, `status` | Shows toast on `done`/`failed` |
| `run_status` | `runId`, `techniqueId?`, `status` | Updates technique step in EmulationScreen |

### API error codes → toasts

`ApiError.toastMessage()` maps the following codes to user-facing messages:

- `authorization_required` — engagement must be authorized before deploying
- `auth_expired` — re-authorize the engagement
- `outside_window` — outside the RoE window
- `empty_scope` — define targets before deploying
- `job_running` — a deploy or teardown is already in progress
- `not_found` — resource not found

## Preferences

User preferences (theme, accent, node style) persist to `localStorage` under
the key `rinfra-prefs` and are applied via `data-theme` attribute and
`--accent-h` CSS variable on `<html>`.

## Notes

- This is a self-contained workspace inside the rinfra Go repo (`web/`).
  Moving it to a separate repo later is a `git mv`.
- Static export can later be served from `cmd/rinfra-server` via `go:embed`.
- The Settings screen and ATT&CK Navigator export are implemented. Still
  deferred per CLAUDE.md: the SIEM/EDR detection-validation phase and PDF export
  of the engagement report (PDF button is a stub).
