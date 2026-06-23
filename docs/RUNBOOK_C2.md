# RInfra C2 Live-Validation Harness

This is the opt-in harness for validating the C2 layer against **real servers**,
to de-risk the production-blocking "C2 deploy/operate validated live" item
(`docs/PROJECT_PLAN.md` §3) without touching a customer environment.

CI does **not** run any of this. The harness tests are behind the `c2live`
build tag and self-skip unless their target env vars are set, so `go test ./...`
and the normal pipeline are unaffected.

What it covers, in order of how runnable it is:

1. **SSH deploy mechanics** (shared by every C2 adapter) — fully runnable here
   against a throwaway `sshd` container.
2. **Per-framework operator smoke** (Sliver gRPC today) — runnable against a
   real teamserver you stand up; env-gated.
3. **Full deploy → operate** against a real teamserver — the manual checklist at
   the bottom.

---

## 1. SSH deploy mechanics (sshd container)

`deploy.SSHRunner` (`Run` / `Upload`) and the install-script upload+exec path are
the mechanic every C2 deploy relies on. Validate them against a real OpenSSH
server:

```sh
make c2-harness-up      # generates .harness/keys/harness{,.pub} + starts sshd on :2222
make test-c2live        # runs the c2live tests against it
make c2-harness-down
```

`make c2-harness-up` needs `ssh-keygen` and `docker`. It starts the `sshd`
service from `docker-compose.c2.yml` (image `lscr.io/linuxserver/openssh-server`,
user `rinfra`, your harness public key).

`test-c2live` reads:

| Env var | Default (only when the harness is up) | Meaning |
|---|---|---|
| `RINFRA_C2LIVE_SSH_ADDR` | `localhost:2222` | SSH target `host:port` |
| `RINFRA_C2LIVE_SSH_USER` | `rinfra` | login user |
| `RINFRA_C2LIVE_SSH_KEY`  | `.harness/keys/harness` | PEM/OpenSSH private key |

The localhost defaults are filled in **only when `.harness/keys/harness` exists**
(i.e. you ran `make c2-harness-up`). If it doesn't, the SSH vars are left unset
so `TestC2Live_*` in `internal/c2/deploy` self-skip — that's what lets you run an
env-gated framework smoke (e.g. `RINFRA_SLIVER_OPERATOR_CONFIG=./operator.cfg
make test-c2live`) on its own without standing up the sshd target. Any SSH vars
you export yourself are always honoured.

It exercises `TestC2Live_RunAndUpload` (command round-trip + upload read-back) and
`TestC2Live_InstallScriptExec` (upload a script, run via bash, non-zero exit
surfaces as an error). Point the env vars at any reachable host to validate the
runner against a real provisioned node instead of the container.

---

## 2. Sliver operator smoke (real teamserver)

Validates the real gRPC operator client (official `rpcpb` stubs over mTLS)
against a live `sliver-server` multiplayer listener — the path most exposed to
upstream wire-format drift.

On the sliver-server:

```sh
sliver-server operator --name rinfra --lhost <server-ip> --save ./operator.cfg
```

Then run the harness pointed at that config:

```sh
RINFRA_SLIVER_OPERATOR_CONFIG=./operator.cfg make test-c2live
```

`TestC2Live_SliverOperatorSessions` loads the config, dials over mTLS
(`DialOperatorClient`), and calls `Sessions` — a no-side-effect RPC, so success
proves auth + a real round-trip (an empty session list is fine). The smoke
self-skips when the env var is unset.

### Mythic operator smoke

Validates the live GraphQL client against a deployed Mythic teamserver — it
authenticates to `/auth` (or uses a pre-issued API token) and runs a GraphQL
query, the path most exposed to schema drift.

```sh
RINFRA_MYTHIC_URL=https://<host>:7443 \
RINFRA_MYTHIC_USER=mythic_admin RINFRA_MYTHIC_PASSWORD=... \
RINFRA_MYTHIC_INSECURE_TLS=1 \
make test-c2live
```

`TestC2Live_MythicCallbacks` authenticates and issues `Callbacks` (a
no-side-effect read). Set `RINFRA_MYTHIC_API_TOKEN` instead of user/password to
use a pre-issued token. Self-skips unless `RINFRA_MYTHIC_URL` is set.

### Metasploit operator smoke

Validates the live msgpack-RPC client against a deployed `msfrpcd` — it performs
`auth.login` and a `session.list`, the path most exposed to RPC method/field
drift.

```sh
RINFRA_MSF_RPC_URL=https://<host>:55553 \
RINFRA_MSF_RPC_USER=msf RINFRA_MSF_RPC_PASSWORD=... \
make test-c2live
```

`TestC2Live_MetasploitSessionList` logs in and lists sessions (no-side-effect).
Self-skips unless `RINFRA_MSF_RPC_URL` is set.

> Adding more frameworks: drop a `//go:build c2live` test in the framework's
> package that reads its endpoint/credentials from env and skips otherwise, and
> calls a no-side-effect read (Sliver `Sessions`, Mythic `Callbacks`, Metasploit
> `SessionList`). Keep them no-side-effect.

---

## 2b. Cloud-side credential smoke (read-only)

The cloud-side counterpart to the C2 smokes: validates that a real engagement
cloud token authenticates against the live provider API and round-trips, using a
strictly **read-only** call (no resources created or destroyed) — so it never
risks spend or orphaned infra. This is the seam the httptest-backed unit tests
can't cover (real API surface + auth).

DigitalOcean today (`TestCloudLive_DigitalOceanAccount` calls `Account.Get`):

```sh
DIGITALOCEAN_TOKEN=dop_v1_... make test-cloudlive
```

Self-skips unless `DIGITALOCEAN_TOKEN` is set. Add a sibling under another
provider package (`//go:build cloudlive`) the same way — read its token from the
provider's credential env and call a read-only endpoint.

---

## 3. Full deploy → operate (manual, real teamserver)

The end-to-end production path (provision a C2 node, install the framework over
SSH, start a listener, get a session, execute a technique) needs a reachable
host plus the framework's release artifact and is driven through the service
layer. Outline:

1. Provision a throwaway node (see `docs/RUNBOOK_DO.md` for the cloud side) and
   note its public IP.
2. Export the per-engagement SSH key material the live runner uses:
   `RINFRA_SSH_PRIVATE_KEY` (or `_FILE`), `RINFRA_SSH_USER`, `RINFRA_SSH_PORT`.
3. Deploy the framework (`C2Provider.Deploy`) — the install script fetches the
   pinned release by URL + SHA-256 and starts the systemd unit. Confirm the unit
   is running on the node.
4. For Orchestrated/Scripted tiers, fetch/operator-config the framework and
   export its creds (e.g. `RINFRA_SLIVER_OPERATOR_CONFIG`,
   `RINFRA_MSF_RPC_USER`/`_PASSWORD`), then drive a listener + a test session via
   the `Operator` (`StartListener` / `Sessions` / `Execute`).
5. Verify the redirector nginx profile fronts the listener end-to-end.

This is the remaining per-framework live work; the harness above shortens the
loop for the parts that don't need a full teamserver.
