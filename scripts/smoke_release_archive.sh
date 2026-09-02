#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 4 ]]; then
  echo "usage: $0 <tag> <linux|macos> <x86_64|aarch64|arm64> <asset-directory>" >&2
  exit 2
fi

tag="$1"
platform="$2"
arch_label="$3"
asset_dir="${4%/}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
base="openbitdo-${tag}-${platform}-${arch_label}"
archive="$asset_dir/${base}.tar.gz"

if [[ ! -s "$archive" ]]; then
  echo "missing or empty release archive: $archive" >&2
  exit 1
fi
archive_listing="$(tar -tzf "$archive")"
if grep -Eq '(^|/)\._' <<<"$archive_listing"; then
  echo "release archive contains AppleDouble metadata files" >&2
  exit 1
fi
while IFS= read -r archived_path; do
  case "$archived_path" in
    "$base"|"$base/"*) ;;
    *)
      echo "release archive contains a path outside ${base}: ${archived_path}" >&2
      exit 1
      ;;
  esac
done <<<"$archive_listing"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT
tar -xzf "$archive" -C "$TMP_ROOT"
payload="$TMP_ROOT/$base"
binary="$payload/bin/openbitdo"

unexpected_types="$(find "$payload" -mindepth 1 ! -type f ! -type d -print)"
if [[ -n "$unexpected_types" ]]; then
  printf 'release archive contains symlinks or special files:\n%s\n' "$unexpected_types" >&2
  exit 1
fi

required=(
  bin/openbitdo
  README.md
  LICENSE
  share/bash-completion/completions/openbitdo
  share/fish/vendor_completions.d/openbitdo.fish
  share/zsh/site-functions/_openbitdo
)
if [[ "$platform" == "linux" ]]; then
  required+=(share/udev/rules.d/99-openbitdo.rules)
fi

for relative in "${required[@]}"; do
  if [[ ! -s "$payload/$relative" ]]; then
    echo "release archive is missing required payload: $relative" >&2
    exit 1
  fi
done

printf '%s\n' "${required[@]}" | LC_ALL=C sort >"$TMP_ROOT/expected-files"
find "$payload" -type f -print \
  | sed "s|^${payload}/||" \
  | LC_ALL=C sort >"$TMP_ROOT/actual-files"
if ! diff -u "$TMP_ROOT/expected-files" "$TMP_ROOT/actual-files"; then
  echo "release archive contains missing or unexpected payload files" >&2
  exit 1
fi
if [[ ! -x "$binary" ]]; then
  echo "release archive binary is not executable: $binary" >&2
  exit 1
fi

case "${platform}/${arch_label}" in
  linux/x86_64) expected_platform="linux/amd64" ;;
  linux/aarch64) expected_platform="linux/arm64" ;;
  macos/arm64) expected_platform="darwin/arm64" ;;
  *)
    echo "unsupported release archive platform: ${platform}/${arch_label}" >&2
    exit 2
    ;;
esac

version_output="$($binary --version)"
if [[ "$version_output" != *"openbitdo ${tag}"* || "$version_output" != *"${expected_platform}"* ]]; then
  echo "unexpected --version output: $version_output" >&2
  exit 1
fi
if [[ "$version_output" == *"commit unknown"* || "$version_output" == *"built unknown"* ]]; then
  echo "packaged --version output is missing commit or build-date metadata: $version_output" >&2
  exit 1
fi
if [[ -n "${OPENBITDO_EXPECT_COMMIT:-}" ]]; then
  expected_commit="$OPENBITDO_EXPECT_COMMIT"
else
  expected_commit="$(git -C "$repo_root" rev-parse --short=12 HEAD)"
fi
if [[ "$version_output" != *"commit ${expected_commit}"* ]]; then
  echo "packaged --version commit does not match source ${expected_commit}: $version_output" >&2
  exit 1
fi
if [[ ! "$version_output" =~ built[[:space:]][0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z ]]; then
  echo "packaged --version build date is not UTC RFC 3339: $version_output" >&2
  exit 1
fi
if [[ -n "${OPENBITDO_EXPECT_DIRTY:-}" && "$version_output" != *"dirty=${OPENBITDO_EXPECT_DIRTY}"* ]]; then
  echo "packaged --version dirty state does not equal ${OPENBITDO_EXPECT_DIRTY}: $version_output" >&2
  exit 1
fi

HOME="$TMP_ROOT/home" XDG_CONFIG_HOME="$TMP_ROOT/config" \
  "$binary" --mock --diagnostics-dump >"$TMP_ROOT/diagnostics.toml"
if [[ ! -s "$TMP_ROOT/diagnostics.toml" ]]; then
  echo "mock diagnostics dump produced no output" >&2
  exit 1
fi

if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 is required for the packaged TUI PTY smoke" >&2
  exit 1
fi
python3 "$script_dir/smoke_tui_pty.py" "$binary"

echo "release archive smoke passed for ${platform}/${arch_label}"
