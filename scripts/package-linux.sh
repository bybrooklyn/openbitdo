#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-v0.0.0-local}"
ARCH_LABEL="${2:-$(uname -m)}"

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "package-linux.sh must run on Linux" >&2
  exit 1
fi

case "$ARCH_LABEL" in
  x86_64) GOARCH="amd64" ;;
  aarch64) GOARCH="arm64" ;;
  arm64) ARCH_LABEL="aarch64"; GOARCH="arm64" ;;
  *)
    echo "unsupported linux arch label: $ARCH_LABEL" >&2
    exit 1
    ;;
esac

DIST_ROOT="$ROOT/dist"
PKG_NAME="openbitdo-${VERSION}-linux-${ARCH_LABEL}"
STAGE_ROOT="$(mktemp -d)"
PKG_DIR="$STAGE_ROOT/$PKG_NAME"
BIN_ASSET="$DIST_ROOT/${PKG_NAME}"

checksum_file() {
  local path="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$path" > "${path}.sha256"
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$path" > "${path}.sha256"
  else
    echo "warning: no checksum tool found for $path" >&2
  fi
}

GIT_COMMIT="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

build_binary() {
  local out="$1"
  (
    cd "$ROOT"
    CGO_ENABLED=1 GOOS=linux GOARCH="$GOARCH" go build \
      -ldflags "-X main.appVersion=${VERSION} -X main.gitCommit=${GIT_COMMIT} -X main.buildDate=${BUILD_DATE}" \
      -o "$out" ./cmd/openbitdo
  )
}

mkdir -p "$PKG_DIR/bin" "$DIST_ROOT"
build_binary "$PKG_DIR/bin/openbitdo"
cp "$PKG_DIR/bin/openbitdo" "$BIN_ASSET"
cp "$ROOT/README.md" "$PKG_DIR/README.md"
cp "$ROOT/LICENSE" "$PKG_DIR/LICENSE"

tar -C "$STAGE_ROOT" -czf "$DIST_ROOT/${PKG_NAME}.tar.gz" "$PKG_NAME"
rm -rf "$STAGE_ROOT"

checksum_file "$DIST_ROOT/${PKG_NAME}.tar.gz"
checksum_file "$BIN_ASSET"

echo "created package: $DIST_ROOT/${PKG_NAME}.tar.gz"
echo "created standalone binary: $BIN_ASSET"
