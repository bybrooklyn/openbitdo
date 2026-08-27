#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf -- "$TMP"' EXIT

MOCK_BIN="$TMP/mock-bin"
mkdir -p "$MOCK_BIN"

cat >"$MOCK_BIN/uname" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  -s) printf '%s\n' "${MOCK_UNAME_S:?}" ;;
  -m) printf '%s\n' "${MOCK_UNAME_M:-arm64}" ;;
  *) echo "unexpected uname arguments: $*" >&2; exit 1 ;;
esac
EOF

cat >"$MOCK_BIN/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

printf 'GOOS=%s GOARCH=%s CGO_ENABLED=%s MACOSX_DEPLOYMENT_TARGET=%s\n' \
  "${GOOS:-}" "${GOARCH:-}" "${CGO_ENABLED:-}" "${MACOSX_DEPLOYMENT_TARGET:-}" \
  >>"${MOCK_GO_LOG:?}"
printf 'args:' >>"$MOCK_GO_LOG"
printf ' <%s>' "$@" >>"$MOCK_GO_LOG"
printf '\n' >>"$MOCK_GO_LOG"

output=""
while [[ $# -gt 0 ]]; do
  if [[ "$1" == "-o" ]]; then
    output="$2"
    break
  fi
  shift
done
if [[ -z "$output" ]]; then
  echo "mock go did not receive -o" >&2
  exit 1
fi

mkdir -p "$(dirname "$output")"
printf '#!/usr/bin/env sh\nprintf "mock openbitdo\\n"\n' >"$output"
chmod 755 "$output"
EOF

cat >"$MOCK_BIN/pkgbuild" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

root=""
identifier=""
version=""
minimum_os=""
destination=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --root) root="$2"; shift 2 ;;
    --identifier) identifier="$2"; shift 2 ;;
    --version) version="$2"; shift 2 ;;
    --min-os-version) minimum_os="$2"; shift 2 ;;
    *) destination="$1"; shift ;;
  esac
done

if [[ -z "$root" || -z "$identifier" || -z "$version" || -z "$minimum_os" || -z "$destination" ]]; then
  echo "mock pkgbuild received incomplete arguments" >&2
  exit 1
fi

find "$root" -type f -print \
  | sed "s|^${root}/||" \
  | LC_ALL=C sort >"${MOCK_PKGROOT_LIST:?}"
printf 'identifier=%s\nversion=%s\nminimum_os=%s\n' \
  "$identifier" "$version" "$minimum_os" >"${MOCK_PKGBUILD_LOG:?}"
printf 'mock macOS installer package\n' >"$destination"
EOF

cat >"$MOCK_BIN/pkgutil" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" != "--payload-files" || $# -ne 2 ]]; then
  echo "unexpected mock pkgutil arguments: $*" >&2
  exit 1
fi
printf '.\n'
sed 's|^|./|' "${MOCK_PKGROOT_LIST:?}"
EOF

cat >"$MOCK_BIN/xattr" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${MOCK_XATTR_LOG:?}"
EOF

chmod 755 \
  "$MOCK_BIN/uname" \
  "$MOCK_BIN/go" \
  "$MOCK_BIN/pkgbuild" \
  "$MOCK_BIN/pkgutil" \
  "$MOCK_BIN/xattr"

assert_checksum() {
  local asset="$1"
  local sidecar="${asset}.sha256"
  local asset_name

  asset_name="$(basename "$asset")"
  test -s "$sidecar"
  grep -Eq "^[0-9a-fA-F]{64} [ *]${asset_name}$" "$sidecar"
  if grep -Fq / "$sidecar"; then
    echo "checksum sidecar contains a path instead of a basename: $sidecar" >&2
    exit 1
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$(dirname "$asset")" && sha256sum -c "$(basename "$sidecar")" >/dev/null)
  else
    (cd "$(dirname "$asset")" && shasum -a 256 -c "$(basename "$sidecar")" >/dev/null)
  fi
}

assert_archive_files() {
  local archive="$1"
  local expected="$2"
  local actual="$TMP/archive-files.txt"

  tar -tzf "$archive" \
    | sed -e '/\/$/d' -e 's|^[^/]*/||' \
    | LC_ALL=C sort >"$actual"
  diff -u "$expected" "$actual"
}

assert_no_appledouble() {
  local description="$1"
  shift
  local listing="$TMP/appledouble-listing.txt"

  "$@" >"$listing"
  if grep -Eq '(^|/)\._' "$listing"; then
    echo "$description contains forbidden AppleDouble entries" >&2
    cat "$listing" >&2
    exit 1
  fi
}

VERSION="v0.1.0-rc.1"

LINUX_DIST="$TMP/linux-dist"
LINUX_GO_LOG="$TMP/linux-go.log"
PATH="$MOCK_BIN:$PATH" \
  MOCK_UNAME_S="Linux" \
  MOCK_UNAME_M="x86_64" \
  MOCK_GO_LOG="$LINUX_GO_LOG" \
  OPENBITDO_DIST_ROOT="$LINUX_DIST" \
  OPENBITDO_GIT_COMMIT="0123456789ab" \
  OPENBITDO_BUILD_DATE="2026-08-27T00:00:00Z" \
  OPENBITDO_GIT_DIRTY="false" \
  bash "$ROOT/scripts/package-linux.sh" "$VERSION" x86_64

LINUX_BASE="openbitdo-${VERSION}-linux-x86_64"
test "$(find "$LINUX_DIST" -type f | wc -l | tr -d ' ')" -eq 4
test -x "$LINUX_DIST/$LINUX_BASE"
test -s "$LINUX_DIST/$LINUX_BASE.tar.gz"
assert_checksum "$LINUX_DIST/$LINUX_BASE"
assert_checksum "$LINUX_DIST/$LINUX_BASE.tar.gz"
grep -Fq 'GOOS=linux GOARCH=amd64 CGO_ENABLED=1 MACOSX_DEPLOYMENT_TARGET=' "$LINUX_GO_LOG"
grep -Fq -- '-X main.appVersion=v0.1.0-rc.1' "$LINUX_GO_LOG"
grep -Fq -- '-X main.buildPlatform=linux/amd64' "$LINUX_GO_LOG"
grep -Fq -- '-X main.gitDirty=false' "$LINUX_GO_LOG"

cat >"$TMP/linux-expected.txt" <<'EOF'
LICENSE
README.md
bin/openbitdo
share/bash-completion/completions/openbitdo
share/fish/vendor_completions.d/openbitdo.fish
share/udev/rules.d/99-openbitdo.rules
share/zsh/site-functions/_openbitdo
EOF
assert_archive_files "$LINUX_DIST/$LINUX_BASE.tar.gz" "$TMP/linux-expected.txt"

MACOS_DIST="$TMP/macos-dist"
MACOS_GO_LOG="$TMP/macos-go.log"
PKGROOT_LIST="$TMP/pkgroot-files.txt"
PKGBUILD_LOG="$TMP/pkgbuild.log"
XATTR_LOG="$TMP/xattr.log"
PATH="$MOCK_BIN:$PATH" \
  MOCK_UNAME_S="Darwin" \
  MOCK_UNAME_M="arm64" \
  MOCK_GO_LOG="$MACOS_GO_LOG" \
  MOCK_PKGROOT_LIST="$PKGROOT_LIST" \
  MOCK_PKGBUILD_LOG="$PKGBUILD_LOG" \
  MOCK_XATTR_LOG="$XATTR_LOG" \
  OPENBITDO_DIST_ROOT="$MACOS_DIST" \
  OPENBITDO_GIT_COMMIT="0123456789ab" \
  OPENBITDO_BUILD_DATE="2026-08-27T00:00:00Z" \
  OPENBITDO_GIT_DIRTY="false" \
  bash "$ROOT/scripts/package-macos.sh" "$VERSION" arm64

MACOS_BASE="openbitdo-${VERSION}-macos-arm64"
test "$(find "$MACOS_DIST" -type f | wc -l | tr -d ' ')" -eq 6
test -x "$MACOS_DIST/$MACOS_BASE"
test -s "$MACOS_DIST/$MACOS_BASE.tar.gz"
test -s "$MACOS_DIST/$MACOS_BASE.pkg"
assert_checksum "$MACOS_DIST/$MACOS_BASE"
assert_checksum "$MACOS_DIST/$MACOS_BASE.tar.gz"
assert_checksum "$MACOS_DIST/$MACOS_BASE.pkg"
grep -Fq 'GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 MACOSX_DEPLOYMENT_TARGET=13.0' "$MACOS_GO_LOG"
grep -Fq -- '-X main.appVersion=v0.1.0-rc.1' "$MACOS_GO_LOG"
grep -Fq -- '-X main.buildPlatform=darwin/arm64' "$MACOS_GO_LOG"
grep -Fq -- '-X main.gitDirty=false' "$MACOS_GO_LOG"

cat >"$TMP/macos-expected.txt" <<'EOF'
LICENSE
README.md
bin/openbitdo
share/bash-completion/completions/openbitdo
share/fish/vendor_completions.d/openbitdo.fish
share/zsh/site-functions/_openbitdo
EOF
assert_archive_files "$MACOS_DIST/$MACOS_BASE.tar.gz" "$TMP/macos-expected.txt"
assert_no_appledouble \
  "macOS archive" \
  tar -tzf "$MACOS_DIST/$MACOS_BASE.tar.gz"
assert_no_appledouble \
  "macOS installer package" \
  env PATH="$MOCK_BIN:$PATH" MOCK_PKGROOT_LIST="$PKGROOT_LIST" \
  pkgutil --payload-files "$MACOS_DIST/$MACOS_BASE.pkg"

cat >"$TMP/pkgroot-expected.txt" <<'EOF'
usr/local/bin/openbitdo
usr/local/share/bash-completion/completions/openbitdo
usr/local/share/fish/vendor_completions.d/openbitdo.fish
usr/local/share/zsh/site-functions/_openbitdo
EOF
diff -u "$TMP/pkgroot-expected.txt" "$PKGROOT_LIST"
grep -Fq 'identifier=io.openbitdo.cli' "$PKGBUILD_LOG"
grep -Fq 'version=0.1.0-rc.1' "$PKGBUILD_LOG"
grep -Fq 'minimum_os=13.0' "$PKGBUILD_LOG"
grep -Fq -- '-cr ' "$XATTR_LOG"
