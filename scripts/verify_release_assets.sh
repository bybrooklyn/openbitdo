#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 || $# -gt 3 ]]; then
  echo "usage: $0 <tag> <asset-directory> [all|linux-x86_64|linux-aarch64|macos-arm64]" >&2
  exit 2
fi

tag="$1"
asset_dir="${2%/}"
scope="${3:-all}"

if [[ ! -d "$asset_dir" ]]; then
  echo "asset directory does not exist: $asset_dir" >&2
  exit 1
fi

expected=()
add_linux_assets() {
  local arch="$1"
  local base="openbitdo-${tag}-linux-${arch}"
  expected+=("$base" "$base.sha256" "$base.tar.gz" "$base.tar.gz.sha256")
}
add_macos_assets() {
  local base="openbitdo-${tag}-macos-arm64"
  expected+=("$base" "$base.sha256" "$base.tar.gz" "$base.tar.gz.sha256" "$base.pkg" "$base.pkg.sha256")
}

case "$scope" in
  all)
    add_linux_assets x86_64
    add_linux_assets aarch64
    add_macos_assets
    ;;
  linux-x86_64) add_linux_assets x86_64 ;;
  linux-aarch64) add_linux_assets aarch64 ;;
  macos-arm64) add_macos_assets ;;
  *)
    echo "unsupported asset scope: $scope" >&2
    exit 2
    ;;
esac

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT
printf '%s\n' "${expected[@]}" | LC_ALL=C sort >"$TMP_ROOT/expected"
find "$asset_dir" -mindepth 1 -maxdepth 1 -print \
  | sed "s|^${asset_dir}/||" \
  | LC_ALL=C sort >"$TMP_ROOT/actual"

if ! diff -u "$TMP_ROOT/expected" "$TMP_ROOT/actual"; then
  echo "release asset manifest mismatch for scope ${scope}" >&2
  exit 1
fi

for name in "${expected[@]}"; do
  path="$asset_dir/$name"
  if [[ -L "$path" || ! -f "$path" || ! -s "$path" ]]; then
    echo "release asset is missing or empty: $name" >&2
    exit 1
  fi
done

for name in "${expected[@]}"; do
  if [[ "$name" != *.sha256 ]]; then
    continue
  fi
  sidecar="$asset_dir/$name"
  target_name="${name%.sha256}"
  target="$asset_dir/$target_name"
  word_count="$(wc -w <"$sidecar" | tr -d '[:space:]')"
  if [[ "$word_count" != "2" ]]; then
    echo "checksum sidecar must contain exactly a digest and basename: $name" >&2
    exit 1
  fi
  read -r expected_digest recorded_name <"$sidecar"
  recorded_name="${recorded_name#\*}"
  if [[ "$recorded_name" != "$target_name" ]]; then
    echo "checksum sidecar must use basename '${target_name}', found '${recorded_name}'" >&2
    exit 1
  fi
  if [[ ! "$expected_digest" =~ ^[0-9a-fA-F]{64}$ ]]; then
    echo "invalid SHA-256 digest in $name" >&2
    exit 1
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    actual_digest="$(sha256sum "$target" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual_digest="$(shasum -a 256 "$target" | awk '{print $1}')"
  else
    echo "no SHA-256 tool available while verifying $name" >&2
    exit 1
  fi
  normalized_actual="$(printf '%s' "$actual_digest" | tr '[:upper:]' '[:lower:]')"
  normalized_expected="$(printf '%s' "$expected_digest" | tr '[:upper:]' '[:lower:]')"
  if [[ "$normalized_actual" != "$normalized_expected" ]]; then
    echo "checksum mismatch for $target_name" >&2
    exit 1
  fi
done

case "$scope" in
  linux-x86_64) executable="openbitdo-${tag}-linux-x86_64" ;;
  linux-aarch64) executable="openbitdo-${tag}-linux-aarch64" ;;
  macos-arm64) executable="openbitdo-${tag}-macos-arm64" ;;
  all) executable="" ;;
esac
if [[ -n "$executable" && ! -x "$asset_dir/$executable" ]]; then
  echo "standalone binary is not executable: $executable" >&2
  exit 1
fi

echo "verified ${#expected[@]} nonempty release assets for ${scope}"
