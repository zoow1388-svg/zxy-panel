#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-0.7.5.6}"
CODENAME="fast-install"
FULL_VERSION="${VERSION}-${CODENAME}-agent-xray"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="$ROOT_DIR/dist-release"
PKG_NAME="zxy-panel-v${VERSION}-${CODENAME}.zip"
PKG_PATH="$OUT_DIR/$PKG_NAME"

export CGO_ENABLED=0
export GOOS=linux
export GOARCH=amd64

command -v go >/dev/null 2>&1 || { echo "ERROR: go not found"; exit 1; }
command -v npm >/dev/null 2>&1 || { echo "ERROR: npm not found"; exit 1; }
command -v zip >/dev/null 2>&1 || { echo "ERROR: zip not found"; exit 1; }

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR" "$ROOT_DIR/bin"

echo "[1/5] Building backend API binary"
(cd "$ROOT_DIR/backend" && go test ./... && go build -trimpath -ldflags="-s -w -X zxy-panel/backend/internal/version.Version=${FULL_VERSION}" -o "$ROOT_DIR/bin/zxy-panel-api-linux-amd64" ./cmd/server)
chmod +x "$ROOT_DIR/bin/zxy-panel-api-linux-amd64"

echo "[2/5] Building agent binary"
(cd "$ROOT_DIR/agent" && go test ./... && go build -trimpath -ldflags="-s -w -X main.version=${FULL_VERSION}" -o "$ROOT_DIR/bin/zxy-agent-linux-amd64" ./cmd/agent)
chmod +x "$ROOT_DIR/bin/zxy-agent-linux-amd64"

echo "[3/5] Building frontend dist"
(cd "$ROOT_DIR/frontend" && npm ci --no-audit --no-fund --progress=false && VITE_BASE_PATH=/ npm run build)

echo "[4/5] Packaging fast release"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
PKG_ROOT="$TMP_DIR/zxy-panel-v${VERSION}-${CODENAME}"
mkdir -p "$PKG_ROOT"
rsync -a \
  --exclude '.git' \
  --exclude '.github' \
  --exclude 'dist-release' \
  --exclude 'frontend/node_modules' \
  --exclude 'backend/tmp' \
  --exclude 'agent/tmp' \
  --exclude '*.tsbuildinfo' \
  "$ROOT_DIR/" "$PKG_ROOT/"

# Ensure fast assets are included.
test -x "$PKG_ROOT/bin/zxy-panel-api-linux-amd64"
test -x "$PKG_ROOT/bin/zxy-agent-linux-amd64"
test -f "$PKG_ROOT/frontend/dist/index.html"

(cd "$TMP_DIR" && zip -qr "$PKG_PATH" "$(basename "$PKG_ROOT")")

SHA256="$(sha256sum "$PKG_PATH" | awk '{print $1}')"
cat > "$OUT_DIR/version.fast.json" <<JSON
{
  "latest": "${FULL_VERSION}",
  "version": "${VERSION}",
  "codename": "${CODENAME}",
  "package": "${PKG_NAME}",
  "download_url": "REPLACE_WITH_GITHUB_RELEASE_DOWNLOAD_URL/${PKG_NAME}",
  "sha256": "${SHA256}",
  "min_supported_version": "0.7.4",
  "release_date": "$(date +%F)",
  "changelog": [
    "新增 fast install 默认路径：检测到预构建 API、Agent、frontend/dist 后自动跳过 Docker、Go 镜像、Node 镜像和 npm 现场构建",
    "保留 Docker 兼容模式：缺少预构建产物时自动回退旧安装方式",
    "Nginx 改为直接托管前端静态文件并反代 API/sub/s 路由",
    "CLI 支持 fast/systemd 模式下的 status、restart、logs、reset-password、uninstall",
    "Agent 安装优先使用内置二进制和内置 Xray，减少 GitHub 运行时下载依赖"
  ]
}
JSON

echo "[5/5] Done"
echo "Package: $PKG_PATH"
echo "SHA256:  $SHA256"
echo "Manifest template: $OUT_DIR/version.fast.json"
