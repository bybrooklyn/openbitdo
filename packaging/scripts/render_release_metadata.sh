#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  render_release_metadata.sh <tag> <repository> <input_dir> <output_dir>

Inputs expected in <input_dir>:
  openbitdo-<tag>-source.tar.gz
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

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

aur_pkgver_from_tag() {
  local version
  version="${1#v}"
  version="${version/-rc./rc}"
  version="${version/-alpha./alpha}"
  version="${version/-beta./beta}"
  echo "$version"
}

VERSION="${TAG#v}"
AUR_PKGVER="$(aur_pkgver_from_tag "$TAG")"

SOURCE_ARCHIVE="${INPUT_DIR}/openbitdo-${TAG}-source.tar.gz"
LINUX_X86_ARCHIVE="${INPUT_DIR}/openbitdo-${TAG}-linux-x86_64.tar.gz"
LINUX_AARCH64_ARCHIVE="${INPUT_DIR}/openbitdo-${TAG}-linux-aarch64.tar.gz"
MACOS_ARM64_ARCHIVE="${INPUT_DIR}/openbitdo-${TAG}-macos-arm64.tar.gz"

for required in \
  "$SOURCE_ARCHIVE" \
  "$LINUX_X86_ARCHIVE" \
  "$LINUX_AARCH64_ARCHIVE" \
  "$MACOS_ARM64_ARCHIVE"; do
  if [[ ! -f "$required" ]]; then
    echo "missing required release input: $required" >&2
    exit 1
  fi
done

SOURCE_SHA256="$(sha256 "$SOURCE_ARCHIVE")"
LINUX_X86_SHA256="$(sha256 "$LINUX_X86_ARCHIVE")"
LINUX_AARCH64_SHA256="$(sha256 "$LINUX_AARCH64_ARCHIVE")"
MACOS_ARM64_SHA256="$(sha256 "$MACOS_ARM64_ARCHIVE")"

mkdir -p \
  "${OUTPUT_DIR}/aur/openbitdo" \
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
    -e "s|@SOURCE_SHA256@|${SOURCE_SHA256}|g" \
    -e "s|@LINUX_X86_64_SHA256@|${LINUX_X86_SHA256}|g" \
    -e "s|@LINUX_AARCH64_SHA256@|${LINUX_AARCH64_SHA256}|g" \
    -e "s|@MACOS_ARM64_SHA256@|${MACOS_ARM64_SHA256}|g" \
    "$template" > "$destination"
}

render \
  "${ROOT}/packaging/aur/openbitdo/PKGBUILD.tmpl" \
  "${OUTPUT_DIR}/aur/openbitdo/PKGBUILD"
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
SOURCE_SHA256=${SOURCE_SHA256}
LINUX_X86_64_SHA256=${LINUX_X86_SHA256}
LINUX_AARCH64_SHA256=${LINUX_AARCH64_SHA256}
MACOS_ARM64_SHA256=${MACOS_ARM64_SHA256}
EOF

echo "rendered release metadata into ${OUTPUT_DIR}"
