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
  guaranteed teardown that reconciles actual cloud state, not just our DB. This
  extends to **emulation artifacts**: a persistence technique (scheduled task,
  Run key) is reverted at the end of its run via the optional `c2.Reverter`
  capability (run in reverse order, audited as `emulation.cleanup`), so an
  engagement leaves no orphaned persistence on the customer's host.

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
and translated down to each framework's native primitives. This keeps scenarios
reusable across C2s — the point, for a consultancy that switches frameworks per
engagement. `Technique` references an Atomic Red Team / Caldera ability; it is
not a payload.

The translation is a **two-layer, data-driven** pipeline (do not reintroduce
per-adapter `switch t.AttackID` tables):

1. **Catalog** (`internal/emulation/ttp`, embedded `catalog.yaml`) maps an
   ATT&CK ID → a portable **`c2.Primitive`** (closed `PrimitiveKind` set:
   powershell, shell, sysinfo, process_list, net_connections, net_config,
   file_list, download, scheduled_task, registry_run_key, plus the read-only
   discovery primitives remote_system_discovery, account_discovery,
   permission_group_discovery, service_discovery, network_share_discovery —
   each backed by a safe Windows built-in via `c2.DiscoveryCommand`, rendered
   uniformly by every framework with a shell) plus argument bindings
   (`from` input key, `default`, `required`). `ttp.Compile(t)` resolves a
   `Technique` to a primitive. **This is the "add a TTP" surface** — a technique
   that reuses an existing primitive is a one-entry YAML change, no Go edits.
2. **Renderers** — each `Operator.Execute` adapter has a small
   `renderXxxPrimitive(c2.Primitive)` switching over the closed primitive set
   and emitting that framework's native command(s). Framework-specific defaults
   live here (e.g. `file_list` root is `.` on Sliver/Havoc/PoshC2, `C:\` on
   Metasploit); universal defaults (`whoami`, `RInfraTest`) live in the catalog.
   A framework that doesn't implement a primitive returns `ExecUnsupported` (no
   fabricated attempt). Scripted-tier allowlists (Havoc/PoshC2) are **derived**
   from "catalog × renderer", not hand-maintained.
3. **Fact-aware sequencing** (`internal/emulation` `FactStore`/`Planner`) — a run
   is executed by an "atomic planner": techniques run in scenario order, but each
   can consume facts an earlier one produced. After a successful technique its
   output is parsed (per primitive kind) into a per-run `FactStore` (currently
   routable IPv4 → `host.ip`; the parser switch is the extension point). A later
   technique may reference collected facts in its `Inputs` with `${fact.key}`
   tokens (resolved at run time) and declare `Requires []string` prerequisite
   keys; an unmet requirement or unresolved token records the technique `not_run`
   (an honest non-attempt), never a fabricated run. When a referenced fact has
   several values the technique **fans out** — `Planner.PrepareAll` runs it once
   per value (cartesian across referenced keys), one recorded result each.
   **Deferred (seams only):** an autonomous next-technique decision engine.

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

`internal/redirector` is the translation layer: it renders a `domain.Profile`
(resolved from a built-in profile catalog by `NodeSpec.ProfileName`) plus the
upstream resolved from the topology `Edge` (the C2/payload node the redirector
fronts) into concrete nginx config — default-deny on unlisted paths
(`return 444`), `RewriteHost` → upstream `Host` header. `InfraService.
RedirectorConfig` computes it from the live topology; `GET /engagements/{id}/
nodes/{nodeId}/redirector-config` exposes it. On-box application (cloud-init /
SSH push) and auto-DNS for the front domain are the live-infra step on top.

## Repo map

- `cmd/rinfra-server` — entrypoint, wiring, HTTP server.
- `internal/domain` — core types: engagements, infrastructure, emulation.
- `internal/cloud` — `CloudProvider` interface + per-provider impls + registry.
- `internal/c2` — `C2Provider`/`Operator` interfaces + per-framework impls + registry.
- `internal/payload` — `Generator` interface (msfvenom) + registry.
- `internal/redirector` — renders `domain.Profile` + topology-resolved upstream
  into reverse-proxy (nginx) config; built-in profile catalog.
- `internal/emulation` — scenario orchestrator; `catalog` (scenario YAMLs),
  `index` (SRA index import), `ttp` (technique→`c2.Primitive` catalog).
- `internal/audit` — append-only audit log.
- `internal/store` — persistence interfaces.
- `migrations` — Postgres schema.
- `docs` — architecture + support matrix.

## Working preferences (Claude Code)

- **Docs are part of the change (definition of done).** Any change that alters
  architecture, an interface, a behavior, a build/run step, or the status of the
  build order MUST update the relevant Markdown in the **same PR** — this file
  first, plus `docs/` (SUPPORT_MATRIX, PROJECT_PLAN, the runbooks) as applicable.
  Capture *what* changed, *why*, and *how it works now*. A PR that changes code
  but leaves the docs describing the old behavior is incomplete.
- **Model floor: minimum Sonnet.** Never dispatch work to Haiku — not for
  subagents / the Task tool, not for any delegated step. Use Sonnet at minimum,
  Opus for anything non-trivial. When spawning agents, set the model explicitly
  (≥ Sonnet) rather than relying on inheritance.
