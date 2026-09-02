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

RC_TAG="v0.1.0-rc.1"
RC_INPUT_DIR="$TMP/rc-input"
RC_OUTPUT_DIR="$TMP/rc-output"
create_inputs "$RC_TAG" "$RC_INPUT_DIR"

bash "$ROOT/scripts/render_release_metadata.sh" \
  "$RC_TAG" \
  "bybrooklyn/openbitdo" \
  "$RC_INPUT_DIR" \
  "$RC_OUTPUT_DIR"

PKGBUILD="$RC_OUTPUT_DIR/aur/openbitdo-bin/PKGBUILD"
FORMULA="$RC_OUTPUT_DIR/homebrew/Formula/openbitdo.rb"
CHECKSUMS="$RC_OUTPUT_DIR/checksums.env"

test -f "$PKGBUILD"
test -f "$FORMULA"
test -f "$CHECKSUMS"

expected_x86="$(sha256 "$RC_INPUT_DIR/openbitdo-${RC_TAG}-linux-x86_64.tar.gz")"
expected_aarch64="$(sha256 "$RC_INPUT_DIR/openbitdo-${RC_TAG}-linux-aarch64.tar.gz")"
expected_macos="$(sha256 "$RC_INPUT_DIR/openbitdo-${RC_TAG}-macos-arm64.tar.gz")"

grep -Fq "pkgver=0.1.0rc1" "$PKGBUILD"
grep -Fq "_upstream_tag=${RC_TAG}" "$PKGBUILD"
grep -Fq "depends=('glibc' 'systemd-libs')" "$PKGBUILD"
grep -Fq "sha256sums_x86_64=('${expected_x86}')" "$PKGBUILD"
grep -Fq "sha256sums_aarch64=('${expected_aarch64}')" "$PKGBUILD"
grep -Fq 'share/udev/rules.d/99-openbitdo.rules' "$PKGBUILD"
grep -Fq 'usr/lib/udev/rules.d/99-openbitdo.rules' "$PKGBUILD"
grep -Fq 'usr/share/bash-completion/completions/openbitdo' "$PKGBUILD"
grep -Fq 'usr/share/fish/vendor_completions.d/openbitdo.fish' "$PKGBUILD"
grep -Fq 'usr/share/zsh/site-functions/_openbitdo' "$PKGBUILD"

grep -Fq 'version "0.1.0-rc.1"' "$FORMULA"
grep -Fq "sha256 \"${expected_x86}\"" "$FORMULA"
grep -Fq "sha256 \"${expected_aarch64}\"" "$FORMULA"
grep -Fq "sha256 \"${expected_macos}\"" "$FORMULA"
grep -Fq "https://github.com/bybrooklyn/openbitdo/releases/download/${RC_TAG}/openbitdo-${RC_TAG}-linux-x86_64.tar.gz" "$FORMULA"
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

grep -Fq "TAG=${RC_TAG}" "$CHECKSUMS"
grep -Fq "VERSION=0.1.0-rc.1" "$CHECKSUMS"
grep -Fq "AUR_PKGVER=0.1.0rc1" "$CHECKSUMS"
grep -Fq "LINUX_X86_64_SHA256=${expected_x86}" "$CHECKSUMS"
grep -Fq "LINUX_AARCH64_SHA256=${expected_aarch64}" "$CHECKSUMS"
grep -Fq "MACOS_ARM64_SHA256=${expected_macos}" "$CHECKSUMS"

STABLE_TAG="v0.1.0"
STABLE_INPUT_DIR="$TMP/stable-input"
STABLE_OUTPUT_DIR="$TMP/stable-output"
create_inputs "$STABLE_TAG" "$STABLE_INPUT_DIR"
bash "$ROOT/scripts/render_release_metadata.sh" \
  "$STABLE_TAG" \
  "bybrooklyn/openbitdo" \
  "$STABLE_INPUT_DIR" \
  "$STABLE_OUTPUT_DIR"

grep -Fq 'pkgver=0.1.0' "$STABLE_OUTPUT_DIR/aur/openbitdo-bin/PKGBUILD"
grep -Fq 'version "0.1.0"' "$STABLE_OUTPUT_DIR/homebrew/Formula/openbitdo.rb"

# Use each package manager's real comparator when it is available. Arch's
# vercmp defines 0.1.0rc1 < 0.1.0, and Homebrew follows the same ordering.
if command -v vercmp >/dev/null 2>&1; then
  test "$(vercmp 0.1.0rc1 0.1.0)" -lt 0
fi
if command -v brew >/dev/null 2>&1; then
  brew ruby -e 'raise unless Version.new("0.1.0-rc.1") < Version.new("0.1.0")'
fi

EMPTY_INPUT_DIR="$TMP/empty-input"
EMPTY_OUTPUT_DIR="$TMP/empty-output"
create_inputs "$RC_TAG" "$EMPTY_INPUT_DIR"
: >"$EMPTY_INPUT_DIR/openbitdo-${RC_TAG}-linux-x86_64.tar.gz"
if bash "$ROOT/scripts/render_release_metadata.sh" \
  "$RC_TAG" \
  "bybrooklyn/openbitdo" \
  "$EMPTY_INPUT_DIR" \
  "$EMPTY_OUTPUT_DIR" >"$TMP/empty.out" 2>&1; then
  echo "metadata renderer accepted an empty release archive" >&2
  exit 1
fi
grep -Fq 'missing or empty required release input' "$TMP/empty.out"
