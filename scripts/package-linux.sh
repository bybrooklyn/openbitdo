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

DIST_ROOT="${OPENBITDO_DIST_ROOT:-$ROOT/dist}"
PKG_NAME="openbitdo-${VERSION}-linux-${ARCH_LABEL}"
STAGE_ROOT="$(mktemp -d)"
PKG_DIR="$STAGE_ROOT/$PKG_NAME"
BIN_ASSET="$DIST_ROOT/${PKG_NAME}"
trap 'rm -rf -- "$STAGE_ROOT"' EXIT

if command -v sha256sum >/dev/null 2>&1; then
  CHECKSUM_TOOL="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  CHECKSUM_TOOL="shasum"
else
  echo "no SHA-256 checksum tool found (need sha256sum or shasum)" >&2
  exit 1
fi

checksum_file() {
  local path="$1"
  local directory
  local basename

  directory="$(dirname "$path")"
  basename="$(basename "$path")"

  case "$CHECKSUM_TOOL" in
    sha256sum)
      (cd "$directory" && sha256sum "$basename" > "${basename}.sha256")
      ;;
    shasum)
      (cd "$directory" && shasum -a 256 "$basename" > "${basename}.sha256")
      ;;
  esac

  if [[ ! -s "${path}.sha256" ]]; then
    echo "checksum sidecar was not created: ${path}.sha256" >&2
    exit 1
  fi
}

build_binary() {
  local out="$1"
  local ldflags

  ldflags="$(GOOS=linux GOARCH="$GOARCH" "$ROOT/scripts/build_metadata.sh" "$VERSION")"
  (
    cd "$ROOT"
    CGO_ENABLED=1 GOOS=linux GOARCH="$GOARCH" go build \
      -ldflags "$ldflags" \
      -o "$out" ./cmd/openbitdo
  )
}

mkdir -p \
  "$PKG_DIR/bin" \
  "$PKG_DIR/share/bash-completion/completions" \
  "$PKG_DIR/share/fish/vendor_completions.d" \
  "$PKG_DIR/share/udev/rules.d" \
  "$PKG_DIR/share/zsh/site-functions" \
  "$DIST_ROOT"
build_binary "$PKG_DIR/bin/openbitdo"
chmod 755 "$PKG_DIR/bin/openbitdo"
cp "$PKG_DIR/bin/openbitdo" "$BIN_ASSET"
cp "$ROOT/README.md" "$PKG_DIR/README.md"
cp "$ROOT/LICENSE" "$PKG_DIR/LICENSE"
cp "$ROOT/packaging/linux/99-openbitdo.rules" "$PKG_DIR/share/udev/rules.d/99-openbitdo.rules"
cp "$ROOT/completions/openbitdo.bash" "$PKG_DIR/share/bash-completion/completions/openbitdo"
cp "$ROOT/completions/openbitdo.fish" "$PKG_DIR/share/fish/vendor_completions.d/openbitdo.fish"
cp "$ROOT/completions/openbitdo.zsh" "$PKG_DIR/share/zsh/site-functions/_openbitdo"

tar -C "$STAGE_ROOT" -czf "$DIST_ROOT/${PKG_NAME}.tar.gz" "$PKG_NAME"

checksum_file "$DIST_ROOT/${PKG_NAME}.tar.gz"
checksum_file "$BIN_ASSET"

echo "created package: $DIST_ROOT/${PKG_NAME}.tar.gz"
echo "created standalone binary: $BIN_ASSET"
