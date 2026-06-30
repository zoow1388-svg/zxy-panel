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
    die "Please run as root or with sudo."
  fi
}

detect_os() {
  if [ ! -r /etc/os-release ]; then
    die "Cannot detect OS. Ubuntu and Debian are recommended."
  fi
  # shellcheck disable=SC1091
  . /etc/os-release
  OS_ID="${ID:-unknown}"
  OS_VERSION="${VERSION_ID:-unknown}"
  case "${OS_ID}:${OS_VERSION}" in
    ubuntu:22.04|debian:12)
      log "OS check passed: ${PRETTY_NAME:-${OS_ID} ${OS_VERSION}}"
      ;;
    ubuntu:*|debian:*)
      log "Detected ${PRETTY_NAME:-${OS_ID} ${OS_VERSION}}. Continuing in compatibility mode."
      ;;
    *)
      die "Unsupported OS: ${PRETTY_NAME:-${OS_ID} ${OS_VERSION}}. Ubuntu or Debian is recommended."
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
    log "Base dependencies already exist. Skipping dependency install."
    return
  fi

  log "Installing missing dependencies: ${missing[*]}"
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
  die "No supported package manager found. Install unzip curl ca-certificates tar gzip manually."
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
    die "version.json is missing field: ${name}"
  fi
}

validate_sha256() {
  local sha256="$1"
  if ! printf '%s' "${sha256}" | grep -Eq '^[a-fA-F0-9]{64}$'; then
    die "version.json sha256 is not a valid 64-character SHA256 value."
  fi
}

fetch_manifest() {
  MANIFEST_FILE="${WORK_DIR}/version.json"
  log "Fetching version manifest: ${MANIFEST_URL}"
  curl -fL --connect-timeout 10 --retry 3 -o "${MANIFEST_FILE}" "${MANIFEST_URL}" \
    || die "Failed to download version manifest."

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
  log "Downloading release package: ${DOWNLOAD_URL}"
  curl -fL --connect-timeout 10 --retry 3 -o "${PACKAGE_FILE}" "${DOWNLOAD_URL}" \
    || die "Failed to download release package."

  log "Verifying SHA256"
  (cd "${WORK_DIR}" && printf '%s  %s\n' "${SHA256}" "${PACKAGE_NAME}" | sha256sum -c -) \
    || die "SHA256 verification failed. Installation stopped."
}

extract_release() {
  RELEASE_ROOT="${INSTALL_ROOT}/${APP_NAME}-${VERSION}"
  rm -rf "${RELEASE_ROOT}"
  mkdir -p "${RELEASE_ROOT}"
  log "Extracting to: ${RELEASE_ROOT}"
  unzip -q -o "${PACKAGE_FILE}" -d "${RELEASE_ROOT}"

  if [ -f "${RELEASE_ROOT}/deploy/install.sh" ]; then
    RELEASE_DIR="${RELEASE_ROOT}"
    return
  fi

  local deploy_install
  deploy_install="$(find "${RELEASE_ROOT}" -maxdepth 4 -type f -path '*/deploy/install.sh' -print -quit)"
  if [ -n "${deploy_install}" ]; then
    RELEASE_DIR="$(cd "$(dirname "${deploy_install}")/.." && pwd)"
    log "Detected nested release directory: ${RELEASE_DIR}"
    return
  fi

  die "deploy/install.sh was not found in the release package."
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
  log "Running deploy/install.sh"
  export ZXY_UPDATE_MANIFEST_URL="${MANIFEST_URL}"
  (cd "${RELEASE_DIR}" && bash deploy/install.sh)
}

print_finish_info() {
  local access_url port
  access_url="$(panel_access_url)"
  port="$(panel_port)"

  cat <<FINISH

ZXY Panel install flow finished.
Common commands:
  zxy-panel info
  zxy-panel status
  zxy-panel logs

Panel access URL:
  ${access_url}

Security group / firewall reminder:
  Open panel port: ${port}
  Also open any relay entry, landing exit, or Agent communication ports required by your deployment.

Elapsed: $(elapsed)
FINISH
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
