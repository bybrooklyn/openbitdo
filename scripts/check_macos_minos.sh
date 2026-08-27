#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 <mach-o-binary> [expected-minimum-macos-version]" >&2
  exit 2
fi

binary="$1"
expected="${2:-13.0}"

if [[ ! -s "$binary" ]]; then
  echo "missing or empty Mach-O binary: $binary" >&2
  exit 1
fi
if ! command -v xcrun >/dev/null 2>&1; then
  echo "xcrun is required for the macOS deployment-target audit" >&2
  exit 1
fi

minimum="$(xcrun vtool -show-build "$binary" | awk '$1 == "minos" { print $2; exit }')"
if [[ -z "$minimum" ]]; then
  echo "unable to read a Mach-O minimum macOS version from $binary" >&2
  exit 1
fi
if [[ "$minimum" != "$expected" && "$minimum" != "${expected}.0" ]]; then
  echo "Mach-O minimum macOS version is ${minimum}; expected ${expected}" >&2
  exit 1
fi

echo "Mach-O minimum macOS version is ${minimum}"
