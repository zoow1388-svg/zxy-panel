#!/usr/bin/env bash
set -euo pipefail

VERSION="0.7.5.5"
APP_DIR=${APP_DIR:-/opt/zxy-panel}
CONFIG_DIR=${CONFIG_DIR:-/etc/zxy-panel}
INFO_FILE="$CONFIG_DIR/panel.info"
API_PORT=${API_PORT:-8088}
WEB_PORT=${WEB_PORT:-5173}
PANEL_PORT=${PANEL_PORT:-}
WEB_BASE_PATH=${WEB_BASE_PATH:-}
AUTO_AGENT=${AUTO_AGENT:-true}
INSTALL_XRAY=${INSTALL_XRAY:-true}
SETUP_XRAY_SERVICE=${SETUP_XRAY_SERVICE:-true}
FRESH_INSTALL=${FRESH_INSTALL:-false}
APT_UPDATED=false
START_TS="$(date +%s)"

export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=a
export NEEDRESTART_SUSPEND=1

step() {
  echo
  echo "======================================"
  echo "$1"
  echo "======================================"
}

elapsed() {
  local now
  now="$(date +%s)"
  printf '%ss' "$((now - START_TS))"
}

apt_update_once() {
  if [[ "$APT_UPDATED" != "true" ]]; then
    apt-get update
    APT_UPDATED=true
  fi
}

apt_install_missing() {
  local missing=()
  local pkg
  for pkg in "$@"; do
    if ! dpkg -s "$pkg" >/dev/null 2>&1; then
      missing+=("$pkg")
    fi
  done
  if [[ "${#missing[@]}" -eq 0 ]]; then
    echo "Dependencies already installed: $*"
    return 0
  fi
  echo "Installing missing packages: ${missing[*]}"
  apt_update_once
  apt-get install -y \
    -o Dpkg::Options::="--force-confdef" \
    -o Dpkg::Options::="--force-confold" \
    "${missing[@]}"
}

random_string() {
  python3 - "$1" <<'PY_RANDOM'
import secrets
import string
import sys
n = int(sys.argv[1])
alphabet = string.ascii_letters + string.digits
print(''.join(secrets.choice(alphabet) for _ in range(n)), end='')
PY_RANDOM
}

port_in_use() {
  local p="$1"
  ss -lnt 2>/dev/null | awk '{print $4}' | grep -qE "[:.]${p}$"
}

random_unused_port() {
  local p
  for _ in $(seq 1 80); do
    p=$(shuf -i 30000-59999 -n 1)
    if ! port_in_use "$p"; then
      echo "$p"
      return 0
    fi
  done
  echo "$(shuf -i 30000-59999 -n 1)"
}

compose_cmd() {
  if docker compose version >/dev/null 2>&1; then
    echo "docker compose"
  elif command -v docker-compose >/dev/null 2>&1; then
    echo "docker-compose"
  else
    echo ""
  fi
}

public_ip() {
  local ip
  for url in https://api.ipify.org https://ifconfig.me/ip https://icanhazip.com; do
    ip=$(curl -fsSL --max-time 5 "$url" 2>/dev/null | tr -d '[:space:]' || true)
    if [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      echo "$ip"
      return 0
    fi
  done
  hostname -I 2>/dev/null | awk '{print $1}'
}

panel_info_value() {
  local key="$1"
  if [[ -f "$INFO_FILE" ]]; then
    grep -E "^${key}=" "$INFO_FILE" | head -n1 | cut -d= -f2- || true
  fi
}

env_file_value() {
  local key="$1"
  if [[ -f "$APP_DIR/.env" ]]; then
    grep -E "^${key}=" "$APP_DIR/.env" | head -n1 | cut -d= -f2- || true
  fi
}

install_base_deps() {
  step "Installing base dependencies"
  if command -v apt-get >/dev/null 2>&1; then
    apt_install_missing curl ca-certificates nginx python3 rsync iproute2
  elif command -v yum >/dev/null 2>&1; then
    yum install -y curl ca-certificates nginx python3 rsync iproute
  else
    echo "ERROR: unsupported system. Please use Debian/Ubuntu/CentOS."
    exit 1
  fi
}

install_docker_if_missing() {
  step "Checking Docker and Docker Compose"

  if ! command -v docker >/dev/null 2>&1; then
    echo "Docker not found, installing docker.io..."
    if command -v apt-get >/dev/null 2>&1; then
      apt_install_missing docker.io
    elif command -v yum >/dev/null 2>&1; then
      yum install -y docker
    else
      echo "ERROR: unsupported system, cannot install Docker automatically."
      exit 1
    fi
  else
    echo "Docker already installed: $(docker --version 2>/dev/null || true)"
  fi

  systemctl enable docker >/dev/null 2>&1 || true
  systemctl start docker >/dev/null 2>&1 || true

  if docker compose version >/dev/null 2>&1; then
    echo "Docker Compose v2 available: $(docker compose version 2>/dev/null || true)"
    return 0
  fi
  if command -v docker-compose >/dev/null 2>&1; then
    echo "Docker Compose v1 available: $(docker-compose --version 2>/dev/null || true)"
    return 0
  fi

  echo "Docker Compose not found, installing compose package..."
  if command -v apt-get >/dev/null 2>&1; then
    apt_update_once
    if apt-get install -y docker-compose-plugin; then
      echo "Docker Compose plugin installed."
    else
      echo "docker-compose-plugin not available from current apt sources, fallback to docker-compose v1."
      apt-get install -y docker-compose
    fi
  elif command -v yum >/dev/null 2>&1; then
    yum install -y docker-compose-plugin || yum install -y docker-compose
  fi
}

cleanup_old_runtime() {
  step "Cleaning old ZXY Panel runtime"
  systemctl stop zxy-agent 2>/dev/null || true
  if command -v docker >/dev/null 2>&1; then
    docker rm -f zxy-panel-api zxy-panel-frontend 2>/dev/null || true
    docker ps -aq --filter "name=zxy-panel" | xargs -r docker rm -f 2>/dev/null || true
  fi
  rm -f /etc/nginx/conf.d/zxy-panel.conf 2>/dev/null || true
}

disable_default_nginx_sites() {
  step "Disabling default Nginx 80 site"
  rm -f /etc/nginx/sites-enabled/default 2>/dev/null || true
  rm -f /etc/nginx/conf.d/default.conf 2>/dev/null || true
}

write_panel_info() {
  mkdir -p "$CONFIG_DIR"
  cat > "$INFO_FILE" <<EOF_INFO
USERNAME=${ADMIN_USERNAME}
PASSWORD=${ADMIN_PASSWORD_DISPLAY}
PORT=${PANEL_PORT}
WEB_BASE_PATH=${WEB_BASE_PATH}
WebBasePath=${WEB_BASE_PATH}
DATABASE=JSON (${APP_DIR}/data/zxy-panel.json)
Database=JSON (${APP_DIR}/data/zxy-panel.json)
ACCESS_URL=http://${PUBLIC_IP}:${PANEL_PORT}/${WEB_BASE_PATH}/
Access URL=http://${PUBLIC_IP}:${PANEL_PORT}/${WEB_BASE_PATH}/
API_TOKEN=${AGENT_SECRET}
API Token=${AGENT_SECRET}
INSTALL_DIR=${APP_DIR}
CONFIG_DIR=${CONFIG_DIR}
VERSION=${VERSION}
EOF_INFO
  chmod 600 "$INFO_FILE"
}

write_host_nginx() {
  step "Writing host Nginx reverse proxy"
  mkdir -p /etc/nginx/conf.d
  cat > /etc/nginx/conf.d/zxy-panel.conf <<EOF_NGINX
server {
    listen ${PANEL_PORT};
    server_name _;

    client_max_body_size 20m;

    location = / {
        return 302 /${WEB_BASE_PATH}/;
    }

    location /${WEB_BASE_PATH}/ {
        proxy_pass http://127.0.0.1:${WEB_PORT}/${WEB_BASE_PATH}/;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
EOF_NGINX
  nginx -t
  systemctl enable nginx >/dev/null 2>&1 || true
  systemctl restart nginx
}

install_cli() {
  step "Installing zxy-panel CLI"
  install -m 0755 "$APP_DIR/scripts/zxy-panel" /usr/local/bin/zxy-panel
}

allow_local_firewall() {
  if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
    ufw allow "${PANEL_PORT}/tcp" || true
  fi
}

print_result() {
  echo
  echo "ZXY Panel installed successfully"
  echo
  echo "Username:    ${ADMIN_USERNAME}"
  echo "Password:    ${ADMIN_PASSWORD_DISPLAY}"
  echo "Port:        ${PANEL_PORT}"
  echo "WebBasePath: ${WEB_BASE_PATH}"
  echo "Database:    JSON (${APP_DIR}/data/zxy-panel.json)"
  echo "Access URL:  http://${PUBLIC_IP}:${PANEL_PORT}/${WEB_BASE_PATH}/"
  echo "API Token:   ${AGENT_SECRET}"
  echo
  echo "Info saved:  ${INFO_FILE}"
  echo
  echo "Commands:"
  echo "  zxy-panel info"
  echo "  zxy-panel status"
  echo "  zxy-panel restart"
  echo "  zxy-panel logs"
  echo "  zxy-panel reset-password"
  echo
  echo "Important: open TCP port ${PANEL_PORT} in your cloud firewall/security group for panel access."
  echo "Important: also open every node inbound port you create, otherwise client tools cannot connect."
  echo "Install duration: $(elapsed)"
}

wait_api() {
  step "Waiting for API"
  for i in $(seq 1 90); do
    if curl -fsS "http://127.0.0.1:${API_PORT}/api/health" >/dev/null 2>&1; then
      echo "API is ready."
      return 0
    fi
    sleep 1
  done
  echo "ERROR: API not ready."
  echo "Check logs: cd ${APP_DIR} && ${COMPOSE} logs --tail=120 zxy-panel-api"
  exit 1
}

install_local_agent() {
  if [[ "$AUTO_AGENT" != "true" ]]; then
    return 0
  fi
  step "Installing local Agent automatically"
  SERVER_PICK=$(APP_DIR="$APP_DIR" ZXY_LOCAL_SERVER_IP="$PUBLIC_IP" ZXY_LOCAL_SERVER_HOST="$LOCAL_HOST" python3 - <<'PY'
import json, os
p=os.path.join(os.environ.get('APP_DIR','/opt/zxy-panel'),'data','zxy-panel.json')
try:
    d=json.load(open(p,encoding='utf-8'))
except Exception:
    print('|')
    raise SystemExit
servers=list(d.get('servers',{}).values())
local_ip=os.environ.get('ZXY_LOCAL_SERVER_IP','')
local_host=os.environ.get('ZXY_LOCAL_SERVER_HOST','')
def score(s):
    v=0
    if s.get('name') == '本机服务器': v += 10
    if s.get('ip') in (local_ip, local_host): v += 8
    if s.get('host') in (local_ip, local_host): v += 8
    if s.get('status') == 'online': v += 2
    return v
if not servers:
    print('|')
else:
    s=max(servers, key=score)
    print(s.get('id','') + '|' + s.get('agent_token',''))
PY
)
  SERVER_ID="${SERVER_PICK%%|*}"
  AGENT_TOKEN="${SERVER_PICK#*|}"
  if [[ -z "$SERVER_ID" || -z "$AGENT_TOKEN" ]]; then
    echo "WARNING: local server not found, skip Agent auto install."
  else
    chmod +x deploy/agent-install.sh
    INSTALL_XRAY="$INSTALL_XRAY" SETUP_XRAY_SERVICE="$SETUP_XRAY_SERVICE" APPLY_CONFIG=true PANEL_BASE="http://127.0.0.1:${API_PORT}" SERVER_ID="$SERVER_ID" AGENT_TOKEN="$AGENT_TOKEN" ./deploy/agent-install.sh
  fi
}

echo "ZXY Panel V${VERSION} product installer"
echo "Target: ${APP_DIR}"

if [[ $(id -u) -ne 0 ]]; then
  echo "ERROR: please run as root."
  exit 1
fi

cleanup_old_runtime
install_base_deps
disable_default_nginx_sites
install_docker_if_missing

if ! command -v docker >/dev/null 2>&1; then
  echo "ERROR: Docker is required but was not installed successfully."
  exit 1
fi
COMPOSE=$(compose_cmd)
if [[ -z "$COMPOSE" ]]; then
  echo "ERROR: docker compose plugin or docker-compose is required but was not installed successfully."
  exit 1
fi

SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PUBLIC_IP=${ZXY_LOCAL_SERVER_IP:-$(public_ip)}
LOCAL_HOST=${ZXY_LOCAL_SERVER_HOST:-$PUBLIC_IP}

EXISTING_PORT=""
EXISTING_WEB_BASE_PATH=""
EXISTING_USERNAME=""
EXISTING_PASSWORD=""
EXISTING_AGENT_SECRET=""
EXISTING_MANIFEST_URL=""
if [[ "$FRESH_INSTALL" != "true" && -f "$INFO_FILE" ]]; then
  EXISTING_PORT=$(panel_info_value PORT)
  EXISTING_WEB_BASE_PATH=$(panel_info_value WEB_BASE_PATH)
  EXISTING_USERNAME=$(panel_info_value USERNAME)
  EXISTING_PASSWORD=$(panel_info_value PASSWORD)
  EXISTING_AGENT_SECRET=$(panel_info_value API_TOKEN)
fi
EXISTING_MANIFEST_URL=$(env_file_value ZXY_UPDATE_MANIFEST_URL)

PANEL_PORT=${PANEL_PORT:-${EXISTING_PORT:-$(random_unused_port)}}
WEB_BASE_PATH=${WEB_BASE_PATH:-${EXISTING_WEB_BASE_PATH:-$(random_string 18)}}
ADMIN_USERNAME=${ZXY_ADMIN_USERNAME:-${EXISTING_USERNAME:-$(random_string 10)}}
ADMIN_PASSWORD=${ZXY_ADMIN_PASSWORD:-${EXISTING_PASSWORD:-$(random_string 12)}}
ADMIN_PASSWORD_DISPLAY="$ADMIN_PASSWORD"
JWT_SECRET=$(random_string 64)
AGENT_SECRET=${EXISTING_AGENT_SECRET:-$(random_string 64)}
MANIFEST_URL_TO_WRITE=${ZXY_UPDATE_MANIFEST_URL:-${EXISTING_MANIFEST_URL:-}}

step "Preparing directories"
mkdir -p "$APP_DIR" "$APP_DIR/backups" "$CONFIG_DIR"

if [[ "$FRESH_INSTALL" == "true" ]]; then
  echo "Fresh install enabled: backing up and clearing old data."
  if [[ -d "$APP_DIR/data" ]]; then
    tar -czf "$APP_DIR/backups/data-before-fresh-$(date +%Y%m%d-%H%M%S).tar.gz" -C "$APP_DIR" data || true
    rm -rf "$APP_DIR/data"
  fi
fi

mkdir -p "$APP_DIR/data"
if [[ -d "$APP_DIR/data" ]]; then
  tar -czf "$APP_DIR/backups/data-backup-$(date +%Y%m%d-%H%M%S).tar.gz" -C "$APP_DIR" data || true
fi

step "Copying package files"
if command -v rsync >/dev/null 2>&1; then
  rsync -a --delete --exclude data --exclude backups --exclude 'frontend/node_modules' --exclude 'frontend/dist' --exclude '*.tsbuildinfo' "$SRC_DIR/." "$APP_DIR/"
else
  cp -a "$SRC_DIR/." "$APP_DIR/"
  rm -rf "$APP_DIR/frontend/node_modules" "$APP_DIR/frontend/dist" "$APP_DIR/frontend"/*.tsbuildinfo 2>/dev/null || true
fi

cd "$APP_DIR"
install_cli

if [[ -f data/zxy-panel.json && -s data/zxy-panel.json && "$FRESH_INSTALL" != "true" ]]; then
  ADMIN_USERNAME=${EXISTING_USERNAME:-existing-admin}
  ADMIN_PASSWORD_DISPLAY=${EXISTING_PASSWORD:-existing password unchanged}
fi

cat > .env <<EOF_ENV
API_PORT=${API_PORT}
WEB_PORT=${WEB_PORT}
WEB_BASE_PATH=${WEB_BASE_PATH}
ZXY_JWT_SECRET=${JWT_SECRET}
ZXY_AGENT_SHARED_SECRET=${AGENT_SECRET}
ZXY_ADMIN_USERNAME=${ADMIN_USERNAME}
ZXY_ADMIN_PASSWORD=${ADMIN_PASSWORD}
ZXY_LOCAL_SERVER_IP=${PUBLIC_IP}
ZXY_LOCAL_SERVER_HOST=${LOCAL_HOST}
ZXY_LOCAL_SERVER_NAME=本机服务器
ZXY_LOCAL_SERVER_REGION=Local
ZXY_LOCAL_SERVER_PROVIDER=Self-hosted
ZXY_UPDATE_MANIFEST_URL=${MANIFEST_URL_TO_WRITE}
EOF_ENV
chmod 600 .env

write_panel_info

step "Starting Docker containers"
$COMPOSE up -d --build --force-recreate

wait_api
write_host_nginx
allow_local_firewall
install_local_agent
print_result
