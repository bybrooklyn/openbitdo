#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

INPUT_DIR="$TMP/input"
OUTPUT_DIR="$TMP/output"
mkdir -p "$INPUT_DIR"

printf 'x86_64 archive payload\n' >"$INPUT_DIR/openbitdo-v0.0.1-rc.4-linux-x86_64.tar.gz"
printf 'aarch64 archive payload\n' >"$INPUT_DIR/openbitdo-v0.0.1-rc.4-linux-aarch64.tar.gz"
printf 'macos archive payload\n' >"$INPUT_DIR/openbitdo-v0.0.1-rc.4-macos-arm64.tar.gz"

bash "$ROOT/packaging/scripts/render_release_metadata.sh" \
  "v0.0.1-rc.4" \
  "bybrooklyn/openbitdo" \
  "$INPUT_DIR" \
  "$OUTPUT_DIR"

PKGBUILD="$OUTPUT_DIR/aur/openbitdo-bin/PKGBUILD"
FORMULA="$OUTPUT_DIR/homebrew/Formula/openbitdo.rb"
CHECKSUMS="$OUTPUT_DIR/checksums.env"

test -f "$PKGBUILD"
test -f "$FORMULA"
test -f "$CHECKSUMS"

expected_x86="$(sha256 "$INPUT_DIR/openbitdo-v0.0.1-rc.4-linux-x86_64.tar.gz")"
expected_aarch64="$(sha256 "$INPUT_DIR/openbitdo-v0.0.1-rc.4-linux-aarch64.tar.gz")"
expected_macos="$(sha256 "$INPUT_DIR/openbitdo-v0.0.1-rc.4-macos-arm64.tar.gz")"

grep -Fq "pkgver=0.0.1rc4" "$PKGBUILD"
grep -Fq "_upstream_tag=v0.0.1-rc.4" "$PKGBUILD"
grep -Fq "sha256sums_x86_64=('${expected_x86}')" "$PKGBUILD"
grep -Fq "sha256sums_aarch64=('${expected_aarch64}')" "$PKGBUILD"

grep -Fq 'version "0.0.1-rc.4"' "$FORMULA"
grep -Fq "sha256 \"${expected_x86}\"" "$FORMULA"
grep -Fq "sha256 \"${expected_aarch64}\"" "$FORMULA"
grep -Fq "sha256 \"${expected_macos}\"" "$FORMULA"
grep -Fq 'https://github.com/bybrooklyn/openbitdo/releases/download/v0.0.1-rc.4/openbitdo-v0.0.1-rc.4-linux-x86_64.tar.gz' "$FORMULA"

if grep -nE '@[A-Z0-9_]+@' "$PKGBUILD" "$FORMULA"; then
  echo "rendered metadata still contains template placeholders" >&2
  exit 1
fi

grep -Fq "TAG=v0.0.1-rc.4" "$CHECKSUMS"
grep -Fq "VERSION=0.0.1-rc.4" "$CHECKSUMS"
grep -Fq "AUR_PKGVER=0.0.1rc4" "$CHECKSUMS"
grep -Fq "LINUX_X86_64_SHA256=${expected_x86}" "$CHECKSUMS"
grep -Fq "LINUX_AARCH64_SHA256=${expected_aarch64}" "$CHECKSUMS"
grep -Fq "MACOS_ARM64_SHA256=${expected_macos}" "$CHECKSUMS"
