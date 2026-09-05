#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf -- "$TMP"' EXIT

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

create_inputs() {
  local tag="$1"
  local destination="$2"

  mkdir -p "$destination"
  printf 'x86_64 archive payload\n' >"$destination/openbitdo-${tag}-linux-x86_64.tar.gz"
  printf 'aarch64 archive payload\n' >"$destination/openbitdo-${tag}-linux-aarch64.tar.gz"
  printf 'macos archive payload\n' >"$destination/openbitdo-${tag}-macos-arm64.tar.gz"
}

assert_no_placeholders() {
  if grep -nE '@[A-Z0-9_]+@' "$@"; then
    echo "rendered metadata still contains template placeholders" >&2
    exit 1
  fi
}

RELEASE_TAG="v0.0.3"
RELEASE_INPUT_DIR="$TMP/release-input"
RELEASE_OUTPUT_DIR="$TMP/release-output"
create_inputs "$RELEASE_TAG" "$RELEASE_INPUT_DIR"

bash "$ROOT/scripts/render_release_metadata.sh" \
  "$RELEASE_TAG" \
  "bybrooklyn/openbitdo" \
  "$RELEASE_INPUT_DIR" \
  "$RELEASE_OUTPUT_DIR"

PKGBUILD="$RELEASE_OUTPUT_DIR/aur/openbitdo-bin/PKGBUILD"
FORMULA="$RELEASE_OUTPUT_DIR/homebrew/Formula/openbitdo.rb"
CHECKSUMS="$RELEASE_OUTPUT_DIR/checksums.env"

test -f "$PKGBUILD"
test -f "$FORMULA"
test -f "$CHECKSUMS"

expected_x86="$(sha256 "$RELEASE_INPUT_DIR/openbitdo-${RELEASE_TAG}-linux-x86_64.tar.gz")"
expected_aarch64="$(sha256 "$RELEASE_INPUT_DIR/openbitdo-${RELEASE_TAG}-linux-aarch64.tar.gz")"
expected_macos="$(sha256 "$RELEASE_INPUT_DIR/openbitdo-${RELEASE_TAG}-macos-arm64.tar.gz")"

grep -Fq "pkgver=0.0.3" "$PKGBUILD"
grep -Fq "_upstream_tag=${RELEASE_TAG}" "$PKGBUILD"
grep -Fq "depends=('glibc' 'systemd-libs')" "$PKGBUILD"
grep -Fq "sha256sums_x86_64=('${expected_x86}')" "$PKGBUILD"
grep -Fq "sha256sums_aarch64=('${expected_aarch64}')" "$PKGBUILD"
grep -Fq 'share/udev/rules.d/99-openbitdo.rules' "$PKGBUILD"
grep -Fq 'usr/lib/udev/rules.d/99-openbitdo.rules' "$PKGBUILD"
grep -Fq 'usr/share/bash-completion/completions/openbitdo' "$PKGBUILD"
grep -Fq 'usr/share/fish/vendor_completions.d/openbitdo.fish' "$PKGBUILD"
grep -Fq 'usr/share/zsh/site-functions/_openbitdo' "$PKGBUILD"

grep -Fq 'version "0.0.3"' "$FORMULA"
grep -Fq "sha256 \"${expected_x86}\"" "$FORMULA"
grep -Fq "sha256 \"${expected_aarch64}\"" "$FORMULA"
grep -Fq "sha256 \"${expected_macos}\"" "$FORMULA"
grep -Fq "https://github.com/bybrooklyn/openbitdo/releases/download/${RELEASE_TAG}/openbitdo-${RELEASE_TAG}-linux-x86_64.tar.gz" "$FORMULA"
grep -Fq 'depends_on arch: :arm64' "$FORMULA"
grep -Fq 'depends_on macos: :ventura' "$FORMULA"
grep -Fq 'bash_completion.install "share/bash-completion/completions/openbitdo"' "$FORMULA"
grep -Fq 'fish_completion.install "share/fish/vendor_completions.d/openbitdo.fish"' "$FORMULA"
grep -Fq 'zsh_completion.install "share/zsh/site-functions/_openbitdo"' "$FORMULA"

assert_no_placeholders "$PKGBUILD" "$FORMULA"
bash -n "$PKGBUILD"
if command -v ruby >/dev/null 2>&1; then
  ruby -c "$FORMULA" >/dev/null
fi
if command -v brew >/dev/null 2>&1; then
  brew style "$FORMULA"
fi

grep -Fq "TAG=${RELEASE_TAG}" "$CHECKSUMS"
grep -Fq "VERSION=0.0.3" "$CHECKSUMS"
grep -Fq "AUR_PKGVER=0.0.3" "$CHECKSUMS"
grep -Fq "LINUX_X86_64_SHA256=${expected_x86}" "$CHECKSUMS"
grep -Fq "LINUX_AARCH64_SHA256=${expected_aarch64}" "$CHECKSUMS"
grep -Fq "MACOS_ARM64_SHA256=${expected_macos}" "$CHECKSUMS"

# v0.0.3 must upgrade cleanly from the previously published v0.0.2 in both
# package managers. Use each one's real comparator when it is available.
PUBLISHED_VERSION="0.0.2"
RELEASE_VERSION="0.0.3"
if command -v vercmp >/dev/null 2>&1; then
  test "$(vercmp "$PUBLISHED_VERSION" "$RELEASE_VERSION")" -lt 0
fi
if command -v brew >/dev/null 2>&1; then
  brew ruby -e "raise unless Version.new(\"${PUBLISHED_VERSION}\") < Version.new(\"${RELEASE_VERSION}\")"
fi

EMPTY_INPUT_DIR="$TMP/empty-input"
EMPTY_OUTPUT_DIR="$TMP/empty-output"
create_inputs "$RELEASE_TAG" "$EMPTY_INPUT_DIR"
: >"$EMPTY_INPUT_DIR/openbitdo-${RELEASE_TAG}-linux-x86_64.tar.gz"
if bash "$ROOT/scripts/render_release_metadata.sh" \
  "$RELEASE_TAG" \
  "bybrooklyn/openbitdo" \
  "$EMPTY_INPUT_DIR" \
  "$EMPTY_OUTPUT_DIR" >"$TMP/empty.out" 2>&1; then
  echo "metadata renderer accepted an empty release archive" >&2
  exit 1
fi
grep -Fq 'missing or empty required release input' "$TMP/empty.out"
