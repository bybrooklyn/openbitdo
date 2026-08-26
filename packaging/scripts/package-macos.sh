#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${1:-v0.0.0-local}"
ARCH_LABEL="${2:-arm64}"
INSTALL_PREFIX="${3:-/opt/homebrew/bin}"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "package-macos.sh must run on macOS" >&2
  exit 1
fi

if [[ "$ARCH_LABEL" != "arm64" ]]; then
  echo "unsupported macOS arch label: $ARCH_LABEL (expected arm64)" >&2
  exit 1
fi

# karalabe/hid vendors hidapi+libusb and compiles them in via cgo, so this
# must run natively on Apple Silicon with CGO enabled - plain GOARCH
# cross-compilation does not work for this dependency.
HOST_ARCH="$(uname -m)"
if [[ "$HOST_ARCH" != "arm64" ]]; then
  echo "package-macos.sh requires a native arm64 host (found $HOST_ARCH); cgo dependencies cannot cross-compile" >&2
  exit 1
fi

DIST_ROOT="$ROOT/dist"
PKG_NAME="openbitdo-${VERSION}-macos-${ARCH_LABEL}"
STAGE_ROOT="$(mktemp -d)"
PKG_DIR="$STAGE_ROOT/$PKG_NAME"
BIN_ASSET="$DIST_ROOT/${PKG_NAME}"
PKG_ASSET="$DIST_ROOT/${PKG_NAME}.pkg"
PKGROOT="$DIST_ROOT/${PKG_NAME}-pkgroot"

checksum_file() {
  local path="$1"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$path" > "${path}.sha256"
  elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$path" > "${path}.sha256"
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
VERSION_STRIPPED="${VERSION#v}"

rm -rf "$PKGROOT" "$PKG_ASSET"
mkdir -p "$PKG_DIR/bin" "$DIST_ROOT"

cp "$BIN_PATH" "$PKG_DIR/bin/openbitdo"
cp "$BIN_PATH" "$BIN_ASSET"
cp "$ROOT/README.md" "$PKG_DIR/README.md"
cp "$ROOT/LICENSE" "$PKG_DIR/LICENSE"

tar -C "$STAGE_ROOT" -czf "$DIST_ROOT/${PKG_NAME}.tar.gz" "$PKG_NAME"
rm -rf "$STAGE_ROOT"

mkdir -p "$PKGROOT${INSTALL_PREFIX}"
cp "$BIN_PATH" "$PKGROOT${INSTALL_PREFIX}/openbitdo"
chmod 755 "$PKGROOT${INSTALL_PREFIX}/openbitdo"

pkgbuild \
  --root "$PKGROOT" \
  --identifier "io.openbitdo.cli" \
  --version "$VERSION_STRIPPED" \
  "$PKG_ASSET"

rm -rf "$PKGROOT"

checksum_file "$DIST_ROOT/${PKG_NAME}.tar.gz"
checksum_file "$BIN_ASSET"
checksum_file "$PKG_ASSET"

echo "created package: $DIST_ROOT/${PKG_NAME}.tar.gz"
echo "created standalone binary: $BIN_ASSET"
echo "created installer pkg: $PKG_ASSET"
