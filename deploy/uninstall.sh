#!/usr/bin/env bash
set -Eeuo pipefail

APP_NAME="zxy-panel"
INSTALL_DIR="${ZXY_INSTALL_DIR:-/opt/zxy-panel}"
CONFIG_DIR="${ZXY_CONFIG_DIR:-/etc/zxy-panel}"

log() {
  printf '[%s uninstall] %s\n' "${APP_NAME}" "$*"
}

if [ "$(id -u)" -ne 0 ]; then
  echo "请使用 root 用户执行卸载脚本" >&2
  exit 1
fi

if systemctl list-unit-files zxy-panel.service >/dev/null 2>&1; then
  systemctl disable --now zxy-panel.service || true
  rm -f /etc/systemd/system/zxy-panel.service
  systemctl daemon-reload || true
fi

if [ -f "${INSTALL_DIR}/docker-compose.yml" ]; then
  if docker compose version >/dev/null 2>&1; then
    (cd "${INSTALL_DIR}" && docker compose down) || true
  elif command -v docker-compose >/dev/null 2>&1; then
    (cd "${INSTALL_DIR}" && docker-compose down) || true
  fi
fi

rm -f /usr/local/bin/zxy-panel

cat <<EOF
ZXY Panel 服务文件和管理命令已移除。

保留目录：
  ${INSTALL_DIR}
  ${CONFIG_DIR}

如确认不再需要，请手动备份后删除上述目录。
EOF
