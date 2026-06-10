# RInfra Live Verification Runbook — DigitalOcean

This checklist documents how to verify the Phase 4 cloud provisioning on a
throwaway DigitalOcean account. The same pattern (credential store → deploy
→ verify → teardown) applies to AWS, GCP, and Azure once those providers
have their TODO(live) seams filled in.

## Prerequisites

1. A throwaway DO account (not production — all resources will be created and
   destroyed during the test).
2. A personal access token with **write** scope from the DO control panel:
   Settings → API → Generate New Token.
3. RInfra server built and running:

   ```bash
   export RINFRA_DEV=1         # use in-memory stores; no Postgres needed
   export PULUMI_CONFIG_PASSPHRASE=changeme-for-local-testing
   go run ./cmd/rinfra-server
   ```

4. Pulumi CLI installed and available on PATH (required by the automation API
   to run the Pulumi engine subprocess). Install: https://www.pulumi.com/docs/install/

## Environment variables required

| Variable                    | Description |
|-----------------------------|-------------|
| `PULUMI_CONFIG_PASSPHRASE`  | Passphrase for encrypting Pulumi secrets in local state. Required; arbitrary value is fine for dev. |
| `PULUMI_BACKEND_URL`        | Set automatically by the Engine to `file://$HOME/.rinfra/pulumi-state`. Do not set manually unless overriding. |
| `RINFRA_DEV`                | Set to `1` to use in-memory stores (no Postgres). Required for this runbook. |

## Step-by-step checklist

### 1. Create an engagement

```bash
curl -s -X POST http://localhost:8080/api/v1/engagements \
  -H 'Content-Type: application/json' \
  -H 'X-RInfra-Operator: operator@example.com' \
  -d '{
    "name": "DO Test Engagement",
    "scope": ["198.51.100.0/24"],
    "authorized": true,
    "auth_start": "2026-01-01T00:00:00Z",
    "auth_end": "2030-01-01T00:00:00Z"
  }' | jq .
```

Note the returned `id` (call it `$ENG_ID`).

### 2. Store the DO token as engagement credentials

```bash
ENG_ID="<id from step 1>"

# The credentials JSON maps to Credentials.Raw.
# Key must be "DIGITALOCEAN_TOKEN" as documented in the DO provider package.
curl -s -X PUT "http://localhost:8080/api/v1/engagements/${ENG_ID}/credentials/digitalocean" \
  -H 'Content-Type: application/json' \
  -H 'X-RInfra-Operator: operator@example.com' \
  -d "{\"DIGITALOCEAN_TOKEN\": \"<your-do-token>\"}"
```

The token is encrypted at rest using RINFRA_MASTER_KEY (ephemeral in dev mode).
It never appears in logs or the audit trail.

### 3. Build a 2-node topology

```bash
curl -s -X PUT "http://localhost:8080/api/v1/engagements/${ENG_ID}/topology" \
  -H 'Content-Type: application/json' \
  -H 'X-RInfra-Operator: operator@example.com' \
  -d '{
    "nodes": [
      {
        "id": "redir-1",
        "spec": {
          "type": "redirector",
          "cloud": "digitalocean",
          "region": "nyc3",
          "size": "s-1vcpu-1gb"
        },
        "canvas": { "name": "HTTPS Redirector" }
      },
      {
        "id": "c2-1",
        "spec": {
          "type": "c2_server",
          "cloud": "digitalocean",
          "region": "nyc3",
          "size": "s-1vcpu-2gb"
        },
        "canvas": { "name": "Sliver C2" }
      }
    ],
    "edges": [
      { "from_node_id": "redir-1", "to_node_id": "c2-1" }
    ]
  }'
```

### 4. Deploy

```bash
curl -s -X POST "http://localhost:8080/api/v1/engagements/${ENG_ID}/deploy" \
  -H 'X-RInfra-Operator: operator@example.com' | jq .
```

Note the returned `job_id`. Provisioning is asynchronous.

### 5. Watch SSE events while deploy runs

```bash
curl -N "http://localhost:8080/api/v1/engagements/${ENG_ID}/events"
```

You should see `node.status` events transitioning: pending → provisioning → live.

### 6. Verify resources exist in the DO control panel

Via the DO web console or doctl:

```bash
# Install doctl: brew install doctl
doctl auth init    # enter your DO token
doctl compute droplet list --tag-name "rinfra:${ENG_ID}" --format Name,PublicIPv4,Status
```

Expected: 2 Droplets with tag `rinfra:<engagement-id>` and status `active`.

Optional additional checks:

```bash
# Check topology returned with live IPs
curl -s "http://localhost:8080/api/v1/engagements/${ENG_ID}/topology" | jq .nodes[].public_ip
```

### 7. Teardown

```bash
curl -s -X POST "http://localhost:8080/api/v1/engagements/${ENG_ID}/teardown" \
  -H 'X-RInfra-Operator: operator@example.com' | jq .
```

### 8. Verify all resources are gone

```bash
# Should return empty list
doctl compute droplet list --tag-name "rinfra:${ENG_ID}"
```

Expected: no Droplets. The Pulumi stack.Destroy call + tagged-resource sweep
in SweepOrphans both run — any escaped resources are caught and deleted.

## Expected Pulumi state location

Stack state is stored at:

```
$HOME/.rinfra/pulumi-state/
  .pulumi/
    stacks/
      rinfra/
        rinfra-<engagement-id>.json
```

After successful teardown, the stack file should be empty or not present.

## Troubleshooting

**"no required credential key DIGITALOCEAN_TOKEN"**
The engagement credentials were not stored, or the wrong key name was used.
Re-run step 2 with the exact key name `DIGITALOCEAN_TOKEN`.

**"stack up failed: error creating Droplet"**
Check the DO API token has write scope. The `active` error in the Pulumi output
will include the DO API error message.

**"PULUMI_CONFIG_PASSPHRASE not set"**
Export `PULUMI_CONFIG_PASSPHRASE` before running the server. Any value works
for local testing.

**Orphaned resources after a failed deploy**
Run teardown (step 7) — it will call `SweepOrphans` which lists and deletes
all Droplets tagged `rinfra:<engagement-id>` even if Pulumi state is partial.
Alternatively: `doctl compute droplet delete $(doctl compute droplet list --tag-name "rinfra:<id>" --no-header --format ID)`.

## Pattern for other clouds

The same flow applies to AWS, GCP, and Azure. The credential key names differ:

| Cloud | Credential keys |
|-------|----------------|
| DigitalOcean | `DIGITALOCEAN_TOKEN` |
| AWS | `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` |
| GCP | `GOOGLE_CREDENTIALS` (SA JSON), `GOOGLE_PROJECT` |
| Azure | `ARM_SUBSCRIPTION_ID`, `ARM_TENANT_ID`, `ARM_CLIENT_ID`, `ARM_CLIENT_SECRET` |

AWS/GCP/Azure providers have `TODO(live)` stubs for the API calls but are
structurally complete and compile-verified. Fill in the TODO(live) stubs and
run through this checklist pattern on each cloud's throwaway account.
