#!/usr/bin/env bash
#
# RInfra fresh-install / update script.
#
# Brings up the whole stack (Postgres + migrations + Go control plane + web
# console) in Docker. Safe to run repeatedly — run it on every update to rebuild
# images from the current checkout and re-apply migrations. It is idempotent:
# secrets in .env are generated once and reused; existing data is preserved.
#
# Usage:
#   scripts/install.sh            # build + (re)start the stack
#   scripts/install.sh --pull     # git pull first, then build + restart
#   scripts/install.sh --fresh    # also wipe the Postgres volume (DESTRUCTIVE)
#   scripts/install.sh --down      # stop and remove the stack
#
set -euo pipefail

# Resolve repo root (this script lives in scripts/).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

PULL=0
FRESH=0
DOWN=0
for arg in "$@"; do
  case "$arg" in
    --pull) PULL=1 ;;
    --fresh) FRESH=1 ;;
    --down) DOWN=1 ;;
    -h|--help)
      sed -n '3,20p' "$0"
      exit 0 ;;
    *)
      echo "Unknown option: $arg" >&2
      exit 2 ;;
  esac
done

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[!]\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31m[x]\033[0m %s\n' "$*" >&2; exit 1; }

# --- Resolve docker compose command -----------------------------------------
command -v docker >/dev/null 2>&1 || die "docker is not installed or not on PATH."
if docker compose version >/dev/null 2>&1; then
  DC=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  DC=(docker-compose)
else
  die "docker compose (or docker-compose) is required."
fi
docker info >/dev/null 2>&1 || die "Docker daemon is not running."

# --- Tear down ----------------------------------------------------------------
if [[ "$DOWN" == 1 ]]; then
  log "Stopping the RInfra stack..."
  "${DC[@]}" down
  exit 0
fi

# --- Optionally pull latest ---------------------------------------------------
if [[ "$PULL" == 1 ]]; then
  if [[ -d .git ]]; then
    branch="$(git rev-parse --abbrev-ref HEAD)"
    log "Pulling latest changes for branch '$branch'..."
    git pull --ff-only origin "$branch" || warn "git pull failed; continuing with the current checkout."
  else
    warn "Not a git checkout; skipping --pull."
  fi
fi

# --- Ensure .env with generated secrets --------------------------------------
gen_key() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 32
  else
    # Fallback: 32 random bytes from /dev/urandom, base64-encoded.
    head -c 32 /dev/urandom | base64 | tr -d '\n'
  fi
}

if [[ ! -f .env ]]; then
  log "Creating .env with a freshly generated master key..."
  cp .env.example .env
fi

# Fill RINFRA_MASTER_KEY if it is empty/missing.
if ! grep -qE '^RINFRA_MASTER_KEY=.+' .env; then
  key="$(gen_key)"
  # Replace the (possibly empty) line, or append it.
  if grep -qE '^RINFRA_MASTER_KEY=' .env; then
    tmp="$(mktemp)"
    sed "s|^RINFRA_MASTER_KEY=.*|RINFRA_MASTER_KEY=${key}|" .env > "$tmp" && mv "$tmp" .env
  else
    printf 'RINFRA_MASTER_KEY=%s\n' "$key" >> .env
  fi
  log "Generated RINFRA_MASTER_KEY."
fi

# --- Fresh volume wipe (destructive) -----------------------------------------
if [[ "$FRESH" == 1 ]]; then
  warn "Removing the Postgres data volume (all engagement data will be lost)..."
  "${DC[@]}" down -v
fi

# --- Build and start ----------------------------------------------------------
log "Building images from the current checkout..."
"${DC[@]}" build

log "Starting the stack (Postgres → migrations → control plane → web console)..."
"${DC[@]}" up -d

# --- Wait for the control plane to report healthy ----------------------------
log "Waiting for the control plane to come up..."
ok=0
for _ in $(seq 1 30); do
  if curl -fsS http://localhost:8080/healthz >/dev/null 2>&1; then
    ok=1
    break
  fi
  sleep 2
done
if [[ "$ok" == 1 ]]; then
  log "Control plane is healthy."
else
  warn "Control plane did not report healthy yet — check: ${DC[*]} logs server"
fi

cat <<'EOF'

  RInfra is up.

    Web console     http://localhost:3000
    Control plane   http://localhost:8080  (GET /healthz)

  Default console login (change it in Settings → Account):

    username: admin
    password: admin

  Useful commands:
    docker compose ps           # status
    docker compose logs -f web  # follow web logs
    scripts/install.sh --pull   # update to latest, rebuild, restart
    scripts/install.sh --down   # stop everything

EOF
