#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-0.7.5.9.1}"
CODENAME="qr-flow-compatibility-fix"
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
(cd "$ROOT_DIR/backend" && go test ./... && go build -trimpath -ldflags='-s -w' -o "$ROOT_DIR/bin/zxy-panel-api-linux-amd64" ./cmd/server)
chmod +x "$ROOT_DIR/bin/zxy-panel-api-linux-amd64"

echo "[2/5] Building agent binary"
(cd "$ROOT_DIR/agent" && go test ./... && go build -trimpath -ldflags='-s -w' -o "$ROOT_DIR/bin/zxy-agent-linux-amd64" ./cmd/agent)
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
  "latest": "${VERSION}-${CODENAME}-agent-xray",
  "version": "${VERSION}",
  "codename": "${CODENAME}",
  "package": "${PKG_NAME}",
  "download_url": "REPLACE_WITH_GITHUB_RELEASE_DOWNLOAD_URL/${PKG_NAME}",
  "sha256": "${SHA256}",
  "min_supported_version": "0.7.5",
  "release_date": "$(date +%F)",
  "changelog": [
    "修复客户分享弹窗二维码内容错误：V2rayN/Shadowrocket 默认二维码改为 vless:// 单节点链接",
    "VLESS Reality 分享链接不再强制添加 flow=xtls-rprx-vision，按服务端 Xray client.flow 保持一致，修复扫码后可导入但无法连接的问题",
    "继续保持单节点二维码为 vless://，订阅二维码与单节点二维码分离，HTTP 订阅仅保留在订阅标签并给出风险提示",
    "二维码改为本地生成白底图片，支持下载二维码图片后从客户端导入",
    "保留 V0.7.5.8.1 节点体检优化与升级任务修复"
  ]
}
JSON

echo "[5/5] Done"
echo "Package: $PKG_PATH"
echo "SHA256:  $SHA256"
echo "Manifest template: $OUT_DIR/version.fast.json"
