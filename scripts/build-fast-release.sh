#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SOURCE_VERSION="$(tr -d '\r\n' < "$ROOT_DIR/VERSION")"
SOURCE_NUMBER="${SOURCE_VERSION%%-*}"
SOURCE_CODENAME="${SOURCE_VERSION#*-}"
VERSION="${1:-$SOURCE_NUMBER}"
CODENAME="${2:-$SOURCE_CODENAME}"
if [[ "$VERSION-$CODENAME" != "$SOURCE_VERSION" ]]; then
  echo "ERROR: build version $VERSION-$CODENAME does not match VERSION ($SOURCE_VERSION)"
  exit 1
fi
command -v node >/dev/null 2>&1 || { echo "ERROR: node not found"; exit 1; }
node "$ROOT_DIR/scripts/check-version-consistency.mjs" --mode dev
OUT_DIR="$ROOT_DIR/dist-release"
PKG_NAME="zxy-panel-v${VERSION}-${CODENAME}.zip"
PKG_PATH="$OUT_DIR/$PKG_NAME"
PYTHON_BIN="${PYTHON3:-python3}"
TARGET_GOOS=linux
TARGET_GOARCH=amd64
HOST_GOOS="$(go env GOOS)"

command -v go >/dev/null 2>&1 || { echo "ERROR: go not found"; exit 1; }
command -v npm >/dev/null 2>&1 || { echo "ERROR: npm not found"; exit 1; }
if [[ ! -x "$PYTHON_BIN" ]] && ! command -v "$PYTHON_BIN" >/dev/null 2>&1; then
  echo "ERROR: python3 not found"
  exit 1
fi

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR" "$ROOT_DIR/bin"

echo "[1/5] Building backend API binary"
if [[ "$HOST_GOOS" == "linux" ]]; then
  (cd "$ROOT_DIR/backend" && go test ./...)
else
  echo "Non-Linux host: running backend tests locally before Linux cross-build."
  (cd "$ROOT_DIR/backend" && go test ./...)
fi
(cd "$ROOT_DIR/backend" && CGO_ENABLED=0 GOOS="$TARGET_GOOS" GOARCH="$TARGET_GOARCH" go build -trimpath -ldflags='-s -w' -o "$ROOT_DIR/bin/zxy-panel-api-linux-amd64" ./cmd/server)
chmod +x "$ROOT_DIR/bin/zxy-panel-api-linux-amd64"

echo "[2/5] Building agent binary"
if [[ "$HOST_GOOS" == "linux" ]]; then
  (cd "$ROOT_DIR/agent" && go test ./...)
else
  echo "Non-Linux host: Agent runtime tests require Linux; validating with a Linux amd64 cross-build."
fi
(cd "$ROOT_DIR/agent" && CGO_ENABLED=0 GOOS="$TARGET_GOOS" GOARCH="$TARGET_GOARCH" go build -trimpath -ldflags='-s -w' -o "$ROOT_DIR/bin/zxy-agent-linux-amd64" ./cmd/agent)
chmod +x "$ROOT_DIR/bin/zxy-agent-linux-amd64"

echo "[3/5] Building frontend dist"
(cd "$ROOT_DIR/frontend" && npm ci --no-audit --no-fund --progress=false && VITE_BASE_PATH=/ npm run build)

echo "[4/5] Packaging fast release"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
PKG_ROOT="$TMP_DIR/zxy-panel-v${VERSION}-${CODENAME}"
mkdir -p "$PKG_ROOT"

if command -v rsync >/dev/null 2>&1; then
  rsync -a \
    --exclude '.git' \
    --exclude '.github' \
    --exclude 'dist-release' \
    --exclude 'releases' \
    --exclude 'data' \
    --exclude 'version.json' \
    --exclude '*.zip' \
    --exclude '*.log' \
    --exclude 'frontend/node_modules' \
    --exclude 'backend/tmp' \
    --exclude 'agent/tmp' \
    --exclude 'video_review_frames' \
    --exclude '*.tsbuildinfo' \
    "$ROOT_DIR/" "$PKG_ROOT/"
else
  echo "rsync not found; using a temporary copy fallback."
  cp -a "$ROOT_DIR/." "$PKG_ROOT/"
  rm -rf \
    "$PKG_ROOT/.git" \
    "$PKG_ROOT/.github" \
    "$PKG_ROOT/dist-release" \
    "$PKG_ROOT/releases" \
    "$PKG_ROOT/data" \
    "$PKG_ROOT/frontend/node_modules" \
    "$PKG_ROOT/backend/tmp" \
    "$PKG_ROOT/agent/tmp" \
    "$PKG_ROOT/video_review_frames"
  find "$PKG_ROOT" -type f \( -name '*.zip' -o -name '*.log' -o -name '*.tsbuildinfo' -o -name 'version.json' \) -delete
fi

test -f "$PKG_ROOT/bin/zxy-panel-api-linux-amd64"
test -f "$PKG_ROOT/bin/zxy-agent-linux-amd64"
test -f "$PKG_ROOT/frontend/dist/index.html"
cat > "$PKG_ROOT/PACKAGE-MANIFEST.json" <<JSON_PACKAGE
{
  "version": "${VERSION}",
  "codename": "${CODENAME}",
  "latest": "${SOURCE_VERSION}",
  "package": "${PKG_NAME}",
  "note": "Release SHA256 is published outside the archive. This package intentionally omits version.json to avoid a self-referential hash."
}
JSON_PACKAGE

"$PYTHON_BIN" - "$TMP_DIR" "$(basename "$PKG_ROOT")" "$PKG_PATH" <<'PYZIP'
import os
import stat
import sys
import zipfile

base, root_name, out = sys.argv[1], sys.argv[2], sys.argv[3]
root = os.path.join(base, root_name)
with zipfile.ZipFile(out, 'w', compression=zipfile.ZIP_DEFLATED, compresslevel=9) as zf:
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = sorted(dirnames)
        for filename in sorted(filenames):
            full = os.path.join(dirpath, filename)
            rel = os.path.relpath(full, base).replace(os.sep, '/')
            info = zipfile.ZipInfo.from_file(full, rel)
            if rel.endswith(('.sh', '/zxy-panel', '/zxy-netopt')) or '/bin/' in rel:
                info.external_attr = (stat.S_IFREG | 0o755) << 16
            with open(full, 'rb') as source:
                zf.writestr(info, source.read(), compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)
PYZIP

SHA256="$(sha256sum "$PKG_PATH" | awk '{print $1}')"
printf '%s  %s\n' "$SHA256" "$PKG_NAME" > "$OUT_DIR/SHA256SUMS"
cat > "$OUT_DIR/version.fast.json" <<JSON
{
  "latest": "${SOURCE_VERSION}",
  "version": "${VERSION}",
  "codename": "${CODENAME}",
  "package": "${PKG_NAME}",
  "download_url": "https://github.com/zoow1388-svg/zxy-panel/releases/download/v${VERSION}/${PKG_NAME}",
  "sha256": "${SHA256}",
  "min_supported_version": "0.7.5",
  "released_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "changelog": [
    "Synchronize source, frontend, backend, Agent, and installer version metadata.",
    "Distinguish development and release version consistency checks.",
    "Preserve existing BBR work and core network behavior."
  ]
}
JSON
node "$ROOT_DIR/scripts/check-version-consistency.mjs" --mode release \
  --manifest "$OUT_DIR/version.fast.json" --package "$PKG_PATH"

echo "[5/5] Done"
echo "Package: $PKG_PATH"
echo "SHA256:  $SHA256"
echo "Manifest template: $OUT_DIR/version.fast.json"
