# RInfra — Claude Code context

This file is the canonical brief for working in this repo. Read it fully before
writing code. Keep it updated as the architecture evolves.

## What RInfra is

RInfra is an **enterprise red-team and purple-team operations platform** for
professional offensive-security consultancies and internal security teams. It
lets operators:

1. **Visually compose attack infrastructure** (redirectors, C2 servers, payload
   hosts) on a drag-and-drop canvas, and provision/tear it down across **four
   cloud providers** (AWS, GCP, Azure, DigitalOcean).
2. **Deploy and front existing C2 frameworks** (Sliver, Mythic, Havoc, Cobalt
   Strike, and an in-house/custom framework) behind that infrastructure.
3. **Run ATT&CK-mapped adversary-emulation scenarios** through the deployed
   infrastructure, on frameworks that expose a usable operator API.

A later phase adds detection **validation** (reconciling what fired against the
customer's SIEM/EDR) and coverage reporting. That phase is **out of scope for
the current build** — design seams for it, do not implement it yet.

## Non-negotiable constraints

These are product requirements *and* the reason this tool is legitimate. Treat
them as invariants; never weaken them to make a feature easier.

- **Authorization gates every deploy.** No infrastructure may be provisioned for
  an engagement that is not in an authorized state, within its allowed time
  window, with a defined scope. Enforced in code via
  `domain.Engagement.CanDeploy()` — call it before any provisioning path.
- **Bring-your-own cloud credentials.** RInfra never hosts attacker
  infrastructure on its own tenancy. Each engagement provisions into the
  *customer's* cloud account using credentials they supply. No shared/default
  cloud account, ever.
- **Everything is audited.** Every privileged action (deploy, teardown, scenario
  run, credential use, scope change) emits an append-only `audit.Event`. The
  audit log is immutable — no update/delete paths.
- **Teardown must be reliable.** Orphaned infra = cost, exposure, and ToS risk.
  Provisioning is transactional where possible; every engagement supports a
  guaranteed teardown that reconciles actual cloud state, not just our DB.

## Architecture

```
Frontend (separate repo: Next.js + React Flow)   <-- designed in Claude Design
        |  REST/JSON
Control plane (this repo, Go)
  - HTTP API + engagement/RoE/authorization service
  - Orchestration engine (compiles canvas topology -> IaC via Pulumi Go SDK)
  - Cloud abstraction   (internal/cloud)   one impl per provider
  - C2 abstraction      (internal/c2)      one impl per framework
  - Emulation engine    (internal/emulation)
  - Audit log           (internal/audit)
  - Persistence         (internal/store -> Postgres)
```

### Two interfaces are the spine

**`cloud.CloudProvider`** (`internal/cloud/provider.go`) — uniform provisioning
across clouds. Compute abstracts cleanly; **networking does not** —
`ConfigureIngress` and DNS are where AWS security groups, GCP VPC firewall rules,
DO cloud firewalls, and Azure NSGs genuinely diverge. Implement those per
provider deliberately; a wrong ingress rule is a dead engagement.

**`c2.C2Provider`** (`internal/c2/provider.go`) — uniform deploy + fronting, but
**control is tiered**. `Control()` returns `(Operator, ok bool)`; `ok=false`
means the framework is deploy-and-operate-manually. See `docs/SUPPORT_MATRIX.md`.

| Tier | Meaning | Frameworks |
|------|---------|------------|
| Orchestrated | Deploy + redirector + automated emulation via `Operator` | Sliver, Mythic, Metasploit, custom |
| Scripted | Deploy + redirector + partial automation | Havoc, PoshC2 |
| Fronted | Deploy + redirector only; human operates | Cobalt Strike, Brute Ratel C4 |

Emulation automation only lights up on Orchestrated/Scripted tiers. Cobalt
Strike and Brute Ratel are provisioned and fronted; the operator drives them by
hand. Metasploit, Sliver, Mythic, PoshC2, and Havoc are open source (no license
key); Cobalt Strike and Brute Ratel are license-gated (customer key per
engagement, never bundled).

### Payload generators (separate from C2)

Initial-access stager tools (msfvenom) are NOT C2 frameworks — they have no
teamserver or sessions — so they implement a separate `payload.Generator`
interface (`internal/payload`). A generator pairs with a C2 (msfvenom →
Metasploit) and produces a stager that calls back to the deployed listener.
Same compose-the-upstream-binary posture: the implementation shells out to the
operator's installed tool and authors no payload bytes/encoders/evasion.
Generation is engagement-bound and audited, gated by `CanDeploy()`.

### Portable technique format (decided)

Scenarios are authored once in a **portable internal format** (`domain.Technique`)
and each `Operator.Execute` adapter translates a technique down to its
framework's native primitives. This keeps scenarios reusable across C2s — the
point, for a consultancy that switches frameworks per engagement. `Technique`
references an Atomic Red Team / Caldera ability; it is not a payload.

## Tech stack & conventions

- **Go 1.24+**. Standard project layout (`cmd/`, `internal/`).
- **IaC:** Pulumi **Go SDK** (automation API) is the default backend —
  programmatic, no HCL context switch. A **Terraform** backend
  (`internal/orchestration/terraform`) is also selectable (set `RINFRA_IAC` or
  choose it in Settings → Infrastructure; persisted in `server_settings`). Both
  satisfy `service.Provisioner`; each `CloudProvider` impl supplies both an
  `orchestration.ProgramBuilder` (Pulumi) and a `terraform.Builder` (Terraform JSON).
- **DB:** Postgres. Use `pgx` + `sqlc` (or plain `pgx`); migrations in
  `migrations/` (golang-migrate format).
- **HTTP:** stdlib `net/http` + `chi` router is fine. Keep handlers thin;
  business logic in services.
- **Logging:** stdlib `log/slog`, structured, context-aware.
- **Errors:** wrap with `fmt.Errorf("...: %w", err)`. No panics in library code.
- **Context:** every I/O method takes `context.Context` first.
- **Tests:** table-driven; mock the cloud/C2 interfaces. Provisioning code is
  tested against fakes, never live cloud, in CI.
- Run `gofmt`/`go vet` clean. Keep packages small and dependency-light.

## Build order (current MVP)

Implement in this sequence; each step should compile and be testable.

1. **Domain + store** — `internal/domain`, `internal/store`, `migrations`.
   Engagement/RoE/authorization with `CanDeploy()` enforced. (Foundations.)
2. **Audit** — append-only logger wired into every privileged action.
3. **Cloud abstraction** — `CloudProvider` interface + **DigitalOcean first**
   (most permissive, cheapest to iterate), then AWS, GCP, Azure. Get
   `ConfigureIngress`/DNS right per provider.
4. **Orchestration engine** — canvas topology -> Pulumi program -> provision,
   with transactional teardown + state reconciliation.
5. **C2 abstraction** — `C2Provider` + **Sliver and Mythic (Orchestrated)**
   first, then a Havoc (Scripted) and Cobalt Strike (Fronted) deploy path.
6. **Emulation engine** — run `domain.Scenario` via the `Operator` interface;
   wrap Atomic Red Team / Caldera abilities as the procedure source.

**Status: steps 1–6 are delivered.** All four cloud providers' standalone
methods are wired to their official SDKs (godo / aws-sdk-go-v2 /
google.golang.org/api / azure-sdk-for-go) and unit-tested; all eight C2
frameworks have real clients (Sliver gRPC, Mythic GraphQL, Metasploit
msgpack-RPC, Havoc/PoshC2 SSH-CLI, Custom REST, Cobalt Strike / Brute Ratel
fronted) over a shared live SSH runner; msfvenom generation is real. Built on
top since: capability-based technique routing (`service.Route` —
technique→capable in-scope agent), an honest BAS status taxonomy
(executed/attempted-failed/manual/unsupported/blocked-by-scope/not-run, so
coverage/TRM count only real attempts), a hardened engagement approval flow
(admin/lead role gate + authorization validation + status-transition guards),
and project- **and** engagement-scope emulation runs. Live validation against
real cloud accounts / C2 teamservers is the remaining per-target work (see
`docs/RUNBOOK_DO.md`, `docs/PROJECT_PLAN.md`).

Explicitly **deferred** (design seams only, do not build): SIEM/EDR
reconciliation, coverage heatmaps, detection-as-code export, QRadar and other
SIEM connectors.

## Redirector note

Classic domain fronting is effectively dead on the major CDNs. The redirector
layer assumes reverse-proxy + categorized-domain patterns, not fronting tricks.

## Repo map

- `cmd/rinfra-server` — entrypoint, wiring, HTTP server.
- `internal/domain` — core types: engagements, infrastructure, emulation.
- `internal/cloud` — `CloudProvider` interface + per-provider impls + registry.
- `internal/c2` — `C2Provider`/`Operator` interfaces + per-framework impls + registry.
- `internal/payload` — `Generator` interface (msfvenom) + registry.
- `internal/emulation` — scenario orchestrator.
- `internal/audit` — append-only audit log.
- `internal/store` — persistence interfaces.
- `migrations` — Postgres schema.
- `docs` — architecture + support matrix.

## Working preferences (Claude Code)

- **Model floor: minimum Sonnet.** Never dispatch work to Haiku — not for
  subagents / the Task tool, not for any delegated step. Use Sonnet at minimum,
  Opus for anything non-trivial. When spawning agents, set the model explicitly
  (≥ Sonnet) rather than relying on inheritance.
