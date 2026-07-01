#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-0.7.6.4}"
CODENAME="install-speed-polish"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="$ROOT_DIR/dist-release"
PKG_NAME="zxy-panel-v${VERSION}-${CODENAME}.zip"
PKG_PATH="$OUT_DIR/$PKG_NAME"

export CGO_ENABLED=0
export GOOS=linux
export GOARCH=amd64

command -v go >/dev/null 2>&1 || { echo "ERROR: go not found"; exit 1; }
command -v npm >/dev/null 2>&1 || { echo "ERROR: npm not found"; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo "ERROR: python3 not found"; exit 1; }

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
  --exclude 'releases' \
  --exclude 'data' \
  --exclude '*.zip' \
  --exclude '*.log' \
  --exclude 'frontend/node_modules' \
  --exclude 'backend/tmp' \
  --exclude 'agent/tmp' \
  --exclude '*.tsbuildinfo' \
  "$ROOT_DIR/" "$PKG_ROOT/"

# Ensure fast assets are included.
test -x "$PKG_ROOT/bin/zxy-panel-api-linux-amd64"
test -x "$PKG_ROOT/bin/zxy-agent-linux-amd64"
test -f "$PKG_ROOT/frontend/dist/index.html"

python3 - "$TMP_DIR" "$(basename "$PKG_ROOT")" "$PKG_PATH" <<'PYZIP'
import os, sys, zipfile
base, root_name, out = sys.argv[1], sys.argv[2], sys.argv[3]
root = os.path.join(base, root_name)
with zipfile.ZipFile(out, 'w', compression=zipfile.ZIP_DEFLATED, compresslevel=9) as zf:
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = sorted(dirnames)
        for filename in sorted(filenames):
            full = os.path.join(dirpath, filename)
            rel = os.path.relpath(full, base).replace(os.sep, '/')
            zf.write(full, rel)
PYZIP

SHA256="$(sha256sum "$PKG_PATH" | awk '{print $1}')"
cat > "$OUT_DIR/version.fast.json" <<JSON
{
  "latest": "${VERSION}-${CODENAME}-agent-xray",
  "version": "${VERSION}",
  "codename": "${CODENAME}",
  "package": "${PKG_NAME}",
  "download_url": "https://github.com/zoow1388-svg/zxy-panel/releases/download/v${VERSION}/${PKG_NAME}",
  "sha256": "${SHA256}",
  "min_supported_version": "0.7.5",
  "released_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "changelog": [
    "新增 zxy-panel doctor 安装后自检命令，集中检查 API、Agent、Xray、Nginx、面板端口反代、Xray 配置和升级清单",
    "安装完成后自动执行基础自检，方便快速判断一键安装是否真的成功",
    "继续保留 V0.7.6.2 已通过的一键安装、Linux ZIP 解压、Python 3.5 兼容、版本比较和禁止降级修复",
    "继续保留 V0.7.6.0 Base Stable 核心功能：Agent 空闲不反复重启 Xray、客户编辑可用、WebBasePath 刷新不空白、vless:// 二维码正常"
  ]
}
JSON

echo "[5/5] Done"
echo "Package: $PKG_PATH"
echo "SHA256:  $SHA256"
echo "Manifest template: $OUT_DIR/version.fast.json"
