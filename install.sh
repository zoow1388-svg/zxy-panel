#!/usr/bin/env bash
set -Eeuo pipefail

APP_NAME="zxy-panel"
REPO_OWNER="${ZXY_REPO_OWNER:-zoow1388-svg}"
REPO_NAME="${ZXY_REPO_NAME:-zxy-panel}"
REPO_BRANCH="${ZXY_REPO_BRANCH:-main}"
INSTALL_ROOT="${ZXY_INSTALL_ROOT:-/root}"
MANIFEST_URL="${ZXY_UPDATE_MANIFEST_URL:-https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/${REPO_BRANCH}/version.json}"
WORK_DIR="$(mktemp -d)"
START_TS="$(date +%s)"
APT_UPDATED=false

cleanup() {
  rm -rf "${WORK_DIR}"
}
trap cleanup EXIT

log() {
  printf '[%s] %s\n' "${APP_NAME}" "$*"
}

die() {
  printf '[%s] ERROR: %s\n' "${APP_NAME}" "$*" >&2
  exit 1
}

elapsed() {
  local now
  now="$(date +%s)"
  printf '%ss' "$((now - START_TS))"
}

require_root() {
  if [ "$(id -u)" -ne 0 ]; then
    die "请使用 root 用户执行，或通过 sudo 执行安装命令"
  fi
}

detect_os() {
  if [ ! -r /etc/os-release ]; then
    die "无法识别系统版本，当前仅优先支持 Ubuntu 22.04 / Debian 12"
  fi
  # shellcheck disable=SC1091
  . /etc/os-release
  OS_ID="${ID:-unknown}"
  OS_VERSION="${VERSION_ID:-unknown}"
  case "${OS_ID}:${OS_VERSION}" in
    ubuntu:22.04|debian:12)
      log "系统检测通过：${PRETTY_NAME:-${OS_ID} ${OS_VERSION}}"
      ;;
    ubuntu:*|debian:*)
      log "检测到 ${PRETTY_NAME:-${OS_ID} ${OS_VERSION}}，将按兼容模式继续"
      ;;
    *)
      die "当前系统为 ${PRETTY_NAME:-${OS_ID} ${OS_VERSION}}，请优先使用 Ubuntu 22.04 / Debian 12"
      ;;
  esac
}

apt_update_once() {
  if [ "${APT_UPDATED}" != "true" ]; then
    apt-get update
    APT_UPDATED=true
  fi
}

install_dependencies() {
  local missing=()
  command -v unzip >/dev/null 2>&1 || missing+=(unzip)
  command -v curl >/dev/null 2>&1 || missing+=(curl)
  command -v tar >/dev/null 2>&1 || missing+=(tar)
  command -v gzip >/dev/null 2>&1 || missing+=(gzip)
  if [ ! -f /etc/ssl/certs/ca-certificates.crt ]; then
    missing+=(ca-certificates)
  fi

  if [ "${#missing[@]}" -eq 0 ]; then
    log "基础依赖已存在，跳过依赖安装"
    return
  fi

  log "安装缺失依赖：${missing[*]}"
  if command -v apt-get >/dev/null 2>&1; then
    apt_update_once
    DEBIAN_FRONTEND=noninteractive apt-get install -y "${missing[@]}"
    return
  fi
  if command -v dnf >/dev/null 2>&1; then
    dnf install -y "${missing[@]}"
    return
  fi
  if command -v yum >/dev/null 2>&1; then
    yum install -y "${missing[@]}"
    return
  fi
  die "未找到支持的包管理器，请手动安装 unzip curl ca-certificates tar gzip"
}

json_value() {
  local key="$1"
  local file="$2"
  sed -nE "s/^[[:space:]]*\"${key}\"[[:space:]]*:[[:space:]]*\"([^\"]*)\".*/\1/p" "${file}" | head -n 1
}

validate_manifest_field() {
  local name="$1"
  local value="$2"
  if [ -z "${value}" ]; then
    die "version.json 缺少字段：${name}"
  fi
}

validate_sha256() {
  local sha256="$1"
  if ! printf '%s' "${sha256}" | grep -Eq '^[a-fA-F0-9]{64}$'; then
    die "version.json 中的 sha256 尚未配置为真实 64 位校验值"
  fi
}

fetch_manifest() {
  MANIFEST_FILE="${WORK_DIR}/version.json"
  log "读取版本清单：${MANIFEST_URL}"
  curl -fL --connect-timeout 10 --retry 3 -o "${MANIFEST_FILE}" "${MANIFEST_URL}" \
    || die "无法下载版本清单"

  PACKAGE_NAME="$(json_value package "${MANIFEST_FILE}")"
  VERSION="$(json_value version "${MANIFEST_FILE}")"
  DOWNLOAD_URL="$(json_value download_url "${MANIFEST_FILE}")"
  SHA256="$(json_value sha256 "${MANIFEST_FILE}")"

  validate_manifest_field "package" "${PACKAGE_NAME}"
  validate_manifest_field "version" "${VERSION}"
  validate_manifest_field "download_url" "${DOWNLOAD_URL}"
  validate_manifest_field "sha256" "${SHA256}"
  validate_sha256 "${SHA256}"
}

download_release() {
  PACKAGE_FILE="${WORK_DIR}/${PACKAGE_NAME}"
  log "下载稳定包：${DOWNLOAD_URL}"
  curl -fL --connect-timeout 10 --retry 3 -o "${PACKAGE_FILE}" "${DOWNLOAD_URL}" \
    || die "无法下载稳定包"

  log "校验 SHA256"
  (cd "${WORK_DIR}" && printf '%s  %s\n' "${SHA256}" "${PACKAGE_NAME}" | sha256sum -c -) \
    || die "SHA256 校验失败，已停止安装"
}

extract_release() {
  RELEASE_ROOT="${INSTALL_ROOT}/${APP_NAME}-${VERSION}"
  rm -rf "${RELEASE_ROOT}"
  mkdir -p "${RELEASE_ROOT}"
  log "解压到：${RELEASE_ROOT}"
  unzip -q -o "${PACKAGE_FILE}" -d "${RELEASE_ROOT}"

  if [ -f "${RELEASE_ROOT}/deploy/install.sh" ]; then
    RELEASE_DIR="${RELEASE_ROOT}"
    return
  fi

  local deploy_install
  deploy_install="$(find "${RELEASE_ROOT}" -maxdepth 4 -type f -path '*/deploy/install.sh' -print -quit)"
  if [ -n "${deploy_install}" ]; then
    RELEASE_DIR="$(cd "$(dirname "${deploy_install}")/.." && pwd)"
    log "检测到嵌套发布目录：${RELEASE_DIR}"
    return
  fi

  die "稳定包中未找到 deploy/install.sh"
}

detect_public_ip() {
  local ip
  for endpoint in \
    "https://api.ipify.org" \
    "https://ifconfig.me/ip" \
    "https://ipv4.icanhazip.com"; do
    ip="$(curl -fsSL --max-time 4 "${endpoint}" 2>/dev/null | tr -d '[:space:]' || true)"
    if printf '%s' "${ip}" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$'; then
      printf '%s' "${ip}"
      return
    fi
  done
  printf '<server-public-ip>'
}

panel_info_value() {
  local key="$1"
  if [ -r /etc/zxy-panel/panel.info ]; then
    awk -F= -v k="${key}" '$1==k {print $2; exit}' /etc/zxy-panel/panel.info 2>/dev/null || true
  fi
}

panel_port() {
  local port
  port="$(panel_info_value PORT)"
  if [ -n "${port}" ]; then
    printf '%s' "${port}"
    return
  fi
  printf '%s' "${ZXY_PANEL_PORT:-2053}"
}

panel_access_url() {
  local url ip port web_base
  url="$(panel_info_value ACCESS_URL)"
  if [ -z "${url}" ]; then
    url="$(panel_info_value 'Access URL')"
  fi
  if [ -n "${url}" ]; then
    printf '%s' "${url}"
    return
  fi

  ip="$(detect_public_ip)"
  port="$(panel_port)"
  web_base="$(panel_info_value WEB_BASE_PATH)"
  if [ -z "${web_base}" ]; then
    web_base="$(panel_info_value WebBasePath)"
  fi

  if [ -n "${web_base}" ]; then
    printf 'http://%s:%s/%s/' "${ip}" "${port}" "${web_base}"
  else
    printf 'http://%s:%s' "${ip}" "${port}"
  fi
}

run_deploy_install() {
  chmod +x "${RELEASE_DIR}/deploy/install.sh"
  log "执行部署脚本"
  export ZXY_UPDATE_MANIFEST_URL="${MANIFEST_URL}"
  (cd "${RELEASE_DIR}" && bash deploy/install.sh)
}

print_finish_info() {
  local access_url port
  access_url="$(panel_access_url)"
  port="$(panel_port)"

  cat <<'FINISH_HEAD'

ZXY Panel 安装流程已完成。

常用命令：
  zxy-panel info
  zxy-panel status
  zxy-panel logs

面板访问地址：
FINISH_HEAD
  echo "  ${access_url}"
  cat <<FINISH_TAIL

请在服务器安全组 / 防火墙中放行面板端口：${port}
如使用中转入口、落地出口或 Agent 通信端口，也需要按实际配置放行。

安装耗时：$(elapsed)
FINISH_TAIL
}

main() {
  require_root
  detect_os
  install_dependencies
  fetch_manifest
  download_release
  extract_release
  run_deploy_install
  print_finish_info
}

main "$@"
