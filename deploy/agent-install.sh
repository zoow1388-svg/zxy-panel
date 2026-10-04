#!/usr/bin/env bash
set -euo pipefail

declare -A AGENT_INPUT=()
for input_key in PANEL_BASE SERVER_ID AGENT_TOKEN APPLY_CONFIG XRAY_CONFIG XRAY_TEST_CMD XRAY_RELOAD_CMD \
  ZXY_PANEL_BASE ZXY_SERVER_ID ZXY_AGENT_TOKEN ZXY_APPLY_CONFIG ZXY_XRAY_CONFIG \
  ZXY_XRAY_TEST_CMD ZXY_XRAY_RELOAD_CMD ZXY_AGENT_INTERVAL_SECONDS INSTALL_XRAY SETUP_XRAY_SERVICE; do
  if [[ -v "$input_key" ]]; then AGENT_INPUT[$input_key]="${!input_key}"; fi
done
unset input_key
APP_DIR="${APP_DIR-/opt/zxy-panel}"
CONFIG_DIR="${CONFIG_DIR-/etc/zxy-panel}"
AGENT_ENV_FILE="$CONFIG_DIR/agent.env"
AGENT_UNIT_FILE=/etc/systemd/system/zxy-agent.service
XRAY_DROPIN=/etc/systemd/system/xray.service.d/99-zxy-panel.conf
XRAY_INSTALLED_THIS_RUN=false
PANEL_BASE="${PANEL_BASE:-http://127.0.0.1:8088}"
SERVER_ID="${SERVER_ID:-}"
AGENT_TOKEN="${AGENT_TOKEN:-}"
APPLY_CONFIG="${APPLY_CONFIG:-true}"
XRAY_CONFIG="${XRAY_CONFIG:-/etc/zxy-panel/xray/config.json}"
XRAY_TEST_CMD="${XRAY_TEST_CMD:-xray run -test -config {config}}"
XRAY_RELOAD_CMD="${XRAY_RELOAD_CMD:-systemctl restart xray}"
INSTALL_XRAY="${INSTALL_XRAY-true}"
SETUP_XRAY_SERVICE="${SETUP_XRAY_SERVICE-true}"
ZXY_FORCE_INSTALL_XRAY="${ZXY_FORCE_INSTALL_XRAY-0}"
ZXY_SKIP_XRAY_INSTALL="${ZXY_SKIP_XRAY_INSTALL-0}"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DST="/usr/local/bin/zxy-agent"
NETOPT_DST="/usr/local/bin/zxy-netopt"

agent_env_value() {
  python3 - "${2:-$AGENT_ENV_FILE}" "$1" "${3:-value}" <<'PY_READ'
import re, sys
from pathlib import Path
values = {}
try:
    path = Path(sys.argv[1])
    if path.exists():
        for line in path.read_text(encoding='utf-8').splitlines():
            if not line.strip() or line.lstrip().startswith('#'): continue
            match = re.fullmatch(r'([A-Za-z_][A-Za-z0-9_]*)=(.*)', line)
            if not match or match[1] in values: raise ValueError()
            value = match[2].strip()
            if value.startswith("'"):
                if not value.endswith("'") or "'" in value[1:-1]: raise ValueError()
                value = value[1:-1]
            elif value.startswith('"'):
                if not value.endswith('"'): raise ValueError()
                value = re.sub(r'\\([\\"$`])', lambda m: m[1], value[1:-1])
            else:
                trailing = re.search(r'\\+$', value)
                if trailing and len(trailing[0]) % 2: raise ValueError()
                value = re.sub(r'\\(.)', lambda m: m[1], value)
            if any(c in value for c in '\x00\r\n'): raise ValueError()
            values[match[1]] = value
    print(('present' if sys.argv[2] in values else '') if sys.argv[3] == 'present' else values.get(sys.argv[2], ''), end='')
except (OSError, UnicodeError, ValueError):
    raise SystemExit('ERROR: invalid existing Agent environment; no changes made.')
PY_READ
}

agent_value() {
  local input="$1" key="$2" fallback="$3" value present
  if [[ ${AGENT_INPUT[$input]+present} ]]; then
    value="${AGENT_INPUT[$input]}"
  elif [[ ${AGENT_INPUT[$key]+present} ]]; then
    value="${AGENT_INPUT[$key]}"
  else
    present=$(agent_env_value "$key" "$AGENT_ENV_FILE" present) || return
    if [[ "$present" != present ]]; then printf '%s' "$fallback"; return; fi
    value=$(agent_env_value "$key") || return
  fi
  [[ -n "$value" ]] || { echo "ERROR: empty $key; no default was substituted." >&2; return 1; }
  printf '%s' "$value"
}

resolve_agent_config() {
  local default_apply=true key old_id old_token
  if [[ "$INSTALL_XRAY" == false ]] && ! command -v xray >/dev/null 2>&1; then default_apply=false; fi
  PANEL_BASE=$(agent_value PANEL_BASE ZXY_PANEL_BASE http://127.0.0.1:8088) || return
  SERVER_ID=$(agent_value SERVER_ID ZXY_SERVER_ID '') || return
  AGENT_TOKEN=$(agent_value AGENT_TOKEN ZXY_AGENT_TOKEN '') || return
  APPLY_CONFIG=$(agent_value APPLY_CONFIG ZXY_APPLY_CONFIG "$default_apply") || return
  XRAY_CONFIG=$(agent_value XRAY_CONFIG ZXY_XRAY_CONFIG "$CONFIG_DIR/xray/config.json") || return
  XRAY_TEST_CMD=$(agent_value XRAY_TEST_CMD ZXY_XRAY_TEST_CMD 'xray run -test -config {config}') || return
  XRAY_RELOAD_CMD=$(agent_value XRAY_RELOAD_CMD ZXY_XRAY_RELOAD_CMD 'systemctl restart xray') || return
  AGENT_INTERVAL=$(agent_value ZXY_AGENT_INTERVAL_SECONDS ZXY_AGENT_INTERVAL_SECONDS 30) || return
  for key in PANEL_BASE SERVER_ID AGENT_TOKEN APPLY_CONFIG XRAY_CONFIG XRAY_TEST_CMD XRAY_RELOAD_CMD AGENT_INTERVAL; do
    [[ "${!key}" != *$'\n'* && "${!key}" != *$'\r'* ]] || { echo "ERROR: invalid $key." >&2; return 1; }
  done
  [[ "$PANEL_BASE" =~ ^https?://[^[:space:]]+$ ]] || { echo 'ERROR: invalid PANEL_BASE.' >&2; return 1; }
  [[ "$APPLY_CONFIG" == true || "$APPLY_CONFIG" == false ]] || { echo 'ERROR: invalid APPLY_CONFIG.' >&2; return 1; }
  [[ "$AGENT_INTERVAL" =~ ^[0-9]{1,9}$ ]] && (( 10#$AGENT_INTERVAL > 0 )) || { echo 'ERROR: invalid Agent interval.' >&2; return 1; }
  [[ "$XRAY_CONFIG" == /* && "$XRAY_CONFIG" != *['"%\']* && -n "$XRAY_TEST_CMD" && -n "$XRAY_RELOAD_CMD" ]] || { echo 'ERROR: invalid Xray configuration parameters.' >&2; return 1; }
  old_id=$(agent_env_value ZXY_SERVER_ID) || return
  old_token=$(agent_env_value ZXY_AGENT_TOKEN) || return
  if [[ -n "$old_id" || -n "$old_token" ]]; then
    [[ -n "$old_id" && -n "$old_token" && "$SERVER_ID" == "$old_id" && "$AGENT_TOKEN" == "$old_token" ]] || {
      echo 'ERROR: existing Agent identity cannot be replaced during installation.' >&2; return 1;
    }
  fi
  if [[ -f "$APP_DIR/.env" && ( -n "$SERVER_ID" || -n "$AGENT_TOKEN" ) ]]; then
      local database="${ZXY_DB_PATH:-}"
      if [[ -z "$database" ]]; then database=$(agent_env_value ZXY_DB_PATH "$APP_DIR/.env") || return; fi
      [[ -n "$database" && -f "$database" ]] || {
        echo 'ERROR: existing panel database is unavailable; Agent identity was not assumed.' >&2; return 1;
      }
      ZXY_ID_CHECK="$SERVER_ID" ZXY_TOKEN_CHECK="$AGENT_TOKEN" python3 - "$database" <<'PY_ID_CHECK'
import json, os, sys
try:
    with open(sys.argv[1], encoding='utf-8') as f: data = json.load(f)
    identity = os.environ['ZXY_ID_CHECK']
    record = data.get('servers', {}).get(identity)
    if not record or record.get('id') != identity or record.get('agent_token') != os.environ['ZXY_TOKEN_CHECK']: raise ValueError()
except (OSError, ValueError, TypeError, AttributeError):
    raise SystemExit('ERROR: existing Agent identity does not match panel data.')
PY_ID_CHECK
  fi
}

agent_unit_owned() {
  local state fragment dropins execution environment
  [[ -f "$AGENT_UNIT_FILE" && ! -L "$AGENT_UNIT_FILE" && -f "$AGENT_ENV_FILE" ]] || return 1
  state=$(systemctl show zxy-agent -p LoadState --value) || return
  fragment=$(systemctl show zxy-agent -p FragmentPath --value) || return
  dropins=$(systemctl show zxy-agent -p DropInPaths --value) || return
  [[ "$state" == loaded && "$fragment" == "$AGENT_UNIT_FILE" && -z "$dropins" ]] || return 1
  execution=$(systemctl show zxy-agent -p ExecStart --value) || return
  environment=$(systemctl show zxy-agent -p EnvironmentFiles --value) || return
  [[ "$environment" == "$AGENT_ENV_FILE (ignore_errors=no)" ]] || return 1
  ZXY_UNIT_EXECUTION="$execution" python3 - "$AGENT_UNIT_FILE" "$AGENT_ENV_FILE" <<'PY_AGENT_UNIT'
import os, re, shlex, sys
from pathlib import Path
try:
    section, values = '', {}
    for line in Path(sys.argv[1]).read_text(encoding='utf-8').splitlines():
        line = line.strip()
        if line.startswith('['): section = line
        elif section == '[Service]' and '=' in line and not line.startswith(('#', ';')):
            key, value = line.split('=', 1)
            values.setdefault(key, []).append(shlex.split(value))
    allowed = {'Type', 'EnvironmentFile', 'ExecStart', 'Restart', 'RestartSec', 'LimitNOFILE'}
    valid = not (set(values) - allowed)
    valid = valid and values.get('EnvironmentFile') == [[sys.argv[2]]]
    valid = valid and values.get('ExecStart') == [['/usr/local/bin/zxy-agent']]
    execution = os.environ['ZXY_UNIT_EXECUTION']
    match = re.search(r'path=(.*?) ; argv\[\]=(.*?) ;', execution)
    valid = valid and execution.count('path=') == 1 and bool(match)
    valid = valid and match[1] == match[2] == '/usr/local/bin/zxy-agent'
    raise SystemExit(0 if valid else 1)
except (OSError, UnicodeError, ValueError):
    raise SystemExit(1)
PY_AGENT_UNIT
}

xray_service_owned() {
  [[ -f "$XRAY_DROPIN" ]] && agent_unit_owned || return 1
  local previous_config binary
  previous_config=$(agent_env_value ZXY_XRAY_CONFIG) || return
  binary=$(command -v xray) || return
  [[ -n "$previous_config" ]] || return 1
  local state fragment dropins execution user group
  state=$(systemctl show xray -p LoadState --value) || return
  fragment=$(systemctl show xray -p FragmentPath --value) || return
  dropins=$(systemctl show xray -p DropInPaths --value) || return
  execution=$(systemctl show xray -p ExecStart --value) || return
  user=$(systemctl show xray -p User --value) || return
  group=$(systemctl show xray -p Group --value) || return
  [[ "$state" == loaded && -f "$fragment" && "$dropins" == "$XRAY_DROPIN" && "$user" == root && "$group" == root ]] || return 1
  ZXY_UNIT_EXECUTION="$execution" python3 - "$XRAY_DROPIN" "$previous_config" "$binary" <<'PY_OWNERSHIP'
import os, re, shlex, sys
from pathlib import Path
try:
    values = {}
    for line in Path(sys.argv[1]).read_text(encoding='utf-8').splitlines():
        if '=' in line and not line.lstrip().startswith(('#', ';')):
            key, value = line.split('=', 1)
            values.setdefault(key.strip(), []).append(value.strip())
    starts = values.get('ExecStart', [])
    valid = values.get('User') == ['root'] and values.get('Group') == ['root']
    valid = valid and starts[:1] == [''] and len(starts) == 2
    valid = valid and shlex.split(starts[1]) == [sys.argv[3], 'run', '-config', sys.argv[2]]
    execution = os.environ['ZXY_UNIT_EXECUTION']
    match = re.search(r'path=(.*?) ; argv\[\]=(.*?) ;', execution)
    valid = valid and execution.count('path=') == 1 and bool(match) and match[1] == sys.argv[3]
    valid = valid and match[2] == ' '.join([sys.argv[3], 'run', '-config', sys.argv[2]])
    raise SystemExit(0 if valid else 1)
except (OSError, UnicodeError, ValueError, IndexError):
    raise SystemExit(1)
PY_OWNERSHIP
}

preflight_agent() {
  local key state fragment dropins mutates_xray=false
  for key in APP_DIR CONFIG_DIR; do
    [[ "${!key}" == /* && "${!key}" != / && "${!key}" != *'/../'* && "${!key}" != */.. && "${!key}" != *['";$`%\']* && "${!key}" != *$'\n'* && "${!key}" != *$'\r'* && "${!key}" != *$'\t'* ]] || { echo "ERROR: unsupported $key path; no changes made." >&2; return 1; }
  done
  for key in INSTALL_XRAY SETUP_XRAY_SERVICE; do
    [[ "${!key}" == true || "${!key}" == false ]] || { echo "ERROR: invalid $key." >&2; return 1; }
  done
  for key in ZXY_FORCE_INSTALL_XRAY ZXY_SKIP_XRAY_INSTALL; do
    case "${!key}" in 0|1|false|true) ;; *) echo "ERROR: invalid $key." >&2; return 1 ;; esac
  done
  [[ ! -L "$AGENT_ENV_FILE" ]] || { echo 'ERROR: Agent environment must not be a symlink.' >&2; return 1; }
  resolve_agent_config || return
  state=$(systemctl show zxy-agent -p LoadState --value) || { echo 'ERROR: cannot inspect Agent unit.' >&2; return 1; }
  if [[ "$state" != not-found || -e "$AGENT_UNIT_FILE" || -L "$AGENT_UNIT_FILE" ]] && ! agent_unit_owned; then
    echo 'ERROR: Agent unit belongs to a different configuration.' >&2; return 1
  fi
  state=$(systemctl show xray -p LoadState --value) || { echo 'ERROR: cannot inspect Xray unit.' >&2; return 1; }
  fragment=$(systemctl show xray -p FragmentPath --value) || { echo 'ERROR: cannot inspect Xray fragment.' >&2; return 1; }
  dropins=$(systemctl show xray -p DropInPaths --value) || { echo 'ERROR: cannot inspect Xray drop-ins.' >&2; return 1; }
  if [[ "$SETUP_XRAY_SERVICE" == true || "$APPLY_CONFIG" == true ]] ||
     { [[ "$INSTALL_XRAY" == true && "$ZXY_SKIP_XRAY_INSTALL" != 1 && "$ZXY_SKIP_XRAY_INSTALL" != true ]] &&
       { [[ "$ZXY_FORCE_INSTALL_XRAY" == 1 || "$ZXY_FORCE_INSTALL_XRAY" == true ]] || ! command -v xray >/dev/null 2>&1; }; }; then
    mutates_xray=true
  fi
  if [[ "$mutates_xray" == true ]]; then
    case "$state" in
      loaded|not-found) ;;
      *) echo 'ERROR: Xray unit is masked or invalid; no installation action taken.' >&2; return 1 ;;
    esac
  fi
  if command -v xray >/dev/null 2>&1 || [[ "$state" != not-found || -n "$fragment" || -n "$dropins" || -e "$XRAY_DROPIN" || -L "$XRAY_DROPIN" ]]; then
    if [[ "$mutates_xray" == true ]] && ! xray_service_owned; then
      echo 'ERROR: Xray ownership is unconfirmed; service setup, apply and reinstall were refused.' >&2
      return 1
    fi
  fi
  if [[ "$APPLY_CONFIG" == true && "$INSTALL_XRAY" == false ]] && ! command -v xray >/dev/null 2>&1; then
    echo 'ERROR: APPLY_CONFIG requires Xray; use false for a status-only Agent.' >&2; return 1
  fi
}

write_agent_env() {
  ZXY_PANEL_BASE="$PANEL_BASE" ZXY_SERVER_ID="$SERVER_ID" ZXY_AGENT_TOKEN="$AGENT_TOKEN" \
    ZXY_XRAY_CONFIG="$XRAY_CONFIG" ZXY_XRAY_TEST_CMD="$XRAY_TEST_CMD" ZXY_XRAY_RELOAD_CMD="$XRAY_RELOAD_CMD" \
    ZXY_APPLY_CONFIG="$APPLY_CONFIG" ZXY_AGENT_INTERVAL_SECONDS="$AGENT_INTERVAL" \
    python3 - "$AGENT_ENV_FILE" <<'PY_AGENT_ENV'
import os, re, sys, tempfile
from pathlib import Path
path = Path(sys.argv[1])
keys = ('ZXY_PANEL_BASE ZXY_SERVER_ID ZXY_AGENT_TOKEN ZXY_XRAY_CONFIG ZXY_XRAY_TEST_CMD '
        'ZXY_XRAY_RELOAD_CMD ZXY_APPLY_CONFIG ZXY_AGENT_INTERVAL_SECONDS').split()
def encode(value):
    if re.fullmatch(r'[A-Za-z0-9_./:@{}+-]*', value): return value
    if "'" not in value: return "'" + value + "'"
    return '"' + re.sub(r'([\\"$`])', r'\\\1', value) + '"'
temporary = None
try:
    if path.is_symlink(): raise ValueError()
    lines = path.read_text(encoding='utf-8').splitlines() if path.exists() else []
    rows = [key + '=' + encode(os.environ[key]) for key in keys]
    rows += [line for line in lines if line.split('=', 1)[0] not in keys]
    with tempfile.NamedTemporaryFile(mode='w', encoding='utf-8', newline='\n', dir=str(path.parent), delete=False) as f:
        temporary = f.name
        os.chmod(temporary, 0o600)
        f.write('\n'.join(rows) + '\n')
        f.flush()
        os.fsync(f.fileno())
    os.replace(temporary, str(path))
except (OSError, UnicodeError, ValueError):
    raise SystemExit('ERROR: Agent configuration write failed; previous environment retained.')
finally:
    if temporary and os.path.exists(temporary): os.unlink(temporary)
PY_AGENT_ENV
}

install_local_xray_if_available() {
  local xray_src=""
  if [[ -f "$ROOT_DIR/bin/xray-linux-amd64" ]]; then
    xray_src="$ROOT_DIR/bin/xray-linux-amd64"
  elif [[ -f "$ROOT_DIR/bin/xray" ]]; then
    xray_src="$ROOT_DIR/bin/xray"
  fi

  if [[ -z "$xray_src" ]]; then
    return 1
  fi

  echo "Installing bundled Xray-core: $xray_src"
  install -m 0755 "$xray_src" /usr/local/bin/xray || return 2
  mkdir -p /usr/local/share/xray /usr/local/etc/xray /var/log/xray /etc/zxy-panel/xray || return 2

  if [[ -f "$ROOT_DIR/bin/geoip.dat" ]]; then
    install -m 0644 "$ROOT_DIR/bin/geoip.dat" /usr/local/share/xray/geoip.dat || return 2
  fi
  if [[ -f "$ROOT_DIR/bin/geosite.dat" ]]; then
    install -m 0644 "$ROOT_DIR/bin/geosite.dat" /usr/local/share/xray/geosite.dat || return 2
  fi
  if [[ ! -f /usr/local/etc/xray/config.json ]]; then
    cat > /usr/local/etc/xray/config.json <<'JSON' || return 2
{
  "log": { "loglevel": "warning" },
  "inbounds": [],
  "outbounds": [ { "protocol": "freedom", "tag": "direct" } ]
}
JSON
  fi
  return 0
}

main() {
case "${1:-}" in ''|--preflight) ;; *) echo 'ERROR: unsupported argument.' >&2; return 1 ;; esac
preflight_agent
if [[ "${1:-}" == --preflight ]]; then return 0; fi
[[ $(id -u) -eq 0 ]] || { echo 'ERROR: root is required.' >&2; return 1; }
[[ -n "$SERVER_ID" && -n "$AGENT_TOKEN" ]] || { echo 'ERROR: SERVER_ID and AGENT_TOKEN are required.' >&2; return 1; }
if [[ "${ZXY_SKIP_XRAY_INSTALL}" == "1" || "${ZXY_SKIP_XRAY_INSTALL}" == "true" ]]; then
  echo "ZXY_SKIP_XRAY_INSTALL is enabled, skip Xray-core installation."
elif [[ "${INSTALL_XRAY}" == "true" ]]; then
  if [[ "${ZXY_FORCE_INSTALL_XRAY}" != "1" && "${ZXY_FORCE_INSTALL_XRAY}" != "true" ]] && command -v xray >/dev/null 2>&1; then
    echo "Xray-core already installed, skip download: $(xray version | head -1)"
  elif install_local_xray_if_available; then
    XRAY_INSTALLED_THIS_RUN=true
    echo "Bundled Xray-core installed: $(xray version | head -1)"
  else
    local bundled_status=$?
    [[ "$bundled_status" == 1 ]] || {
      echo 'ERROR: bundled Xray installation failed; no download fallback or success claim.' >&2; return "$bundled_status";
    }
    if [[ "${ZXY_FORCE_INSTALL_XRAY}" == "1" || "${ZXY_FORCE_INSTALL_XRAY}" == "true" ]]; then
      echo "ZXY_FORCE_INSTALL_XRAY is enabled, reinstalling Xray-core using official community install script..."
    else
      echo "Xray-core not found, installing using official community install script..."
    fi
    local xray_installer
    xray_installer=$(curl -fsSL https://github.com/XTLS/Xray-install/raw/main/install-release.sh)
    bash -c "$xray_installer" @ install
    XRAY_INSTALLED_THIS_RUN=true
  fi
else
  echo "INSTALL_XRAY is false, skip Xray-core installation."
fi

BIN_SRC=""
if [[ -f "$ROOT_DIR/bin/zxy-agent-linux-amd64" ]]; then
  BIN_SRC="$ROOT_DIR/bin/zxy-agent-linux-amd64"
elif [[ -f "$ROOT_DIR/bin/zxy-agent" ]]; then
  BIN_SRC="$ROOT_DIR/bin/zxy-agent"
fi

if [[ -z "$BIN_SRC" ]]; then
  echo "Agent binary not found, trying to build from source..."
  if ! command -v go >/dev/null 2>&1; then
    echo "ERROR: Go is not installed and prebuilt binary is missing."
    exit 1
  fi
  BIN_SRC="$ROOT_DIR/bin/zxy-agent-linux-amd64"
  mkdir -p "$ROOT_DIR/bin"
  (cd "$ROOT_DIR/agent" && go build -o "$BIN_SRC" ./cmd/agent)
fi

install -m 0755 "$BIN_SRC" "$BIN_DST"
if [[ -f "$ROOT_DIR/scripts/zxy-netopt" ]]; then
  install -m 0755 "$ROOT_DIR/scripts/zxy-netopt" "$NETOPT_DST"
else
  echo "WARNING: zxy-netopt source is missing; BBR controls will be unavailable on this server."
fi
mkdir -p "$CONFIG_DIR/xray"

if [[ "${SETUP_XRAY_SERVICE}" == "true" ]]; then
  XRAY_BIN="$(command -v xray || true)"
  if [[ -n "$XRAY_BIN" ]]; then
    [[ "$XRAY_INSTALLED_THIS_RUN" == true ]] || xray_service_owned || return 1
    mkdir -p "$(dirname "$XRAY_DROPIN")"
    cat > "$XRAY_DROPIN" <<EOF_SERVICE
# Managed by ZXY Panel Agent
[Service]
User=root
Group=root
ExecStart=
ExecStart="$XRAY_BIN" run -config "${XRAY_CONFIG}"
EOF_SERVICE
    systemctl daemon-reload
  fi
fi

write_agent_env
cat > "$AGENT_UNIT_FILE" <<SERVICE
[Unit]
Description=ZXY Panel Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile="$AGENT_ENV_FILE"
ExecStart=/usr/local/bin/zxy-agent
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
SERVICE

systemctl daemon-reload
systemctl enable zxy-agent
systemctl restart zxy-agent

if [[ "${ZXY_BBR_AUTO_ENABLE:-true}" == "true" && -x "$NETOPT_DST" ]]; then
  echo "Checking and enabling BBR network optimization..."
  ZXY_BBR_AUTO_ENABLE=true "$NETOPT_DST" enable-bbr || true
fi

echo "ZXY Agent installed."
echo "Check status: systemctl status zxy-agent --no-pager"
echo "Check logs:   journalctl -u zxy-agent -n 80 --no-pager"
}

main "$@"
