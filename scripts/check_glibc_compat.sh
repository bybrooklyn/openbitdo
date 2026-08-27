#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 <linux-binary> [maximum-glibc-version]" >&2
  exit 2
fi

binary="$1"
maximum="${2:-2.35}"

if [[ ! -s "$binary" ]]; then
  echo "missing or empty Linux binary: $binary" >&2
  exit 1
fi
if ! command -v readelf >/dev/null 2>&1; then
  echo "readelf is required for the glibc compatibility audit" >&2
  exit 1
fi

if ! readelf_output="$(LC_ALL=C readelf --version-info "$binary")"; then
  echo "readelf could not inspect $binary" >&2
  exit 1
fi

versions="$(printf '%s\n' "$readelf_output" \
  | sed -nE 's/.*GLIBC_([0-9]+\.[0-9]+(\.[0-9]+)?).*/\1/p' \
  | LC_ALL=C sort -Vu)"

if [[ -z "$versions" ]]; then
  echo "no imported GLIBC symbol versions found (binary may be static)"
  exit 0
fi

too_new=0
while IFS= read -r version; do
  newest="$(printf '%s\n%s\n' "$maximum" "$version" | LC_ALL=C sort -V | tail -n 1)"
  if [[ "$newest" != "$maximum" ]]; then
    echo "binary imports GLIBC_${version}, newer than the supported GLIBC_${maximum} ceiling" >&2
    too_new=1
  fi
done <<<"$versions"

if [[ "$too_new" -ne 0 ]]; then
  exit 1
fi

echo "all imported GLIBC symbols are compatible with GLIBC_${maximum}"
