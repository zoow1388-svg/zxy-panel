#!/usr/bin/env bash
set -euo pipefail

VERSION="0.7.8-stable-engineering"
declare -A INSTALL_INPUT=()
for input_key in APP_DIR CONFIG_DIR API_PORT WEB_PORT PANEL_PORT WEB_BASE_PATH \
  AUTO_AGENT INSTALL_XRAY SETUP_XRAY_SERVICE ZXY_FORCE_INSTALL_XRAY \
  ZXY_SKIP_XRAY_INSTALL FRESH_INSTALL ZXY_INSTALL_MODE ZXY_DB_PATH \
  ZXY_ADMIN_USERNAME ZXY_ADMIN_PASSWORD ZXY_JWT_SECRET ZXY_AGENT_SHARED_SECRET \
  ZXY_LOCAL_SERVER_IP ZXY_LOCAL_SERVER_HOST ZXY_LOCAL_SERVER_NAME \
  ZXY_LOCAL_SERVER_REGION ZXY_LOCAL_SERVER_PROVIDER ZXY_UPDATE_MANIFEST_URL \
  PANEL_BASE SERVER_ID AGENT_TOKEN ZXY_PANEL_BASE ZXY_SERVER_ID ZXY_AGENT_TOKEN APPLY_CONFIG ZXY_APPLY_CONFIG \
  XRAY_CONFIG XRAY_TEST_CMD XRAY_RELOAD_CMD ZXY_AGENT_INTERVAL_SECONDS; do
  if [[ -v "$input_key" ]]; then
    INSTALL_INPUT[$input_key]="${!input_key}"
  fi
done
unset input_key
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
ZXY_FORCE_INSTALL_XRAY=${ZXY_FORCE_INSTALL_XRAY-0}
ZXY_SKIP_XRAY_INSTALL=${ZXY_SKIP_XRAY_INSTALL-0}
FRESH_INSTALL=${FRESH_INSTALL:-false}
ZXY_INSTALL_MODE=${ZXY_INSTALL_MODE:-auto}   # auto | fast | docker

export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=a
export NEEDRESTART_SUSPEND=1

APT_UPDATED=false
START_TS="$(date +%s)"
SRC_DIR=""
PUBLIC_IP=""
LOCAL_HOST=""
COMPOSE=""
INSTALL_MODE=""
ADMIN_USERNAME=""
ADMIN_PASSWORD=""
ADMIN_PASSWORD_DISPLAY=""
JWT_SECRET=""
AGENT_SECRET=""
MANIFEST_URL_TO_WRITE=""
DEFAULT_UPDATE_MANIFEST_URL="https://raw.githubusercontent.com/zoow1388-svg/zxy-panel/main/version.json"
DB_PATH=""
PREVIOUS_MODE=""
API_UNIT_FILE=/etc/systemd/system/zxy-panel-api.service
AGENT_UNIT_FILE=/etc/systemd/system/zxy-agent.service
AGENT_ENV_FILE="$CONFIG_DIR/agent.env"
HOST_NGINX_FILE=/etc/nginx/conf.d/zxy-panel.conf
HOST_NGINX_PREVIOUS_HASH=''
OWNED_CONTAINER_IDS=()
COMPOSE_PROJECT=""
LOCAL_SERVER_NAME=""
LOCAL_SERVER_REGION=""
LOCAL_SERVER_PROVIDER=""

fail() {
  printf 'ERROR: %s\n' "$1" >&2
  return 1
}

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
import random
import string
import sys
n = int(sys.argv[1])
rng = random.SystemRandom()
alphabet = string.ascii_letters + string.digits
print(''.join(rng.choice(alphabet) for _ in range(n)), end='')
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
  shuf -i 30000-59999 -n 1
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
  local file="${2:-$APP_DIR/.env}" consumer="${INSTALL_MODE:-fast}"
  [[ "$file" != "$AGENT_ENV_FILE" ]] || consumer=fast
  python3 - "$file" "$1" "$consumer" "${3:-value}" <<'PY_ENV'
import os, re, sys
from pathlib import Path
path, key, consumer, query = sys.argv[1:]
values = {}

def interpolate(value):
    def replace(match):
        if match[0] == '$$': return '$'
        expression = match[1] if match[1] is not None else match[2]
        parts = re.fullmatch(r'([A-Za-z_][A-Za-z0-9_]*)(?:(:?[-+?])(.*))?', expression)
        if not parts or '$' in (parts[3] or ''): raise ValueError('unsupported interpolation')
        name, operator, argument = parts[1], parts[2], parts[3] or ''
        current = os.environ.get(name, values.get(name))
        exists = current is not None and (':' not in (operator or '') or current != '')
        if operator and operator.endswith('?') and not exists: raise ValueError('required variable is missing')
        if operator and operator.endswith('-'): return current if exists else argument
        if operator and operator.endswith('+'): return argument if exists else ''
        return current or ''
    return re.sub(r'\$\$|\$\{([^}]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)', replace, value)

def decode(raw):
    value = raw.strip()
    if value.startswith("'"):
        if not value.endswith("'"): raise ValueError('invalid quoted value')
        value = value[1:-1]
        if consumer == 'docker':
            value = value.replace("\\'", "'")
        elif "'" in value:
            raise ValueError('invalid quoted value')
        return value
    if value.startswith('"'):
        if not value.endswith('"'): raise ValueError('invalid quoted value')
        value = value[1:-1]
        escapes = {'\\': '\\', '"': '"', '$': '$', '`': '`'}
        if consumer == 'docker': escapes.update({'n': '\n', 'r': '\r', 't': '\t'})
        value = re.sub(r'\\(.)', lambda m: escapes.get(m[1], '\\' + m[1]), value)
        return interpolate(value) if consumer == 'docker' else value
    if consumer == 'docker':
        value = re.split(r'\s+#', value, maxsplit=1)[0].rstrip()
        return interpolate(value)
    trailing = re.search(r'\\+$', value)
    if trailing and len(trailing[0]) % 2:
        raise ValueError('multiline environment requires manual review')
    return re.sub(r'\\(.)', lambda m: m[1], value)

try:
    if Path(path).exists():
        for line in Path(path).read_text(encoding='utf-8').splitlines():
            if not line.strip() or line.lstrip().startswith('#'):
                continue
            match = re.fullmatch(r'([A-Za-z_][A-Za-z0-9_]*)=(.*)', line)
            if not match or match[1] in values:
                raise ValueError('invalid or duplicate environment entry')
            value = decode(match[2])
            if '\x00' in value or '\r' in value or '\n' in value:
                raise ValueError('invalid environment value')
            values[match[1]] = value
    print(('present' if key in values else '') if query == 'present' else values.get(key, ''), end='')
except (OSError, UnicodeError, ValueError):
    raise SystemExit('ERROR: existing environment is invalid; no configuration changed.')
PY_ENV
}

configured_value() {
  local input="$1" env_key="$2" fallback="$3" old="" present=""
  if [[ ${INSTALL_INPUT[$input]+present} ]]; then
    printf '%s' "${INSTALL_INPUT[$input]}"
    return
  fi
  if [[ "$FRESH_INSTALL" != true ]]; then
    present=$(env_file_value "$env_key" "$APP_DIR/.env" present) || return
    if [[ "$present" == present ]]; then
      old=$(env_file_value "$env_key") || return
      [[ -n "$old" ]] || { fail "Existing $env_key is empty; no default was substituted."; return 1; }
      printf '%s' "$old"
      return
    fi
  fi
  printf '%s' "${old:-$fallback}"
}

valid_port() {
  [[ "$1" =~ ^[0-9]{1,5}$ ]] && (( 10#$1 >= 1 && 10#$1 <= 65535 ))
}

validate_config_value() {
  local key="$1" value="$2"
  [[ "$value" != *$'\n'* && "$value" != *$'\r'* ]] || return 1
  case "$key" in
    API_PORT|WEB_PORT|PANEL_PORT) valid_port "$value" ;;
    WEB_BASE_PATH) [[ "$value" =~ ^[A-Za-z0-9_-]+$ ]] ;;
    AUTO_AGENT|INSTALL_XRAY|SETUP_XRAY_SERVICE|FRESH_INSTALL) [[ "$value" == true || "$value" == false ]] ;;
    ZXY_FORCE_INSTALL_XRAY|ZXY_SKIP_XRAY_INSTALL) [[ "$value" =~ ^(0|1|true|false)$ ]] ;;
    APP_DIR|CONFIG_DIR|ZXY_DB_PATH)
      [[ "$value" == /* && "$value" != / && "$value" != *'/../'* && "$value" != */.. && "$value" != *['";$`']* && "$value" != *$'\t'* ]] || return 1
      [[ "$key" == ZXY_DB_PATH || "$value" != *['%\']* ]] ;;
    ZXY_UPDATE_MANIFEST_URL) [[ "$value" =~ ^https?://[^[:space:]]+$ ]] ;;
    *) [[ -n "$value" ]] ;;
  esac
}

resolve_existing_config() {
  local key old info_dir old_port='' old_path='' old_user='' old_password='' old_token=''
  [[ ! -L "$APP_DIR/.env" && ( ! -e "$APP_DIR/.env" || -f "$APP_DIR/.env" ) ]] || {
    fail 'Environment must be a regular non-symlink file.'; return 1;
  }
  for key in APP_DIR CONFIG_DIR FRESH_INSTALL; do
    validate_config_value "$key" "${!key}" || { fail "Invalid $key."; return 1; }
  done
  [[ "$APP_DIR" != "$CONFIG_DIR" ]] || { fail 'Application and configuration directories must differ.'; return 1; }
  info_dir=$(panel_info_value INSTALL_DIR)
  [[ -z "$info_dir" || "$info_dir" == "$APP_DIR" ]] || { fail 'Installation directory change requires a separate migration.'; return 1; }
  env_file_value __VALIDATE__ >/dev/null || return
  if [[ "$FRESH_INSTALL" != true ]]; then
    old_port=$(panel_info_value PORT)
    old_path=$(panel_info_value WEB_BASE_PATH)
    old_user=$(panel_info_value USERNAME)
    old_password=$(panel_info_value PASSWORD)
    old_token=$(panel_info_value API_TOKEN)
    AUTO_AGENT=$(configured_value AUTO_AGENT ZXY_AUTO_AGENT "$(panel_info_value AUTO_AGENT)") || return
    AUTO_AGENT=${AUTO_AGENT:-true}
    INSTALL_XRAY=$(configured_value INSTALL_XRAY ZXY_INSTALL_XRAY "$(panel_info_value INSTALL_XRAY)") || return
    INSTALL_XRAY=${INSTALL_XRAY:-true}
    SETUP_XRAY_SERVICE=$(configured_value SETUP_XRAY_SERVICE ZXY_SETUP_XRAY_SERVICE "$(panel_info_value SETUP_XRAY_SERVICE)") || return
    SETUP_XRAY_SERVICE=${SETUP_XRAY_SERVICE:-true}
  fi
  API_PORT=$(configured_value API_PORT API_PORT 8088) || return
  WEB_PORT=$(configured_value WEB_PORT WEB_PORT 5173) || return
  DB_PATH=$(configured_value ZXY_DB_PATH ZXY_DB_PATH "$APP_DIR/data/zxy-panel.json") || return
  PANEL_PORT=$(configured_value PANEL_PORT PANEL_PORT "$old_port") || return
  [[ -n "$PANEL_PORT" ]] || PANEL_PORT=$(random_unused_port)
  WEB_BASE_PATH=$(configured_value WEB_BASE_PATH WEB_BASE_PATH "$old_path") || return
  [[ -n "$WEB_BASE_PATH" ]] || WEB_BASE_PATH=$(random_string 18)
  ADMIN_USERNAME=$(configured_value ZXY_ADMIN_USERNAME ZXY_ADMIN_USERNAME "$old_user") || return
  [[ -n "$ADMIN_USERNAME" ]] || ADMIN_USERNAME=$(random_string 10)
  ADMIN_PASSWORD=$(configured_value ZXY_ADMIN_PASSWORD ZXY_ADMIN_PASSWORD "$old_password") || return
  [[ -n "$ADMIN_PASSWORD" ]] || ADMIN_PASSWORD=$(random_string 12)
  ADMIN_PASSWORD_DISPLAY="$ADMIN_PASSWORD"
  JWT_SECRET=$(configured_value ZXY_JWT_SECRET ZXY_JWT_SECRET '') || return
  [[ -n "$JWT_SECRET" ]] || JWT_SECRET=$(random_string 64)
  AGENT_SECRET=$(configured_value ZXY_AGENT_SHARED_SECRET ZXY_AGENT_SHARED_SECRET "$old_token") || return
  [[ -n "$AGENT_SECRET" ]] || AGENT_SECRET=$(random_string 64)
  MANIFEST_URL_TO_WRITE=$(configured_value ZXY_UPDATE_MANIFEST_URL ZXY_UPDATE_MANIFEST_URL "$DEFAULT_UPDATE_MANIFEST_URL") || return
  PUBLIC_IP=$(configured_value ZXY_LOCAL_SERVER_IP ZXY_LOCAL_SERVER_IP '') || return
  [[ -n "$PUBLIC_IP" ]] || PUBLIC_IP=$(public_ip)
  LOCAL_HOST=$(configured_value ZXY_LOCAL_SERVER_HOST ZXY_LOCAL_SERVER_HOST "$PUBLIC_IP") || return
  LOCAL_SERVER_NAME=$(configured_value ZXY_LOCAL_SERVER_NAME ZXY_LOCAL_SERVER_NAME '本机服务器') || return
  LOCAL_SERVER_REGION=$(configured_value ZXY_LOCAL_SERVER_REGION ZXY_LOCAL_SERVER_REGION Local) || return
  LOCAL_SERVER_PROVIDER=$(configured_value ZXY_LOCAL_SERVER_PROVIDER ZXY_LOCAL_SERVER_PROVIDER Self-hosted) || return
  for key in API_PORT WEB_PORT PANEL_PORT WEB_BASE_PATH AUTO_AGENT INSTALL_XRAY SETUP_XRAY_SERVICE; do
    validate_config_value "$key" "${!key}" || { fail "Invalid $key."; return 1; }
  done
  validate_config_value ZXY_DB_PATH "$DB_PATH" || { fail 'Invalid database path.'; return 1; }
  validate_config_value ZXY_UPDATE_MANIFEST_URL "$MANIFEST_URL_TO_WRITE" || { fail 'Invalid update manifest URL.'; return 1; }
  for key in ADMIN_USERNAME ADMIN_PASSWORD JWT_SECRET AGENT_SECRET PUBLIC_IP LOCAL_HOST LOCAL_SERVER_NAME LOCAL_SERVER_REGION LOCAL_SERVER_PROVIDER; do
    validate_config_value "$key" "${!key}" || { fail "Invalid $key."; return 1; }
  done
  if [[ "$INSTALL_MODE" == docker && "$DB_PATH" != "$APP_DIR/data/zxy-panel.json" ]]; then
    fail 'Docker supports the existing ./data bind mount only; external DB mapping is not supported.'
    return 1
  fi
  if [[ "$FRESH_INSTALL" == true && "$DB_PATH" != "$APP_DIR/data/zxy-panel.json" ]]; then
    fail 'Fresh install cannot clear an external database automatically.'
    return 1
  fi
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1
}

install_base_deps() {
  step "Installing base dependencies"
  local missing=()
  need_cmd curl || missing+=(curl)
  need_cmd python3 || missing+=(python3)
  need_cmd rsync || missing+=(rsync)
  need_cmd ss || missing+=(iproute2)
  need_cmd nginx || missing+=(nginx)
  need_cmd unzip || missing+=(unzip)
  [[ -f /etc/ssl/certs/ca-certificates.crt ]] || missing+=(ca-certificates)

  if [[ ${#missing[@]} -eq 0 ]]; then
    echo "Base dependencies already installed, skip."
    return 0
  fi

  echo "Installing missing packages: ${missing[*]}"
  if command -v apt-get >/dev/null 2>&1; then
    apt_install_missing "${missing[@]}"
  elif command -v yum >/dev/null 2>&1; then
    yum install -y "${missing[@]}"
  else
    echo "ERROR: unsupported system. Please use Debian/Ubuntu/CentOS."
    exit 1
  fi
}

has_fast_assets() {
  [[ -f "$SRC_DIR/bin/zxy-panel-api-linux-amd64" || -f "$SRC_DIR/bin/zxy-panel-api" ]] || return 1
  [[ -f "$SRC_DIR/bin/zxy-agent-linux-amd64" || -f "$SRC_DIR/bin/zxy-agent" ]] || return 1
  [[ -f "$SRC_DIR/frontend/dist/index.html" || -f "$SRC_DIR/web/index.html" ]] || return 1
  return 0
}

selected_install_mode() {
  local mode="${INSTALL_INPUT[ZXY_INSTALL_MODE]-auto}" previous
  case "$mode" in auto|fast|docker) ;; *) fail 'Invalid ZXY_INSTALL_MODE.'; return 1 ;; esac
  previous=$(panel_info_value INSTALL_MODE)
  case "$previous" in ''|fast|docker) ;; *) fail 'Existing install mode is invalid.'; return 1 ;; esac
  if [[ -z "$previous" && ( -f "$APP_DIR/.env" || -f "$APP_DIR/data/zxy-panel.json" ) ]]; then
    local fast=false docker=false
    if api_unit_owned; then fast=true; fi
    if command -v docker >/dev/null 2>&1; then
      inspect_owned_containers || return
      [[ ${#OWNED_CONTAINER_IDS[@]} -eq 0 ]] || docker=true
    fi
    if [[ "$fast" == true && "$docker" == false ]]; then
      previous=fast
    elif [[ "$docker" == true && "$fast" == false ]]; then
      previous=docker
    else
      fail 'Existing mode cannot be confirmed; no automatic migration is allowed.'
      return 1
    fi
  fi
  if [[ "$mode" == auto && -n "$previous" ]]; then mode="$previous"; fi
  if [[ "$mode" == auto ]]; then
    if has_fast_assets; then mode=fast; else mode=docker; fi
  fi
  if [[ -n "$previous" && "$mode" != "$previous" ]]; then
    fail 'Cross-mode installation requires a separately approved migration.'
    return 1
  fi
  if [[ "$mode" == fast ]] && ! has_fast_assets; then
    fail 'Fast assets are missing; existing runtime was not changed.'
    return 1
  fi
  printf '%s\n' "$mode"
}

preflight_inputs() {
  local key value
  for key in APP_DIR CONFIG_DIR API_PORT WEB_PORT PANEL_PORT WEB_BASE_PATH AUTO_AGENT \
    INSTALL_XRAY SETUP_XRAY_SERVICE ZXY_FORCE_INSTALL_XRAY ZXY_SKIP_XRAY_INSTALL FRESH_INSTALL ZXY_DB_PATH ZXY_UPDATE_MANIFEST_URL \
    ZXY_ADMIN_USERNAME ZXY_ADMIN_PASSWORD ZXY_JWT_SECRET ZXY_AGENT_SHARED_SECRET \
    ZXY_LOCAL_SERVER_IP ZXY_LOCAL_SERVER_HOST ZXY_LOCAL_SERVER_NAME ZXY_LOCAL_SERVER_REGION ZXY_LOCAL_SERVER_PROVIDER; do
    if [[ ${INSTALL_INPUT[$key]+present} ]]; then
      value="${INSTALL_INPUT[$key]}"
      validate_config_value "$key" "$value" || { fail "Invalid explicit $key."; return 1; }
    fi
  done
  case "$(uname -m)" in x86_64|amd64) ;; *) fail 'This package supports Linux amd64 only.'; return 1 ;; esac
  [[ "$(uname -s)" == Linux ]] || { fail 'Linux with systemd is required.'; return 1; }
}

unit_owned() {
  local file="$1" directory="$2" environment="$3" binary="$4" alternate="${5:-$4}" unit="${6:-zxy-panel-api}"
  local state fragment dropins execution effective_environment effective_directory
  [[ -f "$file" && ! -L "$file" && -f "$environment" && ! -L "$environment" ]] || return 1
  state=$(systemctl show "$unit" -p LoadState --value) || return
  fragment=$(systemctl show "$unit" -p FragmentPath --value) || return
  dropins=$(systemctl show "$unit" -p DropInPaths --value) || return
  [[ "$state" == loaded && "$fragment" == "$file" && -z "$dropins" ]] || return 1
  execution=$(systemctl show "$unit" -p ExecStart --value) || return
  effective_environment=$(systemctl show "$unit" -p EnvironmentFiles --value) || return
  effective_directory=$(systemctl show "$unit" -p WorkingDirectory --value) || return
  [[ "$effective_environment" == "$environment (ignore_errors=no)" ]] || return 1
  [[ -z "$directory" || "$effective_directory" == "$directory" ]] || return 1
  ZXY_UNIT_EXECUTION="$execution" python3 - "$file" "$directory" "$environment" "$binary" "$alternate" <<'PY_UNIT'
import os, re, shlex, sys
from pathlib import Path
file, directory, environment, binary, alternate = sys.argv[1:]
try:
    section = ''
    values = {}
    for line in Path(file).read_text(encoding='utf-8').splitlines():
        line = line.strip()
        if line.startswith('['):
            section = line
        elif section == '[Service]' and '=' in line and not line.startswith(('#', ';')):
            key, value = line.split('=', 1)
            values.setdefault(key, []).append(shlex.split(value))
    allowed = {'Type', 'WorkingDirectory', 'EnvironmentFile', 'ExecStart', 'Restart',
               'RestartSec', 'LimitNOFILE'}
    if set(values) - allowed: raise ValueError()
    valid = values.get('EnvironmentFile') == [[environment]]
    valid = valid and values.get('ExecStart') in ([[binary]], [[alternate]])
    if directory:
        valid = valid and values.get('WorkingDirectory') == [[directory]]
    execution = os.environ['ZXY_UNIT_EXECUTION']
    valid = valid and execution.count('path=') == 1
    match = re.search(r'path=(.*?) ; argv\[\]=(.*?) ;', execution)
    valid = valid and bool(match) and match[1] in (binary, alternate) and match[2] == match[1]
    raise SystemExit(0 if valid else 1)
except (OSError, UnicodeError, ValueError):
    raise SystemExit(1)
PY_UNIT
}

api_unit_owned() {
  unit_owned "$API_UNIT_FILE" "$APP_DIR" "$APP_DIR/.env" \
    "$APP_DIR/bin/zxy-panel-api-linux-amd64" "$APP_DIR/bin/zxy-panel-api"
}

agent_unit_owned() {
  unit_owned "$AGENT_UNIT_FILE" '' "$AGENT_ENV_FILE" /usr/local/bin/zxy-agent /usr/local/bin/zxy-agent zxy-agent
}

inspect_owned_containers() {
  OWNED_CONTAINER_IDS=()
  COMPOSE_PROJECT=""
  local names name raw record container_id project context
  [[ -z "${DOCKER_HOST-}" || "${DOCKER_HOST-}" == unix:///var/run/docker.sock ]] || {
    fail 'Remote Docker endpoints are outside this local installation.'; return 1;
  }
  context=$(docker context show) || { fail 'Cannot inspect Docker context.'; return 1; }
  [[ "$context" == default ]] || { fail 'Only the local default Docker context is supported.'; return 1; }
  names=$(docker ps -a --format '{{.Names}}') || { fail 'Cannot inspect Docker ownership.'; return 1; }
  for name in zxy-panel-api zxy-panel-frontend; do
    if ! grep -Fxq "$name" <<< "$names"; then continue; fi
    raw=$(docker inspect "$name") || { fail 'Cannot inspect existing container.'; return 1; }
    record=$(printf '%s' "$raw" | python3 -c '
import json, os, sys
try:
    items = json.load(sys.stdin)
    if not isinstance(items, list) or len(items) != 1: raise ValueError()
    item = items[0]
    labels = item.get("Config", {}).get("Labels") or {}
    app, service = sys.argv[1:]
    valid = labels.get("com.docker.compose.service") == service
    valid = valid and labels.get("com.docker.compose.project.working_dir") == app
    valid = valid and labels.get("com.docker.compose.project.config_files") == app + "/docker-compose.yml"
    valid = valid and bool(labels.get("com.docker.compose.project"))
    if service == "zxy-panel-api":
        valid = valid and any(m.get("Type") == "bind" and m.get("Source") == app + "/data" and m.get("Destination") == "/app/data" for m in item.get("Mounts", []))
    if not valid or not item.get("Id"): raise ValueError()
    project = labels["com.docker.compose.project"]
    if not isinstance(project, str) or not project or "|" in project or "\n" in project: raise ValueError()
    print(item["Id"] + "|" + project)
except (ValueError, TypeError, AttributeError):
    raise SystemExit(1)
' "$APP_DIR" "$name") || { fail 'Container ownership is ambiguous; nothing was removed.'; return 1; }
    container_id="${record%%|*}"
    project="${record#*|}"
    [[ -z "$COMPOSE_PROJECT" || "$COMPOSE_PROJECT" == "$project" ]] || {
      fail 'Existing containers belong to different Compose projects.'; return 1;
    }
    COMPOSE_PROJECT="$project"
    OWNED_CONTAINER_IDS+=("$container_id")
  done
  local saved_project candidate key value
  saved_project=$(panel_info_value COMPOSE_PROJECT)
  [[ -z "$saved_project" || -z "$COMPOSE_PROJECT" || "$saved_project" == "$COMPOSE_PROJECT" ]] || {
    fail 'Compose project metadata conflicts with existing containers.'; return 1;
  }
  COMPOSE_PROJECT="${COMPOSE_PROJECT:-${saved_project:-${APP_DIR##*/}}}"
  [[ "$COMPOSE_PROJECT" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || { fail 'Compose project is invalid.'; return 1; }
  for key in COMPOSE_FILE COMPOSE_PROJECT_NAME DOCKER_HOST DOCKER_CONTEXT; do
    value="${!key-}"
    candidate=$(env_file_value "$key") || return
    for candidate in "$value" "$candidate"; do
      case "$key" in
        COMPOSE_FILE) [[ -z "$candidate" || "$candidate" == "$APP_DIR/docker-compose.yml" ]] ;;
        COMPOSE_PROJECT_NAME) [[ -z "$candidate" || "$candidate" == "$COMPOSE_PROJECT" ]] ;;
        DOCKER_CONTEXT) [[ -z "$candidate" || "$candidate" == default ]] ;;
        DOCKER_HOST) [[ -z "$candidate" || "$candidate" == unix:///var/run/docker.sock ]] ;;
      esac || { fail "Conflicting $key was refused before any service operation."; return 1; }
    done
  done
}

run_panel_compose() {
  [[ -n "$COMPOSE_PROJECT" ]] || { fail 'Compose ownership was not checked.'; return 1; }
  local -a command
  case "$COMPOSE" in
    'docker compose') command=(docker compose) ;;
    docker-compose) command=(docker-compose) ;;
    *) fail 'Docker Compose is unavailable.'; return 1 ;;
  esac
  COMPOSE_REMOVE_ORPHANS=false COMPOSE_IGNORE_ORPHANS=false COMPOSE_PROFILES='' \
    "${command[@]}" -p "$COMPOSE_PROJECT" -f "$APP_DIR/docker-compose.yml" \
    --project-directory "$APP_DIR" --env-file "$APP_DIR/.env" "$@"
}

preflight_runtime_ownership() {
  local state
  if [[ -f "$AGENT_ENV_FILE" ]]; then
    local previous_id previous_token previous_base previous_port requested_base
    previous_id=$(env_file_value ZXY_SERVER_ID "$AGENT_ENV_FILE") || return
    previous_token=$(env_file_value ZXY_AGENT_TOKEN "$AGENT_ENV_FILE") || return
    if [[ "$FRESH_INSTALL" == true && ( -n "$previous_id" || -n "$previous_token" ) ]]; then
      fail 'Fresh install cannot retain an existing Agent identity while replacing its database.'; return 1
    fi
    previous_base=$(env_file_value ZXY_PANEL_BASE "$AGENT_ENV_FILE") || return
    previous_port=$(env_file_value API_PORT) || return
    previous_port="${previous_port:-8088}"
    requested_base="${INSTALL_INPUT[PANEL_BASE]-${INSTALL_INPUT[ZXY_PANEL_BASE]-}}"
    ZXY_OLD_AGENT_BASE="$previous_base" ZXY_REQUESTED_AGENT_BASE="$requested_base" \
      python3 - "$previous_port" "$API_PORT" "$AUTO_AGENT" <<'PY_ENDPOINT' || return
import os, sys
from urllib.parse import urlsplit
try:
    previous = urlsplit(os.environ['ZXY_OLD_AGENT_BASE'])
    if previous.hostname in ('127.0.0.1', 'localhost', '::1') and (previous.port or (443 if previous.scheme == 'https' else 80)) == int(sys.argv[1]) and sys.argv[1] != sys.argv[2]:
        requested = urlsplit(os.environ['ZXY_REQUESTED_AGENT_BASE'])
        if sys.argv[3] != 'true' or requested.scheme != 'http' or requested.hostname not in ('127.0.0.1', 'localhost', '::1') or requested.port != int(sys.argv[2]) or requested.path not in ('', '/') or requested.query or requested.fragment or requested.username or requested.password:
            raise ValueError()
except (ValueError, TypeError):
    raise SystemExit('ERROR: API port change conflicts with the retained local Agent endpoint; provide a matching explicit PANEL_BASE with AUTO_AGENT=true.')
PY_ENDPOINT
  fi
  if [[ "$AUTO_AGENT" == true && ( ${INSTALL_INPUT[SERVER_ID]+present} ||
        ${INSTALL_INPUT[AGENT_TOKEN]+present} || ${INSTALL_INPUT[ZXY_SERVER_ID]+present} ||
        ${INSTALL_INPUT[ZXY_AGENT_TOKEN]+present} ) ]]; then
    local requested_id="${INSTALL_INPUT[SERVER_ID]-${INSTALL_INPUT[ZXY_SERVER_ID]-}}"
    local requested_token="${INSTALL_INPUT[AGENT_TOKEN]-${INSTALL_INPUT[ZXY_AGENT_TOKEN]-}}"
    [[ -n "$requested_id" && -n "$requested_token" ]] || {
      fail 'Explicit Agent identity must include both a nonempty ID and Token.'; return 1;
    }
    if [[ "$FRESH_INSTALL" == true && -f "$DB_PATH" ]]; then
      fail 'Fresh install cannot bind an explicit identity from the database it would replace.'; return 1
    fi
  fi
  state=$(systemctl show zxy-panel-api -p LoadState --value) || { fail 'Cannot inspect API unit.'; return 1; }
  if [[ "$state" != not-found || -e "$API_UNIT_FILE" || -L "$API_UNIT_FILE" ]] && ! api_unit_owned; then
    fail 'API unit does not match this installation.'; return 1
  fi
  if [[ "$AUTO_AGENT" == true ]]; then
    state=$(systemctl show zxy-agent -p LoadState --value) || { fail 'Cannot inspect Agent unit.'; return 1; }
    if [[ "$state" != not-found || -e "$AGENT_UNIT_FILE" || -L "$AGENT_UNIT_FILE" ]] && ! agent_unit_owned; then
      fail 'Agent unit does not match this installation.'; return 1
    fi
  fi
  if [[ "$INSTALL_MODE" == docker ]]; then
    inspect_owned_containers || return
  fi
  if [[ "$AUTO_AGENT" == true ]]; then
    local script="$SRC_DIR/deploy/agent-install.sh"
    APP_DIR="$APP_DIR" CONFIG_DIR="$CONFIG_DIR" ZXY_DB_PATH="$DB_PATH" INSTALL_XRAY="$INSTALL_XRAY" \
      SETUP_XRAY_SERVICE="$SETUP_XRAY_SERVICE" bash "$script" --preflight || return
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


installer_backup_existing() {
  step "Pre-install backup"
  local has_existing="false"
  for item in \
    "$APP_DIR/data/zxy-panel.json" \
    "$APP_DIR/.env" \
    "$INFO_FILE" \
    "/etc/zxy-panel/xray/config.json" \
    "/etc/nginx/conf.d/zxy-panel.conf" \
    "/etc/systemd/system/zxy-panel-api.service" \
    "/etc/systemd/system/zxy-agent.service" \
    "/etc/systemd/system/xray.service.d/99-zxy-panel.conf" \
    "/etc/sysctl.d/99-zxy-bbr.conf" \
    "/etc/zxy-panel/bbr.disabled"; do
    if [[ -e "$item" ]]; then
      has_existing="true"
      break
    fi
  done

  if [[ "$has_existing" != "true" ]]; then
    echo "No existing ZXY Panel data/config found, skip pre-install backup."
    return 0
  fi

  local backup_dir ts tmp root backup item copied=0
  backup_dir="$APP_DIR/backups"
  ts="$(date +%Y%m%d-%H%M%S)"
  backup="$backup_dir/zxy-panel-backup-${ts}.tar.gz"
  mkdir -p "$backup_dir"
  tmp="$(mktemp -d)"
  root="$tmp/root"
  mkdir -p "$root"

  cat > "$root/zxy-backup-meta.txt" <<EOF_META
ZXY Panel backup
created_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
reason=pre-install
source_host=$(hostname 2>/dev/null || echo unknown)
from_version=$(panel_info_value VERSION || true)
to_version=${VERSION}
install_mode=$(panel_info_value INSTALL_MODE || true)
EOF_META

  local items=(
    "$APP_DIR/data/zxy-panel.json"
    "$APP_DIR/.env"
    "$INFO_FILE"
    "/etc/zxy-panel/xray/config.json"
    "/etc/nginx/conf.d/zxy-panel.conf"
    "/etc/systemd/system/zxy-panel-api.service"
    "/etc/systemd/system/zxy-agent.service"
    "/etc/systemd/system/xray.service.d/99-zxy-panel.conf"
    "/etc/sysctl.d/99-zxy-bbr.conf"
    "/etc/zxy-panel/bbr.disabled"
  )

  for item in "${items[@]}"; do
    if [[ -e "$item" ]]; then
      mkdir -p "$root$(dirname "$item")"
      cp -a "$item" "$root$item"
      copied=$((copied+1))
    fi
  done

  if [[ "$copied" -eq 0 ]]; then
    rm -rf "$tmp"
    echo "No backup items copied, skip."
    return 0
  fi

  tar -C "$root" -czf "$backup" .
  chmod 600 "$backup" 2>/dev/null || true
  rm -rf "$tmp"
  echo "Pre-install backup created: $backup"
}

cleanup_old_runtime() {
  step "Cleaning old ZXY Panel runtime"
  if api_unit_owned; then
    systemctl stop zxy-panel-api || return
    if [[ "$INSTALL_MODE" == docker ]]; then systemctl disable zxy-panel-api || return; fi
  fi
  if [[ "$AUTO_AGENT" == true ]] && agent_unit_owned; then
    systemctl stop zxy-agent || return
  fi
  if [[ "$INSTALL_MODE" == docker && ${#OWNED_CONTAINER_IDS[@]} -gt 0 ]]; then
    docker stop "${OWNED_CONTAINER_IDS[@]}" || return
  fi
}

write_panel_info() {
  mkdir -p "$CONFIG_DIR"
  cat > "$INFO_FILE" <<EOF_INFO
USERNAME=${ADMIN_USERNAME}
PASSWORD=${ADMIN_PASSWORD_DISPLAY}
PORT=${PANEL_PORT}
WEB_BASE_PATH=${WEB_BASE_PATH}
WebBasePath=${WEB_BASE_PATH}
DATABASE=JSON (${DB_PATH})
Database=JSON (${DB_PATH})
ACCESS_URL=http://${PUBLIC_IP}:${PANEL_PORT}/${WEB_BASE_PATH}/
Access URL=http://${PUBLIC_IP}:${PANEL_PORT}/${WEB_BASE_PATH}/
API_TOKEN=${AGENT_SECRET}
API Token=${AGENT_SECRET}
INSTALL_DIR=${APP_DIR}
CONFIG_DIR=${CONFIG_DIR}
VERSION=${VERSION}
INSTALL_MODE=${INSTALL_MODE}
COMPOSE_PROJECT=${COMPOSE_PROJECT}
AUTO_AGENT=${AUTO_AGENT}
INSTALL_XRAY=${INSTALL_XRAY}
SETUP_XRAY_SERVICE=${SETUP_XRAY_SERVICE}
EOF_INFO
  chmod 600 "$INFO_FILE"
}

nginx_file_fingerprint() {
  python3 - "$1" <<'PY_NGINX_FINGERPRINT'
import hashlib, json, stat, sys
from pathlib import Path
try:
    p = Path(sys.argv[1])
    if p.is_symlink(): raise ValueError()
    if not p.exists():
        print('missing')
    else:
        before = p.stat()
        if not stat.S_ISREG(before.st_mode): raise ValueError()
        data = p.read_bytes()
        after = p.stat()
        stable = ('st_ino', 'st_dev', 'st_size', 'st_mtime_ns', 'st_ctime_ns', 'st_mode', 'st_uid', 'st_gid')
        if any(getattr(before, key) != getattr(after, key) for key in stable): raise ValueError()
        print(json.dumps(dict(sha256=hashlib.sha256(data).hexdigest(), size=after.st_size,
            mode=after.st_mode, uid=after.st_uid, gid=after.st_gid,
            mtime_ns=after.st_mtime_ns, inode=after.st_ino, device=after.st_dev), sort_keys=True))
except (OSError, ValueError):
    raise SystemExit('ERROR: Nginx file could not be read consistently.')
PY_NGINX_FINGERPRINT
}

preflight_host_nginx() {
  [[ ! -L "$HOST_NGINX_FILE" ]] || { fail 'Nginx configuration must not be a symlink.'; return 1; }
  if [[ ! -e "$HOST_NGINX_FILE" ]]; then HOST_NGINX_PREVIOUS_HASH=missing; return; fi
  [[ -f "$HOST_NGINX_FILE" && -f "$INFO_FILE" && ! -L "$INFO_FILE" ]] || {
    fail 'Existing Nginx configuration ownership cannot be confirmed.'; return 1;
  }
  local old_port old_base old_api old_web
  old_port=$(panel_info_value PORT)
  old_base=$(panel_info_value WEB_BASE_PATH)
  valid_port "$old_port" && validate_config_value WEB_BASE_PATH "$old_base" || {
    fail 'Existing Nginx installation metadata is invalid.'; return 1;
  }
  old_api=$(env_file_value API_PORT) || return
  old_web=$(env_file_value WEB_PORT) || return
  HOST_NGINX_PREVIOUS_HASH=$(python3 - "$HOST_NGINX_FILE" "$APP_DIR" "$old_port" "$old_base" "${old_api:-8088}" "${old_web:-5173}" <<'PY_NGINX_OWNER'
import hashlib, json, shlex, sys
from pathlib import Path
from urllib.parse import urlsplit
try:
    p = Path(sys.argv[1])
    before = p.stat()
    source = p.read_bytes()
    after = p.stat()
    stable = ('st_ino', 'st_dev', 'st_size', 'st_mtime_ns', 'st_ctime_ns', 'st_mode', 'st_uid', 'st_gid')
    if any(getattr(before, key) != getattr(after, key) for key in stable) or p.is_symlink(): raise ValueError()
    app, port, base, api, web = sys.argv[2:]
    locations = {'/', '/index.html', '/api/', '/sub/', '/s/', '/assets/', '/' + base + '/'}
    locations.update('/' + base + '/' + segment + '/' for segment in ('api', 'sub', 's', 'assets'))
    locations.update('^/' + base + '/' + segment + '/(.*)$' for segment in ('api', 'sub', 's', 'assets'))
    safe = {'client_max_body_size', 'index', 'proxy_http_version', 'proxy_set_header', 'add_header'}
    servers, listens, stack, blocks = 0, [], [], {}
    current = None
    for line in source.decode('utf-8').splitlines():
        words = shlex.split(line.strip(), comments=True)
        if not words: continue
        if words == ['server', '{']:
            servers += 1; stack.append('server'); current = ('server',); blocks[current] = {}; continue
        if words == ['}']:
            if not stack: raise ValueError()
            stack.pop(); current = ('server',) if stack else None; continue
        if words[0] == 'location' and words[-1:] == ['{']:
            if stack != ['server'] or words[-2] not in locations: raise ValueError()
            current = tuple(words[1:-1])
            if current in blocks: raise ValueError()
            blocks[current] = {}; stack.append('location'); continue
        if not stack or not words[-1].endswith(';'): raise ValueError()
        words[-1] = words[-1][:-1]
        key, values = words[0], words[1:]
        if key != 'proxy_set_header' and key != 'add_header' and key in blocks[current]: raise ValueError()
        blocks[current][key] = values
        if key == 'listen': listens.append(values)
        elif key == 'server_name':
            if values != ['_']: raise ValueError()
        elif key == 'root':
            if values not in ([app + '/frontend/dist'], [app + '/web']): raise ValueError()
        elif key == 'proxy_pass':
            if len(values) != 1: raise ValueError()
            uri = urlsplit(values[0])
            routes = {'', '/' + base + '/'}
            routes.update('/' + segment + '/' + suffix for segment in ('api', 'sub', 's') for suffix in ('', '$1', '$1$is_args$args'))
            if uri.scheme != 'http' or uri.hostname != '127.0.0.1' or str(uri.port) not in (api, web) or uri.path not in routes or uri.query or uri.fragment: raise ValueError()
        elif key == 'return':
            if values != ['302', '/' + base + '/']: raise ValueError()
        elif key == 'try_files':
            if not values or any(v not in ('$uri', '$uri/', '/index.html', '/assets/$1', '=404') for v in values): raise ValueError()
        elif key not in safe: raise ValueError()
    if stack or servers != 1 or listens != [[port]]: raise ValueError()
    server = blocks[('server',)]
    if server.get('server_name') != ['_'] or server.get('client_max_body_size') != ['20m']: raise ValueError()
    if blocks.get(('=', '/'), {}).get('return') != ['302', '/' + base + '/']: raise ValueError()
    if 'root' in server:
        required = {('server',), ('=', '/'), ('=', '/index.html'), ('^~', '/assets/'), ('/',), ('/' + base + '/',), ('~', '^/' + base + '/assets/(.*)$')}
        if server.get('index') != ['index.html']: raise ValueError()
        if blocks.get(('=', '/index.html'), {}).get('try_files') != ['/index.html', '=404']: raise ValueError()
        if blocks.get(('^~', '/assets/'), {}).get('try_files') != ['$uri', '=404']: raise ValueError()
        if blocks.get(('~', '^/' + base + '/assets/(.*)$'), {}).get('try_files') != ['/assets/$1', '=404']: raise ValueError()
        for path in ('/', '/' + base + '/'):
            if blocks.get((path,), {}).get('try_files') != ['$uri', '$uri/', '/index.html']: raise ValueError()
        for segment in ('api', 'sub', 's'):
            root = ('^~', '/' + segment + '/')
            if blocks.get(root, {}).get('proxy_pass') != ['http://127.0.0.1:' + api + '/' + segment + '/']: raise ValueError()
            required.add(root)
            literal = ('^~', '/' + base + '/' + segment + '/')
            regex = ('~', '^/' + base + '/' + segment + '/(.*)$')
            selected = literal if literal in blocks else regex
            expected = 'http://127.0.0.1:' + api + '/' + segment + '/' + ('' if selected == literal else '$1')
            if blocks.get(selected, {}).get('proxy_pass') not in ([expected], [expected + '$is_args$args'] if selected == regex else [expected]): raise ValueError()
            required.add(selected)
        if set(blocks) != required: raise ValueError()
    else:
        allowed = {('server',), ('=', '/'), ('/' + base + '/',)}
        if blocks.get(('/' + base + '/',), {}).get('proxy_pass') != ['http://127.0.0.1:' + web + '/' + base + '/']: raise ValueError()
        if ('/',) in blocks:
            allowed.add(('/',))
            if blocks[('/',)].get('proxy_pass') != ['http://127.0.0.1:' + web]: raise ValueError()
        if set(blocks) != allowed: raise ValueError()
    print(json.dumps(dict(sha256=hashlib.sha256(source).hexdigest(), size=after.st_size,
        mode=after.st_mode, uid=after.st_uid, gid=after.st_gid,
        mtime_ns=after.st_mtime_ns, inode=after.st_ino, device=after.st_dev), sort_keys=True))
except (OSError, UnicodeError, ValueError, IndexError):
    raise SystemExit('ERROR: existing Nginx file is outside the confirmed panel scope; it was not replaced.')
PY_NGINX_OWNER
  ) || return
}

publish_host_nginx() {
  local candidate="$1" checkdir previous=missing active=false enabled status state checked published current
  local enable_attempted=false start_attempted=false
  [[ -n "$HOST_NGINX_PREVIOUS_HASH" && -f "$candidate" && ! -L "$candidate" ]] || {
    fail 'Nginx publication requires completed ownership preflight.'; return 1;
  }
  previous=$(nginx_file_fingerprint "$HOST_NGINX_FILE") || return
  [[ "$previous" == "$HOST_NGINX_PREVIOUS_HASH" ]] || { fail 'Nginx configuration changed after preflight.'; return 1; }
  checkdir=$(mktemp -d "$(dirname "$HOST_NGINX_FILE")/.zxy-nginx-check-XXXXXX") || return
  if [[ "$previous" != missing ]]; then
    cp -p "$HOST_NGINX_FILE" "$checkdir/previous.conf" || { rm -rf "$checkdir"; return 1; }
  fi
  checked=$(nginx_file_fingerprint "$candidate") || { rm -rf "$checkdir"; return 1; }
  if ! printf 'error_log stderr;\nevents {}\nhttp {\ninclude /etc/nginx/mime.types;\n' > "$checkdir/check.conf" ||
     ! cat "$candidate" >> "$checkdir/check.conf" || ! printf '\n}\n' >> "$checkdir/check.conf"; then
    rm -rf "$checkdir"; fail 'Nginx candidate could not be read; previous configuration retained.'; return 1
  fi
  if ! nginx -t -c "$checkdir/check.conf"; then
    rm -rf "$checkdir"; fail 'Nginx candidate validation failed; previous configuration retained.'; return 1
  fi
  if systemctl is-active --quiet nginx; then active=true; fi
  enabled=$(systemctl is-enabled nginx 2>/dev/null) || enabled="${enabled:-unknown}"
  current=$(nginx_file_fingerprint "$candidate") || { rm -rf "$checkdir"; return 1; }
  [[ "$current" == "$checked" ]] || { rm -rf "$checkdir"; fail 'Nginx candidate changed during validation.'; return 1; }
  if [[ "$previous" != missing ]]; then
    current=$(nginx_file_fingerprint "$checkdir/previous.conf") || { rm -rf "$checkdir"; return 1; }
    if ! python3 - "$previous" "$current" <<'PY_NGINX_BACKUP'
import json, sys
original, backup = map(json.loads, sys.argv[1:])
for value in (original, backup):
    value.pop('inode'); value.pop('device')
if original != backup: raise SystemExit(1)
PY_NGINX_BACKUP
    then rm -rf "$checkdir"; fail 'Nginx recovery copy does not match preflight.'; return 1; fi
  fi
  chmod 644 "$candidate" || { rm -rf "$checkdir"; return 1; }
  current=$(nginx_file_fingerprint "$HOST_NGINX_FILE") || { rm -rf "$checkdir"; return 1; }
  [[ "$current" == "$previous" ]] || { rm -rf "$checkdir"; fail 'Nginx configuration changed during validation.'; return 1; }
  mv "$candidate" "$HOST_NGINX_FILE" || { rm -rf "$checkdir"; return 1; }
  if ! published=$(nginx_file_fingerprint "$HOST_NGINX_FILE") || ! python3 - "$checked" "$published" <<'PY_NGINX_PUBLISHED'
import json, sys
if json.loads(sys.argv[1])['sha256'] != json.loads(sys.argv[2])['sha256']: raise SystemExit(1)
PY_NGINX_PUBLISHED
  then
    state=$(systemctl is-active nginx 2>/dev/null) || state="${state:-unknown}"
    printf 'ERROR: published Nginx file could not be confirmed; recovery directory=%s, actual service state=%s.\n' "$checkdir" "$state" >&2
    return 2
  fi
  status=0
  if ! nginx -t; then status=1
  elif [[ "$active" == true ]]; then
    systemctl reload nginx || status=1
  elif [[ "$enabled" == enabled ]]; then
    start_attempted=true
    systemctl start nginx || status=1
  elif [[ "$enabled" == disabled ]]; then
    enable_attempted=true
    if systemctl enable nginx; then
      start_attempted=true
      systemctl start nginx || status=1
    else status=1
    fi
  else status=1
  fi
  if [[ "$status" == 0 ]] && ! systemctl is-active --quiet nginx; then status=1; fi
  if [[ "$status" != 0 ]]; then
    current=$(nginx_file_fingerprint "$HOST_NGINX_FILE") || current=unreadable
    if [[ "$published" == unreadable || "$current" != "$published" ]]; then
      status=2
    elif [[ "$previous" == missing ]]; then
      rm -f "$HOST_NGINX_FILE" || status=2
    else
      cp -p "$checkdir/previous.conf" "$checkdir/restore.conf" && mv "$checkdir/restore.conf" "$HOST_NGINX_FILE" || status=2
    fi
    if [[ "$status" == 2 ]]; then
      state=$(systemctl is-active nginx 2>/dev/null) || state="${state:-unknown}"
      printf 'ERROR: Nginx file rollback incomplete; recovery directory=%s, actual service state=%s.\n' "$checkdir" "$state" >&2
      return 2
    elif [[ "$active" == true ]]; then
      nginx -t && systemctl reload nginx || status=2
    else
      if [[ "$start_attempted" == true ]]; then systemctl stop nginx || status=2; fi
      if [[ "$enable_attempted" == true ]]; then systemctl disable nginx || status=2; fi
    fi
    state=$(systemctl is-active nginx 2>/dev/null) || state="${state:-unknown}"
    printf 'ERROR: Nginx publication failed; rollback status=%s, actual service state=%s.\n' "$status" "$state" >&2
    if [[ "$status" == 2 ]]; then
      printf 'ERROR: Nginx service recovery incomplete; recovery directory=%s.\n' "$checkdir" >&2
    else rm -rf "$checkdir"
    fi
    return "$status"
  fi
  rm -rf "$checkdir"
  echo 'Nginx configuration validated and service active.'
}

write_host_nginx_docker() {
  step "Writing host Nginx reverse proxy"
  local candidate
  mkdir -p "$(dirname "$HOST_NGINX_FILE")" || return
  candidate=$(mktemp "$(dirname "$HOST_NGINX_FILE")/.zxy-panel-candidate-XXXXXX") || return
  cat > "$candidate" <<EOF_NGINX || { rm -f "$candidate"; return 1; }
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

    location / {
        proxy_pass http://127.0.0.1:${WEB_PORT};
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
EOF_NGINX
  local status=0
  publish_host_nginx "$candidate" || status=$?
  rm -f "$candidate" || { fail 'Nginx temporary file cleanup failed.'; return 1; }
  return "$status"
}

write_host_nginx_fast() {
  step "Writing host Nginx static panel"
  local web_root="$APP_DIR/frontend/dist"
  if [[ ! -f "$web_root/index.html" && -f "$APP_DIR/web/index.html" ]]; then
    web_root="$APP_DIR/web"
  fi
  local candidate
  mkdir -p "$(dirname "$HOST_NGINX_FILE")" || return
  candidate=$(mktemp "$(dirname "$HOST_NGINX_FILE")/.zxy-panel-candidate-XXXXXX") || return
  cat > "$candidate" <<EOF_NGINX || { rm -f "$candidate"; return 1; }
server {
    listen ${PANEL_PORT};
    server_name _;

    root "${web_root}";
    index index.html;
    client_max_body_size 20m;

    location = / {
        return 302 /${WEB_BASE_PATH}/;
    }

    location = /index.html {
        add_header Cache-Control "no-cache, no-store, must-revalidate" always;
        add_header Pragma "no-cache" always;
        add_header Expires "0" always;
        try_files /index.html =404;
    }

    # Fast mode must support both root API paths used by frontend fetch()
    # and WebBasePath-prefixed paths used by subscriptions/short links.
    location ^~ /api/ {
        proxy_pass http://127.0.0.1:${API_PORT}/api/;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    location ^~ /sub/ {
        proxy_pass http://127.0.0.1:${API_PORT}/sub/;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    location ^~ /s/ {
        proxy_pass http://127.0.0.1:${API_PORT}/s/;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    location ^~ /${WEB_BASE_PATH}/api/ {
        proxy_pass http://127.0.0.1:${API_PORT}/api/;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    location ^~ /${WEB_BASE_PATH}/sub/ {
        proxy_pass http://127.0.0.1:${API_PORT}/sub/;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    location ^~ /${WEB_BASE_PATH}/s/ {
        proxy_pass http://127.0.0.1:${API_PORT}/s/;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    location ~ ^/${WEB_BASE_PATH}/assets/(.*)\$ {
        add_header Cache-Control "public, max-age=31536000, immutable" always;
        try_files /assets/\$1 =404;
    }

    location ^~ /assets/ {
        add_header Cache-Control "public, max-age=31536000, immutable" always;
        try_files \$uri =404;
    }

    location /${WEB_BASE_PATH}/ {
        try_files \$uri \$uri/ /index.html;
    }

    location / {
        try_files \$uri \$uri/ /index.html;
    }
}
EOF_NGINX
  local status=0
  publish_host_nginx "$candidate" || status=$?
  rm -f "$candidate" || { fail 'Nginx temporary file cleanup failed.'; return 1; }
  return "$status"
}

install_cli() {
  step "Installing zxy-panel CLI"
  install -m 0755 "$APP_DIR/scripts/zxy-panel" /usr/local/bin/zxy-panel
}

install_netopt() {
  step "Installing host network optimization helper"
  if [[ ! -f "$APP_DIR/scripts/zxy-netopt" ]]; then
    echo "WARNING: zxy-netopt source is missing; BBR control will be unavailable."
    return 0
  fi
  install -m 0755 "$APP_DIR/scripts/zxy-netopt" /usr/local/bin/zxy-netopt
}

enable_default_bbr() {
  if [[ "${ZXY_BBR_AUTO_ENABLE:-true}" != "true" ]]; then
    echo "BBR auto-enable is disabled by ZXY_BBR_AUTO_ENABLE."
    return 0
  fi
  if ! command -v zxy-netopt >/dev/null 2>&1; then
    echo "BBR helper is unavailable; skip without affecting panel installation."
    return 0
  fi

  step "Checking and enabling BBR network optimization"
  local result
  if result="$(ZXY_BBR_AUTO_ENABLE=true zxy-netopt --json enable-bbr 2>&1)"; then
    printf '%s\n' "$result"
    if printf '%s' "$result" | grep -q '"enabled": true'; then
      echo "BBR network optimization enabled."
    else
      echo "BBR is unsupported or remains disabled; panel installation continues normally."
    fi
  else
    printf '%s\n' "$result"
    echo "BBR enable attempt failed; panel installation continues normally."
  fi
}

allow_local_firewall() {
  if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
    ufw allow "${PANEL_PORT}/tcp" || true
  fi
}


post_install_self_check() {
  step "Post-install self check"
  if command -v zxy-panel >/dev/null 2>&1; then
    zxy-panel doctor || true
  else
    echo "zxy-panel CLI not found, skip doctor check."
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
  echo "InstallMode: ${INSTALL_MODE}"
  echo "Database:    JSON (${DB_PATH})"
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
  echo "  zxy-panel doctor"
  echo "  zxy-panel backup"
  echo "  zxy-panel backup-list"
  echo "  zxy-panel restore [backup-file]"
  echo "  zxy-panel reset-password"
  echo
  echo "Important: open TCP port ${PANEL_PORT} in your cloud firewall/security group for panel access."
  echo "Important: also open every node inbound port you create, otherwise client tools cannot connect."
  if command -v zxy-netopt >/dev/null 2>&1; then
    echo
    echo "BBR network optimization:"
    zxy-netopt bbr-status || true
  fi
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
  if [[ "${INSTALL_MODE}" == "fast" ]]; then
    echo "Check logs: journalctl -u zxy-panel-api -n 120 --no-pager"
  else
    echo "Check logs: cd ${APP_DIR} && ${COMPOSE} logs --tail=120 zxy-panel-api"
  fi
  exit 1
}

install_local_agent() {
  if [[ "$AUTO_AGENT" != "true" ]]; then
    return 0
  fi
  step "Installing local Agent automatically"
  local previous_id previous_token selected_id selected_token
  previous_id=$(env_file_value ZXY_SERVER_ID "$AGENT_ENV_FILE") || return
  previous_token=$(env_file_value ZXY_AGENT_TOKEN "$AGENT_ENV_FILE") || return
  if [[ -n "$previous_id" || -n "$previous_token" ]]; then
    [[ -n "$previous_id" && -n "$previous_token" ]] || { fail 'Existing Agent identity is incomplete.'; return 1; }
    ZXY_EXISTING_SERVER_ID="$previous_id" ZXY_EXISTING_AGENT_TOKEN="$previous_token" \
      python3 - "$DB_PATH" <<'PY_ID'
import json, os, sys
try:
    with open(sys.argv[1], encoding='utf-8') as f:
        servers = json.load(f).get('servers', {})
    identity = os.environ['ZXY_EXISTING_SERVER_ID']
    record = servers.get(identity)
    if not record or record.get('id') != identity or record.get('agent_token') != os.environ['ZXY_EXISTING_AGENT_TOKEN']:
        raise ValueError()
except (OSError, ValueError, TypeError, AttributeError):
    raise SystemExit('ERROR: existing Agent identity does not match panel data; no replacement selected.')
PY_ID
    selected_id="$previous_id"
    selected_token="$previous_token"
  elif [[ ${INSTALL_INPUT[SERVER_ID]+present} || ${INSTALL_INPUT[AGENT_TOKEN]+present} ||
          ${INSTALL_INPUT[ZXY_SERVER_ID]+present} || ${INSTALL_INPUT[ZXY_AGENT_TOKEN]+present} ]]; then
    selected_id="${INSTALL_INPUT[SERVER_ID]-${INSTALL_INPUT[ZXY_SERVER_ID]-}}"
    selected_token="${INSTALL_INPUT[AGENT_TOKEN]-${INSTALL_INPUT[ZXY_AGENT_TOKEN]-}}"
  else
  SERVER_PICK=$(ZXY_DB_PATH="$DB_PATH" ZXY_LOCAL_SERVER_IP="$PUBLIC_IP" ZXY_LOCAL_SERVER_HOST="$LOCAL_HOST" python3 - <<'PY'
import json, os
p=os.environ['ZXY_DB_PATH']
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
  selected_id="${SERVER_PICK%%|*}"
  selected_token="${SERVER_PICK#*|}"
  fi
  if [[ -z "$selected_id" || -z "$selected_token" ]]; then
    echo "WARNING: local server not found, skip Agent auto install."
  else
    (
      export APP_DIR CONFIG_DIR INSTALL_XRAY SETUP_XRAY_SERVICE ZXY_FORCE_INSTALL_XRAY ZXY_SKIP_XRAY_INSTALL
      export ZXY_DB_PATH="$DB_PATH"
      if [[ -z "$previous_id" ]]; then
        export SERVER_ID="$selected_id" AGENT_TOKEN="$selected_token"
        if [[ ! ${INSTALL_INPUT[PANEL_BASE]+present} && ! ${INSTALL_INPUT[ZXY_PANEL_BASE]+present} ]]; then
          export PANEL_BASE="http://127.0.0.1:$API_PORT"
        fi
      fi
      bash "$APP_DIR/deploy/agent-install.sh"
    )
  fi
}

copy_package_files() {
  step "Copying package files"
  if command -v rsync >/dev/null 2>&1; then
    local -a preserved=(--exclude '/.env' --exclude '/logs' --exclude '/data' --exclude '/backups')
    if [[ "$DB_PATH" == "$APP_DIR/"* ]]; then preserved+=(--exclude "/${DB_PATH#"$APP_DIR/"}"); fi
    if [[ "$CONFIG_DIR" == "$APP_DIR/"* ]]; then preserved+=(--exclude "/${CONFIG_DIR#"$APP_DIR/"}"); fi
    if [[ "${INSTALL_MODE}" == "fast" ]]; then
      rsync -a --delete "${preserved[@]}" --exclude 'frontend/node_modules' --exclude '*.tsbuildinfo' "$SRC_DIR/." "$APP_DIR/"
    else
      rsync -a --delete "${preserved[@]}" --exclude 'frontend/node_modules' --exclude 'frontend/dist' --exclude '*.tsbuildinfo' "$SRC_DIR/." "$APP_DIR/"
    fi
  else
    python3 - "$SRC_DIR" "$APP_DIR" "$CONFIG_DIR" "$DB_PATH" "$INSTALL_MODE" <<'PY_COPY'
import shutil, sys
from pathlib import Path
source, destination, config, db = map(Path, sys.argv[1:5])
protected = {destination / p for p in ('.env', 'logs', 'data', 'backups', 'frontend/node_modules')}
protected.update((config, db))
if sys.argv[5] != 'fast': protected.add(destination / 'frontend/dist')
def copy_folder(src, dst):
    dst.mkdir(parents=True, exist_ok=True)
    for entry in src.iterdir():
        target = dst / entry.name
        if target in protected or entry.name.endswith('.tsbuildinfo'): continue
        if entry.is_symlink(): raise SystemExit('ERROR: package contains a symbolic link.')
        if entry.is_dir(): copy_folder(entry, target)
        else: shutil.copy2(str(entry), str(target))
copy_folder(source, destination)
PY_COPY
  fi
}

write_env() {
  API_PORT="$API_PORT" WEB_PORT="$WEB_PORT" WEB_BASE_PATH="$WEB_BASE_PATH" \
    ZXY_JWT_SECRET="$JWT_SECRET" ZXY_AGENT_SHARED_SECRET="$AGENT_SECRET" \
    ZXY_ADMIN_USERNAME="$ADMIN_USERNAME" ZXY_ADMIN_PASSWORD="$ADMIN_PASSWORD" \
    ZXY_DB_PATH="$DB_PATH" ZXY_API_ADDR="127.0.0.1:$API_PORT" \
    ZXY_LOCAL_SERVER_IP="$PUBLIC_IP" ZXY_LOCAL_SERVER_HOST="$LOCAL_HOST" \
    ZXY_LOCAL_SERVER_NAME="$LOCAL_SERVER_NAME" ZXY_LOCAL_SERVER_REGION="$LOCAL_SERVER_REGION" \
    ZXY_LOCAL_SERVER_PROVIDER="$LOCAL_SERVER_PROVIDER" ZXY_UPDATE_MANIFEST_URL="$MANIFEST_URL_TO_WRITE" \
    ZXY_AUTO_AGENT="$AUTO_AGENT" ZXY_INSTALL_XRAY="$INSTALL_XRAY" ZXY_SETUP_XRAY_SERVICE="$SETUP_XRAY_SERVICE" \
    python3 - "$APP_DIR/.env" "$INSTALL_MODE" <<'PY_WRITE_ENV'
import os, re, tempfile, sys
from pathlib import Path
path, mode = Path(sys.argv[1]), sys.argv[2]
keys = ('API_PORT WEB_PORT WEB_BASE_PATH ZXY_JWT_SECRET ZXY_AGENT_SHARED_SECRET ZXY_ADMIN_USERNAME '
        'ZXY_ADMIN_PASSWORD ZXY_DB_PATH ZXY_API_ADDR ZXY_LOCAL_SERVER_IP ZXY_LOCAL_SERVER_HOST '
        'ZXY_LOCAL_SERVER_NAME ZXY_LOCAL_SERVER_REGION ZXY_LOCAL_SERVER_PROVIDER ZXY_UPDATE_MANIFEST_URL '
        'ZXY_AUTO_AGENT ZXY_INSTALL_XRAY ZXY_SETUP_XRAY_SERVICE').split()
def encode(value):
    if re.fullmatch(r'[A-Za-z0-9_./:@{}+-]*', value): return value
    if "'" not in value: return "'" + value + "'"
    if mode == 'docker': return "'" + value.replace("'", "\\'") + "'"
    return '"' + re.sub(r'([\\"$`])', r'\\\1', value) + '"'
temporary = None
try:
    if path.is_symlink(): raise ValueError('environment must not be a symlink')
    old = path.read_text(encoding='utf-8').splitlines() if path.exists() else []
    lines = [key + '=' + encode(os.environ[key]) for key in keys]
    lines += [line for line in old if line.split('=', 1)[0] not in keys]
    with tempfile.NamedTemporaryFile(mode='w', encoding='utf-8', newline='\n', dir=str(path.parent), delete=False) as f:
        temporary = f.name
        os.chmod(temporary, 0o600)
        f.write('\n'.join(lines) + '\n')
        f.flush()
        os.fsync(f.fileno())
    os.replace(temporary, str(path))
except (OSError, UnicodeError, ValueError):
    raise SystemExit('ERROR: configuration write failed; previous environment retained.')
finally:
    if temporary and os.path.exists(temporary): os.unlink(temporary)
PY_WRITE_ENV
}

start_fast_runtime() {
  step "Starting fast systemd runtime"
  local api_bin="$APP_DIR/bin/zxy-panel-api-linux-amd64"
  if [[ ! -f "$api_bin" ]]; then
    api_bin="$APP_DIR/bin/zxy-panel-api"
  fi
  if [[ ! -f "$api_bin" ]]; then
    echo "ERROR: fast install selected but API binary is missing."
    echo "Expected: $APP_DIR/bin/zxy-panel-api-linux-amd64"
    exit 1
  fi
  chmod +x "$api_bin"
  cat > /etc/systemd/system/zxy-panel-api.service <<EOF_SERVICE
[Unit]
Description=ZXY Panel API
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory="${APP_DIR}"
EnvironmentFile="${APP_DIR}/.env"
ExecStart="${api_bin}"
Restart=always
RestartSec=5
LimitNOFILE=1000000

[Install]
WantedBy=multi-user.target
EOF_SERVICE
  systemctl daemon-reload
  systemctl enable zxy-panel-api >/dev/null 2>&1 || true
  systemctl restart zxy-panel-api
}

start_docker_runtime() {
  step "Starting Docker containers"
  run_panel_compose up -d --build --force-recreate
}

main() {
  echo "ZXY Panel V${VERSION} product installer"
  echo "Target: ${APP_DIR}"

  if [[ $(id -u) -ne 0 ]]; then
    echo "ERROR: please run as root."
    exit 1
  fi

  SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  preflight_inputs
  case "$ZXY_INSTALL_MODE" in auto|fast|docker) ;; *) fail 'Invalid ZXY_INSTALL_MODE.'; return 1 ;; esac
  if [[ "$ZXY_INSTALL_MODE" == fast ]] && ! has_fast_assets; then
    fail 'Fast assets are missing; nothing was stopped.'
    return 1
  fi
  install_base_deps
  INSTALL_MODE="$(selected_install_mode)"
  PREVIOUS_MODE=$(panel_info_value INSTALL_MODE)
  resolve_existing_config
  echo "Install mode: ${INSTALL_MODE}"
  if [[ "$INSTALL_MODE" == "fast" ]]; then
    echo "Fast mode detected: Docker build / Node build / Go build will be skipped."
    echo "Install speed polish: existing dependencies and existing Xray will be reused when possible."
  else
    echo "Docker compatibility mode: no prebuilt fast assets found."
  fi

  if [[ "$INSTALL_MODE" == "docker" ]]; then
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
  fi
  preflight_runtime_ownership
  preflight_host_nginx
  installer_backup_existing
  cleanup_old_runtime

  step "Preparing directories"
  mkdir -p "$APP_DIR" "$APP_DIR/backups" "$CONFIG_DIR"

  if [[ "$FRESH_INSTALL" == "true" ]]; then
    echo "Fresh install enabled: backing up and clearing old data."
    if [[ -d "$APP_DIR/data" ]]; then
      tar -czf "$APP_DIR/backups/data-before-fresh-$(date +%Y%m%d-%H%M%S).tar.gz" -C "$APP_DIR" data || true
      rm -rf "$APP_DIR/data"
    fi
    rm -f /etc/zxy-panel/bbr.disabled
  fi

  mkdir -p "$APP_DIR/data"
  if [[ -d "$APP_DIR/data" ]]; then
    tar -czf "$APP_DIR/backups/data-backup-$(date +%Y%m%d-%H%M%S).tar.gz" -C "$APP_DIR" data || true
  fi

  copy_package_files
  cd "$APP_DIR"
  install_cli
  install_netopt

  write_env
  write_panel_info

  if [[ "$INSTALL_MODE" == "fast" ]]; then
    start_fast_runtime
    wait_api
    write_host_nginx_fast
  else
    start_docker_runtime
    wait_api
    write_host_nginx_docker
  fi

  allow_local_firewall
  install_local_agent
  enable_default_bbr
  post_install_self_check
  print_result
}

main "$@"
