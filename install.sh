#!/usr/bin/env bash
# AiRouter — one-command install script
# Usage: bash install.sh [OPTIONS]
#
# Options:
#   --port PORT          Backend port (default: random 10000-60000)
#   --frontend-port PORT Frontend port (default: random 10000-60000)
#   --upstream-url URL   Upstream AI API base URL (required)
#   --upstream-key KEY   Upstream AI API key (required)
#   --no-start           Generate .env and exit without starting containers
#   --force              Overwrite existing .env without asking
#
# After install the script prints all credentials and the panel URL.

set -euo pipefail

#──────────────────────────────────────────────────────────────────────────────
# Helpers
#──────────────────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

info()    { echo -e "${CYAN}[•]${RESET} $*"; }
success() { echo -e "${GREEN}[✓]${RESET} $*"; }
warn()    { echo -e "${YELLOW}[!]${RESET} $*"; }
error()   { echo -e "${RED}[✗]${RESET} $*" >&2; exit 1; }

gen_secret() {
    # 32 bytes → 64 hex chars (URL-safe)
    if command -v openssl &>/dev/null; then
        openssl rand -hex 32
    elif command -v dd &>/dev/null && [ -r /dev/urandom ]; then
        dd if=/dev/urandom bs=32 count=1 2>/dev/null | xxd -p | tr -d '\n'
    else
        # pure bash fallback (less entropy but acceptable)
        cat /proc/sys/kernel/random/uuid 2>/dev/null | tr -d '-' || \
        printf '%x%x%x%x' $RANDOM $RANDOM $RANDOM $RANDOM
    fi
}

gen_path() {
    # 12 random hex chars → short but unguessable path segment
    if command -v openssl &>/dev/null; then
        openssl rand -hex 6
    else
        printf '%x%x%x' $RANDOM $RANDOM $RANDOM
    fi
}

gen_port() {
    # Random port in 10000-59999 range
    echo $(( RANDOM % 50000 + 10000 ))
}

check_port_free() {
    local port=$1
    if command -v ss &>/dev/null; then
        ss -ltn 2>/dev/null | grep -q ":${port} " && return 1 || return 0
    elif command -v netstat &>/dev/null; then
        netstat -ltn 2>/dev/null | grep -q ":${port} " && return 1 || return 0
    fi
    return 0  # can't check, assume free
}

pick_free_port() {
    local port
    for _ in $(seq 1 20); do
        port=$(gen_port)
        if check_port_free "$port"; then
            echo "$port"
            return
        fi
    done
    echo $(gen_port)  # give up and use last random
}

#──────────────────────────────────────────────────────────────────────────────
# Parse arguments
#──────────────────────────────────────────────────────────────────────────────
OPT_PORT=""
OPT_FRONTEND_PORT=""
OPT_UPSTREAM_URL=""
OPT_UPSTREAM_KEY=""
OPT_NO_START=0
OPT_FORCE=0

while [[ $# -gt 0 ]]; do
    case $1 in
        --port)            OPT_PORT="$2";           shift 2 ;;
        --frontend-port)   OPT_FRONTEND_PORT="$2";  shift 2 ;;
        --upstream-url)    OPT_UPSTREAM_URL="$2";   shift 2 ;;
        --upstream-key)    OPT_UPSTREAM_KEY="$2";   shift 2 ;;
        --no-start)        OPT_NO_START=1;          shift ;;
        --force)           OPT_FORCE=1;             shift ;;
        -h|--help)
            sed -n '2,12p' "$0" | sed 's/^# \?//'
            exit 0
            ;;
        *) warn "Unknown option: $1"; shift ;;
    esac
done

#──────────────────────────────────────────────────────────────────────────────
# Checks
#──────────────────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

command -v docker &>/dev/null || error "Docker is not installed. Please install Docker first."
docker compose version &>/dev/null 2>&1 || \
    docker-compose version &>/dev/null 2>&1 || \
    error "docker compose (v2) is not available. Please install Docker Compose v2."

# Prefer 'docker compose' (v2), fall back to 'docker-compose' (v1)
if docker compose version &>/dev/null 2>&1; then
    DC="docker compose"
else
    DC="docker-compose"
fi

#──────────────────────────────────────────────────────────────────────────────
# Existing .env guard
#──────────────────────────────────────────────────────────────────────────────
if [ -f ".env" ] && [ "$OPT_FORCE" -eq 0 ]; then
    warn ".env already exists."
    read -rp "    Overwrite and regenerate all secrets? [y/N] " answer
    [[ "$answer" =~ ^[Yy]$ ]] || { info "Aborting. Your existing .env was not changed."; exit 0; }
fi

#──────────────────────────────────────────────────────────────────────────────
# Interactive prompts for required values (if not provided via flags)
#──────────────────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}═══════════════════════════════════════${RESET}"
echo -e "${BOLD}       AiRouter — Quick Installer      ${RESET}"
echo -e "${BOLD}═══════════════════════════════════════${RESET}"
echo ""

if [ -z "$OPT_UPSTREAM_URL" ]; then
    read -rp "  Upstream AI API base URL  [https://api.ecomagent.in]: " OPT_UPSTREAM_URL
    OPT_UPSTREAM_URL="${OPT_UPSTREAM_URL:-https://api.ecomagent.in}"
fi

if [ -z "$OPT_UPSTREAM_KEY" ]; then
    read -rp "  Upstream AI API key (leave empty to configure later): " OPT_UPSTREAM_KEY
fi

#──────────────────────────────────────────────────────────────────────────────
# Determine ports (interactive prompts if not set via flags)
#──────────────────────────────────────────────────────────────────────────────
if [ -z "$OPT_PORT" ]; then
    _suggested_backend=$(pick_free_port)
    read -rp "  Backend port  [${_suggested_backend}]: " _input_backend
    OPT_PORT="${_input_backend:-${_suggested_backend}}"
fi

if [ -z "$OPT_FRONTEND_PORT" ]; then
    _suggested_frontend=$(pick_free_port)
    while [ "$_suggested_frontend" = "$OPT_PORT" ]; do
        _suggested_frontend=$(pick_free_port)
    done
    read -rp "  Frontend port [${_suggested_frontend}]: " _input_frontend
    OPT_FRONTEND_PORT="${_input_frontend:-${_suggested_frontend}}"
fi

#──────────────────────────────────────────────────────────────────────────────
# Generate secrets
#──────────────────────────────────────────────────────────────────────────────
info "Generating secrets…"

DB_PASSWORD=$(gen_secret)
DB_USER="airouter"
DB_NAME="airouter"
ADMIN_TOKEN=$(gen_secret)
ADMIN_PATH=$(gen_path)
SERVER_HOST="${SERVER_HOST:-localhost}"

# VITE_API_URL — what the browser uses to reach the backend
VITE_API_URL="http://${SERVER_HOST}:${OPT_PORT}"

#──────────────────────────────────────────────────────────────────────────────
# Write .env
#──────────────────────────────────────────────────────────────────────────────
cat > .env << EOF
# ─── Database ────────────────────────────────────────────────────────────────
POSTGRES_DB=${DB_NAME}
POSTGRES_USER=${DB_USER}
POSTGRES_PASSWORD=${DB_PASSWORD}

# ─── Backend ─────────────────────────────────────────────────────────────────
PORT=${OPT_PORT}
FRONTEND_PORT=${OPT_FRONTEND_PORT}

# Admin panel token (used as password in the login form)
ADMIN_TOKEN=${ADMIN_TOKEN}

# Secret URL path for admin API — /admin routes are only accessible at this path
# Example: if ADMIN_PATH=xK3mP9aQ then API is at /xK3mP9aQ/keys, etc.
ADMIN_PATH=${ADMIN_PATH}

# ─── Upstream AI API ─────────────────────────────────────────────────────────
UPSTREAM_BASE_URL=${OPT_UPSTREAM_URL}
UPSTREAM_API_KEY=${OPT_UPSTREAM_KEY}

# ─── Frontend ────────────────────────────────────────────────────────────────
# URL the browser uses to reach the backend API
VITE_API_URL=${VITE_API_URL}
EOF

success ".env written"

#──────────────────────────────────────────────────────────────────────────────
# Print summary
#──────────────────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}┌─────────────────────────────────────────────┐${RESET}"
echo -e "${BOLD}│           Installation Summary              │${RESET}"
echo -e "${BOLD}└─────────────────────────────────────────────┘${RESET}"
echo ""
echo -e "  ${BOLD}Admin Panel URL:${RESET}   ${GREEN}http://${SERVER_HOST}:${OPT_FRONTEND_PORT}${RESET}"
echo -e "  ${BOLD}Backend API URL:${RESET}   ${CYAN}http://${SERVER_HOST}:${OPT_PORT}${RESET}"
echo -e "  ${BOLD}Admin API path:${RESET}    ${YELLOW}/${ADMIN_PATH}/...${RESET}   (keep secret!)"
echo ""
echo -e "  ${BOLD}Admin token:${RESET}       ${YELLOW}${ADMIN_TOKEN}${RESET}"
echo ""
echo -e "  ${BOLD}DB password:${RESET}       ${YELLOW}${DB_PASSWORD}${RESET}"
echo ""
echo -e "  ${RED}⚠  Save the token above — it won't be shown again.${RESET}"
echo ""

# Save a human-readable credentials file
CREDS_FILE=".airouter_credentials"
cat > "$CREDS_FILE" << EOF
# AiRouter credentials — generated $(date -u '+%Y-%m-%d %H:%M UTC')
# Keep this file SECRET and out of version control.

Admin Panel:   http://${SERVER_HOST}:${OPT_FRONTEND_PORT}
Backend API:   http://${SERVER_HOST}:${OPT_PORT}
Admin path:    /${ADMIN_PATH}

Admin token:   ${ADMIN_TOKEN}
DB password:   ${DB_PASSWORD}
EOF
chmod 600 "$CREDS_FILE"
info "Credentials also saved to ${BOLD}${CREDS_FILE}${RESET} (chmod 600)"

# Make sure it's gitignored
if [ -f ".gitignore" ]; then
    grep -qxF ".airouter_credentials" .gitignore || echo ".airouter_credentials" >> .gitignore
else
    echo ".airouter_credentials" > .gitignore
fi

#──────────────────────────────────────────────────────────────────────────────
# Start containers
#──────────────────────────────────────────────────────────────────────────────
if [ "$OPT_NO_START" -eq 1 ]; then
    info "Skipping container start (--no-start)."
    echo ""
    info "To start later:  ${BOLD}$DC up -d --build${RESET}"
    exit 0
fi

echo ""
info "Starting containers (this may take a few minutes on first run)…"
echo ""

$DC up -d --build

echo ""
success "AiRouter is running!"
echo ""
echo -e "  ${BOLD}Open the panel:${RESET}  ${GREEN}http://${SERVER_HOST}:${OPT_FRONTEND_PORT}${RESET}"
echo -e "  ${BOLD}Login token:${RESET}     ${YELLOW}${ADMIN_TOKEN}${RESET}"
echo ""
echo -e "  ${BOLD}Logs:${RESET}  $DC logs -f backend"
echo -e "  ${BOLD}Stop:${RESET}  $DC down"
echo ""
