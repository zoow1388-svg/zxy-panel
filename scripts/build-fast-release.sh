#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-0.7.6.2}"
CODENAME="clean-release-fix"
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
  "download_url": "REPLACE_WITH_GITHUB_RELEASE_DOWNLOAD_URL/${PKG_NAME}",
  "sha256": "${SHA256}",
  "min_supported_version": "0.7.5",
  "release_date": "$(date +%F)",
  "changelog": [
    "修复 Agent 空闲状态反复下发配置导致 Xray 周期性重启的问题",
    "修复客户管理编辑按钮无明显响应，点击编辑会自动展开高级客户表单并加载当前客户信息",
    "修复 WebBasePath 下前端路由刷新空白问题，支持 /dashboard、/clients 等页面直接刷新",
    "保留 V0.7.5.9.1 的 vless:// 单节点二维码、flow 兼容修复和 HTTP 订阅分离逻辑",
    "作为基础版稳定候选，保留 fast/systemd、网络策略、节点体检和托管升级中心"
  ]
}
JSON

echo "[5/5] Done"
echo "Package: $PKG_PATH"
echo "SHA256:  $SHA256"
echo "Manifest template: $OUT_DIR/version.fast.json"
