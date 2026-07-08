# RInfra support matrix

## Cloud providers

All four are supported for provisioning + redirector fronting + teardown.
Compute abstracts cleanly; `ConfigureIngress` and DNS diverge per provider and
must be implemented deliberately.

| Provider | Ingress model | Notes |
|----------|---------------|-------|
| DigitalOcean | Cloud Firewall | Implement first: most permissive AUP, cheapest to iterate. Per-node firewall targets by `DropletIDs` only, named `rinfra-fw-<eng>-<node>` (DO firewall `Tags` are a droplet-target selector, not metadata). |
| AWS | Security groups / VPC rules | Enterprise buyer lives here; strictest AUP. Teardown waits out `DependencyViolation`/`InUse` before releasing the EIP + deleting the SG. |
| GCP | VPC firewall rules | One firewall **per source CIDR** so a restricted source's ports aren't unioned onto `0.0.0.0/0`. |
| Azure | Network Security Groups (NSGs) | `ConfigureIngress` rules start at priority 200, above the baked-in allow-ssh (100). |

Ingress is **applied automatically on deploy**: after a node comes live,
`InfraService` derives role-based default inbound rules (SSH everywhere; 80/443 on
redirectors/payload hosts; the C2 listener port on C2 servers) and calls
`ConfigureIngress`. A failure marks the node `degraded` (possibly unreachable)
rather than failing the whole deploy.

**Stable addresses.** Every provider exports a durable public IP as the node's
`PublicIP` so redirector DNS never points at an address that can change: AWS
Elastic IP, GCP static regional Address, Azure static Public IP, and — since a
droplet's own `ipv4_address` is ephemeral — DigitalOcean a **Reserved IP** bound
to the droplet (reclaimed on teardown by the tag sweep).

**DNS zone resolution.** `ManageDNS` accepts the zone as either the provider's
native zone identifier or the DNS domain, and resolves the latter: AWS looks up
the hosted-zone ID by name, and GCP maps the domain to a managed-zone **name** by
matching `DnsName` (a managed-zone name can't contain dots, so the raw apex would
never match). This lets the service default the zone to the apex domain.

Provisioning always uses the **customer's** per-engagement credentials. RInfra
never hosts attacker infra on its own tenancy.

## C2 frameworks

Provisioning + fronting is uniform. **Control is tiered** — automated emulation
only works where the framework exposes a usable operator API.

| Framework | Tier | Automated emulation | Notes |
|-----------|------|---------------------|-------|
| Sliver | Orchestrated | Yes | gRPC operator API (Bishop Fox) |
| Mythic | Orchestrated | Yes | Scripting/GraphQL API, modular C2 profiles |
| Metasploit | Orchestrated | Yes | msfrpcd RPC drives meterpreter; open source, no license |
| custom (in-house) | Orchestrated | Yes | You own the operator surface |
| PoshC2 | Scripted | Partial | Open source; scriptable via v9.0 REST API |
| Havoc | Fronted | No | No headless operator CLI; only an undocumented WebSocket API — deployed + fronted, human-operated |
| Cobalt Strike | Fronted | No | License-gated (customer key); operator drives manually |
| Brute Ratel C4 | Fronted | No | License-gated, EDR-evasion focus; operator drives manually |

`c2.C2Provider.Control()` returns `(Operator, ok)`. `ok=false` (Havoc, Cobalt
Strike, Brute Ratel) means the emulation engine records every technique as
`manual_required` and a human operates the framework.

### Cleanable persistence primitives

Two technique→primitive mappings create real host-side persistence and are
auto-reverted at the end of a run (`c2.IsCleanable`, the `c2.Reverter`
capability): `scheduled_task` and `registry_run_key`. Five more genuinely plant
their artifact (not read-only recon of one) rather than just detecting it:
`shortcut_modification` (T1547.009), `wmi_event_subscription` (T1546.003),
`ifeo_injection` (T1546.012), `port_monitor` (T1547.010), `active_setup`
(T1547.014).

| Primitive | Sliver | Mythic | Metasploit | PoshC2 | custom |
|-----------|:---:|:---:|:---:|:---:|:---:|
| `scheduled_task` | render+revert | render+revert | render+revert | — | — |
| `registry_run_key` | render+revert | — | — | — | — |
| `shortcut_modification` | render+revert | render+revert | render+revert | — | — |
| `wmi_event_subscription` | render+revert | render+revert | render+revert | — | — |
| `ifeo_injection` | render+revert | render+revert | render+revert | — | — |
| `port_monitor` | render+revert | render+revert | render+revert | — | — |
| `active_setup` | render+revert | render+revert | render+revert | — | — |

A "—" reports the technique `unsupported` on that framework (honest BAS
taxonomy, no fabricated attempt) rather than a partial/best-effort try. PoshC2
and custom don't implement any cleanable primitive today — extending them is a
future increment, not an oversight.

### Two usage modes

RInfra supports two ways to use a deployed teamserver:

1. **Automated emulation** — drive the framework through `Operator` (only on
   frameworks with a usable operator API; see the table above). Live operator
   clients are wired for Sliver (gRPC/mTLS), Mythic (GraphQL/HTTPS) and
   Metasploit (msfrpcd/MessagePack).
2. **Manual access** — for operators who don't want auto-run, RInfra opens an
   SSH local port-forward to the teamserver's operator port and the operator
   connects their **native client** (sliver-client, Mythic web UI, Cobalt Strike
   client, …). This mode works for **every** framework, including Fronted-tier
   ones with no `Operator`. See `c2.ManualAccessFor` / `c2.OpenLocalForward`.
   The operator port is never exposed publicly — access is tunneled over the
   per-engagement SSH key, and the service layer audits it.

## Payload generators

Initial-access stager tools, modeled separately from C2 frameworks via
`payload.Generator` (they have no teamserver or sessions). A generator INVOKES
the operator's installed upstream binary — RInfra authors no payload bytes,
encoders, or evasion. Generation is engagement-bound and audited; callers must
pass `domain.Engagement.CanDeploy()` first.

| Generator | Pairs with | Notes |
|-----------|------------|-------|
| msfvenom | Metasploit | Produces meterpreter stagers; part of the Metasploit Framework |


Design seams for these; the current build completes the **offensive** side
(red-team ops + purple-team offensive emulation).
