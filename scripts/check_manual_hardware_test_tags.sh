#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# These patterns identify tests that enumerate arbitrary host HID devices,
# open the vendor channel directly, or ask a human to operate attached
# hardware. Every matching test file must be excluded from ordinary test and
# CI runs by a `manual` build constraint.
patterns=(
  'hid\.Enumerate\('
  'Open\(0x2dc8'
  'IsDevicePresent\('
  '\.ListDevices\('
  'real-hardware smoke'
  'live probe'
  'physical controller'
  'whatever real HID'
  'whatever 8BitDo device'
)

candidate_list="$(mktemp)"
trap 'rm -f "$candidate_list"' EXIT
: >"$candidate_list"

for pattern in "${patterns[@]}"; do
  rg -l --glob '*_test.go' "$pattern" internal cmd >>"$candidate_list" || true
done

LC_ALL=C sort -u "$candidate_list" -o "$candidate_list"
if [[ ! -s "$candidate_list" ]]; then
  echo "no live-hardware tests were detected; update the safety matcher if tests moved" >&2
  exit 1
fi

failed=0
while IFS= read -r file; do
  build_tag="$(awk 'NR <= 10 && /^\/\/go:build / { print; exit }' "$file")"
  if [[ "$build_tag" != *manual* ]]; then
    echo "live-hardware test is missing a manual build constraint: $file" >&2
    failed=1
  fi
done <"$candidate_list"

if [[ "$failed" -ne 0 ]]; then
  exit 1
fi

echo "all detected live-hardware tests require the manual build tag"
