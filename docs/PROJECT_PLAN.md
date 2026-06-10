# RInfra — Full Project Plan (backend · API · DB · UI integration)

This is the master plan for taking RInfra from the current scaffold to a
working MVP. It follows the build order in CLAUDE.md and is grounded in what
already exists. Each phase is a PR-sized, independently shippable chunk with
acceptance criteria. The UI plan in `docs/UI_IMPLEMENTATION_PLAN.md` is done
(see `web/`); this plan covers everything behind it.

## 0. Current state (inventory)

**Done:**
- `internal/domain` — Engagement/RoE/Authorization with `CanDeploy()`,
  infrastructure types (Node/Edge/Topology/Rule), emulation types
  (Technique/Scenario/ScenarioRun/Result).
- Interface spine: `store` (Engagement/Infra/Scenario stores), `audit.Logger`,
  `cloud.CloudProvider`, `c2.C2Provider`/`Operator`, `payload.Generator` —
  all defined, with **stub** per-provider/per-framework adapters registered.
- `migrations/0001_init.sql` — engagements, nodes, edges, scenario_runs,
  technique_results, audit_events.
- `cmd/rinfra-server` — boot wiring + `/healthz` only. stdlib-only; no
  external Go deps yet.
- `web/` — complete 5-screen console (Next.js static export) running on a
  typed mock store behind the `RInfraClient` interface (`web/lib/client.ts`).

**Missing (this plan):** Postgres implementations, the HTTP API, real cloud
provisioning (Pulumi), real C2 adapters, the emulation engine end-to-end,
credentials handling, CI, and wiring the UI to the API.

## 1. Architecture target

```
web/ (Next.js static export, RInfraClient seam)
   |  REST/JSON + SSE
cmd/rinfra-server (chi router)
   ├─ internal/api          HTTP handlers (thin) + SSE hub          [new]
   ├─ internal/service      engagement / infra / emulation services [new]
   │     └─ every privileged path: Engagement.CanDeploy() + audit.Record()
   ├─ internal/orchestration Pulumi automation-API engine           [new]
   ├─ internal/cloud        CloudProvider impls (DO → AWS → GCP → Azure)
   ├─ internal/c2           C2Provider impls (Sliver, Mythic → Havoc → CS)
   ├─ internal/emulation    scenario orchestrator (exists, needs sources)
   ├─ internal/secrets      envelope encryption for creds/licenses  [new]
   ├─ internal/audit        Postgres append-only logger             [impl]
   └─ internal/store        pgx implementations                     [impl]
            └─ Postgres (migrations/, golang-migrate)
```

**Decisions** (per CLAUDE.md options):
- **DB access: plain `pgx/v5`** (no sqlc codegen — fewer moving parts; the
  store surface is small). Queries live in `internal/store/postgres/`.
- **Router: `chi`**, stdlib handlers, JSON codecs in one place.
- **Live updates: SSE** (one event stream per engagement) — simpler than
  websockets, fits one-way status pushes (node status, run progress).
- **IDs:** Postgres UUIDs, surfaced as strings in domain structs (already
  string-typed).
- **Background work:** in-process job runner (goroutine + persistent job row
  per deploy/teardown/run). No external queue for MVP.
- **Simulated cloud first:** a `cloud.Fake` provider (registry id `fake`,
  dev-only, behind a config flag) simulates provisioning with realistic
  delays/state transitions so the full deploy→live→teardown loop works
  end-to-end before any Pulumi code exists. CI and the UI develop against it.

## 2. Security invariants → enforcement points

| Invariant (CLAUDE.md) | Enforced at |
|---|---|
| Authorization gates every deploy | `service.Infra.Deploy/Teardown`, `service.Emulation.Start`, `payload` generation — all call `eng.CanDeploy(now)` first; API returns 403 with the sentinel reason |
| Bring-your-own cloud credentials | `cloud.Credentials` only ever loaded from `engagement_credentials` (encrypted at rest); no global/default provider config exists; server refuses to start a deploy without engagement creds |
| Everything audited | `service.*` emits `audit.Event` for deploy, teardown, scenario start/step/finish, credential create/use, scope/status change; the Postgres logger has INSERT-only statements; migration adds a DB rule/trigger blocking UPDATE/DELETE on `audit_events` |
| Teardown reliability | deploy writes `provider_ref` per resource as it's created (not after); teardown reconciles by listing actual cloud resources tagged `rinfra:<engagement_id>` and destroying anything found, then marks DB rows; `Destroy` is idempotent |
| Secrets never logged | `secrets` package types implement `slog.LogValuer` + `fmt.Stringer` returning `[redacted]`; audit `Detail` builders take allow-listed fields only |

## 3. Database schema (delta on `0001_init.sql`)

New migration `0002_engagement_fields.sql`:
- `engagements`: add `codename TEXT`, `lead_operator TEXT`,
  `scope_exclusions JSONB DEFAULT '[]'`, `engagement_type TEXT`.
- `nodes`: add `name TEXT`, `subtype TEXT` (HTTPS/HTTP/DNS redirector kinds),
  `listener TEXT`, `front_domain TEXT`, `cost_estimate NUMERIC(8,2) DEFAULT 0`,
  `x INT DEFAULT 0`, `y INT DEFAULT 0` (canvas position — UI state worth
  persisting with the topology).

New migration `0003_credentials_jobs.sql`:
- `engagement_credentials(id, engagement_id FK, provider TEXT,
  ciphertext BYTEA, nonce BYTEA, key_id TEXT, created_at, last_used_at)` —
  envelope-encrypted (AES-256-GCM, data key wrapped by master key from
  `RINFRA_MASTER_KEY`; KMS later). One row per provider per engagement.
  C2 license keys reuse this table with `provider = 'c2:<framework>'`.
- `teamservers(id, node_id FK, framework TEXT, host, port, status,
  connection_info_ciphertext BYTEA, created_at)`.
- `jobs(id, engagement_id FK, kind TEXT  -- deploy|teardown|scenario_run,
  status TEXT, detail JSONB, created_at, started_at, finished_at, err TEXT)`
  — durable record of background work; survives restarts (reconcile on boot).
- `audit_events`: add trigger `audit_events_immutable` raising on
  UPDATE/DELETE.

`scenario_runs.results` stay normalized in `technique_results` (already
modeled).

Scenario *catalog* (APT29 etc.) ships as code/data files
(`internal/emulation/catalog/*.yaml`), not DB — it's versioned content, with
each technique referencing an Atomic Red Team GUID / Caldera ability id.

## 4. HTTP API (v1)

Base `/api/v1`, JSON. Errors: `{ "error": { "code", "message" } }`; the
CanDeploy sentinels map to `403 authorization_required|auth_expired|
outside_window|empty_scope`. Contracts mirror `web/lib/types.ts` (that file is
the reference shape; adjust field names there only if the Go side has a
stronger claim).

| Method & path | Purpose |
|---|---|
| `GET  /engagements` · `POST /engagements` | list / create (create records scope, RoE, named authorization; emits `engagement.create`) |
| `GET  /engagements/{id}` · `PATCH /engagements/{id}` | fetch / update status & metadata (audited) |
| `GET/PUT /engagements/{id}/topology` | read / save the canvas graph (nodes+edges, draft-state only fields editable while pending) |
| `POST /engagements/{id}/validate` | server-side topology checks (same rules as the UI popover + CanDeploy) |
| `POST /engagements/{id}/deploy` | gate → create `jobs` row → async provision; `409` if a job is running |
| `POST /engagements/{id}/teardown` | gate-free (teardown must always work) → drain → destroy → reconcile |
| `PUT  /engagements/{id}/credentials/{provider}` | store encrypted creds (write-only; GET returns metadata, never material) |
| `GET  /engagements/{id}/events` | **SSE**: node status, job progress, run progress |
| `GET  /engagements/{id}/audit` | read-only audit page (paginated) |
| `GET  /c2/frameworks` | registry contents: name, tier, gated, listeners |
| `GET  /scenarios` | catalog |
| `POST /engagements/{id}/runs` · `GET /runs/{id}` | start scenario run / fetch results |
| `GET  /healthz` | liveness (exists) |

Middleware: request ID, structured request log, recoverer, CORS (dev:
`localhost:3000`), and an **operator identity seam** — `X-RInfra-Operator`
header for MVP (single-tenant consultancy tool), mapped to `audit.Event.Actor`;
real authn (OIDC) is a later phase, the middleware is the slot.

## 5. Phases

### Phase 1 — Persistence + audit (Postgres)  ▸ first to implement
- `go.mod`: add `pgx/v5`, `chi`, `golang-migrate` (CLI usage documented, not
  vendored), `google/uuid`.
- Migrations `0002`, `0003` (see §3) + fix `0001` to golang-migrate file
  naming (`.up.sql`/`.down.sql` split — current file uses sql-migrate
  comment style, wrong for the documented tool).
- `internal/store/postgres`: implementations of the three store interfaces
  (+ new `CredentialStore`, `JobStore` interfaces in `internal/store`).
- `internal/audit/postgres`: INSERT-only logger.
- `internal/secrets`: AES-256-GCM envelope encryption, master key from env,
  redacting types.
- In-memory fakes for every store interface (`internal/store/memstore`) used
  by service tests and by the server's `--dev` mode (run without Postgres).
- `docker-compose.yml` (postgres:16) + Makefile `db-up`, `migrate-up`.
- **Accept:** `go test ./...` green (fakes); store integration tests behind
  `//go:build integration` pass against compose Postgres; audit UPDATE/DELETE
  rejected by trigger.

### Phase 2 — Services + HTTP API + fake provider (end-to-end loop)
- `internal/service`: `Engagement`, `Infra`, `Emulation` services holding the
  business logic (gate → audit → store → events). Handlers stay thin.
- `internal/api`: chi router, all §4 endpoints, SSE hub.
- `internal/cloud/fake`: simulated provider (provisioning delays,
  deterministic IPs, in-memory "actual state" so teardown reconciliation is
  exercisable in tests).
- Job runner: deploy/teardown execute async, update node statuses, publish
  SSE, finish job row; boot-time reconciliation re-adopts orphaned running
  jobs.
- `cmd/rinfra-server`: full wiring (config from env: `DATABASE_URL`,
  `RINFRA_MASTER_KEY`, `RINFRA_DEV`).
- **Accept:** table-driven handler tests (httptest + memstore) cover the gate
  (draft engagement → 403 on deploy), happy deploy→live→teardown→destroyed
  via fake provider, audit rows emitted for every privileged action.

### Phase 3 — UI ↔ API integration
- `web/lib/client.ts`: `RestClient implements RInfraClient` (fetch + SSE via
  `EventSource`), selected by `NEXT_PUBLIC_RINFRA_API`; mock remains the
  default for static demo builds.
- Replace local deploy/teardown/emulation simulation in the store with API
  calls + SSE-driven state when the REST client is active; New Engagement
  posts to the API; credentials + license-key fields wire to the
  credentials endpoint.
- CORS verified; `make dev` target runs server (+compose) and web together.
- **Accept:** with `RINFRA_DEV=1` + fake provider, the full user journey
  (create engagement → authorize → build topology → deploy → watch nodes go
  live → run scenario → teardown) works in the browser against the Go server.

### Phase 4 — Real clouds via Pulumi (DigitalOcean first)
- `internal/orchestration`: topology → Pulumi program (automation API), one
  stack per engagement (`rinfra-<engagement-id>`), state in a local/workspace
  backend configured per deployment; every resource tagged
  `rinfra:<engagement-id>`.
- `internal/cloud/digitalocean`: droplets, cloud firewalls
  (`ConfigureIngress`), reserved IPs, DNS records — real implementation.
- Teardown = `stack destroy` + provider-level sweep of tagged resources that
  escaped state (the reconciliation promise).
- Then AWS (EC2/SG/EIP/Route53), GCP (GCE/VPC firewall/Cloud DNS), Azure
  (VM/NSG/Public IP/DNS) as separate PRs — `ConfigureIngress`/DNS written
  per provider, deliberately.
- **Accept:** CI still runs only fakes; a manual `docs/RUNBOOK_DO.md`
  checklist documents live verification on a throwaway DO account.

### Phase 5 — C2 layer (real adapters)
- Deploy mechanics shared helper: upload + systemd unit via SSH (key
  generated per engagement, stored encrypted), composing official release
  artifacts only.
- **Sliver** (Orchestrated): teamserver install script, multiplayer config;
  `Operator` over Sliver's gRPC API (listeners, sessions, execute).
- **Mythic** (Orchestrated): docker-compose install; `Operator` over Mythic's
  REST/GraphQL.
- **Havoc** (Scripted): deploy + partial operator (scripted API subset).
- **Cobalt Strike** (Fronted): deploy path only — requires customer license
  key from the credentials store; `Control()` returns `ok=false`.
- `RedirectorConfig`: nginx reverse-proxy templates per framework profile,
  installed on redirector nodes.
- **Accept:** unit tests against recorded/fake framework APIs; Sliver adapter
  integration-tested against a local sliver-server container in an opt-in
  test target.

### Phase 6 — Emulation end-to-end
- Technique catalog YAMLs (APT29 / FIN7 / ransomware-affiliate scenarios,
  each technique = Atomic Red Team GUID reference + params; no payload
  content in-repo).
- `service.Emulation`: pick session, stream per-technique progress over SSE,
  persist `scenario_runs` + `technique_results` (orchestrator already
  exists — wire sources + persistence + events).
- Reporting endpoints: coverage rollup per engagement (feeds the existing
  Coverage screen); ATT&CK Navigator JSON export.
- **Accept:** run against fake Operator in CI; UI timeline driven by real SSE.

### Phase 0 (parallel, immediate) — CI
- `.github/workflows/ci.yml`: Go job (`build`, `vet`, `test`, gofmt check) +
  Postgres service container for integration-tagged tests; web job
  (`npm ci`, lint, `tsc`, build). Required for the PR.

### Deferred (unchanged from CLAUDE.md — seams only)
SIEM/EDR reconciliation, coverage heatmap exports, detection-as-code, QRadar
connectors; OIDC auth; multi-tenancy; `go:embed` of the web export.

## 6. Testing strategy

- Table-driven unit tests everywhere; **no live cloud/C2 in CI** — services
  tested against `memstore` + `cloud/fake` + fake `Operator`.
- Store layer: integration tests (`go:build integration`) against compose/CI
  Postgres, including the audit-immutability trigger.
- API: `httptest` round-trips asserting status codes, gate errors, audit
  emission.
- Web: existing lint/tsc/build; Playwright smoke (journey of Phase 3) as a
  follow-up, not a gate.

## 7. Sequencing & sizing

| Order | Phase | Size | Depends on |
|---|---|---|---|
| 1 | 0 CI | S | — |
| 2 | 1 Persistence + audit | M | — |
| 3 | 2 Services + API + fake provider | L | 1 |
| 4 | 3 UI integration | M | 2 |
| 5 | 4 Pulumi + DO (then AWS/GCP/Azure) | L (+M×3) | 2 |
| 6 | 5 C2 adapters (Sliver, Mythic first) | L | 4 |
| 7 | 6 Emulation E2E | M | 5 |

Phases 0–3 need no cloud accounts and deliver a fully demonstrable product on
the fake provider. 4–6 turn it real, provider by provider, framework by
framework.
