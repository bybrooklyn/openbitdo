#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "$ROOT/.." && pwd)"
VERSION="${1:-v0.0.1-rc.1}"
ARCH_LABEL="${2:-arm64}"
TARGET_TRIPLE="${3:-aarch64-apple-darwin}"
INSTALL_PREFIX="${4:-/opt/homebrew/bin}"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "package-macos.sh must run on macOS" >&2
  exit 1
fi

if [[ "$ARCH_LABEL" != "arm64" ]]; then
  echo "unsupported macOS arch label: $ARCH_LABEL (expected arm64)" >&2
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
  cargo build --release -p openbitdo --target "$TARGET_TRIPLE"
  echo "$ROOT/target/$TARGET_TRIPLE/release/openbitdo"
}

BIN_PATH="$(build_binary)"
VERSION_STRIPPED="${VERSION#v}"

rm -rf "$PKGROOT" "$PKG_ASSET"
mkdir -p "$PKG_DIR/bin" "$DIST_ROOT"

cp "$BIN_PATH" "$PKG_DIR/bin/openbitdo"
cp "$BIN_PATH" "$BIN_ASSET"
cp "$REPO_ROOT/README.md" "$PKG_DIR/README.md"
cp "$ROOT/README.md" "$PKG_DIR/SDK_README.md"
cp "$REPO_ROOT/LICENSE" "$PKG_DIR/LICENSE"

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
