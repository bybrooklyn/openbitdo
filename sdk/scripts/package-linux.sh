#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "$ROOT/.." && pwd)"
VERSION="${1:-v0.0.0-local}"
ARCH_LABEL="${2:-$(uname -m)}"
TARGET_TRIPLE="${3:-}"

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

DIST_ROOT="$ROOT/dist"
PKG_NAME="openbitdo-${VERSION}-linux-${ARCH_LABEL}"
STAGE_ROOT="$(mktemp -d)"
PKG_DIR="$STAGE_ROOT/$PKG_NAME"
BIN_ASSET="$DIST_ROOT/${PKG_NAME}"

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
  if [[ -n "$TARGET_TRIPLE" ]]; then
    cargo build --release -p openbitdo --target "$TARGET_TRIPLE"
    echo "$ROOT/target/$TARGET_TRIPLE/release/openbitdo"
  else
    cargo build --release -p openbitdo
    echo "$ROOT/target/release/openbitdo"
  fi
}

BIN_PATH="$(build_binary)"

mkdir -p "$PKG_DIR/bin" "$DIST_ROOT"

cp "$BIN_PATH" "$PKG_DIR/bin/openbitdo"
cp "$BIN_PATH" "$BIN_ASSET"
cp "$REPO_ROOT/README.md" "$PKG_DIR/README.md"
cp "$ROOT/README.md" "$PKG_DIR/SDK_README.md"
cp "$REPO_ROOT/LICENSE" "$PKG_DIR/LICENSE"

tar -C "$STAGE_ROOT" -czf "$DIST_ROOT/${PKG_NAME}.tar.gz" "$PKG_NAME"
rm -rf "$STAGE_ROOT"

checksum_file "$DIST_ROOT/${PKG_NAME}.tar.gz"
checksum_file "$BIN_ASSET"

echo "created package: $DIST_ROOT/${PKG_NAME}.tar.gz"
echo "created standalone binary: $BIN_ASSET"
