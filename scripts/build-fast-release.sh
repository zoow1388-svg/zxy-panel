#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-0.7.6.1}"
CODENAME="${2:-zip-path-install-fix}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="$ROOT_DIR/dist-release"
PKG_NAME="zxy-panel-v${VERSION}-${CODENAME}.zip"
PKG_PATH="$OUT_DIR/$PKG_NAME"

export CGO_ENABLED=0

command -v go >/dev/null 2>&1 || { echo "ERROR: go not found"; exit 1; }
if command -v npm.cmd >/dev/null 2>&1; then
  NPM_BIN="$(command -v npm.cmd)"
elif command -v npm >/dev/null 2>&1; then
  NPM_BIN="$(command -v npm)"
else
  echo "ERROR: npm not found"
  exit 1
fi
if ! command -v rsync >/dev/null 2>&1 && ! command -v tar >/dev/null 2>&1; then
  echo "ERROR: rsync not found and tar fallback is unavailable"
  exit 1
fi
if ! command -v zip >/dev/null 2>&1 && ! command -v powershell.exe >/dev/null 2>&1; then
  echo "ERROR: zip not found and powershell.exe fallback is unavailable"
  exit 1
fi

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR" "$ROOT_DIR/bin"

echo "[1/5] Building backend API binary"
(cd "$ROOT_DIR/backend" && go test ./... && GOOS=linux GOARCH=amd64 go build -buildvcs=false -trimpath -ldflags='-s -w' -o "$ROOT_DIR/bin/zxy-panel-api-linux-amd64" ./cmd/server)
chmod +x "$ROOT_DIR/bin/zxy-panel-api-linux-amd64"

echo "[2/5] Building agent binary"
(cd "$ROOT_DIR/agent" && GOOS=linux GOARCH=amd64 go test ./... && GOOS=linux GOARCH=amd64 go build -buildvcs=false -trimpath -ldflags='-s -w' -o "$ROOT_DIR/bin/zxy-agent-linux-amd64" ./cmd/agent)
chmod +x "$ROOT_DIR/bin/zxy-agent-linux-amd64"

echo "[3/5] Building frontend dist"
(cd "$ROOT_DIR/frontend" && "$NPM_BIN" ci --no-audit --no-fund --progress=false && VITE_BASE_PATH=/ "$NPM_BIN" run build)

echo "[4/5] Packaging fast release"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
PKG_ROOT="$TMP_DIR/zxy-panel-v${VERSION}-${CODENAME}"
mkdir -p "$PKG_ROOT"

EXCLUDES=(
  --exclude '.git'
  --exclude '.github'
  --exclude '.agents'
  --exclude '.codex'
  --exclude '.codex-release-inspect'
  --exclude 'dist-release'
  --exclude 'releases'
  --exclude 'data'
  --exclude 'backups'
  --exclude '*.zip'
  --exclude '*.log'
  --exclude 'release-clean-audit*.txt'
  --exclude 'SHA256SUMS*'
  --exclude 'frontend/node_modules'
  --exclude 'backend/tmp'
  --exclude 'agent/tmp'
  --exclude '*.tsbuildinfo'
)

if command -v rsync >/dev/null 2>&1; then
  rsync -a "${EXCLUDES[@]}" "$ROOT_DIR/" "$PKG_ROOT/"
else
  (cd "$ROOT_DIR" && tar "${EXCLUDES[@]}" -cf - .) | (cd "$PKG_ROOT" && tar -xf -)
fi

test -f "$PKG_ROOT/bin/zxy-panel-api-linux-amd64"
test -f "$PKG_ROOT/bin/zxy-agent-linux-amd64"
test -f "$PKG_ROOT/frontend/dist/index.html"

if command -v zip >/dev/null 2>&1; then
  (cd "$TMP_DIR" && zip -qr "$PKG_PATH" "$(basename "$PKG_ROOT")")
else
  WIN_ROOT="$(cygpath -w "$PKG_ROOT")"
  WIN_DEST="$(cygpath -w "$PKG_PATH")"
  PS_ZIP_SCRIPT="$TMP_DIR/create-release-zip.ps1"
  WIN_PS_ZIP_SCRIPT="$(cygpath -w "$PS_ZIP_SCRIPT")"
  cat > "$PS_ZIP_SCRIPT" <<'PS'
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem

$root = $env:ZXY_ZIP_ROOT
$dest = $env:ZXY_ZIP_DEST
$rootName = [IO.Path]::GetFileName($root)

if (Test-Path -LiteralPath $dest) {
  Remove-Item -LiteralPath $dest -Force
}

$zip = [System.IO.Compression.ZipFile]::Open($dest, [System.IO.Compression.ZipArchiveMode]::Create)
try {
  Get-ChildItem -LiteralPath $root -Recurse -File | ForEach-Object {
    $rel = $_.FullName.Substring($root.Length).TrimStart([IO.Path]::DirectorySeparatorChar).TrimStart([IO.Path]::AltDirectorySeparatorChar)
    $entry = $rootName + '/' + $rel.Replace('\', '/')
    [System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, $_.FullName, $entry, [System.IO.Compression.CompressionLevel]::Optimal) | Out-Null
  }
}
finally {
  $zip.Dispose()
}
PS
  ZXY_ZIP_ROOT="$WIN_ROOT" ZXY_ZIP_DEST="$WIN_DEST" powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$WIN_PS_ZIP_SCRIPT" >/dev/null
fi

SHA256="$(sha256sum "$PKG_PATH" | awk '{print $1}')"
printf "%s  %s\n" "$SHA256" "$PKG_NAME" > "$OUT_DIR/SHA256SUMS"
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
    "Fix Windows-built release ZIP entries so Linux unzip can extract install paths correctly",
    "Normalize release ZIP entry names to forward slash paths",
    "Keep the default ZXY_UPDATE_MANIFEST_URL write and numeric upgrade version comparison fixes",
    "Keep the stable Agent idle behavior, client edit, WebBasePath refresh, and vless QR fixes"
  ]
}
JSON

echo "[5/5] Done"
echo "Package: $PKG_PATH"
echo "SHA256:  $SHA256"
echo "SHA256SUMS: $OUT_DIR/SHA256SUMS"
echo "Manifest template: $OUT_DIR/version.fast.json"
