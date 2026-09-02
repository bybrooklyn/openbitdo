#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  render_release_metadata.sh <tag> <repository> <input_dir> <output_dir>

Inputs expected in <input_dir>:
  openbitdo-<tag>-linux-x86_64.tar.gz
  openbitdo-<tag>-linux-aarch64.tar.gz
  openbitdo-<tag>-macos-arm64.tar.gz
EOF
}

if [[ $# -ne 4 ]]; then
  usage >&2
  exit 1
fi

TAG="$1"
REPOSITORY="$2"
INPUT_DIR="$3"
OUTPUT_DIR="$4"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ ! "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$ ]]; then
  echo "unsupported release tag: $TAG (expected vMAJOR.MINOR.PATCH or vMAJOR.MINOR.PATCH-rc.N)" >&2
  exit 1
fi

if [[ ! "$REPOSITORY" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]; then
  echo "invalid repository name: $REPOSITORY (expected owner/repository)" >&2
  exit 1
fi

if [[ ! -d "$INPUT_DIR" ]]; then
  echo "release input directory not found: $INPUT_DIR" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  SHA256_TOOL="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  SHA256_TOOL="shasum"
else
  echo "no SHA-256 checksum tool found (need sha256sum or shasum)" >&2
  exit 1
fi

sha256() {
  case "$SHA256_TOOL" in
    sha256sum) sha256sum "$1" | awk '{print $1}' ;;
    shasum) shasum -a 256 "$1" | awk '{print $1}' ;;
  esac
}

aur_pkgver_from_tag() {
  local version
  version="${1#v}"
  version="${version/-rc./rc}"
  echo "$version"
}

VERSION="${TAG#v}"
AUR_PKGVER="$(aur_pkgver_from_tag "$TAG")"

LINUX_X86_ARCHIVE="${INPUT_DIR}/openbitdo-${TAG}-linux-x86_64.tar.gz"
LINUX_AARCH64_ARCHIVE="${INPUT_DIR}/openbitdo-${TAG}-linux-aarch64.tar.gz"
MACOS_ARM64_ARCHIVE="${INPUT_DIR}/openbitdo-${TAG}-macos-arm64.tar.gz"

for required in \
  "$LINUX_X86_ARCHIVE" \
  "$LINUX_AARCH64_ARCHIVE" \
  "$MACOS_ARM64_ARCHIVE"; do
  if [[ ! -s "$required" ]]; then
    echo "missing or empty required release input: $required" >&2
    exit 1
  fi
done

LINUX_X86_SHA256="$(sha256 "$LINUX_X86_ARCHIVE")"
LINUX_AARCH64_SHA256="$(sha256 "$LINUX_AARCH64_ARCHIVE")"
MACOS_ARM64_SHA256="$(sha256 "$MACOS_ARM64_ARCHIVE")"

mkdir -p \
  "${OUTPUT_DIR}/aur/openbitdo-bin" \
  "${OUTPUT_DIR}/homebrew/Formula"

render() {
  local template="$1"
  local destination="$2"
  sed \
    -e "s|@AUR_PKGVER@|${AUR_PKGVER}|g" \
    -e "s|@UPSTREAM_TAG@|${TAG}|g" \
    -e "s|@VERSION@|${VERSION}|g" \
    -e "s|@REPOSITORY@|${REPOSITORY}|g" \
    -e "s|@LINUX_X86_64_SHA256@|${LINUX_X86_SHA256}|g" \
    -e "s|@LINUX_AARCH64_SHA256@|${LINUX_AARCH64_SHA256}|g" \
    -e "s|@MACOS_ARM64_SHA256@|${MACOS_ARM64_SHA256}|g" \
    "$template" > "$destination"
}

render \
  "${ROOT}/packaging/aur/openbitdo-bin/PKGBUILD.tmpl" \
  "${OUTPUT_DIR}/aur/openbitdo-bin/PKGBUILD"
render \
  "${ROOT}/packaging/homebrew/Formula/openbitdo.rb.tmpl" \
  "${OUTPUT_DIR}/homebrew/Formula/openbitdo.rb"

cat > "${OUTPUT_DIR}/checksums.env" <<EOF
TAG=${TAG}
VERSION=${VERSION}
AUR_PKGVER=${AUR_PKGVER}
REPOSITORY=${REPOSITORY}
LINUX_X86_64_SHA256=${LINUX_X86_SHA256}
LINUX_AARCH64_SHA256=${LINUX_AARCH64_SHA256}
MACOS_ARM64_SHA256=${MACOS_ARM64_SHA256}
EOF

echo "rendered release metadata into ${OUTPUT_DIR}"
