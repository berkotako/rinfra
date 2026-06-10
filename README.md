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

> The demo deploys via the `Deploy demo to GitHub Pages` workflow. To enable it
> once, set **Settings → Pages → Source: GitHub Actions**; it then publishes on
> every push to `main` (or on demand via the workflow's "Run workflow" button).

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

```bash
go build ./...
go vet ./...
go run ./cmd/rinfra-server   # serves :8080, logs registered C2 tiers
```

The frontend (Next.js + React Flow drag-and-drop canvas) lives in a separate
repo and talks to this control plane over REST/JSON.

## Build order

See `CLAUDE.md` → "Build order". Summary: domain+store → audit → cloud
(DigitalOcean first) → orchestration → C2 (Sliver/Mythic first) → emulation.
Validation/reporting is deferred.
