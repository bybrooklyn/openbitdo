#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${1:-v0.0.0-local}"
ARCH_LABEL="${2:-$(uname -m)}"

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "package-linux.sh must run on Linux" >&2
  exit 1
fi

case "$ARCH_LABEL" in
  x86_64|aarch64) ;;
  arm64) ARCH_LABEL="aarch64" ;;
  *)
    echo "unsupported linux arch label: $ARCH_LABEL" >&2
    exit 1
    ;;
esac

# karalabe/hid vendors hidapi+libusb and compiles them in via cgo, so builds
# must be native (this script is expected to run on a runner whose host arch
# already matches $ARCH_LABEL) with CGO enabled - plain GOOS/GOARCH
# cross-compilation does not work for this dependency.
HOST_ARCH="$(uname -m)"
if [[ "$HOST_ARCH" != "$ARCH_LABEL" ]]; then
  echo "package-linux.sh requires a native $ARCH_LABEL host (found $HOST_ARCH); cgo dependencies cannot cross-compile" >&2
  exit 1
fi

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

build_binary() {
  cd "$ROOT"
  local commit build_date out
  commit="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
  build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  out="$(mktemp -d)/openbitdo"
  CGO_ENABLED=1 go build -trimpath \
    -ldflags "-X main.appVersion=${VERSION} -X main.gitCommit=${commit} -X main.buildDate=${build_date}" \
    -o "$out" ./cmd/openbitdo
  echo "$out"
}

BIN_PATH="$(build_binary)"

mkdir -p "$PKG_DIR/bin" "$DIST_ROOT"

cp "$BIN_PATH" "$PKG_DIR/bin/openbitdo"
cp "$BIN_PATH" "$BIN_ASSET"
cp "$ROOT/README.md" "$PKG_DIR/README.md"
cp "$ROOT/LICENSE" "$PKG_DIR/LICENSE"

tar -C "$STAGE_ROOT" -czf "$DIST_ROOT/${PKG_NAME}.tar.gz" "$PKG_NAME"
rm -rf "$STAGE_ROOT"

checksum_file "$DIST_ROOT/${PKG_NAME}.tar.gz"
checksum_file "$BIN_ASSET"

echo "created package: $DIST_ROOT/${PKG_NAME}.tar.gz"
echo "created standalone binary: $BIN_ASSET"
