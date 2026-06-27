#!/usr/bin/env bash
#
# RInfra — fresh DigitalOcean droplet installer.
#
# Run this on a brand-new Ubuntu 22.04/24.04 droplet (as root) to stand up the
# whole platform: Docker, a 2 GB swapfile on small droplets, the firewall, and
# the RInfra stack (Postgres + migrations + the all-in-one control plane + Caddy
# for same-origin HTTPS). Idempotent — safe to re-run to update.
#
# One-liner on a fresh droplet:
#   curl -fsSL https://raw.githubusercontent.com/berkotako/rinfra/main/deploy/digitalocean/install.sh | bash
#
# Or from a checkout:
#   sudo deploy/digitalocean/install.sh --domain console.example.com
#
# Options:
#   --domain <fqdn>   Serve HTTPS for this domain (A record must point here).
#                     Omit to serve plain HTTP on port 80 (IP-only / first boot).
#   --repo <url>      Git repo to clone (default: https://github.com/berkotako/rinfra.git).
#   --branch <name>   Branch to deploy (default: main).
#   --update          git pull + rebuild + restart the running stack.
#   --down            Stop and remove the stack (data volumes are preserved).
#   --no-firewall     Do not touch ufw.
#   -h | --help       Show this help.
#
set -euo pipefail

REPO_URL="https://github.com/berkotako/rinfra.git"
BRANCH="main"
INSTALL_DIR="/opt/rinfra"
DOMAIN=""
DO_UPDATE=0
DO_DOWN=0
DO_FIREWALL=1

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[!]\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31m[x]\033[0m %s\n' "$*" >&2; exit 1; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --domain) DOMAIN="${2:?--domain needs a value}"; shift 2 ;;
    --repo)   REPO_URL="${2:?--repo needs a value}"; shift 2 ;;
    --branch) BRANCH="${2:?--branch needs a value}"; shift 2 ;;
    --update) DO_UPDATE=1; shift ;;
    --down)   DO_DOWN=1; shift ;;
    --no-firewall) DO_FIREWALL=0; shift ;;
    -h|--help) sed -n '3,33p' "$0"; exit 0 ;;
    *) die "Unknown option: $1 (try --help)" ;;
  esac
done

[[ "$(id -u)" -eq 0 ]] || die "Run as root (use sudo). RInfra needs to install packages and configure the firewall."

# --- Locate the compose stack ------------------------------------------------
# Running from a checkout? Use it. Otherwise clone/refresh to $INSTALL_DIR.
SCRIPT_DIR=""
if _d="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd)"; then SCRIPT_DIR="$_d"; fi
if [[ -n "$SCRIPT_DIR" && -f "$SCRIPT_DIR/docker-compose.yml" && -f "$SCRIPT_DIR/../../go.mod" ]]; then
  ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
  COMPOSE_DIR="$SCRIPT_DIR"
  log "Using the current checkout at $ROOT_DIR"
else
  log "Installing prerequisites for clone..."
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq git ca-certificates curl >/dev/null
  if [[ -d "$INSTALL_DIR/.git" ]]; then
    log "Refreshing existing checkout at $INSTALL_DIR ..."
    git -C "$INSTALL_DIR" fetch --depth 1 origin "$BRANCH"
    git -C "$INSTALL_DIR" checkout -B "$BRANCH" "origin/$BRANCH"
  else
    log "Cloning $REPO_URL ($BRANCH) into $INSTALL_DIR ..."
    git clone --depth 1 --branch "$BRANCH" "$REPO_URL" "$INSTALL_DIR"
  fi
  ROOT_DIR="$INSTALL_DIR"
  COMPOSE_DIR="$INSTALL_DIR/deploy/digitalocean"
fi
cd "$COMPOSE_DIR"
SELF="$COMPOSE_DIR/install.sh"   # accurate path for the printed manage commands

# --- Resolve docker compose --------------------------------------------------
compose() { docker compose "$@"; }

# --- Tear down ---------------------------------------------------------------
if [[ "$DO_DOWN" == 1 ]]; then
  command -v docker >/dev/null 2>&1 || die "docker is not installed."
  log "Stopping the RInfra stack (data volumes preserved)..."
  compose down
  exit 0
fi

# --- Install Docker (engine + compose plugin) --------------------------------
if ! command -v docker >/dev/null 2>&1; then
  log "Installing Docker Engine + compose plugin..."
  curl -fsSL https://get.docker.com | sh
else
  log "Docker already installed: $(docker --version)"
fi
docker compose version >/dev/null 2>&1 || die "The Docker Compose v2 plugin is required but missing."
systemctl enable --now docker >/dev/null 2>&1 || true
export DOCKER_BUILDKIT=1   # needed for Dockerfile.allinone.dockerignore

# --- Swap on small droplets (the web build OOMs on 1 GB) ---------------------
mem_mb="$(awk '/MemTotal/ {print int($2/1024)}' /proc/meminfo 2>/dev/null || echo 0)"
if [[ "$mem_mb" -lt 2048 && ! -f /swapfile ]]; then
  log "Low memory (${mem_mb} MB) — adding a 2 GB swapfile so the image build doesn't OOM..."
  fallocate -l 2G /swapfile || dd if=/dev/zero of=/swapfile bs=1M count=2048
  chmod 600 /swapfile
  mkswap /swapfile >/dev/null
  swapon /swapfile
  grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
fi

# --- Firewall ----------------------------------------------------------------
if [[ "$DO_FIREWALL" == 1 ]] && command -v ufw >/dev/null 2>&1; then
  log "Configuring ufw (allow SSH + 80/443)..."
  ufw allow OpenSSH      >/dev/null 2>&1 || ufw allow 22/tcp >/dev/null 2>&1 || true
  ufw allow 80/tcp       >/dev/null 2>&1 || true
  ufw allow 443/tcp      >/dev/null 2>&1 || true
  ufw --force enable     >/dev/null 2>&1 || true
fi

# --- .env with generated secrets ---------------------------------------------
gen_b64() { openssl rand -base64 32 2>/dev/null || head -c 32 /dev/urandom | base64 | tr -d '\n'; }
gen_pw()  { openssl rand -hex 24   2>/dev/null || head -c 24 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 32; }

[[ -f .env ]] || { log "Creating .env from .env.example ..."; cp .env.example .env; }

# Fill any empty `KEY=` line in .env with a generated value (idempotent: a value
# already set is never overwritten, so re-runs keep your secrets).
fill_secret() {
  local key="$1" val="$2"
  if grep -qE "^${key}=.+" .env; then return 0; fi   # already set
  local tmp; tmp="$(mktemp)"
  if grep -qE "^${key}=" .env; then
    sed "s|^${key}=.*|${key}=${val}|" .env > "$tmp"
  else
    cp .env "$tmp"; printf '%s=%s\n' "$key" "$val" >> "$tmp"
  fi
  mv "$tmp" .env
  GENERATED+=("$key")
}
GENERATED=()
fill_secret RINFRA_MASTER_KEY "$(gen_b64)"
fill_secret POSTGRES_PASSWORD "$(gen_pw)"
fill_secret PULUMI_CONFIG_PASSPHRASE "$(gen_pw)"
ADMIN_PW="$(gen_pw)"
fill_secret RINFRA_ADMIN_PASSWORD "$ADMIN_PW"
if [[ ${#GENERATED[@]} -gt 0 ]]; then log "Generated secrets: ${GENERATED[*]}"; fi

# Site address: --domain → HTTPS for that domain; otherwise leave the .env value.
if [[ -n "$DOMAIN" ]]; then
  log "Setting site address to $DOMAIN (Caddy will request a Let's Encrypt cert)..."
  tmp="$(mktemp)"
  if grep -qE '^RINFRA_SITE_ADDRESS=' .env; then
    sed "s|^RINFRA_SITE_ADDRESS=.*|RINFRA_SITE_ADDRESS=${DOMAIN}|" .env > "$tmp"
  else
    cp .env "$tmp"; printf 'RINFRA_SITE_ADDRESS=%s\n' "$DOMAIN" >> "$tmp"
  fi
  mv "$tmp" .env
fi
chmod 600 .env

# --- Optionally pull latest (update path) ------------------------------------
if [[ "$DO_UPDATE" == 1 && -d "$ROOT_DIR/.git" ]]; then
  log "Pulling latest ($BRANCH)..."
  git -C "$ROOT_DIR" pull --ff-only origin "$BRANCH" || warn "git pull failed; building the current checkout."
fi

# --- Build and start ---------------------------------------------------------
log "Building images (Go control plane + web console + Caddy)... this takes a few minutes."
compose build
log "Starting the stack (Postgres → migrations → control plane → Caddy)..."
compose up -d

# --- Wait for the control plane to report healthy ----------------------------
log "Waiting for the control plane to become healthy..."
cid="$(compose ps -q app)"
ok=0
for _ in $(seq 1 60); do
  status="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$cid" 2>/dev/null || echo starting)"
  case "$status" in
    healthy|running) ok=1; break ;;
    exited|dead) break ;;
  esac
  sleep 3
done

# --- Report ------------------------------------------------------------------
ip="$(curl -fsS --max-time 3 http://169.254.169.254/metadata/v1/interfaces/public/0/ipv4/address 2>/dev/null || true)"
[[ -n "$ip" ]] || ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
if [[ -n "$DOMAIN" ]]; then url="https://${DOMAIN}"; else url="http://${ip:-<droplet-ip>}"; fi

echo
if [[ "$ok" == 1 ]]; then
  log "RInfra is up."
else
  warn "The stack started but the control plane is not healthy yet."
  warn "Check logs:  cd $COMPOSE_DIR && docker compose logs -f app"
fi
cat <<EOF

  Console + API   ${url}/
  Health check    ${url}/healthz

  First login (change the password in Settings → Account):
    username: admin
    password: $(grep -E '^RINFRA_ADMIN_PASSWORD=' .env | cut -d= -f2-)

  Secrets live in:  ${COMPOSE_DIR}/.env   (chmod 600 — back it up; losing
  RINFRA_MASTER_KEY makes stored cloud credentials undecryptable)

  Manage:
    cd ${COMPOSE_DIR}
    docker compose ps                 # status
    docker compose logs -f app        # follow control-plane logs
    sudo ${SELF} --update             # update to latest
    sudo ${SELF} --down               # stop the stack

EOF

if [[ -z "$DOMAIN" ]]; then
  warn "Serving plain HTTP. Before exposing real credentials, re-run with"
  warn "  --domain <fqdn>  (A record → ${ip:-this droplet}) to enable automatic HTTPS,"
  warn "  or log in over an SSH tunnel:  ssh -L 8080:localhost:80 root@${ip:-<droplet-ip>}"
fi

warn "Live cloud provisioning (deploy/teardown of attacker infra) needs the Pulumi"
warn "CLI inside the app image — see deploy/digitalocean/README.md. The platform"
warn "(engagements, audit, emulation, console) runs fully without it."
