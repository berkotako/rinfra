# RInfra

Enterprise red-team & purple-team operations platform. Visually compose attack
infrastructure across AWS, GCP, Azure, and DigitalOcean; deploy and front C2
frameworks (Sliver, Mythic, Havoc, Cobalt Strike, in-house); and run ATT&CK-mapped
adversary-emulation scenarios — all bound to authorized engagements with a full
audit trail.

> **Scope & posture.** RInfra *composes* existing, publicly available offensive
> tooling. It does not implement implants, payloads, exploits, or evasion. Every
> deploy is gated on an authorized engagement, provisions into the customer's own
> cloud account, and is fully audited. See `CLAUDE.md` for the invariants.

## Live demo

A fully interactive demo of the web console is published to GitHub Pages:

**→ https://berkotako.github.io/rinfra/**

It runs entirely in the browser on mock data (no backend, nothing provisioned),
so you can click through all five screens and the deploy / emulation flows. The
landing page includes a short "how to use" walkthrough. To run it locally
instead, see `web/README.md` (`make web-dev`).

> **One-time setup.** The demo deploys via the `Deploy demo to GitHub Pages`
> workflow, but GitHub Pages must be enabled by hand first:
> **Settings → Pages → Build and deployment → Source: "GitHub Actions"**.
> (The workflow token cannot enable Pages itself.) Note that **Pages on a
> private repo requires a paid GitHub plan** — on a free plan, make the repo
> public to use Pages. Once enabled, the demo publishes on every push to `main`,
> or on demand via the workflow's **Run workflow** button (pick this branch).

## Layout

| Path | Purpose |
|------|---------|
| `cmd/rinfra-server` | Control-plane entrypoint |
| `internal/domain` | Core types (engagements, infrastructure, emulation) |
| `internal/cloud` | `CloudProvider` interface + per-provider adapters |
| `internal/c2` | `C2Provider`/`Operator` interfaces + per-framework adapters |
| `internal/emulation` | Scenario orchestrator |
| `internal/audit` | Append-only audit log |
| `internal/store` | Persistence interfaces (Postgres) |
| `migrations` | Database schema |
| `docs` | Architecture & support matrix |

## Getting started

### Docker (full stack — recommended)

A single script brings up the whole stack (Postgres + migrations + Go control
plane + web console) in Docker. It is safe to re-run on **every update** — it
rebuilds from the current checkout, re-applies migrations, and reuses the
secrets it generated the first time:

```bash
scripts/install.sh            # build + start everything
scripts/install.sh --pull     # update to latest, then rebuild + restart
scripts/install.sh --down     # stop the stack
scripts/install.sh --fresh    # wipe the Postgres volume (DESTRUCTIVE)
```

Then open:

| Service | URL |
|---------|-----|
| Web console | http://localhost:3000 |
| Control plane | http://localhost:8080 (`GET /healthz`) |

The console requires sign-in. **Default credentials on a fresh install are
`admin` / `admin`** — change them under **Settings → Account**. Cloud provider
keys (AWS / GCP / Azure / DigitalOcean) are added under **Settings → Cloud
credentials**; they are stored encrypted and bound to a single engagement
(bring-your-own cloud, never RInfra's tenancy).

The install script generates `RINFRA_MASTER_KEY` into a local `.env` (see
`.env.example`). Live cloud provisioning additionally needs the Pulumi CLI;
see `cmd/rinfra-server` docs and `docs/RUNBOOK_DO.md`.

## Authentication, roles & projects

The control plane authenticates operators with bearer-token sessions and three
roles. On first boot, if no users exist, a default **`admin` / `admin`** account
is seeded (change it immediately).

| Role | Capabilities |
|------|--------------|
| `admin` | Everything: manage all users, projects, and engagements. |
| `lead` | Owns operators and projects; creates operator accounts, creates/manages their own projects, and binds operators to them. |
| `operator` | Works within the projects they are assigned to. |

A **project** groups one or more **engagements** (which carry the infrastructure
and emulations). Access flows from role + project membership: admins see all;
leads see the projects they lead; operators see the projects they're a member of.

Key endpoints (all under `/api/v1`, bearer token required except `auth/login`):
`POST auth/login` · `POST auth/logout` · `GET auth/me` · `users` (CRUD) ·
`projects` (CRUD) + `projects/{id}/members` + `projects/{id}/engagements`.
Auth is enforced when the server wires the auth subsystem; the test suite runs
it disabled (legacy operator-header identity) to stay hermetic.

### Local (Go only)

```bash
go build ./...
go vet ./...
go run ./cmd/rinfra-server   # serves :8080, logs registered C2 tiers
```

The frontend (Next.js + React Flow drag-and-drop canvas) lives under `web/` and
talks to this control plane over REST/JSON (`make web-dev`, or `make dev`).

## Build order

See `CLAUDE.md` → "Build order". Summary: domain+store → audit → cloud
(DigitalOcean first) → orchestration → C2 (Sliver/Mythic first) → emulation.
Validation/reporting is deferred.
