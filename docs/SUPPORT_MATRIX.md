# RInfra support matrix

## Cloud providers

All four are supported for provisioning + redirector fronting + teardown.
Compute abstracts cleanly; `ConfigureIngress` and DNS diverge per provider and
must be implemented deliberately.

| Provider | Ingress model | Notes |
|----------|---------------|-------|
| DigitalOcean | Cloud Firewall | Implement first: most permissive AUP, cheapest to iterate |
| AWS | Security groups / VPC rules | Enterprise buyer lives here; strictest AUP |
| GCP | VPC firewall rules | |
| Azure | Network Security Groups (NSGs) | |

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
| Havoc | Scripted | Partial | Teamserver API; less stable to automate |
| PoshC2 | Scripted | Partial | Open source; scriptable but no modern API |
| Cobalt Strike | Fronted | No | License-gated (customer key); operator drives manually |
| Brute Ratel C4 | Fronted | No | License-gated, EDR-evasion focus; operator drives manually |

`c2.C2Provider.Control()` returns `(Operator, ok)`. `ok=false` (Cobalt Strike,
Brute Ratel) means the emulation engine records every technique as `skipped` and
a human operates the framework.

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
