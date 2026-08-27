#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <binary> <linux|macos> <x86_64|aarch64|arm64>" >&2
  exit 2
fi

binary="$1"
platform="$2"
architecture="$3"

if [[ -L "$binary" || ! -f "$binary" || ! -s "$binary" || ! -x "$binary" ]]; then
  echo "missing, empty, symlinked, or non-executable binary: $binary" >&2
  exit 1
fi

case "${platform}/${architecture}" in
  linux/x86_64)
    command -v readelf >/dev/null 2>&1 || { echo "readelf is required" >&2; exit 1; }
    machine="$(LC_ALL=C readelf --file-header "$binary" | awk -F: '$1 ~ /^[[:space:]]*Machine$/ { sub(/^[[:space:]]+/, "", $2); print $2; exit }')"
    [[ "$machine" == "Advanced Micro Devices X86-64" ]] || {
      echo "expected Linux x86_64 ELF, found machine '${machine:-unknown}'" >&2
      exit 1
    }
    ;;
  linux/aarch64)
    command -v readelf >/dev/null 2>&1 || { echo "readelf is required" >&2; exit 1; }
    machine="$(LC_ALL=C readelf --file-header "$binary" | awk -F: '$1 ~ /^[[:space:]]*Machine$/ { sub(/^[[:space:]]+/, "", $2); print $2; exit }')"
    [[ "$machine" == "AArch64" ]] || {
      echo "expected Linux aarch64 ELF, found machine '${machine:-unknown}'" >&2
      exit 1
    }
    ;;
  macos/arm64)
    command -v xcrun >/dev/null 2>&1 || { echo "xcrun is required" >&2; exit 1; }
    architectures="$(xcrun lipo -archs "$binary")"
    [[ "$architectures" == "arm64" ]] || {
      echo "expected a thin macOS arm64 Mach-O, found '${architectures:-unknown}'" >&2
      exit 1
    }
    ;;
  *)
    echo "unsupported binary platform/architecture: ${platform}/${architecture}" >&2
    exit 2
    ;;
esac

echo "binary architecture is ${platform}/${architecture}"
