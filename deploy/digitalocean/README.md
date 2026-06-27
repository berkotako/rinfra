# Deploy RInfra on a DigitalOcean droplet

One script stands up the whole platform on a fresh droplet: Docker, a swapfile on
small droplets, the firewall, and the RInfra stack — **Postgres → migrations →
the all-in-one control plane (Go API + web console on one origin) → Caddy** for
automatic HTTPS.

This deploys the **RInfra platform itself**. Provisioning attacker
infrastructure *into* DigitalOcean (or AWS/GCP/Azure) for an engagement is a
separate, in-app flow — see [`docs/RUNBOOK_DO.md`](../../docs/RUNBOOK_DO.md).

## 1. Create the droplet

- **Image:** Ubuntu 22.04 or 24.04 (x64).
- **Size:** **2 GB RAM / 1 vCPU minimum** (the web console is built from source on
  the box). The installer adds a 2 GB swapfile on 1 GB droplets so the build
  doesn't OOM, but 2 GB is the comfortable floor.
- **Auth:** add your SSH key.

For HTTPS, point an `A` record for your domain (e.g. `console.example.com`) at the
droplet's public IP before (or shortly after) running the installer.

## 2. Run the installer

SSH in as `root`, then either pipe it straight from the repo:

```bash
curl -fsSL https://raw.githubusercontent.com/berkotako/rinfra/main/deploy/digitalocean/install.sh \
  | bash -s -- --domain console.example.com
```

…or clone first and run it:

```bash
git clone https://github.com/berkotako/rinfra.git /opt/rinfra
sudo /opt/rinfra/deploy/digitalocean/install.sh --domain console.example.com
```

Omit `--domain` to serve plain HTTP on port 80 (IP-only first boot). The script
generates all secrets, brings the stack up, waits for health, and prints the URL
plus the generated `admin` password.

### Options

| Flag | Effect |
|------|--------|
| `--domain <fqdn>` | Serve HTTPS for this domain (Caddy gets a Let's Encrypt cert automatically). |
| `--branch <name>` | Deploy a branch other than `main`. |
| `--repo <url>` | Clone from a fork. |
| `--update` | `git pull` + rebuild + restart. |
| `--down` | Stop the stack (data volumes are kept). |
| `--no-firewall` | Don't touch `ufw`. |

## 3. First login

Open the printed URL and log in as `admin` with the generated password (also in
`deploy/digitalocean/.env`). **Change it immediately** in *Settings → Account*.

## What it runs

- **`app`** — the all-in-one image (`Dockerfile.allinone`): the Go API at
  `/api/v1`, the web console for everything else, on one origin (`:8080`). It is
  not published to the host; Caddy is the only public entrypoint, so there is no
  CORS to configure.
- **`postgres`** — durable data in the `postgres_data` volume.
- **`migrate`** — one-shot golang-migrate run that applies `migrations/`.
- **`caddy`** — TLS terminator / reverse proxy on `:80`/`:443`. With a domain it
  provisions and renews a Let's Encrypt cert; without one it serves HTTP on `:80`.

Secrets live in `deploy/digitalocean/.env` (`chmod 600`). **Back it up.** Losing
`RINFRA_MASTER_KEY` makes stored cloud credentials undecryptable.

## Updating

```bash
sudo /opt/rinfra/deploy/digitalocean/install.sh --update
```

Re-runs are idempotent: existing `.env` secrets and the Postgres volume are
preserved; images are rebuilt from the latest checkout and migrations re-applied.

## Live cloud provisioning (Pulumi) — optional follow-up

The deploy/teardown endpoints that provision attacker infrastructure use the
**Pulumi CLI**, which is **not** in the slim all-in-one image. Everything else —
engagements, RoE/authorization, audit, the emulation engine, the console — works
without it. To enable live provisioning, build a derived image that adds Pulumi
on top of the all-in-one image and point the `app` service at it, e.g.:

```dockerfile
# deploy/digitalocean/Dockerfile.pulumi
FROM rinfra-allinone
USER root
RUN apk add --no-cache curl bash && curl -fsSL https://get.pulumi.com | sh \
 && ln -s /root/.pulumi/bin/pulumi /usr/local/bin/pulumi
USER rinfra
```

Then set the `app` build to that Dockerfile (with `Dockerfile.allinone` built and
tagged `rinfra-allinone` first). `PULUMI_CONFIG_PASSPHRASE` is already wired in
`.env`. Per-engagement **cloud credentials are supplied by the customer in-app**
(bring-your-own-cloud) — never baked into the image.

## Teardown

```bash
sudo /opt/rinfra/deploy/digitalocean/install.sh --down   # stop, keep data
cd /opt/rinfra/deploy/digitalocean && docker compose down -v   # also wipe data
```

Then destroy the droplet from the DigitalOcean control panel.
