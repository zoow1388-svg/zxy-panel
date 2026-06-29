#!/usr/bin/env bash
set -euo pipefail

PANEL_BASE="${PANEL_BASE:-http://127.0.0.1:8088}"
SERVER_ID="${SERVER_ID:-}"
AGENT_TOKEN="${AGENT_TOKEN:-}"
APPLY_CONFIG="${APPLY_CONFIG:-true}"
XRAY_CONFIG="${XRAY_CONFIG:-/etc/zxy-panel/xray/config.json}"
XRAY_TEST_CMD="${XRAY_TEST_CMD:-xray run -test -config {config}}"
XRAY_RELOAD_CMD="${XRAY_RELOAD_CMD:-systemctl restart xray}"
INSTALL_XRAY="${INSTALL_XRAY:-true}"
SETUP_XRAY_SERVICE="${SETUP_XRAY_SERVICE:-true}"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DST="/usr/local/bin/zxy-agent"

if [[ -z "$SERVER_ID" || -z "$AGENT_TOKEN" ]]; then
  echo "ERROR: SERVER_ID and AGENT_TOKEN are required."
  echo "Example: PANEL_BASE='http://1.2.3.4:8088' SERVER_ID='srv_xxx' AGENT_TOKEN='token' ./deploy/agent-install.sh"
  exit 1
fi

install_local_xray_if_available() {
  local xray_src=""
  if [[ -x "$ROOT_DIR/bin/xray-linux-amd64" ]]; then
    xray_src="$ROOT_DIR/bin/xray-linux-amd64"
  elif [[ -x "$ROOT_DIR/bin/xray" ]]; then
    xray_src="$ROOT_DIR/bin/xray"
  fi

  if [[ -z "$xray_src" ]]; then
    return 1
  fi

  echo "Installing bundled Xray-core: $xray_src"
  install -m 0755 "$xray_src" /usr/local/bin/xray
  mkdir -p /usr/local/share/xray /usr/local/etc/xray /var/log/xray /etc/zxy-panel/xray

  if [[ -f "$ROOT_DIR/bin/geoip.dat" ]]; then
    install -m 0644 "$ROOT_DIR/bin/geoip.dat" /usr/local/share/xray/geoip.dat
  fi
  if [[ -f "$ROOT_DIR/bin/geosite.dat" ]]; then
    install -m 0644 "$ROOT_DIR/bin/geosite.dat" /usr/local/share/xray/geosite.dat
  fi
  if [[ ! -f /usr/local/etc/xray/config.json ]]; then
    cat > /usr/local/etc/xray/config.json <<'JSON'
{
  "log": { "loglevel": "warning" },
  "inbounds": [],
  "outbounds": [ { "protocol": "freedom", "tag": "direct" } ]
}
JSON
  fi
  return 0
}

if [[ "${INSTALL_XRAY}" == "true" ]]; then
  if command -v xray >/dev/null 2>&1; then
    echo "Xray-core already installed: $(xray version | head -1)"
  elif install_local_xray_if_available; then
    echo "Bundled Xray-core installed: $(xray version | head -1)"
  else
    echo "Installing Xray-core using the official community install script..."
    bash -c "$(curl -L https://github.com/XTLS/Xray-install/raw/main/install-release.sh)" @ install
  fi
fi

BIN_SRC=""
if [[ -x "$ROOT_DIR/bin/zxy-agent-linux-amd64" ]]; then
  BIN_SRC="$ROOT_DIR/bin/zxy-agent-linux-amd64"
elif [[ -x "$ROOT_DIR/bin/zxy-agent" ]]; then
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
mkdir -p /etc/zxy-panel/xray
cat > /etc/zxy-panel/agent.env <<ENV
ZXY_PANEL_BASE=${PANEL_BASE}
ZXY_SERVER_ID=${SERVER_ID}
ZXY_AGENT_TOKEN=${AGENT_TOKEN}
ZXY_XRAY_CONFIG=${XRAY_CONFIG}
ZXY_XRAY_TEST_CMD=${XRAY_TEST_CMD}
ZXY_XRAY_RELOAD_CMD=${XRAY_RELOAD_CMD}
ZXY_APPLY_CONFIG=${APPLY_CONFIG}
ZXY_AGENT_INTERVAL_SECONDS=30
ENV
chmod 0600 /etc/zxy-panel/agent.env

if [[ "${SETUP_XRAY_SERVICE}" == "true" ]]; then
  XRAY_BIN="$(command -v xray || true)"
  if [[ -n "$XRAY_BIN" ]]; then
    mkdir -p /etc/systemd/system/xray.service.d
    cat > /etc/systemd/system/xray.service.d/99-zxy-panel.conf <<EOF_SERVICE
[Service]
User=root
Group=root
ExecStart=
ExecStart=$XRAY_BIN run -config ${XRAY_CONFIG}
EOF_SERVICE
    systemctl daemon-reload
  fi
fi

cat > /etc/systemd/system/zxy-agent.service <<'SERVICE'
[Unit]
Description=ZXY Panel Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/zxy-panel/agent.env
ExecStart=/usr/local/bin/zxy-agent
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
SERVICE

systemctl daemon-reload
systemctl enable zxy-agent
systemctl restart zxy-agent

echo "ZXY Agent installed."
echo "Check status: systemctl status zxy-agent --no-pager"
echo "Check logs:   journalctl -u zxy-agent -n 80 --no-pager"
