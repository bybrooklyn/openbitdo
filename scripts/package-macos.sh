#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-v0.0.0-local}"
ARCH_LABEL="${2:-arm64}"

if [[ $# -gt 2 ]]; then
  echo "usage: package-macos.sh [version] [arm64]" >&2
  exit 1
fi

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "package-macos.sh must run on macOS" >&2
  exit 1
fi

if [[ "$ARCH_LABEL" != "arm64" ]]; then
  echo "unsupported macOS arch label: $ARCH_LABEL (expected arm64)" >&2
  exit 1
fi

DIST_ROOT="${OPENBITDO_DIST_ROOT:-$ROOT/dist}"
PKG_NAME="openbitdo-${VERSION}-macos-${ARCH_LABEL}"
STAGE_ROOT="$(mktemp -d)"
PKG_DIR="$STAGE_ROOT/$PKG_NAME"
BIN_ASSET="$DIST_ROOT/${PKG_NAME}"
PKG_ASSET="$DIST_ROOT/${PKG_NAME}.pkg"
PKGROOT="$STAGE_ROOT/pkgroot"
INSTALL_PREFIX="/usr/local"
PKGBUILD_STDERR="$STAGE_ROOT/pkgbuild.stderr"
PACKAGE_PAYLOAD_LISTING="$STAGE_ROOT/package-payload.list"
TAR_PAYLOAD_LISTING="$STAGE_ROOT/tar-payload.list"
trap 'rm -rf -- "$STAGE_ROOT"' EXIT

if command -v shasum >/dev/null 2>&1; then
  CHECKSUM_TOOL="shasum"
elif command -v sha256sum >/dev/null 2>&1; then
  CHECKSUM_TOOL="sha256sum"
else
  echo "no SHA-256 checksum tool found (need shasum or sha256sum)" >&2
  exit 1
fi

checksum_file() {
  local path="$1"
  local directory
  local basename

  directory="$(dirname "$path")"
  basename="$(basename "$path")"

  case "$CHECKSUM_TOOL" in
    shasum)
      (cd "$directory" && shasum -a 256 "$basename" > "${basename}.sha256")
      ;;
    sha256sum)
      (cd "$directory" && sha256sum "$basename" > "${basename}.sha256")
      ;;
  esac

  if [[ ! -s "${path}.sha256" ]]; then
    echo "checksum sidecar was not created: ${path}.sha256" >&2
    exit 1
  fi
}

VERSION_STRIPPED="${VERSION#v}"

build_binary() {
  local out="$1"
  local ldflags

  ldflags="$(GOOS=darwin GOARCH=arm64 "$ROOT/scripts/build_metadata.sh" "$VERSION")"
  (
    cd "$ROOT"
    CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 MACOSX_DEPLOYMENT_TARGET=13.0 go build \
      -ldflags "$ldflags" \
      -o "$out" ./cmd/openbitdo
  )
}

package_has_appledouble() {
  if ! pkgutil --payload-files "$1" >"$PACKAGE_PAYLOAD_LISTING"; then
    echo "unable to inspect macOS installer package payload" >&2
    exit 1
  fi
  grep -Eq '(^|/)\._' "$PACKAGE_PAYLOAD_LISTING"
}

sanitize_installer_payload() {
  local package="$1"
  local expanded="$STAGE_ROOT/pkg-expanded"
  local clean_package="$STAGE_ROOT/pkg-clean.pkg"
  local bom_listing="$STAGE_ROOT/Bom.list"
  local clean_bom="$STAGE_ROOT/Bom.clean"
  local payload_listing="$STAGE_ROOT/Payload.list"
  local clean_payload="$STAGE_ROOT/Payload.clean"
  local clean_package_info="$STAGE_ROOT/PackageInfo.clean"
  local payload_count

  for tool in pkgutil lsbom mkbom cpio gzip; do
    if ! command -v "$tool" >/dev/null 2>&1; then
      echo "cannot remove AppleDouble metadata: missing required tool $tool" >&2
      exit 1
    fi
  done

  pkgutil --expand "$package" "$expanded"

  # pkgbuild can serialize a protected xattr as virtual ._* AppleDouble
  # records even after xattr -c. Rebuild the BOM and cpio payload from the
  # actual staging tree so those metadata-only records cannot enter the pkg.
  lsbom "$expanded/Bom" \
    | awk '$1 !~ /(^|\/)\._/' >"$bom_listing"
  mkbom -i "$bom_listing" "$clean_bom"
  mv -f -- "$clean_bom" "$expanded/Bom"

  (
    cd "$PKGROOT"
    find . \( -name '._*' -o -name '.DS_Store' \) -prune -o -print \
      | LC_ALL=C sort
  ) >"$payload_listing"
  payload_count="$(wc -l <"$payload_listing" | tr -d '[:space:]')"

  (
    cd "$PKGROOT"
    COPYFILE_DISABLE=1 cpio -o --format odc --owner 0:0 \
      <"$payload_listing" 2>/dev/null \
      | gzip -9 >"$clean_payload"
  )
  mv -f -- "$clean_payload" "$expanded/Payload"

  sed -E \
    "s/numberOfFiles=\"[0-9]+\"/numberOfFiles=\"${payload_count}\"/" \
    "$expanded/PackageInfo" >"$clean_package_info"
  mv -f -- "$clean_package_info" "$expanded/PackageInfo"

  COPYFILE_DISABLE=1 pkgutil --flatten "$expanded" "$clean_package"
  mv -f -- "$clean_package" "$package"
}

rm -f -- "$PKG_ASSET"
mkdir -p \
  "$PKG_DIR/bin" \
  "$PKG_DIR/share/bash-completion/completions" \
  "$PKG_DIR/share/fish/vendor_completions.d" \
  "$PKG_DIR/share/zsh/site-functions" \
  "$DIST_ROOT"

build_binary "$PKG_DIR/bin/openbitdo"
chmod 755 "$PKG_DIR/bin/openbitdo"
cp "$PKG_DIR/bin/openbitdo" "$BIN_ASSET"
cp "$ROOT/README.md" "$PKG_DIR/README.md"
cp "$ROOT/LICENSE" "$PKG_DIR/LICENSE"
cp "$ROOT/completions/openbitdo.bash" "$PKG_DIR/share/bash-completion/completions/openbitdo"
cp "$ROOT/completions/openbitdo.fish" "$PKG_DIR/share/fish/vendor_completions.d/openbitdo.fish"
cp "$ROOT/completions/openbitdo.zsh" "$PKG_DIR/share/zsh/site-functions/_openbitdo"
xattr -cr "$PKG_DIR" "$BIN_ASSET"

COPYFILE_DISABLE=1 tar --no-xattrs \
  -C "$STAGE_ROOT" -czf "$DIST_ROOT/${PKG_NAME}.tar.gz" "$PKG_NAME"

if ! COPYFILE_DISABLE=1 tar -tzf "$DIST_ROOT/${PKG_NAME}.tar.gz" >"$TAR_PAYLOAD_LISTING"; then
  echo "unable to inspect macOS archive payload" >&2
  exit 1
fi
if grep -Eq '(^|/)\._' "$TAR_PAYLOAD_LISTING"; then
  echo "macOS archive contains forbidden AppleDouble entries" >&2
  exit 1
fi

mkdir -p \
  "$PKGROOT${INSTALL_PREFIX}/bin" \
  "$PKGROOT${INSTALL_PREFIX}/share/bash-completion/completions" \
  "$PKGROOT${INSTALL_PREFIX}/share/fish/vendor_completions.d" \
  "$PKGROOT${INSTALL_PREFIX}/share/zsh/site-functions"
cp "$BIN_ASSET" "$PKGROOT${INSTALL_PREFIX}/bin/openbitdo"
chmod 755 "$PKGROOT${INSTALL_PREFIX}/bin/openbitdo"
cp "$ROOT/completions/openbitdo.bash" "$PKGROOT${INSTALL_PREFIX}/share/bash-completion/completions/openbitdo"
cp "$ROOT/completions/openbitdo.fish" "$PKGROOT${INSTALL_PREFIX}/share/fish/vendor_completions.d/openbitdo.fish"
cp "$ROOT/completions/openbitdo.zsh" "$PKGROOT${INSTALL_PREFIX}/share/zsh/site-functions/_openbitdo"
xattr -cr "$PKGROOT"

if ! COPYFILE_DISABLE=1 pkgbuild \
  --root "$PKGROOT" \
  --identifier "io.openbitdo.cli" \
  --version "$VERSION_STRIPPED" \
  --min-os-version "13.0" \
  "$PKG_ASSET" 2>"$PKGBUILD_STDERR"; then
  cat "$PKGBUILD_STDERR" >&2
  exit 1
fi

if package_has_appledouble "$PKG_ASSET"; then
  sanitize_installer_payload "$PKG_ASSET"
fi
if package_has_appledouble "$PKG_ASSET"; then
  echo "macOS installer package contains forbidden AppleDouble entries" >&2
  exit 1
fi

# A protected provenance xattr can make pkgbuild report these copyfile writes
# even though it returns success. They are expected only when the clean-payload
# rebuild above was required; preserve every other diagnostic.
if [[ -s "$PKGBUILD_STDERR" ]]; then
  grep -vFx 'write: Permission denied' "$PKGBUILD_STDERR" >&2 || true
fi

checksum_file "$DIST_ROOT/${PKG_NAME}.tar.gz"
checksum_file "$BIN_ASSET"
checksum_file "$PKG_ASSET"

echo "created package: $DIST_ROOT/${PKG_NAME}.tar.gz"
echo "created standalone binary: $BIN_ASSET"
echo "created installer pkg: $PKG_ASSET"
