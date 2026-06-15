# RInfra — Roadmap & Status

This document is the **current roadmap and delivery status** for RInfra. It
replaces the original build plan (a scaffold-to-MVP plan written when only the
domain types and `/healthz` existed). That historical plan — now substantially
delivered — is preserved in the **Archive** at the bottom as a changelog.

_Last updated: 2026-06-15._

> How to read this: **Completed** = implemented and covered by CI against
> fakes/memstore. **Partially completed** = implemented but only exercised
> against fakes / not yet validated live. **Production-blocking** = must be
> resolved before running a real customer engagement. **Next milestones** = the
> ordered near-term work.

---

## 1. Completed

**Foundations & security model**
- `internal/domain`: Engagement / RoE / Authorization with `CanDeploy()`;
  infrastructure (Node/Edge/Topology/Rule) and emulation (Technique / Scenario /
  ScenarioRun / Result) types.
- **Scope enforcement**: robust `TargetInScope` (CIDR containment incl.
  CIDR-in-CIDR, IPv4/IPv6, bare IP, domain exact + subdomain, `*.` wildcard,
  exclusion precedence) + `EnforceTargetInScope`; enforced at emulation
  execution time (in-scope agent selection), not just at deploy.

**Persistence & audit**
- Postgres stores via `pgx/v5` (`internal/store/postgres`) + in-memory fakes
  (`internal/store/memstore`) for tests and `--dev` mode.
- Migrations `000001`–`000006` (init, engagement fields, credentials/jobs,
  users/projects, user scenarios, user techniques).
- Append-only audit (`internal/audit`, Postgres + memstore); immutability
  enforced.
- `internal/secrets`: AES-256-GCM envelope encryption; redacting types.

**HTTP API (chi) — `internal/api`**
- AuthN: bearer-token sessions, seeded admin, roles (admin/lead/operator);
  query-param token accepted for browser streaming (SSE/WebSocket).
- AuthZ: **project-membership/role guard on every engagement-scoped route**
  (admin → all; lead → led projects; operator → member projects).
- Engagements (CRUD + status/authorization), topology get/put/validate,
  deploy/teardown (async jobs + boot reconciliation), credentials (write-only,
  encrypted), SSE events, audit feed.
- C2: frameworks registry, manual-access descriptor, **teamservers list**,
  **SSH tunnel with bounded lifecycle** (ownership + idle/absolute TTL +
  shutdown), **in-browser web shell over WebSocket**.
- Emulation: scenarios **full CRUD** + runs; **TTPs full CRUD**; coverage
  rollup; ATT&CK **Navigator JSON export**.
- Users & projects (+ membership) administration.

**Orchestration & cloud**
- `internal/orchestration`: Pulumi automation-API engine; one stack per
  engagement; resources tagged `rinfra:<engagement-id>`; teardown sweep
  (`cloud/sweeper.go`).
- `internal/cloud`: `CloudProvider` impls for **DigitalOcean, AWS, GCP, Azure**
  + `fake`; per-provider ingress divergence covered by tests.

**C2 & payload**
- `internal/c2`: registry + adapters — Sliver / Mythic / Metasploit / custom
  (Orchestrated), Havoc / PoshC2 (Scripted), Cobalt Strike / Brute Ratel
  (Fronted). Manual access, SSH tunnel, and browser web shell.
- `internal/payload`: msfvenom generator.

**Emulation**
- Orchestrator + YAML catalog (`internal/emulation/catalog`); capability-aware
  routing to a supporting, in-scope agent; coverage + Navigator export.

**Frontend (`web/`)**
- Next.js console wired to `RestClient` (live REST + SSE) with the mock client
  as the offline fallback; screens: Engagements, Projects, Infrastructure
  builder, C2 frameworks, **Alive C2s** (multi-terminal), **TTPs** (CRUD +
  C2-capability tags), Emulation (scenario builder/CRUD, auto-route to agent),
  Coverage & Reports, Users, Settings.

**Tooling & CI**
- `.github/workflows/ci.yml` (Go build/vet/test/gofmt + web lint/tsc/build);
  `pages.yml` (static demo deploy).
- `docker-compose.yml` (Postgres), `Makefile`, integration tests behind
  `//go:build integration`.

---

## 2. Partially completed (implemented; needs live validation)

- **Real cloud provisioning** is implemented for all four providers, but CI
  runs only the `fake` provider. Live verification is the manual
  `docs/RUNBOOK_DO.md` checklist; only DigitalOcean has a documented live path —
  AWS/GCP/Azure are unverified against real accounts.
- **C2 live operation**: Sliver and Mythic have live transports/operator paths;
  the shared deploy mechanics (SSH upload + systemd) and live `Operator` calls
  are opt-in and under-verified against real teamservers. Redirector nginx
  profiles are templated but not end-to-end verified.
- **Web shell** streams a *bounded, read-only operator command surface*, not a
  raw remote PTY into the teamserver (deliberate, per the authorization/audit
  invariants). A raw passthrough would require an `Operator` shell primitive +
  a reachable host with credentials.
- **Authored scenarios/TTPs** persist via the backend in REST mode; the static
  GitHub Pages demo keeps them session-local (demo constraint, not a product
  gap).

---

## 3. Production-blocking (before real engagements)

1. **Live-cloud E2E validation** per provider: `deploy → live → teardown →
   reconcile` on throwaway accounts, confirming the tagged-resource sweep
   destroys anything that escaped Pulumi state. Required before trusting
   teardown (orphaned infra = cost/exposure/ToS risk).
2. **C2 deploy/operate validated live** for each supported framework (real
   teamserver install, listener, session, execute) + redirector fronting
   verified end-to-end.
3. **Key management**: the master key is env-provided (`RINFRA_MASTER_KEY`).
   Move to a KMS/HSM-backed data-key flow with documented rotation before
   holding customer cloud credentials and C2 license keys at scale.
4. **AuthN hardening**: bearer-token sessions today — add OIDC/SSO, review
   session expiry/rotation, and replace the query-param streaming token
   (SSE/WS) with a short-lived token or a WebSocket subprotocol.
5. **Teardown durability under failure**: job-runner re-adoption on boot is
   tested with fakes; needs a live soak across induced crashes. C2 tunnels are
   process-bound (no cross-restart reconcile) — operational runbooks must cover
   restart.

---

## 4. Next milestones (ordered)

1. **DigitalOcean live pass** via `RUNBOOK_DO.md` → mark DO production-ready;
   then repeat the checklist for AWS, GCP, Azure.
2. **Sliver + Mythic live integration** (opt-in container test targets) green,
   plus one real-teamserver soak; promote the Havoc scripted path.
3. **KMS-backed secrets** + key rotation; secret-scanning in CI.
4. **OIDC auth** + multi-operator session hardening; audit-log viewer filters.
5. **Detection-validation phase** (still deferred, design-seams only):
   SIEM/EDR reconciliation, coverage heatmaps, detection-as-code export, QRadar
   and other connectors.

---

## 5. Reference (implemented design)

The architecture, decisions, security invariants, schema, and API contract
below were the plan and are now **implemented**. Kept here as living reference.

### Architecture

```
web/ (Next.js, RInfraClient seam: RestClient | MockClient)
   |  REST/JSON + SSE + WebSocket (web shell)
cmd/rinfra-server (chi router)
   ├─ internal/api          HTTP handlers (thin) + SSE hub + WS shell
   ├─ internal/service      engagement / infra / emulation / c2 / auth / project
   │     └─ every privileged path: Engagement.CanDeploy() + audit.Record()
   ├─ internal/orchestration Pulumi automation-API engine
   ├─ internal/cloud        CloudProvider impls (DO, AWS, GCP, Azure, fake)
   ├─ internal/c2           C2Provider/Operator impls (8 frameworks)
   ├─ internal/emulation    scenario orchestrator + YAML catalog
   ├─ internal/payload      Generator (msfvenom)
   ├─ internal/secrets      envelope encryption
   ├─ internal/audit        append-only logger (Postgres + memstore)
   └─ internal/store        pgx implementations + memstore fakes
            └─ Postgres (migrations/, golang-migrate)
```

### Decisions (delivered)
- DB access: plain `pgx/v5` (no sqlc). Router: `chi`. Live updates: **SSE** per
  engagement; **WebSocket** for the operator web shell. IDs: Postgres UUIDs as
  strings. Background work: in-process job runner with durable `jobs` rows and
  boot reconciliation. The `cloud/fake` provider drives CI and the demo
  end-to-end.

### Security invariants → enforcement points

| Invariant | Enforced at |
|---|---|
| Authorization gates every deploy | `service.Infra.Deploy/Teardown`, `service.Emulation.Start`, C2 manual-access/tunnel/shell, payload generation — all call `eng.CanDeploy(now)`; API maps sentinels to 403 |
| Project-membership boundaries | `requireEngagementAccess` middleware on every `/engagements/{id}` route + `ProjectService.CanAccessProject` |
| Scope enforced on action | `Engagement.TargetInScope` / `EnforceTargetInScope`; emulation runs only against in-scope agents |
| Bring-your-own cloud credentials | `engagement_credentials` (envelope-encrypted); no global provider config; deploy refuses without engagement creds |
| Everything audited | `service.*` emits `audit.Event` for deploy/teardown/scenario/credential/scope/status/tunnel/shell; Postgres logger is INSERT-only + immutability trigger |
| Teardown reliability | per-resource `provider_ref` + tagged-resource sweep reconciliation; idempotent destroy |
| Bounded C2 tunnels | engagement/opener binding, idle+absolute TTL reaper, shutdown hook |
| Secrets never logged | `secrets` types redact via `LogValuer`/`Stringer`; audit details are allow-listed |

### Schema & API
Schema is migrations `000001`–`000006` (engagements, nodes, edges,
scenario_runs, technique_results, audit_events + immutability, credentials,
jobs, users, projects + membership, sessions, user_scenarios, user_techniques).
The API surface is enumerated in §1 and mirrored by `web/lib/types.ts` and
`web/lib/client.ts`. Errors map the `CanDeploy` sentinels to
`403 authorization_required|auth_expired|outside_window|empty_scope`.

---

## Archive — original build plan (historical, delivered)

> The notes below are the **original scaffold-to-MVP plan**, describing the work
> when `cmd/rinfra-server` had `/healthz` only and Postgres, the API, Pulumi,
> real C2, emulation E2E, credentials, and CI were all missing. Every phase has
> since landed (see §1). Retained as a changelog of intent.

**Original "current state" (now obsolete):** domain types + interface spine +
stub adapters + `0001_init.sql` + `/healthz`-only server + mock-only web.
Declared missing: Postgres impls, HTTP API, Pulumi, real C2, emulation E2E,
credentials, CI, UI↔API wiring — **all now delivered.**

**Original phases (all delivered):**
- **Phase 0 — CI**: Go + web workflows. ✅ `.github/workflows/ci.yml`.
- **Phase 1 — Persistence + audit**: pgx stores, memstore fakes, migrations,
  audit immutability, secrets, compose + Makefile. ✅
- **Phase 2 — Services + HTTP API + fake provider**: chi router, services with
  gate→audit→store→events, async job runner, fake cloud. ✅
- **Phase 3 — UI ↔ API integration**: `RestClient` + SSE, store wired to the
  API behind `NEXT_PUBLIC_RINFRA_API`. ✅
- **Phase 4 — Real clouds via Pulumi**: orchestration engine + DO/AWS/GCP/Azure
  providers + tagged-resource teardown sweep. ✅ (implemented; live validation
  pending — see §2/§3).
- **Phase 5 — C2 layer**: 8 framework adapters across the support tiers, manual
  access + tunnel + web shell. ✅ (live operation under-verified — §2).
- **Phase 6 — Emulation E2E**: catalog YAMLs, run persistence + SSE progress,
  coverage rollup + Navigator export. ✅

**Still deferred (unchanged):** SIEM/EDR reconciliation, coverage heatmap
exports, detection-as-code, QRadar connectors; OIDC auth; multi-tenancy;
`go:embed` of the web export.
